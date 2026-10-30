package releasechecktest

import (
	"bytes"
	"errors"
	"github.com/rezzminator/engine-sync/internal/testjail"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

const version = "9.9.9"
const freshSums = "aaaa  engine-sync_9.9.9_linux_amd64\n"
const staleSums = "bbbb  engine-sync_9.9.9_linux_amd64\n"
const stubBuild = `#!/usr/bin/env bash
set -eu
[ "$1" = --out ] || exit 2
printf '%s' "$FRESH_SUMS" > "$2/SHA256SUMS"
`

// releaseCheckTree writes matching version places and the requested committed sums.
func releaseCheckTree(t *testing.T, committed, changelogHeading string) string {
	t.Helper()
	root := testjail.Scratch(t)
	src, err := os.ReadFile(filepath.Join("..", "..", "scripts", "release-check.sh"))
	if err != nil {
		t.Fatal(err)
	}
	files := map[string]string{
		"scripts/release-check.sh":                       string(src),
		"scripts/build-release.sh":                       stubBuild,
		"plugins/engine-sync/.claude-plugin/plugin.json": `{"name":"engine-sync","version":"` + version + `"}`,
		"README.md":                              "[![Version](https://img.shields.io/badge/version-" + version + "-blue)](./CHANGELOG.md)\n",
		"CHANGELOG.md":                           "# Changelog\n\n" + changelogHeading + "\n",
		"plugins/engine-sync/libexec/SHA256SUMS": committed,
	}
	for rel, body := range files {
		releaseCheckWriteFixture(t, root, rel, body)
	}
	return root
}

func releaseCheckWriteFixture(t *testing.T, root, rel, body string) {
	t.Helper()
	p := filepath.Join(root, rel)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	mode := os.FileMode(0o644)
	if strings.HasSuffix(rel, ".sh") {
		mode = 0o755
	}
	if err := os.WriteFile(p, []byte(body), mode); err != nil {
		t.Fatal(err)
	}
}

// runReleaseCheck returns the script's exit code and combined output.
func runReleaseCheck(t *testing.T, root string, extraEnv []string, args ...string) (int, string) {
	t.Helper()
	cmd := exec.Command(filepath.Join(root, "scripts", "release-check.sh"), args...)
	// Keep the fence's tool lookup and jailed filesystem roots, not interpreter
	// startup hooks or Python configuration from the caller's shell.
	for _, value := range append(os.Environ(), extraEnv...) {
		name, _, _ := strings.Cut(value, "=")
		switch name {
		case "PATH", "HOME", "TMPDIR", "XDG_DATA_HOME", "XDG_STATE_HOME":
			cmd.Env = append(cmd.Env, value)
		}
	}
	cmd.Env = append(cmd.Env, "FRESH_SUMS="+freshSums)
	var out bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &out
	err := cmd.Run()
	var exit *exec.ExitError
	switch {
	case err == nil:
		return 0, out.String()
	case errors.As(err, &exit):
		return exit.ExitCode(), out.String()
	default:
		t.Fatalf("run release-check.sh: %v", err)
		return -1, ""
	}
}

// The release ledger stays pinned between releases; only --release compares it.
func TestReleaseCheckComparesSumsOnlyForARelease(t *testing.T) {
	dated := "## [" + version + "] — 2026-01-02"
	undated := "## [" + version + "]"
	plain := "release-check: ok: " + version + " (places agree; plugins/engine-sync/libexec/SHA256SUMS not compared, only --release compares it with a fresh build)\n"
	failedBuilder := map[string]string{"scripts/build-release.sh": "#!/usr/bin/env bash\necho builder-unavailable >&2\nexit 9\n"}
	tests := []struct {
		name      string
		committed string
		heading   string
		args      []string
		wantCode  int
		wantOut   string
		files     map[string]string
		remove    string
		env       []string
	}{
		{name: "plain stale sums pass with explicit non-comparison", committed: staleSums, heading: undated, wantOut: plain},
		{name: "release check fails on stale sums", committed: staleSums, heading: dated, args: []string{"--release"}, wantCode: 1, wantOut: "SHA256SUMS is stale"},
		{name: "release check passes on fresh sums", committed: freshSums, heading: dated, args: []string{"--release"}, wantOut: "release-check: ok: " + version + " (release)\n"},
		{name: "plain missing heading fails", committed: staleSums, heading: "## [0.0.1]", wantCode: 1, wantOut: "has no '## [" + version + "]' heading"},
		{name: "plain does not invoke failing builder", committed: staleSums, heading: undated, files: failedBuilder, wantOut: plain},
		{name: "release reports builder failure", committed: freshSums, heading: dated, args: []string{"--release"}, files: failedBuilder, wantCode: 1, wantOut: "build-release.sh failed, so plugins/engine-sync/libexec/SHA256SUMS cannot be compared:"},
		{name: "release requires dated heading", committed: freshSums, heading: undated, args: []string{"--release"}, wantCode: 1, wantOut: "a release carries its date"},
		{name: "plain rejects disagreeing badge", committed: staleSums, heading: undated, files: map[string]string{"README.md": "badge/version-0.0.1-blue\n"}, wantCode: 1, wantOut: "README.md badge says '0.0.1'"},
		{name: "plain rejects nonincreasing base", committed: staleSums, heading: undated, args: []string{version}, wantCode: 1, wantOut: "does not sort after " + version},
		{name: "plain still requires readable ledger", committed: staleSums, heading: undated, remove: "plugins/engine-sync/libexec/SHA256SUMS", wantCode: 2, wantOut: "cannot read plugins/engine-sync/libexec/SHA256SUMS"},
		{name: "plain refuses unknown option", committed: staleSums, heading: undated, args: []string{"--unknown"}, wantCode: 2, wantOut: "Usage: release-check.sh"},
		{name: "fixture ignores inherited bash startup", committed: staleSums, heading: undated, files: map[string]string{"startup.sh": "exit 19\n"}, env: []string{"BASH_ENV={ROOT}/startup.sh"}, wantOut: plain},
		{name: "fixture ignores inherited python module path", committed: staleSums, heading: undated, files: map[string]string{"poison/json.py": "raise SystemExit('untrusted json')\n"}, env: []string{"PYTHONPATH={ROOT}/poison"}, wantOut: plain},
		{name: "fixture ignores inherited python home", committed: staleSums, heading: undated, env: []string{"PYTHONHOME={ROOT}/absent-python"}, wantOut: plain},
	}
	for _, tc := range tests {
		// Serial: a parallel case's fork can inherit the write descriptor of
		// another case's fixture script, and exec of that script then fails
		// with ETXTBSY ("text file busy").
		t.Run(tc.name, func(t *testing.T) {
			root := releaseCheckTree(t, tc.committed, tc.heading)
			for rel, body := range tc.files {
				releaseCheckWriteFixture(t, root, rel, body)
			}
			if tc.remove != "" {
				if err := os.Remove(filepath.Join(root, tc.remove)); err != nil {
					t.Fatal(err)
				}
			}
			env := make([]string, len(tc.env))
			for i, value := range tc.env {
				env[i] = strings.ReplaceAll(value, "{ROOT}", root)
			}
			code, out := runReleaseCheck(t, root, env, tc.args...)
			if code != tc.wantCode {
				t.Fatalf("exit %d, want %d; output:\n%s", code, tc.wantCode, out)
			}
			if code == 0 {
				if out != tc.wantOut {
					t.Fatalf("output %q, want %q", out, tc.wantOut)
				}
			} else if !strings.Contains(out, tc.wantOut) {
				t.Fatalf("output lacks %q:\n%s", tc.wantOut, out)
			}
		})
	}
}

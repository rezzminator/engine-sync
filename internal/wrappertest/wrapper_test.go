package wrappertest

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/rezzminator/engine-sync/internal/testjail"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

const version = "0.1.0"

const fakeBinary = "#!/bin/sh\nprintf 'ran %s args=%s\\n' \"$FAKE_NAME\" \"$*\"\n"

type rig struct {
	wrapper string
	home    string
	env     []string
}

func newRig(t *testing.T, sums string) rig {
	t.Helper()
	base := testjail.Scratch(t)
	plugin := filepath.Join(base, "plugin")
	src, err := os.ReadFile(filepath.Join("..", "..", "plugins", "engine-sync", "libexec", "engine-sync"))
	if err != nil {
		t.Fatalf("read the wrapper: %v", err)
	}
	wrapper := filepath.Join(plugin, "libexec", "engine-sync")
	writeFile(t, wrapper, string(src), 0o755)
	writeFile(t, filepath.Join(plugin, "libexec", "SHA256SUMS"), sums, 0o644)
	writeFile(t, filepath.Join(plugin, ".claude-plugin", "plugin.json"),
		`{"name": "engine-sync", "version": "`+version+`"}`+"\n", 0o644)
	home := filepath.Join(base, "es-home")
	return rig{wrapper: wrapper, home: home, env: []string{
		"PATH=/usr/bin:/bin", "HOME=" + filepath.Join(base, "home"), "ENGINE_SYNC_HOME=" + home,
		"ENGINE_SYNC_RELEASE_BASE=http://127.0.0.1:1/unreachable",
	}}
}

func writeFile(t *testing.T, path, content string, mode os.FileMode) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), mode); err != nil {
		t.Fatal(err)
	}
}

// cache installs a fake binary as version v's cached copy.
func (r rig) cache(t *testing.T, v string) {
	writeFile(t, filepath.Join(r.home, "versions", v, "engine-sync"), fakeBinary, 0o755)
}

func (r rig) run(t *testing.T, stdin string, extra []string, args ...string) (string, string, int) {
	t.Helper()
	cmd := exec.Command(r.wrapper, args...)
	cmd.Env = append(append([]string{}, r.env...), extra...)
	cmd.Stdin = strings.NewReader(stdin)
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	err := cmd.Run()
	code := 0
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		code = exitErr.ExitCode()
	} else if err != nil {
		t.Fatalf("run the wrapper: %v", err)
	}
	return stdout.String(), stderr.String(), code
}

func (r rig) linkTarget(t *testing.T) string {
	t.Helper()
	target, err := os.Readlink(filepath.Join(r.home, "bin", "engine-sync"))
	if errors.Is(err, os.ErrNotExist) {
		return ""
	}
	if err != nil {
		t.Fatal(err)
	}
	return target
}

func assetName() string {
	return fmt.Sprintf("engine-sync_%s_%s_%s", version, runtime.GOOS, runtime.GOARCH)
}

func TestTheCacheRunsAndTheLinkOnlyMovesForward(t *testing.T) {
	for _, tc := range []struct {
		name, linked, want string
	}{
		{name: "absent", want: "../versions/0.1.0/engine-sync"},
		{name: "older", linked: "0.0.9", want: "../versions/0.1.0/engine-sync"},
		{name: "older pre-release core", linked: "0.1.0-alpha", want: "../versions/0.1.0/engine-sync"},
		{name: "newer", linked: "0.2.0", want: "../versions/0.2.0/engine-sync"},
		{name: "dangling", linked: "missing", want: "../versions/0.1.0/engine-sync"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := newRig(t, "")
			r.cache(t, version)
			if tc.linked != "" {
				if tc.linked != "missing" {
					r.cache(t, tc.linked)
				}
				if err := os.MkdirAll(filepath.Join(r.home, "bin"), 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink("../versions/"+tc.linked+"/engine-sync", filepath.Join(r.home, "bin", "engine-sync")); err != nil {
					t.Fatal(err)
				}
			}
			out, stderr, code := r.run(t, "", []string{"FAKE_NAME=cached"}, "version")
			if code != 0 || out != "ran cached args=version\n" {
				t.Fatalf("code %d, stdout %q, stderr %q: the cached copy runs", code, out, stderr)
			}
			if got := r.linkTarget(t); got != tc.want {
				t.Fatalf("stable link = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestTheDownloadIsVerifiedInstalledAndLinked(t *testing.T) {
	for _, tc := range []struct {
		name     string
		summed   string
		wantCode int
		wantOut  string
		wantErr  string
	}{
		{name: "verified", summed: fakeBinary, wantOut: "ran downloaded args=build .\n"},
		{name: "mismatch", summed: "other bytes", wantCode: 1, wantErr: "engine-sync: binary unavailable: checksum mismatch"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
				if req.URL.Path != "/v"+version+"/"+assetName() {
					http.NotFound(w, req)
					return
				}
				if _, err := w.Write([]byte(fakeBinary)); err != nil {
					t.Errorf("serve the asset: %v", err)
				}
			}))
			defer server.Close()
			sum := sha256.Sum256([]byte(tc.summed))
			r := newRig(t, hex.EncodeToString(sum[:])+"  "+assetName()+"\n")
			out, stderr, code := r.run(t, "", []string{"ENGINE_SYNC_RELEASE_BASE=" + server.URL, "FAKE_NAME=downloaded"}, "build", ".")
			if code != tc.wantCode || out != tc.wantOut || !strings.Contains(stderr, tc.wantErr) {
				t.Fatalf("code %d, stdout %q, stderr %q; want %d, %q, %q", code, out, stderr, tc.wantCode, tc.wantOut, tc.wantErr)
			}
			_, statErr := os.Stat(filepath.Join(r.home, "versions", version, "engine-sync"))
			if installed := statErr == nil; installed != (tc.wantCode == 0) {
				t.Fatalf("installed = %v after %s", installed, tc.name)
			}
			wantLink := ""
			if tc.wantCode == 0 {
				wantLink = "../versions/0.1.0/engine-sync"
			}
			if got := r.linkTarget(t); got != wantLink {
				t.Fatalf("stable link = %q, want %q", got, wantLink)
			}
		})
	}
}

// TestAnUnavailableBinaryUnderAHook ports scripts/test-codex-sync.sh's missing
// compiler case: a Stop with no binary exits 0 and answers a systemMessage
// naming the unavailability and the retained dirty flag; every other hook
// exits 0 silently on stdout; a plain command exits 1.
func TestAnUnavailableBinaryUnderAHook(t *testing.T) {
	r := newRig(t, "")
	out, stderr, code := r.run(t, `{"stop_hook_active":false}`, nil, "hook", "claude", "stop")
	if code != 0 {
		t.Fatalf("a Stop hook must never exit nonzero; got %d, stderr %q", code, stderr)
	}
	var answer map[string]string
	if err := json.Unmarshal([]byte(out), &answer); err != nil {
		t.Fatalf("no JSON systemMessage — the failed lookup was silent: %v, stdout %q", err, out)
	}
	for _, part := range []string{"binary unavailable", "dirty flag retained", "no checksum for " + assetName()} {
		if !strings.Contains(answer["systemMessage"], part) {
			t.Fatalf("systemMessage %q does not name %q", answer["systemMessage"], part)
		}
	}
	if out, _, code := r.run(t, `{}`, nil, "hook", "claude", "mark"); code != 0 || out != "" {
		t.Fatalf("mark: code %d, stdout %q: a hook exits 0 and keeps stdout clean", code, out)
	}
	if _, stderr, code := r.run(t, "", nil, "check", "."); code != 1 || !strings.Contains(stderr, "engine-sync: binary unavailable: no checksum") {
		t.Fatalf("check: code %d, stderr %q: a plain command fails naming the cause", code, stderr)
	}
}

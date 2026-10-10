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
	"time"
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
	sum := sha256.Sum256([]byte(fakeBinary))
	writeFile(t, filepath.Join(filepath.Dir(r.wrapper), "SHA256SUMS"), hex.EncodeToString(sum[:])+"  "+assetName()+"\n", 0644)
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
// naming the unavailability and the next recovery opportunity; every other hook
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
	for _, part := range []string{"binary unavailable", "mirrors refresh at the next SessionStart with an available binary", "no checksum for " + assetName()} {
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

func TestReviewWrapperSafety(t *testing.T) {
	t.Run("child failure", func(t *testing.T) {
		r := newRig(t, "")
		binary := filepath.Join(r.home, "broken")
		writeFile(t, binary, "#!/bin/sh\nexit 7\n", 0755)
		out, errOut, rc := r.run(t, `{}`, []string{"ENGINE_SYNC_BIN=" + binary}, "hook", "claude", "stop")
		if rc != 0 || !strings.Contains(errOut, "7") {
			t.Errorf("hook failed rc=%d %s%s", rc, out, errOut)
		}
	})
	t.Run("tampered cache", func(t *testing.T) {
		r := newRig(t, "")
		r.cache(t, version)
		writeFile(t, filepath.Join(r.home, "versions", version, "engine-sync"), "#!/bin/sh\necho unverified\n", 0755)
		out, _, rc := r.run(t, "", nil, "hook", "claude", "mark")
		if rc != 0 || strings.Contains(out, "unverified") {
			t.Errorf("unverified binary ran rc=%d %s", rc, out)
		}
	})
	t.Run("unavailable JSON", func(t *testing.T) {
		r := newRig(t, "")
		r.env = append(r.env, "ENGINE_SYNC_HOME="+r.home+`"quoted`)
		out, _, rc := r.run(t, `{}`, nil, "hook", "claude", "stop")
		var value map[string]string
		if rc != 0 || json.Unmarshal([]byte(out), &value) != nil || strings.Contains(out, "dirty flag retained") {
			t.Errorf("false or invalid message: %d %s", rc, out)
		}
	})
	t.Run("backoff", func(t *testing.T) {
		calls := 0
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			calls++
			http.Error(w, "unavailable", http.StatusServiceUnavailable)
		}))
		defer server.Close()
		sum := sha256.Sum256([]byte(fakeBinary))
		r := newRig(t, hex.EncodeToString(sum[:])+"  "+assetName()+"\n")
		extra := []string{"ENGINE_SYNC_RELEASE_BASE=" + server.URL}
		for i := 0; i < 2; i++ {
			_, _, rc := r.run(t, `{}`, extra, "hook", "claude", "mark")
			if rc != 0 {
				t.Errorf("hook failed %d", rc)
			}
		}
		if calls != 1 {
			t.Errorf("download repeated %d", calls)
		}
	})
}

func TestReviewWrapperPreservesStdin(t *testing.T) {
	for _, args := range [][]string{{"hook", "claude", "mark"}, {"version"}} {
		t.Run(strings.Join(args, "-"), func(t *testing.T) {
			r := newRig(t, "")
			binary := filepath.Join(r.home, "echo-input")
			writeFile(t, binary, "#!/bin/sh\ncat\n", 0755)
			out, errOut, rc := r.run(t, "source payload", []string{"ENGINE_SYNC_BIN=" + binary}, args...)
			if rc != 0 || out != "source payload" {
				t.Fatalf("stdin lost: %d %q %s", rc, out, errOut)
			}
		})
	}
}

func TestTheOverrideRunsAndIsNeverLinked(t *testing.T) {
	r := newRig(t, "")
	r.cache(t, version)
	override := filepath.Join(testjail.Scratch(t), "dev-engine-sync")
	writeFile(t, override, fakeBinary, 0o755)
	out, stderr, code := r.run(t, "", []string{"ENGINE_SYNC_BIN=" + override, "FAKE_NAME=override"}, "codex", "check", ".")
	if code != 0 || out != "ran override args=codex check .\n" {
		t.Fatalf("code %d, stdout %q, stderr %q: $ENGINE_SYNC_BIN runs first", code, out, stderr)
	}
	if got := r.linkTarget(t); got != "" {
		t.Fatalf("the stable link names %q: an override is never linked", got)
	}
}

func TestReviewStopRejectsInvalidJSON(t *testing.T) {
	for _, body := range []string{"garbage", "{broken}", "{\"systemMessage\":\"x\"} trailing"} {
		r := newRig(t, "")
		binary := filepath.Join(r.home, "invalid-hook")
		writeFile(t, binary, "#!/bin/sh\nprintf '%s' '"+body+"'\n", 0755)
		out, stderr, rc := r.run(t, `{}`, []string{"ENGINE_SYNC_BIN=" + binary}, "hook", "claude", "stop")
		var value map[string]string
		if rc != 0 || json.Unmarshal([]byte(out), &value) != nil || !strings.Contains(stderr, "invalid Stop JSON") {
			t.Errorf("invalid child response was forwarded: rc=%d out=%q error=%q", rc, out, stderr)
		}
	}
}

func TestAuditDownloadLockRecovery(t *testing.T) {
	for _, form := range []string{"dead owner", "expired live owner", "live owner"} {
		t.Run(form, func(t *testing.T) {
			sum := sha256.Sum256([]byte(fakeBinary))
			r := newRig(t, hex.EncodeToString(sum[:])+"  "+assetName()+"\n")
			lock := filepath.Join(r.home, "versions", ".lock-"+version)
			owner := fmt.Sprint(os.Getpid())
			if form == "dead owner" {
				killed := exec.Command("/bin/sleep", "30")
				if err := killed.Start(); err != nil {
					t.Fatal(err)
				}
				owner = fmt.Sprint(killed.Process.Pid)
				if err := killed.Process.Kill(); err != nil {
					t.Fatal(err)
				}
				_ = killed.Wait()
			}
			writeFile(t, filepath.Join(lock, "owner"), owner+"\n", 0600)
			if form == "expired live owner" {
				old := time.Now().Add(-121 * time.Second)
				if err := os.Chtimes(lock, old, old); err != nil {
					t.Fatal(err)
				}
			}
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte(fakeBinary)) }))
			defer server.Close()
			before := time.Now()
			out, errout, rc := r.run(t, `{}`, []string{"ENGINE_SYNC_RELEASE_BASE=" + server.URL, "FAKE_NAME=recovered"}, "hook", "claude", "mark")
			if rc != 0 || time.Since(before) > 3*time.Second {
				t.Fatalf("unbounded hook rc=%d %s", rc, errout)
			}
			if form == "live owner" {
				if !strings.Contains(errout, "lock busy") || out != "" {
					t.Fatalf("live lock not honoured: %s %s", out, errout)
				}
			} else if !strings.Contains(out, "ran recovered") {
				t.Fatalf("orphan not recovered: %s %s", out, errout)
			}
		})
	}
}

func TestAuditUnavailableMarkResumesAtNextHook(t *testing.T) {
	r := newRig(t, "")
	project := filepath.Join(r.home, "project")
	writeFile(t, filepath.Join(project, ".claude", "engine-sync.json"), `{"version":1}`, 0644)
	env := []string{"CLAUDE_PROJECT_DIR=" + project}
	if _, _, rc := r.run(t, `{"tool_input":{"file_path":"CLAUDE.md"}}`, env, "hook", "claude", "mark"); rc != 0 {
		t.Fatal(rc)
	}
	binary := filepath.Join(r.home, "equipped")
	writeFile(t, binary, "#!/bin/sh\nif [ \"$3\" = start ];then printf synced >\"$CLAUDE_PROJECT_DIR/synced\";fi\n", 0755)
	out, errout, rc := r.run(t, `{}`, append(env, "ENGINE_SYNC_BIN="+binary), "hook", "claude", "stop")
	if _, err := os.Stat(filepath.Join(project, "synced")); err != nil || rc != 0 {
		t.Fatalf("next equipped hook lost edits: %s %s rc=%d err=%v", out, errout, rc, err)
	}
}

func TestAuditKilledDownloadRecoversThroughHook(t *testing.T) {
	sum := sha256.Sum256([]byte(fakeBinary))
	r := newRig(t, hex.EncodeToString(sum[:])+"  "+assetName()+"\n")
	started, release := make(chan struct{}), make(chan struct{})
	first := true
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if first {
			first = false
			close(started)
			<-release
		}
		_, _ = w.Write([]byte(fakeBinary))
	}))
	defer server.Close()
	cmd := exec.Command(r.wrapper, "hook", "claude", "mark")
	cmd.Env = append(r.env, "ENGINE_SYNC_RELEASE_BASE="+server.URL)
	cmd.Stdin = strings.NewReader(`{}`)
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	select {
	case <-started:
	case <-time.After(4 * time.Second):
		close(release)
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		t.Fatal("download did not start")
	}
	if err := cmd.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	_ = cmd.Wait()
	close(release)
	out, errout, rc := r.run(t, `{}`, []string{"ENGINE_SYNC_RELEASE_BASE=" + server.URL, "FAKE_NAME=recovered"}, "hook", "claude", "mark")
	if rc != 0 || !strings.Contains(out, "ran recovered") {
		t.Fatalf("killed download not recovered: %s %s rc=%d", out, errout, rc)
	}
}

func TestAuditHookCrashingAndEmptyChildren(t *testing.T) {
	for _, body := range []string{"#!/bin/sh\nkill -SEGV $$\n", ""} {
		t.Run(fmt.Sprintf("bytes-%d", len(body)), func(t *testing.T) {
			r := newRig(t, "")
			binary := filepath.Join(r.home, "child")
			writeFile(t, binary, body, 0755)
			out, errout, rc := r.run(t, `{}`, []string{"ENGINE_SYNC_BIN=" + binary}, "hook", "claude", "stop")
			if rc != 0 {
				t.Fatalf("hook leaked child status: %d %s", rc, errout)
			}
			{
				var obj map[string]string
				if err := json.Unmarshal([]byte(out), &obj); err != nil || obj["systemMessage"] == "" {
					t.Fatalf("crash not surfaced: %s %v", out, err)
				}
			}
		})
	}
}

func TestAuditHookHangingChildDeadline(t *testing.T) {
	r := newRig(t, "")
	binary := filepath.Join(r.home, "child")
	writeFile(t, binary, "#!/bin/sh\nexec sleep 20\n", 0755)
	before := time.Now()
	out, errout, rc := r.run(t, `{}`, []string{"ENGINE_SYNC_BIN=" + binary}, "hook", "claude", "stop")
	if rc != 0 || time.Since(before) > 18*time.Second || !strings.Contains(out, "systemMessage") {
		t.Fatalf("unbounded child: %s %s rc=%d elapsed=%s", out, errout, rc, time.Since(before))
	}
}

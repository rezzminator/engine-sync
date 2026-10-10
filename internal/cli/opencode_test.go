package cli

import (
	"bytes"
	"fmt"
	"github.com/rezzminator/engine-sync/internal/diag"
	"github.com/rezzminator/engine-sync/internal/paths"
	"github.com/rezzminator/engine-sync/internal/testjail"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

func TestRunCommandRejectsUnknownAction(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if code := invokeOpenCodeCLI(
		[]string{"generate"},
		func() (string, error) { return ".", nil },
		".",
		&stdout,
		&stderr,
	); code != 2 {
		t.Fatalf("unknown action code=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
}

func TestRunCommandBuildPassLineCountsAndNamesDeletions(t *testing.T) {
	root := testjail.Scratch(t)
	home := filepath.Join(root, "home")
	source := filepath.Join(root, ".claude", "commands", "review.md")
	putProjectFile(t, filepath.Dir(source), filepath.Base(source), "---\ndescription: Review.\n---\nReview.\n")
	build := func() string {
		t.Helper()
		var stdout, stderr bytes.Buffer
		if code := invokeOpenCodeCLI(
			[]string{"build", root, "--home", home},
			func() (string, error) { return root, nil },
			home,
			&stdout,
			&stderr,
		); code != 0 {
			t.Fatalf("build code=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
		}
		return stdout.String()
	}

	first := build()
	if !regexp.MustCompile(`(?m)^OPENCODE BUILD PASS wrote=[1-9][0-9]* unchanged=0 deleted=0$`).MatchString(first) {
		t.Fatalf("first build stdout=%q", first)
	}
	if err := os.Remove(source); err != nil {
		t.Fatal(err)
	}
	second := build()
	deleted := filepath.Join(root, ".opencode", "command", "review.md")
	if !regexp.MustCompile(`(?m)^OPENCODE BUILD PASS wrote=0 unchanged=[1-9][0-9]* deleted=1$`).MatchString(second) ||
		!regexp.MustCompile(`(?m)^engine-sync opencode: deleted `+regexp.QuoteMeta(deleted)+`$`).MatchString(second) {
		t.Fatalf("second build stdout=%q", second)
	}
}

func invokeOpenCodeCLI(args []string, resolve func() (string, error), home string, out, errout io.Writer) int {
	if len(args) > 0 && (args[0] == "build" || args[0] == "check" || args[0] == "doctor") {
		hasRoot := false
		for i := 1; i < len(args); i++ {
			if args[i] == "--home" {
				i++
				continue
			}
			if !strings.HasPrefix(args[i], "-") {
				hasRoot = true
			}
		}
		if !hasRoot {
			root, err := resolve()
			if err != nil {
				if containsFlag(args, "--json") {
					return diag.WriteJSON(out, errout, diag.Report{}, err)
				}
				fmt.Fprintln(errout, err)
				return 1
			}
			args = append(args, root)
		}
		if !containsFlag(args, "--home") {
			args = append(args, "--home", home)
		}
	}
	return runOpenCode(args, out, errout, env{get: paths.Getenv})
}

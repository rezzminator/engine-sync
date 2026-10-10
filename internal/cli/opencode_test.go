package cli

import (
	"bytes"
	"github.com/rezzminator/engine-sync/internal/testjail"
	"os"
	"path/filepath"
	"regexp"
	"testing"
)

func TestRunCommandRejectsUnknownAction(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if code := RunOpenCodeCommand(
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
		if code := RunOpenCodeCommand(
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

package paths

import (
	"os"
	"os/exec"
)

func Git(args ...string) ([]byte, error) { return exec.Command("git", args...).Output() }

var Getenv = os.Getenv

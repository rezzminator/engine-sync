package machine

import (
	"fmt"
	"github.com/rezzminator/engine-sync/internal/paths"
	"path/filepath"
)

type Mode uint8

const (
	ModeBuild Mode = iota
	ModeCheck
	actionWrite = "write"
	actionLink  = "link"
)

func resolveMachineHome(home string) (string, error) {
	if home == "" {
		var e error
		home, e = paths.Home()
		if e != nil {
			return "", e
		}
	}
	p, e := filepath.Abs(home)
	if e != nil {
		return "", fmt.Errorf("resolve home: %w", e)
	}
	return filepath.Clean(p), nil
}

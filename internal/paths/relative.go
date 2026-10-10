package paths

import (
	"fmt"
	"path/filepath"
	"strings"
)

func RelativePath(value, key string) error {
	clean := filepath.Clean(filepath.FromSlash(value))
	if strings.TrimSpace(value) == "" || filepath.IsAbs(clean) || clean == "." || clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return fmt.Errorf("%s %q must be relative and stay inside its root", key, value)
	}
	return nil
}

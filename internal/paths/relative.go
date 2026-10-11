package paths

import (
	"fmt"
	"golang.org/x/text/cases"
	"golang.org/x/text/unicode/norm"
	"path/filepath"
	"strings"
)

func RelativePath(value, key string) error {
	clean := filepath.Clean(filepath.FromSlash(value))
	if strings.TrimSpace(value) != value || strings.HasPrefix(value, "~") || filepath.ToSlash(clean) != value || strings.TrimSpace(value) == "" || filepath.IsAbs(clean) || clean == "." || clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return fmt.Errorf("%s %q must be relative and stay inside its root", key, value)
	}
	return nil
}

// FoldName compares filesystem names independent of case and Unicode form.
func FoldName(name string) string { return cases.Fold().String(norm.NFC.String(name)) }

func ValidateName(name string) error {
	if strings.TrimSpace(name) != name || name == "" || strings.HasPrefix(name, ".") || strings.ContainsAny(name, "/\\\x00\r\n") || name == ".." {
		return fmt.Errorf("invalid output name %q", name)
	}
	return nil
}

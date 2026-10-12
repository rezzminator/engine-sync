package source

import (
	"path/filepath"
	"regexp"
	"strings"
)

var errorCode = regexp.MustCompile(`^[A-Z]{3,}(-[A-Z]+)+ `)

// variantProblem names one variant whose render failed: an error that carries
// its own code as it is, any other as VARIANT-INVALID.
func variantProblem(err error) string {
	if errorCode.MatchString(err.Error()) {
		return err.Error()
	}
	return "VARIANT-INVALID " + err.Error()
}

// failedSourceNames is every name a source whose header does not parse may be
// installed under: its file name, and the `name:` line when one reads.
func failedSourceNames(path string, raw []byte) []string {
	names := []string{strings.TrimSuffix(filepath.Base(path), ".md")}
	header, _, found, err := Split(string(raw))
	if err != nil || !found {
		return names
	}
	for _, line := range strings.Split(header, "\n") {
		if m := ScalarField(line); m != nil && m[1] == "name" {
			if name := strings.Trim(strings.TrimSpace(m[2]), `"'`); name != "" && name != names[0] {
				names = append(names, name)
			}
		}
	}
	return names
}

package source

import (
	"fmt"
	"os"
)

// LinkTarget is the target text of the unresolvable source link at path. An
// unreadable link names the read failure, never an empty string that would
// read as "no link".
func LinkTarget(path string) string {
	target, err := os.Readlink(path)
	if err != nil {
		return fmt.Sprintf("unreadable link (%v)", err)
	}
	return target
}

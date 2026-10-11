package paths

import (
	"testing"
)

func TestRelativePathsStayInsideRoot(t *testing.T) {
	for _, value := range []string{"", "..", "../outside", "/absolute", "~/x", "skip/", " x", "x/../wb.json"} {
		if RelativePath(value, "mirrorFiles") == nil {
			t.Fatal(value)
		}
	}
	if err := RelativePath("nested/FILE.md", "mirrorFiles"); err != nil {
		t.Fatal(err)
	}
}

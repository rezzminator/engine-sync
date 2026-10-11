package rewrite

import (
	"testing"
)

func TestTextRewritesRespectModelAndCommandBoundaries(t *testing.T) {
	if got := ModelsIn("tiny tiny-extra", map[string]string{"tiny": "chosen"}); got != "chosen tiny-extra" {
		t.Fatal(got)
	}
	if got := CommandsIn("/dev run /dev/tty", map[string]string{"dev": "flat"}, func(s string) string { return "$" + s }); got != "$flat run /dev/tty" {
		t.Fatal(got)
	}
}

func TestReviewCommandReferencesPreserveFilenames(t *testing.T) {
	input := "{clone}/inspect.config.json /inspect. /inspect/file /inspect run"
	want := "{clone}/inspect.config.json $inspect. /inspect/file $inspect run"
	if got := CommandsIn(input, map[string]string{"inspect": "$inspect"}, nil); got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}

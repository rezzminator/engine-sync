package source

import "testing"

func TestReviewFrontmatterForms(t *testing.T) {
	for _, text := range []string{"---\r\ndescription: Reader\r\ntools: [Read, Write]\r\n---\r\nbody", "\ufeff--- \ndescription: Reader\ntools:\n  - 'Read'\n  - \"Write\"\n--- \nbody"} {
		f, body, e := Parse(text)
		if e != nil || f.Description != "Reader" || len(f.Tools) != 2 || f.Tools[1] != "Write" || body != "body" {
			t.Fatalf("frontmatter %+v body %q error %v", f, body, e)
		}
	}
}

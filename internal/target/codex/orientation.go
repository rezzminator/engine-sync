package codex

import (
	"fmt"
	"github.com/rezzminator/engine-sync/internal/artifact"
)

func generatedHeader(source string) string {
	return artifact.Marker(artifact.Markdown, source)
}

func generatedLine(source string) string {
	return artifact.Line(source)
}

func compileSourceMarkdown(source string, options TransformOptions, problem func(string)) (string, bool) {
	raw, err := options.Source.Read(source)
	if err != nil {
		problem(fmt.Sprintf("read %s: %v", source, err))
		return "", false
	}
	options.Kind = "orientation"
	options.Path = source
	return transformMarkdown(string(raw), options), true
}

package source

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// GlobalAgentVariantsFile is the optional declaration beside the global agent
// sources: variant name -> {"from": source agent, <frontmatter key>: value,
// "replace": {body text: new text}}.
// Absent means "this clone declares no variants" and is never an error.
const GlobalAgentVariantsFile = "variants.json"

// globalAgentVariantFrom and globalAgentVariantReplace are the reserved keys
// of a variant declaration; every other key is a frontmatter override.
const (
	globalAgentVariantFrom    = "from"
	globalAgentVariantReplace = "replace"
)

var (
	globalAgentVariantName = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`)
	// A value made only of these needs no YAML quoting; anything else is
	// single-quoted, the one YAML style with a single escape rule.
)

// GlobalAgentVariant is one Claude agent rendered from another: the source
// file with `name:` and the declared frontmatter keys overridden, the body
// byte-identical but for the declared "replace" swaps. Path is where the
// rendered file belongs in the engine-sync-owned generated directory; nothing is
// written by loading it.
type GlobalAgentVariant struct {
	Name    string
	From    string
	Path    string
	Content []byte
}

// LoadGlobalAgentVariants renders every variant agentsDir/variants.json
// declares, sorted by name. A missing file is (nil, nil). Every malformed
// declaration is an error naming the variant: an unparseable file, a name
// that is not kebab-case, a missing or unknown "from", a name colliding with
// an original agent, a non-string value, or an override key the source
// frontmatter does not carry (an override never ADDS a key, so a typo cannot
// ship as a silently ignored field), or a "replace" that is not an object of
// strings or whose text does not occur exactly once in the source body.
func LoadGlobalAgentVariants(agentsDir, outputDir string, registry ...string) ([]GlobalAgentVariant, error) {
	declPath := filepath.Join(agentsDir, GlobalAgentVariantsFile)
	if len(registry) > 0 && registry[0] != "" {
		declPath = registry[0]
		if !filepath.IsAbs(declPath) {
			declPath = filepath.Join(agentsDir, declPath)
		}
	}
	raw, err := os.ReadFile(declPath)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", declPath, err)
	}
	var decl map[string]map[string]any
	if err := json.Unmarshal(raw, &decl); err != nil {
		return nil, fmt.Errorf("parse %s: %w", declPath, err)
	}
	names := make([]string, 0, len(decl))
	for name := range decl {
		names = append(names, name)
	}
	sort.Strings(names)

	variants := make([]GlobalAgentVariant, 0, len(names))
	for _, name := range names {
		if !globalAgentVariantName.MatchString(name) {
			return nil, fmt.Errorf("%s: variant %q: name is not kebab-case", declPath, name)
		}
		overrides := make(map[string]string, len(decl[name]))
		var swaps map[string]string
		for key, value := range decl[name] {
			if key == globalAgentVariantReplace {
				parsed, err := globalAgentVariantSwaps(value)
				if err != nil {
					return nil, fmt.Errorf("%s: variant %q: %w", declPath, name, err)
				}
				swaps = parsed
				continue
			}
			text, ok := value.(string)
			if !ok {
				return nil, fmt.Errorf("%s: variant %q: %q must be a string, got %T", declPath, name, key, value)
			}
			if strings.ContainsAny(text, "\r\n") {
				return nil, fmt.Errorf("%s: variant %q: %q must be a single line", declPath, name, key)
			}
			overrides[key] = text
		}
		from := overrides[globalAgentVariantFrom]
		delete(overrides, globalAgentVariantFrom)
		if !globalAgentVariantName.MatchString(from) {
			return nil, fmt.Errorf("%s: variant %q: \"from\" must name a source agent, got %q", declPath, name, from)
		}
		if _, exists := decl[from]; exists {
			return nil, fmt.Errorf("%s: variant %q: \"from\" names %q, itself a variant", declPath, name, from)
		}
		if _, err := os.Lstat(filepath.Join(agentsDir, name+".md")); err == nil {
			return nil, fmt.Errorf("%s: variant %q collides with the original agent %s.md", declPath, name, name)
		} else if !errors.Is(err, fs.ErrNotExist) {
			return nil, fmt.Errorf("inspect %s: %w", filepath.Join(agentsDir, name+".md"), err)
		}
		if _, reserved := overrides["name"]; reserved {
			return nil, fmt.Errorf(
				"%s: variant %q: \"name\" is the variant's own key, never an override",
				declPath,
				name,
			)
		}
		sourcePath := filepath.Join(agentsDir, from+".md")
		source, err := os.ReadFile(sourcePath)
		if err != nil {
			return nil, fmt.Errorf("%s: variant %q: read source agent: %w", declPath, name, err)
		}
		content, err := RenderVariant(string(source), name, "templates/global/agents/"+from+".md + "+GlobalAgentVariantsFile, overrides, swaps)
		if err != nil {
			return nil, fmt.Errorf("%s: variant %q from %s: %w", declPath, name, sourcePath, err)
		}
		variants = append(variants, GlobalAgentVariant{
			Name:    name,
			From:    from,
			Path:    filepath.Join(outputDir, name+".md"),
			Content: []byte(content),
		})
	}
	return variants, nil
}

// globalAgentVariantSwaps reads a "replace" value: an object whose keys are
// non-empty body text and whose values are the text that takes their place.
func globalAgentVariantSwaps(value any) (map[string]string, error) {
	object, ok := value.(map[string]any)
	if !ok {
		return nil, fmt.Errorf(
			"%q must be an object of body text to new text, got %T",
			globalAgentVariantReplace,
			value,
		)
	}
	swaps := make(map[string]string, len(object))
	for text, with := range object {
		if text == "" {
			return nil, fmt.Errorf("%q carries an empty body text", globalAgentVariantReplace)
		}
		replacement, ok := with.(string)
		if !ok {
			return nil, fmt.Errorf("%q entry %q must be a string, got %T", globalAgentVariantReplace, text, with)
		}
		swaps[text] = replacement
	}
	return swaps, nil
}

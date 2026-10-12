package source

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"github.com/rezzminator/engine-sync/internal/artifact"
	"github.com/rezzminator/engine-sync/internal/paths"
	"go.yaml.in/yaml/v3"
)

func (d *variantsDecl) content(name string, r *renderedVariant) ([]byte, error) {
	source := filepath.ToSlash(filepath.Join(d.markerDir, filepath.Base(d.path)))
	if r.root != GeneralPurpose {
		source = filepath.ToSlash(filepath.Join(d.markerDir, r.root+".md")) + " + " + filepath.Base(d.path)
	}
	var head bytes.Buffer
	enc := yaml.NewEncoder(&head)
	enc.SetIndent(2)
	if err := enc.Encode(r.front); err != nil {
		return nil, fmt.Errorf("render frontmatter: %w", err)
	}
	if err := enc.Close(); err != nil {
		return nil, fmt.Errorf("render frontmatter: %w", err)
	}
	return []byte("---\n" + artifact.Marker(artifact.YAMLFront, source) + "\n" + head.String() + "---\n" + r.body), nil
}

// render resolves one variant: its from (original, variant, or the built-in),
// then the profile chain's layers outermost first, then its own layer.
func (d *variantsDecl) render(name string, chain []string) (*renderedVariant, error) {
	if r, ok := d.rendered[name]; ok {
		return r, nil
	}
	if slices.Contains(chain, name) {
		return nil, fmt.Errorf("%s: \"from\" cycle %s", d.path, strings.Join(append(chain, name), " → "))
	}
	chain = append(chain, name)
	layer := d.variants[name]
	var base renderedVariant
	switch {
	case layer.from == GeneralPurpose:
		base = renderedVariant{front: &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}, root: GeneralPurpose}
	case d.variants[layer.from].hasFrom:
		parent, err := d.render(layer.from, chain)
		if err != nil {
			return nil, err
		}
		base = renderedVariant{front: cloneNode(parent.front), body: parent.body, root: parent.root}
	default:
		if !globalAgentVariantName.MatchString(layer.from) {
			return nil, fmt.Errorf("%s: variant %q: \"from\" must name a source agent, a variant or %s, got %q", d.path, name, GeneralPurpose, layer.from)
		}
		sourcePath := filepath.Join(d.agentsDir, layer.from+".md")
		raw, err := paths.ReadRegular(sourcePath)
		if err != nil {
			return nil, fmt.Errorf("%s: variant %q: read source agent: %w", d.path, name, err)
		}
		header, body, found, err := Split(string(raw))
		if err != nil {
			return nil, fmt.Errorf("%s: variant %q: %s", d.path, name, DescribeParseError(sourcePath, err))
		}
		front := &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
		if found {
			if front, err = ParseHeader(header); err != nil {
				return nil, fmt.Errorf("%s: variant %q: %s", d.path, name, DescribeParseError(sourcePath, err))
			}
		}
		base = renderedVariant{front: front, body: body, root: layer.from}
	}
	layers, err := d.profileLayers(layer.profile, nil)
	if err != nil {
		return nil, fmt.Errorf("%s: variant %q: %w", d.path, name, err)
	}
	layers = append(layers, layer)

	front := base.front
	for _, l := range layers {
		for _, path := range l.unset {
			if !unsetPath(front, strings.Split(path, ".")) {
				return nil, fmt.Errorf("UNSET-MISSING %s: %s.md has no frontmatter key %q", name, layer.from, path)
			}
		}
		if l.set != nil {
			setMerge(front, cloneNode(l.set))
		}
		if l.add != nil {
			if err := addMerge(front, cloneNode(l.add), ""); err != nil {
				return nil, fmt.Errorf("%s: variant %q: %w", d.path, name, err)
			}
		}
	}
	normalizeTools(front)
	stampIdentity(front, name, base.root)

	var sections []sectionOverride
	var renames []renamePair
	var prepends, appends []string
	for _, l := range layers {
		for _, s := range l.sections {
			if i := slices.IndexFunc(sections, func(o sectionOverride) bool { return o.key == s.key }); i >= 0 {
				sections[i] = s
			} else {
				sections = append(sections, s)
			}
		}
		for _, r := range l.rename {
			if i := slices.IndexFunc(renames, func(o renamePair) bool { return o.from == r.from }); i >= 0 {
				renames[i] = r
			} else {
				renames = append(renames, r)
			}
		}
		if l.prepend != nil {
			prepends = append(prepends, *l.prepend)
		}
		if l.appendix != nil {
			appends = append(appends, *l.appendix)
		}
	}
	body := base.body
	for _, s := range sections {
		now, err := SectionPin(base.body, s.key)
		if err != nil {
			return nil, sectionError(err, name, layer.from, s.key)
		}
		d.uses = append(d.uses, pinFound{variant: name, section: s, now: now})
		if d.repin != nil && d.repin(name) {
			d.pins = append(d.pins, pinFound{variant: name, section: s, now: now})
		} else if !s.pinned {
			return nil, fmt.Errorf("SECTION-UNPINNED %s: %s.md § %s has no pin — run engine-sync variants pin %s", name, layer.from, s.key, name)
		} else if s.pin != now {
			return nil, fmt.Errorf("SECTION-DRIFT %s: %s.md § %s changed since pinned (pin %s, now %s) — review the override, then run engine-sync variants pin %s",
				name, layer.from, s.key, s.pin, now, name)
		}
		if body, err = ReplaceSection(body, s.key, s.text, s.heading); err != nil {
			return nil, sectionError(err, name, layer.from, s.key)
		}
	}
	body, counts := RenameAgents(body, renames)
	for i, r := range renames {
		if counts[i] == 0 {
			return nil, fmt.Errorf("RENAME-UNUSED %s: %s matched 0 times", name, r.from)
		}
	}
	if len(prepends) > 0 {
		body = joinNonEmpty(strings.Join(prepends, "\n\n"), body)
	}
	if len(appends) > 0 {
		body = joinNonEmpty(body, strings.Join(appends, "\n\n"))
	}
	r := &renderedVariant{front: front, body: body, root: base.root}
	d.rendered[name] = r
	return r, nil
}

func (d *variantsDecl) profileLayers(name string, chain []string) ([]variantLayer, error) {
	if name == "" {
		return nil, nil
	}
	if slices.Contains(chain, name) {
		return nil, fmt.Errorf("profile cycle %s", strings.Join(append(chain, name), " → "))
	}
	profile, ok := d.profiles[name]
	if !ok {
		return nil, fmt.Errorf("unknown profile %q", name)
	}
	outer, err := d.profileLayers(profile.profile, append(chain, name))
	if err != nil {
		return nil, err
	}
	return append(outer, profile), nil
}

func joinNonEmpty(a, b string) string {
	switch {
	case a == "":
		return b
	case b == "":
		return a
	}
	return a + "\n\n" + b
}

var (
	errSectionMissing   = errors.New("section missing")
	errSectionAmbiguous = errors.New("section ambiguous")
)

func sectionError(err error, variant, from, key string) error {
	switch {
	case errors.Is(err, errSectionMissing):
		return fmt.Errorf("SECTION-MISSING %s: %s.md has no section %q", variant, from, key)
	case errors.Is(err, errSectionAmbiguous):
		return fmt.Errorf("SECTION-AMBIGUOUS %s: %s.md has more than one section %q", variant, from, key)
	}
	return err
}

var markdownHeading = regexp.MustCompile(`^(#{1,6}) (.*)$`)

type bodyHeading struct {
	line, level int
	text        string
}

// bodyHeadings lists the ATX headings outside fenced code blocks.
func bodyHeadings(lines []string) []bodyHeading {
	var out []bodyHeading
	fence := false
	for i, line := range lines {
		if strings.HasPrefix(strings.TrimLeft(line, " \t"), "```") {
			fence = !fence
			continue
		}
		if m := markdownHeading.FindStringSubmatch(line); m != nil && !fence {
			out = append(out, bodyHeading{line: i, level: len(m[1]), text: m[2]})
		}
	}
	return out
}

// sectionExtent is the heading line (-1 for the intro), the extent's first
// and past-the-end lines, and the heading level.
func sectionExtent(lines []string, key string) (heading, start, end, level int, err error) {
	hs := bodyHeadings(lines)
	if key == IntroSection {
		end = len(lines)
		if len(hs) > 0 {
			end = hs[0].line
		}
		return -1, 0, end, 0, nil
	}
	found := -1
	for i, h := range hs {
		if h.text == key {
			if found >= 0 {
				return 0, 0, 0, 0, errSectionAmbiguous
			}
			found = i
		}
	}
	if found < 0 {
		return 0, 0, 0, 0, errSectionMissing
	}
	h := hs[found]
	end = len(lines)
	for _, next := range hs[found+1:] {
		if next.level <= h.level {
			end = next.line
			break
		}
	}
	return h.line, h.line + 1, end, h.level, nil
}

func trimBlankLines(lines []string) []string {
	for len(lines) > 0 && strings.TrimSpace(lines[0]) == "" {
		lines = lines[1:]
	}
	for len(lines) > 0 && strings.TrimSpace(lines[len(lines)-1]) == "" {
		lines = lines[:len(lines)-1]
	}
	return lines
}

// SectionPin is `sha256:` and the hex SHA-256 of a section's extent, leading
// and trailing blank lines removed, lines joined with "\n".
func SectionPin(body, key string) (string, error) {
	lines := strings.Split(body, "\n")
	_, start, end, _, err := sectionExtent(lines, key)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256([]byte(strings.Join(trimBlankLines(lines[start:end]), "\n")))
	return "sha256:" + hex.EncodeToString(sum[:]), nil
}

// ReplaceSection writes the heading (or the new heading at the same level),
// one blank line, the text with trailing whitespace trimmed, and one blank
// line before the next heading. The intro keeps the body's leading blank
// lines; a body's final newline survives a replaced last section.
func ReplaceSection(body, key, text, heading string) (string, error) {
	lines := strings.Split(body, "\n")
	hi, start, end, level, err := sectionExtent(lines, key)
	if err != nil {
		return "", err
	}
	var before, out []string
	if hi >= 0 {
		before = lines[:hi]
		if heading == "" {
			heading = key
		}
		out = append(out, strings.Repeat("#", level)+" "+heading, "")
	} else {
		for i := start; i < end && strings.TrimSpace(lines[i]) == ""; i++ {
			out = append(out, lines[i])
		}
	}
	out = append(out, strings.Split(strings.TrimRight(text, " \t\r\n"), "\n")...)
	if end < len(lines) {
		out = append(out, "")
	} else if strings.HasSuffix(body, "\n") {
		out = append(out, "")
	}
	return strings.Join(slices.Concat(before, out, lines[end:]), "\n"), nil
}

func renameBefore(c byte) bool {
	return c >= 'A' && c <= 'Z' || c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || strings.IndexByte("_./:-", c) >= 0
}

func renameAfter(c byte) bool {
	return c >= 'A' && c <= 'Z' || c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || strings.IndexByte("_/-", c) >= 0
}

func alnum(c byte) bool {
	return c >= 'A' && c <= 'Z' || c >= 'a' && c <= 'z' || c >= '0' && c <= '9'
}

// RenameAgents replaces, in one pass over text, every standalone occurrence
// of each pair's from with its to: the byte before is outside
// [A-Za-z0-9_./:-], the byte after outside [A-Za-z0-9_/-], and it is not
// followed by "." plus an alphanumeric (a file name such as name.md). The
// replacements are simultaneous — a replacement is never matched again — so
// {a: b, b: c} turns a into b and b into c. It returns the text and each
// pair's number of occurrences in the original text.
func RenameAgents(text string, renames []renamePair) (string, []int) {
	counts := make([]int, len(renames))
	var out strings.Builder
	last := 0
	for at := 0; at < len(text); at++ {
		if at > 0 && renameBefore(text[at-1]) {
			continue
		}
		for i, r := range renames {
			after := at + len(r.from)
			if r.from == "" || !strings.HasPrefix(text[at:], r.from) ||
				after < len(text) && renameAfter(text[after]) ||
				after+1 < len(text) && text[after] == '.' && alnum(text[after+1]) {
				continue
			}
			out.WriteString(text[last:at])
			out.WriteString(r.to)
			counts[i]++
			last, at = after, after-1
			break
		}
	}
	out.WriteString(text[last:])
	return out.String(), counts
}

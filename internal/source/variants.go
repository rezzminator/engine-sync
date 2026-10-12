package source

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"

	"github.com/rezzminator/engine-sync/internal/atomicfile"
	"github.com/rezzminator/engine-sync/internal/paths"
	"go.yaml.in/yaml/v3"
)

// GlobalAgentVariantsFile is the optional declaration beside the global agent
// sources (version 2, docs/interface.md § Agent variants). Absent means "this
// clone declares no variants" and is never an error.
const GlobalAgentVariantsFile = "variants.json"

// GeneralPurpose is the Claude Code built-in a variant may start from: an
// empty frontmatter and an empty body.
const GeneralPurpose = "general-purpose"

// IntroSection is the section key of the text before the first heading.
const IntroSection = "(intro)"

var globalAgentVariantName = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`)

// GlobalAgentVariant is one Claude agent rendered from a variants.json
// declaration. Path is where the rendered file belongs in the engine-sync-owned
// generated directory; nothing is written by loading it. From is the agent it
// is declared from, Root the original at the end of its from chain
// (GeneralPurpose when the chain ends at the built-in).
type GlobalAgentVariant struct {
	Name    string
	From    string
	Root    string
	Path    string
	Content []byte
}

// PinChange is one section pin `variants pin` rewrote.
type PinChange struct {
	Variant, Heading, Pin string
}

// variantLayer is one profile's or variant's declaration, fragments resolved.
type variantLayer struct {
	from     string
	hasFrom  bool
	profile  string
	set, add *yaml.Node
	unset    []string
	sections []sectionOverride
	prepend  *string
	appendix *string
	rename   []renamePair
	at       []string // JSON path of the layer, for pin splicing
}

type sectionOverride struct {
	key, pin, text, heading string
	pinned                  bool
	at                      []string
}

type renamePair struct{ from, to string }

type variantsDecl struct {
	path, agentsDir, markerDir, outputDir string
	raw                                   []byte
	profiles                              map[string]variantLayer
	variants                              map[string]variantLayer
	rendered                              map[string]*renderedVariant
	repin                                 func(variant string) bool
	pins                                  []pinFound
	uses                                  []pinFound // every section override rendered, pinned or not
}

type renderedVariant struct {
	front *yaml.Node
	body  string
	root  string
}

type pinFound struct {
	variant string
	section sectionOverride
	now     string
}

// variantsPath resolves the declaration: agentsDir/variants.json, or the
// configured machine.variants path (relative to agentsDir).
func variantsPath(agentsDir string, registry []string) (string, bool) {
	if len(registry) > 0 && registry[0] != "" {
		path := registry[0]
		if !filepath.IsAbs(path) {
			path = filepath.Join(agentsDir, path)
		}
		return path, true
	}
	return filepath.Join(agentsDir, GlobalAgentVariantsFile), false
}

// VariantsDeclaration is the declaration LoadMachine reads for agentsDir: the
// configured machine.variants (relative to agentsDir) when set, else
// agentsDir/variants.json.
func VariantsDeclaration(agentsDir, configured string) string {
	path, _ := variantsPath(agentsDir, []string{configured})
	return path
}

// LoadGlobalAgentVariants renders every variant the declaration names, sorted
// by name. A missing default declaration is (nil, nil); a configured one
// (registry[0]) must exist. registry[1] names the marker's directory. Every
// malformed declaration and every failed render is an error.
func LoadGlobalAgentVariants(agentsDir, outputDir string, registry ...string) ([]GlobalAgentVariant, error) {
	decl, err := readVariantsDecl(agentsDir, outputDir, registry)
	if err != nil || decl == nil {
		return nil, err
	}
	return decl.renderAll()
}

// PinGlobalAgentVariants recomputes the section pins of the named variants
// (every variant when names is empty) and rewrites the declaration, changing
// only the pin literals. It returns each pin it changed.
func PinGlobalAgentVariants(agentsDir string, names []string, registry ...string) ([]PinChange, error) {
	decl, err := readVariantsDecl(agentsDir, agentsDir, registry)
	if err != nil {
		return nil, err
	}
	if decl == nil {
		path, _ := variantsPath(agentsDir, registry)
		return nil, fmt.Errorf("read %s: %w", path, fs.ErrNotExist)
	}
	for _, name := range names {
		if _, ok := decl.variants[name]; !ok {
			return nil, fmt.Errorf("%s: no variant %q", decl.path, name)
		}
	}
	decl.repin = func(variant string) bool { return len(names) == 0 || slices.Contains(names, variant) }
	// Only a named variant's failure stops the pin: another variant's own
	// drift is its own pin's to resolve, never a block on this one.
	if _, failed := decl.renderEach(); len(failed) > 0 {
		for _, name := range slices.Sorted(maps.Keys(failed)) {
			if decl.repin(name) {
				return nil, failed[name]
			}
		}
	}
	type splice struct {
		at   []string
		pin  string
		seen PinChange
	}
	byPath := map[string]splice{}
	var order []string
	for _, found := range decl.pins {
		if found.section.pinned && found.section.pin == found.now {
			continue
		}
		key := strings.Join(found.section.at, "\x00")
		if prior, ok := byPath[key]; ok {
			if prior.pin != found.now {
				return nil, fmt.Errorf("%s: section %q is declared once (%s) but pins differently for %s and %s — declare it per variant",
					decl.path, found.section.key, strings.Join(found.section.at[:2], " "), prior.seen.Variant, found.variant)
			}
			continue
		}
		byPath[key] = splice{at: found.section.at, pin: found.now, seen: PinChange{Variant: found.variant, Heading: found.section.key, Pin: found.now}}
		order = append(order, key)
	}
	if len(order) == 0 {
		return nil, nil
	}
	if err := decl.refuseSharedDrift(byPathPins(order, func(key string) string { return byPath[key].pin })); err != nil {
		return nil, err
	}
	raw := decl.raw
	offsets, err := jsonOffsets(raw)
	if err != nil {
		return nil, fmt.Errorf("parse %s: %w", decl.path, err)
	}
	type edit struct {
		start, end int
		text       string
	}
	var edits []edit
	changes := make([]PinChange, 0, len(order))
	for _, key := range order {
		s := byPath[key]
		pinLiteral, _ := json.Marshal(s.pin)
		if span, ok := offsets[strings.Join(append(slices.Clone(s.at), "pin"), "\x00")]; ok {
			edits = append(edits, edit{span[0], span[1], string(pinLiteral)})
		} else {
			span, ok := offsets[key]
			if !ok {
				return nil, fmt.Errorf("%s: cannot locate section %q to pin", decl.path, s.seen.Heading)
			}
			at := span[0] + 1 // just past the object's '{'
			insert := `"pin": ` + string(pinLiteral) + `, `
			if rest := raw[at:]; len(rest) > 0 && rest[0] == '\n' {
				line := rest[1:]
				indent := line[:len(line)-len(bytes.TrimLeft(line, " \t"))]
				insert = "\n" + string(indent) + `"pin": ` + string(pinLiteral) + ","
			}
			edits = append(edits, edit{at, at, insert})
		}
		changes = append(changes, s.seen)
	}
	sort.Slice(edits, func(i, j int) bool { return edits[i].start > edits[j].start })
	out := slices.Clone(raw)
	for _, e := range edits {
		out = slices.Concat(out[:e.start], []byte(e.text), out[e.end:])
	}
	// A symlinked declaration is written through to its target, never
	// replaced by a regular file.
	target, err := filepath.EvalSymlinks(decl.path)
	if err != nil {
		return nil, fmt.Errorf("resolve %s: %w", decl.path, err)
	}
	info, err := os.Stat(target)
	if err != nil {
		return nil, fmt.Errorf("stat %s: %w", target, err)
	}
	// A declaration another pin rewrote since this one read it is never
	// overwritten with a stale copy.
	if current, err := paths.ReadRegular(target); err != nil || !bytes.Equal(current, raw) {
		return nil, fmt.Errorf("%s changed while pinning (%v) — run variants pin again", decl.path, err)
	}
	if err := atomicfile.Write(target, out, info.Mode().Perm()); err != nil {
		return nil, err // atomicfile names the path
	}
	return changes, nil
}

func readVariantsDecl(agentsDir, outputDir string, registry []string) (*variantsDecl, error) {
	declPath, configured := variantsPath(agentsDir, registry)
	raw, err := paths.ReadRegular(declPath)
	if errors.Is(err, fs.ErrNotExist) && !configured {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", declPath, err)
	}
	markerDir := filepath.Base(agentsDir)
	if len(registry) > 1 && registry[1] != "" {
		markerDir = registry[1]
	}
	decl := &variantsDecl{path: declPath, agentsDir: agentsDir, markerDir: markerDir, outputDir: outputDir, raw: raw,
		profiles: map[string]variantLayer{}, variants: map[string]variantLayer{}, rendered: map[string]*renderedVariant{}}
	var top map[string]json.RawMessage
	if err := json.Unmarshal(raw, &top); err != nil {
		return nil, fmt.Errorf("parse %s: %w", declPath, err)
	}
	var version int
	if v, ok := top["version"]; ok {
		if err := json.Unmarshal(v, &version); err != nil {
			return nil, fmt.Errorf("%s: \"version\" must be a number: %w", declPath, err)
		}
	}
	switch version {
	case 2:
	case 0, 1:
		return nil, errors.New("variants.json: version 1 is retired — rewrite as version 2 (see docs/interface.md)")
	default:
		return nil, fmt.Errorf("%s: version %d is not supported (want 2)", declPath, version)
	}
	var shape struct {
		Version   int                        `json:"version"`
		Fragments map[string]string          `json:"fragments"`
		Profiles  map[string]json.RawMessage `json:"profiles"`
		Variants  map[string]json.RawMessage `json:"variants"`
	}
	if err := strictJSON(raw, &shape); err != nil {
		return nil, fmt.Errorf("parse %s: %w", declPath, err)
	}
	fragment := func(owner, field, value string) (string, error) {
		name, ok := strings.CutPrefix(value, "@")
		if !ok || strings.ContainsAny(name, " \t\r\n") {
			return value, nil
		}
		text, ok := shape.Fragments[name]
		if !ok {
			return "", fmt.Errorf("%s: %s: %s names unknown fragment %q", declPath, owner, field, value)
		}
		return text, nil
	}
	for kind, group := range map[string]map[string]json.RawMessage{"profiles": shape.Profiles, "variants": shape.Variants} {
		for name, body := range group {
			owner := fmt.Sprintf("%s %q", strings.TrimSuffix(kind, "s"), name)
			if !globalAgentVariantName.MatchString(name) {
				return nil, fmt.Errorf("%s: %s: name is not kebab-case", declPath, owner)
			}
			layer, err := parseVariantLayer(body, owner, []string{kind, name}, fragment)
			if err != nil {
				return nil, fmt.Errorf("%s: %w", declPath, err)
			}
			if kind == "profiles" {
				if layer.hasFrom {
					return nil, fmt.Errorf("%s: %s: \"from\" belongs to a variant, never a profile", declPath, owner)
				}
				decl.profiles[name] = layer
				continue
			}
			if !layer.hasFrom || layer.from == "" {
				return nil, fmt.Errorf("%s: %s: \"from\" must name a source agent, a variant or %s", declPath, owner, GeneralPurpose)
			}
			if name == GeneralPurpose {
				return nil, fmt.Errorf("%s: %s: %s is the Claude Code built-in, never a variant", declPath, owner, GeneralPurpose)
			}
			original := filepath.Join(agentsDir, name+".md")
			if _, err := os.Lstat(original); err == nil {
				return nil, fmt.Errorf("%s: %s collides with the original agent %s.md", declPath, owner, name)
			} else if !errors.Is(err, fs.ErrNotExist) {
				return nil, fmt.Errorf("inspect %s: %w", original, err)
			}
			decl.variants[name] = layer
		}
	}
	return decl, nil
}

func strictJSON(raw []byte, into any) error {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(into); err != nil {
		return err
	}
	if _, err := dec.Token(); err != io.EOF {
		return errors.New("trailing data after the top-level object")
	}
	return nil
}

func parseVariantLayer(raw json.RawMessage, owner string, at []string, fragment func(owner, field, value string) (string, error)) (variantLayer, error) {
	var shape struct {
		From     *string         `json:"from"`
		Profile  string          `json:"profile"`
		Set      json.RawMessage `json:"set"`
		Add      json.RawMessage `json:"add"`
		Unset    []string        `json:"unset"`
		Sections json.RawMessage `json:"sections"`
		Prepend  *string         `json:"prepend"`
		Append   *string         `json:"append"`
		Rename   json.RawMessage `json:"rename"`
	}
	if err := strictJSON(raw, &shape); err != nil {
		return variantLayer{}, fmt.Errorf("%s: %w", owner, err)
	}
	layer := variantLayer{profile: shape.Profile, unset: shape.Unset, at: at}
	if shape.From != nil {
		layer.hasFrom, layer.from = true, *shape.From
	}
	for field, src := range map[string]json.RawMessage{"set": shape.Set, "add": shape.Add} {
		if len(src) == 0 {
			continue
		}
		node, err := jsonNode(src)
		if err != nil || node.Kind != yaml.MappingNode {
			return variantLayer{}, fmt.Errorf("%s: %q must be an object", owner, field)
		}
		for i := 0; i+1 < len(node.Content); i += 2 {
			key, value := node.Content[i].Value, node.Content[i+1]
			if key == "name" || key == "role" {
				return variantLayer{}, fmt.Errorf("%s: %q can never be set — the variant's name and root decide it", owner, key)
			}
			if field == "add" && value.Kind != yaml.SequenceNode && value.Kind != yaml.MappingNode {
				return variantLayer{}, fmt.Errorf("%s: add.%s must be a list or a map", owner, key)
			}
		}
		if field == "set" {
			layer.set = node
		} else {
			layer.add = node
		}
	}
	for _, path := range layer.unset {
		if first, _, _ := strings.Cut(path, "."); path == "" || first == "name" || first == "role" {
			return variantLayer{}, fmt.Errorf("%s: unset %q: name a frontmatter key other than name and role", owner, path)
		}
	}
	for field, value := range map[string]*string{"prepend": shape.Prepend, "append": shape.Append} {
		if value == nil {
			continue
		}
		text, err := fragment(owner, field, *value)
		if err != nil {
			return variantLayer{}, err
		}
		if field == "prepend" {
			layer.prepend = &text
		} else {
			layer.appendix = &text
		}
	}
	if len(shape.Sections) > 0 {
		node, err := jsonNode(shape.Sections)
		if err != nil || node.Kind != yaml.MappingNode {
			return variantLayer{}, fmt.Errorf("%s: \"sections\" must be an object of heading to override", owner)
		}
		for i := 0; i+1 < len(node.Content); i += 2 {
			key, value := node.Content[i].Value, node.Content[i+1]
			raw, _ := NodeJSON(value)
			var o struct {
				Pin     *string `json:"pin"`
				Text    *string `json:"text"`
				Heading string  `json:"heading"`
			}
			if err := strictJSON(raw, &o); err != nil {
				return variantLayer{}, fmt.Errorf("%s: section %q: %w", owner, key, err)
			}
			if o.Text == nil {
				return variantLayer{}, fmt.Errorf("%s: section %q: \"text\" is required", owner, key)
			}
			if key == IntroSection && o.Heading != "" {
				return variantLayer{}, fmt.Errorf("%s: section %q has no heading to rename", owner, key)
			}
			text, err := fragment(owner, "section "+strconv.Quote(key), *o.Text)
			if err != nil {
				return variantLayer{}, err
			}
			s := sectionOverride{key: key, text: text, heading: o.Heading, at: append(slices.Clone(at), "sections", key)}
			if o.Pin != nil {
				s.pin, s.pinned = *o.Pin, true
			}
			layer.sections = append(layer.sections, s)
		}
	}
	if len(shape.Rename) > 0 {
		node, err := jsonNode(shape.Rename)
		if err != nil || node.Kind != yaml.MappingNode {
			return variantLayer{}, fmt.Errorf("%s: \"rename\" must be an object of agent name to new name", owner)
		}
		for i := 0; i+1 < len(node.Content); i += 2 {
			key, value := node.Content[i], node.Content[i+1]
			if key.Value == "" || value.Kind != yaml.ScalarNode || value.Tag != "!!str" {
				return variantLayer{}, fmt.Errorf("%s: rename %q must map a name to a string", owner, key.Value)
			}
			layer.rename = append(layer.rename, renamePair{key.Value, value.Value})
		}
	}
	return layer, nil
}

func (d *variantsDecl) renderAll() ([]GlobalAgentVariant, error) {
	out, failed := d.renderEach()
	if len(failed) > 0 {
		return nil, failed[slices.Min(slices.Collect(maps.Keys(failed)))]
	}
	return out, nil
}

// renderEach renders every variant, sorted by name; a variant whose render
// fails is left out and returned in failed under its name.
func (d *variantsDecl) renderEach() (out []GlobalAgentVariant, failed map[string]error) {
	names := slices.Sorted(maps.Keys(d.variants))
	failed = map[string]error{}
	for _, name := range names {
		r, err := d.render(name, nil)
		if err != nil {
			failed[name] = err
			continue
		}
		content, err := d.content(name, r)
		if err != nil {
			failed[name] = fmt.Errorf("%s: variant %q: %w", d.path, name, err)
			continue
		}
		out = append(out, GlobalAgentVariant{Name: name, From: d.variants[name].from, Root: r.root,
			Path: filepath.Join(d.outputDir, name+".md"), Content: content})
	}
	return out, failed
}

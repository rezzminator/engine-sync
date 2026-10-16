package source

import (
	"errors"
	"fmt"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"github.com/rezzminator/engine-sync/internal/paths"
	"go.yaml.in/yaml/v3"
)

// GeneralPurpose is the Claude Code built-in with empty frontmatter and body.
const GeneralPurpose = "general-purpose"

// IntroSection names text before the first heading.
const IntroSection = "(intro)"

var declarationName = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`)

// DeclarationKeys are machine source controls, never rendered agent settings.
var DeclarationKeys = []string{"from", "extends", "abstract", "add", "unset", "rename", "prepend", "append", "pins", "retitle"}

// GeneratedAgent is a rendered machine declaration, not written by loading.
type GeneratedAgent struct {
	Name, From, Root, Path string
	Content                []byte
}

// PinChange is one source section pin changed by variants pin.
type PinChange struct{ Declaration, Heading, Pin string }

type layer struct {
	from              string
	hasFrom           bool
	extends           []string
	set, add          *yaml.Node
	unset             []string
	sections          []sectionOverride
	prepend, appendix *string
	rename            []renamePair
	path              string
	raw               []byte
}

type sectionOverride struct {
	key, pin, text, heading string
	pinned                  bool
	owner                   string
}
type renamePair struct{ from, to string }
type declarationSet struct {
	path, agentsDir, markerDir, outputDir string
	declarations                          map[string]layer
	shared                                map[string]layer
	used                                  map[string]bool
	failures                              map[string]error
	rendered                              map[string]*renderedAgent
	repin                                 func(string) bool
	pins, uses                            []pinFound
}
type renderedAgent struct {
	front      *yaml.Node
	body, root string
}
type pinFound struct {
	declaration string
	section     sectionOverride
	now         string
}

// LoadDeclarations renders all source-file declarations in name order.
func LoadDeclarations(agentsDir, outputDir string, markerDir ...string) ([]GeneratedAgent, error) {
	d, err := readDeclarations(agentsDir, outputDir, markerDir)
	if err != nil {
		return nil, err
	}
	out, failed := d.renderEach()
	var errs []error
	for _, name := range slices.Sorted(maps.Keys(failed)) {
		errs = append(errs, failed[name])
	}
	return out, errors.Join(errs...)
}

func readDeclarations(agentsDir, outputDir string, marker []string) (*declarationSet, error) {
	d := &declarationSet{path: agentsDir, agentsDir: agentsDir, markerDir: filepath.Base(agentsDir), outputDir: outputDir,
		declarations: map[string]layer{}, shared: map[string]layer{}, used: map[string]bool{}, failures: map[string]error{}, rendered: map[string]*renderedAgent{}}
	if len(marker) > 0 && marker[0] != "" {
		d.markerDir = marker[0]
	}
	files, err := filepath.Glob(filepath.Join(agentsDir, "*.md"))
	if err != nil {
		return nil, fmt.Errorf("enumerate %s: %w", agentsDir, err)
	}
	for _, path := range files {
		if filepath.Base(path) == "README.md" {
			continue
		}
		raw, err := paths.ReadRegular(path)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return nil, fmt.Errorf("read %s: %w", path, err)
		}
		header, body, _, err := Split(string(raw))
		var front *yaml.Node
		if err == nil {
			front, err = ParseHeader(header)
		}
		name := strings.TrimSuffix(filepath.Base(path), ".md")
		if err != nil {
			// Only a declaration fails the render; an original that does not
			// parse is its loader's problem, and a declaration built on it
			// fails on the missing base.
			if declarationLine.Match(raw) {
				d.failures[name] = errors.New(DescribeParseError(path, err))
			}
			continue
		}
		if !machineControls(front) {
			continue
		}
		layer, err := parseDeclaration(path, raw, front, body, false)
		if err != nil {
			d.failures[name] = err
			continue
		}
		d.declarations[name] = layer
	}
	if err := d.readShared(); err != nil {
		return nil, err
	}
	return d, nil
}

// readShared loads every .md below a subdirectory of the agents directory:
// a shared file, never an agent, reached only through a declaration's
// extends path. Its key is its slash path relative to the agents directory.
func (d *declarationSet) readShared() error {
	root := filepath.Clean(d.agentsDir)
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() || filepath.Dir(path) == root || filepath.Ext(path) != ".md" || entry.Name() == "README.md" {
			return nil
		}
		key := d.ownerName(path)
		raw, err := paths.ReadRegular(path)
		if err != nil {
			d.failures[key] = fmt.Errorf("read shared file %s: %w", path, err)
			return nil
		}
		header, body, found, err := Split(string(raw))
		front := &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
		if err == nil && found {
			front, err = ParseHeader(header)
		}
		if err != nil {
			d.failures[key] = errors.New(DescribeParseError(path, err))
			return nil
		}
		layer, err := parseDeclaration(path, raw, front, body, true)
		if err != nil {
			d.failures[key] = err
			return nil
		}
		d.shared[key] = layer
		return nil
	})
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("enumerate shared files under %s: %w", root, err)
	}
	return nil
}

// ownerName names a source in pins and failures: an agent declaration by its
// file base, a shared file by its slash path relative to the agents directory.
func (d *declarationSet) ownerName(path string) string {
	rel, err := filepath.Rel(filepath.Clean(d.agentsDir), path)
	if err != nil || !strings.Contains(filepath.ToSlash(rel), "/") {
		return strings.TrimSuffix(filepath.Base(path), ".md")
	}
	return filepath.ToSlash(rel)
}

// parseDeclaration reads one agent declaration, or with shared one shared
// file: the same layer keys, without name or from.
func parseDeclaration(path string, raw []byte, front *yaml.Node, body string, shared bool) (layer, error) {
	l := layer{path: path, raw: raw, set: &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}}
	resolved, err := resolveDeclaration(front)
	if err != nil {
		return l, fmt.Errorf("DECLARATION-INVALID %s: %w", path, err)
	}
	front = resolved
	fail := func(field string) (layer, error) {
		return l, fmt.Errorf("DECLARATION-INVALID %s: %s", path, field)
	}
	if MapValue(front, "abstract") != nil {
		return fail("abstract is retired: move the file into a subdirectory, drop name and abstract, and name it by relative path in extends")
	}
	name := MapValue(front, "name")
	if shared {
		if name != nil || MapValue(front, "from") != nil {
			return fail("a shared file is not an agent: it carries no name and no from")
		}
	} else {
		if name == nil || name.Kind != yaml.ScalarNode || name.Tag != "!!str" || name.Value != strings.TrimSuffix(filepath.Base(path), ".md") || !declarationName.MatchString(name.Value) {
			return fail("name must equal the kebab-case file base")
		}
		n := MapValue(front, "from")
		if n == nil {
			return fail("from is required")
		}
		if n.Kind != yaml.ScalarNode || n.Tag != "!!str" || !declarationName.MatchString(n.Value) {
			return fail("from must name an agent")
		}
		l.from, l.hasFrom = n.Value, true
		if name.Value == GeneralPurpose {
			return fail("general-purpose is the built-in, never a declaration")
		}
	}
	for _, key := range []string{"extends", "unset"} {
		n := MapValue(front, key)
		if n == nil {
			continue
		}
		if n.Kind != yaml.SequenceNode {
			return fail(key + " must be a list of strings")
		}
		var values []string
		for _, item := range n.Content {
			if item.Kind != yaml.ScalarNode || item.Tag != "!!str" || item.Value == "" {
				return fail(key + " must be a list of non-empty strings")
			}
			if key == "unset" {
				first, _, _ := strings.Cut(item.Value, ".")
				if first == "name" || first == "role" || slices.Contains(DeclarationKeys, first) {
					return fail("unset cannot remove " + first)
				}
			}
			if key == "extends" && (!strings.HasSuffix(item.Value, ".md") || filepath.IsAbs(item.Value) || strings.Contains(item.Value, "\\")) {
				return fail("extends names a shared file by its relative path, e.g. shared/base.md")
			}
			values = append(values, item.Value)
		}
		if key == "extends" {
			l.extends = values
		} else {
			l.unset = values
		}
	}
	for i := 0; i+1 < len(front.Content); i += 2 {
		key, value := front.Content[i], front.Content[i+1]
		if key.Value == "name" || slices.Contains(DeclarationKeys, key.Value) {
			continue
		}
		if key.Value == "role" {
			return fail("role is stamped by the compiler")
		}
		l.set.Content = append(l.set.Content, canonicalNode(key), canonicalNode(value))
	}
	if n := MapValue(front, "add"); n != nil {
		if n.Kind != yaml.MappingNode {
			return fail("add must be a map")
		}
		l.add = canonicalNode(n)
		for i := 0; i+1 < len(l.add.Content); i += 2 {
			key, value := l.add.Content[i].Value, l.add.Content[i+1]
			if key == "name" || key == "role" || slices.Contains(DeclarationKeys, key) {
				return fail("add cannot set " + key)
			}
			if key == "tools" && value.Kind == yaml.ScalarNode && value.Tag == "!!str" {
				value = toolsSequence(value)
				l.add.Content[i+1] = value
			}
			if value.Kind != yaml.SequenceNode && value.Kind != yaml.MappingNode {
				return fail("add." + key + " must be a list or a map")
			}
		}
	}
	for _, key := range []string{"prepend", "append"} {
		if n := MapValue(front, key); n != nil {
			if n.Kind != yaml.ScalarNode || n.Tag != "!!str" {
				return fail(key + " must be text")
			}
			value := n.Value
			if key == "prepend" {
				l.prepend = &value
			} else {
				l.appendix = &value
			}
		}
	}
	for _, key := range []string{"rename", "pins", "retitle"} {
		if n := MapValue(front, key); n != nil {
			if n.Kind != yaml.MappingNode {
				return fail(key + " must be a map of strings")
			}
			for i := 0; i+1 < len(n.Content); i += 2 {
				if n.Content[i].Value == "" || n.Content[i+1].Kind != yaml.ScalarNode || n.Content[i+1].Tag != "!!str" {
					return fail(key + " must be a map of strings")
				}
			}
		}
	}
	if n := MapValue(front, "rename"); n != nil {
		for i := 0; i+1 < len(n.Content); i += 2 {
			l.rename = append(l.rename, renamePair{n.Content[i].Value, n.Content[i+1].Value})
		}
	}
	lines := strings.Split(body, "\n")
	var headings []bodyHeading
	for _, h := range bodyHeadings(lines) {
		if h.level == 2 {
			headings = append(headings, h)
		}
	}
	end := len(lines)
	if len(headings) > 0 {
		end = headings[0].line
	}
	addSection := func(key, text, heading string) {
		s := sectionOverride{key: key, text: strings.Join(trimBlankLines(strings.Split(text, "\n")), "\n"), heading: heading, owner: path}
		if pin := MapValue(MapValue(front, "pins"), key); pin != nil {
			s.pin, s.pinned = pin.Value, true
		}
		l.sections = append(l.sections, s)
	}
	intro := strings.Join(lines[:end], "\n")
	if strings.TrimSpace(intro) != "" {
		addSection(IntroSection, intro, "")
	}
	retitle := MapValue(front, "retitle")
	seen := map[string]bool{}
	for i, h := range headings {
		end := len(lines)
		if i+1 < len(headings) {
			end = headings[i+1].line
		}
		key, title := h.text, ""
		if retitle != nil {
			for j := 0; j+1 < len(retitle.Content); j += 2 {
				if retitle.Content[j+1].Value == h.text {
					key, title = retitle.Content[j].Value, h.text
				}
			}
		}
		if seen[key] {
			return fail("SECTION-AMBIGUOUS override " + key)
		}
		seen[key] = true
		addSection(key, strings.Join(lines[h.line+1:end], "\n"), title)
	}
	if retitle != nil {
		for i := 0; i+1 < len(retitle.Content); i += 2 {
			if !seen[retitle.Content[i].Value] {
				return fail("retitle has no body section " + retitle.Content[i+1].Value)
			}
		}
	}
	return l, nil
}

// canonicalNode preserves semantic values and comments, not declaration YAML style.
func canonicalNode(n *yaml.Node) *yaml.Node {
	out := cloneNode(n)
	var clear func(*yaml.Node)
	clear = func(n *yaml.Node) {
		n.Style = 0
		for _, child := range n.Content {
			clear(child)
		}
	}
	clear(out)
	return out
}

func (d *declarationSet) renderEach() (out []GeneratedAgent, failed map[string]error) {
	failed = maps.Clone(d.failures)
	for _, name := range slices.Sorted(maps.Keys(d.declarations)) {
		r, err := d.render(name, nil)
		// Declarations render into the source repository's directory; with
		// none named each one fails, named, instead of writing anywhere. The
		// render above still runs, so the shared files it extends count as used.
		if d.outputDir == "" {
			failed[name] = fmt.Errorf("VARIANT-INVALID %s: set machine.sourceGeneratedAgentsDir, the source-repository directory the rendered agents are written into", name)
			continue
		}
		if err != nil {
			failed[name] = err
			continue
		}
		content, err := d.content(name, r)
		if err != nil {
			failed[name] = fmt.Errorf("%s: declaration %q: %w", d.path, name, err)
			continue
		}
		out = append(out, GeneratedAgent{Name: name, From: d.declarations[name].from, Root: r.root, Path: filepath.Join(d.outputDir, name+".md"), Content: content})
	}
	// Shared-file failures are validation problems, never agent rows: a
	// broken extends chain, or a shared file no declaration extends.
	for _, key := range slices.Sorted(maps.Keys(d.shared)) {
		layer := d.shared[key]
		if _, err := d.sharedLayers(layer.path, layer.extends, []string{key}); err != nil {
			failed[key] = fmt.Errorf("%s: %w", layer.path, err)
		} else if !d.used[key] {
			failed[key] = fmt.Errorf("SHARED-UNUSED %s: no declaration extends it — extend it, or delete it", layer.path)
		}
	}
	return out, failed
}

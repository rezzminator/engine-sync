package source

import (
	"bytes"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/rezzminator/engine-sync/internal/atomicfile"
	"github.com/rezzminator/engine-sync/internal/paths"
	"go.yaml.in/yaml/v3"
)

// writePins is atomicfile.Write; a variable so a test can refuse a write.
var writePins = atomicfile.Write

// PinGlobalAgentVariants rewrites only source pins blocks, through symlinks.
// On a failed write it returns the pins already written with the error.
func PinGlobalAgentVariants(agentsDir string, names []string) ([]PinChange, error) {
	d, err := readVariantsDecl(agentsDir, agentsDir, nil)
	if err != nil {
		return nil, err
	}
	for _, name := range names {
		if _, ok := d.declarations[name]; !ok {
			if _, ok := d.shared[name]; !ok {
				if err := d.failures[name]; err != nil {
					return nil, err
				}
				return nil, fmt.Errorf("%s: no declaration or shared file %q", agentsDir, name)
			}
		}
	}
	d.repin = func(name string) bool { return len(names) == 0 || slices.Contains(names, name) }
	_, failed := d.renderEach()
	for _, name := range slices.Sorted(maps.Keys(failed)) {
		if d.repin(name) {
			return nil, failed[name]
		}
	}
	type rewrite struct {
		layer   variantLayer
		pins    map[string]string
		changes []PinChange
	}
	writes := map[string]*rewrite{}
	newPins := map[string]string{}
	for _, found := range d.pins {
		key := found.section.owner + "\x00" + found.section.key
		if prior, ok := newPins[key]; ok && prior != found.now {
			return nil, fmt.Errorf("%s: section %q declared once but pins differently for its consumers — declare it per agent", found.section.owner, found.section.key)
		}
		newPins[key] = found.now
		if found.section.pinned && found.section.pin == found.now {
			continue
		}
		w := writes[found.section.owner]
		if w == nil {
			owner := d.ownerName(found.section.owner)
			layer, ok := d.declarations[owner]
			if !ok {
				layer = d.shared[owner]
			}
			w = &rewrite{layer: layer, pins: map[string]string{}}
			writes[found.section.owner] = w
		}
		if _, seen := w.pins[found.section.key]; seen {
			continue
		}
		w.pins[found.section.key] = found.now
		variant := found.variant
		owner := d.ownerName(found.section.owner)
		if slices.Contains(names, owner) {
			variant = owner
		}
		w.changes = append(w.changes, PinChange{Variant: variant, Heading: found.section.key, Pin: found.now})
	}
	if err := d.refuseSharedDrift(newPins); err != nil {
		return nil, err
	}
	for _, name := range names {
		if layer, ok := d.shared[name]; ok {
			for _, section := range layer.sections {
				if _, used := newPins[section.owner+"\x00"+section.key]; !used {
					return nil, fmt.Errorf("%s: section %q has no declaration base to pin", layer.path, section.key)
				}
			}
		}
	}
	// Compute and validate every write before changing any file.
	type edit struct {
		path     string
		raw, out []byte
		mode     os.FileMode
		changes  []PinChange
	}
	var edits []edit
	for _, path := range slices.Sorted(maps.Keys(writes)) {
		w := writes[path]
		out, err := rewritePins(w.layer.raw, w.pins)
		if err != nil {
			return nil, fmt.Errorf("pins %s: %w", path, err)
		}
		target, err := filepath.EvalSymlinks(path)
		if err != nil {
			return nil, fmt.Errorf("resolve %s: %w", path, err)
		}
		info, err := os.Stat(target)
		if err != nil {
			return nil, fmt.Errorf("stat %s: %w", target, err)
		}
		current, err := paths.ReadRegular(target)
		if err != nil || !bytes.Equal(current, w.layer.raw) {
			return nil, fmt.Errorf("%s changed while pinning (%v) — run variants pin again", path, err)
		}
		edits = append(edits, edit{target, w.layer.raw, out, info.Mode().Perm(), w.changes})
	}
	var changes []PinChange
	for _, e := range edits {
		current, err := paths.ReadRegular(e.path)
		if err != nil || !bytes.Equal(current, e.raw) {
			return nil, fmt.Errorf("%s changed while pinning (%v) — run variants pin again", e.path, err)
		}
		if err := writePins(e.path, e.out, e.mode); err != nil {
			// Files written before this one stay pinned: name them.
			return changes, fmt.Errorf("write %s: %w", e.path, err)
		}
		changes = append(changes, e.changes...)
	}
	return changes, nil
}

func (d *variantsDecl) refuseSharedDrift(pins map[string]string) error {
	var drifted []string
	var heading string
	for _, use := range d.uses {
		pin, rewritten := pins[use.section.owner+"\x00"+use.section.key]
		if !rewritten || d.repin(use.variant) || d.repin(d.ownerName(use.section.owner)) || !use.section.pinned || use.section.pin != use.now || pin == use.now {
			continue
		}
		if !slices.Contains(drifted, fmt.Sprintf("%q", use.variant)) {
			drifted = append(drifted, fmt.Sprintf("%q", use.variant))
		}
		heading = use.section.key
	}
	if len(drifted) == 0 {
		return nil
	}
	slices.Sort(drifted)
	return fmt.Errorf("%s: pinning the shared section %q would turn %s from current to SECTION-DRIFT — name them too, or run variants pin with no names", d.path, heading, strings.Join(drifted, ", "))
}

// rewritePins replaces the YAML pins block without serializing the source.
func rewritePins(raw []byte, pins map[string]string) ([]byte, error) {
	header, _, found, err := Split(string(raw))
	if err != nil {
		return nil, err
	}
	if !found {
		return nil, fmt.Errorf("source has no frontmatter")
	}
	front, err := ParseHeader(header)
	if err != nil {
		return nil, err
	}
	resolved, err := resolveDeclaration(front)
	if err != nil {
		return nil, err
	}
	mapping := cloneNode(MapValue(resolved, "pins"))
	if mapping == nil {
		mapping = &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
	}
	for _, key := range slices.Sorted(maps.Keys(pins)) {
		value := &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: pins[key]}
		if MapValue(mapping, key) != nil {
			replaceValue(mapping, key, value)
		} else {
			mapping.Content = append(mapping.Content, &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: key}, value)
		}
	}
	mapping = canonicalNode(mapping)
	var block bytes.Buffer
	enc := yaml.NewEncoder(&block)
	enc.SetIndent(2)
	top := &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map", Content: []*yaml.Node{{Kind: yaml.ScalarNode, Tag: "!!str", Value: "pins"}, mapping}}
	if err := enc.Encode(top); err != nil {
		return nil, err
	}
	if err := enc.Close(); err != nil {
		return nil, err
	}
	lines := strings.SplitAfter(string(raw), "\n")
	close := -1
	for i := 1; i < len(lines); i++ {
		if strings.TrimSpace(lines[i]) == "---" {
			close = i
			break
		}
	}
	if close < 0 {
		return nil, fmt.Errorf("source has no closing fence")
	}
	start, end := close, close
	for i := 0; i+1 < len(front.Content); i += 2 {
		if front.Content[i].Value != "pins" {
			continue
		}
		start, end = front.Content[i].Line, close
		if i+2 < len(front.Content) {
			end = front.Content[i+2].Line
		}
		// Blank lines and top-level comments after the block are outside it.
		for end > start+1 {
			line := strings.TrimRight(lines[end-1], "\r\n")
			if strings.TrimSpace(line) == "" || strings.HasPrefix(line, "#") {
				end--
			} else {
				break
			}
		}
		break
	}
	newline := "\n"
	if bytes.Contains(raw, []byte("\r\n")) {
		newline = "\r\n"
	}
	text := strings.ReplaceAll(block.String(), "\n", newline)
	return []byte(strings.Join(lines[:start], "") + text + strings.Join(lines[end:], "")), nil
}

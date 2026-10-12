package source

import (
	"fmt"
	"slices"
	"strings"

	"go.yaml.in/yaml/v3"
)

func cloneNode(n *yaml.Node) *yaml.Node {
	if n == nil {
		return nil
	}
	c := *n
	c.Content = make([]*yaml.Node, len(n.Content))
	for i, child := range n.Content {
		c.Content[i] = cloneNode(child)
	}
	return &c
}

func unsetPath(node *yaml.Node, path []string) bool {
	if node == nil || node.Kind != yaml.MappingNode {
		return false
	}
	for i := 0; i+1 < len(node.Content); i += 2 {
		if node.Content[i].Value != path[0] {
			continue
		}
		if len(path) == 1 {
			node.Content = slices.Delete(node.Content, i, i+2)
			return true
		}
		return unsetPath(node.Content[i+1], path[1:])
	}
	return false
}

// setMerge applies a set layer: maps deep-merge, scalars and lists replace.
func setMerge(target, layer *yaml.Node) {
	for i := 0; i+1 < len(layer.Content); i += 2 {
		key, value := layer.Content[i], layer.Content[i+1]
		existing := MapValue(target, key.Value)
		switch {
		case existing == nil:
			target.Content = append(target.Content, key, value)
		case existing.Kind == yaml.MappingNode && value.Kind == yaml.MappingNode:
			setMerge(existing, value)
		default:
			replaceValue(target, key.Value, value)
		}
	}
}

// addMerge applies an add layer: lists append, maps deep-merge; a scalar
// target is an error. A comma-string tools value is read as its list.
func addMerge(target, layer *yaml.Node, prefix string) error {
	for i := 0; i+1 < len(layer.Content); i += 2 {
		key, value := layer.Content[i], layer.Content[i+1]
		path := prefix + key.Value
		existing := MapValue(target, key.Value)
		if existing != nil && path == "tools" && existing.Kind == yaml.ScalarNode {
			existing = toolsSequence(existing)
			replaceValue(target, key.Value, existing)
		}
		switch {
		case existing == nil:
			target.Content = append(target.Content, key, value)
		case existing.Kind == yaml.SequenceNode && value.Kind == yaml.SequenceNode:
			existing.Content = append(existing.Content, value.Content...)
		case existing.Kind == yaml.MappingNode && value.Kind == yaml.MappingNode:
			if err := addMerge(existing, value, path+"."); err != nil {
				return err
			}
		case existing.Kind == yaml.ScalarNode:
			return fmt.Errorf("add.%s: the frontmatter value is a scalar — use set", path)
		default:
			return fmt.Errorf("add.%s: a list adds to a list and a map to a map", path)
		}
	}
	return nil
}

func replaceValue(target *yaml.Node, key string, value *yaml.Node) {
	for i := 0; i+1 < len(target.Content); i += 2 {
		if target.Content[i].Value == key {
			if old := target.Content[i+1]; old.LineComment != "" && value.LineComment == "" {
				value.LineComment = old.LineComment
			}
			target.Content[i+1] = value
			return
		}
	}
}

func toolsSequence(scalar *yaml.Node) *yaml.Node {
	seq := &yaml.Node{Kind: yaml.SequenceNode, Tag: "!!seq"}
	for _, tool := range strings.Split(scalar.Value, ",") {
		if tool = strings.TrimSpace(tool); tool != "" {
			seq.Content = append(seq.Content, &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: tool})
		}
	}
	return seq
}

// normalizeTools renders tools as Claude's comma form `A, B, C`.
func normalizeTools(front *yaml.Node) {
	tools := MapValue(front, "tools")
	if tools == nil {
		return
	}
	seq := tools
	if tools.Kind == yaml.ScalarNode {
		seq = toolsSequence(tools)
	}
	names := make([]string, 0, len(seq.Content))
	for _, item := range seq.Content {
		names = append(names, item.Value)
	}
	replaceValue(front, "tools", &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: strings.Join(names, ", "), LineComment: tools.LineComment})
}

// stampIdentity sets name (in place, else first) and stamps role: {root}
// right after it; the built-in root stamps no role.
func stampIdentity(front *yaml.Node, name, root string) {
	for i := 0; i+1 < len(front.Content); i += 2 {
		if front.Content[i].Value == "role" {
			front.Content = slices.Delete(front.Content, i, i+2)
			break
		}
	}
	str := func(v string) *yaml.Node { return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: v} }
	at := -1
	for i := 0; i+1 < len(front.Content); i += 2 {
		if front.Content[i].Value == "name" {
			front.Content[i+1] = str(name)
			at = i
			break
		}
	}
	if at < 0 {
		front.Content = slices.Insert(front.Content, 0, str("name"), str(name))
		at = 0
	}
	if root != GeneralPurpose {
		front.Content = slices.Insert(front.Content, at+2, str("role"), str(root))
	}
}

// machineControls also recognizes controls inherited through YAML merge keys.
func machineControls(node *yaml.Node) bool {
	seen := map[*yaml.Node]bool{}
	var walk func(*yaml.Node) bool
	walk = func(n *yaml.Node) bool {
		if n == nil || seen[n] {
			return false
		}
		seen[n] = true
		if n.Kind == yaml.AliasNode {
			return walk(n.Alias)
		}
		if n.Kind == yaml.SequenceNode {
			for _, c := range n.Content {
				if walk(c) {
					return true
				}
			}
		}
		if n.Kind == yaml.MappingNode {
			for i := 0; i+1 < len(n.Content); i += 2 {
				if n.Content[i].Value == "from" || n.Content[i].Value == "abstract" {
					return true
				}
				if n.Content[i].Tag == "!!merge" && walk(n.Content[i+1]) {
					return true
				}
			}
		}
		return false
	}
	return walk(node)
}

// resolveDeclaration expands YAML aliases and merge keys before layer edits.
// Explicit map keys override merged keys; the first merged map wins.
func resolveDeclaration(node *yaml.Node) (*yaml.Node, error) {
	active := map[*yaml.Node]bool{}
	var resolve func(*yaml.Node) (*yaml.Node, error)
	resolve = func(n *yaml.Node) (*yaml.Node, error) {
		if n == nil {
			return nil, nil
		}
		if active[n] {
			return nil, fmt.Errorf("cyclic YAML alias")
		}
		active[n] = true
		defer delete(active, n)
		if n.Kind == yaml.AliasNode {
			return resolve(n.Alias)
		}
		out := *n
		out.Anchor = ""
		out.Content = nil
		if n.Kind != yaml.MappingNode {
			for _, child := range n.Content {
				c, err := resolve(child)
				if err != nil {
					return nil, err
				}
				out.Content = append(out.Content, c)
			}
			return &out, nil
		}
		seen := map[string]bool{}
		var merges []*yaml.Node
		for i := 0; i+1 < len(n.Content); i += 2 {
			key, value := n.Content[i], n.Content[i+1]
			if key.Kind != yaml.ScalarNode {
				return nil, fmt.Errorf("mapping key must be scalar")
			}
			if seen[key.Value] {
				return nil, fmt.Errorf("duplicate key %s", key.Value)
			}
			seen[key.Value] = true
			c, err := resolve(value)
			if err != nil {
				return nil, err
			}
			if key.Tag == "!!merge" {
				merges = append(merges, c)
				continue
			}
			out.Content = append(out.Content, cloneNode(key), c)
		}
		var merge func(*yaml.Node) error
		merge = func(m *yaml.Node) error {
			if m.Kind == yaml.SequenceNode {
				for _, c := range m.Content {
					if err := merge(c); err != nil {
						return err
					}
				}
				return nil
			}
			if m.Kind != yaml.MappingNode {
				return fmt.Errorf("YAML merge must name a map or list of maps")
			}
			for i := 0; i+1 < len(m.Content); i += 2 {
				if MapValue(&out, m.Content[i].Value) == nil {
					out.Content = append(out.Content, m.Content[i], m.Content[i+1])
				}
			}
			return nil
		}
		for _, m := range merges {
			if err := merge(m); err != nil {
				return nil, err
			}
		}
		return &out, nil
	}
	return resolve(node)
}

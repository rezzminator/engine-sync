package source

import (
	"bytes"
	"encoding/json"
	"fmt"
	"slices"
	"strconv"
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

// jsonNode converts one JSON value to a YAML node, keeping object key order
// and leaving every style to the encoder.
func jsonNode(raw []byte) (*yaml.Node, error) {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	var value func() (*yaml.Node, error)
	value = func() (*yaml.Node, error) {
		tok, err := dec.Token()
		if err != nil {
			return nil, err
		}
		switch t := tok.(type) {
		case json.Delim:
			node := &yaml.Node{Kind: yaml.SequenceNode, Tag: "!!seq"}
			if t == '{' {
				node = &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
			}
			for dec.More() {
				if t == '{' {
					key, err := dec.Token()
					if err != nil {
						return nil, err
					}
					node.Content = append(node.Content, &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: key.(string)})
				}
				child, err := value()
				if err != nil {
					return nil, err
				}
				node.Content = append(node.Content, child)
			}
			if _, err := dec.Token(); err != nil {
				return nil, err
			}
			return node, nil
		case string:
			return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: t}, nil
		case json.Number:
			tag := "!!int"
			if strings.ContainsAny(t.String(), ".eE") {
				tag = "!!float"
			}
			return &yaml.Node{Kind: yaml.ScalarNode, Tag: tag, Value: t.String()}, nil
		case bool:
			return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!bool", Value: strconv.FormatBool(t)}, nil
		default:
			return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!null", Value: "null"}, nil
		}
	}
	return value()
}

// jsonOffsets maps each value's JSON path (keys joined by NUL) to its byte
// span in raw.
func jsonOffsets(raw []byte) (map[string][2]int, error) {
	dec := json.NewDecoder(bytes.NewReader(raw))
	out := map[string][2]int{}
	var walk func(path []string) error
	walk = func(path []string) error {
		before := int(dec.InputOffset())
		tok, err := dec.Token()
		if err != nil {
			return err
		}
		start := before
		for start < len(raw) && strings.IndexByte(" \t\r\n:,", raw[start]) >= 0 {
			start++
		}
		key := strings.Join(path, "\x00")
		if delim, ok := tok.(json.Delim); ok {
			out[key] = [2]int{start, start + 1}
			for i := 0; dec.More(); i++ {
				child := strconv.Itoa(i)
				if delim == '{' {
					k, err := dec.Token()
					if err != nil {
						return err
					}
					child = k.(string)
				}
				if err := walk(append(slices.Clone(path), child)); err != nil {
					return err
				}
			}
			_, err := dec.Token()
			return err
		}
		out[key] = [2]int{start, int(dec.InputOffset())}
		return nil
	}
	return out, walk(nil)
}

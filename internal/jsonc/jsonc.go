// Package jsonc edits one top-level property of a JSONC document in place,
// leaving every comment and every other property byte-for-byte intact, so an
// operator's config never round-trips through encoding/json and loses its
// comments.
package jsonc

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"slices"

	"github.com/tailscale/hujson"
)

// DecodeObject parses raw as one JSONC object: comments and trailing commas
// allowed, a duplicate key or a non-object refused.
func DecodeObject(raw []byte) (map[string]any, error) {
	root, _, err := parseObject(raw)
	if err != nil {
		return nil, err
	}
	root.Standardize()
	var document map[string]any
	if err := json.Unmarshal(root.Pack(), &document); err != nil {
		return nil, err
	}
	return document, nil
}

// parseObject parses a copy of raw as a JSONC object whose top-level keys are
// unique: the tree's literals and extras alias the parsed bytes, and
// Standardize blanks comments in place, so the caller's raw stays untouched.
func parseObject(raw []byte) (hujson.Value, *hujson.Object, error) {
	root, err := hujson.Parse(bytes.Clone(raw))
	if err != nil {
		return root, nil, err
	}
	object, ok := root.Value.(*hujson.Object)
	if !ok {
		return root, nil, errors.New("expected object")
	}
	seen := map[string]bool{}
	for _, member := range object.Members {
		name := member.Name.Value.(hujson.Literal).String()
		if seen[name] {
			return root, nil, fmt.Errorf("duplicate object key %q", name)
		}
		seen[name] = true
	}
	return root, object, nil
}

func memberIndex(object *hujson.Object, name string) int {
	for index, member := range object.Members {
		if member.Name.Value.(hujson.Literal).String() == name {
			return index
		}
	}
	return -1
}

// SetProperty replaces the top-level property name's value with value, or
// appends the property when absent.
func SetProperty(raw []byte, name string, value []byte) ([]byte, error) {
	if !json.Valid(value) {
		return nil, fmt.Errorf("replacement for %q is not JSON", name)
	}
	replacement, err := hujson.Parse(value)
	if err != nil {
		return nil, fmt.Errorf("replacement for %q: %w", name, err)
	}
	root, object, err := parseObject(raw)
	if err != nil {
		return nil, err
	}
	if index := memberIndex(object, name); index >= 0 {
		object.Members[index].Value.Value = replacement.Value
		return root.Pack(), nil
	}
	// The new member goes on its own line, one level deeper than the closing
	// bracket, and stays last with no trailing comma, so the result is strict
	// JSON there.
	closingIndent := []byte{}
	if newline := bytes.LastIndexByte(object.AfterExtra, '\n'); newline >= 0 {
		line := object.AfterExtra[newline+1:]
		closingIndent = line[:len(line)-len(bytes.TrimLeft(line, " \t"))]
	}
	member := hujson.ObjectMember{
		Name:  hujson.Value{BeforeExtra: fmt.Appendf(nil, "%s\n%s  ", object.AfterExtra, closingIndent), Value: hujson.String(name)},
		Value: hujson.Value{BeforeExtra: hujson.Extra(" "), Value: replacement.Value},
	}
	object.Members = append(object.Members, member)
	object.AfterExtra = slices.Concat([]byte("\n"), closingIndent)
	return root.Pack(), nil
}

// RemoveProperty deletes the top-level property name and its separator; an
// absent property returns raw unchanged. The comments before the property
// survive, and a formatter-stripped trailing comma stays stripped.
func RemoveProperty(raw []byte, name string) ([]byte, error) {
	root, object, err := parseObject(raw)
	if err != nil {
		return nil, err
	}
	index := memberIndex(object, name)
	if index < 0 {
		return raw, nil
	}
	removed := object.Members[index]
	object.Members = append(object.Members[:index], object.Members[index+1:]...)
	if index < len(object.Members) {
		next := &object.Members[index].Name.BeforeExtra
		*next = slices.Concat(removed.Name.BeforeExtra, *next)
		return root.Pack(), nil
	}
	object.AfterExtra = slices.Concat(removed.Name.BeforeExtra, object.AfterExtra)
	// hujson marks a trailing comma by a non-nil AfterExtra on the last value;
	// the new last value keeps the removed one's.
	if index > 0 {
		prior := &object.Members[index-1].Value
		if removed.Value.AfterExtra == nil {
			object.AfterExtra = slices.Concat(prior.AfterExtra, object.AfterExtra)
			prior.AfterExtra = nil
		} else if prior.AfterExtra == nil {
			prior.AfterExtra = hujson.Extra{}
		}
	}
	return root.Pack(), nil
}

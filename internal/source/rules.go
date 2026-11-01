package source

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"go.yaml.in/yaml/v3"
)

// Rule is one Claude rule file, a global one or a project's.
type Rule struct {
	Path string // absolute
	Rel  string // below its rules directory, slash-separated
	// Scope is the rule's `paths:` globs; nil for an always-on rule.
	Scope []string
	// VisibleTo reports a rule-control `visible-to:` key: it narrows the
	// rule's loops on Claude Code only.
	VisibleTo bool
	Body      string
	// ParseErr is a header that does not parse: the rule is reported, never
	// compiled.
	ParseErr error
}

// Rules reads every rule below dir through read, in sorted relative-path
// order. An absent dir holds no rules; a directory or rule that cannot be
// read is an error, never an empty set.
func Rules(dir string, read func(string) ([]byte, error)) ([]Rule, error) {
	rels, err := ruleFiles(dir)
	if err != nil {
		return nil, err
	}
	rules := make([]Rule, 0, len(rels))
	for _, rel := range rels {
		path := filepath.Join(dir, rel)
		raw, err := read(path)
		if err != nil {
			return nil, fmt.Errorf("read rule %s: %w", path, err)
		}
		rule := Rule{Path: path, Rel: filepath.ToSlash(rel)}
		front, body, err := Parse(string(raw))
		if err != nil {
			rule.ParseErr = err
			rules = append(rules, rule)
			continue
		}
		rule.Body = body
		rule.Scope = scopeGlobs(MapValue(front.Node, "paths"))
		if visibleTo := MapValue(front.Node, "visible-to"); visibleTo != nil && visibleTo.Tag != "!!null" {
			rule.VisibleTo = true
		}
		rules = append(rules, rule)
	}
	return rules, nil
}

// scopeGlobs is a `paths:` value, a list or one scalar, as its globs; nil
// when it names no path.
func scopeGlobs(value *yaml.Node) []string {
	if value == nil || value.Tag == "!!null" || (len(value.Content) == 0 && strings.TrimSpace(value.Value) == "") {
		return nil
	}
	if value.Kind != yaml.SequenceNode {
		return []string{value.Value}
	}
	globs := make([]string, 0, len(value.Content))
	for _, item := range value.Content {
		globs = append(globs, item.Value)
	}
	return globs
}

// ruleFiles lists every .md below dir, following links, as sorted relative
// paths; an absent dir has none. A dangling link that is not a .md names no
// rule and is passed over; one that is, or any other failure, is an error.
func ruleFiles(dir string) ([]string, error) {
	if _, err := os.Stat(dir); errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	} else if err != nil {
		return nil, fmt.Errorf("inspect rules %s: %w", dir, err)
	}
	var rels []string
	visited := map[string]bool{}
	var walk func(current, relative string) error
	walk = func(current, relative string) error {
		resolved, err := filepath.EvalSymlinks(current)
		if err != nil {
			return fmt.Errorf("resolve rules %s: %w", current, err)
		}
		if visited[resolved] {
			return nil // a cycle: the directory is already being listed
		}
		visited[resolved] = true
		defer delete(visited, resolved)
		entries, err := os.ReadDir(current)
		if err != nil {
			return fmt.Errorf("read rules %s: %w", current, err)
		}
		for _, entry := range entries {
			path, rel := filepath.Join(current, entry.Name()), filepath.Join(relative, entry.Name())
			isRule := strings.HasSuffix(entry.Name(), ".md")
			if !isRule && !entry.IsDir() && entry.Type()&fs.ModeSymlink == 0 {
				continue
			}
			info, err := os.Stat(path)
			if !isRule && errors.Is(err, fs.ErrNotExist) {
				continue
			}
			if err != nil {
				return fmt.Errorf("inspect rule %s: %w", path, err)
			}
			if info.IsDir() {
				if err := walk(path, rel); err != nil {
					return err
				}
			} else if isRule {
				rels = append(rels, rel)
			}
		}
		return nil
	}
	if err := walk(dir, ""); err != nil {
		return nil, err
	}
	sort.Slice(rels, func(i, j int) bool { return filepath.ToSlash(rels[i]) < filepath.ToSlash(rels[j]) })
	return rels, nil
}

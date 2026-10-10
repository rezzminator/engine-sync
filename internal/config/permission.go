package config

import (
	"bytes"
	"encoding/json"
	"fmt"
)

// Permission is OpenCode's native tool -> action-or-ordered-patterns object.
type Permission struct {
	Action string
	Tools  []ToolPermission
}
type ToolPermission struct {
	Name  string
	Value PermissionValue
}

// PermissionValue preserves native object order because the last matching rule wins.
type PermissionValue struct {
	Action string
	Rules  []PermissionRule
}
type PermissionRule struct{ Pattern, Action string }

func validAction(value string) bool { return value == "allow" || value == "deny" || value == "ask" }
func (value *PermissionValue) UnmarshalJSON(raw []byte) error {
	*value = PermissionValue{}
	if err := json.Unmarshal(raw, &value.Action); err == nil {
		if !validAction(value.Action) {
			return fmt.Errorf("invalid permission action %q", value.Action)
		}
		return nil
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	token, err := decoder.Token()
	if err != nil || token != json.Delim('{') {
		return fmt.Errorf("permission must be allow, deny, ask, or an ordered pattern object")
	}
	seen := map[string]bool{}
	for decoder.More() {
		token, err := decoder.Token()
		if err != nil {
			return err
		}
		pattern := token.(string)
		if pattern == "" || seen[pattern] {
			return fmt.Errorf("invalid or duplicate permission pattern %q", pattern)
		}
		seen[pattern] = true
		var action string
		if err := decoder.Decode(&action); err != nil {
			return fmt.Errorf("pattern %q: %w", pattern, err)
		}
		if !validAction(action) {
			return fmt.Errorf("pattern %q: invalid permission action %q", pattern, action)
		}
		value.Rules = append(value.Rules, PermissionRule{Pattern: pattern, Action: action})
	}
	_, err = decoder.Token()
	return err
}
func (value PermissionValue) MarshalJSON() ([]byte, error) {
	if value.Action != "" {
		return json.Marshal(value.Action)
	}
	var out bytes.Buffer
	out.WriteByte('{')
	for i, rule := range value.Rules {
		if i > 0 {
			out.WriteByte(',')
		}
		pattern, _ := json.Marshal(rule.Pattern)
		action, _ := json.Marshal(rule.Action)
		out.Write(pattern)
		out.WriteByte(':')
		out.Write(action)
	}
	out.WriteByte('}')
	return out.Bytes(), nil
}

// UnmarshalJSON merges tool entries across layers while keeping rule precedence.
func (permission *Permission) UnmarshalJSON(raw []byte) error {
	var action string
	if err := json.Unmarshal(raw, &action); err == nil {
		if !validAction(action) {
			return fmt.Errorf("invalid permission action %q", action)
		}
		*permission = Permission{Action: action}
		return nil
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	token, err := decoder.Token()
	if err != nil || token != json.Delim('{') {
		return fmt.Errorf("permission must be an action or tool object")
	}
	permission.Action = ""
	tools := append([]ToolPermission(nil), permission.Tools...)
	seen := map[string]bool{}
	for decoder.More() {
		token, err := decoder.Token()
		if err != nil {
			return err
		}
		name := token.(string)
		if name == "" || seen[name] {
			return fmt.Errorf("invalid or duplicate permission tool %q", name)
		}
		seen[name] = true
		var value PermissionValue
		if err := decoder.Decode(&value); err != nil {
			return fmt.Errorf("%s: %w", name, err)
		}
		// A layer that names a tool again replaces its rules in place: tool
		// order is precedence, and a later layer never moves a tool past another.
		replaced := false
		for i, tool := range tools {
			if tool.Name == name {
				tools[i] = ToolPermission{Name: name, Value: value}
				replaced = true
				break
			}
		}
		if !replaced {
			tools = append(tools, ToolPermission{Name: name, Value: value})
		}
	}
	if _, err := decoder.Token(); err != nil {
		return err
	}
	permission.Tools = tools
	return nil
}
func (permission Permission) MarshalJSON() ([]byte, error) {
	if permission.Action != "" {
		return json.Marshal(permission.Action)
	}
	var out bytes.Buffer
	out.WriteByte('{')
	for i, tool := range permission.Tools {
		if i > 0 {
			out.WriteByte(',')
		}
		name, _ := json.Marshal(tool.Name)
		value, err := json.Marshal(tool.Value)
		if err != nil {
			return nil, err
		}
		out.Write(name)
		out.WriteByte(':')
		out.Write(value)
	}
	out.WriteByte('}')
	return out.Bytes(), nil
}

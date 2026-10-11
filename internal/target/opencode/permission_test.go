package opencode

import (
	"encoding/json"
	"github.com/rezzminator/engine-sync/internal/config"
	"github.com/rezzminator/engine-sync/internal/testjail"
	"os"
	"path"
	"path/filepath"
	"testing"
)

func TestNativePermissionForms(t *testing.T) {
	for _, tc := range []struct{ name, input, want string }{
		{"whole policy action", `"ask"`, `"ask"`},
		{"ordered tool patterns", `{"edit":{"*":"deny","src/**":"allow","src/private/**":"ask"},"bash":"ask"}`, `{"edit":{"*":"deny","src/**":"allow","src/private/**":"ask"},"bash":"ask"}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var permission config.Permission
			if err := json.Unmarshal([]byte(tc.input), &permission); err != nil {
				t.Fatalf("native permission: %v", err)
			}
			raw, err := json.Marshal(permission)
			if err != nil || string(raw) != tc.want {
				t.Fatalf("round trip=%s err=%v want=%s", raw, err, tc.want)
			}
		})
	}
}

func TestReviewNativeDeniedDoors(t *testing.T) {
	root, home := testjail.Scratch(t), testjail.Scratch(t)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	writeOpenCodeFixture(t, filepath.Join(root, "CLAUDE.md"), "Sample.\n")
	writeOpenCodeFixture(t, filepath.Join(home, ".config", "engine-sync", "config.json"), `{"version":1,"opencode":{"permission":"deny"}}`)
	writeOpenCodeFixture(t, filepath.Join(root, ".claude", "engine-sync.json"), `{"version":1,"opencode":{"permission":{"edit":{"notes.md":"allow"},"bash":{"git status":"allow"}}}}`)
	result, err := Compile(Options{Root: root, Home: home, Mode: ModeBuild})
	if err != nil || !result.OK {
		t.Fatalf("compile=%#v %v", result, err)
	}
	raw, err := os.ReadFile(filepath.Join(root, ".opencode", "opencode.jsonc"))
	if err != nil {
		t.Fatal(err)
	}
	var object struct {
		Permission config.Permission `json:"permission"`
	}
	if err := json.Unmarshal(parseOpenCodeJSONC(raw), &object); err != nil {
		t.Fatal(err)
	}
	action := func(tool, value string) string {
		answer := "allow"
		for _, entry := range object.Permission.Tools {
			if entry.Name != "*" && entry.Name != tool {
				continue
			}
			if entry.Value.Action != "" {
				answer = entry.Value.Action
			}
			for _, rule := range entry.Value.Rules {
				if matched, _ := path.Match(rule.Pattern, value); matched {
					answer = rule.Action
				}
			}
		}
		return answer
	}
	for _, door := range []struct{ tool, value, want string }{{"edit", "CLAUDE.md", "deny"}, {"edit", "AGENTS.md", "deny"}, {"edit", ".claude/settings.json", "deny"}, {"bash", "git push origin", "deny"}, {"bash", "git commit -m change", "deny"}, {"edit", "notes.md", "allow"}, {"bash", "git status", "allow"}} {
		if got := action(door.tool, door.value); got != door.want {
			t.Errorf("native %s %s=%s want %s (fail-open policy)", door.tool, door.value, got, door.want)
		}
	}
}

func TestAuditMachinePermissionWithProjectBash(t *testing.T) {
	root, home := testjail.Scratch(t), testjail.Scratch(t)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	raw, err := os.ReadFile(filepath.Join("..", "..", "..", "testdata", "machine-permission.json"))
	if err != nil {
		t.Fatal(err)
	}
	writeOpenCodeFixture(t, filepath.Join(home, ".config", "engine-sync", "config.json"), string(raw))
	writeOpenCodeFixture(t, filepath.Join(root, "CLAUDE.md"), "Sample.\n")
	writeOpenCodeFixture(t, filepath.Join(root, ".claude", "engine-sync.json"), `{"version":1,"opencode":{"permission":{"bash":{"git status":"allow","git push":"allow"}}}}`)
	result, err := Compile(Options{Root: root, Home: home, Mode: ModeBuild})
	if err != nil || !result.OK {
		t.Fatalf("%#v %v", result, err)
	}
	raw, err = os.ReadFile(filepath.Join(root, ".opencode", "opencode.jsonc"))
	if err != nil {
		t.Fatal(err)
	}
	var output struct {
		Permission config.Permission `json:"permission"`
	}
	if err = json.Unmarshal(parseOpenCodeJSONC(raw), &output); err != nil {
		t.Fatal(err)
	}
	for _, command := range []string{"git push", "git push origin", "git -C repo push origin", "git commit -m change"} {
		answer := "deny"
		for _, tool := range output.Permission.Tools {
			if tool.Name != "*" && tool.Name != "bash" {
				continue
			}
			if tool.Value.Action != "" {
				answer = tool.Value.Action
			}
			for _, rule := range tool.Value.Rules {
				if match, _ := path.Match(rule.Pattern, command); match {
					answer = rule.Action
				}
			}
		}
		if answer != "deny" {
			t.Errorf("%s=%s", command, answer)
		}
	}
}

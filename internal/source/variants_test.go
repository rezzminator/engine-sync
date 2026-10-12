package source

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rezzminator/engine-sync/internal/testjail"
)

// TestPinGlobalAgentVariants: `variants pin` rewrites only pin values — a
// stale inline pin is replaced in place, an unpinned multi-line override
// gains a pin line at its own indentation — and every other byte, key order
// included, survives; a second pin changes nothing.
func TestPinGlobalAgentVariants(t *testing.T) {
	decl := "{\n  \"version\": 2,\n  \"variants\": {\n" +
		"    \"lead-a\": {\"from\": \"lead\", \"sections\": {\"Budget\": {\"pin\": \"sha256:00\", \"text\": \"Spend more.\"}}},\n" +
		"    \"lead-b\": {\n      \"from\": \"lead\",\n      \"sections\": {\n        \"Procedure\": {\n          \"text\": \"Do less.\"\n        }\n      }\n    }\n  }\n}\n"
	budget, err := SectionPin(variantLeadBody, "Budget")
	if err != nil {
		t.Fatal(err)
	}
	procedure, err := SectionPin(variantLeadBody, "Procedure")
	if err != nil {
		t.Fatal(err)
	}
	want := "{\n  \"version\": 2,\n  \"variants\": {\n" +
		"    \"lead-a\": {\"from\": \"lead\", \"sections\": {\"Budget\": {\"pin\": \"" + budget + "\", \"text\": \"Spend more.\"}}},\n" +
		"    \"lead-b\": {\n      \"from\": \"lead\",\n      \"sections\": {\n        \"Procedure\": {\n          \"pin\": \"" + procedure + "\",\n          \"text\": \"Do less.\"\n        }\n      }\n    }\n  }\n}\n"
	dir := filepath.Join(testjail.Scratch(t), "agents")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, GlobalAgentVariantsFile)
	for file, content := range map[string]string{"lead.md": variantLead, GlobalAgentVariantsFile: decl} {
		if err := os.WriteFile(filepath.Join(dir, file), []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	// A named pin is never blocked by another variant's own drift: lead-a's
	// stale pin stays, lead-b alone is pinned.
	onlyB := "{\n  \"version\": 2,\n  \"variants\": {\n" +
		"    \"lead-a\": {\"from\": \"lead\", \"sections\": {\"Budget\": {\"pin\": \"sha256:00\", \"text\": \"Spend more.\"}}},\n" +
		"    \"lead-b\": {\n      \"from\": \"lead\",\n      \"sections\": {\n        \"Procedure\": {\n          \"pin\": \"" + procedure + "\",\n          \"text\": \"Do less.\"\n        }\n      }\n    }\n  }\n}\n"
	changes, err := PinGlobalAgentVariants(dir, []string{"lead-b"})
	if err != nil || len(changes) != 1 || changes[0] != (PinChange{Variant: "lead-b", Heading: "Procedure", Pin: procedure}) {
		t.Fatalf("pin lead-b = %+v (%v), want lead-b's pin alone", changes, err)
	}
	if raw, err := os.ReadFile(path); err != nil || string(raw) != onlyB {
		t.Fatalf("variants.json after pin lead-b (%v):\n%s\nwant:\n%s", err, raw, onlyB)
	}
	changes, err = PinGlobalAgentVariants(dir, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(changes) != 1 || changes[0] != (PinChange{Variant: "lead-a", Heading: "Budget", Pin: budget}) {
		t.Fatalf("changes = %+v", changes)
	}
	if err := os.WriteFile(path, []byte(decl), 0o600); err != nil {
		t.Fatal(err)
	}
	changes, err = PinGlobalAgentVariants(dir, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(changes) != 2 || changes[0] != (PinChange{Variant: "lead-a", Heading: "Budget", Pin: budget}) ||
		changes[1] != (PinChange{Variant: "lead-b", Heading: "Procedure", Pin: procedure}) {
		t.Fatalf("changes = %+v", changes)
	}
	if raw, err := os.ReadFile(path); err != nil || string(raw) != want {
		t.Fatalf("variants.json after pin (%v):\n%s\nwant:\n%s", err, raw, want)
	}
	if again, err := PinGlobalAgentVariants(dir, nil); err != nil || len(again) != 0 {
		t.Fatalf("second pin = %+v (%v), want no change", again, err)
	}
	if raw, err := os.ReadFile(path); err != nil || string(raw) != want {
		t.Fatalf("second pin rewrote variants.json (%v):\n%s", err, raw)
	}
}

// `variants pin {names}` refuses, naming them, a rewrite of a shared section
// that would turn a variant it was not asked to pin from current to
// SECTION-DRIFT; pinning with no names pins all. A symlinked declaration is
// written through to its target and stays a link.
func TestPinRefusesDriftingAnUnaskedVariantAndWritesThroughALink(t *testing.T) {
	root := testjail.Scratch(t)
	dir := filepath.Join(root, "agents")
	shared := filepath.Join(root, "shared", GlobalAgentVariantsFile)
	for _, d := range []string{dir, filepath.Dir(shared)} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	agent := func(name, budget string) string {
		return "---\nname: " + name + "\ndescription: Leads.\n---\nLead.\n\n## Budget\n\n" + budget + "\n"
	}
	decl := `{"version":2,"profiles":{"p":{"sections":{"Budget":{"text":"Spend more."}}}},"variants":{"a":{"from":"lead","profile":"p"},"b":{"from":"other","profile":"p"}}}`
	for path, content := range map[string]string{filepath.Join(dir, "lead.md"): agent("lead", "Spend little."), filepath.Join(dir, "other.md"): agent("other", "Spend little."), shared: decl} {
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	link := filepath.Join(dir, GlobalAgentVariantsFile)
	if err := os.Symlink(shared, link); err != nil {
		t.Fatal(err)
	}
	if _, err := PinGlobalAgentVariants(dir, nil); err != nil {
		t.Fatalf("pin all: %v", err)
	}
	if info, err := os.Lstat(link); err != nil || info.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("variants.json after pin: %v, %v; want the link kept", info, err)
	}
	if raw, err := os.ReadFile(shared); err != nil || !strings.Contains(string(raw), `"pin"`) {
		t.Fatalf("link target not pinned: %s (%v)", raw, err)
	}
	if err := os.WriteFile(filepath.Join(dir, "lead.md"), []byte(agent("lead", "Spend nothing.")), 0o644); err != nil {
		t.Fatal(err)
	}
	before, _ := os.ReadFile(shared)
	if _, err := PinGlobalAgentVariants(dir, []string{"a"}); err == nil || !strings.Contains(err.Error(), `"b"`) || !strings.Contains(err.Error(), "SECTION-DRIFT") {
		t.Fatalf("pin a = %v, want a refusal naming b", err)
	}
	if after, _ := os.ReadFile(shared); string(after) != string(before) {
		t.Fatalf("a refused pin rewrote the declaration")
	}
}

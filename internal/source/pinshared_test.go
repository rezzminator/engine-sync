package source

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rezzminator/engine-sync/internal/testjail"
)

func TestPinRefusesDriftingAnUnaskedVariantAndWritesThroughALink(t *testing.T) {
	root := testjail.Scratch(t)
	dir := filepath.Join(root, "agents")
	shared := filepath.Join(root, "shared.md")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	agent := func(name, budget string) string {
		return "---\nname: " + name + "\ndescription: Leads.\n---\nLead.\n\n## Budget\n\n" + budget + "\n"
	}
	for path, text := range map[string]string{
		filepath.Join(dir, "lead.md"): agent("lead", "Spend little."), filepath.Join(dir, "other.md"): agent("other", "Spend little."),
		shared:                     "---\nname: lane\nabstract: true\n---\n\n## Budget\n\nSpend more.\n",
		filepath.Join(dir, "a.md"): "---\nname: a\nfrom: lead\nextends: [lane]\n---\n",
		filepath.Join(dir, "b.md"): "---\nname: b\nfrom: other\nextends: [lane]\n---\n",
	} {
		if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	link := filepath.Join(dir, "lane.md")
	if err := os.Symlink(shared, link); err != nil {
		t.Fatal(err)
	}
	if _, err := PinGlobalAgentVariants(dir, []string{"lane"}); err != nil {
		t.Fatalf("pin abstract: %v", err)
	}
	if info, err := os.Lstat(link); err != nil || info.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("link replaced: %v (%v)", info, err)
	}
	raw, err := os.ReadFile(shared)
	if err != nil || !strings.Contains(string(raw), "pins:\n  Budget: "+variantPin("Spend little.")) {
		t.Fatalf("target = %q (%v)", raw, err)
	}
	if err := os.WriteFile(filepath.Join(dir, "lead.md"), []byte(agent("lead", "Spend nothing.")), 0o644); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(shared)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := PinGlobalAgentVariants(dir, []string{"a"}); err == nil || !strings.Contains(err.Error(), `"b"`) || !strings.Contains(err.Error(), "SECTION-DRIFT") {
		t.Fatalf("pin a = %v, want refusal naming b", err)
	}
	after, err := os.ReadFile(shared)
	if err != nil || string(after) != string(before) {
		t.Fatalf("refused pin changed target: %v", err)
	}
	if _, err := PinGlobalAgentVariants(dir, nil); err == nil || !strings.Contains(err.Error(), "pins differently") {
		t.Fatalf("incompatible shared pins = %v", err)
	}
}

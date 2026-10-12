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
		shared:                     "---\n---\n\n## Budget\n\nSpend more.\n",
		filepath.Join(dir, "a.md"): "---\nname: a\nfrom: lead\nextends: [shared/lane.md]\n---\n",
		filepath.Join(dir, "b.md"): "---\nname: b\nfrom: other\nextends: [shared/lane.md]\n---\n",
	} {
		if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	link := filepath.Join(dir, "shared", "lane.md")
	if err := os.MkdirAll(filepath.Dir(link), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(shared, link); err != nil {
		t.Fatal(err)
	}
	if _, err := PinGlobalAgentVariants(dir, []string{"shared/lane.md"}); err != nil {
		t.Fatalf("pin shared file: %v", err)
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

// TestPinIgnoresAnUnrelatedBrokenOriginal: `variants pin` with no names
// refuses only on a declaration, a shared file, or a base one of them uses; an
// original agent nobody derives from, with a header that does not parse,
// stays its own problem.
func TestPinIgnoresAnUnrelatedBrokenOriginal(t *testing.T) {
	dir := testjail.Scratch(t)
	for name, text := range map[string]string{
		"lead.md":     "---\nname: lead\ndescription: Leads.\n---\nLead.\n\n## Budget\n\nSpend little.\n",
		"lead-pro.md": "---\nname: lead-pro\nfrom: lead\n---\n\n## Budget\n\nSpend more.\n",
		"notes.md":    "---\nname: notes\ndescription: Notes.\nengines:\n\tcodex: false\n---\nNotes.\n",
	} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	changes, err := PinGlobalAgentVariants(dir, nil)
	if err != nil || len(changes) != 1 {
		t.Fatalf("pin = %v (%v), want lead-pro's Budget pinned despite notes.md", changes, err)
	}
}

// TestPinReportsWhatItWroteBeforeAFailedWrite: pins span several files; when a
// later write fails, the pins already written are returned with the error.
func TestPinReportsWhatItWroteBeforeAFailedWrite(t *testing.T) {
	dir := testjail.Scratch(t)
	for name, text := range map[string]string{
		"lead.md":  "---\nname: lead\ndescription: Leads.\n---\nLead.\n\n## Budget\n\nSpend little.\n",
		"a-pro.md": "---\nname: a-pro\nfrom: lead\n---\n\n## Budget\n\nSpend more.\n",
		"b-pro.md": "---\nname: b-pro\nfrom: lead\n---\n\n## Budget\n\nSpend most.\n",
	} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	real := writePins
	t.Cleanup(func() { writePins = real })
	writePins = func(path string, data []byte, mode os.FileMode) error {
		if filepath.Base(path) == "b-pro.md" {
			return os.ErrPermission
		}
		return real(path, data, mode)
	}
	changes, err := PinGlobalAgentVariants(dir, nil)
	if err == nil || len(changes) != 1 || changes[0].Variant != "a-pro" {
		t.Fatalf("pin = %v (%v), want a-pro's pin reported with b-pro's write error", changes, err)
	}
}

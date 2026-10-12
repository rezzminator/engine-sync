package source

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rezzminator/engine-sync/internal/testjail"
)

// TestPinGlobalAgentVariants keeps every byte outside pins, including CRLF.
func TestPinGlobalAgentVariants(t *testing.T) {
	for _, newline := range []string{"\n", "\r\n"} {
		t.Run(strings.ReplaceAll(newline, "\r", "CR"), func(t *testing.T) {
			dir := filepath.Join(testjail.Scratch(t), "agents")
			if err := os.MkdirAll(dir, 0o755); err != nil {
				t.Fatal(err)
			}
			a := "---\n# source note\nname: lead-a\nfrom: lead\npins: {Budget: 'sha256:00'}\nmodel: 'sonnet' # keep me\n---\n\n## Budget\n\nSpend more.\n"
			b := "---\nname: lead-b\nfrom: lead\nappend: |-\n  Tail.\n---\n\n## Procedure\n\nDo less.\n"
			for name, text := range map[string]string{"lead.md": variantLead, "lead-a.md": a, "lead-b.md": b} {
				if err := os.WriteFile(filepath.Join(dir, name), []byte(strings.ReplaceAll(text, "\n", newline)), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			procedure, err := SectionPin(variantLeadBody, "Procedure")
			if err != nil {
				t.Fatal(err)
			}
			budget, err := SectionPin(variantLeadBody, "Budget")
			if err != nil {
				t.Fatal(err)
			}
			changes, err := PinGlobalAgentVariants(dir, []string{"lead-b"})
			if err != nil || len(changes) != 1 || changes[0] != (PinChange{Variant: "lead-b", Heading: "Procedure", Pin: procedure}) {
				t.Fatalf("pin lead-b = %+v (%v)", changes, err)
			}
			wantB := strings.Replace(b, "---\n\n## Procedure", "pins:\n  Procedure: "+procedure+"\n---\n\n## Procedure", 1)
			check := func(name, want string) {
				t.Helper()
				raw, err := os.ReadFile(filepath.Join(dir, name+".md"))
				if err != nil || string(raw) != strings.ReplaceAll(want, "\n", newline) {
					t.Fatalf("%s = %q (%v), want %q", name, raw, err, want)
				}
			}
			check("lead-a", a)
			check("lead-b", wantB)
			changes, err = PinGlobalAgentVariants(dir, nil)
			if err != nil || len(changes) != 1 || changes[0] != (PinChange{Variant: "lead-a", Heading: "Budget", Pin: budget}) {
				t.Fatalf("pin all = %+v (%v)", changes, err)
			}
			wantA := strings.Replace(a, "pins: {Budget: 'sha256:00'}\n", "pins:\n  Budget: "+budget+"\n", 1)
			check("lead-a", wantA)
			check("lead-b", wantB)
			if again, err := PinGlobalAgentVariants(dir, nil); err != nil || len(again) != 0 {
				t.Fatalf("second pin = %+v (%v)", again, err)
			}
			check("lead-a", wantA)
			check("lead-b", wantB)
			if _, err := PinGlobalAgentVariants(dir, []string{"ghost"}); err == nil || !strings.Contains(err.Error(), "ghost") {
				t.Fatalf("unknown = %v", err)
			}
		})
	}
}

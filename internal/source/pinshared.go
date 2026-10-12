package source

import (
	"fmt"
	"slices"
	"strings"
)

// byPathPins is each declared section path's new pin, in rewrite order.
func byPathPins(order []string, pin func(string) string) map[string]string {
	pins := make(map[string]string, len(order))
	for _, key := range order {
		pins[key] = pin(key)
	}
	return pins
}

// refuseSharedDrift refuses a pin rewrite of a shared section (a profile or
// fragment) that would turn a variant the caller did not ask to pin from
// current to SECTION-DRIFT, naming each such variant.
func (d *variantsDecl) refuseSharedDrift(pins map[string]string) error {
	var drifted []string
	var heading string
	for _, use := range d.uses {
		pin, rewritten := pins[strings.Join(use.section.at, "\x00")]
		if !rewritten || d.repin(use.variant) || !use.section.pinned || use.section.pin != use.now || pin == use.now {
			continue
		}
		if !slices.Contains(drifted, fmt.Sprintf("%q", use.variant)) {
			drifted = append(drifted, fmt.Sprintf("%q", use.variant))
		}
		heading = use.section.key
	}
	if len(drifted) == 0 {
		return nil
	}
	slices.Sort(drifted)
	return fmt.Errorf("%s: pinning the shared section %q would turn %s from current to SECTION-DRIFT — name them too, or run variants pin with no names",
		d.path, heading, strings.Join(drifted, ", "))
}

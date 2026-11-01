package reconcile

type State string

const (
	Missing     State = "MISSING"
	Stale       State = "STALE"
	Orphan      State = "ORPHAN"
	Conflict    State = "CONFLICT"
	Unparseable State = "UNPARSEABLE"
	Invalid     State = "INVALID"
	Dangling    State = "DANGLING"
	Kept        State = "KEPT"
	// OverBudget is a file written whole that its reader cuts, or one whose
	// reader's limit could not be read.
	OverBudget State = "OVER-BUDGET"
)

type Mode uint8

const (
	Build Mode = iota
	Check
	Plan
)

type Action struct{ Kind, Path, Target string }
type Result struct {
	Dangling                        []string
	Wrote, Unchanged, Deleted       int
	Warnings, Problems, Rebuildable []string
	Actions                         []Action
}

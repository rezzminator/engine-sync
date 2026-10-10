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

package diag

import (
	"encoding/json"
	"fmt"
	"github.com/rezzminator/engine-sync/internal/reconcile"
	"github.com/rezzminator/engine-sync/internal/source"
	"io"
)

type Severity uint8

const (
	Error Severity = iota
	Finding
	Warning
)

type Diagnostic struct {
	Severity                      Severity
	State                         reconcile.State
	Path, Source, Message, Remedy string
}

func Text(w io.Writer, diagnostics []Diagnostic) {
	for _, d := range diagnostics {
		fmt.Fprintln(w, d.Message)
		if d.Remedy != "" {
			fmt.Fprintln(w, d.Remedy)
		}
	}
}
func JSON(w io.Writer, value any) error { return json.NewEncoder(w).Encode(value) }

type Report struct {
	OK                                        bool
	Warnings, Problems, Rebuildable, Dangling []string
	Source                                    *source.Project
	LeafLinks                                 []source.Entry
	Wrote, Unchanged, Deleted                 int
	Actions                                   []reconcile.Action
}

// danglingSource records a source that could not be resolved: a warning for
// every mode, plus the typed entry Check gates on.
func (result *Report) RecordDangling(path string, err error) {
	message := fmt.Sprintf("dangling source %s: %v", path, err)
	result.Warnings = append(result.Warnings, message)
	result.Dangling = append(result.Dangling, message)
}

// unresolvedLeaf records a leaf source link discoverMarkdown cannot resolve:
// the typed dangling entry Check gates on, plus the link itself so a caller
// that can name the link's twin keeps that twin instead of sweeping it as an
// orphan (source.TargetTarget names an unreadable link's read failure).
func (result *Report) RecordUnresolved(path, rel string, err error) {
	result.RecordDangling(path, err)
	result.LeafLinks = append(result.LeafLinks, source.Entry{
		Path:   path,
		Rel:    rel,
		Target: result.Source.LinkTarget(path),
	})
}

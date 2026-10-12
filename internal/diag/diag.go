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

// Merge folds one engine's result into a combined report: OK only while every
// engine passed, every list and count accumulated.
func (result *Report) Merge(r reconcile.Result, pass bool) {
	result.OK = result.OK && pass
	result.Problems = append(result.Problems, r.Problems...)
	result.Warnings = append(result.Warnings, r.Warnings...)
	result.Rebuildable = append(result.Rebuildable, r.Rebuildable...)
	result.Wrote += r.Wrote
	result.Unchanged += r.Unchanged
	result.Deleted += r.Deleted
	result.Actions = append(result.Actions, r.Actions...)
}

// PrintLines writes problems as they are, then warnings prefixed "warning:".
func PrintLines(w io.Writer, problems, warnings []string) {
	for _, s := range problems {
		fmt.Fprintln(w, s)
	}
	for _, s := range warnings {
		fmt.Fprintln(w, "warning:", s)
	}
}

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

package codex

import (
	claudesource "github.com/rezzminator/engine-sync/internal/source"
)

func codexReporter(r *Result) claudesource.Reporter {
	return claudesource.Reporter{Problem: func(s string) { r.Problems = append(r.Problems, s) }, Warning: func(s string) { r.Warnings = append(r.Warnings, s) }, Dangling: func(path, rel string, err error) {
		if rel == "" {
			r.RecordDangling(path, err)
		} else {
			r.RecordUnresolved(path, rel, err)
		}
	}}
}

func discoverProjects(root string, cfg Config, r *Result, markers ...string) []string {
	if r.Source != nil {
		r.Problems = append(r.Problems, r.Source.Problems...)
		r.Warnings = append(r.Warnings, r.Source.Warnings...)
		return r.Source.Projects
	}
	return claudesource.Projects(root, cfg.Projects, cfg.ExcludeProjects, codexReporter(r), markers...)
}

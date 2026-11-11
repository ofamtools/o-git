package main

import (
	"slices"

	"ofam.dev/ochazuke"
)

func repoParameter() ochazuke.Parameter {
	return ochazuke.Parameter{Type: "path", Path: "dir", Free: true, From: "row:repo", Description: "repository directory or o-proj name; names remain literal selectors (default: the row's repository, then cwd)"}
}

func directoriesParameter() ochazuke.Parameter {
	return ochazuke.Parameter{Type: "path", Path: "dir", Description: "extra repository directories; a single chooser result or the legacy comma-separated list"}
}

func changedPathParameter() ochazuke.Parameter {
	return ochazuke.Parameter{Type: "path", Path: "file", New: true, From: "row:path", Description: "literal file path, relative to the actual repository root, or absolute within it; may be historical or deleted"}
}

// ListFacts infers JSON field types, not filesystem semantics. Refine those
// declarations in place, retaining its id key and label role, then regenerate
// the supported standard verbs. The preferred path precedes ancillary paths.
func (a *App) typePathProps() {
	for _, spec := range []struct {
		noun string
		files, dirs []string
		preferred string
	}{
		{noun: "repo", dirs: []string{"path"}, preferred: "path"},
		{noun: "worktree", dirs: []string{"path", "repo"}, preferred: "path"},
		{noun: "branch", dirs: []string{"repo", "worktree"}, preferred: "repo"},
		{noun: "commit", dirs: []string{"repo"}, preferred: "repo"},
		{noun: "change", files: []string{"path", "old_path"}, dirs: []string{"repo"}, preferred: "path"},
		{noun: "endpoint", dirs: []string{"repo", "path"}, preferred: "repo"},
		{noun: "comparison", dirs: []string{"repo"}, preferred: "repo"},
		{noun: "diff-file", files: []string{"path", "oldPath", "newPath"}, dirs: []string{"repo"}, preferred: "path"},
	} {
		n := a.r.Noun(spec.noun)
		for i := range n.Props {
			p := &n.Props[i]
			if slices.Contains(spec.files, p.Name) {
				p.Type, p.Path, p.New = "path", "file", true
			}
			if slices.Contains(spec.dirs, p.Name) {
				p.Type, p.Path = "path", "dir"
			}
		}
		slices.SortStableFunc(n.Props, func(x, y ochazuke.PropSpec) int {
			rank := func(p ochazuke.PropSpec) int {
				if p.Key { return 0 }
				if p.Name == spec.preferred { return 1 }
				return 2
			}
			return rank(x) - rank(y)
		})
		a.r.Define(spec.noun).Store(*n.Store)
	}
}

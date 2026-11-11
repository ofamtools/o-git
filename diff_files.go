package main

import (
	"fmt"

	"ofam.dev/ochazuke"
)

// DiffFileRow gives a structured diff file a stable subject and the same
// provenance as a change. OldPath/NewPath remain repository-relative facts.
type DiffFileRow struct {
	Repo         string `json:"repo"`
	Path         string `json:"path"`
	Source       string `json:"source"`
	Revision     string `json:"revision,omitempty"`
	BaseSource   string `json:"base_source"`
	BaseRevision string `json:"base_revision,omitempty"`
	DiffFile
}

func (a *App) registerDiffFileVerbs() {
	a.r.Define("diff-file").Columns(a.r.Noun("change").Columns...).
		Verb("list", a.diffFileList, "structured files in a comparison, including repository, old/new paths, hunks and endpoint provenance").Rows("diff-file").Fetch().Quiet().Positional("repo")
	op := a.r.Operations.At(ochazuke.Key(a.r.CanonicalKey("diff-file.list")))
	op.Parameters = map[string]ochazuke.Parameter{"repo": repoParameter(), "path": changedPathParameter()}
	for _, name := range []string{"pair", "from", "to"} {
		op.Parameters[name] = ochazuke.Parameter{Type: "string", Description: "comparison selector; use --pair or --from/--to"}
	}
	a.r.Operations.Set(op)
}

func (a *App) diffFileList(c ochazuke.Ctx) ochazuke.Result {
	pair, from, to, err := comparisonArgs(c)
	if err != nil {
		return ochazuke.Failed(err)
	}
	opt := a.diffOptions(c)
	opt.Plain = false
	return a.comparisonRead(listContext(c), func(r *EndpointResolver) (any, error) {
		rows := []ochazuke.ListItem{}
		for _, p := range r.Show(pair, from, to) {
			d, err := BuildDiff(r.ctx, p, opt)
			if err != nil {
				return nil, err
			}
			for _, file := range d.Files {
				path := file.NewPath
				if path == "" {
					path = file.OldPath
				}
				data := DiffFileRow{Repo: d.Comparison.Repo, Path: path, Source: p.To.Kind, Revision: p.To.SHA, BaseSource: p.From.Kind, BaseRevision: p.From.SHA, DiffFile: file}
				rows = append(rows, ochazuke.ListItem{ID: "diff-file/" + data.Repo + ":" + path, Noun: "diff-file", Label: path, Data: data, Cells: []string{file.Status, path, fmt.Sprint(file.Added), fmt.Sprint(file.Removed)}})
			}
		}
		return rows, nil
	})
}

package main

import (
	"fmt"
	"strings"

	"ofam.dev/ochazuke"
)

func (a *App) registerDiffVerbs() error {
	if err := declareDiffSettings(a.r); err != nil {
		return err
	}
	// The shared commit/change declarations live in registerListsVerbs;
	// configure their diff parameters here after those verbs exist.
	for _, key := range []string{"change.diff", "commit.diff"} {
		op := a.r.Operations[key]
		op.Parameters = map[string]ochazuke.Parameter{
			"repo":    {Type: "string", Description: "repository directory or o-proj name; default current repository"},
			"context": {Type: "integer", Description: "context lines; defaults to compare.diff.context"},
			"output":  {Type: "string", Enum: []string{"plain", "json"}, Description: "exact unified patch text (default plain), or structured JSON"},
		}
		if key == "change.diff" {
			op.Parameters["path"] = ochazuke.Parameter{Type: "string", Required: true, Description: "literal path relative to repository root (either rename path)"}
			for _, name := range []string{"pair", "from", "to"} {
				op.Parameters[name] = ochazuke.Parameter{Type: "string", Description: "comparison selector; use --pair or --from/--to"}
			}
		} else {
			op.Parameters["sha"] = ochazuke.Parameter{Type: "string", Required: true, Description: "commit SHA or ref"}
		}
		op.Result = "{comparison, reversed, files: diffview.File[]} or unified text"
		a.r.Operations[key] = op
	}
	return nil
}

func (a *App) diffOptions(c ochazuke.Ctx) DiffOptions {
	return DiffOptions{Path: ochazuke.StringArg(c.Args, "path", ""), Context: ochazuke.IntArg(c.Args, "context", a.r.Settings().Int("compare.diff.context")), MaxLines: a.r.Settings().Int("compare.diff.max-lines"), Plain: ochazuke.StringArg(c.Args, "output", "plain") == "plain"}
}

func diffOutput(results []DiffResult, plain bool) any {
	if plain {
		var text strings.Builder
		for _, d := range results {
			text.WriteString(d.Unified)
		}
		return ochazuke.Verbatim(text.String())
	}
	if len(results) == 1 {
		return results[0]
	}
	return results
}

func (a *App) changeDiff(c ochazuke.Ctx) ochazuke.Result {
	pair, from, to, err := comparisonArgs(c)
	if err != nil {
		return ochazuke.Failed(err)
	}
	opt := a.diffOptions(c)
	return a.comparisonRead(c, func(r *EndpointResolver) (any, error) {
		results := []DiffResult{}
		for _, p := range r.Show(pair, from, to) {
			d, err := BuildDiff(r.ctx, p, opt)
			if err != nil {
				return nil, err
			}
			results = append(results, d)
		}
		return diffOutput(results, opt.Plain), nil
	})
}

func (a *App) commitDiff(c ochazuke.Ctx) ochazuke.Result {
	sha, opt := ochazuke.StringArg(c.Args, "sha", ""), a.diffOptions(c)
	return a.comparisonRead(c, func(r *EndpointResolver) (any, error) {
		ends := r.Resolve(sha, "HEAD")
		results := []DiffResult{}
		for _, to := range ends {
			if !to.Available {
				p := makeComparison(r.dir, sha, sha, sha, to, to)
				results = append(results, DiffResult{Comparison: p, Files: []DiffFile{}})
				continue
			}
			if isMutable(to) || to.Kind == "empty" {
				return nil, fmt.Errorf("%s is not a commit", resolvedRef(to))
			}
			// Read stored parents rather than rev-list's shallow-boundary view:
			// a missing parent must not silently turn a shallow commit into a root.
			out, err := git(r.ctx, readTimeout, r.dir, "cat-file", "commit", to.SHA)
			if err != nil {
				return nil, err
			}
			headers, _, _ := strings.Cut(string(out), "\n\n")
			parent := ""
			for _, line := range strings.Split(headers, "\n") {
				if value, ok := strings.CutPrefix(line, "parent "); ok {
					parent = value
					break
				}
			}
			from := Endpoint{Kind: "commit", Available: true, Repo: r.dir}
			if parent != "" {
				from.SHA, from.Spec = parent, parent
			} else {
				out, err := git(r.ctx, readTimeout, r.dir, "hash-object", "-t", "tree", "/dev/null")
				if err != nil {
					return nil, err
				}
				from.Kind, from.SHA, from.Spec = "empty", strings.TrimSpace(string(out)), "(empty tree)"
			}
			p := makeComparison(r.dir, sha, from.Spec, sha, from, to)
			if parent == "" {
				p.State = "root commit: empty tree"
			}
			d, err := BuildDiff(r.ctx, p, opt)
			if err != nil {
				return nil, err
			}
			results = append(results, d)
		}
		return diffOutput(results, opt.Plain), nil
	})
}

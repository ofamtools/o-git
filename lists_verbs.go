package main

import (
	"fmt"
	"strings"

	"ofam.dev/ochazuke"
)

func (a *App) registerListsVerbs() {
	r := a.r
	r.Define("commit").Columns(commitColumns(r.Settings().Bool("compare.commits.split"))...).
		Verb("diff", a.commitDiff, "content diff of SHA against its first parent (empty tree for a root commit)").Fetch().Quiet().Positional("sha").
		Verb("list", a.commitList, "commits on each side in git's own order; section data or < / >; limit per side").Rows("commit").Fetch().Quiet().Positional("repo").
		Verb("show", a.commitShow, "full commit message, parents and decorations for SHA (default HEAD or the focused row)").Fetch().Quiet().Positional("sha", "repo").
		Verb("files", a.commitFiles, "changes against SHA's first parent, or the empty tree for a root commit").Rows("change").Fetch().Quiet().Positional("sha", "repo").
		Verb("copy", a.commitCopy, "copy the full SHA through the shared unnamed register").Quiet().Positional("sha", "repo")
	r.Define("change").Columns(ochazuke.Column{Title: "status"}, ochazuke.Column{Title: "path"}, ochazuke.Column{Title: "added"}, ochazuke.Column{Title: "removed"}).
		Verb("diff", a.changeDiff, "content diff of a literal repository-relative PATH; default pair is compare.default-pair").Fetch().Quiet().Positional("path").
		Verb("list", a.changeList, "changed paths and numstats; HEAD..WORKTREE has git status order and XY letters; ends with totals").Rows("change").Fetch().Quiet().Positional("repo").
		Verb("show", a.changeShow, "one changed PATH with status, rename/copy source and numstats").Fetch().Quiet().Positional("path", "repo").
		Verb("copy", a.changeCopy, "copy the changed path through the shared unnamed register").Quiet().Positional("path", "repo")
	r.Define("compare").Verb("commits", a.commitList, "commit.list for this comparison").Rows("commit").Fetch().Quiet().Positional("repo").
		Verb("files", a.changeList, "change.list for this comparison").Rows("change").Fetch().Quiet().Positional("repo")
	setHistory(r, ochazuke.HistoryAction, "commit.copy", "change.copy")
	for _, key := range []string{"commit.list", "commit.show", "commit.files", "commit.copy", "change.list", "change.show", "change.copy", "compare.commits", "compare.files"} {
		op := r.Operations[r.CanonicalKey(key)]
		op.Parameters = map[string]ochazuke.Parameter{"repo": {Type: "string", Description: "repository directory or o-proj name"}}
		if key == "commit.show" || key == "commit.files" || key == "commit.copy" {
			op.Parameters["sha"] = ochazuke.Parameter{Type: "string", Description: "commit ref or SHA; default focused commit or HEAD"}
		} else {
			for _, name := range []string{"pair", "from", "to"} {
				op.Parameters[name] = ochazuke.Parameter{Type: "string", Description: "comparison as in compare.show; --pair excludes --from/--to"}
			}
		}
		if key == "commit.list" || key == "compare.commits" {
			op.Parameters["limit"] = ochazuke.Parameter{Type: "integer", Description: "maximum commits per side; default compare.commits.limit"}
		}
		if key == "change.show" || key == "change.copy" {
			op.Parameters["path"] = ochazuke.Parameter{Type: "string", Description: "exact changed path; default focused change"}
		}
		switch key {
		case "commit.list", "change.list", "commit.files", "compare.commits", "compare.files":
			op.Result = "row[]"
		case "commit.copy", "change.copy":
			op.Result = "string"
		default:
			op.Result = strings.Split(key, ".")[0]
		}
		r.Operations[r.CanonicalKey(key)] = op
	}
}

func commitColumns(split bool) []ochazuke.Column {
	cols := []ochazuke.Column{{Title: "sha"}, {Title: "decorations"}, {Title: "subject"}, {Title: "author"}, {Title: "age"}, {Title: "parents"}}
	if !split {
		cols = append([]ochazuke.Column{{Title: "side"}}, cols...)
	}
	return cols
}

// Keep row repository inference local to these nouns without changing the
// core resolver's verb code (the parallel core lane owns that file).
func listContext(c ochazuke.Ctx) ochazuke.Ctx {
	if ochazuke.StringArg(c.Args, "repo", "") != "" || c.Instance == nil {
		return c
	}
	var repo string
	switch d := c.Instance.Data.(type) {
	case Commit:
		repo = d.Repo
	case Change:
		repo = d.Repo
	}
	if repo != "" {
		args := ochazuke.Arguments{}
		for k, v := range c.Args {
			args[k] = v
		}
		args["repo"] = repo
		c.Args = args
	}
	return c
}

func (a *App) listRead(c ochazuke.Ctx, work func(*EndpointResolver, Comparison) ([]ochazuke.ListItem, string, error)) ochazuke.Result {
	pair, from, to, err := comparisonArgs(c)
	if err != nil {
		return ochazuke.Failed(err)
	}
	var reasons []string
	return a.comparisonRead(listContext(c), func(r *EndpointResolver) (any, error) {
		rows := []ochazuke.ListItem{}
		for _, p := range r.Show(pair, from, to) {
			items, reason, err := work(r, p)
			if err != nil {
				return nil, err
			}
			if reason != "" {
				reasons = append(reasons, p.Name+": "+reason)
			}
			rows = append(rows, items...)
		}
		return rows, nil
	}).Then(func(c ochazuke.Ctx, _ any, err error) {
		if err == nil && len(reasons) > 0 {
			c.Info(strings.Join(reasons, "; "))
		}
	})
}

func (a *App) commitList(c ochazuke.Ctx) ochazuke.Result {
	s := a.r.Settings()
	limit, split := ochazuke.IntArg(c.Args, "limit", s.Int("compare.commits.limit")), s.Bool("compare.commits.split")
	a.r.Define("commit").Columns(commitColumns(split)...)
	if limit < 0 {
		return ochazuke.Failed(fmt.Errorf("commit limit must not be negative"))
	}
	return a.listRead(c, func(r *EndpointResolver, p Comparison) ([]ochazuke.ListItem, string, error) {
		list, err := ReadCommits(r.ctx, r.dir, p, limit, split, r.now)
		return list.Items, list.Reason, err
	})
}

func commitArg(c ochazuke.Ctx) string {
	if sha := ochazuke.StringArg(c.Args, "sha", ""); sha != "" {
		return sha
	}
	if row, ok := rowData[Commit](c); ok {
		return row.SHA
	}
	return "HEAD"
}

func (a *App) commitShow(c ochazuke.Ctx) ochazuke.Result {
	sha := commitArg(c)
	return a.comparisonRead(listContext(c), func(r *EndpointResolver) (any, error) {
		return ReadCommit(r.ctx, r.dir, sha, r.now)
	})
}

func (a *App) commitFiles(c ochazuke.Ctx) ochazuke.Result {
	sha := commitArg(c)
	findCopiesHarder := a.r.Settings().Bool("compare.files.find-copies-harder")
	return a.comparisonRead(listContext(c), func(r *EndpointResolver) (any, error) {
		commit, err := ReadCommit(r.ctx, r.dir, sha, r.now)
		if err != nil {
			return nil, err
		}
		var base string
		if len(commit.Parents) > 0 {
			base = commit.Parents[0]
		} else {
			out, err := git(r.ctx, readTimeout, r.dir, "hash-object", "-t", "tree", "--stdin")
			if err != nil {
				return nil, err
			}
			base = strings.TrimSpace(string(out))
		}
		files, err := readCommitChanges(r.ctx, r.dir, findCopiesHarder, base, commit.SHA)
		if err != nil {
			return nil, err
		}
		list := ChangeList{Files: files}
		list.count()
		return list.Items(r.dir), nil
	})
}

func (a *App) commitCopy(c ochazuke.Ctx) ochazuke.Result {
	sha := commitArg(c)
	return a.copyRead(listContext(c), func(r *EndpointResolver) (any, error) {
		commit, err := ReadCommit(r.ctx, r.dir, sha, r.now)
		return commit.SHA, err
	})
}

func (a *App) changeList(c ochazuke.Ctx) ochazuke.Result {
	findCopiesHarder := a.r.Settings().Bool("compare.files.find-copies-harder")
	return a.listRead(c, func(r *EndpointResolver, p Comparison) ([]ochazuke.ListItem, string, error) {
		list, err := ReadChanges(r.ctx, r.dir, p, findCopiesHarder)
		return list.Items(r.dir), list.Reason, err
	})
}

func changeArg(c ochazuke.Ctx) string {
	if path := ochazuke.StringArg(c.Args, "path", ""); path != "" {
		return path
	}
	if row, ok := rowData[Change](c); ok {
		return row.Path
	}
	return ""
}

func findChange(r *EndpointResolver, pair, from, to, path string, findCopiesHarder bool) (Change, error) {
	if path == "" {
		return Change{}, fmt.Errorf("give a changed path")
	}
	for _, p := range r.Show(pair, from, to) {
		list, err := ReadChanges(r.ctx, r.dir, p, findCopiesHarder)
		if err != nil {
			return Change{}, err
		}
		for _, file := range list.Files {
			if file.Path == path {
				return file, nil
			}
		}
	}
	return Change{}, fmt.Errorf("no changed path %q in this comparison", path)
}

func (a *App) changeShow(c ochazuke.Ctx) ochazuke.Result {
	pair, from, to, err := comparisonArgs(c)
	if err != nil {
		return ochazuke.Failed(err)
	}
	path := changeArg(c)
	findCopiesHarder := a.r.Settings().Bool("compare.files.find-copies-harder")
	return a.comparisonRead(listContext(c), func(r *EndpointResolver) (any, error) {
		return findChange(r, pair, from, to, path, findCopiesHarder)
	})
}

func (a *App) changeCopy(c ochazuke.Ctx) ochazuke.Result {
	pair, from, to, err := comparisonArgs(c)
	if err != nil {
		return ochazuke.Failed(err)
	}
	path := changeArg(c)
	findCopiesHarder := a.r.Settings().Bool("compare.files.find-copies-harder")
	return a.copyRead(listContext(c), func(r *EndpointResolver) (any, error) {
		file, err := findChange(r, pair, from, to, path, findCopiesHarder)
		return file.Path, err
	})
}

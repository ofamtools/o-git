package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/unixsuperhero/ochazuke"
	ocli "github.com/unixsuperhero/ochazuke/cli"
)

// The verbs. Every one that reads or changes a repository is pending work,
// so git runs off the event loop. The reads are Quiet (no jobs ticker
// entry); the mutations show on the ticker and print their
// "job #N VERB done" line on the CLI's stderr. The destructive verbs take plain
// arguments (worktree remove PATH, branch delete NAME), which the TUI's rows
// fill from the focused row (FromRow), or act on the row --target names (a
// host's row id); the other row verbs act on the row they target (the
// focused row, or --target ID on the command line); a REPO word is a
// directory or a name o-proj lists, and defaults to the row's repository,
// then the one the TUI shows, then the current directory.

func (a *App) registerVerbs() {
	r := a.r
	r.Define("repo").
		Verb("list", a.repoList, "every repository o-proj lists, plus --path directories, with its status: rows {noun, id: repo/DIR, label, data: {name, path, state, branch, head, upstream, ahead, behind, staged, unstaged, untracked, conflicts}, cells}").Rows("repo").Fetch().Quiet().Positional("path").
		Verb("status", a.repoStatus, "one repository's status: {name, path, state, branch, head, detached, upstream, gone, ahead, behind, staged, unstaged, untracked, conflicts}").Fetch().Quiet().Positional("repo").
		Verb("show", a.repoShow, "one repository's status, worktrees and branches: {repo, worktrees, branches}; the TUI shows them and moves to its worktrees").Fetch().Quiet().Positional("repo").
		Verb("state", a.repoState, "every list's rows in one answer, JSON Lines {noun, id, label, data, cells}: each repository, then every repository's worktrees, then its branches (the catalog's state)").State().Fetch().Quiet().Positional("path")
	r.Define("worktree").
		Verb("list", a.worktreeList, "a repository's worktrees, the main one first: rows {noun, id: worktree/PATH, label, data: {repo, path, head, branch, detached, bare, main, locked, prunable, state, changes}, cells}").Rows("worktree").Fetch().Quiet().Positional("repo").
		Verb("add", a.worktreeAdd, "add a worktree for BRANCH (created from --from, else HEAD, when it does not exist) at PATH, default REPO-BRANCH beside the main worktree").Positional("branch", "path").
		Verb("remove", a.worktreeRemove, "remove the worktree at PATH; refuses the main one, and a dirty one unless --force").Confirm("Remove worktree {{label}}?").Positional("path")
	r.Define("branch").
		Verb("list", a.branchList, "a repository's local branches, newest commit first: rows {noun, id: branch/REPO:NAME, label, data: {repo, name, current, worktree, upstream, gone, ahead, behind, merged, commit, subject, author, date}, cells}").Rows("branch").Fetch().Quiet().Positional("repo").
		Verb("create", a.branchCreate, "create branch NAME at FROM (default HEAD) without checking it out; --switch checks it out").Positional("name", "from").
		Verb("switch", a.branchSwitch, "check out NAME (default: the targeted branch) in REPO's worktree").Positional("name").
		Verb("delete", a.branchDelete, "delete branch NAME in REPO; refuses the checked-out one, and an unmerged one unless --force").Confirm("Delete branch {{label}}?").Positional("name")
	setHistory(r, ochazuke.HistoryChange, "worktree.add", "worktree.remove", "branch.create", "branch.switch", "branch.delete")

	repo := ochazuke.Parameter{Type: "string", Description: "a repository directory or a name o-proj lists (default: the row's, else the current directory)"}
	paths := ochazuke.Parameter{Type: "string", Description: "extra repository directories, comma-separated, listed after o-proj's"}
	force := func(what string) ochazuke.Parameter {
		return ochazuke.Parameter{Type: "boolean", Description: what}
	}
	set := func(key, result string, params map[string]ochazuke.Parameter) {
		op := r.Operations[r.CanonicalKey(key)]
		op.Parameters, op.Result = params, result
		r.Operations[r.CanonicalKey(key)] = op
	}
	set("repo.list", "row[]", map[string]ochazuke.Parameter{"path": paths})
	set("repo.status", "repo", map[string]ochazuke.Parameter{"repo": repo})
	set("repo.show", "{repo, worktrees, branches}", map[string]ochazuke.Parameter{"repo": repo})
	set("repo.state", "JSON Lines of rows", map[string]ochazuke.Parameter{"path": paths})
	set("worktree.list", "row[]", map[string]ochazuke.Parameter{"repo": repo})
	set("worktree.add", "{repo, path, branch, created}", map[string]ochazuke.Parameter{
		"repo":   repo,
		"branch": {Type: "string", Description: "the branch to check out; a new one is created when it does not exist (the TUI asks)"},
		"path":   {Type: "string", Description: "the new worktree's directory (default: REPO-BRANCH beside the main worktree)"},
		"from":   {Type: "string", Description: "the commit a new branch starts at (default HEAD)"},
	})
	set("worktree.remove", "{repo, removed, force}", map[string]ochazuke.Parameter{
		"path":  {Type: "string", Description: "the worktree's directory (default: the --target row's; the TUI passes the focused row's)"},
		"force": force("remove it even with uncommitted changes or untracked files"),
	})
	set("branch.list", "row[]", map[string]ochazuke.Parameter{"repo": repo})
	set("branch.create", "{repo, branch, from, switched}", map[string]ochazuke.Parameter{
		"repo":   repo,
		"name":   {Type: "string", Description: "the new branch (the TUI asks)"},
		"from":   {Type: "string", Description: "the commit it starts at (default HEAD)"},
		"switch": force("check the new branch out in REPO's worktree"),
	})
	set("branch.switch", "{repo, branch}", map[string]ochazuke.Parameter{
		"repo": repo,
		"name": {Type: "string", Description: "the branch (default: the targeted row's)"},
	})
	set("branch.delete", "{repo, deleted, force}", map[string]ochazuke.Parameter{
		"repo":  repo,
		"name":  {Type: "string", Description: "the branch (default: the --target row's; the TUI passes the focused row's)"},
		"force": force("delete it even when it is not merged"),
	})
}

// repoWord is the REPO a worktree or branch verb acts in: --repo, else the
// targeted row's repository, else the one the TUI shows, else "" (the
// current directory).
func (a *App) repoWord(c ochazuke.Ctx) string {
	if word := ochazuke.StringArg(c.Args, "repo", ""); word != "" {
		return word
	}
	if c.Instance != nil {
		switch d := c.Instance.Data.(type) {
		case Repo:
			return d.Path
		case WorktreeRow:
			if d.Repo != "" {
				return d.Repo
			}
			return d.Path
		case BranchRow:
			return d.Repo
		}
	}
	return a.current
}

// splitPaths is a comma-separated --path value.
func splitPaths(s string) []string {
	var out []string
	for _, p := range strings.Split(s, ",") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

func (a *App) repoList(c ochazuke.Ctx) ochazuke.Result {
	extra := append(append([]string{}, a.Paths...), splitPaths(ochazuke.StringArg(c.Args, "path", ""))...)
	return ochazuke.Pending(func(ctx context.Context) (any, error) {
		repos, err := listRepos(ctx, extra)
		if err != nil {
			return nil, err
		}
		return rowsOf(repoItems(repos)), nil
	}).Replacing("repos").Then(func(_ ochazuke.Ctx, v any, err error) {
		a.listErr = err
		if err == nil {
			a.setRepos(v.([]ocli.Row))
		}
	})
}

func (a *App) repoStatus(c ochazuke.Ctx) ochazuke.Result {
	word := a.repoWord(c)
	return ochazuke.Pending(func(ctx context.Context) (any, error) {
		name, dir, err := resolve(ctx, word)
		if err != nil {
			return nil, err
		}
		return inspect(ctx, name, dir), nil
	})
}

func (a *App) repoShow(c ochazuke.Ctx) ochazuke.Result {
	name := ""
	if r, ok := rowData[Repo](c); ok && ochazuke.StringArg(c.Args, "repo", "") == "" {
		name = r.Name
	}
	return a.show(a.repoWord(c), name, true)
}

// show reads one repository's detail and, on the loop, makes it the one
// the worktree and branch lists show; focus moves to its worktrees. name,
// when set, is the repo row's name (o-proj's), else the directory's.
func (a *App) show(word, name string, focus bool) ochazuke.Result {
	a.loading = word
	return ochazuke.Pending(func(ctx context.Context) (any, error) {
		base, dir, err := resolve(ctx, word)
		if err != nil {
			return nil, err
		}
		if name == "" {
			name = base
		}
		return detail(ctx, name, dir)
	}).Replacing("show").Then(func(_ ochazuke.Ctx, v any, err error) {
		if a.loading == word {
			a.loading = ""
		}
		if err == nil {
			a.setDetail(v.(RepoDetail), focus)
		}
	})
}

func (a *App) repoState(c ochazuke.Ctx) ochazuke.Result {
	extra := append(append([]string{}, a.Paths...), splitPaths(ochazuke.StringArg(c.Args, "path", ""))...)
	now := a.Now()
	return ochazuke.Pending(func(ctx context.Context) (any, error) {
		repos, err := listRepos(ctx, extra)
		if err != nil {
			return nil, err
		}
		details := make([]RepoDetail, len(repos))
		parallel(len(repos), func(i int) {
			if repos[i].Error == "" {
				details[i], _ = detail(ctx, repos[i].Name, repos[i].Path)
			}
		})
		rows := rowsOf(repoItems(repos))
		for _, d := range details {
			rows = append(rows, rowsOf(worktreeItems(d.Worktrees))...)
		}
		for _, d := range details {
			rows = append(rows, rowsOf(branchItems(d.Branches, now))...)
		}
		return lines(rows), ctx.Err()
	})
}

func (a *App) worktreeList(c ochazuke.Ctx) ochazuke.Result {
	word := a.repoWord(c)
	return ochazuke.Pending(func(ctx context.Context) (any, error) {
		_, dir, err := resolve(ctx, word)
		if err != nil {
			return nil, err
		}
		list, err := listWorktreeRows(ctx, dir)
		if err != nil {
			return nil, err
		}
		return rowsOf(worktreeItems(list)), nil
	}).Then(func(_ ochazuke.Ctx, v any, err error) {
		if err == nil {
			_ = a.worktrees.SetItems(items(v.([]ocli.Row)))
		}
	})
}

func (a *App) branchList(c ochazuke.Ctx) ochazuke.Result {
	word := a.repoWord(c)
	now := a.Now()
	return ochazuke.Pending(func(ctx context.Context) (any, error) {
		_, dir, err := resolve(ctx, word)
		if err != nil {
			return nil, err
		}
		list, err := listBranchRows(ctx, dir)
		if err != nil {
			return nil, err
		}
		return rowsOf(branchItems(list, now)), nil
	}).Then(func(_ ochazuke.Ctx, v any, err error) {
		if err == nil {
			_ = a.branches.SetItems(items(v.([]ocli.Row)))
		}
	})
}

// changed is the Then of a verb that changed a repository: the TUI reads
// it again and says what happened.
func (a *App) changed(say func(any) string) func(ochazuke.Ctx, any, error) {
	return func(ctx ochazuke.Ctx, v any, err error) {
		if err != nil || !a.tui {
			return
		}
		ctx.Info(say(v))
		a.refresh()
	}
}

// AddResult is worktree add's answer.
type AddResult struct {
	Repo    string `json:"repo"`
	Path    string `json:"path"`
	Branch  string `json:"branch"`
	Created bool   `json:"created"`
}

func (a *App) worktreeAdd(c ochazuke.Ctx) ochazuke.Result {
	branch := ochazuke.StringArg(c.Args, "branch", "")
	if branch == "" {
		if a.tui {
			return a.prompt(c, "New worktree for branch", "worktree", "add", "branch")
		}
		return ochazuke.Failed(errors.New("a branch is required: worktree add BRANCH [PATH]"))
	}
	word, path, from := a.repoWord(c), ochazuke.StringArg(c.Args, "path", ""), ochazuke.StringArg(c.Args, "from", "")
	if path != "" {
		path = absPath(path)
	}
	return ochazuke.Pending(func(ctx context.Context) (any, error) {
		_, dir, err := resolve(ctx, word)
		if err != nil {
			return nil, err
		}
		main, err := mainWorktree(ctx, dir)
		if err != nil {
			return nil, err
		}
		if path == "" {
			path = defaultWorktreePath(main, branch)
		}
		if _, err := os.Stat(path); err == nil {
			return nil, fmt.Errorf("%s exists; pass another path", tilde(path))
		}
		created, err := AddWorktree(ctx, main, path, branch, from)
		if err != nil {
			return nil, err
		}
		return AddResult{Repo: main, Path: path, Branch: branch, Created: created}, nil
	}).Then(a.changed(func(v any) string {
		res := v.(AddResult)
		return "added worktree " + tilde(res.Path) + " on " + res.Branch
	}))
}

// RemoveResult is worktree remove's answer.
type RemoveResult struct {
	Repo    string `json:"repo"`
	Removed string `json:"removed"`
	Force   bool   `json:"force"`
}

func (a *App) worktreeRemove(c ochazuke.Ctx) ochazuke.Result {
	w, ok := rowData[WorktreeRow](c)
	if path := ochazuke.StringArg(c.Args, "path", ""); path != "" {
		w, ok = WorktreeRow{Worktree: Worktree{Path: absPath(path)}}, true
	}
	if !ok {
		return ochazuke.Failed(errors.New("a worktree is required: worktree remove PATH (worktree list prints them)"))
	}
	force, _ := c.Args["force"].(bool)
	return ochazuke.Pending(func(ctx context.Context) (any, error) {
		main, err := mainWorktree(ctx, w.Path)
		if err != nil {
			return nil, err
		}
		if samePath(main, w.Path) {
			return nil, fmt.Errorf("%s is the main worktree; only a linked worktree can be removed", tilde(w.Path))
		}
		if _, err := os.Stat(w.Path); err == nil && !force {
			s, err := ReadStatus(ctx, w.Path)
			if err != nil {
				return nil, err
			}
			if s.Dirty() {
				return nil, fmt.Errorf("worktree %s has changes (%s); --force removes it anyway", tilde(w.Path), changesText(s))
			}
		}
		if err := RemoveWorktree(ctx, main, w.Path, force); err != nil {
			return nil, err
		}
		return RemoveResult{Repo: main, Removed: w.Path, Force: force}, nil
	}).Then(a.changed(func(v any) string { return "removed worktree " + tilde(v.(RemoveResult).Removed) }))
}

// BranchResult is the answer of branch create, switch and delete.
type BranchResult struct {
	Repo     string `json:"repo"`
	Branch   string `json:"branch"`
	From     string `json:"from,omitempty"`
	Switched bool   `json:"switched,omitempty"`
	Deleted  bool   `json:"deleted,omitempty"`
	Force    bool   `json:"force,omitempty"`
}

func (a *App) branchCreate(c ochazuke.Ctx) ochazuke.Result {
	name := ochazuke.StringArg(c.Args, "name", "")
	if name == "" {
		if a.tui {
			return a.prompt(c, "New branch", "branch", "create", "name")
		}
		return ochazuke.Failed(errors.New("a name is required: branch create NAME [FROM]"))
	}
	word, from := a.repoWord(c), ochazuke.StringArg(c.Args, "from", "")
	switchTo, _ := c.Args["switch"].(bool)
	return ochazuke.Pending(func(ctx context.Context) (any, error) {
		_, dir, err := resolve(ctx, word)
		if err != nil {
			return nil, err
		}
		if err := CreateBranch(ctx, dir, name, from); err != nil {
			return nil, err
		}
		res := BranchResult{Repo: dir, Branch: name, From: from}
		if switchTo {
			if err := SwitchBranch(ctx, dir, name); err != nil {
				return nil, fmt.Errorf("created %s, but: %w", name, err)
			}
			res.Switched = true
		}
		return res, nil
	}).Then(a.changed(func(v any) string { return "created branch " + v.(BranchResult).Branch }))
}

func (a *App) branchSwitch(c ochazuke.Ctx) ochazuke.Result {
	name := ochazuke.StringArg(c.Args, "name", "")
	word := ochazuke.StringArg(c.Args, "repo", "")
	if b, ok := rowData[BranchRow](c); ok && name == "" {
		name = b.Name
		if word == "" {
			word = b.Repo
		}
	}
	if word == "" {
		word = a.repoWord(c)
	}
	if name == "" {
		return ochazuke.Failed(errors.New("a branch is required: branch switch NAME [--repo REPO]"))
	}
	return ochazuke.Pending(func(ctx context.Context) (any, error) {
		_, dir, err := resolve(ctx, word)
		if err != nil {
			return nil, err
		}
		if err := SwitchBranch(ctx, dir, name); err != nil {
			return nil, err
		}
		return BranchResult{Repo: dir, Branch: name, Switched: true}, nil
	}).Then(a.changed(func(v any) string { return "switched to " + v.(BranchResult).Branch }))
}

func (a *App) branchDelete(c ochazuke.Ctx) ochazuke.Result {
	b, ok := rowData[BranchRow](c)
	if name := ochazuke.StringArg(c.Args, "name", ""); name != "" {
		b, ok = BranchRow{Repo: a.repoWord(c), Branch: Branch{Name: name}}, true
	}
	if !ok {
		return ochazuke.Failed(errors.New("a branch is required: branch delete NAME [--repo REPO]"))
	}
	force, _ := c.Args["force"].(bool)
	return ochazuke.Pending(func(ctx context.Context) (any, error) {
		_, dir, err := resolve(ctx, b.Repo)
		if err != nil {
			return nil, err
		}
		if s, err := ReadStatus(ctx, dir); err == nil && s.Branch == b.Name {
			return nil, fmt.Errorf("branch %s is checked out in %s; switch to another branch first", b.Name, tilde(dir))
		}
		if err := DeleteBranch(ctx, dir, b.Name, force); err != nil {
			return nil, err
		}
		return BranchResult{Repo: dir, Branch: b.Name, Deleted: true, Force: force}, nil
	}).Then(a.changed(func(v any) string { return "deleted branch " + v.(BranchResult).Branch }))
}

// rowData is the targeted row's record when it is a T.
func rowData[T any](c ochazuke.Ctx) (T, bool) {
	var zero T
	if c.Instance == nil {
		return zero, false
	}
	d, ok := c.Instance.Data.(T)
	return d, ok
}

// setHistory declares the history kind (Operation.HistoryKind) of each
// named verb, so none is left to the default: NounBuilder.Fetch marks a
// read where it is declared, and the builder has no method for a change or
// an action. An unknown key panics, so a renamed verb is renamed here too.
func setHistory(r *ochazuke.Runtime, kind string, keys ...string) {
	for _, key := range keys {
		op, ok := r.Operations[r.CanonicalKey(key)]
		if !ok {
			panic("setHistory: no operation " + key)
		}
		op.HistoryKind = kind
		r.Operations[r.CanonicalKey(key)] = op
	}
}

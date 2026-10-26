package main

import "github.com/unixsuperhero/ochazuke"

// bind installs the key rows, following the family key map
// (~/hub/o-plans/plans/mtm-keys-and-ux-review-2.md, "Key map"): n is new,
// d d removes or deletes (through the verb's confirm), d D does it with
// force, enter on a branch switches to it. On a repo row the stack is
// [repo row, the repo list, git, app]; on a worktree or branch row it is
// [row, its list, git, app]. The lists' own rows (n) answer on an empty
// list too. j/k move in a list and spill into the next one; } { and tab
// move between the lists (the app's focus rows). R reloads by hand until
// the page refreshes itself.
// The worktree and branch rows pass the row's fields as the verb's
// arguments (FromRow), so a key runs the same request the command line
// spells out: worktree remove PATH, branch delete NAME --repo REPO.
func (a *App) bind() error {
	branchRow := func(b ochazuke.Binding) ochazuke.Binding {
		return b.FromRow("name", "name").FromRow("repo", "repo")
	}
	r := a.r
	r.Define("git").Mode("normal").
		Bind("R", ochazuke.Do("repo.list")).
		Bind("q", ochazuke.Do("app.quit"))
	r.Define("repo").Mode("normal").
		Bind("enter", ochazuke.Do("repo.show"))
	r.Define("worktree").Mode("normal").
		Bind("d d", ochazuke.Do("worktree.remove").FromRow("path", "path")).
		Bind("d D", ochazuke.Do("worktree.remove", ochazuke.Arguments{"force": true}).FromRow("path", "path").Hinted("force remove"))
	r.Define("branch").Mode("normal").
		Bind("enter", branchRow(ochazuke.Do("branch.switch"))).
		Bind("d d", branchRow(ochazuke.Do("branch.delete"))).
		Bind("d D", branchRow(ochazuke.Do("branch.delete", ochazuke.Arguments{"force": true})).Hinted("force delete"))
	add := func(list, op string) ochazuke.Binding {
		row := ochazuke.Do(op)
		row.Instance, row.Mode, row.Keys = list, "normal", "n"
		return row
	}
	return r.Bind(add(worktreesID, "worktree.add"), add(branchesID, "branch.create"))
}

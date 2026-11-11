package main

import "ofam.dev/ochazuke"

// bind installs the key rows, following the family key map: n is new,
// d d removes or deletes (through the verb's confirm), d D does it with
// force, enter on a branch switches to it. On a repo row the stack is
// [repo row, the repo list, git, app]; on a worktree or branch row it is
// [row, its list, git, app]. The lists' own rows (n) answer on an empty
// list too. j/k move in a list and spill into the next one; } { and tab
// move between the lists (the app's focus rows). R reloads by hand until
// the page refreshes itself.
// The worktree and branch verbs declare the focused row's fields as their
// parameters' sources (Parameter.From), so a key, the help and the palette
// run the same request the command line spells out: worktree remove PATH,
// branch delete NAME --repo REPO.
func (a *App) bind() error {
	r := a.r
	r.Define("git-page").Mode("normal").
		Bind("R", ochazuke.Do("repo.list")).
		Bind("q", ochazuke.Do("app.quit"))
	r.Define("repo").Mode("normal").
		Bind("enter", ochazuke.Do("repo.inspect"))
	r.Define("worktree").Mode("normal").
		Bind("d d", ochazuke.Do("worktree.remove")).
		Bind("d D", ochazuke.Do("worktree.remove", ochazuke.Arguments{"force": true}).Hinted("force remove"))
	r.Define("branch").Mode("normal").
		Bind("enter", ochazuke.Do("branch.switch")).
		Bind("d d", ochazuke.Do("branch.delete")).
		Bind("d D", ochazuke.Do("branch.delete", ochazuke.Arguments{"force": true}).Hinted("force delete"))
	add := func(list, op string) ochazuke.Binding {
		row := ochazuke.Do(op)
		row.Instance, row.Mode, row.Keys = list, "normal", "n"
		return row
	}
	return r.Bind(add(worktreesID, "worktree.add"), add(branchesID, "branch.create"))
}

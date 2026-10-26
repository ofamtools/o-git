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
func (a *App) bind() error {
	r := a.r
	r.Define("git").Mode("normal").
		Bind("R", ochazuke.Do("repo.list")).
		Bind("q", ochazuke.Do("app.quit"))
	// Nothing here acts on marked rows: space would run the list's
	// list.mark.
	ignore := ochazuke.Do("key.ignore").Hide()
	r.Define("repo").Mode("normal").
		Bind("enter", ochazuke.Do("repo.show")).
		Bind("space", ignore)
	r.Define("worktree").Mode("normal").
		Bind("space", ignore).
		Bind("d d", ochazuke.Do("worktree.remove")).
		Bind("d D", ochazuke.Do("worktree.remove", ochazuke.Arguments{"force": true}).Hinted("force remove"))
	r.Define("branch").Mode("normal").
		Bind("space", ignore).
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

package main

import "github.com/unixsuperhero/ochazuke"

// bind installs the key rows. On a repo row the stack is [repo row, the
// repo list, git, app]; on a worktree or branch row it is [row, its list,
// git, app]. The lists' own rows (a: add) answer on an empty list too.
// j/k move in a list and spill into the next one; } { and tab move
// between the lists (the app's focus rows).
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
		Bind("x", ochazuke.Do("worktree.remove")).
		Bind("X", ochazuke.Do("worktree.remove", ochazuke.Arguments{"force": true}).Hinted("force remove"))
	r.Define("branch").Mode("normal").
		Bind("space", ignore).
		Bind("s", ochazuke.Do("branch.switch")).
		Bind("x", ochazuke.Do("branch.delete")).
		Bind("X", ochazuke.Do("branch.delete", ochazuke.Arguments{"force": true}).Hinted("force delete"))
	add := func(list, op string) ochazuke.Binding {
		row := ochazuke.Do(op)
		row.Instance, row.Mode, row.Keys = list, "normal", "a"
		return row
	}
	return r.Bind(add(worktreesID, "worktree.add"), add(branchesID, "branch.create"))
}

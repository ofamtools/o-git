package main

import (
	"io"
	"os"
	"time"

	"github.com/unixsuperhero/ochazuke"
	ocli "github.com/unixsuperhero/ochazuke/cli"
)

// The application: one ochazuke runtime holding the three list nouns and
// their verbs, used by the command line (the shell runs one verb and
// settles it) and by the TUI. The runtime holds interaction state only;
// what o-git read from git is the lists' rows and the fields below.

const (
	pageID      = "git"
	reposID     = "git/repos"
	worktreesID = "git/worktrees"
	branchesID  = "git/branches"
)

// Version is the catalog's version.
const Version = "0.1.0"

// App is o-git's state.
type App struct {
	r *ochazuke.Runtime
	m *ochazuke.Model

	repos, worktrees, branches *ochazuke.List

	// Paths are extra repository directories the repo list shows beside
	// o-proj's (the TUI's --path flags).
	Paths []string
	// Now is the clock branch ages read.
	Now func() time.Time

	// The repository the worktree and branch lists show: its name and
	// toplevel, "" before the first is read.
	current, currentName string
	shown                *Repo // its status as last read
	loading              string
	listErr              error  // the last repo list's failure
	following            string // the repo row the lists last followed
	tui                  bool   // a person is at the keys: prompts, and reads after a change
	ask                  *question
	field                *ochazuke.Input

	Stdin          io.Reader
	Stdout, Stderr io.Writer
}

// New builds the runtime: the Git page holding the three lists, the nouns
// with their columns, verbs and bindings, the prompt and the model. The
// command line uses the same runtime (its catalog lists the page and the
// key rows). The lists start empty.
func New() (*App, error) {
	r := ochazuke.NewRuntime()
	a := &App{r: r, Now: time.Now, Stdin: os.Stdin, Stdout: os.Stdout, Stderr: os.Stderr}
	r.Instance(pageID, ochazuke.Kind("git"))
	var err error
	if a.repos, err = r.AddList(reposID, nil, ochazuke.Parent(pageID), ochazuke.Columns(columns["repo"]...), ochazuke.NoMarks()); err != nil {
		return nil, err
	}
	if a.worktrees, err = r.AddList(worktreesID, nil, ochazuke.Parent(pageID), ochazuke.Columns(columns["worktree"]...), ochazuke.NoMarks()); err != nil {
		return nil, err
	}
	if a.branches, err = r.AddList(branchesID, nil, ochazuke.Parent(pageID), ochazuke.Columns(columns["branch"]...), ochazuke.NoMarks()); err != nil {
		return nil, err
	}
	for noun, cols := range columns {
		r.Define(noun).Columns(cols...)
	}
	a.registerVerbs()
	a.m = ochazuke.NewModel(r)
	a.m.Title = "o-git"
	a.registerPrompt()
	if err := a.bind(); err != nil {
		return nil, err
	}
	// The page is declared as a data page (dataPage), so a host's
	// catalog draws its lists; o-git's own TUI keeps drawing it itself.
	if err := ocli.DeclarePage(r, dataPage()); err != nil {
		return nil, err
	}
	page := r.PageOf(pageID)
	page.Render, page.Focus, page.Remembered = a.render, reposID, reposID
	if err := r.OpenPage(pageID); err != nil {
		return nil, err
	}
	return a, nil
}

// Shell is the NOUN VERB command line over the runtime.
func (a *App) Shell() *ocli.Shell {
	sh := ocli.New(a.r, "o-git", a.Stdin, a.Stdout, a.Stderr)
	sh.Usage = ocli.DefaultUsage("o-git")
	sh.Version = Version
	// A --target names a row no list holds yet: make it exist from its
	// id alone; the verb reads the repository itself.
	sh.Reveal = func(target string) error {
		item, err := revealed(target)
		if err != nil {
			return err
		}
		return a.listOf(item.Noun).SetItems([]ochazuke.ListItem{item})
	}
	return sh
}

func (a *App) listOf(noun string) *ochazuke.List {
	switch noun {
	case "repo":
		return a.repos
	case "worktree":
		return a.worktrees
	}
	return a.branches
}

// items turns a list verb's rows back into list items.
func items(rows []ocli.Row) []ochazuke.ListItem {
	out := make([]ochazuke.ListItem, len(rows))
	for i, r := range rows {
		out[i] = ochazuke.ListItem{ID: r.ID, Label: r.Label, Noun: r.Noun, Data: r.Data, Cells: r.Cells}
	}
	return out
}

// setRepos fills the repo list, keeping the selection by id; the lists
// then follow the selected repository again (it may have changed).
func (a *App) setRepos(rows []ocli.Row) {
	_ = a.repos.SetItems(items(rows))
	a.following = ""
}

// setDetail makes a repository the one the worktree and branch lists show.
func (a *App) setDetail(d RepoDetail, focus bool) {
	repo := d.Repo
	a.current, a.currentName, a.shown = repo.Path, repo.Name, &repo
	_ = a.worktrees.SetItems(worktreeItems(d.Worktrees))
	_ = a.branches.SetItems(branchItems(d.Branches, a.Now()))
	if focus {
		_ = a.worktrees.Focus()
	}
}

// follow makes the worktree and branch lists show the selected repo row's
// repository when the selection moved (any motion, a search, a reload):
// one replacing read off the loop. It reports whether it started one.
func (a *App) follow() bool {
	item, ok := a.repos.Selected()
	if !ok || item.ID == a.following {
		return false
	}
	a.following = item.ID
	repo, _ := item.Data.(Repo)
	return a.launch(reposID, "repo.show", a.show(repo.Path, repo.Name, false))
}

// refresh reads the repositories and the shown one again (after a change).
func (a *App) refresh() {
	a.launch(reposID, "repo.list", a.repoList(ochazuke.Ctx{Args: ochazuke.Arguments{}}))
	if a.current != "" {
		a.launch(reposID, "repo.show", a.show(a.current, a.currentName, false))
	}
}

// launch starts pending work owned by an instance, outside a key's verb.
func (a *App) launch(owner, verb string, res ochazuke.Result) bool {
	_, err := a.r.Launch(owner, verb, res, ochazuke.Ctx{Runtime: a.r, Context: a.r.Context(), Args: ochazuke.Arguments{}, Instance: a.r.Instances[owner]})
	if err != nil {
		a.r.UI().Status(err.Error())
	}
	return err == nil
}

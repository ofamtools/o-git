package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"github.com/unixsuperhero/ochazuke"
	"github.com/unixsuperhero/ochazuke/keytest"
)

// The page's tests send keys through the runtime's stack walk (what the
// model does with a key press) and settle the reads they start on the
// test's goroutine, the lists following the selection as the model does
// after every message.

// open builds the page over the directories, read and settled.
func open(t *testing.T, paths ...string) *App {
	t.Helper()
	isolate(t)
	a, err := New()
	if err != nil {
		t.Fatal(err)
	}
	a.Paths, a.tui = paths, true
	a.m.Width, a.m.Height = 100, 30
	if _, err := a.r.Execute(ochazuke.Request{Noun: "repo", Verb: "list", Target: pageID}); err != nil {
		t.Fatal(err)
	}
	if err := a.Settle(); err != nil {
		t.Fatal(err)
	}
	return a
}

// keys sends keys and settles; a key's error or a failed job fails the test.
func keys(t *testing.T, a *App, names ...string) {
	t.Helper()
	for _, name := range names {
		if err := keyErr(a, name); err != nil {
			t.Fatalf("key %q: %v", name, err)
		}
	}
}

// keyErr sends one key and settles, returning the key's or a job's error.
func keyErr(a *App, name string) error {
	if _, err := a.r.Dispatch(ochazuke.MacroKey{Key: name}); err != nil {
		return err
	}
	return a.Settle()
}

// typeText types into the open prompt.
func typeText(t *testing.T, a *App, text string) {
	t.Helper()
	for _, r := range text {
		keys(t, a, string(r))
	}
}

func screen(a *App) string { return ansi.Strip(a.m.View().Content) }

// focusRow puts the focus on a list's row.
func focusRow(t *testing.T, a *App, list *ochazuke.List, id string) {
	t.Helper()
	if err := list.Select(id); err != nil {
		t.Fatal(err)
	}
	if err := list.Focus(); err != nil {
		t.Fatal(err)
	}
	if item, ok := list.Selected(); !ok || item.ID != id {
		t.Fatalf("selected %+v, want %s", item, id)
	}
}

func TestPageFollowsTheSelectedRepo(t *testing.T) {
	one := newRepo(t, "one")
	two := newRepo(t, "two")
	sh(t, two, "branch", "side")
	a := open(t, one, two)
	if a.current != one || a.worktrees.Len() != 1 || a.branches.Len() != 1 {
		t.Fatalf("first repo: %s %d %d", a.current, a.worktrees.Len(), a.branches.Len())
	}
	if s := screen(a); !strings.Contains(s, "Repos 2") || !strings.Contains(s, "Worktrees one") || !strings.Contains(s, "repo      one") {
		t.Fatalf("screen\n%s", s)
	}
	keys(t, a, "j")
	if a.current != two || a.branches.Len() != 2 {
		t.Fatalf("after j: %s %d", a.current, a.branches.Len())
	}
	if s := screen(a); !strings.Contains(s, "Branches two") || !strings.Contains(s, "side") {
		t.Fatalf("screen\n%s", s)
	}
	// enter moves to the repo's worktrees.
	keys(t, a, "enter")
	if !a.worktrees.Focused() {
		t.Fatalf("focus %s", a.r.Focus)
	}
	if s := screen(a); !strings.Contains(s, "state     clean main") {
		t.Fatalf("worktree detail\n%s", s)
	}

	// The model follows after every message: a motion starts the read.
	m := &model{a: a}
	keys(t, a, "shift+tab")
	if !a.repos.Focused() {
		t.Fatalf("focus %s", a.r.Focus)
	}
	if _, cmd := m.Update(keytest.Msg("k", false, false)); cmd == nil || a.loading != one {
		t.Fatalf("k through the model: cmd %v, loading %q", cmd != nil, a.loading)
	}
}

func TestPageWorktreeAddAndRemove(t *testing.T) {
	dir := newRepo(t, "r")
	a := open(t, dir)
	keys(t, a, "enter", "n")
	if a.r.TopModal() == "" || !strings.Contains(screen(a), "New worktree for branch") {
		t.Fatalf("no prompt\n%s", screen(a))
	}
	typeText(t, a, "feat")
	keys(t, a, "enter")
	wt := filepath.Join(filepath.Dir(dir), "r-feat")
	if _, err := os.Stat(wt); err != nil || a.worktrees.Len() != 2 || !strings.Contains(a.m.Info, "added worktree") || a.m.Status != "" {
		t.Fatalf("added: %v %d info %q error %q", err, a.worktrees.Len(), a.m.Info, a.m.Status)
	}
	if s := screen(a); !strings.Contains(s, "feat") || strings.Contains(s, "Error: added") {
		t.Fatalf("screen\n%s", s)
	}

	focusRow(t, a, a.worktrees, worktreeID(wt))
	write(t, wt, "dirt", "x\n")
	keys(t, a, "d", "d")
	if top := a.r.Instances[a.r.TopModal()]; top == nil || top.Payload.Prompt != "Remove worktree "+tilde(wt)+"?" {
		t.Fatalf("no confirm\n%s", screen(a))
	}
	if err := keyErr(a, "y"); err == nil || !strings.Contains(err.Error(), "has changes") {
		t.Fatalf("dirty remove: %v", err)
	}
	if _, err := os.Stat(wt); err != nil {
		t.Fatal("refused remove removed it")
	}
	// n declines; d D forces.
	keys(t, a, "d", "D", "n")
	if _, err := os.Stat(wt); err != nil {
		t.Fatal("declined remove removed it")
	}
	keys(t, a, "d", "D", "y")
	if _, err := os.Stat(wt); !os.IsNotExist(err) || a.worktrees.Len() != 1 {
		t.Fatalf("forced remove: %v %d", err, a.worktrees.Len())
	}
}

func TestPageBranches(t *testing.T) {
	dir := newRepo(t, "r")
	a := open(t, dir)
	if err := a.branches.Focus(); err != nil {
		t.Fatal(err)
	}
	keys(t, a, "n")
	typeText(t, a, "topic")
	keys(t, a, "enter")
	if ok, _ := BranchExists(t.Context(), dir, "topic"); !ok || a.branches.Len() != 2 {
		t.Fatalf("created: %v %d", ok, a.branches.Len())
	}
	focusRow(t, a, a.branches, branchID(dir, "topic"))
	keys(t, a, "enter")
	if s, _ := ReadStatus(t.Context(), dir); s.Branch != "topic" {
		t.Fatalf("switched to %s", s.Branch)
	}
	commit(t, dir, "t", "topic work")
	keys(t, a, "R")
	focusRow(t, a, a.branches, branchID(dir, "topic"))
	keys(t, a, "d", "d")
	if err := keyErr(a, "y"); err == nil || !strings.Contains(err.Error(), "checked out") {
		t.Fatalf("delete current: %v", err)
	}
	focusRow(t, a, a.branches, branchID(dir, "main"))
	keys(t, a, "enter")
	focusRow(t, a, a.branches, branchID(dir, "topic"))
	keys(t, a, "d", "d")
	if err := keyErr(a, "y"); err == nil || !strings.Contains(err.Error(), "not fully merged") {
		t.Fatalf("delete unmerged: %v", err)
	}
	keys(t, a, "d", "D", "y")
	if ok, _ := BranchExists(t.Context(), dir, "topic"); ok || a.branches.Len() != 1 {
		t.Fatalf("forced delete: %v %d", ok, a.branches.Len())
	}
}

// An empty prompt runs nothing; esc cancels.
func TestPromptCancel(t *testing.T) {
	dir := newRepo(t, "r")
	a := open(t, dir)
	if err := a.branches.Focus(); err != nil {
		t.Fatal(err)
	}
	keys(t, a, "n", "esc")
	keys(t, a, "n", "enter")
	if a.r.TopModal() != "" || a.branches.Len() != 1 {
		t.Fatalf("modal %q branches %d", a.r.TopModal(), a.branches.Len())
	}
}

// The page follows the family key map: a, x, X and s answer nowhere on
// the page (n, d d, d D and enter replaced them).
func TestRetiredKeysAreUnbound(t *testing.T) {
	dir := newRepo(t, "r")
	sh(t, dir, "branch", "topic")
	a := open(t, dir)
	wt, ok := a.worktrees.Selected()
	if !ok {
		t.Fatal("no worktree row")
	}
	for _, focus := range []func(){
		func() { _ = a.repos.Focus() },
		func() { focusRow(t, a, a.worktrees, wt.ID) },
		func() { focusRow(t, a, a.branches, branchID(dir, "topic")) },
	} {
		focus()
		for _, key := range []string{"a", "x", "X", "s"} {
			handled, err := a.r.Dispatch(ochazuke.MacroKey{Key: key})
			if handled || err != nil || a.r.TopModal() != "" {
				t.Fatalf("%s at %s: handled %v, %v, modal %q", key, a.r.Focus, handled, err, a.r.TopModal())
			}
		}
	}
	if err := a.Settle(); err != nil {
		t.Fatal(err)
	}
	if ok, _ := BranchExists(t.Context(), dir, "topic"); !ok || a.worktrees.Len() != 1 {
		t.Fatal("a retired key changed something")
	}
	if s, _ := ReadStatus(t.Context(), dir); s.Branch != "main" {
		t.Fatalf("switched to %s", s.Branch)
	}
}

func TestBindingsAreAtomic(t *testing.T) {
	isolate(t)
	a, err := New()
	if err != nil {
		t.Fatal(err)
	}
	warnings, err := a.r.ValidateBindings()
	if err != nil || len(warnings) > 0 {
		t.Fatalf("%v %v", warnings, err)
	}
}

func TestSmoke(t *testing.T) {
	dir := newRepo(t, "smoky")
	for _, args := range [][]string{{"--smoke", "--path", dir}, {"view", "--smoke", "--width", "60", dir}} {
		out := run(t, "", args...)
		if !strings.Contains(out, "Repos 1") || !strings.Contains(out, "smoky") || !strings.Contains(out, "Worktrees smoky") {
			t.Fatalf("%v\n%s", args, out)
		}
	}
	// With nothing to list, the page says so instead of failing.
	if out := run(t, "", "--smoke"); !strings.Contains(out, "o-proj is not available") && !strings.Contains(out, "No repositories") {
		t.Fatalf("empty\n%s", out)
	}
}

// Nothing acts on marked rows: space marks no row in any list, and no
// hint offers list.mark.
func TestListsTakeNoMarks(t *testing.T) {
	dir := newRepo(t, "r")
	sh(t, dir, "branch", "topic")
	a := open(t, dir)
	wt, ok := a.worktrees.Selected()
	if !ok {
		t.Fatal("no worktree row")
	}
	for _, focus := range []func(){
		func() { _ = a.repos.Focus() },
		func() { focusRow(t, a, a.worktrees, wt.ID) },
		func() { focusRow(t, a, a.branches, branchID(dir, "topic")) },
	} {
		focus()
		_, _ = a.r.Dispatch(ochazuke.MacroKey{Key: "space"})
		for _, list := range []*ochazuke.List{a.repos, a.worktrees, a.branches} {
			if marked := list.Marked(); len(marked) != 0 {
				t.Fatalf("space at %s marked %v", a.r.Focus, marked)
			}
		}
		if s := screen(a); strings.Contains(s, "list.mark") {
			t.Fatalf("a hint offers list.mark at %s\n%s", a.r.Focus, s)
		}
	}
}

// ctrl+w > and ctrl+w < move the divider; ctrl+w = puts it
// back (ochazuke's pane keys, through PaneLayout); stacked, the same keys
// move the repo list's bottom edge.
func TestPaneKeysResizeTheRepoList(t *testing.T) {
	dir := newRepo(t, "r")
	a := open(t, dir)
	divider := func() int {
		for _, line := range strings.Split(screen(a), "\n") {
			if i := strings.Index(line, "Worktrees"); i >= 0 {
				return ansi.StringWidth(line[:i])
			}
		}
		t.Fatalf("no worktrees panel\n%s", screen(a))
		return 0
	}
	before := divider()
	keys(t, a, "ctrl+w", ">")
	if grown := divider(); grown <= before {
		t.Fatalf("> did not grow the repo list: %d -> %d", before, grown)
	}
	keys(t, a, "ctrl+w", "<", "ctrl+w", "<")
	if shrunk := divider(); shrunk >= before {
		t.Fatalf("< did not shrink the repo list: %d", shrunk)
	}
	keys(t, a, "ctrl+w", "=")
	if reset := divider(); reset != before {
		t.Fatalf("= did not reset: %d, want %d", reset, before)
	}
	// Stacked: the worktrees panel starts lower when the repo list grows.
	a.m.Width = 60
	row := func() int {
		for i, line := range strings.Split(screen(a), "\n") {
			if strings.Contains(line, "Worktrees") {
				return i
			}
		}
		t.Fatalf("no worktrees panel\n%s", screen(a))
		return 0
	}
	top := row()
	keys(t, a, "3", "ctrl+w", ">")
	if lower := row(); lower <= top {
		t.Fatalf("3> did not grow the stacked repo list: %d -> %d", top, lower)
	}
}

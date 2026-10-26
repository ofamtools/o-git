package main

import (
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/unixsuperhero/ochazuke"
	ocli "github.com/unixsuperhero/ochazuke/cli"
)

// Rows: every list noun (repo, worktree, branch) has an id scheme, a
// label, table columns and one cell per column. The same rows fill the
// TUI's lists, a list verb's answer and the state's JSON Lines, so a host
// draws them as o-git does.
//
//	repo/DIR              the repository's toplevel
//	worktree/PATH         the worktree's directory
//	branch/REPO:NAME      REPO the directory the branch was listed from
//	                      (a ref name cannot hold a colon)

func repoID(dir string) string           { return "repo/" + dir }
func worktreeID(path string) string      { return "worktree/" + path }
func branchID(repo, name string) string  { return "branch/" + repo + ":" + name }
func worktreeLabel(w WorktreeRow) string { return tilde(w.Path) }

var columns = map[string][]ochazuke.Column{
	"repo": {
		{Title: "repo", Min: 10}, {Title: "branch", Priority: 1}, {Title: "sync", Priority: 2},
		{Title: "changes", Priority: 3}, {Title: "path", Priority: 4},
	},
	"worktree": {
		{Title: "branch", Min: 10}, {Title: "state", Priority: 1}, {Title: "path", Priority: 2}, {Title: "head", Priority: 3},
	},
	"branch": {
		{Title: "branch", Min: 10}, {Title: "sync", Priority: 1}, {Title: "age", Align: ochazuke.AlignRight, Priority: 2},
		{Title: "upstream", Priority: 4}, {Title: "merged", Priority: 5}, {Title: "subject", Priority: 3},
	},
}

// sync is how a branch stands against its upstream: - for none, gone, =
// when even, else ↑ahead ↓behind.
func syncText(upstream string, gone bool, ahead, behind int) string {
	switch {
	case upstream == "":
		return "-"
	case gone:
		return "gone"
	case ahead == 0 && behind == 0:
		return "="
	}
	var parts []string
	if ahead > 0 {
		parts = append(parts, fmt.Sprintf("↑%d", ahead))
	}
	if behind > 0 {
		parts = append(parts, fmt.Sprintf("↓%d", behind))
	}
	return strings.Join(parts, " ")
}

// changesText is a status's changes by kind: +staged ~unstaged
// ?untracked !conflicts, or clean.
func changesText(s Status) string {
	var parts []string
	for _, p := range []struct {
		mark string
		n    int
	}{{"+", s.Staged}, {"~", s.Unstaged}, {"?", s.Untracked}, {"!", s.Conflicts}} {
		if p.n > 0 {
			parts = append(parts, fmt.Sprintf("%s%d", p.mark, p.n))
		}
	}
	if len(parts) == 0 {
		return "clean"
	}
	return strings.Join(parts, " ")
}

func branchText(s Status) string {
	switch {
	case s.Detached:
		return "(detached " + shortSHA(s.Head) + ")"
	case s.Branch == "":
		return "-"
	}
	return s.Branch
}

func shortSHA(sha string) string { return sha[:min(len(sha), 7)] }

func repoCells(r Repo) []string {
	if r.Error != "" {
		return []string{r.Name, "-", "-", r.State, tilde(r.Path)}
	}
	return []string{r.Name, branchText(r.Status), syncText(r.Upstream, r.Gone, r.Ahead, r.Behind), changesText(r.Status), tilde(r.Path)}
}

func worktreeState(w WorktreeRow) string {
	state := w.State
	if w.State == "dirty" {
		state = fmt.Sprintf("dirty %d", w.Changes)
	}
	if w.Locked != "" {
		state += " locked"
	}
	if w.Main {
		state += " main"
	}
	return state
}

func worktreeCells(w WorktreeRow) []string {
	branch := w.Branch
	switch {
	case w.Bare:
		branch = "(bare)"
	case w.Detached:
		branch = "(detached " + shortSHA(w.Head) + ")"
	}
	return []string{branch, worktreeState(w), tilde(w.Path), shortSHA(w.Head)}
}

func branchCells(b BranchRow, now time.Time) []string {
	name := "  " + b.Name
	if b.Current {
		name = "* " + b.Name
	}
	merged := ""
	if b.Merged && !b.Current {
		merged = "merged"
	}
	return []string{name, syncText(b.Upstream, b.Gone, b.Ahead, b.Behind), age(now, b.Date), b.Upstream, merged, b.Subject}
}

// age is how long ago a Unix time was, in its largest whole unit.
func age(now time.Time, unix int64) string {
	if unix == 0 {
		return ""
	}
	d := now.Sub(time.Unix(unix, 0))
	switch {
	case d < time.Minute:
		return "now"
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d/time.Minute))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh", int(d/time.Hour))
	case d < 14*24*time.Hour:
		return fmt.Sprintf("%dd", int(d/(24*time.Hour)))
	case d < 60*24*time.Hour:
		return fmt.Sprintf("%dw", int(d/(7*24*time.Hour)))
	case d < 365*24*time.Hour:
		return fmt.Sprintf("%dmo", int(d/(30*24*time.Hour)))
	}
	return fmt.Sprintf("%dy", int(d/(365*24*time.Hour)))
}

func repoItems(repos []Repo) []ochazuke.ListItem {
	items := make([]ochazuke.ListItem, len(repos))
	for i, r := range repos {
		items[i] = ochazuke.ListItem{ID: repoID(r.Path), Label: r.Name, Noun: "repo", Data: r, Cells: repoCells(r)}
	}
	return items
}

func worktreeItems(rows []WorktreeRow) []ochazuke.ListItem {
	items := make([]ochazuke.ListItem, len(rows))
	for i, w := range rows {
		items[i] = ochazuke.ListItem{ID: worktreeID(w.Path), Label: worktreeLabel(w), Noun: "worktree", Data: w, Cells: worktreeCells(w)}
	}
	return items
}

func branchItems(rows []BranchRow, now time.Time) []ochazuke.ListItem {
	items := make([]ochazuke.ListItem, len(rows))
	for i, b := range rows {
		items[i] = ochazuke.ListItem{ID: branchID(b.Repo, b.Name), Label: b.Name, Noun: "branch", Data: b, Cells: branchCells(b, now)}
	}
	return items
}

// rowsOf turns list items into the CLI's rows: a list verb's answer.
func rowsOf(items []ochazuke.ListItem) []ocli.Row {
	out := make([]ocli.Row, len(items))
	for i, it := range items {
		out[i] = ocli.Row{Noun: it.Noun, ID: it.ID, Label: it.Label, Data: it.Data, Cells: it.Cells}
	}
	return out
}

// lines is rows as JSON Lines (the state's answer).
func lines(rows []ocli.Row) ocli.Lines {
	out := make(ocli.Lines, len(rows))
	for i, r := range rows {
		out[i] = r
	}
	return out
}

// revealed is the row a --target id names, before anything is read: the
// verb that acts on it reads the repository itself. A relative path is
// the current directory's; a branch id without REPO: is the current
// directory's branch.
func revealed(target string) (ochazuke.ListItem, error) {
	noun, rest, _ := strings.Cut(target, "/")
	if rest == "" {
		return ochazuke.ListItem{}, fmt.Errorf("not a row id (repo/DIR, worktree/PATH or branch/REPO:NAME): %s", target)
	}
	switch noun {
	case "repo":
		dir := absPath(rest)
		return ochazuke.ListItem{ID: target, Label: filepath.Base(dir), Noun: noun, Data: Repo{Name: filepath.Base(dir), Path: dir}}, nil
	case "worktree":
		w := WorktreeRow{Worktree: Worktree{Path: absPath(rest)}}
		return ochazuke.ListItem{ID: target, Label: worktreeLabel(w), Noun: noun, Data: w}, nil
	case "branch":
		repo, name := ".", rest
		if i := strings.LastIndex(rest, ":"); i >= 0 {
			repo, name = rest[:i], rest[i+1:]
		}
		if name == "" {
			return ochazuke.ListItem{}, fmt.Errorf("no branch name in %s", target)
		}
		return ochazuke.ListItem{ID: target, Label: name, Noun: noun, Data: BranchRow{Repo: absPath(repo), Branch: Branch{Name: name}}}, nil
	}
	return ochazuke.ListItem{}, fmt.Errorf("not a row id (repo/DIR, worktree/PATH or branch/REPO:NAME): %s", target)
}

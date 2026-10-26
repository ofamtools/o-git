package main

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/x/ansi"
	"github.com/unixsuperhero/ochazuke"
	ocli "github.com/unixsuperhero/ochazuke/cli"
)

// The Git page: a status line, then the repo list on the left and, on the
// right, the shown repository's worktrees, its branches and the detail of
// the focused row (stacked below stackWidth columns). The panel holding
// the focus has the accent border. The divider between the repo list and
// the rest follows the page's pane keys (> and < move it, = resets;
// ochazuke's Runtime.PaneLayout): the repo list's width side by side, its
// height stacked, the other panels sharing what is left as before.

// dataPage is the Git page as the catalog declares it (ocli.DeclarePage),
// so a host (mtm, nabe) draws it from o-git's rows through ocli.MountPage:
// the repo, branch and worktree lists split beside the selected row's
// record. The host fills every list from the state verb (repo.state), so
// its branch and worktree lists hold the rows of every repository, not of
// a selected one as the TUI's do. Detail fields are one order for all
// three nouns; a field a row lacks is left out.
func dataPage() ocli.CatalogPage {
	return ocli.CatalogPage{
		ID: pageID, Title: "Git", Layout: ocli.LayoutSplit,
		Lists: []ocli.CatalogList{
			{ID: reposID, Noun: "repo", Title: "Repositories"},
			{ID: branchesID, Noun: "branch", Title: "Branches"},
			{ID: worktreesID, Noun: "worktree", Title: "Worktrees"},
		},
		Detail: &ocli.CatalogDetail{Fields: []string{
			"name", "repo", "path", "branch", "state", "error", "head",
			"commit", "subject", "author", "date",
			"current", "worktree", "main", "upstream", "gone", "ahead", "behind", "merged",
			"staged", "unstaged", "untracked", "conflicts", "changes",
			"detached", "bare", "locked", "prunable",
		}},
	}
}

// stackWidth is the page width below which the panels stack.
const stackWidth = 70

func (a *App) render(r *ochazuke.Runtime, width, height int) ochazuke.Block {
	s := ochazuke.NewStyles(*a.m.Appearance)
	l := r.PaneLayout(pageID, ochazuke.Layout{Width: width, Height: height, Styles: s})
	status := s.Muted.Render(ansi.Truncate(a.statusLine(), width, "…"))
	bh := max(0, height-1)
	if bh < 8 || width < 20 {
		return ochazuke.Text(status)
	}
	panel := func(title string, list *ochazuke.List, w, h int) ochazuke.Block {
		cw, ch := ochazuke.PanelContentSize(w, h)
		return l.PanelBlock(title, ochazuke.Text(list.View(cw, ch)), w, h, list.Focused())
	}
	detail := func(w, h int) ochazuke.Block {
		cw, _ := ochazuke.PanelContentSize(w, h)
		return l.PanelBlock("Detail", ochazuke.Text(ansi.Hardwrap(a.detailText(), max(1, cw), true)), w, h, false)
	}
	name := a.currentName
	if name != "" {
		name = " " + name
	}
	if width < stackWidth {
		rh0 := bh * 3 / 10
		rh := l.Divide(rh0, bh, ochazuke.PaneMinHeight)
		// The worktrees and branches keep their share of the rest.
		wh, brh := bh/5*(bh-rh)/(bh-rh0), bh/4*(bh-rh)/(bh-rh0)
		blocks := []ochazuke.Block{
			panel(fmt.Sprintf("Repos %d", a.repos.Len()), a.repos, width, rh),
			panel("Worktrees"+name, a.worktrees, width, wh),
			panel("Branches"+name, a.branches, width, brh),
			detail(width, bh-rh-wh-brh),
		}
		texts := make([]string, len(blocks))
		for i, b := range blocks {
			texts[i] = b.Text
		}
		return ochazuke.Text(status + "\n" + strings.Join(texts, "\n"))
	}
	lw := l.Divide(min(max(30, width*2/5), width-40), width-1, ochazuke.PaneMinWidth)
	rw := width - lw - 1
	left := panel(fmt.Sprintf("Repos %d", a.repos.Len()), a.repos, lw, bh)
	wh := max(4, bh/4)
	bhh := max(4, bh*7/20)
	dh := bh - wh - bhh
	right := strings.Join([]string{
		panel("Worktrees"+name, a.worktrees, rw, wh).Text,
		panel("Branches"+name, a.branches, rw, bhh).Text,
		detail(rw, dh).Text,
	}, "\n")
	lr, rr := strings.Split(left.Text, "\n"), strings.Split(right, "\n")
	rows := make([]string, bh)
	for i := range rows {
		rows[i] = pad(at(lr, i), lw) + " " + at(rr, i)
	}
	return ochazuke.Text(status + "\n" + strings.Join(rows, "\n"))
}

func at(rows []string, i int) string {
	if i < len(rows) {
		return rows[i]
	}
	return ""
}

func pad(line string, width int) string {
	return line + strings.Repeat(" ", max(0, width-ansi.StringWidth(line)))
}

// statusLine is the line above the panels: how many repositories, the
// shown one, and what is loading.
func (a *App) statusLine() string {
	parts := []string{fmt.Sprintf("%d repos", a.repos.Len())}
	if s := a.shown; s != nil {
		parts = append(parts, s.Name+" "+tilde(s.Path)+" on "+branchText(s.Status)+" "+changesText(s.Status))
	}
	if a.loading != "" {
		parts = append(parts, "reading "+tilde(a.loading)+"…")
	}
	if a.listErr != nil {
		parts = append(parts, "error: "+a.listErr.Error())
	}
	return strings.Join(parts, "  ·  ")
}

// detailText describes the focused row: a repository, a worktree or a
// branch; on an empty list, the shown repository.
func (a *App) detailText() string {
	var data any
	if inst := a.r.Instances[a.r.BaseFocus()]; inst != nil {
		data = inst.Data
	}
	if data == nil && a.shown != nil {
		data = *a.shown
	}
	var b strings.Builder
	line := func(key, value string) {
		if value != "" {
			fmt.Fprintf(&b, "%-9s %s\n", key, value)
		}
	}
	switch d := data.(type) {
	case Repo:
		line("repo", d.Name+"  "+tilde(d.Path))
		line("state", d.State)
		line("error", d.Error)
		if d.Error == "" {
			line("branch", strings.TrimSpace(branchText(d.Status)+" "+shortSHA(d.Head)))
			if d.Upstream != "" {
				line("upstream", d.Upstream+" "+syncText(d.Upstream, d.Gone, d.Ahead, d.Behind))
			}
			line("changes", changeWords(d.Status))
		}
	case WorktreeRow:
		line("worktree", tilde(d.Path))
		line("branch", strings.TrimSpace(worktreeCells(d)[0]+" "+shortSHA(d.Head)))
		line("state", worktreeState(d))
		line("locked", d.Locked)
		line("prunable", d.Prunable)
		line("error", d.Error)
	case BranchRow:
		name := d.Name
		if d.Current {
			name += " (checked out here)"
		}
		line("branch", name)
		line("worktree", tilde(d.Worktree))
		if d.Upstream != "" {
			line("upstream", d.Upstream+" "+syncText(d.Upstream, d.Gone, d.Ahead, d.Behind))
		}
		merged := "not merged into HEAD"
		if d.Merged {
			merged = "merged into HEAD"
		}
		line("commit", shortSHA(d.Commit)+" "+merged)
		line("subject", d.Subject)
		if d.Date != 0 {
			line("author", d.Author+", "+time.Unix(d.Date, 0).Format("2006-01-02 15:04")+" ("+age(a.Now(), d.Date)+" ago)")
		}
	default:
		if a.repos.Len() == 0 && a.loading == "" {
			return "No repositories: o-proj lists none, and no --path was given."
		}
	}
	return strings.TrimRight(b.String(), "\n")
}

// changeWords is a status's changes in words, the nonzero kinds only.
func changeWords(s Status) string {
	var parts []string
	for _, p := range []struct {
		n    int
		what string
	}{{s.Staged, "staged"}, {s.Unstaged, "unstaged"}, {s.Untracked, "untracked"}, {s.Conflicts, "conflicted"}} {
		if p.n > 0 {
			parts = append(parts, fmt.Sprintf("%d %s", p.n, p.what))
		}
	}
	if len(parts) == 0 {
		return "none"
	}
	return strings.Join(parts, ", ")
}

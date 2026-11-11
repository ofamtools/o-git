package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"ofam.dev/ochazuke"
	ocli "ofam.dev/ochazuke/cli"
	"ofam.dev/oden"
)

// viewCommand is only a spelling of the registered repo.view verb.
func viewCommand(a *App) oden.Command {
	return oden.Command{
		Name: "view", Summary: "open the Git page: repos on the left, the selected repo's worktrees, branches and the focused row's detail on the right",
		Usage: "[--page git|compare] [--repo DIR] [--path DIR,...] [--smoke] [--width N] [--height N]", Source: "o-git",
		Details: "o-git with no command opens it too. Keys (the family key map): enter shows a repo or switches to a branch, n adds a worktree or branch, d d removes or deletes (confirm), d D forces, R reloads, q quits; ? on the page lists them all.",
		Run: func(ctx context.Context, args []string) error {
			return a.Shell().Run(ctx, append([]string{"repo", "view"}, args...))
		},
	}
}

// runTUI is retained for callers that launch a standalone page; the registered
// verb owns every flag and is also reachable from a palette or mounted host.
func runTUI(args []string, stdin io.Reader, stdout, stderr io.Writer) error {
	a, err := New()
	if err != nil {
		return err
	}
	a.Stdin, a.Stdout, a.Stderr = stdin, stdout, stderr
	return a.Shell().Run(context.Background(), append([]string{"repo", "view"}, args...))
}

func (a *App) view(opt viewOptions) error {
	page, repo, extra := opt.Page, opt.Repo, splitPaths(opt.Paths)
	if page == "compare" && repo == "" && len(extra) == 0 {
		dir, err := os.Getwd()
		if err != nil {
			return err
		}
		repo = dir
	}
	a.Paths, a.viewRepo = extra, repo
	if _, err := a.r.Execute(ochazuke.Request{Noun: "repo", Verb: "list", Target: pageID}); err != nil {
		return err
	}
	if page != "git" && page != "compare" {
		return fmt.Errorf("unknown page %q", page)
	}
	if page == "compare" {
		if err := a.Settle(); err != nil {
			a.r.UI().Error(err.Error())
		}
		if row, ok := a.repos.Selected(); ok {
			a.compare.repo = row.Data.(Repo).Path
		}
		if err := a.r.OpenPage(comparePageID); err != nil {
			return err
		}
	}
	if opt.Smoke {
		// A failed read is shown, as the page would show it.
		if err := a.Settle(); err != nil {
			a.r.UI().Error(err.Error())
		}
		a.m.Width, a.m.Height = opt.Width, opt.Height
		_, err := fmt.Fprintln(a.Stdout, strings.TrimRight(ansi.Strip(a.m.View().Content), "\n"))
		return err
	}
	a.tui = true
	_, err := tea.NewProgram(&model{a: a}, tea.WithInput(a.Stdin), tea.WithOutput(a.Stdout)).Run()
	return err
}

// Settle runs the scheduled reads on this goroutine until none is left,
// the lists following the selection (--smoke, tests).
func (a *App) Settle() error {
	return ocli.Settle(a.r, func() error { a.follow(); return nil })
}

// model is the ochazuke model plus the follow rule: after every message
// the worktree and branch lists follow the selected repo row.
type model struct{ a *App }
type compareClockMsg struct{}

func compareClock() tea.Cmd {
	return tea.Tick(time.Second, func(time.Time) tea.Msg { return compareClockMsg{} })
}

func (m *model) Init() tea.Cmd {
	return tea.Batch(m.a.m.Init(), m.a.r.PendingCommands(), compareClock())
}

func (m *model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	_, cmd := m.a.m.Update(msg)
	if _, ok := msg.(compareClockMsg); ok {
		cmd = tea.Batch(cmd, compareClock())
	}
	if m.a.r.Quit {
		return m, cmd
	}
	if m.a.follow() {
		cmd = tea.Batch(cmd, m.a.r.PendingCommands())
	}
	return m, cmd
}

func (m *model) View() tea.View { return m.a.m.View() }

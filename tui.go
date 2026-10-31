package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/unixsuperhero/ochazuke"
	ocli "github.com/unixsuperhero/ochazuke/cli"
	"github.com/unixsuperhero/oden"
)

// viewCommand is o-git view: the Git page.
func viewCommand(stdin io.Reader, stdout, stderr io.Writer) oden.Command {
	return oden.Command{
		Name: "view", Summary: "open the Git page: repos on the left, the selected repo's worktrees, branches and the focused row's detail on the right",
		Usage: "[--page git|compare] [--path DIR,...] [--smoke] [--width N] [--height N]", Source: "o-git",
		Details: "o-git with no command opens it too. Keys (the family key map): enter shows a repo or switches to a branch, n adds a worktree or branch, d d removes or deletes (confirm), d D forces, R reloads, q quits; ? on the page lists them all.",
		Run: func(_ context.Context, args []string) error {
			return runTUI(args, stdin, stdout, stderr)
		},
	}
}

// runTUI parses the page's flags and runs it (or renders one frame).
func runTUI(args []string, stdin io.Reader, stdout, stderr io.Writer) error {
	flags := flag.NewFlagSet("o-git view", flag.ContinueOnError)
	flags.SetOutput(stderr)
	page := flags.String("page", "git", "page to open: git or compare")
	paths := flags.String("path", "", "extra repository directories, comma-separated, listed after o-proj's")
	smoke := flags.Bool("smoke", false, "read the repositories, render one frame to stdout and exit")
	width := flags.Int("width", 100, "--smoke frame width")
	height := flags.Int("height", 30, "--smoke frame height")
	if err := flags.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return &oden.ExitError{Code: 2}
	}
	extra := splitPaths(*paths)
	for _, word := range flags.Args() {
		extra = append(extra, splitPaths(word)...)
	}
	a, err := New()
	if err != nil {
		return err
	}
	a.Paths, a.Stdin, a.Stdout, a.Stderr = extra, stdin, stdout, stderr
	if _, err := a.r.Execute(ochazuke.Request{Noun: "repo", Verb: "list", Target: pageID}); err != nil {
		return err
	}
	if *page != "git" && *page != "compare" {
		return fmt.Errorf("unknown page %q", *page)
	}
	if *page == "compare" {
		if err := a.Settle(); err != nil {
			return err
		}
		if err := a.r.OpenPage(comparePageID); err != nil {
			return err
		}
	}
	if *smoke {
		// A failed read is shown, as the page would show it.
		if err := a.Settle(); err != nil {
			a.r.UI().Error(err.Error())
		}
		a.m.Width, a.m.Height = *width, *height
		_, err := fmt.Fprintln(stdout, strings.TrimRight(ansi.Strip(a.m.View().Content), "\n"))
		return err
	}
	a.tui = true
	_, err = tea.NewProgram(&model{a: a}).Run()
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

func (m *model) Init() tea.Cmd { return tea.Batch(m.a.m.Init(), m.a.r.PendingCommands()) }

func (m *model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	_, cmd := m.a.m.Update(msg)
	if m.a.r.Quit {
		return m, cmd
	}
	if m.a.follow() {
		cmd = tea.Batch(cmd, m.a.r.PendingCommands())
	}
	return m, cmd
}

func (m *model) View() tea.View { return m.a.m.View() }

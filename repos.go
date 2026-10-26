package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"
)

// Repositories: the ones o-proj knows (o-proj list, its manifest read
// through its own command line, never opened here) plus directories named
// on the command line. o-proj owns the manifest, clones and syncs; o-git
// only looks inside the repositories and changes their worktrees and
// branches.

// Known is one repository o-proj lists.
type Known struct {
	Name string `json:"name"`
	Dir  string `json:"dir"`
}

// projTimeout bounds one o-proj list call.
const projTimeout = 10 * time.Second

// projCommand is the o-proj bin: $O_GIT_PROJ, else o-proj on PATH; "none"
// turns discovery off (tests, or a machine without o-proj).
func projCommand() string {
	if v := os.Getenv("O_GIT_PROJ"); v != "" {
		return v
	}
	return "o-proj"
}

// ErrNoProj is listKnown's error when discovery is off or o-proj is not
// installed; a list with --path still answers.
var ErrNoProj = errors.New("o-proj is not available (install it or set O_GIT_PROJ); pass --path DIR")

// listKnown runs o-proj list and returns its repositories in its order.
func listKnown(ctx context.Context) ([]Known, error) {
	bin := projCommand()
	if bin == "none" {
		return nil, ErrNoProj
	}
	if _, err := exec.LookPath(bin); err != nil {
		return nil, ErrNoProj
	}
	ctx, cancel := context.WithTimeout(ctx, projTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, bin, "list")
	out, err := cmd.Output()
	if err != nil {
		var exit *exec.ExitError
		if errors.As(err, &exit) && len(exit.Stderr) > 0 {
			err = fmt.Errorf("%w: %s", err, strings.TrimSpace(string(exit.Stderr)))
		}
		return nil, fmt.Errorf("o-proj list: %w", err)
	}
	var rows []Known
	if err := json.Unmarshal(out, &rows); err != nil {
		return nil, fmt.Errorf("o-proj list: %w", err)
	}
	return rows, nil
}

// Repo is one repository's row: where it is and how its own worktree
// stands. State is clean, dirty, missing (no directory), not-repo or
// error; Error says why for the last three.
type Repo struct {
	Name  string `json:"name"`
	Path  string `json:"path"`
	State string `json:"state"`
	Status
	Error string `json:"error,omitempty"`
}

// inspect reads one repository's status. A directory that is inside a
// repository but not its toplevel counts as that repository.
func inspect(ctx context.Context, name, dir string) Repo {
	r := Repo{Name: name, Path: dir}
	if _, err := os.Stat(dir); errors.Is(err, os.ErrNotExist) {
		r.State, r.Error = "missing", "directory does not exist"
		return r
	}
	top, err := Toplevel(ctx, dir)
	if err != nil {
		r.State, r.Error = "not-repo", err.Error()
		return r
	}
	r.Path = top
	if r.Status, err = ReadStatus(ctx, top); err != nil {
		r.State, r.Error = "error", err.Error()
		return r
	}
	r.State = "clean"
	if r.Dirty() {
		r.State = "dirty"
	}
	return r
}

// repoName is a directory's name: o-proj's for a directory it knows,
// else the base name.
func repoName(dir string, known []Known) string {
	for _, k := range known {
		if samePath(k.Dir, dir) {
			return k.Name
		}
	}
	return filepath.Base(dir)
}

// samePath compares two directories with symlinks resolved.
func samePath(a, b string) bool {
	if a == b {
		return true
	}
	ra, err1 := filepath.EvalSymlinks(a)
	rb, err2 := filepath.EvalSymlinks(b)
	return err1 == nil && err2 == nil && ra == rb
}

// resolve names the repository a REPO word means: a directory (inside a
// worktree; "" is the current directory), else a name o-proj knows. It
// returns the name and the worktree's toplevel.
func resolve(ctx context.Context, word string) (string, string, error) {
	if word == "" {
		word = "."
	}
	if info, err := os.Stat(expand(word)); err == nil && info.IsDir() {
		top, err := Toplevel(ctx, absPath(word))
		if err != nil {
			return "", "", err
		}
		return filepath.Base(top), top, nil
	}
	known, err := listKnown(ctx)
	if err != nil && !errors.Is(err, ErrNoProj) {
		return "", "", err
	}
	for _, k := range known {
		if k.Name == word {
			top, err := Toplevel(ctx, k.Dir)
			if err != nil {
				return "", "", fmt.Errorf("repo %s: %w", word, err)
			}
			return k.Name, top, nil
		}
	}
	return "", "", fmt.Errorf("no repo %q: not a directory, and not a name o-proj lists", word)
}

// listRepos is every repository o-proj knows plus extra directories, each
// inspected (in parallel), o-proj's order first; a directory listed twice
// is shown once. Without o-proj the extra directories still answer; with
// neither it is ErrNoProj.
func listRepos(ctx context.Context, extra []string) ([]Repo, error) {
	known, err := listKnown(ctx)
	if err != nil && (len(extra) == 0 || !errors.Is(err, ErrNoProj)) {
		return nil, err
	}
	type want struct{ name, dir string }
	var wants []want
	for _, k := range known {
		wants = append(wants, want{k.Name, k.Dir})
	}
	for _, p := range extra {
		dir := absPath(p)
		wants = append(wants, want{repoName(dir, known), dir})
	}
	repos := make([]Repo, len(wants))
	parallel(len(wants), func(i int) { repos[i] = inspect(ctx, wants[i].name, wants[i].dir) })
	out := make([]Repo, 0, len(repos))
	for _, r := range repos {
		if !slices.ContainsFunc(out, func(o Repo) bool { return o.State != "missing" && o.Path == r.Path }) {
			out = append(out, r)
		}
	}
	return out, ctx.Err()
}

// workers bounds the git calls one command runs at once.
const workers = 8

// parallel runs fn(0..n-1) on at most workers goroutines.
func parallel(n int, fn func(int)) {
	var wg sync.WaitGroup
	sem := make(chan struct{}, workers)
	for i := range n {
		wg.Add(1)
		sem <- struct{}{}
		go func() {
			defer func() { <-sem; wg.Done() }()
			fn(i)
		}()
	}
	wg.Wait()
}

// WorktreeRow is one worktree of a repository with its own changes. State
// is clean, dirty, missing (its directory is gone: prunable), bare or
// error.
type WorktreeRow struct {
	Repo string `json:"repo"`
	Worktree
	State   string `json:"state"`
	Changes int    `json:"changes"`
	Error   string `json:"error,omitempty"`
}

// listWorktreeRows lists dir's repository's worktrees, each with its status.
func listWorktreeRows(ctx context.Context, dir string) ([]WorktreeRow, error) {
	list, err := ListWorktrees(ctx, dir)
	if err != nil {
		return nil, err
	}
	rows := make([]WorktreeRow, len(list))
	repo := dir
	if len(list) > 0 {
		repo = list[0].Path
	}
	parallel(len(list), func(i int) {
		w := WorktreeRow{Repo: repo, Worktree: list[i]}
		switch {
		case w.Bare:
			w.State = "bare"
		case w.Prunable != "":
			w.State = "missing"
		default:
			s, err := ReadStatus(ctx, w.Path)
			switch {
			case err != nil:
				w.State, w.Error = "error", err.Error()
			case s.Dirty():
				w.State, w.Changes = "dirty", s.Changed()
			default:
				w.State = "clean"
			}
		}
		rows[i] = w
	})
	return rows, ctx.Err()
}

// BranchRow is one local branch of a repository.
type BranchRow struct {
	Repo string `json:"repo"`
	Branch
}

func listBranchRows(ctx context.Context, dir string) ([]BranchRow, error) {
	list, err := ListBranches(ctx, dir)
	if err != nil {
		return nil, err
	}
	rows := make([]BranchRow, len(list))
	for i, b := range list {
		rows[i] = BranchRow{Repo: dir, Branch: b}
	}
	return rows, nil
}

// RepoDetail is repo show's answer.
type RepoDetail struct {
	Repo      Repo          `json:"repo"`
	Worktrees []WorktreeRow `json:"worktrees"`
	Branches  []BranchRow   `json:"branches"`
}

// detail reads one repository's status, worktrees and branches.
func detail(ctx context.Context, name, dir string) (RepoDetail, error) {
	d := RepoDetail{Repo: inspect(ctx, name, dir), Worktrees: []WorktreeRow{}, Branches: []BranchRow{}}
	if d.Repo.Error != "" {
		return d, errors.New(d.Repo.Error)
	}
	var werr, berr error
	var wg sync.WaitGroup
	wg.Add(2)
	go func() { defer wg.Done(); d.Worktrees, werr = listWorktreeRows(ctx, d.Repo.Path) }()
	go func() { defer wg.Done(); d.Branches, berr = listBranchRows(ctx, d.Repo.Path) }()
	wg.Wait()
	return d, errors.Join(werr, berr)
}

// mainWorktree is the main worktree of the repository holding dir.
func mainWorktree(ctx context.Context, dir string) (string, error) {
	list, err := ListWorktrees(ctx, dir)
	if err != nil {
		return "", err
	}
	if len(list) == 0 {
		return "", fmt.Errorf("%s: git lists no worktrees", dir)
	}
	return list[0].Path, nil
}

// defaultWorktreePath is where worktree add puts a branch with no path:
// beside the main worktree, named after it and the branch.
func defaultWorktreePath(main, branch string) string {
	return filepath.Join(filepath.Dir(main), filepath.Base(main)+"-"+strings.ReplaceAll(branch, "/", "-"))
}

// expand replaces a leading ~ with the home directory.
func expand(p string) string {
	if p == "~" || strings.HasPrefix(p, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, strings.TrimPrefix(p, "~"))
		}
	}
	return p
}

// absPath is p expanded and made absolute (as is when that fails).
func absPath(p string) string {
	if abs, err := filepath.Abs(expand(p)); err == nil {
		return abs
	}
	return p
}

// tilde shortens a path under the home directory to ~/...
func tilde(p string) string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return p
	}
	if p == home {
		return "~"
	}
	if rest, ok := strings.CutPrefix(p, home+string(filepath.Separator)); ok {
		return "~/" + rest
	}
	return p
}

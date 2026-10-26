package main

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// Every test works on repositories it creates under t.TempDir, with the
// owner's git configuration shut out, so no test reads or writes a real
// repository or the manifest.

// isolate shuts out the global and system git configuration and fixes the
// identity commits use.
func isolate(t *testing.T) {
	t.Helper()
	for k, v := range map[string]string{
		"GIT_CONFIG_GLOBAL": os.DevNull, "GIT_CONFIG_NOSYSTEM": "1",
		"GIT_AUTHOR_NAME": "Test", "GIT_AUTHOR_EMAIL": "test@example.com",
		"GIT_COMMITTER_NAME": "Test", "GIT_COMMITTER_EMAIL": "test@example.com",
		"O_GIT_PROJ": "none",
	} {
		t.Setenv(k, v)
	}
}

// sh runs git in dir and fails the test on an error.
func sh(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
	return strings.TrimSpace(string(out))
}

// newRepo makes a repository with one commit on main and returns its
// toplevel as git spells it.
func newRepo(t *testing.T, name string) string {
	t.Helper()
	isolate(t)
	dir := filepath.Join(t.TempDir(), name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	sh(t, dir, "init", "-q", "-b", "main")
	write(t, dir, "README", "hello\n")
	sh(t, dir, "add", "README")
	sh(t, dir, "commit", "-q", "-m", "first")
	return sh(t, dir, "rev-parse", "--show-toplevel")
}

func write(t *testing.T, dir, name, text string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
}

func commit(t *testing.T, dir, file, msg string) {
	t.Helper()
	write(t, dir, file, msg+"\n")
	sh(t, dir, "add", file)
	sh(t, dir, "commit", "-q", "-m", msg)
}

func TestReadStatus(t *testing.T) {
	ctx := context.Background()
	dir := newRepo(t, "r")
	s, err := ReadStatus(ctx, dir)
	if err != nil || s.Branch != "main" || s.Dirty() || s.Head == "" || s.Upstream != "" {
		t.Fatalf("clean %+v %v", s, err)
	}
	write(t, dir, "README", "changed\n")
	write(t, dir, "new", "x\n")
	write(t, dir, "staged", "x\n")
	sh(t, dir, "add", "staged")
	if s, err = ReadStatus(ctx, dir); err != nil || s.Staged != 1 || s.Unstaged != 1 || s.Untracked != 1 || s.Changed() != 3 {
		t.Fatalf("dirty %+v %v", s, err)
	}
	// A rename's record carries its source path as an extra field.
	sh(t, dir, "commit", "-q", "-am", "two")
	sh(t, dir, "mv", "staged", "moved")
	if s, err = ReadStatus(ctx, dir); err != nil || s.Staged != 1 || s.Untracked != 1 {
		t.Fatalf("rename %+v %v", s, err)
	}

	// Ahead and behind an upstream, then the upstream gone.
	clone := filepath.Join(t.TempDir(), "clone")
	sh(t, dir, "clone", "-q", dir, clone)
	commit(t, clone, "a", "ahead one")
	commit(t, dir, "b", "behind one")
	sh(t, clone, "fetch", "-q")
	if s, err = ReadStatus(ctx, clone); err != nil || s.Upstream != "origin/main" || s.Ahead != 1 || s.Behind != 1 || s.Gone {
		t.Fatalf("tracking %+v %v", s, err)
	}
	sh(t, clone, "update-ref", "-d", "refs/remotes/origin/main")
	if s, err = ReadStatus(ctx, clone); err != nil || !s.Gone {
		t.Fatalf("gone %+v %v", s, err)
	}

	sh(t, dir, "checkout", "-q", "--detach")
	if s, err = ReadStatus(ctx, dir); err != nil || !s.Detached || s.Branch != "" {
		t.Fatalf("detached %+v %v", s, err)
	}
}

func TestWorktreesAndBranches(t *testing.T) {
	ctx := context.Background()
	dir := newRepo(t, "r")
	wt := filepath.Join(filepath.Dir(dir), "r-feat")
	created, err := AddWorktree(ctx, dir, wt, "feat", "")
	if err != nil || !created {
		t.Fatalf("add new branch: %v %v", created, err)
	}
	sh(t, dir, "branch", "old")
	other := filepath.Join(filepath.Dir(dir), "r-old")
	if created, err = AddWorktree(ctx, dir, other, "old", ""); err != nil || created {
		t.Fatalf("add existing branch: %v %v", created, err)
	}
	if _, err := AddWorktree(ctx, dir, filepath.Join(dir, "x"), "old", "main"); err == nil {
		t.Fatal("--from on an existing branch")
	}
	if _, err := AddWorktree(ctx, dir, filepath.Join(dir, "x"), "-bad", ""); err == nil {
		t.Fatal("a branch name starting with -")
	}
	list, err := ListWorktrees(ctx, wt)
	if err != nil || len(list) != 3 || !list[0].Main || list[0].Branch != "main" || list[1].Branch != "feat" || list[2].Branch != "old" {
		t.Fatalf("worktrees %+v %v", list, err)
	}
	if real, _ := filepath.EvalSymlinks(wt); list[1].Path != real {
		t.Fatalf("path %s, want %s", list[1].Path, real)
	}

	commit(t, wt, "f", "feature work")
	branches, err := ListBranches(ctx, dir)
	if err != nil || len(branches) != 3 {
		t.Fatalf("branches %+v %v", branches, err)
	}
	byName := map[string]Branch{}
	for _, b := range branches {
		byName[b.Name] = b
	}
	if branches[0].Name != "feat" || byName["feat"].Merged || !byName["old"].Merged || !byName["main"].Current || byName["feat"].Subject != "feature work" || byName["feat"].Worktree == "" {
		t.Fatalf("branch fields %+v", branches)
	}

	// Removing: git refuses a dirty worktree without force.
	write(t, wt, "dirt", "x")
	if err := RemoveWorktree(ctx, dir, wt, false); err == nil {
		t.Fatal("removed a dirty worktree")
	}
	if err := RemoveWorktree(ctx, dir, wt, true); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(wt); !os.IsNotExist(err) {
		t.Fatalf("worktree dir still there: %v", err)
	}

	// Branches: create, switch, delete merged, refuse unmerged.
	if err := CreateBranch(ctx, dir, "topic", ""); err != nil {
		t.Fatal(err)
	}
	if err := CreateBranch(ctx, dir, "topic", ""); err == nil {
		t.Fatal("created a branch twice")
	}
	if err := SwitchBranch(ctx, dir, "topic"); err != nil {
		t.Fatal(err)
	}
	if s, _ := ReadStatus(ctx, dir); s.Branch != "topic" {
		t.Fatalf("switched to %s", s.Branch)
	}
	sh(t, dir, "switch", "-q", "main")
	if err := DeleteBranch(ctx, dir, "topic", false); err != nil {
		t.Fatal(err)
	}
	if err := DeleteBranch(ctx, dir, "feat", false); !errors.Is(err, ErrUnmerged) {
		t.Fatalf("unmerged delete: %v", err)
	}
	if err := DeleteBranch(ctx, dir, "feat", true); err != nil {
		t.Fatal(err)
	}
	if ok, err := BranchExists(ctx, dir, "feat"); ok || err != nil {
		t.Fatalf("feat still exists: %v %v", ok, err)
	}
	if err := DeleteBranch(ctx, dir, "old", true); err == nil {
		t.Fatal("deleted a branch checked out in a worktree")
	}
}

func TestToplevel(t *testing.T) {
	ctx := context.Background()
	dir := newRepo(t, "r")
	if err := os.Mkdir(filepath.Join(dir, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	if top, err := Toplevel(ctx, filepath.Join(dir, "sub")); err != nil || top != dir {
		t.Fatalf("toplevel %q %v", top, err)
	}
	if _, err := Toplevel(ctx, t.TempDir()); err == nil || !strings.Contains(err.Error(), "not in a git worktree") {
		t.Fatalf("outside: %v", err)
	}
	if _, err := Toplevel(ctx, filepath.Join(dir, "nope")); err == nil {
		t.Fatal("missing dir")
	}
}

// The environment never points git at another repository.
func TestGitIgnoresGitDir(t *testing.T) {
	dir := newRepo(t, "r")
	other := newRepo(t, "other")
	t.Setenv("GIT_DIR", filepath.Join(other, ".git"))
	top, err := Toplevel(context.Background(), dir)
	if err != nil || top != dir {
		t.Fatalf("toplevel %q %v", top, err)
	}
}

package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// The git layer: every call runs the git CLI (no libgit2) with a timeout,
// never prompts, fetches only on an explicit fetch verb, and ignores the caller's
// GIT_DIR-style variables so a call means the directory it names. Callers
// run it off the event loop (pending work); nothing here knows ochazuke.

// Timeouts for one git call: reads are quick, a mutation may check out a
// tree.
const (
	readTimeout  = 15 * time.Second
	writeTimeout = 2 * time.Minute
)

// gitEnv is the environment git runs with: the process's, minus the
// variables that would point it at another repository, with prompts off
// and messages in English (some errors are matched below).
func gitEnv() []string {
	var env []string
	for _, value := range os.Environ() {
		key, _, _ := strings.Cut(value, "=")
		switch key {
		case "GIT_DIR", "GIT_WORK_TREE", "GIT_INDEX_FILE", "GIT_COMMON_DIR", "GIT_OBJECT_DIRECTORY", "GIT_ALTERNATE_OBJECT_DIRECTORIES", "GIT_PREFIX", "LC_ALL", "LANG", "GIT_TERMINAL_PROMPT", "GIT_ASKPASS", "SSH_ASKPASS", "SSH_ASKPASS_REQUIRE", "GIT_OPTIONAL_LOCKS", "GIT_NO_LAZY_FETCH":
			continue
		}
		env = append(env, value)
	}
	return append(env, "GIT_TERMINAL_PROMPT=0", "GIT_ASKPASS=/usr/bin/false", "SSH_ASKPASS=/usr/bin/false", "SSH_ASKPASS_REQUIRE=force", "GIT_OPTIONAL_LOCKS=0", "GIT_NO_LAZY_FETCH=1", "LC_ALL=C")
}

// GitError is a failed git call: the arguments, git's stderr and the exit
// error (a timeout says so).
type GitError struct {
	Args   []string
	Stderr string
	Err    error
}

func (e *GitError) Error() string {
	msg := strings.TrimSpace(e.Stderr)
	if msg == "" {
		msg = e.Err.Error()
	}
	return "git " + strings.Join(e.Args, " ") + ": " + msg
}

func (e *GitError) Unwrap() error { return e.Err }

// git runs one git command in dir and returns its stdout.
func git(ctx context.Context, timeout time.Duration, dir string, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	options := []string{"-C", dir, "-c", "core.fsmonitor=false", "-c", "color.ui=false"}
	env := gitEnv()
	fetch := len(args) > 0 && args[0] == "fetch"
	if fetch {
		// Unknown credential settings are ignored by older Git versions.
		options = append(options, "-c", "credential.interactive=never")
		env = replaceGitEnv(env, "GCM_INTERACTIVE", "never")
		if strings.TrimSpace(os.Getenv("GIT_SSH_COMMAND")) == "" {
			out, err := git(ctx, readTimeout, dir, "config", "--get", "core.sshCommand")
			var exit *exec.ExitError
			if err != nil && (!errors.As(err, &exit) || exit.ExitCode() != 1) {
				return nil, err
			}
			env = replaceGitEnv(env, "GIT_SSH_COMMAND", "")
			if strings.TrimSpace(string(out)) == "" {
				env = append(env, "GIT_SSH_COMMAND=ssh -o BatchMode=yes")
			}
		}
	}
	cmd := exec.CommandContext(ctx, "git", append(options, args...)...)
	cmd.Env = env
	if fetch {
		cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	}
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			err = fmt.Errorf("timed out after %s", timeout)
		}
		return nil, &GitError{Args: args, Stderr: stderr.String(), Err: err}
	}
	return out, nil
}

func replaceGitEnv(env []string, key, value string) []string {
	out := make([]string, 0, len(env)+1)
	for _, entry := range env {
		if !strings.HasPrefix(entry, key+"=") {
			out = append(out, entry)
		}
	}
	if value != "" {
		out = append(out, key+"="+value)
	}
	return out
}

// gitStream is the read-only streaming counterpart of git. noIndex permits
// diff --no-index's exit status 1 (differences found), without hiding failures.
// consume must drain stdout; its error cancels the child before waiting.
func gitStream(ctx context.Context, dir string, noIndex bool, consume func(io.Reader) error, args ...string) error {
	ctx, cancel := context.WithTimeout(ctx, readTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", append([]string{"-C", dir, "-c", "core.fsmonitor=false", "-c", "color.ui=false"}, args...)...)
	cmd.Env = replaceGitEnv(gitEnv(), "GIT_PAGER", "cat")
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	if err = cmd.Start(); err != nil {
		return err
	}
	rerr := consume(stdout)
	if rerr != nil {
		cancel()
	}
	err = cmd.Wait()
	if rerr != nil {
		return rerr
	}
	if ctx.Err() != nil {
		return &GitError{Args: args, Stderr: stderr.String(), Err: ctx.Err()}
	}
	var exit *exec.ExitError
	if noIndex && errors.As(err, &exit) && exit.ExitCode() == 1 && !strings.Contains(stderr.String(), "error:") && !strings.Contains(stderr.String(), "fatal:") {
		return nil
	}
	if err != nil {
		return &GitError{Args: args, Stderr: stderr.String(), Err: err}
	}
	return nil
}

// Toplevel is the root of the worktree holding dir, as git spells it
// (symlinks resolved).
func Toplevel(ctx context.Context, dir string) (string, error) {
	if info, err := os.Stat(dir); err != nil {
		return "", err
	} else if !info.IsDir() {
		return "", fmt.Errorf("%s is not a directory", dir)
	}
	out, err := git(ctx, readTimeout, dir, "rev-parse", "--show-toplevel")
	if err != nil {
		return "", fmt.Errorf("%s is not in a git worktree: %w", dir, err)
	}
	return strings.TrimSpace(string(out)), nil
}

// Status is one worktree's state: its branch, how it stands against its
// upstream, and its changes by kind.
type Status struct {
	Branch    string `json:"branch,omitempty"` // "" when detached or before the first commit's branch exists
	Head      string `json:"head,omitempty"`   // the short commit id, "" before the first commit
	Detached  bool   `json:"detached,omitempty"`
	Upstream  string `json:"upstream,omitempty"`
	Gone      bool   `json:"gone,omitempty"` // the upstream is configured but its ref is gone
	Ahead     int    `json:"ahead"`
	Behind    int    `json:"behind"`
	Staged    int    `json:"staged"`
	Unstaged  int    `json:"unstaged"`
	Untracked int    `json:"untracked"`
	Conflicts int    `json:"conflicts"`
}

// Changed is the number of paths with any change.
func (s Status) Changed() int { return s.Staged + s.Unstaged + s.Untracked + s.Conflicts }

// Dirty reports whether the worktree has changes, untracked files included.
func (s Status) Dirty() bool { return s.Changed() > 0 }

// ReadStatus reads dir's status (git status --porcelain=v2).
func ReadStatus(ctx context.Context, dir string) (Status, error) {
	out, err := git(ctx, readTimeout, dir, "status", "--porcelain=v2", "-z", "--branch", "--untracked-files=all", "--ignore-submodules=none")
	if err != nil {
		return Status{}, err
	}
	return parseStatus(out)
}

// parseStatus reads porcelain v2 -z output: "# branch.*" headers, then one
// NUL-terminated record per path (a rename's record is followed by its
// source path). A staged and unstaged change to one path counts in both.
func parseStatus(out []byte) (Status, error) {
	var s Status
	upstream := false
	fields := strings.Split(string(out), "\x00")
	for i := 0; i < len(fields); i++ {
		rec := fields[i]
		if rec == "" {
			continue
		}
		if h, ok := strings.CutPrefix(rec, "# "); ok {
			key, value, _ := strings.Cut(h, " ")
			switch key {
			case "branch.oid":
				if value != "(initial)" {
					s.Head = value[:min(len(value), 12)]
				}
			case "branch.head":
				if value == "(detached)" {
					s.Detached = true
				} else {
					s.Branch = value
				}
			case "branch.upstream":
				s.Upstream, upstream = value, true
			case "branch.ab":
				a, b, _ := strings.Cut(value, " ")
				s.Ahead, _ = strconv.Atoi(strings.TrimPrefix(a, "+"))
				s.Behind, _ = strconv.Atoi(strings.TrimPrefix(b, "-"))
				upstream = false
			}
			continue
		}
		switch rec[0] {
		case '?':
			s.Untracked++
		case '!':
		case 'u':
			s.Conflicts++
		case '1', '2':
			if len(rec) < 4 {
				return s, fmt.Errorf("git status: short record %q", rec)
			}
			if rec[2] != '.' {
				s.Staged++
			}
			if rec[3] != '.' {
				s.Unstaged++
			}
			if rec[0] == '2' {
				i++ // the rename's source path
			}
		default:
			return s, fmt.Errorf("git status: unknown record %q", rec)
		}
	}
	// An upstream with no branch.ab line is configured but its ref is gone.
	s.Gone = upstream
	return s, nil
}

// Worktree is one entry of git worktree list.
type Worktree struct {
	Path     string `json:"path"`
	Head     string `json:"head,omitempty"`
	Branch   string `json:"branch,omitempty"` // "" when detached or bare
	Detached bool   `json:"detached,omitempty"`
	Bare     bool   `json:"bare,omitempty"`
	Main     bool   `json:"main,omitempty"` // the first entry: the repository's own worktree
	Locked   string `json:"locked,omitempty"`
	Prunable string `json:"prunable,omitempty"`
}

// ListWorktrees lists the worktrees of the repository holding dir, the
// main one first.
func ListWorktrees(ctx context.Context, dir string) ([]Worktree, error) {
	out, err := git(ctx, readTimeout, dir, "worktree", "list", "--porcelain", "-z")
	if err != nil {
		return nil, err
	}
	return parseWorktrees(out), nil
}

// parseWorktrees reads porcelain -z output: NUL-terminated attribute
// lines, an empty one between worktrees. A locked or prunable attribute
// without a reason reads as "yes".
func parseWorktrees(out []byte) []Worktree {
	var list []Worktree
	var cur *Worktree
	for _, line := range strings.Split(string(out), "\x00") {
		if line == "" {
			cur = nil
			continue
		}
		key, value, _ := strings.Cut(line, " ")
		if key == "worktree" {
			list = append(list, Worktree{Path: value, Main: len(list) == 0})
			cur = &list[len(list)-1]
			continue
		}
		if cur == nil {
			continue
		}
		reason := value
		if reason == "" {
			reason = "yes"
		}
		switch key {
		case "HEAD":
			cur.Head = value
		case "branch":
			cur.Branch = strings.TrimPrefix(value, "refs/heads/")
		case "detached":
			cur.Detached = true
		case "bare":
			cur.Bare = true
		case "locked":
			cur.Locked = reason
		case "prunable":
			cur.Prunable = reason
		}
	}
	return list
}

// Branch is one local branch.
type Branch struct {
	Name     string `json:"name"`
	Current  bool   `json:"current,omitempty"` // checked out in the worktree that was asked
	Worktree string `json:"worktree,omitempty"`
	Upstream string `json:"upstream,omitempty"`
	Gone     bool   `json:"gone,omitempty"`
	Ahead    int    `json:"ahead"`
	Behind   int    `json:"behind"`
	Merged   bool   `json:"merged"` // reachable from the asked worktree's HEAD
	Commit   string `json:"commit"`
	Subject  string `json:"subject"`
	Author   string `json:"author"`
	Date     int64  `json:"date"` // committer date, Unix seconds
}

const branchFormat = "%(refname:short)%00%(HEAD)%00%(worktreepath)%00%(upstream:short)%00%(upstream:track,nobracket)%00%(objectname:short=12)%00%(committerdate:unix)%00%(authorname)%00%(subject)"

// ListBranches lists the local branches of the repository holding dir,
// newest commit first; Current and Merged are relative to dir's HEAD.
func ListBranches(ctx context.Context, dir string) ([]Branch, error) {
	out, err := git(ctx, readTimeout, dir, "for-each-ref", "--sort=-committerdate", "--format="+branchFormat, "refs/heads")
	if err != nil {
		return nil, err
	}
	list, err := parseBranches(out)
	if err != nil || len(list) == 0 {
		return list, err
	}
	merged, err := git(ctx, readTimeout, dir, "for-each-ref", "--merged=HEAD", "--format=%(refname:short)", "refs/heads")
	if err != nil {
		return nil, err
	}
	in := map[string]bool{}
	for _, name := range strings.Split(strings.TrimSpace(string(merged)), "\n") {
		in[name] = true
	}
	for i := range list {
		list[i].Merged = in[list[i].Name]
	}
	return list, nil
}

func parseBranches(out []byte) ([]Branch, error) {
	list := []Branch{}
	for _, line := range strings.Split(strings.TrimRight(string(out), "\n"), "\n") {
		if line == "" {
			continue
		}
		f := strings.Split(line, "\x00")
		if len(f) != 9 {
			return nil, fmt.Errorf("git for-each-ref: unexpected line %q", line)
		}
		b := Branch{Name: f[0], Current: f[1] == "*", Worktree: f[2], Upstream: f[3], Commit: f[5], Author: f[7], Subject: f[8]}
		b.Date, _ = strconv.ParseInt(f[6], 10, 64)
		for _, part := range strings.Split(f[4], ", ") {
			switch {
			case part == "gone":
				b.Gone = true
			case strings.HasPrefix(part, "ahead "):
				b.Ahead, _ = strconv.Atoi(strings.TrimPrefix(part, "ahead "))
			case strings.HasPrefix(part, "behind "):
				b.Behind, _ = strconv.Atoi(strings.TrimPrefix(part, "behind "))
			}
		}
		list = append(list, b)
	}
	return list, nil
}

// BranchExists reports whether refs/heads/name exists in dir's repository.
func BranchExists(ctx context.Context, dir, name string) (bool, error) {
	_, err := git(ctx, readTimeout, dir, "show-ref", "--verify", "--quiet", "refs/heads/"+name)
	var ge *GitError
	switch {
	case err == nil:
		return true, nil
	case errors.As(err, &ge):
		var exit *exec.ExitError
		if errors.As(ge.Err, &exit) && exit.ExitCode() == 1 {
			return false, nil
		}
	}
	return false, err
}

// checkBranchName refuses a name git would refuse, before any git call
// that would take it as an option or a path.
func checkBranchName(ctx context.Context, dir, name string) error {
	if name == "" {
		return errors.New("a branch name is required")
	}
	if strings.HasPrefix(name, "-") {
		return fmt.Errorf("branch name %q starts with -", name)
	}
	if _, err := git(ctx, readTimeout, dir, "check-ref-format", "--branch", name); err != nil {
		return fmt.Errorf("%q is not a valid branch name", name)
	}
	return nil
}

// AddWorktree adds a worktree at path on branch: an existing branch is
// checked out there, a new one is created from base (HEAD when empty).
// It reports whether the branch was created.
func AddWorktree(ctx context.Context, dir, path, branch, base string) (bool, error) {
	if err := checkBranchName(ctx, dir, branch); err != nil {
		return false, err
	}
	exists, err := BranchExists(ctx, dir, branch)
	if err != nil {
		return false, err
	}
	if exists && base != "" {
		return false, fmt.Errorf("branch %s exists; --from applies only to a new branch", branch)
	}
	args := []string{"worktree", "add", "--", path, branch}
	if !exists {
		args = []string{"worktree", "add", "-b", branch, "--", path}
		if base != "" {
			args = append(args, base)
		}
	}
	_, err = git(ctx, writeTimeout, dir, args...)
	return !exists, err
}

// RemoveWorktree removes the worktree at path. git itself refuses a dirty
// one without force; the caller checks first so the refusal says why.
func RemoveWorktree(ctx context.Context, dir, path string, force bool) error {
	args := []string{"worktree", "remove"}
	if force {
		args = append(args, "--force")
	}
	_, err := git(ctx, writeTimeout, dir, append(args, "--", path)...)
	return err
}

// CreateBranch creates branch name at base (HEAD when empty) without
// checking it out.
func CreateBranch(ctx context.Context, dir, name, base string) error {
	if err := checkBranchName(ctx, dir, name); err != nil {
		return err
	}
	args := []string{"branch", "--", name}
	if base != "" {
		args = append(args, base)
	}
	_, err := git(ctx, writeTimeout, dir, args...)
	return err
}

// SwitchBranch checks out branch name in the worktree at dir.
func SwitchBranch(ctx context.Context, dir, name string) error {
	if err := checkBranchName(ctx, dir, name); err != nil {
		return err
	}
	_, err := git(ctx, writeTimeout, dir, "switch", "--no-guess", name)
	return err
}

// ErrUnmerged is DeleteBranch's refusal of a branch not merged into its
// upstream or HEAD.
var ErrUnmerged = errors.New("not fully merged")

// DeleteBranch deletes branch name; without force git refuses one that is
// not merged (ErrUnmerged).
func DeleteBranch(ctx context.Context, dir, name string, force bool) error {
	flag := "-d"
	if force {
		flag = "-D"
	}
	_, err := git(ctx, writeTimeout, dir, "branch", flag, "--", name)
	var ge *GitError
	if errors.As(err, &ge) && strings.Contains(ge.Stderr, "not fully merged") {
		return fmt.Errorf("branch %s is %w; --force deletes it anyway", name, ErrUnmerged)
	}
	return err
}

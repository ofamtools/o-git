package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/unixsuperhero/ochazuke"
)

func compareDefaults() CompareConfig {
	return CompareConfig{Endpoints: slices.Clone(defaultEndpoints), Pairs: slices.Clone(defaultPairs), MainNames: []string{"main", "master"}, Collapse: true, DefaultPair: "dirty", StaleAfter: 24 * time.Hour}
}

func compareResolver(t *testing.T, dir string, cfg CompareConfig) *EndpointResolver {
	t.Helper()
	r, err := NewEndpointResolver(context.Background(), dir, cfg, time.Now(), &CompareCache{})
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func oneEndpoint(t *testing.T, r *EndpointResolver, spec string) Endpoint {
	t.Helper()
	es := r.Resolve(spec, "HEAD")
	if len(es) != 1 {
		t.Fatalf("%s: %+v", spec, es)
	}
	return es[0]
}

func isolateCompare(t *testing.T) {
	t.Helper()
	isolate(t)
	base := t.TempDir()
	for _, name := range []string{"HOME", "XDG_CONFIG_HOME", "XDG_STATE_HOME", "XDG_DATA_HOME"} {
		dir := filepath.Join(base, name)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		t.Setenv(name, dir)
	}
}

func TestCompareValidRef(t *testing.T) {
	for _, ref := range []string{"", "--help", "a b", "a\nb", "a\tb", "a\x7fb", strings.Repeat("x", 257)} {
		if ValidRef(ref) == nil {
			t.Errorf("accepted %q", ref)
		}
	}
	for _, ref := range []string{"HEAD", "main", "refs/heads/foo/bar", "HEAD~2", "@{upstream}", "a-b"} {
		if err := ValidRef(ref); err != nil {
			t.Errorf("%s: %v", ref, err)
		}
	}
}

func TestCompareRevisionExpressions(t *testing.T) {
	d := newRepo(t, "r")
	addBareRemote(t, d, "origin")
	sh(t, d, "branch", "--set-upstream-to=origin/main", "main")
	var mu sync.Mutex
	verified := 0
	runner := func(ctx context.Context, timeout time.Duration, dir string, args ...string) ([]byte, error) {
		if slices.Equal(args, []string{"rev-parse", "--verify", "refs/heads/main^{commit}"}) {
			mu.Lock()
			verified++
			mu.Unlock()
		}
		return git(ctx, timeout, dir, args...)
	}
	r, err := newEndpointResolver(context.Background(), d, compareDefaults(), time.Now(), &CompareCache{}, runner)
	if err != nil {
		t.Fatal(err)
	}
	for _, spec := range []string{sh(t, d, "rev-parse", "HEAD"), "HEAD~0", "HEAD@{0}", "@{upstream}", "refs/heads/main", "refs/heads/main"} {
		e := oneEndpoint(t, r, spec)
		if !e.Available || e.SHA != sh(t, d, "rev-parse", spec+"^{commit}") {
			t.Fatalf("%s: %+v", spec, e)
		}
		if spec == "@{upstream}" && (e.Remote != "origin" || e.Fetched == nil) {
			t.Fatalf("remote expression metadata: %+v", e)
		}
	}
	if verified != 1 {
		t.Fatalf("explicit ref verified %d times, want once", verified)
	}
	blob := sh(t, d, "rev-parse", "HEAD:README")
	if e := oneEndpoint(t, r, blob); e.Available || e.Reason == "" {
		t.Fatalf("blob became commit: %+v", e)
	}
}

// Six policy/count/fork cases adapted from hiiro's branch_comparison_test.rb.
// The last deliberately requires an unavailable fork point, never its fallback.
func TestCompareHiiroCases(t *testing.T) {
	for _, tc := range []struct {
		name  string
		check func(*testing.T, string)
	}{
		{"first existing local ref", func(t *testing.T, dir string) {
			sh(t, dir, "branch", "-m", "master")
			e := oneEndpoint(t, compareResolver(t, dir, compareDefaults()), "main | master")
			if !e.Available || e.Ref != "refs/heads/master" {
				t.Fatalf("%+v", e)
			}
		}},
		{"remote first policy order", func(t *testing.T, dir string) {
			sh(t, dir, "update-ref", "refs/remotes/origin/main", "HEAD")
			e := oneEndpoint(t, compareResolver(t, dir, compareDefaults()), "origin/master | master | origin/main | main")
			if !e.Available || e.Ref != "refs/remotes/origin/main" {
				t.Fatalf("%+v", e)
			}
		}},
		{"fallback and none", func(t *testing.T, dir string) {
			commit(t, dir, "next", "second")
			r := compareResolver(t, dir, compareDefaults())
			e := oneEndpoint(t, r, "absent | HEAD~1")
			if !e.Available || e.SHA != sh(t, dir, "rev-parse", "HEAD~1") {
				t.Fatalf("%+v", e)
			}
			if e = oneEndpoint(t, r, "absent | also-absent"); e.Available || e.Reason == "" {
				t.Fatalf("%+v", e)
			}
		}},
		{"explicit target", func(t *testing.T, dir string) {
			sh(t, dir, "branch", "develop")
			commit(t, dir, "next", "main moved")
			e := oneEndpoint(t, compareResolver(t, dir, compareDefaults()), "develop")
			if !e.Available || e.Ref != "refs/heads/develop" || e.SHA == sh(t, dir, "rev-parse", "HEAD") {
				t.Fatalf("%+v", e)
			}
		}},
		{"ahead behind and ancestry", func(t *testing.T, dir string) {
			sh(t, dir, "checkout", "-q", "-b", "feature")
			commit(t, dir, "feature", "ahead")
			sh(t, dir, "checkout", "-q", "main")
			commit(t, dir, "main", "behind one")
			commit(t, dir, "main", "behind two")
			r := compareResolver(t, dir, compareDefaults())
			p := r.Show("", "refs/heads/main", "feature")[0]
			if !p.Available || p.Ahead != 1 || p.Behind != 2 {
				t.Fatalf("%+v", p)
			}
			p = r.Show("", "merge-base(main)", "feature")[0]
			if !p.Available || p.Ahead != 1 || p.Behind != 0 {
				t.Fatalf("%+v", p)
			}
		}},
		{"fork point does not silently fall back", func(t *testing.T, dir string) {
			root := sh(t, dir, "rev-parse", "HEAD")
			commit(t, dir, "main", "old main")
			sh(t, dir, "branch", "feature")
			sh(t, dir, "update-ref", "--create-reflog", "refs/remotes/origin/main", "HEAD")
			sh(t, dir, "checkout", "-q", "--detach", root)
			commit(t, dir, "rewritten", "new main")
			sh(t, dir, "update-ref", "refs/remotes/origin/main", "HEAD")
			sh(t, dir, "checkout", "-q", "feature")
			commit(t, dir, "feature", "feature work")
			r := compareResolver(t, dir, compareDefaults())
			e := oneEndpoint(t, r, "fork-point(origin/main)")
			if !e.Available || e.SHA != sh(t, dir, "rev-parse", "feature~1") {
				t.Fatalf("with reflog: %+v", e)
			}
			// Model an expired reflog directly in the fixture, without running
			// a destructive git command.
			if err := os.WriteFile(filepath.Join(r.commonDir, "logs/refs/remotes/origin/main"), nil, 0o644); err != nil {
				t.Fatal(err)
			}
			r = compareResolver(t, dir, compareDefaults())
			e = oneEndpoint(t, r, "fork-point(origin/main)")
			mb := oneEndpoint(t, r, "merge-base(origin/main)")
			if e.Available || !strings.Contains(e.Reason, "reflog") || !mb.Available || mb.SHA != root {
				t.Fatalf("fork %+v, merge %+v", e, mb)
			}
		}},
	} {
		t.Run(tc.name, func(t *testing.T) { tc.check(t, newRepo(t, "r")) })
	}
}

func addBareRemote(t *testing.T, dir, name string) string {
	t.Helper()
	bare := filepath.Join(t.TempDir(), name+".git")
	sh(t, dir, "clone", "--bare", "-q", dir, bare)
	sh(t, dir, "remote", "add", name, bare)
	sh(t, dir, "fetch", "-q", name)
	return bare
}

func TestCompareMainDetection(t *testing.T) {
	for _, tc := range []struct {
		name     string
		setup    func(*testing.T, string)
		override string
		want     string
	}{
		{"local main", nil, "", "refs/heads/main"},
		{"local master", func(t *testing.T, d string) { sh(t, d, "branch", "-m", "master") }, "", "refs/heads/master"},
		{"origin HEAD wins", func(t *testing.T, d string) {
			addBareRemote(t, d, "origin")
			sh(t, d, "branch", "develop")
			sh(t, d, "update-ref", "refs/remotes/origin/develop", "HEAD")
			sh(t, d, "symbolic-ref", "refs/remotes/origin/HEAD", "refs/remotes/origin/develop")
		}, "", "refs/heads/develop"},
		{"only remote HEAD", func(t *testing.T, d string) {
			addBareRemote(t, d, "upstream")
			sh(t, d, "update-ref", "refs/remotes/upstream/trunk", "HEAD")
			sh(t, d, "symbolic-ref", "refs/remotes/upstream/HEAD", "refs/remotes/upstream/trunk")
		}, "", "refs/remotes/upstream/trunk"},
		{"two remotes no HEAD", func(t *testing.T, d string) { addBareRemote(t, d, "origin"); addBareRemote(t, d, "upstream") }, "", "refs/heads/main"},
		{"remote candidate without local", func(t *testing.T, d string) {
			addBareRemote(t, d, "upstream")
			sh(t, d, "branch", "-m", "feature")
		}, "", "refs/remotes/upstream/main"},
		{"override", func(t *testing.T, d string) { sh(t, d, "branch", "trunk") }, "trunk", "refs/heads/trunk"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d := newRepo(t, "r")
			if tc.setup != nil {
				tc.setup(t, d)
			}
			cfg := compareDefaults()
			cfg.Main = tc.override
			e := oneEndpoint(t, compareResolver(t, d, cfg), "{main}")
			if !e.Available || e.Ref != tc.want {
				t.Fatalf("%+v, want %s", e, tc.want)
			}
		})
	}
}

func TestCompareDefaultPairs(t *testing.T) {
	for _, state := range []string{"two remotes", "origin HEAD", "master", "detached", "unborn", "no upstream", "shallow"} {
		t.Run(state, func(t *testing.T) {
			d := newRepo(t, "r")
			if state == "unborn" {
				d = t.TempDir()
				sh(t, d, "init", "-q", "-b", "main")
			} else {
				if state == "master" {
					sh(t, d, "branch", "-m", "master")
				}
				addBareRemote(t, d, "origin")
				addBareRemote(t, d, "upstream")
				branch := "main"
				if state == "master" {
					branch = "master"
				}
				if state != "no upstream" {
					sh(t, d, "branch", "--set-upstream-to=origin/"+branch, branch)
				}
				if state == "origin HEAD" {
					sh(t, d, "symbolic-ref", "refs/remotes/origin/HEAD", "refs/remotes/origin/main")
				}
				if state == "detached" {
					sh(t, d, "checkout", "-q", "--detach")
				}
				if state == "shallow" {
					commit(t, d, "next", "second")
					clone := filepath.Join(t.TempDir(), "shallow")
					sh(t, d, "clone", "-q", "--depth=1", "file://"+d, clone)
					d = clone
				}
			}
			cfg := compareDefaults()
			cfg.Collapse = false
			r := compareResolver(t, d, cfg)
			pairs := r.Pairs()
			want := 8
			if len(r.remotes) > 0 {
				want = 6 + 2*len(r.remotes)
			}
			if len(pairs) != want {
				t.Fatalf("%d pairs want %d: %+v", len(pairs), want, pairs)
			}
			for _, p := range pairs {
				if !p.Available && p.Reason == "" {
					t.Errorf("no reason: %+v", p)
				}
				if !p.From.Available && p.From.Reason == "" || !p.To.Available && p.To.Reason == "" {
					t.Errorf("endpoint lacks reason: %+v", p)
				}
				if p.Name == "dirty" && (!p.Available || p.From.Kind != map[bool]string{true: "empty", false: "commit"}[state == "unborn"] || p.To.Kind != "worktree") {
					t.Errorf("dirty: %+v", p)
				}
				if p.Name == "upstream" && (state == "detached" || state == "no upstream" || state == "unborn") && p.Available {
					t.Errorf("upstream: %+v", p)
				}
			}
		})
	}
}

func TestCompareDegeneratePairs(t *testing.T) {
	d := newRepo(t, "r")
	sh(t, d, "branch", "base")
	commit(t, d, "next", "second")
	r := compareResolver(t, d, compareDefaults())
	for _, tc := range []struct {
		from, to, state string
		available       bool
		ahead, behind   int
	}{
		{"WORKTREE", "HEAD", "reversed: the working tree is on the left", true, 0, 0},
		{"INDEX", "HEAD", "reversed: the working tree is on the left", true, 0, 0},
		{"INDEX", "WORKTREE", "the working tree is not a commit", true, 0, 0},
		{"WORKTREE", "INDEX", "the working tree is not a commit", true, 0, 0},
		{"HEAD", "INDEX", "the working tree is not a commit", true, 0, 0},
		{"HEAD", "HEAD", "same commit: nothing differs", true, 0, 0},
		{"WORKTREE", "WORKTREE", "same commit: nothing differs", true, 0, 0},
		{"merge-base(main)", "fork-point(main)", "both sides are relative to each other", false, 0, 0},
		{"merge-base(base)", "WORKTREE", "relative to the worktree's HEAD", true, 0, 0},
		{"merge-base(base)", "INDEX", "relative to the worktree's HEAD", true, 0, 0},
		{"HEAD", "base", "ready", true, 0, 1},
	} {
		t.Run(tc.from+".."+tc.to, func(t *testing.T) {
			p := r.Show("", tc.from, tc.to)[0]
			if p.Available != tc.available || !strings.Contains(p.State, tc.state) || p.Ahead != tc.ahead || p.Behind != tc.behind {
				t.Fatalf("%+v", p)
			}
		})
	}
}

func TestCompareUnbornAndWorktrees(t *testing.T) {
	d := newRepo(t, "r")
	wt := filepath.Join(t.TempDir(), "other")
	sh(t, d, "worktree", "add", "-q", "-b", "other", wt)
	r := compareResolver(t, d, compareDefaults())
	e := oneEndpoint(t, r, "worktree:"+wt)
	if !e.Available || e.SHA != sh(t, d, "rev-parse", "HEAD") {
		t.Fatalf("%+v", e)
	}
	if err := os.Rename(wt, wt+"-gone"); err != nil {
		t.Fatal(err)
	}
	if e = oneEndpoint(t, r, "worktree:"+wt); e.Available || !strings.Contains(e.Reason, "gone") {
		t.Fatalf("registered but missing: %+v", e)
	}
	missing := oneEndpoint(t, r, "worktree:"+filepath.Join(t.TempDir(), "missing"))
	if missing.Available || !strings.Contains(missing.Reason, "gone") {
		t.Fatalf("%+v", missing)
	}
	unborn := t.TempDir()
	sh(t, unborn, "init", "-q", "-b", "main")
	// A local object store lets unborn HEAD be compared to another history.
	sh(t, unborn, "fetch", "-q", d, "main:refs/heads/existing")
	r = compareResolver(t, unborn, compareDefaults())
	for _, tc := range []struct {
		from, to      string
		ahead, behind int
	}{{"HEAD", "existing", 1, 0}, {"existing", "HEAD", 0, 1}, {"HEAD", "HEAD", 0, 0}} {
		p := r.Show("", tc.from, tc.to)[0]
		if !p.Available || p.Ahead != tc.ahead || p.Behind != tc.behind {
			t.Fatalf("%+v", p)
		}
	}
	if oneEndpoint(t, r, "HEAD").Kind != "empty" {
		t.Fatal("unborn HEAD is not the empty tree")
	}
}

func TestCompareCollapseAndMemo(t *testing.T) {
	d := newRepo(t, "r")
	sh(t, d, "branch", "base")
	commit(t, d, "next", "next")
	cfg := compareDefaults()
	cfg.Pairs = []string{"a=base..HEAD", "b=base..HEAD", "same=HEAD..HEAD", "gone=missing..HEAD"}
	cache := &CompareCache{}
	var mu sync.Mutex
	calls := map[string]int{}
	runner := func(ctx context.Context, timeout time.Duration, dir string, args ...string) ([]byte, error) {
		mu.Lock()
		calls[args[0]]++
		mu.Unlock()
		return git(ctx, timeout, dir, args...)
	}
	refresh := func() []Comparison {
		r, err := newEndpointResolver(context.Background(), d, cfg, time.Now(), cache, runner)
		if err != nil {
			t.Fatal(err)
		}
		return r.Pairs()
	}
	pairs := refresh()
	if len(pairs) != 3 || pairs[0].Name != "a · b" || len(pairs[0].Members) != 2 || pairs[0].Ahead != 1 {
		t.Fatalf("%+v", pairs)
	}
	refresh()
	if calls["rev-list"] != 1 || calls["for-each-ref"] != 2 || calls["worktree"] != 2 || calls["fetch"] != 0 {
		t.Fatalf("calls: %+v", calls)
	}
	commit(t, d, "next", "next again")
	refresh()
	if calls["rev-list"] != 2 {
		t.Fatalf("memo did not invalidate: %+v", calls)
	}
	cfg.Collapse = false
	pairs = refresh()
	if len(pairs) != 4 || calls["rev-list"] != 2 {
		t.Fatalf("uncollapsed memo: %+v %+v", pairs, calls)
	}
}

func TestCompareSettingsAndCLI(t *testing.T) {
	isolateCompare(t)
	d := newRepo(t, "r")
	addBareRemote(t, d, "origin")
	addBareRemote(t, d, "upstream")
	for _, noun := range []string{"endpoint", "compare"} {
		rows := decode[[]row[map[string]any]](t, run(t, "", noun, "list", d))
		if len(rows) == 0 {
			t.Fatal("no rows")
		}
		ids := map[string]bool{}
		for _, r := range rows {
			if ids[r.ID] {
				t.Errorf("duplicate %s row id %s", noun, r.ID)
			}
			ids[r.ID] = true
			if r.Noun != noun || len(r.Cells) != len(columns[noun]) {
				t.Fatalf("%+v", r)
			}
		}
		for _, format := range []string{"table", "markdown", "toon"} {
			text := run(t, "", noun, "list", d, "--format", format)
			if !strings.Contains(text, map[string]string{"endpoint": "head", "compare": "dirty"}[noun]) {
				t.Fatalf("%s: %s", format, text)
			}
		}
	}
	e := decode[Endpoint](t, run(t, "", "endpoint", "resolve", "HEAD~0", "--repo", d))
	if !e.Available || e.SHA != sh(t, d, "rev-parse", "HEAD") {
		t.Fatalf("%+v", e)
	}
	p := decode[Comparison](t, run(t, "", "compare", "show", "--pair", "dirty", "--repo", d))
	if !p.Available || p.To.Kind != "worktree" {
		t.Fatalf("%+v", p)
	}
	if _, _, err := try("", "compare", "show", "--pair", "dirty", "--from", "HEAD", "--repo", d); err == nil {
		t.Fatal("accepted ambiguous selector")
	}
	a, err := New()
	if err != nil {
		t.Fatal(err)
	}
	a.r.Config().Dir = t.TempDir()
	cfg := a.compareConfig()
	if !reflect.DeepEqual(cfg.Endpoints, defaultEndpoints) || !reflect.DeepEqual(cfg.Pairs, defaultPairs) || cfg.StaleAfter != 24*time.Hour || !cfg.Collapse || cfg.DefaultPair != "dirty" {
		t.Fatalf("%+v", cfg)
	}
	for _, s := range a.r.Settings().Declared() {
		if strings.HasPrefix(s.Name, "compare.commits.") {
			t.Fatal(s.Name)
		}
	}
	if err := a.r.Settings().Set("compare.stale-after", "-1s"); err == nil {
		t.Fatal("negative stale age")
	}
	if err := a.r.Settings().Set("compare.main", "absent"); err != nil {
		t.Fatal(err)
	}
	if oneEndpoint(t, compareResolver(t, d, a.compareConfig()), "{main}").Available {
		t.Fatal("override fell back")
	}
	for _, spec := range []string{"--help", "HEAD\nmain", "HEAD\n", "missing"} {
		e := decode[Endpoint](t, run(t, "", "endpoint", "resolve", "--spec", spec, "--repo", d))
		if e.Available || e.Reason == "" {
			t.Fatalf("%+v", e)
		}
	}
}

func TestCompareCopyUsesRegisters(t *testing.T) {
	isolateCompare(t)
	d := newRepo(t, "r")
	a, err := New()
	if err != nil {
		t.Fatal(err)
	}
	a.r.Config().Disable()
	for _, tc := range []struct {
		noun string
		args ochazuke.Arguments
		want string
	}{
		{"endpoint", ochazuke.Arguments{"repo": d, "spec": "HEAD"}, sh(t, d, "rev-parse", "HEAD")},
		{"compare", ochazuke.Arguments{"repo": d, "from": "HEAD", "to": "HEAD"}, sh(t, d, "rev-parse", "HEAD") + ".." + sh(t, d, "rev-parse", "HEAD")},
	} {
		// Shell settles pending work and delivers the register write on its loop.
		var out strings.Builder
		a.Stdout = &out
		a.Stderr = &strings.Builder{}
		args := []string{tc.noun, "copy", "--repo", d}
		for _, key := range []string{"spec", "from", "to"} {
			if v, ok := tc.args[key]; ok {
				args = append(args, "--"+key, v.(string))
			}
		}
		if err := a.Shell().Run(context.Background(), args); err != nil {
			t.Fatal(err)
		}
		if got := a.r.Registers().Named["\""].Yank.Text; got != tc.want {
			t.Fatalf("register %q want %q; %s", got, tc.want, out.String())
		}
	}
}

func TestCompareUnavailableRecords(t *testing.T) {
	d := newRepo(t, "r")
	cfg := compareDefaults()
	cfg.Endpoints = append(cfg.Endpoints, "a=b", "b=a")
	cfg.Pairs = []string{"broken", "cycle=a..HEAD", "two-relative=merge-base(main)..fork-point(main)", "x=missing..HEAD", "y=missing..HEAD"}
	r := compareResolver(t, d, cfg)
	if e := oneEndpoint(t, r, "a"); e.Available || !strings.Contains(e.Reason, "cycle") {
		t.Fatalf("%+v", e)
	}
	pairs := r.Pairs()
	if len(pairs) != 5 {
		t.Fatalf("lost unavailable rows: %+v", pairs)
	}
	for _, p := range pairs {
		if p.Available || p.Reason == "" {
			t.Fatalf("%+v", p)
		}
	}
	if p := r.Show("unknown", "", "")[0]; p.Available || !strings.Contains(p.Reason, "no saved pair") {
		t.Fatalf("%+v", p)
	}
	if e := oneEndpoint(t, r, "merge-base(main"); e.Available || !strings.Contains(e.Reason, "parentheses") {
		t.Fatalf("%+v", e)
	}
}

func TestCompareFetchRunnerAndEnvironment(t *testing.T) {
	isolateCompare(t)
	t.Setenv("GIT_TERMINAL_PROMPT", "1")
	t.Setenv("GIT_ASKPASS", "owner-askpass")
	t.Setenv("SSH_ASKPASS", "owner-ssh-askpass")
	t.Setenv("GIT_NO_LAZY_FETCH", "0")
	env := map[string]string{}
	for _, v := range gitEnv() {
		k, v, _ := strings.Cut(v, "=")
		env[k] = v
	}
	if env["GIT_TERMINAL_PROMPT"] != "0" || env["GIT_ASKPASS"] != "/usr/bin/false" || env["SSH_ASKPASS"] != "/usr/bin/false" || env["GIT_NO_LAZY_FETCH"] != "1" {
		t.Fatalf("prompt env: %+v", env)
	}
	var calls [][]string
	runner := func(_ context.Context, timeout time.Duration, _ string, args ...string) ([]byte, error) {
		if timeout != writeTimeout {
			t.Fatal(timeout)
		}
		calls = append(calls, slices.Clone(args))
		return nil, nil
	}
	if err := FetchRemotes(context.Background(), t.TempDir(), []string{"origin", "upstream"}, []string{"origin", "upstream"}, runner, nil); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(calls, [][]string{{"fetch", "--prune", "--", "origin"}, {"fetch", "--prune", "--", "upstream"}}) {
		t.Fatalf("%+v", calls)
	}
	before := len(calls)
	for _, remotes := range [][]string{nil, {"missing"}, {"origin", "--all"}} {
		if FetchRemotes(context.Background(), t.TempDir(), remotes, []string{"origin"}, runner, nil) == nil {
			t.Fatal("accepted", remotes)
		}
	}
	if len(calls) != before {
		t.Fatal("fetch before validation")
	}
	fail := func(context.Context, time.Duration, string, ...string) ([]byte, error) {
		return nil, fmt.Errorf("local fixture transport failed")
	}
	if err := FetchRemotes(context.Background(), t.TempDir(), []string{"origin"}, []string{"origin"}, fail, nil); err == nil || !strings.Contains(err.Error(), "local fixture transport failed") {
		t.Fatalf("%v", err)
	}
}

package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

func ageFixture(t *testing.T, r *EndpointResolver, remote string, at time.Time) {
	t.Helper()
	sha := sh(t, r.dir, "rev-parse", "HEAD")
	line := fmt.Sprintf("%s %s Test <test@example.com> %d +0000\tfixture\n", sha, sha, at.Unix())
	if err := os.WriteFile(r.reflogPath("refs/remotes/"+remote+"/main"), []byte(line), 0o644); err != nil {
		t.Fatal(err)
	}
}

// Review 1: a successful unchanged fetch advances only its own remote, and
// survives an app restart. A failed fetch must not advance it.
func TestCompareRound2FetchAge(t *testing.T) {
	isolateCompare(t)
	d := newRepo(t, "r")
	addBareRemote(t, d, "origin")
	addBareRemote(t, d, "other")
	a, err := New()
	if err != nil {
		t.Fatal(err)
	}
	a.r.Config().Dir = t.TempDir()
	a.r.Config().StateDir = t.TempDir()
	now := time.Now().UTC()
	a.Now = func() time.Time { return now }
	r := compareResolver(t, d, a.compareConfig())
	old := now.Add(-72 * time.Hour).Truncate(time.Second)
	ageFixture(t, r, "origin", old)
	ageFixture(t, r, "other", old)
	if err := os.Chtimes(r.fetchHead, old, old); err != nil {
		t.Fatal(err)
	}
	// No ref or reflog changes: a quiet remote still completed a fetch.
	a.fetchRun = func(context.Context, time.Duration, string, ...string) ([]byte, error) { return nil, nil }
	a.Stdout, a.Stderr = &strings.Builder{}, &strings.Builder{}
	if err := a.Shell().Run(context.Background(), []string{"repo", "fetch", "--remote", "origin", "--repo", d}); err != nil {
		t.Fatal(err)
	}
	check := func(cfg CompareConfig) {
		t.Helper()
		r := compareResolver(t, d, cfg)
		origin, other := oneEndpoint(t, r, "origin/main"), oneEndpoint(t, r, "other/main")
		if origin.Fetched == nil || origin.Fetched.Before(now) || origin.Stale {
			t.Errorf("unchanged origin stays stale: %+v", origin)
		}
		if other.Fetched == nil || !other.Fetched.Equal(old) || !other.Stale {
			t.Errorf("other remote refreshed: %+v", other)
		}
	}
	check(a.compareConfig())
	b, err := New()
	if err != nil {
		t.Fatal(err)
	}
	b.r.Config().Dir = a.r.Config().Dir
	b.r.Config().StateDir = a.r.Config().StateDir
	check(b.compareConfig())
	a.fetchRun = func(context.Context, time.Duration, string, ...string) ([]byte, error) {
		return nil, fmt.Errorf("fixture offline")
	}
	if err := a.Shell().Run(context.Background(), []string{"repo", "fetch", "--remote", "other", "--repo", d}); err == nil {
		t.Fatal("fetch failure lost")
	}
	check(a.compareConfig())
}

func TestCompareRound2FetchHeadScope(t *testing.T) {
	d := newRepo(t, "r")
	origin := addBareRemote(t, d, "origin")
	addBareRemote(t, d, "other")
	r := compareResolver(t, d, compareDefaults())
	for _, remote := range []string{"origin", "other"} {
		if err := os.WriteFile(r.reflogPath("refs/remotes/"+remote+"/main"), nil, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	line := sh(t, d, "rev-parse", "HEAD") + "\t\tbranch 'main' of " + origin + "\n"
	if err := os.WriteFile(r.fetchHead, []byte(line), 0o644); err != nil {
		t.Fatal(err)
	}
	originEndpoint, other := oneEndpoint(t, r, "origin/main"), oneEndpoint(t, r, "other/main")
	if originEndpoint.Fetched == nil {
		t.Error("named origin FETCH_HEAD not recognized")
	}
	if other.Fetched != nil {
		t.Errorf("unrelated FETCH_HEAD made other fresh: %+v", other)
	}
}

// Review 2: canonical identity determines remote scope, including ambiguity
// with a local branch. Revision expressions must not acquire invented refs.
func TestCompareRound2RemoteIdentity(t *testing.T) {
	for _, collision := range []bool{false, true} {
		t.Run(fmt.Sprint(collision), func(t *testing.T) {
			d := newRepo(t, "r")
			addBareRemote(t, d, "josh")
			root := sh(t, d, "rev-parse", "HEAD")
			commit(t, d, "new", "local advanced")
			sh(t, d, "branch", "josh/feature")
			if collision {
				sh(t, d, "update-ref", "--create-reflog", "refs/remotes/josh/feature", root)
			}
			r := compareResolver(t, d, compareDefaults())
			e := oneEndpoint(t, r, "josh/feature")
			if !e.Available || e.Ref != "refs/heads/josh/feature" || e.Remote != "" || len(e.Remotes) > 0 {
				t.Errorf("local branch identified as remote: %+v", e)
			}
			expanded := oneEndpoint(t, r, "{remote}/feature")
			pair := r.Show("", "{remote}/feature", "HEAD")[0]
			if !expanded.Available || len(expanded.Remotes) > 0 || len(pair.Remotes) > 0 {
				t.Errorf("expansion overrode canonical identity: %+v; %+v", expanded, pair)
			}
			fp := oneEndpoint(t, r, "fork-point(josh/feature)")
			if !fp.Available || fp.SHA != sh(t, d, "rev-parse", "HEAD") {
				t.Errorf("wrong reflog: %+v", fp)
			}
			e = oneEndpoint(t, r, "josh/main~0")
			if !e.Available || strings.HasPrefix(e.Ref, "refs/remotes/") || len(e.Remotes) > 0 {
				t.Errorf("invented revision ref: %+v", e)
			}
		})
	}
}

func TestCompareRound2AmbiguousFetchHead(t *testing.T) {
	d := newRepo(t, "r")
	url := addBareRemote(t, d, "origin")
	sh(t, d, "remote", "add", "alias", url)
	sh(t, d, "fetch", "-q", "alias")
	r := compareResolver(t, d, compareDefaults())
	for _, remote := range []string{"origin", "alias"} {
		if err := os.WriteFile(r.reflogPath("refs/remotes/"+remote+"/main"), nil, 0o644); err != nil {
			t.Fatal(err)
		}
		if e := oneEndpoint(t, r, remote+"/main"); e.Fetched != nil {
			t.Errorf("ambiguous source attributed to %s: %+v", remote, e)
		}
	}
}

// Review 3: fixed category order, preserving source order within a category.
func TestCompareRound2RelativeFallbacks(t *testing.T) {
	d := newRepo(t, "r")
	addBareRemote(t, d, "josh")
	sh(t, d, "checkout", "-q", "-b", "feature")
	commit(t, d, "next", "next")
	r := compareResolver(t, d, compareDefaults())
	for _, tc := range []struct{ from, to, left, right string }{
		{"merge-base(main) | main", "merge-base(josh/main) | HEAD", "merge-base", "commit"},
		{"merge-base(missing) | main", "merge-base(josh/main) | HEAD", "commit", "merge-base"},
		{"merge-base(missing) | main", "merge-base(missing) | HEAD", "commit", "commit"},
	} {
		t.Run(tc.from+".."+tc.to, func(t *testing.T) {
			for range 2 {
				f, to := r.ResolvePair(tc.from, tc.to)
				if !f.Available || !to.Available || f.Kind != tc.left || to.Kind != tc.right {
					t.Fatalf("from %+v; to %+v", f, to)
				}
			}
		})
	}
}

// Review 4: a fake git executable probes child isolation without running a
// real fetch or pruning. The default SSH policy preserves user overrides.
func TestCompareRound2FetchNoPrompt(t *testing.T) {
	isolateCompare(t)
	d := newRepo(t, "r")
	realGit, err := exec.LookPath("git")
	if err != nil {
		t.Fatal(err)
	}
	python, err := exec.LookPath("python3")
	if err != nil {
		t.Fatal(err)
	}
	bin := t.TempDir()
	script := "#!" + python + "\nimport json,os,sys\nif 'fetch' in sys.argv[1:]:\n print(json.dumps({'pid':os.getpid(),'sid':os.getsid(0),'gcm':os.getenv('GCM_INTERACTIVE'),'ssh':os.getenv('GIT_SSH_COMMAND'),'args':sys.argv[1:]}))\nelse:\n os.execv(" + fmt.Sprintf("%q", realGit) + ",[" + fmt.Sprintf("%q", realGit) + "]+sys.argv[1:])\n"
	if err := os.WriteFile(filepath.Join(bin, "git"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("GCM_INTERACTIVE", "always")
	for _, tc := range []struct{ name, env, config, want string }{
		{"default", "", "", "ssh -o BatchMode=yes"},
		{"environment", "ssh -F fixture-env", "", "ssh -F fixture-env"},
		{"config", "", "ssh -F fixture-config", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("GIT_SSH_COMMAND", tc.env)
			sh(t, d, "config", "core.sshCommand", tc.config)
			out, err := git(context.Background(), writeTimeout, d, "fetch", "--prune", "--", "origin")
			if err != nil {
				t.Fatal(err)
			}
			var probe struct {
				PID  int      `json:"pid"`
				SID  int      `json:"sid"`
				GCM  string   `json:"gcm"`
				SSH  string   `json:"ssh"`
				Args []string `json:"args"`
			}
			if err := json.Unmarshal(out, &probe); err != nil {
				t.Fatal(err, string(out))
			}
			if probe.PID != probe.SID || probe.GCM != "never" || probe.SSH != tc.want || !slices.Contains(probe.Args, "credential.interactive=never") {
				t.Errorf("prompt isolation: %+v", probe)
			}
		})
	}
}

// Review 5: tables carry a concise reason; JSON retains git's exact error.
func TestCompareRound2Reasons(t *testing.T) {
	d := newRepo(t, "r")
	addBareRemote(t, d, "origin")
	sh(t, d, "checkout", "-q", "-b", "feature")
	r := compareResolver(t, d, compareDefaults())
	e := oneEndpoint(t, r, "origin/{branch}")
	if e.Reason != "origin has no feature branch (not pushed, or not fetched)" {
		t.Errorf("reason: %s", e.Reason)
	}
	b, err := json.Marshal(e)
	if err != nil {
		t.Fatal(err)
	}
	var data map[string]any
	if err := json.Unmarshal(b, &data); err != nil {
		t.Fatal(err)
	}
	if detail, _ := data["detail"].(string); !strings.Contains(detail, "rev-parse") {
		t.Errorf("missing raw detail: %s", b)
	}
	p := r.Show("", "origin/{branch}", "HEAD")[0]
	row := comparisonRow(p, time.Now())
	if strings.Contains(row.Cells[6], "git ") || strings.Contains(row.Cells[6], "fatal:") {
		t.Errorf("raw table state: %s", row.Cells[6])
	}
	for _, spec := range []string{"absent", sh(t, d, "rev-parse", "HEAD:README")} {
		e := oneEndpoint(t, r, spec)
		if e.Available || e.Reason == "" || strings.Contains(e.Reason, "git ") || strings.Contains(e.Reason, "fatal:") {
			t.Errorf("reason %s: %+v", spec, e)
		}
	}
}

// Review 6: endpoint specs remain in JSON and a separate low-priority column.
func TestCompareRound2ResolvedColumns(t *testing.T) {
	d := newRepo(t, "r")
	sh(t, d, "branch", "-m", "master")
	addBareRemote(t, d, "origin")
	sh(t, d, "branch", "--set-upstream-to=origin/master", "master")
	r := compareResolver(t, d, compareDefaults())
	p := r.Show("upstream", "", "")[0]
	row := comparisonRow(p, time.Now())
	if row.Cells[1] != "origin/master" || row.Cells[2] != "HEAD" {
		t.Errorf("columns: %v", row.Cells)
	}
	if p.From.Spec != "{upstream}" {
		t.Errorf("lost spec: %+v", p.From)
	}
	found := false
	for i, c := range columns["compare"] {
		if c.Title == "spec" {
			found = true
			if row.Cells[i] != "{upstream}..HEAD" {
				t.Errorf("spec column: %v", row.Cells)
			}
		}
	}
	if !found {
		t.Error("missing spec column")
	}
}

// Review 7: expanded remote names cannot be reinterpreted as endpoint syntax.
func TestCompareRound2RemoteSyntax(t *testing.T) {
	for _, name := range []string{"odd|remote", "odd(remote"} {
		t.Run(name, func(t *testing.T) {
			d := newRepo(t, "r")
			addBareRemote(t, d, name)
			r := compareResolver(t, d, compareDefaults())
			e := oneEndpoint(t, r, "{remote}/main")
			if e.Available || !strings.Contains(e.Reason, "remote name") {
				t.Errorf("remote was parsed as syntax: %+v", e)
			}
			pairs := r.Show("{remote}/main", "", "")
			if len(pairs) != 1 || pairs[0].Available || !strings.Contains(pairs[0].Reason, "remote name") {
				t.Errorf("pair: %+v", pairs)
			}
		})
	}
}

// Review 8: nested relative bases have a direct, actionable diagnostic.
func TestCompareRound2NestedRelative(t *testing.T) {
	d := newRepo(t, "r")
	e := oneEndpoint(t, compareResolver(t, d, compareDefaults()), "merge-base(fork-point(main))")
	if e.Available || e.Reason != "relative base must be a commit" {
		t.Fatalf("%+v", e)
	}
}

// Review 9: successful counts from an incomplete graph are labelled too.
func TestCompareRound2ShallowCounts(t *testing.T) {
	d := newRepo(t, "r")
	commit(t, d, "next", "next")
	clone := filepath.Join(t.TempDir(), "shallow")
	sh(t, d, "clone", "-q", "--depth=1", "file://"+d, clone)
	r := compareResolver(t, clone, compareDefaults())
	p := r.Show("", "HEAD", "HEAD")[0]
	if !p.Available || p.Ahead != 0 || p.Behind != 0 || !strings.Contains(p.State, "counts limited by shallow clone") {
		t.Fatalf("%+v", p)
	}
}

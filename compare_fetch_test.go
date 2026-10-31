package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

func TestCompareStaleness(t *testing.T) {
	d := newRepo(t, "r")
	addBareRemote(t, d, "origin")
	cfg := compareDefaults()
	r := compareResolver(t, d, cfg)
	ref := "refs/remotes/origin/main"
	logPath := filepath.Join(r.commonDir, "logs", ref)
	// Build a reflog update timestamp independently of the commit date.
	old := time.Now().Add(-48 * time.Hour).Truncate(time.Second).UTC()
	if err := os.Chtimes(r.fetchHead, old, old); err != nil {
		t.Fatal(err)
	}
	sha := sh(t, d, "rev-parse", "HEAD")
	line := fmt.Sprintf("%s %s Test <test@example.com> %d +0000\tfetch: fixture\n", sha, sha, old.Unix())
	if err := os.WriteFile(logPath, []byte(line), 0o644); err != nil {
		t.Fatal(err)
	}
	r = compareResolver(t, d, cfg)
	e := oneEndpoint(t, r, "origin/main")
	if !e.Available || e.Fetched == nil || !e.Fetched.Equal(old) || !e.Stale {
		t.Fatalf("reflog age: %+v", e)
	}
	cfg.StaleAfter = 0
	e = oneEndpoint(t, compareResolver(t, d, cfg), "origin/main")
	if e.Stale || e.Fetched == nil {
		t.Fatalf("disabled: %+v", e)
	}
	// An empty/missing reflog falls back to FETCH_HEAD's filesystem time.
	if err := os.WriteFile(logPath, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(r.fetchHead, old, old); err != nil {
		t.Fatal(err)
	}
	cfg.StaleAfter = 24 * time.Hour
	e = oneEndpoint(t, compareResolver(t, d, cfg), "origin/main")
	if e.Fetched == nil || !e.Fetched.Equal(old) || !e.Stale {
		t.Fatalf("FETCH_HEAD fallback: %+v", e)
	}
}

func TestCompareFetchLocalRemote(t *testing.T) {
	isolateCompare(t)
	d := newRepo(t, "r")
	bare := addBareRemote(t, d, "origin")
	otherBare := addBareRemote(t, d, "other")
	sh(t, d, "branch", "--set-upstream-to=origin/main", "main")
	// Advance local bare remotes via a private checkout; no network or push.
	writer := filepath.Join(t.TempDir(), "writer")
	sh(t, d, "clone", "-q", bare, writer)
	commit(t, writer, "new", "remote advanced")
	newSHA := sh(t, writer, "rev-parse", "HEAD")
	sh(t, bare, "fetch", "-q", writer, "main:main")
	sh(t, otherBare, "fetch", "-q", writer, "main:main")
	oldSHA := sh(t, d, "rev-parse", "origin/main")
	a, err := New()
	if err != nil {
		t.Fatal(err)
	}
	a.r.Config().Disable()
	// The destructive flag is asserted with an injectable runner. It delegates
	// just the non-pruning fetch to git for this local bare-remote integration.
	// Actual pruning remains fake-only per the lane's test rule.
	var called []string
	a.fetchRun = func(ctx context.Context, timeout time.Duration, dir string, args ...string) ([]byte, error) {
		if len(args) != 4 || args[0] != "fetch" || args[1] != "--prune" || args[2] != "--" {
			return nil, fmt.Errorf("unexpected fetch: %v", args)
		}
		called = append(called, args[3])
		return git(ctx, timeout, dir, "fetch", "--", args[3])
	}
	r := compareResolver(t, d, compareDefaults())
	sha := sh(t, d, "rev-parse", "HEAD")
	old := time.Now().Add(-48 * time.Hour).Unix()
	line := fmt.Sprintf("%s %s Test <test@example.com> %d +0000\tfixture\n", sha, sha, old)
	if err := os.WriteFile(r.reflogPath("refs/remotes/origin/main"), []byte(line), 0o644); err != nil {
		t.Fatal(err)
	}
	if !oneEndpoint(t, r, "origin/main").Stale {
		t.Fatal("fixture was not stale")
	}
	var out, errOut strings.Builder
	a.Stdout, a.Stderr = &out, &errOut
	if err := a.Shell().Run(context.Background(), []string{"compare", "fetch", "--pair", "upstream", "--repo", d}); err != nil {
		t.Fatalf("%v: %s", err, errOut.String())
	}
	result := decode[FetchResult](t, out.String())
	if !slices.Equal(called, []string{"origin"}) || sh(t, d, "rev-parse", "origin/main") != newSHA || sh(t, d, "rev-parse", "other/main") != oldSHA {
		t.Fatalf("scope: calls %v, result %+v", called, result)
	}
	if len(result.Comparisons) != 1 || result.Comparisons[0].Stale || result.Comparisons[0].Behind != 1 {
		t.Fatalf("after fetch: %+v", result)
	}
	if !strings.Contains(errOut.String(), "compare.fetch done") {
		t.Fatalf("no visible job: %s", errOut.String())
	}
	out.Reset()
	errOut.Reset()
	called = nil
	if err := a.Shell().Run(context.Background(), []string{"repo", "fetch", "--remote", "other", "--repo", d}); err != nil {
		t.Fatalf("%v: %s", err, errOut.String())
	}
	if !slices.Equal(called, []string{"other"}) || sh(t, d, "rev-parse", "other/main") != newSHA {
		t.Fatalf("repo scope: %v", called)
	}
	// Reads never invoke the fetch runner.
	called = nil
	out.Reset()
	errOut.Reset()
	if err := a.Shell().Run(context.Background(), []string{"compare", "list", "--repo", d}); err != nil {
		t.Fatal(err)
	}
	if len(called) != 0 {
		t.Fatalf("implicit fetch: %v", called)
	}
	// A failed explicit job retains the transport's reason.
	a.fetchRun = func(context.Context, time.Duration, string, ...string) ([]byte, error) {
		return nil, fmt.Errorf("fixture unavailable")
	}
	out.Reset()
	errOut.Reset()
	if err := a.Shell().Run(context.Background(), []string{"repo", "fetch", "--remote", "origin", "--repo", d}); err == nil || !strings.Contains(err.Error(), "fixture unavailable") {
		t.Fatalf("%v, %s", err, errOut.String())
	}
}

func TestCompareRelativeAliasesAndShallow(t *testing.T) {
	d := newRepo(t, "r")
	addBareRemote(t, d, "origin")
	addBareRemote(t, d, "upstream")
	cfg := compareDefaults()
	cfg.Endpoints = append(cfg.Endpoints, "remote-base={remote}/{main}", "cycle=cycle | HEAD")
	r := compareResolver(t, d, cfg)
	ends := r.Resolve("merge-base(remote-base)", "HEAD")
	if len(ends) != 2 {
		t.Fatalf("%+v", ends)
	}
	for _, e := range ends {
		if !e.Available || len(e.Remotes) != 1 {
			t.Fatalf("%+v", e)
		}
	}
	if e := oneEndpoint(t, r, "cycle"); !e.Available {
		t.Fatalf("alternation did not skip cyclic alias: %+v", e)
	}
	ends = r.Resolve("merge-base(main)", "{remote}/main")
	if len(ends) != 2 {
		t.Fatalf("remote other side: %+v", ends)
	}
	for _, e := range ends {
		if !e.Available {
			t.Fatalf("remote other side: %+v", e)
		}
	}
	if len(r.Show("{remote}/main", "", "")) != 2 || len(r.Show("origin/main", "", "")) != 1 {
		t.Fatal("saved versus expanded pair selection")
	}
	// A shallow boundary hides the parent and adds
	// its diagnostic instead of silently substituting another base.
	sh(t, d, "branch", "old")
	commit(t, d, "next", "next")
	r = compareResolver(t, d, cfg)
	p := r.Show("", "merge-base(old) | HEAD", "HEAD")[0]
	if !p.Available || p.From.Kind != "merge-base" || p.Ahead != 1 {
		t.Fatalf("relative alternation priority: %+v", p)
	}
	clone := filepath.Join(t.TempDir(), "shallow")
	sh(t, d, "clone", "-q", "--depth=1", "file://"+d, clone)
	sh(t, clone, "fetch", "-q", "--depth=1", "origin", "old:refs/heads/old")
	r = compareResolver(t, clone, compareDefaults())
	e := oneEndpoint(t, r, "merge-base(old)")
	if e.Available || !strings.Contains(e.Reason, "shallow clone") {
		t.Fatalf("%+v", e)
	}
}

func TestCompareRelativeMemoAndTimeout(t *testing.T) {
	d := newRepo(t, "r")
	sh(t, d, "branch", "base")
	commit(t, d, "next", "next")
	cache := &CompareCache{}
	calls := 0
	runner := func(ctx context.Context, timeout time.Duration, dir string, args ...string) ([]byte, error) {
		if args[0] == "merge-base" {
			calls++
		}
		return git(ctx, timeout, dir, args...)
	}
	for range 2 {
		r, err := newEndpointResolver(context.Background(), d, compareDefaults(), time.Now(), cache, runner)
		if err != nil {
			t.Fatal(err)
		}
		if !oneEndpoint(t, r, "fork-point(base)").Available {
			t.Fatal("missing fork point")
		}
	}
	if calls != 1 {
		t.Fatalf("fork memo: %d", calls)
	}
	r := compareResolver(t, d, compareDefaults())
	r.run = func(ctx context.Context, timeout time.Duration, dir string, args ...string) ([]byte, error) {
		if args[0] == "merge-base" {
			return nil, &GitError{Err: fmt.Errorf("timed out after %s", readTimeout)}
		}
		return git(ctx, timeout, dir, args...)
	}
	e := oneEndpoint(t, r, "merge-base(base)")
	if e.Available || e.Reason != "merge-base timed out" {
		t.Fatalf("%+v", e)
	}
}

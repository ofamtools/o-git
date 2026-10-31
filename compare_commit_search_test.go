package main

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"ofam.dev/ochazuke"
	ocli "ofam.dev/ochazuke/cli"
)

func TestCompareCommitSearchStory(t *testing.T) {
	a, dir := compareStory(t)
	commit(t, dir, "needle", "unlisted subject needle")
	sha := sh(t, dir, "rev-parse", "HEAD")
	keys(t, a, "f")
	c := a.r.Chooser("compare-side")
	c.SetQuery("unlisted subject needle")
	if err := a.Settle(); err != nil {
		t.Fatal(err)
	}
	row, ok := c.Highlighted()
	choice, _ := row.Data.(sideChoice)
	if !ok || choice.Spec != sha || !strings.Contains(row.Label, "unlisted subject needle") {
		t.Fatalf("subject completion: %+v", row)
	}
	compareFrame(t, a, "compare-commit-search")
	keys(t, a, "enter")
	if got := a.compare.custom[dir].From; got != sha {
		t.Fatalf("picked %q, want full SHA %q", got, sha)
	}
}

// Plumbing one pending command at a time lets the test see each batch before
// the next chunk runs, without timing assumptions or sleeps.
func deliverSearchCommand(t *testing.T, r *ochazuke.Runtime, cmd tea.Cmd) {
	t.Helper()
	if cmd == nil {
		t.Fatal("missing pending search")
	}
	switch msg := cmd().(type) {
	case tea.BatchMsg:
		for _, c := range msg {
			deliverSearchCommand(t, r, c)
		}
	default:
		if _, err := r.Deliver(msg); err != nil {
			t.Fatal(err)
		}
	}
}

func commitSearchRepo(t *testing.T) (string, []string) {
	t.Helper()
	isolateCompare(t)
	dir := newRepo(t, "search")
	tree := sh(t, dir, "rev-parse", "HEAD^{tree}")
	commits := []string{sh(t, dir, "rev-parse", "HEAD")}
	for i := 1; i < 150; i++ {
		sha := sh(t, dir, "commit-tree", tree, "-p", commits[len(commits)-1], "-m", fmt.Sprintf("history needle %03d", i))
		commits = append(commits, sha)
	}
	sh(t, dir, "update-ref", "refs/heads/main", commits[len(commits)-1])
	return dir, commits
}

func searchFixture(t *testing.T, dir, head string, run gitRunner) (*ochazuke.Runtime, *ochazuke.Chooser, *[]int) {
	t.Helper()
	r := ochazuke.NewRuntime()
	_ = ochazuke.NewModel(r)
	var resume *commitSearch
	var skips []int
	if run == nil {
		run = git
	}
	wrapped := func(ctx context.Context, timeout time.Duration, dir string, args ...string) ([]byte, error) {
		var skip int
		if _, err := fmt.Sscanf(args[2], "--skip=%d", &skip); err != nil {
			return nil, err
		}
		skips = append(skips, skip)
		return run(ctx, timeout, dir, args...)
	}
	c, err := r.OpenChooser(ochazuke.ChooserSpec{Kind: "search-test", Sources: []ochazuke.ChooserSource{{Name: "history", Live: true,
		Load: func(q string) ochazuke.Result {
			if q == "" {
				return ochazuke.Done([]ochazuke.ChooserRow(nil))
			}
			if resume != nil && resume.query == q {
				s := resume
				resume = nil
				return ochazuke.Done(ochazuke.ChooserBatch{Rows: s.rows, More: s.load()})
			}
			return (commitSearch{dir: dir, head: head, query: q, depth: 100, run: wrapped}).load()
		},
		Open: func(row ochazuke.ChooserRow) (string, bool) {
			resume = row.Data.(sideChoice).Search
			return resume.query, true
		},
	}}})
	if err != nil {
		t.Fatal(err)
	}
	return r, c, &skips
}

func TestCommitSearchChunksPrefixAndMore(t *testing.T) {
	dir, commits := commitSearchRepo(t)
	t.Run("prefix within depth", func(t *testing.T) {
		r, c, skips := searchFixture(t, dir, commits[149], nil)
		c.SetQuery(strings.ToUpper(commits[120][:8]))
		if err := ocli.Settle(r, nil); err != nil {
			t.Fatal(err)
		}
		rows := c.Matches()
		if len(rows) != 2 || rows[0].Data.(sideChoice).Spec != commits[120] || !rows[1].NoPick {
			t.Fatalf("rows %+v", rows)
		}
		if fmt.Sprint(*skips) != "[0 25 50 75]" {
			t.Fatal(*skips)
		}
	})
	t.Run("no match then continue", func(t *testing.T) {
		r, c, skips := searchFixture(t, dir, commits[149], nil)
		c.SetQuery("history needle 020")
		if err := ocli.Settle(r, nil); err != nil {
			t.Fatal(err)
		}
		rows := c.Matches()
		if len(rows) != 1 || rows[0].Label != "Search 100 more commits…" {
			t.Fatalf("rows %+v", rows)
		}
		if _, err := r.Execute(ochazuke.Request{Noun: "ochazuke.chooser", Verb: "pick"}); err != nil {
			t.Fatal(err)
		}
		if err := ocli.Settle(r, nil); err != nil {
			t.Fatal(err)
		}
		rows = c.Matches()
		if len(rows) != 1 || rows[0].Data.(sideChoice).Spec != commits[20] {
			t.Fatalf("rows %+v", rows)
		}
		if fmt.Sprint(*skips) != "[0 25 50 75 100 125 150]" {
			t.Fatal(*skips)
		}
	})
	t.Run("stream parts", func(t *testing.T) {
		r, c, _ := searchFixture(t, dir, commits[149], nil)
		c.SetQuery("history needle")
		for _, want := range []int{25, 50, 75, 101} {
			deliverSearchCommand(t, r, r.PendingCommands())
			if got := len(c.Matches()); got != want {
				t.Fatalf("rows %d want %d", got, want)
			}
			if c.Loading() != (want != 101) {
				t.Fatalf("loading %v at %d", c.Loading(), want)
			}
		}
		// The first block's results survive navigation into the next block.
		for range 100 {
			if _, err := r.Execute(ochazuke.Request{Noun: "ochazuke.chooser", Verb: "move", Args: ochazuke.Arguments{"delta": 1}}); err != nil {
				t.Fatal(err)
			}
		}
		if _, err := r.Execute(ochazuke.Request{Noun: "ochazuke.chooser", Verb: "pick"}); err != nil {
			t.Fatal(err)
		}
		if err := ocli.Settle(r, nil); err != nil {
			t.Fatal(err)
		}
		if got := len(c.Matches()); got != 149 {
			t.Fatalf("accumulated rows %d", got)
		}
	})
}

func TestCommitSearchQueryCancelsAndRejectsStaleRows(t *testing.T) {
	dir, commits := commitSearchRepo(t)
	started := make(chan context.Context, 1)
	release := make(chan struct{})
	run := func(ctx context.Context, timeout time.Duration, dir string, args ...string) ([]byte, error) {
		started <- ctx
		<-release
		// Deliberately return stale rows after cancellation: they must not land.
		return []byte(commits[149] + "\x00short\x00stale subject\x00Test\x002026-10-31\n"), nil
	}
	r, c, _ := searchFixture(t, dir, commits[149], run)
	c.SetQuery("stale subject")
	cmd := r.PendingCommands()
	done := make(chan tea.Msg, 1)
	go func() { done <- cmd() }()
	var ctx context.Context
	select {
	case ctx = <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("search did not start")
	}
	c.SetQuery("fresh query")
	select {
	case <-ctx.Done():
	case <-time.After(5 * time.Second):
		t.Fatal("query edit did not cancel")
	}
	close(release)
	msg := <-done
	if _, err := r.Deliver(msg); err != nil {
		t.Fatal(err)
	}
	if len(c.Matches()) != 0 {
		t.Fatal("stale rows landed", c.Matches())
	}
	// Closing also cancels the replacement before its Git call starts.
	if _, err := r.Execute(ochazuke.Request{Noun: "ochazuke.chooser", Verb: "cancel"}); err != nil {
		t.Fatal(err)
	}
}

func TestCompareCommitSearchSetting(t *testing.T) {
	a, _ := compareStory(t)
	if a.r.Settings().Int("compare.commit-search-depth") != 100 {
		t.Fatal("default depth")
	}
	if _, err := a.r.Settings().SetSession("compare.commit-search-depth", -1); err == nil {
		t.Fatal("negative depth accepted")
	}
	if _, err := a.r.Settings().SetSession("compare.commit-search-depth", 0); err != nil {
		t.Fatal(err)
	}
	keys(t, a, "f")
	c := a.r.Chooser("compare-side")
	c.SetQuery("nonexistent commit subject")
	if err := a.Settle(); err != nil {
		t.Fatal(err)
	}
	for _, row := range c.Matches() {
		if choice, ok := row.Data.(sideChoice); ok && choice.Search != nil {
			t.Fatal("search enabled at depth zero")
		}
	}
}

func TestCompareCommitSearchContinuesRepeatedly(t *testing.T) {
	dir, commits := commitSearchRepo(t)
	a := open(t, dir)
	a.r.Config().StateDir = t.TempDir()
	storyRequest(t, a, "open", nil)
	if _, err := a.r.Settings().SetSession("compare.commit-search-depth", 25); err != nil {
		t.Fatal(err)
	}
	keys(t, a, "t")
	c := a.r.Chooser("compare-side")
	c.SetQuery("history needle 020")
	if err := a.Settle(); err != nil {
		t.Fatal(err)
	}
	for block := 0; block < 5; block++ {
		rows := c.Matches()
		at := -1
		for i, row := range rows {
			if choice, ok := row.Data.(sideChoice); ok && choice.Search != nil {
				at = i
				if choice.Search.skip != (block+1)*25 {
					t.Fatal("wrong search offset", choice.Search.skip)
				}
			}
		}
		if at < 0 {
			t.Fatal("missing continuation", rows)
		}
		if _, err := a.r.Execute(ochazuke.Request{Noun: "ochazuke.chooser", Verb: "move", Args: ochazuke.Arguments{"delta": at}}); err != nil {
			t.Fatal(err)
		}
		keys(t, a, "enter")
		if a.r.Chooser("compare-side") != c {
			t.Fatal("continuation closed chooser")
		}
	}
	row, ok := c.Highlighted()
	if !ok || row.Data.(sideChoice).Spec != commits[20] {
		t.Fatal("older commit not found", row)
	}
	keys(t, a, "enter")
	if got := a.compare.custom[dir].To; got != commits[20] {
		t.Fatal("endpoint is not full SHA", got)
	}
}

package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/unixsuperhero/ochazuke"
)

// These are app-page stories: o-story's pages fixture builds synthetic pages,
// whereas these dispatch real bindings and settle real pending git reads.
func compareStory(t *testing.T) (*App, string) {
	t.Helper()
	isolateCompare(t)
	t.Setenv("GIT_AUTHOR_DATE", "2026-10-31T12:00:00Z")
	t.Setenv("GIT_COMMITTER_DATE", "2026-10-31T12:00:00Z")
	dir := newRepo(t, "story")
	sh(t, dir, "switch", "-c", "topic")
	commit(t, dir, "next", "feat: next")
	write(t, dir, "README", "changed\n")
	a := open(t, dir)
	a.r.Config().StateDir = filepath.Join(t.TempDir(), "state")
	a.r.Config().Dir = filepath.Join(t.TempDir(), "config")
	a.Now = func() time.Time { return time.Date(2026, 10, 31, 13, 0, 0, 0, time.UTC) }
	storyRequest(t, a, "open", ochazuke.Arguments{"repo": dir})
	return a, dir
}

func storyRequest(t *testing.T, a *App, verb string, args ochazuke.Arguments) {
	t.Helper()
	if _, err := a.r.Execute(ochazuke.Request{Noun: "compare", Verb: verb, Args: args}); err != nil {
		t.Fatal(err)
	}
	if err := a.Settle(); err != nil {
		t.Fatal(err)
	}
}

func compareFrame(t *testing.T, a *App, name string) {
	t.Helper()
	// Substitute only presentation data before layout. Scrubbing a wrapped temp
	// path afterwards makes goldens depend on MkdirTemp's suffix length.
	repo := a.compare.repo
	a.compare.repo = "/fixture/story"
	defer func() { a.compare.repo = repo }()
	if chooser := a.r.Chooser("compare-side"); chooser != nil {
		original := chooser.Matches()
		display := slices.Clone(original)
		for i := range display {
			display[i].Label = strings.ReplaceAll(display[i].Label, repo, "/fixture/story")
			display[i].Cells = slices.Clone(display[i].Cells)
			for j := range display[i].Cells {
				display[i].Cells[j] = strings.ReplaceAll(display[i].Cells[j], repo, "/fixture/story")
			}
			if value, ok := display[i].Data.(sideChoice); ok {
				value.Spec = strings.ReplaceAll(value.Spec, repo, "/fixture/story")
				display[i].Data = value
			}
		}
		chooser.SetRows(display)
		defer chooser.SetRows(original)
	}
	for _, width := range []int{80, 120, 200} {
		a.m.Width, a.m.Height = width, 40
		frame := screen(a)
		// Stable fixture contents; scrub ephemeral test paths and commit hashes.
		frame = regexp.MustCompile(`/private/var/folders/[^\s│]*|/tmp/[^\s│]*`).ReplaceAllString(frame, "<tmp>")
		frame = regexp.MustCompile(`[0-9a-f]{7,40}`).ReplaceAllString(frame, "<sha>")
		lines := strings.Split(frame, "\n")
		for i := range lines {
			lines[i] = strings.TrimRight(lines[i], " ")
		}
		frame = strings.Join(lines, "\n")
		file := filepath.Join("testdata", "compare", fmt.Sprintf("%s-%d.txt", name, width))
		if os.Getenv("O_GIT_UPDATE_FRAMES") == "1" {
			if err := os.MkdirAll(filepath.Dir(file), 0755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(file, []byte(frame), 0644); err != nil {
				t.Fatal(err)
			}
		}
		want, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		if frame != string(want) {
			if out := os.Getenv("O_GIT_FRAME_DIR"); out != "" {
				_ = os.WriteFile(filepath.Join(out, fmt.Sprintf("%s-%d.actual", name, width)), []byte(frame), 0644)
			}
			t.Fatalf("frame differs: %s (O_GIT_UPDATE_FRAMES=1 to update)", file)
		}
		if out := os.Getenv("O_GIT_FRAME_DIR"); out != "" && width == 120 {
			if err := os.WriteFile(filepath.Join(out, name+"-120.txt"), []byte(frame), 0644); err != nil {
				t.Fatal(err)
			}
		}
	}
}

func TestComparePageStories(t *testing.T) {
	t.Run("git/compare-pairs", func(t *testing.T) {
		a, _ := compareStory(t)
		list := a.r.List("compare/pairs")
		if list == nil {
			t.Fatal("pairs missing")
		}
		var names []string
		unavailable, collapsed, counted := false, false, false
		for _, row := range list.Items() {
			p := row.Data.(Comparison)
			names = append(names, p.Names...)
			unavailable = unavailable || !p.Available && p.Reason != ""
			collapsed = collapsed || len(p.Names) > 1
			counted = counted || p.Ahead > 0 || p.Behind > 0
		}
		for _, entry := range defaultPairs {
			name, _, _ := strings.Cut(entry, "=")
			if !slices.Contains(names, name) {
				t.Errorf("missing default %s: %v", name, names)
			}
		}
		if !counted {
			t.Fatal("nonzero ahead/behind counts missing")
		}
		if !unavailable || !collapsed {
			t.Fatalf("unavailable %v collapsed %v", unavailable, collapsed)
		}
		row, _ := list.Selected()
		if !slices.Contains(row.Data.(Comparison).Names, "dirty") {
			t.Fatal("initial pair is not dirty")
		}
		compareFrame(t, a, "compare-pairs")
	})
	t.Run("git/compare-pick-side", func(t *testing.T) {
		a, _ := compareStory(t)
		for _, key := range []string{"f", "t"} {
			keys(t, a, key)
			c := a.r.Chooser("compare-side")
			if c == nil {
				t.Fatal("no chooser")
			}
			if !slices.Equal(c.Sources(), []string{"Endpoints", "Log", "All", "Refs", "Graph", "Worktrees"}) {
				t.Fatal(c.Sources())
			}
			for _, source := range c.Sources() {
				if c.Source() != source {
					t.Fatal(c.Source())
				}
				compareFrame(t, a, "compare-pick-"+key+"-"+strings.ToLower(source))
				keys(t, a, "tab")
			}
			keys(t, a, "esc")
		}
		keys(t, a, "f")
		typeText(t, a, "merge-base(topic)")
		keys(t, a, "enter")
		row, _ := a.r.List("compare/pairs").Selected()
		p := row.Data.(Comparison)
		if !strings.HasPrefix(p.Name, "(custom)") || p.From.Spec != "merge-base(topic)" {
			t.Fatalf("%+v", p)
		}
		compareFrame(t, a, "compare-pick-side")
	})
	t.Run("git/compare-swap", func(t *testing.T) {
		a, _ := compareStory(t)
		keys(t, a, "~")
		row, _ := a.r.List("compare/pairs").Selected()
		p := row.Data.(Comparison)
		if p.From.Spec != "WORKTREE" || p.To.Spec != "HEAD" {
			t.Fatalf("%+v", p)
		}
		compareFrame(t, a, "compare-swap")
	})
	t.Run("git/compare-fetch", func(t *testing.T) {
		a, dir := compareStory(t)
		sh(t, dir, "remote", "add", "origin", "/not-used")
		sh(t, dir, "update-ref", "refs/remotes/origin/main", sh(t, dir, "rev-parse", "HEAD"))
		calls := 0
		a.fetchRun = func(_ context.Context, _ time.Duration, _ string, args ...string) ([]byte, error) {
			calls++
			if !slices.Equal(args, []string{"fetch", "--prune", "--", "origin"}) {
				t.Errorf("fetch args %v", args)
			}
			return nil, nil
		}
		now := a.Now().Add(72 * time.Hour)
		a.Now = func() time.Time { return now }
		storyRequest(t, a, "set", ochazuke.Arguments{"side": "from", "endpoint": "origin/main"})
		row, _ := a.r.List("compare/pairs").Selected()
		if !row.Data.(Comparison).Stale {
			t.Fatal("fixture should be stale")
		}
		if calls != 0 {
			t.Fatal("implicit fetch")
		}
		if _, err := a.r.Dispatch(ochazuke.MacroKey{Key: "F"}); err != nil {
			t.Fatal(err)
		}
		if len(a.r.Jobs()) == 0 {
			t.Fatal("fetch did not start a job")
		}
		if err := a.Settle(); err != nil {
			t.Fatal(err)
		}
		row, _ = a.r.List("compare/pairs").Selected()
		if calls != 1 {
			t.Fatalf("fetch calls %d", calls)
		}
		if row.Data.(Comparison).Stale {
			t.Fatal("fetch left stale tone")
		}
		if row.Data.(Comparison).Fetched == nil {
			t.Fatal("fetch age missing")
		}
		compareFrame(t, a, "compare-fetch")
	})
	t.Run("git/compare-graph-wrap", func(t *testing.T) {
		t.Skip("gap: ochazuke v0.68.0 chooser has no Wrap column API; connectors are in preview")
	})
	t.Run("git/compare-open-diff", func(t *testing.T) { t.Skip("gap: Phase B waits for compare-lists and compare-diff") })
}

func TestComparePagePersistsCustomAndSavesThroughSettings(t *testing.T) {
	a, dir := compareStory(t)
	original := a.r.Settings().Strings("compare.pairs")
	storyRequest(t, a, "set", ochazuke.Arguments{"side": "from", "endpoint": "not-a-ref"})
	row, _ := a.compare.pairs.Selected()
	if p := row.Data.(Comparison); p.Available || p.Reason == "" {
		t.Fatalf("typed unavailable spec lost: %+v", p)
	}
	if !slices.Equal(original, a.r.Settings().Strings("compare.pairs")) {
		t.Fatal("editing a side changed settings")
	}
	state, err := readCustomPairs(a.r.Config().StateDirectory())
	if err != nil || state[dir].From != "not-a-ref" {
		t.Fatalf("state: %+v %v", state, err)
	}
	storyRequest(t, a, "reload", nil)
	row, _ = a.compare.pairs.Selected()
	if row.Data.(Comparison).From.Spec != "not-a-ref" {
		t.Fatal("reload lost custom row")
	}
	storyRequest(t, a, "save", ochazuke.Arguments{"name": "review"})
	if !slices.Contains(a.r.Settings().Strings("compare.pairs"), "review=not-a-ref..WORKTREE") {
		t.Fatal(a.r.Settings().Strings("compare.pairs"))
	}
	b, err := New()
	if err != nil {
		t.Fatal(err)
	}
	b.r.Config().StateDir = a.r.Config().StateDir
	b.r.Config().Dir = a.r.Config().Dir
	storyRequest(t, b, "open", ochazuke.Arguments{"repo": dir})
	found := false
	for _, row := range b.compare.pairs.Items() {
		p := row.Data.(Comparison)
		if strings.HasPrefix(p.Name, "(custom)") {
			found = true
			if p.From.Spec != "not-a-ref" {
				t.Fatal(p)
			}
		}
	}
	if !found {
		t.Fatal("new app lost custom row")
	}
}

func TestComparePageEntryPointsAndFollow(t *testing.T) {
	a, dir := compareStory(t)
	if err := a.r.OpenPage(pageID); err != nil {
		t.Fatal(err)
	}
	for _, list := range []*ochazuke.List{a.repos, a.worktrees, a.branches} {
		if err := list.Focus(); err != nil {
			t.Fatal(err)
		}
		row, _ := list.Selected()
		keys(t, a, "c")
		if a.r.Page != comparePageID {
			t.Fatal("c did not open Compare")
		}
		selected, _ := a.compare.pairs.Selected()
		p := selected.Data.(Comparison)
		switch row.Data.(type) {
		case Repo:
			if p.To.Spec != "HEAD" {
				t.Fatal(p)
			}
		case WorktreeRow:
			if p.To.Spec != "worktree:"+dir {
				t.Fatal(p)
			}
		case BranchRow:
			if p.To.Spec != row.Data.(BranchRow).Name {
				t.Fatal(p)
			}
		}
		if err := a.r.OpenPage(pageID); err != nil {
			t.Fatal(err)
		}
	}
	storyRequest(t, a, "open", ochazuke.Arguments{"repo": dir})
	if a.followCompare() {
		t.Fatal("settled row followed twice")
	}
	if err := a.compare.pairs.Focus(); err != nil {
		t.Fatal(err)
	}
	if _, err := a.r.Dispatch(ochazuke.MacroKey{Key: "j"}); err != nil {
		t.Fatal(err)
	}
	if !a.followCompare() {
		t.Fatal("pair move started no read")
	}
	if a.followCompare() {
		t.Fatal("pair move started two reads")
	}
	if err := a.Settle(); err != nil {
		t.Fatal(err)
	}
	row, _ := a.compare.pairs.Selected()
	if a.compare.shown == nil || a.compare.shown.From.Spec != row.Data.(Comparison).From.Spec {
		t.Fatal("detail did not follow")
	}
	keys(t, a, "enter")
	if !a.compare.commits.Focused() {
		t.Fatal("enter did not focus commits")
	}
}

func TestComparePageCopyCustom(t *testing.T) {
	a, dir := compareStory(t)
	storyRequest(t, a, "set", ochazuke.Arguments{"side": "to", "endpoint": "topic"})
	keys(t, a, "y", "y")
	sha := sh(t, dir, "rev-parse", "HEAD")
	if got := a.r.Registers().Named["\""].Yank.Text; got != sha+".."+sha {
		t.Fatalf("copied %q", got)
	}
}

func TestComparePageHiddenAndCommand(t *testing.T) {
	a, dir := compareStory(t)
	a.r.EnsureConfigSettings()
	if err := a.r.OpenPage(pageID); err != nil {
		t.Fatal(err)
	}
	if _, err := a.r.Settings().SetSession("pages.hidden", []string{comparePageID}); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(screen(a), "› Compare") {
		t.Fatal("hidden page still in tabs")
	}
	out := run(t, "", "view", "--page", "compare", "--path", dir, "--smoke", "--width", "120")
	if !strings.Contains(out, "Pairs") || !strings.Contains(out, "Phase B: commits") {
		t.Fatal(out)
	}
}

func TestComparePageSourcesKeepGitOrder(t *testing.T) {
	isolateCompare(t)
	dir := newRepo(t, "graph")
	sh(t, dir, "switch", "-c", "feature")
	commit(t, dir, "feature", "feature")
	sh(t, dir, "switch", "main")
	commit(t, dir, "main", "main work")
	sh(t, dir, "merge", "--no-ff", "feature", "-m", "merge")
	r := compareResolver(t, dir, compareDefaults())
	for _, source := range []string{"Log", "All", "Graph"} {
		rows, err := loadSideChoices(r, source, "HEAD")
		if err != nil {
			t.Fatal(err)
		}
		args := []string{"log", "--format=%H"}
		switch source {
		case "Log":
			args = append(args, "HEAD")
		case "All":
			args = append(args, "--all")
		case "Graph":
			args = append(args, "--all", "--topo-order")
		}
		want := strings.Fields(sh(t, dir, args...))
		var got []string
		connectors := false
		for _, row := range rows {
			choice := row.Data.(sideChoice)
			got = append(got, choice.Spec)
			connectors = connectors || strings.Contains(choice.Graph, "\n")
		}
		if !slices.Equal(got, want) {
			t.Fatalf("%s order %v want %v", source, got, want)
		}
		if source == "Graph" && !connectors {
			t.Fatal("connector lines did not stay with a commit")
		}
	}
}

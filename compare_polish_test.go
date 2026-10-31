package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"ofam.dev/ochazuke"
)

func TestComparePolishStories(t *testing.T) {
	t.Run("git/compare-spill-order", func(t *testing.T) {
		a, _ := compareStory(t)
		selectComparePair(t, a, "main")
		a.m.Width, a.m.Height = 120, 40
		_ = screen(a)
		rows := a.compare.pairs.Items()
		focusRow(t, a, a.compare.pairs, rows[len(rows)-1].ID)
		keys(t, a, "j")
		if !a.compare.commits.Focused() {
			t.Fatal("last pair did not spill into Commits")
		}
		keys(t, a, "k")
		if !a.compare.pairs.Focused() {
			t.Fatal("first commit did not spill back to Pairs")
		}
		compareFrame(t, a, "compare-spill-order")
	})
	t.Run("git/compare-chooser-tab-cycles-match", func(t *testing.T) {
		a, _ := compareStory(t)
		if _, err := a.r.Settings().SetSession("compare.commit-search-depth", 0); err != nil {
			t.Fatal(err)
		}
		keys(t, a, "f")
		c := a.r.Chooser("compare-side")
		mode, source := c.MatchMode(), c.Source()
		keys(t, a, "tab")
		if c.MatchMode() == mode || c.Source() != source {
			t.Fatal("tab did not cycle only the match mode")
		}
		keys(t, a, "ctrl+o")
		if c.Source() == source {
			t.Fatal("standard source key did not step")
		}
		compareFrame(t, a, "compare-chooser-tab-cycles-match")
	})
	t.Run("git/compare-commit-files-follow", func(t *testing.T) {
		a, dir := compareStory(t)
		commit(t, dir, "third", "third")
		storyRequest(t, a, "reload", nil)
		selectComparePair(t, a, "main")
		keys(t, a, "enter")
		first, _ := a.compare.commits.Selected()
		if a.compare.fileCommit != first.Data.(Commit).SHA {
			t.Fatal("Files did not follow initial commit focus")
		}
		keys(t, a, "j")
		next, _ := a.compare.commits.Selected()
		if a.compare.fileCommit != next.Data.(Commit).SHA || a.compare.fileCommit == first.Data.(Commit).SHA {
			t.Fatal("Files did not follow commit motion")
		}
		keys(t, a, "enter", "esc")
		if a.compare.fileCommit != "" {
			t.Fatal("Escape did not restore pair files")
		}
		_ = a.compare.commits.Focus()
		if err := a.Settle(); err != nil {
			t.Fatal(err)
		}
		_ = a.compare.pairs.Focus()
		if err := a.Settle(); err != nil {
			t.Fatal(err)
		}
		if a.compare.fileCommit != "" {
			t.Fatal("Pairs retained commit breadcrumb")
		}
		compareFrame(t, a, "compare-commit-files-follow")
	})
	t.Run("git/compare-diff-files-xy", func(t *testing.T) {
		a, dir := compareStory(t)
		commit(t, dir, "old.txt", "rename source")
		sh(t, dir, "mv", "old.txt", "renamed.txt")
		storyRequest(t, a, "reload", nil)
		_ = a.compare.files.Focus()
		keys(t, a, "enter")
		for _, row := range a.compare.diffFiles.Items() {
			if file := row.Data.(Change); file.OldPath != "" && !strings.Contains(row.Cells[1], file.OldPath+" → ") {
				t.Fatal("rename source lost", row.Cells)
			}
		}
		for _, width := range []int{80, 120, 200} {
			a.m.Width, a.m.Height = width, 40
			frame := screen(a)
			if !strings.Contains(frame, "● README") || strings.Contains(frame, "status") {
				t.Fatalf("file columns at %d:\n%s", width, frame)
			}
			rows := a.compare.diffFiles.Items()
			marked := false
			for _, row := range rows {
				marked = marked || strings.Contains(strings.Join(row.Cells, " "), "●")
			}
			if !marked {
				t.Fatal("open file has no marker")
			}
		}
		compareFrame(t, a, "compare-diff-files-xy")
	})
	for _, unresolved := range []bool{false, true} {
		name := "git/compare-fetch-collapsed-group"
		if unresolved {
			name = "git/compare-fetch-unresolved-remote"
		}
		t.Run(name, func(t *testing.T) {
			a, dir := compareStory(t)
			sha := sh(t, dir, "rev-parse", "main")
			for _, remote := range []string{"origin", "upstream"} {
				addBareRemote(t, dir, remote)
				sh(t, dir, "update-ref", "refs/remotes/"+remote+"/main", sha)
			}
			storyRequest(t, a, "reload", nil)
			if unresolved {
				storyRequest(t, a, "set", ochazuke.Arguments{"side": "from", "endpoint": "upstream/missing"})
			} else {
				selectComparePair(t, a, "main")
			}
			var remotes []string
			a.fetchRun = func(_ context.Context, _ time.Duration, _ string, args ...string) ([]byte, error) {
				remotes = append(remotes, args[len(args)-1])
				return nil, nil
			}
			keys(t, a, "F")
			want := []string{"origin", "upstream"}
			if unresolved {
				want = []string{"upstream"}
			}
			slices.Sort(remotes)
			if !slices.Equal(remotes, want) {
				t.Fatalf("remotes %v want %v", remotes, want)
			}
			compareFrame(t, a, strings.TrimPrefix(name, "git/"))
		})
	}
	t.Run("git/compare-detail-plain-reason", func(t *testing.T) {
		a, _ := compareStory(t)
		storyRequest(t, a, "set", ochazuke.Arguments{"side": "from", "endpoint": "not-a-ref"})
		detail := a.compareDetail()
		if strings.Contains(detail, "git rev-parse") || strings.Contains(detail, "fatal:") || !strings.Contains(detail, "check") {
			t.Fatal(detail)
		}
		compareFrame(t, a, "compare-detail-plain-reason")
	})
	t.Run("git/compare-fetch-failure-message", func(t *testing.T) {
		err := FetchRemotes(context.Background(), t.TempDir(), []string{"origin"}, []string{"origin"}, func(context.Context, time.Duration, string, ...string) ([]byte, error) {
			return nil, &GitError{Args: []string{"fetch", "--prune", "--", "origin"}, Stderr: "fatal: '../gone-origin.git' does not appear to be a git repository\nfatal: Could not read from remote repository.", Err: errors.New("exit status 128")}
		}, nil)
		if err == nil || strings.Contains(err.Error(), "git fetch") || strings.Contains(err.Error(), "fatal:") || !strings.Contains(err.Error(), "check git remote -v") || !strings.Contains(err.Error(), "../gone-origin.git") {
			t.Fatal(err)
		}
	})
	t.Run("git/compare-pairs-width", func(t *testing.T) {
		a, _ := compareStory(t)
		selectComparePair(t, a, "main")
		for _, width := range []int{120, 200} {
			a.m.Width, a.m.Height = width, 40
			frame := screen(a)
			p, _ := a.selectedComparison()
			if !strings.Contains(frame, "author") || !strings.Contains(frame, fmt.Sprintf("+%d", len(p.Names)-1)) {
				t.Fatalf("width %d:\n%s", width, frame)
			}
		}
		compareFrame(t, a, "compare-pairs-width")
	})
	t.Run("git/compare-side-hints", func(t *testing.T) {
		a, _ := compareStory(t)
		frame := screen(a)
		if !strings.Contains(frame, "f from") || !strings.Contains(frame, "t to") {
			t.Fatal(frame)
		}
		compareFrame(t, a, "compare-side-hints")
	})
	t.Run("git/compare-no-remotes", func(t *testing.T) {
		a, _ := compareStory(t)
		count := 0
		for _, row := range a.compare.pairs.Items() {
			p := row.Data.(Comparison)
			if strings.Contains(p.Reason, "no remotes") {
				count++
			}
			if !p.From.Available && row.Cells[1] != "-" {
				t.Fatalf("unresolved cell %q", row.Cells[1])
			}
		}
		if count != 1 {
			t.Fatalf("no-remotes rows %d", count)
		}
		compareFrame(t, a, "compare-no-remotes")
	})
	t.Run("git/compare-chooser-dedupe", func(t *testing.T) {
		a, dir := compareStory(t)
		sh(t, dir, "remote", "add", "origin", "../origin.git")
		sh(t, dir, "update-ref", "refs/remotes/origin/main", sh(t, dir, "rev-parse", "main"))
		r := compareResolver(t, dir, a.compareConfig())
		rows, err := loadSideChoices(r, "Endpoints", "HEAD")
		if err != nil {
			t.Fatal(err)
		}
		count := 0
		for _, row := range rows {
			if strings.Contains(strings.Join(row.Cells, " "), "origin/main") {
				count++
			}
		}
		if count != 1 {
			t.Fatalf("origin/main rows %d", count)
		}
		keys(t, a, "f")
		compareFrame(t, a, "compare-chooser-dedupe")
	})
	t.Run("git/compare-custom-collapse", func(t *testing.T) {
		a, _ := compareStory(t)
		keys(t, a, "~", "~")
		p, _ := a.selectedComparison()
		if !slices.Contains(p.Names, "dirty") || !slices.Contains(p.Names, "(custom)") {
			t.Fatal(p.Names)
		}
		compareFrame(t, a, "compare-custom-collapse")
	})
	t.Run("git/compare-row-default-from", func(t *testing.T) {
		a, _ := compareStory(t)
		storyRequest(t, a, "set", ochazuke.Arguments{"side": "from", "endpoint": "main"})
		_ = a.r.OpenPage(pageID)
		_ = a.branches.Focus()
		keys(t, a, "c")
		p, _ := a.selectedComparison()
		if p.From.Spec != "HEAD" {
			t.Fatalf("from %s", p.From.Spec)
		}
		compareFrame(t, a, "compare-row-default-from")
	})
	t.Run("git/compare-fetch-age-live", func(t *testing.T) {
		a, _ := compareStory(t)
		row, _ := a.compare.pairs.Selected()
		p := row.Data.(Comparison)
		at := a.Now().Add(-12 * time.Minute)
		p.Fetched = &at
		rows, cols := pairItems([]Comparison{p}, nil, a.Now())
		a.compare.pairs.SetColumns(cols...)
		_ = a.compare.pairs.SetItems(rows)
		a.compare.shown = &p
		a.m.Width, a.m.Height = 200, 40
		if !strings.Contains(screen(a), "12m") {
			t.Fatal("compact age missing")
		}
		now := a.Now().Add(time.Minute)
		a.Now = func() time.Time { return now }
		if !strings.Contains(screen(a), "13m") || !strings.Contains(a.compareDetail(), "as of the last fetch, 13m ago") {
			t.Fatal("age did not update", a.compareDetail())
		}
		compareFrame(t, a, "compare-fetch-age-live")
	})
	t.Run("git/compare-fetch-age-collapsed-detail", func(t *testing.T) {
		a, dir := compareStory(t)
		addBareRemote(t, dir, "origin")
		sh(t, dir, "update-ref", "refs/remotes/origin/main", sh(t, dir, "rev-parse", "main"))
		storyRequest(t, a, "reload", nil)
		selectComparePair(t, a, "main")
		if !strings.Contains(a.compareDetail(), "as of the last fetch") {
			t.Fatal(a.compareDetail())
		}
	})
	t.Run("git/compare-refs-short", func(t *testing.T) {
		a, dir := compareStory(t)
		sh(t, dir, "tag", "v1.0")
		rows, err := loadSideChoices(compareResolver(t, dir, a.compareConfig()), "Refs", "HEAD")
		if err != nil {
			t.Fatal(err)
		}
		for _, row := range rows {
			if strings.HasPrefix(row.Cells[0], "refs/") {
				t.Fatal(row.Cells)
			}
		}
		keys(t, a, "f", "ctrl+o", "ctrl+o", "ctrl+o")
		compareFrame(t, a, "compare-refs-short")
	})
	t.Run("git/compare-detail-readable", func(t *testing.T) {
		a, _ := compareStory(t)
		selectComparePair(t, a, "main")
		keys(t, a, "enter")
		if strings.Contains(a.compareDetail(), "1 parents") {
			t.Fatal(a.compareDetail())
		}
		keys(t, a, "enter", "enter")
		if strings.Contains(a.m.ModalViews[diffFrame].Title, strings.Repeat("0", 40)) {
			t.Fatal("invalid fixture")
		}
		title := a.m.ModalViews[diffFrame].Title
		for _, word := range strings.Fields(title) {
			if len(word) >= 40 {
				t.Fatal("full SHA in title", title)
			}
		}
		r := compareResolver(t, a.compare.repo, a.compareConfig())
		e := r.Resolve("merge-base(main)", "HEAD")[0]
		if !strings.Contains(endpointDetail(e), shortSHA(e.SHA)) {
			t.Fatal(endpointDetail(e))
		}
		compareFrame(t, a, "compare-detail-readable")
	})
	t.Run("git/compare-copy-feedback", func(t *testing.T) {
		a, _ := compareStory(t)
		keys(t, a, "y", "y")
		if !strings.Contains(a.m.Info, "copied ") {
			t.Fatal("copy notice missing")
		}
		compareFrame(t, a, "compare-copy-feedback")
	})
	t.Run("git/compare-typed-spec-preview", func(t *testing.T) {
		a, _ := compareStory(t)
		keys(t, a, "f")
		c := a.r.Chooser("compare-side")
		c.SetQuery("merge-base(main)")
		_ = c.Matches()
		if err := a.Settle(); err != nil {
			t.Fatal(err)
		}
		row, ok := c.Highlighted()
		choice, _ := row.Data.(sideChoice)
		if !ok || choice.Spec != "merge-base(main)" || !strings.Contains(choice.Graph, "resolved") {
			t.Fatalf("typed preview %+v", row)
		}
		compareFrame(t, a, "compare-typed-spec-preview")
	})
	t.Run("git/compare-empty-reason", func(t *testing.T) {
		a, _ := compareStory(t)
		if strings.Contains(screen(a), "(empty list)") {
			t.Fatal("duplicate empty-list message")
		}
		compareFrame(t, a, "compare-empty-reason")
	})
	t.Run("git/compare-cwd-repo", func(t *testing.T) {
		isolateCompare(t)
		dir := newRepo(t, "cwd")
		old, err := os.Getwd()
		if err != nil {
			t.Fatal(err)
		}
		if err := os.Chdir(dir); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = os.Chdir(old) })
		for _, width := range []int{80, 120, 200} {
			var out, errout strings.Builder
			if err := runTUI([]string{"--page", "compare", "--smoke", "--width", fmt.Sprint(width), "--height", "40"}, strings.NewReader(""), &out, &errout); err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(out.String(), "Compare ") || !strings.Contains(out.String(), "dirty") {
				t.Fatal(fmt.Sprint(out.String(), errout.String()))
			}
			if dest := os.Getenv("O_GIT_FRAME_DIR"); dest != "" {
				if err := os.WriteFile(filepath.Join(dest, fmt.Sprintf("compare-cwd-repo-%d.txt", width)), []byte(out.String()), 0644); err != nil {
					t.Fatal(err)
				}
			}
		}
	})
}

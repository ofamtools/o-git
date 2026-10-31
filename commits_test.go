package main

import (
	"context"
	"encoding/json"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	"ofam.dev/ochazuke"
)

func listPair(t *testing.T, dir, from, to string) Comparison {
	t.Helper()
	return compareResolver(t, dir, compareDefaults()).Show("", from, to)[0]
}

func commitData(t *testing.T, row ochazuke.ListItem) Commit {
	t.Helper()
	if c, ok := row.Data.(Commit); ok {
		return c
	}
	out, err := json.Marshal(row.Data)
	if err != nil {
		t.Fatal(err)
	}
	var c Commit
	if err := json.Unmarshal(out, &c); err != nil {
		t.Fatal(err)
	}
	return c
}

func TestCommitGitOrderCountsAndLimit(t *testing.T) {
	isolateCompare(t)
	dir := newRepo(t, "r")
	sh(t, dir, "checkout", "-q", "-b", "feature")
	for i := range 3 {
		commit(t, dir, "feature", "feature "+strconv.Itoa(i))
	}
	sh(t, dir, "checkout", "-q", "main")
	for i := range 2 {
		commit(t, dir, "main", "main "+strconv.Itoa(i))
	}
	pair := listPair(t, dir, "main", "feature")
	for _, split := range []bool{false, true} {
		list, err := ReadCommits(context.Background(), dir, pair, 500, split, time.Now())
		if err != nil {
			t.Fatal(err)
		}
		got, ahead, behind := []string{}, 0, 0
		for _, row := range list.Items {
			c := commitData(t, row)
			mark, section := ">", "Ahead"
			if c.Side == "behind" {
				mark, section = "<", "Behind"
				behind++
			} else {
				ahead++
			}
			got = append(got, mark+" "+c.SHA)
			if split && (row.Section != section || len(row.Cells) != 6) {
				t.Fatalf("section row: %+v", row)
			}
			if !split && (row.Section != "" || row.Cells[0] != mark) {
				t.Fatalf("side row: %+v", row)
			}
		}
		want := strings.Split(sh(t, dir, "log", "--left-right", "--format=%m %H", "main...feature"), "\n")
		if split {
			grouped := []string{}
			for _, mark := range []string{">", "<"} {
				for _, line := range want {
					if strings.HasPrefix(line, mark) {
						grouped = append(grouped, line)
					}
				}
			}
			want = grouped
		}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("git order: %v != %v", got, want)
		}
		counts := strings.Fields(sh(t, dir, "rev-list", "--left-right", "--count", "main...feature"))
		if counts[0] != strconv.Itoa(behind) || counts[1] != strconv.Itoa(ahead) {
			t.Fatalf("counts: %v vs %d %d", counts, behind, ahead)
		}
	}
	for _, limit := range []int{0, 1, 2, 500} {
		list, err := ReadCommits(context.Background(), dir, pair, limit, true, time.Now())
		if err != nil {
			t.Fatal(err)
		}
		seen, more := map[string]int{}, map[string]int{}
		for _, row := range list.Items {
			c := commitData(t, row)
			if c.More > 0 {
				more[c.Side] = c.More
				if row.Label != strconv.Itoa(c.More)+" more" {
					t.Fatalf("more row %+v", row)
				}
			} else {
				seen[c.Side]++
			}
		}
		if seen["ahead"] != min(limit, 3) || seen["behind"] != min(limit, 2) || seen["ahead"]+more["ahead"] != 3 || seen["behind"]+more["behind"] != 2 {
			t.Fatalf("limit %d: seen %v more %v", limit, seen, more)
		}
	}
}

func TestCommitShowRootAndMergeFiles(t *testing.T) {
	isolateCompare(t)
	dir := newRepo(t, "r")
	root := sh(t, dir, "rev-parse", "HEAD")
	rootFiles := decode[[]ochazuke.ListItem](t, run(t, "", "commit", "files", root, "--repo", dir))
	if len(rootFiles) != 2 || rootFiles[0].Label != "A README" {
		t.Fatalf("root files: %+v", rootFiles)
	}
	sh(t, dir, "checkout", "-q", "-b", "feature")
	commit(t, dir, "feature", "feat: topic")
	sh(t, dir, "checkout", "-q", "main")
	commit(t, dir, "main", "fix: main")
	sh(t, dir, "merge", "-q", "--no-ff", "feature", "-m", "merge topic\n\nfull body\nmore")
	sh(t, dir, "tag", "release")
	c := decode[Commit](t, run(t, "", "commit", "show", "HEAD", "--repo", dir))
	if c.ParentCount != 2 || len(c.Parents) != 2 || !strings.Contains(c.Decorations, "tag: release") || !strings.Contains(c.Message, "full body\nmore\n") || c.ShortSHA == "" || c.Author != "Test" || c.Side != "" {
		t.Fatalf("merge: %+v", c)
	}
	files := decode[[]ochazuke.ListItem](t, run(t, "", "commit", "files", "HEAD", "--repo", dir))
	if len(files) != 2 || files[0].Label != "A feature" {
		t.Fatalf("merge first parent files %+v", files)
	}
	list, err := ReadCommits(context.Background(), dir, listPair(t, dir, root, "HEAD"), 500, true, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, row := range list.Items {
		if commitData(t, row).SHA == c.SHA {
			found = commitData(t, row).ParentCount == 2
		}
	}
	if !found {
		t.Fatal("merge missing from list")
	}
}

func TestCommitDegenerateAndUnborn(t *testing.T) {
	isolateCompare(t)
	dir := newRepo(t, "r")
	for _, tc := range []struct{ from, to, reason string }{
		{"HEAD", "HEAD", "same commit"}, {"HEAD", "WORKTREE", "not a commit"}, {"INDEX", "HEAD", "not a commit"}, {"absent", "HEAD", "absent"},
	} {
		list, err := ReadCommits(context.Background(), dir, listPair(t, dir, tc.from, tc.to), 500, true, time.Now())
		if err != nil || len(list.Items) != 0 || !strings.Contains(list.Reason, tc.reason) {
			t.Fatalf("%+v: %+v %v", tc, list, err)
		}
	}
	unborn := t.TempDir()
	sh(t, unborn, "init", "-q", "-b", "main")
	empty := oneEndpoint(t, compareResolver(t, unborn, compareDefaults()), "HEAD")
	if empty.Kind != "empty" {
		t.Fatalf("unborn: %+v", empty)
	}
	list, err := ReadCommits(context.Background(), unborn, listPair(t, unborn, "HEAD", "HEAD"), 500, true, time.Now())
	if err != nil || len(list.Items) != 0 {
		t.Fatalf("unborn same: %+v %v", list, err)
	}
	head := oneEndpoint(t, compareResolver(t, dir, compareDefaults()), "HEAD")
	for _, reverse := range []bool{false, true} {
		f, to := empty, head
		side := "ahead"
		if reverse {
			f, to, side = head, empty, "behind"
		}
		list, err := ReadCommits(context.Background(), dir, makeComparison(dir, "empty", "", "", f, to), 500, true, time.Now())
		if err != nil || len(list.Items) != 1 || commitData(t, list.Items[0]).Side != side {
			t.Fatalf("unborn whole history: %+v %v", list, err)
		}
	}
	_, stderr, err := try("", "commit", "list", "--repo", dir, "--pair", "dirty")
	if err != nil || !strings.Contains(stderr, "not a commit") {
		t.Fatalf("empty reason: %s %v", stderr, err)
	}
}

func TestCommitSettingsFormatsAndCopies(t *testing.T) {
	isolateCompare(t)
	dir := newRepo(t, "r")
	commit(t, dir, "next", "second")
	a, err := New()
	if err != nil {
		t.Fatal(err)
	}
	a.r.Config().Disable()
	if !a.r.Settings().Bool("compare.commits.split") || a.r.Settings().Int("compare.commits.limit") != 500 {
		t.Fatal("wrong defaults")
	}
	if _, err := a.r.Settings().SetSession("compare.commits.limit", -1); err == nil {
		t.Fatal("negative setting accepted")
	}
	for _, split := range []bool{true, false} {
		if _, err := a.r.Settings().SetSession("compare.commits.split", split); err != nil {
			t.Fatal(err)
		}
		for _, format := range []string{"table", "markdown", "toon"} {
			var out, stderr strings.Builder
			a.Stdout, a.Stderr = &out, &stderr
			if err := a.Shell().Run(context.Background(), []string{"commit", "list", "--repo", dir, "--from", "HEAD~1", "--to", "HEAD", "--format", format}); err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(out.String(), "second") || strings.Contains(out.String(), "side") == split || stderr.Len() != 0 {
				t.Fatalf("split=%v %s: %s; %s", split, format, &out, &stderr)
			}
		}
	}
	var out, stderr strings.Builder
	a.Stdout, a.Stderr = &out, &stderr
	if err := a.Shell().Run(context.Background(), []string{"commit", "copy", "HEAD", "--repo", dir}); err != nil {
		t.Fatal(err)
	}
	if got := a.r.Registers().Named["\""].Yank.Text; got != sh(t, dir, "rev-parse", "HEAD") {
		t.Fatalf("copied %q", got)
	}
	rows := decode[[]ochazuke.ListItem](t, run(t, "", "compare", "commits", "--from", "HEAD~1", "--to", "HEAD", "--repo", dir, "--limit", "0"))
	if len(rows) != 1 || commitData(t, rows[0]).More != 1 {
		t.Fatalf("limit override %+v", rows)
	}
}

func TestCompareListsCoreReasonsAndDetails(t *testing.T) {
	isolateCompare(t)
	dir := newRepo(t, "r")
	pair := listPair(t, dir, "missing", "HEAD")
	if pair.Available || pair.Detail == "" || strings.Contains(pair.Reason, "fatal:") {
		t.Fatalf("core diagnostic: %+v", pair)
	}
	commits, err := ReadCommits(context.Background(), dir, pair, 500, true, time.Now())
	if err != nil || commits.Reason != pair.Reason || commits.Detail != pair.Detail || len(commits.Items) != 0 {
		t.Fatalf("commit diagnostic: %+v %v", commits, err)
	}
	changes, err := ReadChanges(context.Background(), dir, pair, false)
	if err != nil || changes.Reason != pair.Reason || changes.Detail != pair.Detail || len(changes.Files) != 0 {
		t.Fatalf("change diagnostic: %+v %v", changes, err)
	}
	for _, row := range changes.Items(dir) {
		for _, cell := range row.Cells {
			if strings.Contains(cell, "fatal:") || strings.Contains(cell, "git rev-parse") {
				t.Fatalf("raw diagnostic in cell %q", cell)
			}
		}
	}
}

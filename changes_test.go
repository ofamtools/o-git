package main

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	"ofam.dev/ochazuke"
)

func changeFixture(t *testing.T) string {
	t.Helper()
	isolateCompare(t)
	dir := newRepo(t, "r")
	for path, content := range map[string]string{
		"rename source": "rename one\nrename two\nrename three\n",
		"copy source":   "copy one\ncopy two\ncopy three\ncopy four\n",
		"deleted":       "deleted only\n", "typed": "regular file\n", "binary": "old\x00binary\n",
		"tab\tand\nnewline": "original\n",
	} {
		write(t, dir, path, content)
	}
	sh(t, dir, "add", "-A")
	sh(t, dir, "commit", "-q", "-m", "fixture")
	sh(t, dir, "mv", "rename source", "renamed target")
	write(t, dir, "copied", "copy one\ncopy two\ncopy three\ncopy four\n")
	sh(t, dir, "add", "copied")
	if err := os.Remove(filepath.Join(dir, "deleted")); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(dir, "typed")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("README", filepath.Join(dir, "typed")); err != nil {
		t.Fatal(err)
	}
	write(t, dir, "binary", "new\x00binary data\n")
	write(t, dir, "tab\tand\nnewline", "updated\n")
	write(t, dir, "README", "staged\n")
	sh(t, dir, "add", "README")
	write(t, dir, "README", "unstaged\n")
	write(t, dir, "untracked \"name", "unknown\n")
	if err := os.Mkdir(filepath.Join(dir, "unknown-dir"), 0o755); err != nil {
		t.Fatal(err)
	}
	write(t, dir, "unknown-dir/file", "unknown in directory\n")
	return dir
}

func changeData(t *testing.T, row ochazuke.ListItem) Change {
	t.Helper()
	if c, ok := row.Data.(Change); ok {
		return c
	}
	out, err := json.Marshal(row.Data)
	if err != nil {
		t.Fatal(err)
	}
	var c Change
	if err := json.Unmarshal(out, &c); err != nil {
		t.Fatal(err)
	}
	return c
}

func readChanges(t *testing.T, dir, from, to string) ChangeList {
	t.Helper()
	list, err := ReadChanges(context.Background(), dir, listPair(t, dir, from, to), false)
	if err != nil {
		t.Fatal(err)
	}
	return list
}

func TestChangeDirtyMatchesGitStatusBytes(t *testing.T) {
	dir := changeFixture(t)
	list := readChanges(t, dir, "HEAD", "WORKTREE")
	var got strings.Builder
	for _, file := range list.Files {
		got.WriteString(file.Status + " " + file.Path + "\x00")
		if file.OldPath != "" {
			got.WriteString(file.OldPath + "\x00")
		}
	}
	want, err := git(context.Background(), readTimeout, dir, "-c", "core.quotePath=false", "status", "-s", "-z")
	if err != nil {
		t.Fatal(err)
	}
	if got.String() != string(want) {
		t.Fatalf("paths/letters/order differ:\ngot  %q\nwant %q", got.String(), want)
	}
	rows := decode[[]ochazuke.ListItem](t, run(t, "", "change", "list", "--pair", "dirty", "--repo", dir))
	got.Reset()
	for _, row := range rows[:len(rows)-1] {
		file := changeData(t, row)
		got.WriteString(file.Status + " " + file.Path + "\x00")
		if file.OldPath != "" {
			got.WriteString(file.OldPath + "\x00")
		}
	}
	if got.String() != string(want) {
		t.Fatalf("CLI paths/letters/order differ:\ngot %q\nwant %q", got.String(), want)
	}
	byPath := map[string]Change{}
	for _, file := range list.Files {
		byPath[file.Path] = file
	}
	f := byPath["README"]
	if f.Status != "MM" || f.Added != 2 || f.Removed != 2 || f.Index == nil || f.Worktree == nil || f.Index.Added != 1 || f.Worktree.Removed != 1 {
		t.Fatalf("staged plus unstaged: %+v", f)
	}
	if !byPath["binary"].Binary || byPath["typed"].Status != " T" || byPath["renamed target"].OldPath != "rename source" || byPath["unknown-dir/"].Status != "??" {
		t.Fatalf("special files: %+v", byPath)
	}
	if byPath["copied"].Status != "A " || byPath["copied"].Added != 4 {
		t.Fatalf("status copy is an index addition: %+v", byPath["copied"])
	}
	if list.Totals.Files != len(list.Files) || list.Totals.Binary != 1 || rows[len(rows)-1].Cells[0] != "total" {
		t.Fatalf("totals: %+v rows %+v", list.Totals, rows)
	}
}

func TestChangeCommitTypesCopiesAndBinary(t *testing.T) {
	dir := changeFixture(t)
	// Default -C considers modified sources, without scanning every unchanged file.
	write(t, dir, "copy source", "copy one\ncopy two\ncopy three\ncopy four\nsource edit\n")
	sh(t, dir, "add", "-A")
	sh(t, dir, "commit", "-q", "-m", "all types")
	list := readChanges(t, dir, "HEAD~1", "HEAD")
	byPath := map[string]Change{}
	for _, file := range list.Files {
		byPath[file.Path] = file
	}
	for _, tc := range []struct{ path, status, old string }{
		{"README", "M", ""}, {"deleted", "D", ""}, {"typed", "T", ""},
		{"renamed target", "R", "rename source"}, {"copied", "C", "copy source"},
		{"binary", "M", ""}, {"untracked \"name", "A", ""},
	} {
		got, ok := byPath[tc.path]
		if !ok || got.Status != tc.status || got.OldPath != tc.old {
			t.Fatalf("%+v: %+v", tc, got)
		}
	}
	if byPath["copied"].Added != 0 || byPath["copied"].Removed != 0 {
		t.Fatalf("copy counts: %+v", byPath["copied"])
	}
	row := changeItem(byPath["binary"])
	if row.Cells[2] != "bin" || row.Cells[3] != "bin" {
		t.Fatalf("binary: %+v", row)
	}
	if changeItem(byPath["renamed target"]).Cells[1] != "rename source → renamed target" {
		t.Fatal("rename display")
	}
	for _, format := range []string{"table", "markdown", "toon"} {
		out := run(t, "", "change", "list", "--from", "HEAD~1", "--to", "HEAD", "--repo", dir, "--format", format)
		if !strings.Contains(out, "bin") || !strings.Contains(out, "total") || !strings.Contains(out, "copy source → copied") {
			t.Fatalf("%s: %s", format, out)
		}
	}
}

func TestChangeCopyDetectionSetting(t *testing.T) {
	isolateCompare(t)
	dir := newRepo(t, "r")
	write(t, dir, "copied", "hello\n")
	sh(t, dir, "add", "copied")
	sh(t, dir, "commit", "-q", "-m", "copy an unchanged source")
	a, err := New()
	if err != nil {
		t.Fatal(err)
	}
	a.r.Config().Disable()
	const setting = "compare.files.find-copies-harder"
	if a.r.Settings().Bool(setting) {
		t.Fatal("harder copy search must default to false")
	}
	// The read helper consumes exactly these extra argv words. Default -C
	// keeps ordinary copy detection without scanning every unchanged source.
	args := commitChangeArgs(a.r.Settings().Bool(setting), "base", "tip")
	if slices.Contains(args, "--find-copies-harder") || !slices.Equal(args, []string{"-C", "base", "tip"}) {
		t.Fatalf("default commit diff argv: %v", args)
	}
	for _, harder := range []bool{false, true, false} {
		if _, err := a.r.Settings().SetSession(setting, harder); err != nil {
			t.Fatal(err)
		}
		if got := slices.Contains(commitChangeArgs(a.r.Settings().Bool(setting)), "--find-copies-harder"); got != harder {
			t.Fatalf("setting=%v, harder argv flag=%v", harder, got)
		}
		want := "A"
		if harder {
			want = "C"
		}
		for _, command := range [][]string{
			{"change", "list", "--from", "HEAD~1", "--to", "HEAD"},
			{"compare", "files", "--from", "HEAD~1", "--to", "HEAD"},
			{"commit", "files", "HEAD"},
			{"change", "show", "copied", "--from", "HEAD~1", "--to", "HEAD"},
		} {
			var out, stderr strings.Builder
			a.Stdout, a.Stderr = &out, &stderr
			if err := a.Shell().Run(context.Background(), append(command, "--repo", dir)); err != nil {
				t.Fatal(err)
			}
			var file Change
			if command[1] == "show" {
				file = decode[Change](t, out.String())
			} else {
				rows := decode[[]ochazuke.ListItem](t, out.String())
				if len(rows) != 2 {
					t.Fatalf("%v: %s", command, &out)
				}
				file = changeData(t, rows[0])
			}
			if file.Path != "copied" || file.Status != want || stderr.Len() != 0 {
				t.Fatalf("setting=%v %v: %+v; %s", harder, command, file, &stderr)
			}
			if harder && file.OldPath != "README" {
				t.Fatalf("copy source: %+v", file)
			}
			if !harder && file.Added != 1 {
				t.Fatalf("addition count: %+v", file)
			}
		}
	}
}

func TestChangeIndexUnstagedAndReversed(t *testing.T) {
	dir := changeFixture(t)
	for _, tc := range []struct {
		from, to string
		reverse  bool
		check    func(Change) bool
	}{
		{"HEAD", "INDEX", false, func(f Change) bool { return f.Status[1] == ' ' && f.Index != nil && f.Worktree == nil }},
		{"INDEX", "WORKTREE", false, func(f Change) bool { return f.Status == "??" || (f.Status[0] == ' ' && f.Index == nil) }},
		{"WORKTREE", "INDEX", true, func(f Change) bool { return f.Status[0] == ' ' && f.Status != "??" }},
		{"WORKTREE", "HEAD", true, func(f Change) bool { return len(f.Status) == 1 }},
		{"INDEX", "HEAD", true, func(f Change) bool { return len(f.Status) == 1 }},
	} {
		t.Run(tc.from+".."+tc.to, func(t *testing.T) {
			list := readChanges(t, dir, tc.from, tc.to)
			if len(list.Files) == 0 || list.Reversed != tc.reverse || (tc.reverse && !strings.Contains(list.Reason, "reversed: the working tree is on the left")) {
				t.Fatalf("%+v", list)
			}
			for _, file := range list.Files {
				if !tc.check(file) {
					t.Fatalf("unexpected change %+v", file)
				}
			}
			for _, file := range list.Files {
				if file.Path == "deleted" && tc.reverse && file.Status != "A" && file.Status != " A" {
					t.Fatalf("reverse delete: %+v", file)
				}
			}
		})
	}
	for _, side := range []string{"HEAD", "INDEX", "WORKTREE"} {
		list := readChanges(t, dir, side, side)
		if len(list.Files) != 0 || list.Totals.Files != 0 || !strings.Contains(list.Reason, "nothing differs") {
			t.Fatalf("same %s: %+v", side, list)
		}
	}
	list := readChanges(t, dir, "missing", "WORKTREE")
	if len(list.Files) != 0 || list.Reason == "" {
		t.Fatalf("unavailable %+v", list)
	}
}

func TestChangeUnbornAndArbitraryBase(t *testing.T) {
	isolateCompare(t)
	dir := t.TempDir()
	sh(t, dir, "init", "-q", "-b", "main")
	write(t, dir, "staged", "first\n")
	sh(t, dir, "add", "staged")
	write(t, dir, "staged", "second\n")
	write(t, dir, "untracked", "unknown\n")
	list := readChanges(t, dir, "HEAD", "WORKTREE")
	if len(list.Files) != 2 || list.Files[0].Path != "staged" || list.Files[0].Status != "AM" || list.Files[1].Status != "??" {
		t.Fatalf("unborn dirty: %+v", list)
	}
	index := readChanges(t, dir, "HEAD", "INDEX")
	if len(index.Files) != 1 || index.Files[0].Status != "A " || index.Files[0].Added != 1 {
		t.Fatalf("unborn staged: %+v", index)
	}
	sh(t, dir, "commit", "-q", "-m", "first")
	commit(t, dir, "other", "second")
	list = readChanges(t, dir, "HEAD~1", "WORKTREE")
	byPath := map[string]string{}
	for _, file := range list.Files {
		byPath[file.Path] = file.Status
	}
	if byPath["other"] != "A " || byPath["staged"] != " M" || byPath["untracked"] != "??" {
		t.Fatalf("base-relative XY %+v", list)
	}
}

func TestChangeShowCopyAndPinnedFlags(t *testing.T) {
	dir := changeFixture(t)
	marker := filepath.Join(t.TempDir(), "executed")
	driver := filepath.Join(t.TempDir(), "driver")
	// Paths are generated by TempDir and contain no shell quotes.
	if err := os.WriteFile(driver, []byte("#!/bin/sh\nprintf bad > '"+marker+"'\nexit 1\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	sh(t, dir, "config", "diff.external", driver)
	sh(t, dir, "config", "diff.custom.textconv", driver)
	sh(t, dir, "config", "core.pager", driver)
	sh(t, dir, "config", "diff.relative", "true")
	write(t, dir, ".gitattributes", "README diff=custom\n")
	file := decode[Change](t, run(t, "", "change", "show", "README", "--pair", "dirty", "--repo", dir))
	if file.Path != "README" || file.Status != "MM" || file.Added != 2 {
		t.Fatalf("show %+v", file)
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatalf("diff driver ran: %v", err)
	}
	a, err := New()
	if err != nil {
		t.Fatal(err)
	}
	a.r.Config().Disable()
	var out, stderr strings.Builder
	a.Stdout, a.Stderr = &out, &stderr
	if err := a.Shell().Run(context.Background(), []string{"change", "copy", "README", "--repo", dir}); err != nil {
		t.Fatal(err)
	}
	if got := a.r.Registers().Named["\""].Yank.Text; got != "README" {
		t.Fatalf("copy %q", got)
	}
	for _, args := range [][]string{
		{"change", "show", "missing", "--repo", dir},
		{"change", "list", "--pair", "dirty", "--from", "HEAD", "--repo", dir},
		{"commit", "list", "--limit", "-1", "--repo", dir},
	} {
		if _, _, err := try("", args...); err == nil {
			t.Fatalf("accepted %v", args)
		}
	}
}

func TestChangeRawNumstatParser(t *testing.T) {
	for _, tc := range []struct {
		name, output string
		want         []Change
	}{
		{"rename newline tab", ":100644 100644 a b R100\x00old\npath\x00new\tpath\x001\t2\t\x00old\npath\x00new\tpath\x00", []Change{{Path: "new\tpath", OldPath: "old\npath", Status: "R", Added: 1, Removed: 2}}},
		{"copy", ":100644 100644 a b C100\x00a\x00b\x000\t0\t\x00a\x00b\x00", []Change{{Path: "b", OldPath: "a", Status: "C"}}},
		{"binary type", ":100644 120000 a b T\x00x\x00-\t-\tx\x00", []Change{{Path: "x", Status: "T", Binary: true}}},
		{"empty", "", []Change{}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := parseRawNumstat([]byte(tc.output), "")
			if err != nil || !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("got %+v %v want %+v", got, err, tc.want)
			}
		})
	}
	for _, bad := range []string{":bad\x00", ":100644 100644 a b R100\x00a\x00", "garbage\x00", ":100644 100644 a b M\x00x\x00z\t1\tx\x00", "1\t1\tx\x00"} {
		if _, err := parseRawNumstat([]byte(bad), ""); err == nil {
			t.Errorf("accepted %q", bad)
		}
	}
}

func TestChangeConflictStatusAndCommittedReverse(t *testing.T) {
	isolateCompare(t)
	dir := newRepo(t, "r")
	sh(t, dir, "checkout", "-q", "-b", "feature")
	commit(t, dir, "README", "feature version")
	sh(t, dir, "checkout", "-q", "main")
	commit(t, dir, "README", "main version")
	commit(t, dir, "new", "added on main")
	forward := readChanges(t, dir, "HEAD~2", "HEAD")
	reverse := readChanges(t, dir, "HEAD", "HEAD~2")
	if reverse.Totals.Added != forward.Totals.Removed || reverse.Totals.Removed != forward.Totals.Added {
		t.Fatalf("reverse totals %+v vs %+v", forward, reverse)
	}
	for _, f := range reverse.Files {
		if f.Path == "new" && f.Status != "D" {
			t.Fatalf("reverse addition %+v", f)
		}
	}
	// A conflict is left in this temporary fixture only.
	_, err := git(context.Background(), readTimeout, dir, "merge", "--no-ff", "feature", "-m", "conflict")
	if err == nil {
		t.Fatal("fixture did not conflict")
	}
	list := readChanges(t, dir, "HEAD", "WORKTREE")
	found := false
	for _, f := range list.Files {
		if f.Path == "README" {
			found = f.Status == "UU"
		}
	}
	if !found {
		t.Fatalf("conflict missing: %+v", list)
	}
}

func TestChangeUntrackedOverBaseDeletion(t *testing.T) {
	isolateCompare(t)
	dir := newRepo(t, "r")
	if err := os.Remove(filepath.Join(dir, "README")); err != nil {
		t.Fatal(err)
	}
	sh(t, dir, "add", "-A")
	sh(t, dir, "commit", "-q", "-m", "remove tracked path")
	write(t, dir, "README", "untracked replacement\n")
	list := readChanges(t, dir, "HEAD~1", "WORKTREE")
	if len(list.Files) != 1 || list.Files[0].Path != "README" || list.Files[0].Status != "D?" || !list.Files[0].Untracked || list.Files[0].Index == nil || list.Files[0].Index.Removed != 1 {
		t.Fatalf("base deletion and untracked replacement: %+v", list)
	}
}

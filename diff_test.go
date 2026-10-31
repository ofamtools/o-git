package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/unixsuperhero/ochazuke"
)

// Independent git invocations are the oracle for exact unified text, including
// paths, modes, binary notices, rename headers and missing-newline markers.
func diffOracle(t *testing.T, dir string, noIndex bool, args ...string) string {
	t.Helper()
	flags := []string{"-C", dir, "-c", "core.quotePath=false", "-c", "diff.noprefix=false", "diff", "--no-color", "--no-ext-diff", "--no-textconv", "--no-relative", "-M", "-U3"}
	cmd := exec.Command("git", append(flags, args...)...)
	cmd.Env = append(gitEnv(), "GIT_PAGER=cat")
	out, err := cmd.Output()
	if err != nil && !(noIndex && cmd.ProcessState.ExitCode() == 1) {
		t.Fatalf("oracle %v: %v", args, err)
	}
	return string(out)
}

func diffArtifact(t *testing.T, name, text string) {
	t.Helper()
	if dir := os.Getenv("O_GIT_DIFF_ARTIFACTS"); dir != "" {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

func diffPair(t *testing.T, dir, from, to string) Comparison {
	t.Helper()
	r := compareResolver(t, dir, compareDefaults())
	f, e := r.ResolvePair(from, to)
	p := makeComparison(dir, "fixture", from, to, f, e)
	if !p.Available {
		t.Fatalf("pair %s..%s: %s", from, to, p.Reason)
	}
	return p
}

func fixtureDiff(t *testing.T, dir, from, to string, opt DiffOptions) DiffResult {
	t.Helper()
	d, err := BuildDiff(t.Context(), diffPair(t, dir, from, to), opt)
	if err != nil {
		t.Fatal(err)
	}
	return d
}

func TestDiffTrackedAndCommit(t *testing.T) {
	dir := newRepo(t, "diff")
	for name, text := range map[string]string{
		"modify": "one\ntwo\nthree\n", "delete": "gone\n", "old name": "rename\n", "binary": "\x00old\x01", "no-newline": "before", "tabs\tand\nnewlines": "old\r\n",
	} {
		write(t, dir, name, text)
	}
	sh(t, dir, "add", ".")
	sh(t, dir, "commit", "-qm", "before")
	old := sh(t, dir, "rev-parse", "HEAD")
	write(t, dir, "modify", "one\nnew\nthree\n")
	write(t, dir, "added", "added\n")
	write(t, dir, "no-newline", "after")
	write(t, dir, "binary", "\x00longer\x01blob")
	write(t, dir, "tabs\tand\nnewlines", "new\r\n")
	if err := os.Remove(filepath.Join(dir, "delete")); err != nil {
		t.Fatal(err)
	}
	sh(t, dir, "mv", "old name", "new name")
	sh(t, dir, "add", ".")
	sh(t, dir, "commit", "-qm", "after")
	opt := DiffOptions{Context: 3, MaxLines: defaultDiffLines, Plain: true}
	d := fixtureDiff(t, dir, old, "HEAD", opt)
	want := diffOracle(t, dir, false, old, "HEAD", "--")
	if d.Unified != want {
		t.Fatalf("patch mismatch\nGOT %s\nWANT %s", d.Unified, want)
	}
	byPath := map[string]DiffFile{}
	for _, f := range d.Files {
		path := f.NewPath
		if path == "" {
			path = f.OldPath
		}
		byPath[path] = f
	}
	if f := byPath["binary"]; !f.Binary || f.OldSize != 5 || f.NewSize != 12 {
		t.Fatalf("binary %+v", f)
	}
	if f := byPath["new name"]; f.Status != "R" || f.OldPath != "old name" || f.NewPath != "new name" {
		t.Fatalf("rename %+v", f)
	}
	for path, status := range map[string]string{"added": "A", "delete": "D", "modify": "M"} {
		if byPath[path].Status != status {
			t.Fatalf("%s %+v", path, byPath[path])
		}
	}
	if f := byPath["no-newline"]; len(f.Hunks) != 1 || !f.Hunks[0].Lines[0].NoNewline || !f.Hunks[0].Lines[1].NoNewline {
		t.Fatalf("newline %+v", f)
	}
	if f := byPath["tabs\tand\nnewlines"]; f.Hunks[0].Lines[1].Text != "new\r" {
		t.Fatalf("path/CR %+v", f)
	}
	for _, path := range []string{"new name", "old name"} {
		opt.Path = path
		rename := fixtureDiff(t, dir, old, "HEAD", opt)
		if len(rename.Files) != 1 || rename.Files[0].Status != "R" || !strings.Contains(rename.Unified, "rename from old name") {
			t.Fatalf("literal rename path %+v", rename)
		}
	}
	if got := run(t, "", "commit", "diff", "HEAD", "--repo", dir); got != want {
		t.Fatalf("commit CLI patch\n%s", got)
	}
	structured := run(t, "", "commit", "diff", "HEAD", "--repo", dir, "--output", "json")
	if got := decode[DiffResult](t, structured); !reflect.DeepEqual(got.Files, d.Files) {
		t.Fatalf("CLI JSON %+v", got)
	}
	diffArtifact(t, "tracked.diff", want)
	diffArtifact(t, "tracked.json", structured)
}

func TestDiffOutputParameter(t *testing.T) {
	dir := newRepo(t, "output")
	write(t, dir, "README", "changed\n")
	empty := sh(t, dir, "hash-object", "-t", "tree", "/dev/null")
	for _, tc := range []struct {
		args []string
		text string
	}{
		{[]string{"change", "diff", "README", "--repo", dir}, diffOracle(t, dir, false, "HEAD", "--")},
		{[]string{"commit", "diff", "HEAD", "--repo", dir}, diffOracle(t, dir, false, empty, "HEAD", "--")},
	} {
		for _, flags := range [][]string{nil, {"--output", "plain"}, {"--json"}, {"--format", "json"}} {
			args := append(append([]string{}, tc.args...), flags...)
			if got := run(t, "", args...); got != tc.text {
				t.Fatalf("%v changed plain output\n%s", args, got)
			}
		}
		for _, flags := range [][]string{{"--output", "json"}, {"--output=json", "--json"}, {"--output", "json", "--format", "json"}} {
			args := append(append([]string{}, tc.args...), flags...)
			if got := decode[DiffResult](t, run(t, "", args...)); len(got.Files) != 1 || !got.Comparison.Available {
				t.Fatalf("%v: %+v", args, got)
			}
		}
		// Direct shell dispatch has the same defaults, without passing through
		// Main or any argv adapter. API callers can request structured data
		// through the declared output parameter as well.
		a, err := New()
		if err != nil {
			t.Fatal(err)
		}
		var stdout, stderr bytes.Buffer
		a.Stdout, a.Stderr = &stdout, &stderr
		if err := a.Shell().Run(t.Context(), tc.args); err != nil || stdout.String() != tc.text || stderr.Len() != 0 {
			t.Fatalf("direct shell: %v\nstdout %s\nstderr %s", err, &stdout, &stderr)
		}
		if !a.diffOptions(ochazuke.Ctx{}).Plain || a.diffOptions(ochazuke.Ctx{Args: ochazuke.Arguments{"output": "json"}}).Plain {
			t.Fatal("CLI and API must share the output parameter's plain default")
		}
	}
}

func TestDiffMutableShapes(t *testing.T) {
	dir := newRepo(t, "mutable")
	write(t, dir, "README", "staged\n")
	write(t, dir, "staged-add", "new\n")
	write(t, dir, "staged-binary", "\x00blob")
	sh(t, dir, "add", ".")
	write(t, dir, "README", "working\n")
	write(t, dir, "untracked space", "untracked\n")
	write(t, dir, "untracked-binary", "\x00bytes")
	for _, tc := range []struct {
		from, to string
		args     []string
		reverse  bool
		others   bool
	}{
		{"HEAD", "INDEX", []string{"--cached", "HEAD"}, false, false},
		{"INDEX", "HEAD", []string{"-R", "--cached", "HEAD"}, true, false},
		{"HEAD", "WORKTREE", []string{"HEAD"}, false, true},
		{"WORKTREE", "HEAD", []string{"-R", "HEAD"}, true, true},
		{"INDEX", "WORKTREE", nil, false, true},
		{"WORKTREE", "INDEX", []string{"-R"}, true, true},
	} {
		t.Run(tc.from+"-"+tc.to, func(t *testing.T) {
			opt := DiffOptions{Context: 3, MaxLines: defaultDiffLines, Plain: true}
			d := fixtureDiff(t, dir, tc.from, tc.to, opt)
			want := diffOracle(t, dir, false, append(tc.args, "--")...)
			if tc.others {
				for _, path := range []string{"untracked space", "untracked-binary"} {
					args := []string{"--no-index"}
					if tc.reverse {
						args = append(args, "-R")
					}
					want += diffOracle(t, dir, true, append(args, "--", "/dev/null", path)...)
				}
			}
			if d.Unified != want || d.Reversed != tc.reverse {
				t.Fatalf("reverse %v want %v\nGOT %s\nWANT %s", d.Reversed, tc.reverse, d.Unified, want)
			}
			jsonText := run(t, "", "change", "diff", "README", "--repo", dir, "--from", tc.from, "--to", tc.to, "--output", "json")
			got := decode[DiffResult](t, jsonText)
			if len(got.Files) != 1 || got.Reversed != tc.reverse {
				t.Fatalf("CLI %+v", got)
			}
			for _, f := range d.Files {
				if f.Binary && f.NewPath == "staged-binary" && f.NewSize != 5 {
					t.Fatalf("index binary size %+v", f)
				}
				if f.Binary && f.OldPath == "staged-binary" && f.OldSize != 5 {
					t.Fatalf("reversed index binary size %+v", f)
				}
			}
			diffArtifact(t, tc.from+"-"+tc.to+".diff", want)
			b, err := json.MarshalIndent(d, "", "  ")
			if err != nil {
				t.Fatal(err)
			}
			diffArtifact(t, tc.from+"-"+tc.to+".json", string(b)+"\n")
		})
	}
	got := run(t, "", "change", "diff", "untracked space", "--repo", dir)
	want := diffOracle(t, dir, true, "--no-index", "--", "/dev/null", "untracked space")
	if got != want {
		t.Fatalf("untracked default pair\nGOT %s\nWANT %s", got, want)
	}
	for _, f := range fixtureDiff(t, dir, "HEAD", "WORKTREE", DiffOptions{Context: 3, MaxLines: 100}).Files {
		if f.NewPath == "untracked-binary" && (!f.Binary || f.OldSize != 0 || f.NewSize != 6) {
			t.Fatalf("untracked binary size %+v", f)
		}
	}
	if got := run(t, "", "change", "diff", "./README", "--repo", dir); got != diffOracle(t, dir, false, "HEAD", "--", "README") {
		t.Fatalf("relative path output %s", got)
	}
}

func TestDiffMergeFirstParentAndLiteralPath(t *testing.T) {
	dir := newRepo(t, "merge")
	sh(t, dir, "checkout", "-qb", "topic")
	write(t, dir, "star*file", "topic\n")
	write(t, dir, "star-other-file", "other\n")
	sh(t, dir, "add", ".")
	sh(t, dir, "commit", "-qm", "topic")
	sh(t, dir, "checkout", "-q", "main")
	write(t, dir, "main-file", "main\n")
	sh(t, dir, "add", ".")
	sh(t, dir, "commit", "-qm", "main")
	sh(t, dir, "merge", "--no-ff", "-qm", "merge topic", "topic")
	if got, want := run(t, "", "commit", "diff", "HEAD", "--repo", dir), diffOracle(t, dir, false, "HEAD^1", "HEAD", "--"); got != want {
		t.Fatalf("merge patch\nGOT %s\nWANT %s", got, want)
	}
	got := decode[DiffResult](t, run(t, "", "change", "diff", "star*file", "--from", "HEAD^1", "--to", "HEAD", "--repo", dir, "--output", "json"))
	if len(got.Files) != 1 || got.Files[0].NewPath != "star*file" {
		t.Fatalf("glob path expanded %+v", got)
	}
}

func TestDiffTypeChange(t *testing.T) {
	dir := newRepo(t, "type")
	if err := os.Remove(filepath.Join(dir, "README")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("target", filepath.Join(dir, "README")); err != nil {
		t.Fatal(err)
	}
	d := fixtureDiff(t, dir, "HEAD", "WORKTREE", DiffOptions{Context: 3, MaxLines: 100, Plain: true})
	if want := diffOracle(t, dir, false, "HEAD", "--"); d.Unified != want {
		t.Fatalf("type change\nGOT %s\nWANT %s", d.Unified, want)
	}
	if len(d.Files) != 1 || d.Files[0].Status != "T" || d.Files[0].Added != 1 || d.Files[0].Removed != 1 || len(d.Files[0].Hunks) != 2 {
		t.Fatalf("type change metadata %+v", d.Files)
	}
	sh(t, dir, "add", ".")
	d = fixtureDiff(t, dir, "INDEX", "HEAD", DiffOptions{Context: 3, MaxLines: 100, Plain: true})
	if want := diffOracle(t, dir, false, "-R", "--cached", "HEAD", "--"); d.Unified != want {
		t.Fatalf("reversed type change\nGOT %s\nWANT %s", d.Unified, want)
	}
}

func diffRepositoryFiles(t *testing.T, dir string) map[string][32]byte {
	t.Helper()
	out := map[string][32]byte{}
	err := filepath.WalkDir(filepath.Join(dir, ".git"), func(path string, e os.DirEntry, err error) error {
		if err != nil || e.IsDir() {
			return err
		}
		b, err := os.ReadFile(path)
		if err == nil {
			out[path] = sha256.Sum256(b)
		}
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func TestDiffSuppressesExternalProgramsAndWrites(t *testing.T) {
	dir := newRepo(t, "hooks")
	marker := filepath.Join(t.TempDir(), "ran")
	script := filepath.Join(t.TempDir(), "hook")
	if err := os.WriteFile(script, []byte("#!/bin/sh\nprintf ran >> '"+marker+"'\nexit 99\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	write(t, dir, ".gitattributes", "README diff=owned\n")
	sh(t, dir, "add", ".")
	sh(t, dir, "commit", "-qm", "driver attributes")
	for _, key := range []string{"core.pager", "pager.diff", "diff.external", "diff.owned.command", "diff.owned.textconv"} {
		sh(t, dir, "config", key, script)
	}
	sh(t, dir, "config", "diff.noprefix", "true")
	t.Setenv("GIT_EXTERNAL_DIFF", script)
	t.Setenv("GIT_PAGER", script)
	write(t, dir, "README", "changed\n")
	write(t, dir, "untracked", "new\n")
	before := diffRepositoryFiles(t, dir)
	got := run(t, "", "change", "diff", "README", "--repo", dir)
	want := diffOracle(t, dir, false, "HEAD", "--", "README")
	if got != want || !strings.Contains(got, "--- a/README") {
		t.Fatalf("pinned output\n%s", got)
	}
	fixtureDiff(t, dir, "HEAD", "WORKTREE", DiffOptions{Context: 3, MaxLines: defaultDiffLines})
	after := diffRepositoryFiles(t, dir)
	if !reflect.DeepEqual(before, after) {
		t.Fatal("diff changed repository metadata/index/objects")
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatalf("external program ran: %v", err)
	}
}

func TestDiffLimitsAndWireModel(t *testing.T) {
	dir := newRepo(t, "limits")
	write(t, dir, "README", strings.Repeat("line\n", defaultDiffLines+1))
	d := fixtureDiff(t, dir, "HEAD", "WORKTREE", DiffOptions{Context: 3, MaxLines: defaultDiffLines})
	if len(d.Files) != 1 || !d.Files[0].Truncated || d.Files[0].Added != defaultDiffLines+1 || d.Files[0].Removed != 1 || len(d.Files[0].Hunks) != 0 {
		t.Fatalf("line limit %+v", d.Files)
	}
	write(t, dir, "README", strings.Repeat("x", maxDiffBytes+100))
	d = fixtureDiff(t, dir, "HEAD", "WORKTREE", DiffOptions{Context: 3, MaxLines: defaultDiffLines})
	if !d.Files[0].Truncated || d.Files[0].Added != 1 || d.Files[0].Removed != 1 {
		t.Fatalf("byte limit %+v", d.Files)
	}
	write(t, dir, "README", "new\n")
	d = fixtureDiff(t, dir, "HEAD", "WORKTREE", DiffOptions{Context: 0, MaxLines: defaultDiffLines})
	b, err := json.Marshal(d.Files)
	if err != nil {
		t.Fatal(err)
	}
	// Independent structural mirror of diffview/model.go: JSON names and numeric
	// kinds must remain consumable without a translation by the future page.
	var model []struct {
		OldPath, NewPath, Status string
		Binary, Truncated        bool
		OldSize, NewSize         int64
		Added, Removed           int
		Hunks                    []struct {
			OldStart, OldCount, NewStart, NewCount int
			Header                                 string
			Lines                                  []struct {
				Kind      uint8
				Old, New  int
				Text      string
				NoNewline bool
			}
		}
	}
	if err := json.Unmarshal(b, &model); err != nil || model[0].Added != 1 || model[0].Removed != 1 || model[0].Hunks[0].OldCount != 1 || model[0].Hunks[0].Lines[1].Kind != 1 || model[0].Hunks[0].Lines[1].New != 1 {
		t.Fatalf("wire model %s: %v", b, err)
	}
	for _, key := range []string{`"oldPath"`, `"newPath"`, `"oldStart"`, `"oldCount"`, `"newStart"`, `"newCount"`, `"kind"`, `"added"`, `"removed"`} {
		if !strings.Contains(string(b), key) {
			t.Errorf("missing model key %s: %s", key, b)
		}
	}
}

func TestDiffDegenerateRootUnbornAndSettings(t *testing.T) {
	dir := newRepo(t, "root")
	root := sh(t, dir, "rev-parse", "HEAD")
	empty := sh(t, dir, "hash-object", "-t", "tree", "/dev/null")
	if got, want := run(t, "", "commit", "diff", root, "--repo", dir), diffOracle(t, dir, false, empty, root, "--"); got != want {
		t.Fatalf("root patch\nGOT %s\nWANT %s", got, want)
	}
	for _, side := range []string{"HEAD", "WORKTREE", "INDEX"} {
		d := fixtureDiff(t, dir, side, side, DiffOptions{Context: 3, MaxLines: 100})
		if len(d.Files) != 0 || d.Unified != "" {
			t.Fatalf("equal sides %+v", d)
		}
	}
	got := decode[DiffResult](t, run(t, "", "change", "diff", "README", "--from", "missing", "--to", "HEAD", "--repo", dir, "--output", "json"))
	if got.Comparison.Available || got.Comparison.Reason == "" || len(got.Files) != 0 {
		t.Fatalf("unavailable %+v", got)
	}
	for _, args := range [][]string{
		{"change", "diff", "README", "--context", "-1"},
		{"change", "diff", "../escape"},
		{"change", "diff", "README", "--pair", "dirty", "--from", "HEAD"},
		{"commit", "diff", "WORKTREE"},
	} {
		if _, _, err := try("", append(args, "--repo", dir)...); err == nil {
			t.Errorf("accepted %v", args)
		}
	}
	a, err := New()
	if err != nil {
		t.Fatal(err)
	}
	if a.r.Settings().Int("compare.diff.context") != 3 || a.r.Settings().Int("compare.diff.max-lines") != defaultDiffLines {
		t.Fatal("setting defaults")
	}
	for name, value := range map[string]string{"compare.diff.context": "-1", "compare.diff.max-lines": "0"} {
		if err := a.r.Settings().Set(name, value); err == nil {
			t.Errorf("accepted %s=%s", name, value)
		}
	}
	for _, key := range []string{"change.diff", "commit.diff"} {
		op := a.r.Operations[key]
		if op.HistoryKind != ochazuke.HistoryFetch || !op.Quiet {
			t.Fatalf("read registration %+v", op)
		}
	}
	unborn := filepath.Join(t.TempDir(), "unborn")
	if err := os.MkdirAll(unborn, 0o755); err != nil {
		t.Fatal(err)
	}
	sh(t, unborn, "init", "-qb", "main")
	write(t, unborn, "staged", "first\n")
	sh(t, unborn, "add", "staged")
	before := diffRepositoryFiles(t, unborn)
	got = decode[DiffResult](t, run(t, "", "change", "diff", "staged", "--repo", unborn, "--output", "json"))
	if len(got.Files) != 1 || got.Files[0].Status != "A" || got.Files[0].Added != 1 {
		t.Fatalf("unborn %+v", got)
	}
	if !reflect.DeepEqual(before, diffRepositoryFiles(t, unborn)) {
		t.Fatal("unborn diff wrote an object or index")
	}
}

func TestDiffParserFailures(t *testing.T) {
	for _, text := range []string{":bad\x00", ":100644 100644 abc def M\x00path\x00", "invalid\x00"} {
		if _, err := parseDiffMetadata(text); err == nil {
			t.Fatalf("accepted %q", text)
		}
	}
	for _, text := range []string{"@@@", "@@ -1,-2 +1 @@", "@@ -no +1 @@"} {
		if _, err := diffHunkHeader(text); err == nil {
			t.Fatalf("accepted %q", text)
		}
	}
	if err := gitStream(t.Context(), t.TempDir(), true, func(r io.Reader) error { _, e := io.Copy(io.Discard, r); return e }, "diff", "--no-index", "--", "missing-a", "missing-b"); err == nil {
		t.Fatal("no-index concealed a real failure")
	}
}

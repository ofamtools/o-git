package main

import (
	"bytes"
	"errors"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	"ofam.dev/ochazuke"
	ocli "ofam.dev/ochazuke/cli"
)

func fileActionApp(t *testing.T) (*App, *bytes.Buffer, *bytes.Buffer, *[][]string) {
	t.Helper()
	a, err := New()
	if err != nil {
		t.Fatal(err)
	}
	out, errout := new(bytes.Buffer), new(bytes.Buffer)
	a.Stdin, a.Stdout, a.Stderr = strings.NewReader(""), out, errout
	calls := new([][]string)
	a.r.SetOpener(ochazuke.Opener{GOOS: "darwin", Start: func(name string, args ...string) error {
		*calls = append(*calls, append([]string{name}, args...))
		return nil
	}})
	return a, out, errout, calls
}

func TestFilesystemCatalogTypesAndRoles(t *testing.T) {
	isolateCompare(t)
	a, _, _, _ := fileActionApp(t)
	data, err := ocli.Catalog(a.r, "o-git", Version)
	if err != nil {
		t.Fatal(err)
	}
	doc := decode[ocli.CatalogDoc](t, string(data))
	props := map[string]map[string]ochazuke.PropSpec{}
	verbs := map[string]ocli.CatalogVerb{}
	for _, n := range doc.Nouns {
		if !slices.Contains([]string{"repo", "worktree", "branch", "commit", "change", "comparison", "endpoint", "diff-file"}, n.Name) {
			continue
		}
		props[n.Name] = map[string]ochazuke.PropSpec{}
		for _, p := range n.Props {
			props[n.Name][p.Name] = p
		}
		if !props[n.Name]["id"].Key || props[n.Name]["label"].Role != ochazuke.RoleLabel {
			t.Errorf("%s lost its stable key/label roles", n.Name)
		}
		for _, v := range n.Verbs {
			verbs[n.Name+"."+v.Name] = v
			if p, ok := v.Params["repo"]; ok && (p.Type != "path" || p.Path != "dir") {
				t.Errorf("%s.%s repo: %+v", n.Name, v.Name, p)
			}
		}
	}
	for _, slot := range []struct{ noun, field, kind string }{
		{"repo", "path", "dir"}, {"worktree", "repo", "dir"}, {"worktree", "path", "dir"},
		{"branch", "repo", "dir"}, {"branch", "worktree", "dir"}, {"commit", "repo", "dir"},
		{"change", "repo", "dir"}, {"change", "path", "file"}, {"change", "old_path", "file"},
		{"endpoint", "repo", "dir"}, {"endpoint", "path", "dir"}, {"comparison", "repo", "dir"},
		{"diff-file", "repo", "dir"}, {"diff-file", "oldPath", "file"}, {"diff-file", "newPath", "file"},
	} {
		p := props[slot.noun][slot.field]
		if p.Type != "path" || p.Path != slot.kind || !p.ReadOnly {
			t.Errorf("%s.%s: %+v", slot.noun, slot.field, p)
		}
	}
	for _, noun := range []string{"repo", "worktree", "change", "diff-file"} {
		for _, name := range []string{"reveal", "open", "open-with", "yank-path", "edit-file", "history", "show"} {
			if v, ok := verbs[noun+"."+name]; !ok || v.Disabled {
				t.Errorf("%s.%s is unavailable: %+v", noun, name, v)
			}
		}
	}
	for _, noun := range []string{"repo", "worktree", "branch", "commit", "change", "comparison", "endpoint", "diff-file"} {
		if v := verbs[noun+".show"]; v.Standard != "show" {
			t.Errorf("%s.show shadows the shared record form: %+v", noun, v)
		}
	}
	for _, key := range []string{"change.open", "diff-file.open", "change.export", "diff-file.export"} {
		if p := verbs[key].Params["source"]; p.From != "row:source" || !p.Plumbing {
			t.Errorf("%s lost revision provenance: %+v", key, p)
		}
	}
	if p := verbs["repo.view"].Params["path"]; p.Type != "path" || p.Path != "dir" {
		t.Fatalf("view path %+v", p)
	}
	if !verbs["repo.view"].Foreground {
		t.Fatal("view is not a foreground entry point")
	}
	if dataPage().Detail.Kind != ocli.DetailRecordForm {
		t.Fatal("mounted detail does not request the shared show form")
	}
}

func TestDirectoryPathActionsAndRepositoryNames(t *testing.T) {
	isolateCompare(t)
	dir := newRepo(t, "repo with spaces")
	fakeProj(t, Known{Name: "friendly-name", Dir: dir})
	a, out, _, calls := fileActionApp(t)
	for _, args := range [][]string{
		{"repo", "reveal", "--repo", "friendly-name"},
		{"repo", "open", "--target", repoID(dir)},
		{"worktree", "open-with", "--path", dir, "--with", "TextEdit"},
	} {
		out.Reset()
		if err := a.Shell().Run(t.Context(), args); err != nil {
			t.Fatal(args, err)
		}
		if result := decode[resolvedPath](t, out.String()); result.Path != dir || !result.Exists {
			t.Fatalf("%v: %+v", args, result)
		}
	}
	want := [][]string{{"open", "-R", "--", dir}, {"open", "--", dir}, {"open", "-a", "TextEdit", "--", dir}}
	if !reflect.DeepEqual(*calls, want) {
		t.Fatalf("opener calls %q, want %q", *calls, want)
	}
	out.Reset()
	if err := a.Shell().Run(t.Context(), []string{"worktree", "yank-path", "--target", worktreeID(dir)}); err != nil {
		t.Fatal(err)
	}
	value, err := a.r.ReadYank("\"")
	if err != nil || value.Text != dir {
		t.Fatalf("register %+v %v", value, err)
	}
}

func TestChangePathsAreRepositoryRelativeAndHistoricalPathsStayHistorical(t *testing.T) {
	isolateCompare(t)
	dir := newRepo(t, "r")
	write(t, dir, "README", "working copy\n")
	a, out, _, calls := fileActionApp(t)
	if err := a.Shell().Run(t.Context(), []string{"change", "open", "README", "--repo", dir}); err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(dir, "README")
	if result := decode[resolvedPath](t, out.String()); result.Path != want {
		t.Fatal(result)
	}
	if !reflect.DeepEqual(*calls, [][]string{{"open", "--", want}}) {
		t.Fatal(*calls)
	}
	out.Reset()
	if err := a.Shell().Run(t.Context(), []string{"change", "yank-path", "--repo", dir, "--target", "change/" + dir + ":README"}); err != nil {
		t.Fatal(err)
	}
	value, _ := a.r.ReadYank("\"")
	if value.Text != want {
		t.Fatalf("copied %q, want %q", value.Text, want)
	}

	// This is exactly what a mounted historical row forwards. A current file
	// at the same path must not make that historical subject openable.
	before := len(*calls)
	for _, noun := range []string{"change", "diff-file"} {
		for _, verb := range []string{"open", "reveal", "edit-file"} {
			out.Reset()
			err := a.Shell().Run(t.Context(), []string{noun, verb, "README", "--repo", dir, "--source", "commit", "--revision", sh(t, dir, "rev-parse", "HEAD")})
			if err == nil || !strings.Contains(err.Error(), "not a working file") {
				t.Fatalf("%s.%s: %v", noun, verb, err)
			}
		}
	}
	if len(*calls) != before {
		t.Fatal("historical path reached the system opener")
	}

	if err := os.Remove(filepath.Join(dir, "README")); err != nil {
		t.Fatal(err)
	}
	out.Reset()
	if err := a.Shell().Run(t.Context(), []string{"change", "yank-path", "README", "--repo", dir}); err != nil {
		t.Fatal(err)
	}
	if result := decode[resolvedPath](t, out.String()); result.Exists || result.Path != want {
		t.Fatal(result)
	}
	if err := a.Shell().Run(t.Context(), []string{"change", "open", "README", "--repo", dir}); err == nil {
		t.Fatal("deleted path opened as an existing working file")
	}
}

func TestChangeAndDiffFileExportExactHistoricalBytes(t *testing.T) {
	isolateCompare(t)
	dir := newRepo(t, "r")
	commit(t, dir, "README", "historical")
	sha := sh(t, dir, "rev-parse", "HEAD")
	write(t, dir, "README", "today\n")
	for _, noun := range []string{"change", "diff-file"} {
		destination := filepath.Join(t.TempDir(), "snapshot")
		result := decode[FileExport](t, run(t, "", noun, "export", "README", destination, "--repo", dir, "--source", "commit", "--revision", sha))
		data, err := os.ReadFile(destination)
		if err != nil || string(data) != "historical\n" || result.Bytes != 11 {
			t.Fatalf("%s: %q %+v %v", noun, data, result, err)
		}
		if _, _, err := try("", noun, "export", "README", destination, "--repo", dir); err == nil {
			t.Fatal("export overwrote an existing destination")
		}
		if got, _ := os.ReadFile(destination); string(got) != "historical\n" {
			t.Fatal("refused export changed the destination")
		}
	}
	if err := os.Remove(filepath.Join(dir, "README")); err != nil {
		t.Fatal(err)
	}
	destination := filepath.Join(t.TempDir(), "deleted-content")
	run(t, "", "change", "export", "README", destination, "--side", "from", "--repo", dir)
	if got, _ := os.ReadFile(destination); string(got) != "historical\n" {
		t.Fatalf("deleted content %q", got)
	}
}

func TestDiffOutputDestinationAndAbsoluteChooserPaths(t *testing.T) {
	isolateCompare(t)
	dir := newRepo(t, "r")
	write(t, dir, "README", "changed\n")
	destination := filepath.Join(t.TempDir(), "change.patch")
	run(t, "", "change", "diff", filepath.Join(dir, "README"), "--repo", dir, "--destination", destination)
	patch, err := os.ReadFile(destination)
	if err != nil || string(patch) != diffOracle(t, dir, false, "HEAD", "--", "README") {
		t.Fatalf("patch %q %v", patch, err)
	}
	rows := decode[[]row[DiffFileRow]](t, run(t, "", "diff-file", "list", "--repo", dir))
	if len(rows) != 1 || rows[0].Data.NewPath != "README" || rows[0].Data.Source != "worktree" {
		t.Fatalf("diff files %+v", rows)
	}
	if _, _, err := try("", "change", "inspect", filepath.Join(filepath.Dir(dir), "outside"), "--repo", dir); err == nil {
		t.Fatal("accepted a path outside the repository")
	}
}

func TestRegisteredViewAndBareViewHaveTheSameOptions(t *testing.T) {
	isolateCompare(t)
	dir := newRepo(t, "r")
	fakeProj(t, Known{Name: "friendly-name", Dir: dir})
	flags := []string{"--repo", "friendly-name", "--smoke", "--width", "100", "--height", "30"}
	registered := run(t, "", append([]string{"repo", "view"}, flags...)...)
	bare := run(t, "", append([]string{"view"}, flags...)...)
	if bare != registered || !strings.Contains(registered, "Worktrees friendly-name") {
		t.Fatalf("registered and bare view diverged:\n%s\n%s", registered, bare)
	}
}

func TestFileExportFailureDoesNotCreateOrReplaceDestination(t *testing.T) {
	dir := t.TempDir()
	destination := filepath.Join(dir, "output")
	fail := func(w io.Writer) error { _, _ = io.WriteString(w, "partial"); return errors.New("read failed") }
	if err := exportToFile(destination, false, fail); err == nil {
		t.Fatal("lost source failure")
	}
	if _, err := os.Stat(destination); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("partial destination survived")
	}
	if err := os.WriteFile(destination, []byte("original"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := exportToFile(destination, true, fail); err == nil {
		t.Fatal("lost source failure")
	}
	if got, _ := os.ReadFile(destination); string(got) != "original" {
		t.Fatal("failure replaced existing output")
	}
	entries, err := os.ReadDir(dir)
	if err != nil || len(entries) != 1 {
		t.Fatalf("temporary export remained: %v %v", entries, err)
	}
}

func TestEditorCommandKeepsFilenameOutsideShellText(t *testing.T) {
	t.Setenv("VISUAL", "editor --wait")
	t.Setenv("EDITOR", "other-editor")
	path := "/tmp/a ; echo not-a-command"
	cmd := editorCommand(path)
	if !reflect.DeepEqual(cmd.Args, []string{"sh", "-c", "exec editor --wait \"$@\"", "o-git-editor", path}) {
		t.Fatal(cmd.Args)
	}
}

// read-only store calls remain Pending, even when addressed by a row key from
// a different process; no owner repository or real opener is involved.
func TestScopedFactGetFindsAnExplicitRepository(t *testing.T) {
	isolateCompare(t)
	dir := newRepo(t, "r")
	data := decode[map[string]any](t, run(t, "", "repo", "get", repoID(dir)))
	if data["path"] != dir || data["name"] != "r" {
		t.Fatal(data)
	}
	sha := sh(t, dir, "rev-parse", "HEAD")
	commit := decode[map[string]any](t, run(t, "", "commit", "get", "commit/"+dir+":"+sha))
	if commit["sha"] != sha {
		t.Fatal(commit)
	}
}

func TestFileExportKeepsSymlinkBytesAndNeverReadsTheOutsideTarget(t *testing.T) {
	isolateCompare(t)
	dir := newRepo(t, "r")
	for _, name := range []string{"outside.txt", "other.txt"} {
		if err := os.WriteFile(filepath.Join(filepath.Dir(dir), name), []byte("private target contents"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	link := filepath.Join(dir, "link")
	if err := os.Symlink("../outside.txt", link); err != nil {
		t.Fatal(err)
	}
	sh(t, dir, "add", "link")
	sh(t, dir, "commit", "-q", "-m", "record symlink")
	sha := sh(t, dir, "rev-parse", "HEAD")
	if err := os.Remove(link); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("../other.txt", link); err != nil {
		t.Fatal(err)
	}
	for _, noun := range []string{"change", "diff-file"} {
		for _, tc := range []struct {
			name string
			args []string
			want string
		}{
			{"working", nil, "../other.txt"},
			{"historical", []string{"--source", "commit", "--revision", sha}, "../outside.txt"},
			{"from", []string{"--side", "from"}, "../outside.txt"},
		} {
			t.Run(noun+"/"+tc.name, func(t *testing.T) {
				destination := filepath.Join(t.TempDir(), "symlink-bytes")
				args := []string{noun, "export", "link", destination, "--repo", dir}
				result := decode[FileExport](t, run(t, "", append(args, tc.args...)...))
				content, err := os.ReadFile(destination)
				if err != nil || string(content) != tc.want || result.Bytes != int64(len(tc.want)) {
					t.Fatalf("exported %q (%+v), want literal %q: %v", content, result, tc.want, err)
				}
				if strings.Contains(string(content), "private target contents") {
					t.Fatal("symlink export read a file outside the repository")
				}
			})
		}
	}
}

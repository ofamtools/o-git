package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"testing"

	ocli "github.com/unixsuperhero/ochazuke/cli"
)

// jobLine is the line the shell prints for a job that is not Quiet.
var jobLine = regexp.MustCompile(`(?m)^job #\d+ (worktree|branch|repo|compare)\.\w+ done\n`)

// run runs one command line and fails on an error or stray stderr.
func run(t *testing.T, stdin string, args ...string) string {
	t.Helper()
	out, errOut, err := try(stdin, args...)
	if err != nil {
		t.Fatalf("%v: %v\n%s", args, err, errOut)
	}
	// A mutation is not Quiet: its job line is the only stderr allowed.
	if rest := jobLine.ReplaceAllString(errOut, ""); rest != "" {
		t.Fatalf("%v wrote to stderr: %q", args, errOut)
	}
	return out
}

func try(stdin string, args ...string) (string, string, error) {
	var out, errOut bytes.Buffer
	err := Main(args, strings.NewReader(stdin), &out, &errOut)
	return out.String(), errOut.String(), err
}

func decode[T any](t *testing.T, text string) T {
	t.Helper()
	var v T
	if err := json.Unmarshal([]byte(text), &v); err != nil {
		t.Fatalf("%v\n%s", err, text)
	}
	return v
}

type row[T any] struct {
	Noun  string   `json:"noun"`
	ID    string   `json:"id"`
	Label string   `json:"label"`
	Data  T        `json:"data"`
	Cells []string `json:"cells"`
}

// fakeProj makes an o-proj that lists repos, and points O_GIT_PROJ at it.
func fakeProj(t *testing.T, repos ...Known) {
	t.Helper()
	data, _ := json.Marshal(repos)
	bin := filepath.Join(t.TempDir(), "o-proj")
	list := filepath.Join(filepath.Dir(bin), "list.json")
	if err := os.WriteFile(list, data, 0o644); err != nil {
		t.Fatal(err)
	}
	script := "#!/bin/sh\n[ \"$1\" = list ] || exit 2\ncat '" + list + "'\n"
	if err := os.WriteFile(bin, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("O_GIT_PROJ", bin)
}

func TestRepoListAndStatus(t *testing.T) {
	a := newRepo(t, "alpha")
	b := newRepo(t, "beta")
	extra := newRepo(t, "extra")
	write(t, b, "README", "dirty\n")
	fakeProj(t, Known{Name: "alpha", Dir: a}, Known{Name: "b-repo", Dir: b}, Known{Name: "gone", Dir: filepath.Join(t.TempDir(), "gone")})

	rows := decode[[]row[Repo]](t, run(t, "", "repo", "list", "--path", extra+","+a))
	if len(rows) != 4 {
		t.Fatalf("rows %+v", rows)
	}
	if rows[0].ID != "repo/"+a || rows[0].Label != "alpha" || rows[0].Data.State != "clean" || rows[0].Data.Branch != "main" {
		t.Fatalf("alpha %+v", rows[0])
	}
	if rows[1].Label != "b-repo" || rows[1].Data.State != "dirty" || rows[1].Data.Unstaged != 1 || rows[1].Cells[3] != "~1" {
		t.Fatalf("beta %+v", rows[1])
	}
	if rows[2].Label != "gone" || rows[2].Data.State != "missing" {
		t.Fatalf("missing %+v", rows[2])
	}
	if rows[3].Label != "extra" || len(rows[3].Cells) != len(columns["repo"]) {
		t.Fatalf("extra (and alpha listed once) %+v", rows[3])
	}

	// REPO is a name o-proj lists or a directory.
	if s := decode[Repo](t, run(t, "", "repo", "status", "b-repo")); s.Path != b || s.Unstaged != 1 {
		t.Fatalf("status by name %+v", s)
	}
	if s := decode[Repo](t, run(t, "", "repo", "status", extra, "--json")); s.Name != "extra" || s.State != "clean" {
		t.Fatalf("status by path %+v", s)
	}
	if _, _, err := try("", "repo", "status", "nope"); err == nil || !strings.Contains(err.Error(), `no repo "nope"`) {
		t.Fatalf("unknown repo: %v", err)
	}

	// Without o-proj: --path still answers, nothing at all is an error.
	t.Setenv("O_GIT_PROJ", "none")
	if rows := decode[[]row[Repo]](t, run(t, "", "repo", "list", extra)); len(rows) != 1 {
		t.Fatalf("path only %+v", rows)
	}
	if _, _, err := try("", "repo", "list"); err == nil || !strings.Contains(err.Error(), "o-proj is not available") {
		t.Fatalf("no o-proj: %v", err)
	}
}

func TestRepoShowAndState(t *testing.T) {
	dir := newRepo(t, "r")
	run(t, "", "worktree", "add", "feat", "--repo", dir)
	d := decode[RepoDetail](t, run(t, "", "repo", "show", dir))
	if d.Repo.Name != "r" || len(d.Worktrees) != 2 || len(d.Branches) != 2 {
		t.Fatalf("show %+v", d)
	}

	out := run(t, "", "repo", "state", "--path", dir)
	sc := bufio.NewScanner(strings.NewReader(out))
	var nouns []string
	for sc.Scan() {
		var r row[json.RawMessage]
		if err := json.Unmarshal(sc.Bytes(), &r); err != nil {
			t.Fatalf("line %q: %v", sc.Text(), err)
		}
		if len(r.Cells) != len(columns[r.Noun]) {
			t.Fatalf("cells %+v", r)
		}
		nouns = append(nouns, r.Noun)
	}
	if strings.Join(nouns, " ") != "repo worktree worktree branch branch" {
		t.Fatalf("state nouns %v\n%s", nouns, out)
	}
}

func TestWorktreeAddRemove(t *testing.T) {
	dir := newRepo(t, "r")
	add := decode[AddResult](t, run(t, "", "worktree", "add", "feat", "--repo", dir))
	want := filepath.Join(filepath.Dir(dir), "r-feat")
	if add.Path != want || !add.Created || add.Branch != "feat" {
		t.Fatalf("add %+v", add)
	}
	if _, _, err := try("", "worktree", "add", "feat", "--repo", dir); err == nil || !strings.Contains(err.Error(), "exists") {
		t.Fatalf("add twice: %v", err)
	}
	custom := filepath.Join(t.TempDir(), "elsewhere")
	if add := decode[AddResult](t, run(t, "", "worktree", "add", "topic/x", custom, "--repo", dir, "--from", "main")); add.Path != custom {
		t.Fatalf("add at a path %+v", add)
	}
	rows := decode[[]row[WorktreeRow]](t, run(t, "", "worktree", "list", dir))
	if len(rows) != 3 || !rows[0].Data.Main || rows[1].Data.Branch != "feat" {
		t.Fatalf("list %+v", rows)
	}

	// Declining the confirm changes nothing and fails.
	if _, errOut, err := try("n\n", "worktree", "remove", want); err == nil || !strings.Contains(errOut, "Remove worktree "+want+"?") {
		t.Fatalf("declined: %v %q", err, errOut)
	}
	if _, err := os.Stat(want); err != nil {
		t.Fatal("declined remove removed the worktree")
	}
	// A dirty worktree is refused without --force.
	write(t, want, "scratch", "x\n")
	if _, _, err := try("", "worktree", "remove", want, "--yes"); err == nil || !strings.Contains(err.Error(), "has changes (?1); --force") {
		t.Fatalf("dirty remove: %v", err)
	}
	if _, err := os.Stat(want); err != nil {
		t.Fatal("refused remove removed the worktree")
	}
	// Answering y on stdin is --yes.
	out, errOut, err := try("y\n", "worktree", "remove", want, "--force")
	if err != nil || !strings.Contains(errOut, "Remove worktree "+want+"? [y/N]") {
		t.Fatalf("confirmed: %v %q", err, errOut)
	}
	if res := decode[RemoveResult](t, out); !res.Force || res.Removed != want {
		t.Fatalf("forced %+v", res)
	}
	if _, err := os.Stat(want); !os.IsNotExist(err) {
		t.Fatalf("still there: %v", err)
	}
	// The main worktree is never removed.
	if _, _, err := try("", "worktree", "remove", dir, "--yes", "--force"); err == nil || !strings.Contains(err.Error(), "main worktree") {
		t.Fatalf("main: %v", err)
	}
	// A host's row id (--target) names the worktree too.
	if _, errOut, err := try("n\n", "worktree", "remove", "--target", "worktree/"+dir); err == nil || !strings.Contains(errOut, "Remove worktree "+tilde(dir)+"?") {
		t.Fatalf("--target confirm: %v %q", err, errOut)
	}
	if _, _, err := try("", "worktree", "remove", "--target", "worktree/"+dir, "--yes"); err == nil || !strings.Contains(err.Error(), "main worktree") {
		t.Fatalf("--target main: %v", err)
	}
	if _, _, err := try("", "worktree", "remove", "--yes"); err == nil || !strings.Contains(err.Error(), "a worktree is required") {
		t.Fatalf("no target: %v", err)
	}
}

func TestBranchVerbs(t *testing.T) {
	dir := newRepo(t, "r")
	t.Chdir(dir)
	if res := decode[BranchResult](t, run(t, "", "branch", "create", "topic")); res.Branch != "topic" || res.Switched {
		t.Fatalf("create %+v", res)
	}
	if res := decode[BranchResult](t, run(t, "", "branch", "create", "work", "main", "--switch")); !res.Switched {
		t.Fatalf("create --switch %+v", res)
	}
	commit(t, dir, "w", "unmerged work")
	run(t, "", "branch", "switch", "main")
	rows := decode[[]row[BranchRow]](t, run(t, "", "branch", "list"))
	byName := map[string]row[BranchRow]{}
	for _, r := range rows {
		byName[r.Label] = r
	}
	if len(rows) != 3 || !byName["main"].Data.Current || byName["work"].Data.Merged || !byName["topic"].Data.Merged || byName["main"].Cells[0] != "* main" {
		t.Fatalf("list %+v", rows)
	}
	if byName["work"].ID != "branch/"+dir+":work" {
		t.Fatalf("id %s", byName["work"].ID)
	}

	// switch takes the targeted row too.
	if res := decode[BranchResult](t, run(t, "", "branch", "switch", "--target", "branch/topic")); res.Branch != "topic" {
		t.Fatalf("switch row %+v", res)
	}
	if _, _, err := try("", "branch", "delete", "topic", "--yes"); err == nil || !strings.Contains(err.Error(), "checked out") {
		t.Fatalf("delete current: %v", err)
	}
	run(t, "", "branch", "switch", "main")
	if _, errOut, err := try("\n", "branch", "delete", "topic"); err == nil || !strings.Contains(errOut, "Delete branch topic?") {
		t.Fatalf("declined: %v %q", err, errOut)
	}
	if _, errOut, err := try("\n", "branch", "delete", "--target", "branch/topic"); err == nil || !strings.Contains(errOut, "Delete branch topic?") {
		t.Fatalf("--target confirm: %v %q", err, errOut)
	}
	if res := decode[BranchResult](t, run(t, "", "branch", "delete", "topic", "--yes")); !res.Deleted {
		t.Fatalf("delete merged %+v", res)
	}
	if _, _, err := try("", "branch", "delete", "work", "--repo", dir, "--yes"); err == nil || !strings.Contains(err.Error(), "not fully merged; --force") {
		t.Fatalf("delete unmerged: %v", err)
	}
	if res := decode[BranchResult](t, run(t, "", "branch", "delete", "work", "--repo", dir, "--yes", "--force")); !res.Force {
		t.Fatalf("forced %+v", res)
	}
	if ok, _ := BranchExists(t.Context(), dir, "work"); ok {
		t.Fatal("work still exists")
	}
	if _, _, err := try("", "branch", "create", "-x"); err == nil {
		t.Fatal("created a branch named -x")
	}
}

func TestCatalog(t *testing.T) {
	cat := decode[struct {
		Name    string `json:"name"`
		Version string `json:"version"`
		State   string `json:"state"`
		Nouns   []struct {
			Name    string `json:"name"`
			Columns []struct {
				Title string `json:"title"`
			} `json:"columns"`
			Verbs []struct {
				Name    string `json:"name"`
				Confirm string `json:"confirm"`
				Rows    string `json:"rows"`
				Kind    string `json:"kind"`
			} `json:"verbs"`
		} `json:"nouns"`
	}](t, run(t, "", "catalog"))
	if cat.Name != "o-git" || cat.Version != Version || cat.State != "repo.state" {
		t.Fatalf("catalog %s %s %s", cat.Name, cat.Version, cat.State)
	}
	got := map[string]string{}
	columnCounts := map[string]int{"commit": len(commitColumns(true)), "change": 4}
	for noun, cols := range columns {
		columnCounts[noun] = len(cols)
	}
	for _, n := range cat.Nouns {
		count, known := columnCounts[n.Name]
		if !known {
			continue
		}
		if len(n.Columns) != count {
			t.Fatalf("%s columns %+v", n.Name, n.Columns)
		}
		var verbs []string
		for _, v := range n.Verbs {
			if v.Kind != "application" {
				t.Fatalf("%s.%s kind %q", n.Name, v.Name, v.Kind)
			}
			s := v.Name
			if v.Confirm != "" {
				s += "!"
			}
			if v.Rows != "" {
				s += "=" + v.Rows
			}
			verbs = append(verbs, s)
		}
		got[n.Name] = strings.Join(verbs, " ")
	}
	want := map[string]string{
		"repo":     "compare fetch list=repo show state status",
		"endpoint": "compare copy list=endpoint resolve show",
		"compare":  "close commit-open commits=commit copy fetch file-open files=change list=compare open reload save set show swap",
		"worktree": "add compare list=worktree remove!",
		"branch":   "compare create delete! list=branch switch",
		"commit":   "compare copy diff files=change list=commit show",
		"change":   "copy diff list=change show",
	}
	for noun, verbs := range want {
		if got[noun] != verbs {
			t.Fatalf("%s verbs %q, want %q", noun, got[noun], verbs)
		}
	}
}

// --format prints a list verb's rows as the table the page draws: the
// noun's declared columns over the rows' cells, one table per noun.
func TestListFormats(t *testing.T) {
	dir := newRepo(t, "r")
	out := run(t, "", "worktree", "list", dir, "--format", "toon")
	if want := "worktree[1]{branch,state,path,head}:\n  main,clean main," + tilde(dir) + ","; !strings.HasPrefix(out, want) {
		t.Fatalf("worktree toon:\n%s", out)
	}
	out = run(t, "", "branch", "list", dir, "--format", "markdown")
	if !strings.HasPrefix(out, "| branch | sync | age | upstream | merged | subject |\n| --- | --- | ---: |") || !strings.Contains(out, "| * main | - |") {
		t.Fatalf("branch markdown:\n%s", out)
	}
	out = run(t, "", "repo", "state", "--path", dir, "--format", "table")
	for _, head := range []string{"repo  branch", "branch  state", "branch  sync"} {
		if !strings.Contains(out, head) {
			t.Fatalf("state table has no %q:\n%s", head, out)
		}
	}
}

// The catalog declares the Git page as a data page, so a host draws its
// lists: split, the repo, branch and worktree lists over their nouns'
// row sources, and a detail whose fields name the rows' records.
func TestCatalogDeclaresTheDataPage(t *testing.T) {
	cat := decode[ocli.CatalogDoc](t, run(t, "", "catalog"))
	if len(cat.Pages) != 2 {
		t.Fatalf("pages %+v", cat.Pages)
	}
	if p := cat.Pages[1]; p.ID != comparePageID || p.Title != "Compare" || p.IsData() {
		t.Fatalf("Compare should remain TUI-only: %+v", p)
	}
	p := cat.Pages[0]
	if p.ID != pageID || p.Title != "Git" || !p.IsData() || p.Layout != ocli.LayoutSplit {
		t.Fatalf("page %+v", p)
	}
	var lists []string
	for _, l := range p.Lists {
		if verb, rows := cat.RowSource(l.Noun); verb != "list" || rows != l.Noun || l.Title == "" {
			t.Fatalf("list %+v: row source %s of %s", l, verb, rows)
		}
		lists = append(lists, l.ID+"="+l.Noun)
	}
	if got, want := strings.Join(lists, " "), reposID+"=repo "+branchesID+"=branch "+worktreesID+"=worktree"; got != want {
		t.Fatalf("lists %q, want %q", got, want)
	}
	if p.Detail == nil || len(p.Detail.Fields) == 0 || len(p.Widgets) != 0 {
		t.Fatalf("detail %+v widgets %+v", p.Detail, p.Widgets)
	}
	// Every detail field is a field of some row's record.
	known := map[string]bool{}
	for _, v := range []any{Repo{}, WorktreeRow{}, BranchRow{}} {
		for _, f := range reflect.VisibleFields(reflect.TypeOf(v)) {
			if name, _, _ := strings.Cut(f.Tag.Get("json"), ","); name != "" && name != "-" {
				known[name] = true
			}
		}
	}
	for _, f := range p.Detail.Fields {
		if !known[f] {
			t.Errorf("detail field %q is in no row's record", f)
		}
	}
}

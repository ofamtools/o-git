package main

import (
	"context"
	"fmt"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/unixsuperhero/ochazuke"
	"github.com/unixsuperhero/ochazuke/diffview"
)

const diffFrame = "compare-diff"
const diffID = "compare/diff"
const diffFilesID = "compare/diff-files"

func (a *App) registerCompareViews() error {
	r := a.r
	if err := r.DeclareSetting(
		ochazuke.Setting{Name: "compare.diff.layout", Type: ochazuke.SettingString, Enum: []string{"auto", "unified", "split"}, Default: "auto", Description: "Diff layout; auto splits at 140 content columns"},
		ochazuke.Setting{Name: "compare.diff.highlight", Type: ochazuke.SettingBoolean, Default: true, Description: "Syntax highlighting in file diffs"},
	); err != nil {
		return err
	}
	diffview.Register(r)
	r.Define("compare").Verb("commit-open", a.compareCommitOpen, "show the selected commit's files").Fetch().Quiet().
		Verb("file-open", a.compareFileOpen, "open the selected file's structured diff").Fetch().Quiet().
		Verb("close", func(c ochazuke.Ctx) ochazuke.Result {
			if r.TopModal() != diffFrame {
				return ochazuke.Failed(fmt.Errorf("no file diff is open"))
			}
			err := r.Pop()
			a.compare.diff = nil
			a.compare.diffFiles = nil
			if err != nil {
				return ochazuke.Failed(err)
			}
			return ochazuke.Done(nil)
		}, "close the file diff and return to its file row")
	r.Define("commit").Verb("compare", func(c ochazuke.Ctx) ochazuke.Result {
		row, ok := rowData[Commit](c)
		if !ok || row.SHA == "" {
			return ochazuke.Failed(fmt.Errorf("no commit selected"))
		}
		return a.compareSet(ochazuke.Ctx{Args: ochazuke.Arguments{"repo": row.Repo, "side": ochazuke.StringArg(c.Args, "side", "to"), "endpoint": row.SHA}})
	}, "set either comparison side to this commit")
	op := r.Operations["commit.compare"]
	op.Parameters = map[string]ochazuke.Parameter{"side": {Type: "string", Enum: []string{"from", "to"}}}
	r.Operations["commit.compare"] = op
	setHistory(r, ochazuke.HistoryAction, "commit.compare", "compare.close")
	r.DefineGuard("compare-commit-row", func(r *ochazuke.Runtime, ev ochazuke.KeyEvent) bool {
		inst := r.Instances[ev.Focus]
		if inst == nil {
			return false
		}
		row, ok := inst.Data.(Commit)
		return ok && row.SHA != "" && row.More == 0
	})
	r.DefineGuard("compare-file-row", func(r *ochazuke.Runtime, ev ochazuke.KeyEvent) bool {
		inst := r.Instances[ev.Focus]
		if inst == nil {
			return false
		}
		_, ok := inst.Data.(Change)
		return ok
	})
	// A live file row already supplies the path, including commit-file rows.
	// Keep the CLI's existing copy handler for requests without a live row.
	op = r.Operations["change.copy"]
	op.App = func(c ochazuke.Ctx) ochazuke.Result {
		if file, ok := rowData[Change](c); ok && a.r.Page == comparePageID {
			_, err := r.Execute(ochazuke.Request{Noun: "ochazuke.register", Verb: "set", Args: ochazuke.Arguments{"name": "\"", "text": file.Path}})
			if err != nil {
				return ochazuke.Failed(err)
			}
			return ochazuke.Done(file.Path)
		}
		return a.changeCopy(c)
	}
	r.Operations["change.copy"] = op
	a.m.ModalViews[diffFrame] = ochazuke.ModalView{Title: "Diff", Render: a.renderCompareDiff}
	return r.LoadDefaultBindings("compare-views", []byte(`{"bindings":[
  {"context":"commit/normal","bindings":[
   {"key":"enter","action":"compare.commit-open","when":"compare-commit-row"},
   {"key":"f","action":"commit.compare","args":{"side":"from"},"when":"compare-commit-row"},
   {"key":"t","action":"commit.compare","args":{"side":"to"},"when":"compare-commit-row"},
   {"key":"y y","action":"commit.copy","when":"compare-commit-row"}
  ]},
  {"context":"change/normal","bindings":[
   {"key":"enter","action":"compare.file-open","when":"compare-file-row"},
   {"key":"y y","action":"change.copy","when":"compare-file-row"}
  ]},
  {"context":"compare-diff/normal","bindings":[
   {"key":"q","action":"compare.close"},{"key":"esc","action":"compare.close"}
  ]}
 ]}`))
}

func (a *App) compareFilesTitle() string {
	if a.compare.fileCommit != "" {
		return "Files " + shortSHA(a.compare.fileCommit)
	}
	return "Files"
}

func (a *App) compareCommitOpen(c ochazuke.Ctx) ochazuke.Result {
	row, ok := rowData[Commit](c)
	if !ok || row.SHA == "" {
		return ochazuke.Failed(fmt.Errorf("no commit selected"))
	}
	return a.commitFiles(c).ReplacingAt(comparePageID, "commit-files").Then(func(_ ochazuke.Ctx, v any, err error) {
		if err != nil {
			return
		}
		a.compare.fileCommit = row.SHA
		a.compare.fileReason = ""
		_ = a.compare.files.SetItems(v.([]ochazuke.ListItem))
		_ = a.compare.files.Focus()
	})
}

func (a *App) compareFileOpen(c ochazuke.Ctx) ochazuke.Result {
	file, ok := rowData[Change](c)
	if !ok {
		return ochazuke.Failed(fmt.Errorf("no file selected"))
	}
	return a.loadCompareDiff(file.Path, a.r.TopModal() != diffFrame)
}

func viewFile(file DiffFile) diffview.File {
	out := diffview.File{OldPath: file.OldPath, NewPath: file.NewPath, Status: file.Status, Binary: file.Binary, OldSize: file.OldSize, NewSize: file.NewSize, Truncated: file.Truncated, Added: file.Added, Removed: file.Removed}
	for _, h := range file.Hunks {
		hunk := diffview.Hunk{OldStart: h.OldStart, OldCount: h.OldCount, NewStart: h.NewStart, NewCount: h.NewCount, Header: h.Header}
		for _, line := range h.Lines {
			hunk.Lines = append(hunk.Lines, diffview.Line{Kind: diffview.Kind(line.Kind), Text: line.Text, Old: line.Old, New: line.New, NoNewline: line.NoNewline})
		}
		out.Hunks = append(out.Hunks, hunk)
	}
	return out
}

func (a *App) loadCompareDiff(path string, opening bool) ochazuke.Result {
	p := a.compare
	if p.shown == nil {
		return ochazuke.Failed(fmt.Errorf("comparison is still loading"))
	}
	pair := *p.shown
	c := ochazuke.Ctx{Args: ochazuke.Arguments{"repo": pair.Repo, "path": path, "output": "json", "sha": p.fileCommit}}
	var res ochazuke.Result
	if p.fileCommit != "" {
		res = a.commitDiff(c)
	} else {
		opts := a.diffOptions(c)
		res = ochazuke.Pending(func(ctx context.Context) (any, error) { return BuildDiff(ctx, pair, opts) })
	}
	return res.ReplacingAt("app", "compare-diff-file").Then(func(_ ochazuke.Ctx, v any, err error) {
		if err != nil {
			return
		}
		if !opening && a.r.TopModal() != diffFrame {
			return
		}
		data := v.(DiffResult)
		if len(data.Files) == 0 {
			a.r.UI().Info("No diff for " + path)
			return
		}
		file := viewFile(data.Files[0])
		if opening {
			if _, err := a.r.OpenModal(diffFrame, ochazuke.Payload{}); err != nil {
				a.r.UI().Error(err.Error())
				return
			}
			list, err := a.r.AddList(diffFilesID, nil, ochazuke.Parent(diffFrame), ochazuke.NoMarks(), ochazuke.Columns(compareFileColumns...))
			if err != nil {
				a.r.UI().Error(err.Error())
				_ = a.r.Pop()
				return
			}
			var rows []ochazuke.ListItem
			for _, row := range p.files.Items() {
				if _, ok := row.Data.(Change); ok {
					row.ID = "diff/" + row.ID
					rows = append(rows, row)
				}
			}
			_ = list.SetItems(rows)
			p.diffFiles = list
			view, err := diffview.New(a.r, diffID, diffFrame, file,
				diffview.WithLayout(diffview.Layout(a.r.Settings().String("compare.diff.layout"))),
				diffview.WithHighlight(a.r.Settings().Bool("compare.diff.highlight")),
				diffview.WithFileChange(a.stepCompareDiff))
			if err != nil {
				a.r.UI().Error(err.Error())
				_ = a.r.Pop()
				return
			}
			p.diff = view
		} else {
			p.diff.SetFile(file)
		}
		p.diffPath = path
		a.selectDiffFile(path)
		view := a.m.ModalViews[diffFrame]
		view.Title = "Diff " + path + " · " + endpointDetail(data.Comparison.From) + " → " + endpointDetail(data.Comparison.To)
		a.m.ModalViews[diffFrame] = view
		_ = a.r.SetFocus(diffID)
	})
}

func (a *App) selectDiffFile(path string) {
	for _, row := range a.compare.files.Items() {
		if file, ok := row.Data.(Change); ok && file.Path == path {
			_ = a.compare.files.Select(row.ID)
			if a.compare.diffFiles != nil {
				_ = a.compare.diffFiles.Select("diff/" + row.ID)
			}
			return
		}
	}
}

// diffview emits diff.file before calling its host callback. File order comes
// from the same native change list; totals never become navigation targets.
func (a *App) stepCompareDiff(delta int) {
	p := a.compare
	if a.r.TopModal() != diffFrame {
		return
	}
	var files []Change
	at := 0
	for _, row := range p.files.Items() {
		if file, ok := row.Data.(Change); ok {
			if file.Path == p.diffPath {
				at = len(files)
			}
			files = append(files, file)
		}
	}
	if len(files) == 0 {
		return
	}
	next := min(max(0, at+delta), len(files)-1)
	if next == at {
		return
	}
	p.diffPath = files[next].Path
	a.selectDiffFile(p.diffPath)
	a.launch(diffID, "compare.file-open", a.loadCompareDiff(p.diffPath, false))
}

func (a *App) renderCompareDiff(r *ochazuke.Runtime, _ *ochazuke.Instance, w, h int) ochazuke.Block {
	p := a.compare
	s := a.m.Styles()
	if p.diff == nil {
		return ochazuke.Text("Loading diff…")
	}
	p.diff.Layout = diffview.Layout(r.Settings().String("compare.diff.layout"))
	p.diff.Highlight = r.Settings().Bool("compare.diff.highlight")
	l := ochazuke.Layout{Width: w, Height: h, Styles: s}
	files := func(w, h int) ochazuke.Block {
		cw, ch := ochazuke.PanelContentSize(w, h)
		return l.PanelBlock(a.compareFilesTitle(), ochazuke.Text(p.diffFiles.View(cw, ch)), w, h, p.diffFiles.Focused())
	}
	diff := func(w, h int) ochazuke.Block {
		cw, ch := ochazuke.PanelContentSize(w, h)
		return l.PanelBlock(p.diffPath, ochazuke.Text(p.diff.View(s, cw, ch)), w, h, r.Focus == diffID)
	}
	if w < 90 {
		fh := min(7, max(3, h/3))
		return ochazuke.Text(strings.Join([]string{files(w, fh).Text, diff(w, h-fh).Text}, "\n"))
	}
	lw := min(32, w/4)
	rw := w - lw - 1
	return ochazuke.Text(lipgloss.JoinHorizontal(lipgloss.Top, files(lw, h).Text, " ", diff(rw, h).Text))
}

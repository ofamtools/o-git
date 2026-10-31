package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"ofam.dev/ochazuke"
	"ofam.dev/ochazuke/diffview"
)

const comparePageID = "compare"
const pairsID = "compare/pairs"
const commitsID = "compare/commits"
const filesID = "compare/files"

// comparePage holds domain state; ochazuke owns focus, chooser and delivery.
type comparePage struct {
	pairs, commits, files                       *ochazuke.List
	repo, loadedRepo, following                 string
	custom                                      map[string]customPair
	shown                                       *Comparison
	loading, failure                            string
	fileCommit, commitReason, fileReason        string
	pairFiles                                   []ochazuke.ListItem
	pairFileReason                              string
	fileFollowing                               string
	chooserRepo, chooserOther, chooserFollowing string

	diff      *diffview.View
	diffFiles *ochazuke.List
	diffPath  string
}

type customPair struct {
	From string `json:"from"`
	To   string `json:"to"`
}
type comparePageData struct {
	Repo       string
	Pairs      []Comparison
	Custom     bool
	FileCounts map[string]int
}

var pairColumns = []ochazuke.Column{
	{Title: "pair", Min: 12, Max: 24}, {Title: "from", Min: 5, Priority: 3}, {Title: "to", Min: 5, Priority: 3},
	{Title: "↑", Min: 1, Align: ochazuke.AlignRight}, {Title: "↓", Min: 1, Align: ochazuke.AlignRight},
	{Title: "files", Min: 3}, {Title: "fetched", Min: 7, Max: 7, Priority: 4}, {Title: "state", Min: 12, Max: 32, Priority: 1},
}

func (a *App) registerComparePage() error {
	r := a.r
	p := &comparePage{custom: map[string]customPair{}}
	a.compare = p
	page, err := r.AddPage(comparePageID, "Compare", a.renderCompare, ochazuke.Kind("compare-page"))
	if err != nil {
		return err
	}
	for _, entry := range []struct {
		id   string
		dest **ochazuke.List
		cols []ochazuke.Column
	}{
		{pairsID, &p.pairs, pairColumns}, {commitsID, &p.commits, nil}, {filesID, &p.files, nil},
	} {
		list, err := r.AddList(entry.id, nil, ochazuke.Parent(comparePageID), ochazuke.Columns(entry.cols...), ochazuke.NoMarks())
		if err != nil {
			return err
		}
		*entry.dest = list
	}
	page.Focus, page.Remembered = pairsID, pairsID
	page.OnOpen = func(_ *ochazuke.Runtime, _ string) {
		if p.repo == "" {
			p.repo = a.current
		}
		a.launch(pairsID, "compare.reload", a.loadComparePairs(p.repo))
	}
	r.Define("compare").
		Verb("open", a.compareOpen, "open Compare, or enter the selected pair's commits").
		Verb("reload", func(c ochazuke.Ctx) ochazuke.Result { return a.loadComparePairs(a.compareRepo(c)) }, "resolve pairs again").Fetch().Quiet().
		Verb("set", a.compareSet, "set --side from|to to ENDPOINT; omitted endpoint opens a chooser").Positional("endpoint").
		Verb("swap", a.compareSwap, "swap from and to, remembering an ad hoc comparison").
		Verb("save", a.compareSave, "append the current comparison to compare.pairs as NAME").Positional("name")
	for _, noun := range []string{"repo", "worktree", "branch", "endpoint"} {
		r.Define(noun).Verb("compare", a.rowCompare, "open Compare with this row as the to side (or --side from)")
	}
	setHistory(r, ochazuke.HistoryAction, "compare.open", "compare.set", "compare.swap", "repo.compare", "worktree.compare", "branch.compare", "endpoint.compare")
	setHistory(r, ochazuke.HistoryChange, "compare.save")
	for _, key := range []string{"compare.open", "compare.reload", "compare.set", "compare.swap", "compare.save", "repo.compare", "worktree.compare", "branch.compare", "endpoint.compare"} {
		op := r.Operations[r.CanonicalKey(key)]
		op.Parameters = map[string]ochazuke.Parameter{"repo": {Type: "string", Description: "repository path or name"}}
		switch key {
		case "compare.set":
			op.Parameters["endpoint"] = ochazuke.Parameter{Type: "string", Description: "endpoint name, ref, SHA or spec"}
			fallthrough
		case "repo.compare", "worktree.compare", "branch.compare", "endpoint.compare":
			op.Parameters["side"] = ochazuke.Parameter{Type: "string", Enum: []string{"from", "to"}, Description: "side to replace (default to)"}
		case "compare.save":
			op.Parameters["name"] = ochazuke.Parameter{Type: "string", Required: true, Description: "saved pair name"}
		}
		r.Operations[r.CanonicalKey(key)] = op
	}
	// Reuse the core fetch verb and retain its non-quiet job and history kind.
	copyOp := r.Operations[r.CanonicalKey("compare.copy")]
	copyOp.App = func(c ochazuke.Ctx) ochazuke.Result { c.Args = a.compareArguments(c); return a.compareCopy(c) }
	r.Operations[r.CanonicalKey("compare.copy")] = copyOp
	op := r.Operations[r.CanonicalKey("compare.fetch")]
	op.App = func(c ochazuke.Ctx) ochazuke.Result {
		c.Args = a.compareArguments(c)
		return a.fetch(c, true).Then(func(_ ochazuke.Ctx, _ any, err error) {
			if err == nil && a.tui {
				a.launch(pairsID, "compare.reload", a.loadComparePairs(a.compare.repo))
			}
		})
	}
	r.Operations[r.CanonicalKey("compare.fetch")] = op
	repoFetch := r.Operations[r.CanonicalKey("repo.fetch")]
	repoFetch.App = func(c ochazuke.Ctx) ochazuke.Result {
		return a.fetch(c, false).Then(func(_ ochazuke.Ctx, _ any, err error) {
			if err == nil && a.tui {
				a.refresh()
			}
		})
	}
	r.Operations[r.CanonicalKey("repo.fetch")] = repoFetch
	if err := a.registerCompareViews(); err != nil {
		return err
	}
	return a.bindComparePage()
}

func (a *App) compareRepo(c ochazuke.Ctx) string {
	if word := ochazuke.StringArg(c.Args, "repo", ""); word != "" {
		return word
	}
	if a.compare.repo != "" {
		return a.compare.repo
	}
	return a.repoWord(c)
}

func (a *App) selectedComparison() (Comparison, bool) {
	row, ok := a.compare.pairs.Selected()
	if !ok {
		return Comparison{}, false
	}
	p, ok := row.Data.(Comparison)
	return p, ok
}

func (a *App) compareArguments(c ochazuke.Ctx) ochazuke.Arguments {
	args := ochazuke.Arguments{}
	for k, v := range c.Args {
		args[k] = v
	}
	if args["repo"] == nil {
		args["repo"] = a.compareRepo(c)
	}
	if args["pair"] == nil && args["from"] == nil && args["to"] == nil && a.r.Page == comparePageID {
		if p, ok := a.selectedComparison(); ok {
			args["from"], args["to"] = p.From.Spec, p.To.Spec
			args["remotes"] = slices.Clone(p.Remotes)
		}
	}
	return args
}

func readCustomPairs(dir string) (map[string]customPair, error) {
	out := map[string]customPair{}
	if dir == "" {
		return out, nil
	}
	data, err := os.ReadFile(filepath.Join(dir, "compare.json"))
	if errors.Is(err, os.ErrNotExist) {
		return out, nil
	}
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(data, &out); err != nil {
		return nil, fmt.Errorf("compare.json: %w", err)
	}
	return out, nil
}

func writeCustomPair(dir, repo string, pair customPair) error {
	if dir == "" {
		return nil
	}
	all, err := readCustomPairs(dir)
	if err != nil {
		return err
	}
	all[repo] = pair
	data, err := json.MarshalIndent(all, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	f, err := os.CreateTemp(dir, "compare-*.json")
	if err != nil {
		return err
	}
	name := f.Name()
	defer os.Remove(name)
	if _, err = f.Write(append(data, '\n')); err != nil {
		f.Close()
		return err
	}
	if err = f.Close(); err != nil {
		return err
	}
	return os.Rename(name, filepath.Join(dir, "compare.json"))
}

func pagePairs(r *EndpointResolver, custom *customPair) []Comparison {
	pairs := r.Pairs()
	if custom != nil {
		adhoc := r.Show("", custom.From, custom.To)
		for i := range adhoc {
			adhoc[i].Name = "(custom) " + resolvedRef(adhoc[i].From) + ".." + resolvedRef(adhoc[i].To)
		}
		pairs = append(adhoc, pairs...)
	}
	if r.cfg.Collapse {
		pairs = collapseComparisons(pairs, true)
	}
	return pairs
}

func (a *App) loadComparePairs(repo string) ochazuke.Result {
	cfg, now := a.compareConfig(), a.Now()
	copies := a.r.Settings().Bool("compare.files.find-copies-harder")
	memory, hasMemory := a.compare.custom[repo]
	a.compare.loading = "pairs"
	return ochazuke.Pending(func(ctx context.Context) (any, error) {
		_, dir, err := resolve(ctx, repo)
		if err != nil {
			return nil, err
		}
		resolver, err := NewEndpointResolver(ctx, dir, cfg, now, &a.compareCache)
		if err != nil {
			return nil, err
		}
		saved, err := readCustomPairs(cfg.StateDir)
		if err != nil {
			return nil, err
		}
		var custom *customPair
		if cfg.StateDir == "" && hasMemory {
			custom = &memory
		}
		if value, ok := saved[dir]; ok {
			custom = &value
		}
		return pageFileCounts(ctx, comparePageData{Repo: dir, Pairs: pagePairs(resolver, custom), Custom: custom != nil}, copies), nil
	}).ReplacingAt("app", "compare-pairs").Then(func(_ ochazuke.Ctx, v any, err error) {
		a.compare.loading = ""
		if err != nil {
			a.compare.failure = err.Error()
			return
		}
		a.applyComparePairs(v.(comparePageData), false)
	})
}

func (a *App) applyComparePairs(data comparePageData, selectCustom bool) {
	p := a.compare
	changed := p.loadedRepo != data.Repo
	p.repo, p.loadedRepo, p.failure, p.loading = data.Repo, data.Repo, "", ""
	if data.Custom && len(data.Pairs) > 0 {
		p.custom[data.Repo] = customPair{data.Pairs[0].From.Spec, data.Pairs[0].To.Spec}
	}
	rows, cols := pairItems(data.Pairs, data.FileCounts, a.Now())
	p.pairs.SetColumns(cols...)
	_ = p.pairs.SetItems(rows)
	if selectCustom && len(rows) > 0 {
		_ = p.pairs.Select(rows[0].ID)
	} else if changed {
		want := a.r.Settings().String("compare.default-pair")
		for _, row := range rows {
			if slices.Contains(row.Data.(Comparison).Names, want) {
				_ = p.pairs.Select(row.ID)
				break
			}
		}
	}
	p.following = ""
}

func (a *App) compareOpen(c ochazuke.Ctx) ochazuke.Result {
	if a.r.Page == comparePageID && ochazuke.StringArg(c.Args, "repo", "") == "" {
		if err := a.compare.commits.Focus(); err != nil {
			return ochazuke.Failed(err)
		}
		return ochazuke.Done(nil)
	}
	repo := a.repoWord(c)
	changed := a.compare.repo != repo
	a.compare.repo = repo
	if err := a.r.OpenPage(comparePageID); err != nil {
		return ochazuke.Failed(err)
	}
	if changed && a.compare.loadedRepo != "" {
		return a.loadComparePairs(repo)
	}
	return ochazuke.Done(nil)
}

// editComparison snapshots the UI selection before the job starts. CLI requests
// fall back to the remembered custom pair, then the configured default pair.
func (a *App) editComparison(c ochazuke.Ctx, edit func(customPair) customPair) ochazuke.Result {
	cfg, now, repo := a.compareConfig(), a.Now(), a.compareRepo(c)
	copies := a.r.Settings().Bool("compare.files.find-copies-harder")
	reset := a.r.Page != comparePageID
	var selected *customPair
	if pair, ok := a.selectedComparison(); ok && a.r.Page == comparePageID && (repo == pair.Repo || repo == "") {
		selected = &customPair{pair.From.Spec, pair.To.Spec}
	}
	return ochazuke.Pending(func(ctx context.Context) (any, error) {
		_, dir, err := resolve(ctx, repo)
		if err != nil {
			return nil, err
		}
		resolver, err := NewEndpointResolver(ctx, dir, cfg, now, &a.compareCache)
		if err != nil {
			return nil, err
		}
		saved, err := readCustomPairs(cfg.StateDir)
		if err != nil {
			return nil, err
		}
		current, ok := saved[dir]
		if reset {
			ok = false
		}
		if !ok {
			defaults := resolver.Show("", "", "")
			if len(defaults) == 0 {
				return nil, fmt.Errorf("no default comparison")
			}
			current = customPair{defaults[0].From.Spec, defaults[0].To.Spec}
		}
		if selected != nil {
			current = *selected
		}
		custom := edit(current)
		if err := writeCustomPair(cfg.StateDir, dir, custom); err != nil {
			return nil, err
		}
		return pageFileCounts(ctx, comparePageData{Repo: dir, Pairs: pagePairs(resolver, &custom), Custom: true}, copies), nil
	}).ReplacingAt("app", "compare-pairs").Then(func(_ ochazuke.Ctx, v any, err error) {
		if err == nil {
			a.applyComparePairs(v.(comparePageData), true)
		}
	})
}

func (a *App) compareSet(c ochazuke.Ctx) ochazuke.Result {
	side := ochazuke.StringArg(c.Args, "side", "to")
	if side != "from" && side != "to" {
		return ochazuke.Failed(fmt.Errorf("side must be from or to"))
	}
	endpoint := ochazuke.StringArg(c.Args, "endpoint", "")
	if endpoint == "" {
		return a.pickCompareSide(c, side)
	}
	return a.editComparison(c, func(pair customPair) customPair {
		if side == "from" {
			pair.From = endpoint
		} else {
			pair.To = endpoint
		}
		return pair
	})
}

func (a *App) compareSwap(c ochazuke.Ctx) ochazuke.Result {
	return a.editComparison(c, func(pair customPair) customPair { return customPair{pair.To, pair.From} })
}

func (a *App) compareSave(c ochazuke.Ctx) ochazuke.Result {
	name := strings.TrimSpace(ochazuke.StringArg(c.Args, "name", ""))
	if name == "" || strings.ContainsAny(name, "=\r\n") {
		return ochazuke.Failed(fmt.Errorf("expected a pair name without = or newline"))
	}
	pair, ok := a.selectedComparison()
	if !ok {
		return ochazuke.Failed(fmt.Errorf("no comparison selected"))
	}
	values := a.r.Settings().Strings("compare.pairs")
	for _, entry := range values {
		saved, _, _ := strings.Cut(entry, "=")
		if strings.TrimSpace(saved) == name {
			return ochazuke.Failed(fmt.Errorf("pair %s already exists", name))
		}
	}
	values = append(values, name+"="+pair.From.Spec+".."+pair.To.Spec)
	if err := a.r.Settings().Set("compare.pairs", values); err != nil {
		return ochazuke.Failed(err)
	}
	return a.loadComparePairs(pair.Repo)
}

func (a *App) rowCompare(c ochazuke.Ctx) ochazuke.Result {
	endpoint := "HEAD"
	var data any
	if c.Instance != nil {
		data = c.Instance.Data
	}
	switch row := data.(type) {
	case Repo:
		endpoint = "HEAD"
	case WorktreeRow:
		endpoint = "worktree:" + row.Path
	case BranchRow:
		endpoint = row.Name
	case Endpoint:
		endpoint = row.Spec
	}
	side := ochazuke.StringArg(c.Args, "side", "to")
	args := ochazuke.Arguments{"repo": a.repoWord(c), "side": side, "endpoint": endpoint}
	// Both ends of the relation use compare.set; opening follows delivery.
	res := a.compareSet(ochazuke.Ctx{Args: args})
	return res.Then(func(_ ochazuke.Ctx, v any, err error) {
		if err == nil {
			a.applyComparePairs(v.(comparePageData), true)
			_ = a.r.OpenPage(comparePageID)
		}
	})
}

func (a *App) renderCompare(r *ochazuke.Runtime, width, height int) ochazuke.Block {
	a.updateCompareAges()
	p := a.compare
	s := a.m.Styles()
	l := r.PaneLayout(comparePageID, ochazuke.Layout{Width: width, Height: height, Styles: s})
	status := "Compare " + tilde(p.repo)
	if p.loading != "" {
		status += " · reading " + p.loading + "…"
	}
	if p.failure != "" {
		status += " · " + p.failure
	}
	// The status is one row: a multi-line failure (git's stderr on Linux
	// adds "Stopping at filesystem boundary ...") would otherwise push the
	// title off it, leaving only its last line.
	status = strings.Join(strings.Fields(status), " ")
	bh := height - 1
	if width < 20 || bh < 8 {
		return ochazuke.Text(ansi.Truncate(status, width, "…"))
	}
	panel := func(title string, list *ochazuke.List, w, h int) ochazuke.Block {
		cw, ch := ochazuke.PanelContentSize(w, h)
		body := list.View(cw, ch)
		if list.Len() == 0 {
			reason := p.commitReason
			if list == p.files {
				reason = p.fileReason
			}
			if reason != "" {
				body = ansi.Truncate(reason, cw, "…")
			}
		}
		return l.PanelBlock(title, ochazuke.Text(body), w, h, list.Focused())
	}
	detail := func(w, h int) ochazuke.Block {
		cw, _ := ochazuke.PanelContentSize(w, h)
		return l.PanelBlock("Detail", ochazuke.Text(ansi.Wordwrap(a.compareDetail(), max(1, cw), "")), w, h, false)
	}
	if width < 90 {
		ph := l.Divide(min(max(6, p.pairs.Len()+3), max(6, bh-12)), bh, ochazuke.PaneMinHeight)
		ch, fh := max(2, (bh-ph)/3), max(2, (bh-ph)/3)
		return ochazuke.Text(s.Muted.Render(ansi.Truncate(status, width, "…")) + "\n" + strings.Join([]string{
			panel(fmt.Sprintf("Pairs %d", p.pairs.Len()), p.pairs, width, ph).Text,
			panel("Commits", p.commits, width, ch).Text,
			panel(a.compareFilesTitle(), p.files, width, fh).Text,
			detail(width, bh-ph-ch-fh).Text,
		}, "\n"))
	}
	lw := l.Divide(max(32, width*r.Settings().Int("compare.pairs.width-percent")/100), width-1, ochazuke.PaneMinWidth)
	rw := width - lw - 1
	ch, fh := max(4, bh/3), max(4, bh/3)
	left := panel(fmt.Sprintf("Pairs %d", p.pairs.Len()), p.pairs, lw, bh).Text
	right := strings.Join([]string{panel("Commits", p.commits, rw, ch).Text, panel(a.compareFilesTitle(), p.files, rw, fh).Text, detail(rw, bh-ch-fh).Text}, "\n")
	return ochazuke.Text(s.Muted.Render(ansi.Truncate(status, width, "…")) + "\n" + lipgloss.JoinHorizontal(lipgloss.Top, left, " ", right))
}

package main

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/x/ansi"
	"github.com/unixsuperhero/ochazuke"
)

var compareFileColumns = []ochazuke.Column{
	{Title: "XY", Width: 2, Priority: 1}, {Title: "path", Min: 8},
	{Title: "added", Priority: 2}, {Title: "removed", Priority: 3},
}

func comparisonFileKey(p Comparison) string { return endpointKey(p.From) + "\x01" + endpointKey(p.To) }

// Summary counts and focused lists share the merged change reader. The page
// job owns these reads; neither rendering nor selection motion runs git.
func pageFileCounts(ctx context.Context, data comparePageData, copies bool) comparePageData {
	data.FileCounts = map[string]int{}
	counts := make([]int, len(data.Pairs))
	errs := make([]error, len(data.Pairs))
	parallel(len(data.Pairs), func(i int) {
		if !data.Pairs[i].Available {
			return
		}
		list, err := ReadChanges(ctx, data.Repo, data.Pairs[i], copies)
		counts[i], errs[i] = list.Totals.Files, err
	})
	for i, pair := range data.Pairs {
		if !pair.Available {
			continue
		}
		if errs[i] != nil {
			data.Pairs[i].State = "files unavailable"
			data.Pairs[i].Detail = errs[i].Error()
			continue
		}
		data.FileCounts[comparisonFileKey(pair)] = counts[i]
	}
	return data
}

func pairItems(pairs []Comparison, counts map[string]int, now time.Time) ([]ochazuke.ListItem, []ochazuke.Column) {
	cols := append([]ochazuke.Column(nil), pairColumns...)
	allUnfetched, allHead := true, true
	rows := make([]ochazuke.ListItem, 0, len(pairs))
	for _, pair := range pairs {
		row := comparisonRow(pair, now)
		label := pair.Name
		if len(pair.Names) > 1 {
			name := pair.Names[0]
			if !pair.Available {
				name = pair.Reason
			}
			suffix := fmt.Sprintf(" +%d", len(pair.Names)-1)
			label = ansi.Truncate(name, max(1, cols[0].Min-ansi.StringWidth(suffix)), "…") + suffix
		}
		files := "-"
		if count, ok := counts[comparisonFileKey(pair)]; ok {
			files = strconv.Itoa(count)
		}
		cells := []string{label, row.Cells[1], row.Cells[2], row.Cells[3], row.Cells[4], files, row.Cells[5], row.Cells[6]}
		allUnfetched = allUnfetched && cells[6] == "-"
		allHead = allHead && cells[2] == "HEAD"
		item := ochazuke.ListItem{ID: row.ID, Label: label, Noun: row.Noun, Data: pair, Cells: cells}
		if !pair.Available {
			for i := range cells {
				item.Tones = append(item.Tones, ochazuke.ToneSpan{Cell: i, Tone: ochazuke.ToneTreeGuide})
			}
		}
		if pair.Stale {
			item.Tones = append(item.Tones, ochazuke.ToneSpan{Cell: 6, Tone: ochazuke.ToneNeedsInput})
		}
		rows = append(rows, item)
	}
	if allUnfetched {
		cols[6].Priority = 99
	}
	if allHead {
		cols[2].Priority = 98
	}
	return rows, cols
}

type compareDetailData struct {
	Pair    Comparison
	Commits CommitList
	Changes ChangeList
}

func (a *App) followCompare() bool {
	p := a.compare
	if a.r.TopModal() == "compare-side" {
		return a.followCompareChooser()
	}
	if a.r.Page != comparePageID || a.r.TopModal() != "" {
		return false
	}
	row, ok := p.pairs.Selected()
	if !ok {
		return false
	}
	if row.ID == p.following {
		return a.followCompareFiles()
	}
	p.following = row.ID
	pair := row.Data.(Comparison)
	p.shown = nil
	p.fileCommit = ""
	p.fileFollowing = ""
	p.commitReason = "Reading commits…"
	p.fileReason = "Reading files…"
	_ = p.commits.SetItems(nil)
	_ = p.files.SetItems(nil)
	s := a.r.Settings()
	limit, split, copies := s.Int("compare.commits.limit"), s.Bool("compare.commits.split"), s.Bool("compare.files.find-copies-harder")
	cols := commitColumns(split)
	for i := range cols {
		switch cols[i].Title {
		case "subject":
			cols[i].Min = 8
		case "sha":
			cols[i].Min = 7
		default:
			cols[i].Priority = i + 1
		}
	}
	p.commits.SetColumns(cols...)
	p.files.SetColumns(compareFileColumns...)
	c := ochazuke.Ctx{Args: ochazuke.Arguments{"repo": pair.Repo}}
	res := a.comparisonRead(c, func(r *EndpointResolver) (any, error) {
		resolved := r.Show("", pair.From.Spec, pair.To.Spec)
		if len(resolved) == 0 {
			return nil, fmt.Errorf("no comparison")
		}
		fresh := resolved[0]
		fresh.Name, fresh.Names, fresh.Members = pair.Name, pair.Names, pair.Members
		fresh.Remotes, fresh.Fetched, fresh.Stale = pair.Remotes, pair.Fetched, pair.Stale
		commits, err := ReadCommits(r.ctx, r.dir, fresh, limit, split, r.now)
		if err != nil {
			return nil, err
		}
		changes, err := ReadChanges(r.ctx, r.dir, fresh, copies)
		if err != nil {
			return nil, err
		}
		return compareDetailData{fresh, commits, changes}, nil
	}).Replacing("pair-detail").Then(func(_ ochazuke.Ctx, v any, err error) {
		if err != nil {
			p.failure = err.Error()
			return
		}
		data := v.(compareDetailData)
		p.shown = &data.Pair
		p.commitReason = data.Commits.Reason
		p.fileReason = data.Changes.Reason
		p.pairFileReason = data.Changes.Reason
		p.pairFiles = nil
		_ = p.commits.SetItems(data.Commits.Items)
		if data.Pair.Available {
			p.pairFiles = data.Changes.Items(data.Pair.Repo)
			_ = p.files.SetItems(p.pairFiles)
		}
	})
	return a.launch(pairsID, "compare.show", res)
}

func endpointDetail(e Endpoint) string {
	if !e.Available {
		return e.Spec + ": " + e.Reason
	}
	ref := resolvedRef(e)
	if ref == "" {
		ref = e.Spec
	}
	if e.Spec != "" && e.Spec != ref {
		return ref + " (" + e.Spec + ")"
	}
	return ref
}

func (a *App) compareDetail() string {
	var detail string
	if inst := a.r.Instances[a.r.BaseFocus()]; inst != nil {
		switch row := inst.Data.(type) {
		case Commit:
			detail = fmt.Sprintf("%s %s\n%s\n%s · %s\n%s", row.ShortSHA, row.Decorations, row.Subject, row.Author, row.Age, counted(row.ParentCount, "parent"))
		case Change:
			detail = fmt.Sprintf("%s %s\n+%d -%d", row.Status, row.Path, row.Added, row.Removed)
			if row.OldPath != "" {
				detail += "\nfrom " + row.OldPath
			}
		case ChangeTotals:
			detail = fmt.Sprintf("%s, +%d -%d", counted(row.Files, "file"), row.Added, row.Removed)
		}
	}
	if detail != "" {
		return detail
	}
	p := a.compare.shown
	if p == nil {
		return "Reading selected comparison…"
	}
	row := comparisonRow(*p, a.Now())
	text := fmt.Sprintf("%s\nfrom  %s\nto    %s\n↑%s ↓%s\n%s", strings.Join(p.Names, " · "), endpointDetail(p.From), endpointDetail(p.To), row.Cells[3], row.Cells[4], p.State)
	if p.Fetched != nil {
		text += "\nas of the last fetch, " + fetchedText(p.Fetched, false, a.Now()) + " ago"
	}
	return text
}

func counted(n int, noun string) string {
	if n != 1 {
		noun += "s"
	}
	return fmt.Sprintf("%d %s", n, noun)
}

// Render ages from the timestamps, without re-reading git or recreating rows.
func (a *App) updateCompareAges() {
	rows := a.compare.pairs.Items()
	changed := false
	for i, row := range rows {
		p := row.Data.(Comparison)
		if len(row.Cells) > 6 {
			text := fetchedText(p.Fetched, false, a.Now())
			changed = changed || row.Cells[6] != text
			rows[i].Cells[6] = text
		}
	}
	if changed {
		_ = a.compare.pairs.SetItems(rows)
	}
}

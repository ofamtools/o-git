package main

import (
	"context"
	"fmt"
	"strings"

	"ofam.dev/ochazuke"
)

const commitSearchChunk = 25

// Each continuation is an immutable snapshot. Only a chooser's load job reads
// git; opening the final row hands the next block to that same load pipeline.
type commitSearch struct {
	dir, head, query       string
	depth, skip, remaining int
	rows                   []ochazuke.ChooserRow
	run                    gitRunner
	preview                *ochazuke.ChooserRow
}

func (s commitSearch) load() ochazuke.Result {
	if s.remaining == 0 {
		s.remaining = s.depth
	}
	return ochazuke.Pending(func(ctx context.Context) (any, error) {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if s.head == "" {
			return s.finish(false), nil
		}
		count := min(commitSearchChunk, s.remaining)
		out, err := s.run(ctx, readTimeout, s.dir, "log", fmt.Sprintf("--max-count=%d", count), fmt.Sprintf("--skip=%d", s.skip), "--format=%H%x00%h%x00%s%x00%an%x00%as", s.head, "--")
		if err != nil {
			return nil, err
		}
		var rows []ochazuke.ChooserRow
		scanned := 0
		for _, line := range strings.Split(strings.TrimSuffix(string(out), "\n"), "\n") {
			parts := strings.SplitN(line, "\x00", 5)
			if len(parts) != 5 {
				continue
			}
			scanned++
			// SHA queries are prefixes (including uppercase hex), subject
			// queries are literal case-insensitive substrings, never Git args.
			q := strings.ToLower(strings.TrimSpace(s.query))
			if !(len(q) >= 4 && strings.HasPrefix(parts[0], q)) && !strings.Contains(strings.ToLower(parts[2]), q) {
				continue
			}
			rows = append(rows, ochazuke.ChooserRow{ID: "commit-search/" + parts[0], Label: strings.Join(parts, " "), Cells: parts[1:], Data: sideChoice{Spec: parts[0], Graph: strings.Join(parts[1:], " ")}})
		}
		s.skip += scanned
		s.remaining -= scanned
		s.rows = append(append([]ochazuke.ChooserRow(nil), s.rows...), rows...)
		if scanned < count {
			return append(rows, s.finish(false)...), nil
		}
		if s.remaining == 0 {
			return append(rows, s.finish(true)...), nil
		}
		return ochazuke.ChooserBatch{Rows: rows, More: s.load()}, nil
	})
}

func (s commitSearch) finish(more bool) []ochazuke.ChooserRow {
	var rows []ochazuke.ChooserRow
	if s.preview != nil {
		rows = append(rows, *s.preview)
	}
	if more {
		s.remaining = 0
		rows = append(rows, ochazuke.ChooserRow{ID: "commit-search/more", Label: fmt.Sprintf("Search %d more commits…", s.depth), Opens: true, NoPick: true, Data: sideChoice{Search: &s}})
	}
	return rows
}

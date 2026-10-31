package main

// Raw/numstat metadata and the streaming hunk parser are adapted from tuios's
// internal/review/{diff,parse,unified}.go (MIT; see LICENSE-tuios). Unlike its
// Build, this reader never snapshots a tree, writes an index or creates objects.

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// These wire types mirror ochazuke v0.68 diffview/model.go and types.go without
// importing a package unavailable in o-git's v0.66 dependency. Kind values are
// Context=0, Add=1, Delete=2. Added/Removed are the model's actual count fields.
type DiffFile struct {
	OldPath   string     `json:"oldPath,omitempty"`
	NewPath   string     `json:"newPath,omitempty"`
	Status    string     `json:"status,omitempty"`
	Binary    bool       `json:"binary,omitempty"`
	OldSize   int64      `json:"oldSize,omitempty"`
	NewSize   int64      `json:"newSize,omitempty"`
	Truncated bool       `json:"truncated,omitempty"`
	Added     int        `json:"added,omitempty"`
	Removed   int        `json:"removed,omitempty"`
	Hunks     []DiffHunk `json:"hunks,omitempty"`
}

type DiffHunk struct {
	OldStart int        `json:"oldStart"`
	OldCount int        `json:"oldCount"`
	NewStart int        `json:"newStart"`
	NewCount int        `json:"newCount"`
	Header   string     `json:"header,omitempty"`
	Lines    []DiffLine `json:"lines"`
}

type DiffLine struct {
	Kind      uint8  `json:"kind"`
	Text      string `json:"text"`
	Old       int    `json:"old,omitempty"`
	New       int    `json:"new,omitempty"`
	NoNewline bool   `json:"noNewline,omitempty"`
}

type DiffResult struct {
	Comparison Comparison `json:"comparison"`
	Reversed   bool       `json:"reversed"`
	Files      []DiffFile `json:"files"`
	Unified    string     `json:"-"`
}

type DiffOptions struct {
	Path              string // repository-relative, literal; empty means every file
	Context, MaxLines int
	Plain             bool // collect exact patch text; structured limits still apply
}

type diffEntry struct {
	DiffFile
	oldID, newID string
}

func diffFlags() []string {
	return []string{"-c", "core.quotePath=false", "-c", "diff.noprefix=false", "diff", "--no-color", "--no-ext-diff", "--no-textconv", "--no-relative", "-M"}
}

// diffShape normalises mutable-left comparisons to a git-supported direction.
// The patch and metadata both use -R, so counts, status, paths and lines reverse.
func diffShape(c Comparison) (args []string, reversed, untracked bool) {
	f, t := c.From, c.To
	if isMutable(f) && (!isMutable(t) || f.Kind == "worktree" && t.Kind == "index") {
		f, t, reversed = t, f, true
	}
	if reversed {
		args = append(args, "-R")
	}
	switch {
	case f.Kind == "index" && t.Kind == "worktree":
		// git diff without revisions compares index to worktree.
		untracked = true
	case t.Kind == "index":
		args = append(args, "--cached", f.SHA)
	case t.Kind == "worktree":
		args = append(args, f.SHA)
		untracked = true
	default:
		args = append(args, f.SHA, t.SHA)
	}
	return
}

// BuildDiff reads a resolved pair. Unavailable and equal pairs remain visible
// as an empty result with the resolver's reason; no substitute base is chosen.
func BuildDiff(ctx context.Context, c Comparison, opt DiffOptions) (DiffResult, error) {
	d := DiffResult{Comparison: c, Reversed: c.Reversed, Files: []DiffFile{}}
	if opt.Context < 0 || opt.MaxLines < 1 {
		return d, fmt.Errorf("diff context must be nonnegative and max-lines positive")
	}
	if opt.Path != "" && (filepath.IsAbs(opt.Path) || filepath.Clean(opt.Path) == ".." || strings.HasPrefix(filepath.Clean(opt.Path), ".."+string(filepath.Separator)) || strings.ContainsRune(opt.Path, 0)) {
		return d, fmt.Errorf("diff path must be relative to the repository")
	}
	if opt.Path != "" {
		opt.Path = filepath.Clean(opt.Path)
	}
	if !c.Available || endpointKey(c.From) == endpointKey(c.To) {
		return d, nil
	}
	shape, reversed, untracked := diffShape(c)
	d.Reversed = reversed
	var patch strings.Builder
	read := func(shape []string, noIndex bool, selectedPath string) error {
		metaArgs := append(diffFlags(), "--raw", "--numstat", "--abbrev=64", "-z")
		metaArgs = append(metaArgs, shape...)
		var metadata bytes.Buffer
		if err := gitStream(ctx, c.Repo, noIndex, func(r io.Reader) error { _, err := io.Copy(&metadata, r); return err }, metaArgs...); err != nil {
			return err
		}
		entries, err := parseDiffMetadata(metadata.String())
		if err != nil {
			return err
		}
		if len(entries) == 0 {
			return nil
		}
		if noIndex {
			for i := range entries {
				if reversed {
					entries[i].OldPath, entries[i].NewPath = selectedPath, ""
				} else {
					entries[i].OldPath, entries[i].NewPath = "", selectedPath
				}
			}
		}
		args := append(diffFlags(), fmt.Sprintf("-U%d", opt.Context))
		args = append(args, shape...)
		selected := make([]bool, len(entries))
		for i, e := range entries {
			selected[i] = opt.Path == "" || opt.Path == e.OldPath || opt.Path == e.NewPath
		}
		if err := gitStream(ctx, c.Repo, noIndex, func(r io.Reader) error {
			return readDiffUnified(r, entries, selected, opt, &patch)
		}, args...); err != nil {
			return err
		}
		for i := range entries {
			if !selected[i] {
				continue
			}
			e := &entries[i]
			if e.Binary {
				e.OldSize = diffSideSize(ctx, c.Repo, c.From, e.OldPath, e.oldID)
				e.NewSize = diffSideSize(ctx, c.Repo, c.To, e.NewPath, e.newID)
			}
			d.Files = append(d.Files, e.DiffFile)
		}
		return nil
	}
	if err := read(append(shape, "--"), false, ""); err != nil {
		return d, err
	}
	if untracked {
		out, err := git(ctx, readTimeout, c.Repo, "ls-files", "--others", "--exclude-standard", "-z")
		if err != nil {
			return d, err
		}
		for _, path := range strings.Split(string(out), "\x00") {
			if path == "" || opt.Path != "" && path != opt.Path {
				continue
			}
			args := []string{"--no-index"}
			if reversed {
				args = append(args, "-R")
			}
			if err := read(append(args, "--", "/dev/null", path), true, path); err != nil {
				return d, err
			}
		}
	}
	d.Unified = patch.String()
	return d, nil
}

func diffSideSize(ctx context.Context, dir string, side Endpoint, path, id string) int64 {
	if path == "" {
		return 0
	}
	if side.Kind == "worktree" {
		info, err := os.Lstat(filepath.Join(dir, path))
		if err == nil {
			return info.Size()
		}
		return -1
	}
	if strings.Trim(id, "0") == "" {
		if side.Kind == "index" {
			id = ":" + path
		} else {
			id = side.SHA + ":" + path
		}
	}
	out, err := git(ctx, readTimeout, dir, "cat-file", "-s", id)
	if err != nil {
		return -1
	}
	n, err := strconv.ParseInt(strings.TrimSpace(string(out)), 10, 64)
	if err != nil {
		return -1
	}
	return n
}

// Metadata is NUL-delimited, including rename paths; patch header quoting is
// deliberately irrelevant. Numstat and raw records share git's file order.
func parseDiffMetadata(out string) ([]diffEntry, error) {
	tokens := strings.Split(out, "\x00")
	files := []diffEntry{}
	counted := 0
	for i := 0; i < len(tokens); i++ {
		tok := strings.TrimLeft(tokens[i], "\n")
		if tok == "" {
			continue
		}
		if strings.HasPrefix(tok, ":") {
			f := strings.Fields(tok)
			if len(f) != 5 || len(f[4]) == 0 || i+1 >= len(tokens) {
				return nil, fmt.Errorf("invalid raw diff record %q", tok)
			}
			e := diffEntry{DiffFile: DiffFile{Status: f[4][:1], OldSize: -1, NewSize: -1}, oldID: f[2], newID: f[3]}
			i++
			e.OldPath, e.NewPath = tokens[i], tokens[i]
			if e.Status == "R" || e.Status == "C" {
				i++
				if i >= len(tokens) {
					return nil, fmt.Errorf("missing rename destination")
				}
				e.NewPath = tokens[i]
			}
			if strings.TrimPrefix(f[0], ":") == "000000" {
				e.OldPath, e.OldSize = "", 0
			}
			if f[1] == "000000" {
				e.NewPath, e.NewSize = "", 0
			}
			files = append(files, e)
			continue
		}
		f := strings.SplitN(tok, "\t", 3)
		if len(f) != 3 || counted >= len(files) {
			return nil, fmt.Errorf("invalid numstat record %q", tok)
		}
		if f[2] == "" {
			i += 2 // numstat rename: counts TAB TAB NUL old NUL new NUL
			if i >= len(tokens) {
				return nil, fmt.Errorf("missing numstat rename paths")
			}
		}
		e := &files[counted]
		counted++
		e.Binary = f[0] == "-" && f[1] == "-"
		if !e.Binary {
			var err error
			if e.Added, err = strconv.Atoi(f[0]); err != nil {
				return nil, err
			}
			if e.Removed, err = strconv.Atoi(f[1]); err != nil {
				return nil, err
			}
		}
	}
	if counted != len(files) {
		return nil, fmt.Errorf("diff metadata has %d files but %d counts", len(files), counted)
	}
	return files, nil
}

func diffRange(s string) (int, int, error) {
	a, b, count := strings.Cut(s, ",")
	start, err := strconv.Atoi(a)
	if err != nil || start < 0 {
		return 0, 0, fmt.Errorf("bad hunk range %q", s)
	}
	n := 1
	if count {
		n, err = strconv.Atoi(b)
		if err != nil || n < 0 {
			return 0, 0, fmt.Errorf("bad hunk range %q", s)
		}
	}
	return start, n, nil
}

func diffHunkHeader(text string) (DiffHunk, error) {
	var h DiffHunk
	rest, ok := strings.CutPrefix(text, "@@ -")
	ranges, header, end := strings.Cut(rest, " @@")
	a, b, sides := strings.Cut(ranges, " +")
	if !ok || !end || !sides {
		return h, fmt.Errorf("invalid hunk header %q", text)
	}
	var err error
	if h.OldStart, h.OldCount, err = diffRange(a); err != nil {
		return h, err
	}
	if h.NewStart, h.NewCount, err = diffRange(b); err != nil {
		return h, err
	}
	h.Header = strings.TrimPrefix(header, " ")
	h.Lines = []DiffLine{}
	return h, nil
}

// readDiffLine drains arbitrarily long source lines using a fixed-size reader.
// Structured results retain at most maxDiffBytes from one line; plain output
// keeps the full line because its contract is byte-for-byte git output.
func readDiffLine(r *bufio.Reader, plain bool) (string, int, error) {
	var b strings.Builder
	n := 0
	for {
		part, err := r.ReadSlice('\n')
		n += len(part)
		if plain || b.Len()+len(part) <= maxDiffBytes {
			b.Write(part)
		}
		if err != bufio.ErrBufferFull {
			return b.String(), n, err
		}
	}
}

func readDiffUnified(r io.Reader, files []diffEntry, selected []bool, opt DiffOptions, patch *strings.Builder) error {
	br := bufio.NewReaderSize(r, 64<<10)
	idx, old, newLine, oldLeft, newLeft, lines, size := -1, 0, 0, 0, 0, 0, 0
	header := ""
	for {
		raw, n, err := readDiffLine(br, opt.Plain)
		if n > 0 {
			text := strings.TrimSuffix(raw, "\n") // preserve CRs in source text
			if oldLeft == 0 && newLeft == 0 && strings.HasPrefix(text, "diff --git ") {
				// Git can render a type change as deletion then addition, two
				// blocks with the exact same header but one raw metadata row.
				// Compare the opaque header; never decode its quoted paths.
				if idx < 0 || files[idx].Status != "T" || text != header {
					idx++
					lines, size = 0, 0
				}
				header = text
				if idx >= len(files) {
					return fmt.Errorf("more patch files than metadata")
				}
			}
			if idx >= 0 {
				f := &files[idx]
				if opt.Plain && selected[idx] {
					patch.WriteString(raw)
				}
				size += n
				if size > maxDiffBytes {
					f.Truncated, f.Hunks = true, nil
				}
				keep := selected[idx] && !f.Truncated && !f.Binary
				switch {
				case strings.HasPrefix(text, "\\ No newline at end of file"):
					if keep && len(f.Hunks) > 0 {
						h := &f.Hunks[len(f.Hunks)-1]
						if len(h.Lines) > 0 {
							h.Lines[len(h.Lines)-1].NoNewline = true
						}
					}
				case oldLeft > 0 || newLeft > 0:
					lines++
					ln := DiffLine{}
					if len(text) > 0 {
						ln.Text = text[1:]
					}
					switch {
					case strings.HasPrefix(text, "-"):
						ln.Kind, ln.Old = 2, old
						old++
						oldLeft--
					case strings.HasPrefix(text, "+"):
						ln.Kind, ln.New = 1, newLine
						newLine++
						newLeft--
					default:
						ln.Old, ln.New = old, newLine
						old++
						newLine++
						oldLeft--
						newLeft--
					}
					if lines > opt.MaxLines {
						f.Truncated, f.Hunks = true, nil
					} else if keep {
						h := &f.Hunks[len(f.Hunks)-1]
						h.Lines = append(h.Lines, ln)
					}
				case strings.HasPrefix(text, "@@ "):
					h, e := diffHunkHeader(text)
					if e != nil {
						return e
					}
					old, newLine, oldLeft, newLeft = h.OldStart, h.NewStart, h.OldCount, h.NewCount
					if keep {
						f.Hunks = append(f.Hunks, h)
					}
				}
			}
		}
		if err != nil {
			if err == io.EOF {
				if oldLeft != 0 || newLeft != 0 || idx+1 != len(files) {
					return fmt.Errorf("incomplete unified diff")
				}
				return nil
			}
			return err
		}
	}
}

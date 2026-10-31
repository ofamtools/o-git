package main

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/unixsuperhero/ochazuke"
)

// Change keeps paths unquoted and separate, including a rename/copy source.
// Index and Worktree carry the individual numstats for an XY comparison.
type Change struct {
	Repo      string      `json:"repo"`
	Path      string      `json:"path"`
	OldPath   string      `json:"old_path,omitempty"`
	Status    string      `json:"status"`
	Added     int         `json:"added"`
	Removed   int         `json:"removed"`
	Binary    bool        `json:"binary"`
	Untracked bool        `json:"untracked,omitempty"`
	Index     *ChangeStat `json:"index,omitempty"`
	Worktree  *ChangeStat `json:"worktree,omitempty"`
}

type ChangeStat struct {
	Added   int  `json:"added"`
	Removed int  `json:"removed"`
	Binary  bool `json:"binary"`
}

type ChangeTotals struct {
	Files   int `json:"files"`
	Added   int `json:"added"`
	Removed int `json:"removed"`
	Binary  int `json:"binary"`
}

type ChangeList struct {
	Files    []Change     `json:"files"`
	Totals   ChangeTotals `json:"totals"`
	Reason   string       `json:"reason,omitempty"`
	Detail   string       `json:"detail,omitempty"`
	Reversed bool         `json:"reversed"`
}

// parseRawNumstat adapts tuios internal/review.ParseRawNumstat (MIT;
// LICENSE-tuios). Unlike that renderer, retain C and T, and reject malformed
// counts rather than silently converting them to zero. The NUL path handling
// also follows hiiro's CommitPlanning.name_status.
func parseRawNumstat(out []byte, dir string) ([]Change, error) {
	tokens := strings.Split(string(out), "\x00")
	files := []Change{}
	byPath := map[string]int{}
	for i := 0; i < len(tokens); i++ {
		tok := strings.TrimLeft(tokens[i], "\n")
		if tok == "" {
			continue
		}
		if strings.HasPrefix(tok, ":") {
			fields := strings.Fields(tok)
			if len(fields) != 5 || fields[4] == "" || i+1 >= len(tokens) || tokens[i+1] == "" {
				return nil, fmt.Errorf("unexpected raw diff record %q", tok)
			}
			f := Change{Repo: dir, Status: fields[4][:1], Path: tokens[i+1]}
			i++
			if f.Status == "R" || f.Status == "C" {
				if i+1 >= len(tokens) || tokens[i+1] == "" {
					return nil, fmt.Errorf("missing rename/copy destination")
				}
				f.OldPath, f.Path = f.Path, tokens[i+1]
				i++
			}
			byPath[f.Path] = len(files)
			files = append(files, f)
			continue
		}
		parts := strings.SplitN(tok, "\t", 3)
		if len(parts) != 3 {
			return nil, fmt.Errorf("unexpected numstat record %q", tok)
		}
		path := parts[2]
		if path == "" {
			if i+2 >= len(tokens) {
				return nil, fmt.Errorf("missing numstat rename/copy paths")
			}
			path = tokens[i+2]
			i += 2
		}
		idx, ok := byPath[path]
		if !ok {
			return nil, fmt.Errorf("numstat without raw path %q", path)
		}
		f := &files[idx]
		if parts[0] == "-" && parts[1] == "-" {
			f.Binary = true
			continue
		}
		var err error
		if f.Added, err = strconv.Atoi(parts[0]); err != nil || f.Added < 0 {
			return nil, fmt.Errorf("invalid added count %q", parts[0])
		}
		if f.Removed, err = strconv.Atoi(parts[1]); err != nil || f.Removed < 0 {
			return nil, fmt.Errorf("invalid removed count %q", parts[1])
		}
	}
	return files, nil
}

// Every diff read uses these flags through the existing timed git runner.
func readListDiff(ctx context.Context, dir string, args ...string) ([]Change, error) {
	flags := []string{"-c", "core.quotePath=false", "diff", "--no-color", "--no-ext-diff", "--no-textconv", "--no-relative", "--raw", "--numstat", "-z", "-M"}
	flags = append(flags, args...)
	flags = append(flags, "--")
	out, err := git(ctx, readTimeout, dir, flags...)
	if err != nil {
		return nil, err
	}
	return parseRawNumstat(out, dir)
}

func commitChangeArgs(findCopiesHarder bool, args ...string) []string {
	flags := []string{"-C"}
	if findCopiesHarder {
		flags = append(flags, "--find-copies-harder")
	}
	return append(flags, args...)
}

func readCommitChanges(ctx context.Context, dir string, findCopiesHarder bool, args ...string) ([]Change, error) {
	return readListDiff(ctx, dir, commitChangeArgs(findCopiesHarder, args...)...)
}

// parseChangeStatus extends parseStatus's NUL-record approach with the paths
// and exact XY letters. Porcelain v1 -z puts the rename destination first.
func parseChangeStatus(out []byte, dir string) ([]Change, error) {
	tokens := strings.Split(string(out), "\x00")
	files := []Change{}
	for i := 0; i < len(tokens); i++ {
		rec := tokens[i]
		if rec == "" {
			continue
		}
		if len(rec) < 4 || rec[2] != ' ' {
			return nil, fmt.Errorf("git status: short path record %q", rec)
		}
		f := Change{Repo: dir, Status: rec[:2], Path: rec[3:], Untracked: rec[:2] == "??"}
		if strings.ContainsAny(f.Status, "RC") {
			if i+1 >= len(tokens) || tokens[i+1] == "" {
				return nil, fmt.Errorf("git status: missing rename/copy source")
			}
			f.OldPath = tokens[i+1]
			i++
		}
		files = append(files, f)
	}
	return files, nil
}

func readChangeStatus(ctx context.Context, dir string) ([]Change, error) {
	out, err := git(ctx, readTimeout, dir, "-c", "core.quotePath=false", "status", "--porcelain=v1", "-z")
	if err != nil {
		return nil, err
	}
	return parseChangeStatus(out, dir)
}

func addChangeStat(f *Change, part Change, index bool) {
	s := &ChangeStat{Added: part.Added, Removed: part.Removed, Binary: part.Binary}
	if index {
		f.Index = s
	} else {
		f.Worktree = s
	}
	f.Added += part.Added
	f.Removed += part.Removed
	f.Binary = f.Binary || part.Binary
}

func xyChanges(staged, unstaged []Change) []Change {
	byPath := map[string]*Change{}
	for layer, files := range [][]Change{staged, unstaged} {
		for _, part := range files {
			f := byPath[part.Path]
			if f == nil {
				f = &Change{Repo: part.Repo, Path: part.Path, Status: "  "}
				byPath[part.Path] = f
			}
			status := []byte(f.Status)
			status[layer] = part.Status[0]
			f.Status = string(status)
			if part.OldPath != "" {
				f.OldPath = part.OldPath
			}
			addChangeStat(f, part, layer == 0)
		}
	}
	paths := make([]string, 0, len(byPath))
	for path := range byPath {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	files := make([]Change, 0, len(paths))
	for _, path := range paths {
		files = append(files, *byPath[path])
	}
	return files
}

// ReadChanges uses status itself as the HEAD..WORKTREE row authority, so its
// letters, rename paths, conflict states and ordering cannot drift from git.
// Counts on XY rows are the sum of the two layers, also retained separately.
func ReadChanges(ctx context.Context, dir string, pair Comparison, findCopiesHarder bool) (ChangeList, error) {
	result := ChangeList{Files: []Change{}, Reversed: pair.Reversed}
	if !pair.Available {
		result.Reason = pair.Reason
		result.Detail = pair.Detail
		return result, nil
	}
	f, t := pair.From, pair.To
	if endpointKey(f) == endpointKey(t) {
		result.Reason = "same commit: nothing differs"
		return result, nil
	}
	var err error
	switch {
	case !isMutable(f) && !isMutable(t):
		result.Files, err = readCommitChanges(ctx, dir, findCopiesHarder, f.SHA, t.SHA)
	case isMutable(f) && !isMutable(t):
		if f.Path != "" {
			dir = f.Path
		}
		args := []string{"-R", t.SHA}
		if f.Kind == "index" {
			args = append([]string{"--cached"}, args...)
		}
		result.Files, err = readListDiff(ctx, dir, args...)
		result.Reversed = true
		result.Reason = "reversed: the working tree is on the left"
		if err == nil && f.Kind == "worktree" {
			var status []Change
			status, err = readChangeStatus(ctx, dir)
			result.Files = appendReverseUntracked(result.Files, status, "D")
		}
	default:
		if t.Path != "" {
			dir = t.Path
		}
		var staged, unstaged []Change
		if !isMutable(f) {
			staged, err = readListDiff(ctx, dir, "--cached", f.SHA)
		} else if f.Kind == "worktree" {
			unstaged, err = readListDiff(ctx, dir, "-R")
			result.Reversed = true
			result.Reason = "reversed: the working tree is on the left"
		}
		if err == nil && t.Kind == "worktree" {
			unstaged, err = readListDiff(ctx, dir)
		}
		if err != nil {
			break
		}
		result.Files = xyChanges(staged, unstaged)
		if f.Kind == "worktree" && t.Kind == "index" {
			var status []Change
			status, err = readChangeStatus(ctx, dir)
			result.Files = appendReverseUntracked(result.Files, status, " D")
		}
		if t.Kind == "worktree" {
			var status []Change
			status, err = readChangeStatus(ctx, dir)
			if err != nil {
				break
			}
			if !isMutable(f) && (f.SHA == t.Head || f.Kind == "empty") {
				stats := map[string]Change{}
				for _, file := range result.Files {
					stats[file.Path] = file
				}
				for i := range status {
					counts := stats[status[i].Path]
					status[i].Added, status[i].Removed, status[i].Binary = counts.Added, counts.Removed, counts.Binary
					status[i].Index, status[i].Worktree = counts.Index, counts.Worktree
				}
				result.Files = status
			} else {
				byPath := map[string]int{}
				for i, file := range result.Files {
					byPath[file.Path] = i
				}
				for _, file := range status {
					if file.Status == "??" {
						if i, exists := byPath[file.Path]; exists {
							// A path deleted from an older base can have an
							// untracked working copy: retain both layers in
							// one path row rather than duplicating its id.
							result.Files[i].Status = result.Files[i].Status[:1] + "?"
							result.Files[i].Untracked = true
						} else {
							result.Files = append(result.Files, file)
						}
					}
				}
			}
		}
	}
	if err != nil {
		return result, err
	}
	result.count()
	return result, nil
}

func appendReverseUntracked(files, status []Change, letter string) []Change {
	seen := map[string]bool{}
	for _, file := range files {
		seen[file.Path] = true
	}
	for _, file := range status {
		if file.Status == "??" && !seen[file.Path] {
			file.Status = letter
			files = append(files, file)
		}
	}
	return files
}

func (l *ChangeList) count() {
	l.Totals = ChangeTotals{Files: len(l.Files)}
	for _, file := range l.Files {
		l.Totals.Added += file.Added
		l.Totals.Removed += file.Removed
		if file.Binary {
			l.Totals.Binary++
		}
	}
}

func changeItem(f Change) ochazuke.ListItem {
	path := f.Path
	if f.OldPath != "" {
		path = f.OldPath + " → " + path
	}
	added, removed := "+"+strconv.Itoa(f.Added), "-"+strconv.Itoa(f.Removed)
	if f.Binary {
		added, removed = "bin", "bin"
	} else if f.Untracked {
		added, removed = "", ""
	}
	return ochazuke.ListItem{ID: "change/" + f.Repo + ":" + f.Path, Noun: "change", Label: f.Status + " " + path, Data: f, Cells: []string{f.Status, path, added, removed}}
}

func (l ChangeList) Items(dir string) []ochazuke.ListItem {
	rows := make([]ochazuke.ListItem, 0, len(l.Files)+1)
	for _, file := range l.Files {
		rows = append(rows, changeItem(file))
	}
	t := l.Totals
	rows = append(rows, ochazuke.ListItem{ID: "change-total/" + dir, Noun: "change", Label: fmt.Sprintf("%s, +%d -%d", counted(t.Files, "file"), t.Added, t.Removed), Data: t, Cells: []string{"total", counted(t.Files, "file"), "+" + strconv.Itoa(t.Added), "-" + strconv.Itoa(t.Removed)}})
	return rows
}

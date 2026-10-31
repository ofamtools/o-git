package main

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/unixsuperhero/ochazuke"
)

// Commit retains full object ids in data while the row uses git's short id.
type Commit struct {
	Repo        string   `json:"repo"`
	SHA         string   `json:"sha"`
	ShortSHA    string   `json:"short_sha"`
	Decorations string   `json:"decorations"`
	Subject     string   `json:"subject"`
	Author      string   `json:"author"`
	Date        int64    `json:"date"`
	Age         string   `json:"age"`
	Parents     []string `json:"parents"`
	ParentCount int      `json:"parent_count"`
	Side        string   `json:"side,omitempty"`
	Message     string   `json:"message,omitempty"`
	More        int      `json:"more,omitempty"`
}

type CommitList struct {
	Items  []ochazuke.ListItem `json:"items"`
	Reason string              `json:"reason,omitempty"`
}

// NUL separates fields; git commit messages cannot contain NULs. A fixed
// field count avoids treating newlines or control characters as row breaks.
const commitFields = "%m%x00%H%x00%h%x00%D%x00%s%x00%an%x00%at%x00%P%x00"
const commitFormat = commitFields + "%B%x00"

func parseCommits(out []byte, dir string, now time.Time) ([]Commit, error) {
	fields := strings.Split(string(out), "\x00")
	commits := []Commit{}
	for i := 0; i+9 < len(fields); i += 10 {
		date, err := strconv.ParseInt(fields[i+6], 10, 64)
		if err != nil {
			return nil, fmt.Errorf("git log: invalid author date: %w", err)
		}
		parents := strings.Fields(fields[i+7])
		if parents == nil {
			parents = []string{}
		}
		side := "ahead"
		if strings.TrimSpace(fields[i]) == "<" {
			side = "behind"
		}
		commits = append(commits, Commit{Repo: dir, SHA: fields[i+1], ShortSHA: fields[i+2], Decorations: fields[i+3], Subject: fields[i+4], Author: fields[i+5], Date: date, Age: age(now, date), Parents: parents, ParentCount: len(parents), Side: side, Message: fields[i+8]})
	}
	return commits, nil
}

func commitItem(c Commit, split bool) ochazuke.ListItem {
	mark, section := ">", "Ahead"
	if c.Side == "behind" {
		mark, section = "<", "Behind"
	}
	if split {
		mark = ""
	} else {
		section = ""
	}
	cells := []string{mark, c.ShortSHA, c.Decorations, c.Subject, c.Author, c.Age, strconv.Itoa(c.ParentCount)}
	id, label := "commit/"+c.Repo+":"+c.SHA, c.ShortSHA+" "+c.Subject
	if c.More > 0 {
		id, label = "commit-more/"+c.Repo+":"+c.Side, fmt.Sprintf("%d more", c.More)
		cells = []string{mark, "", "", label, "", "", ""}
	}
	if split {
		cells = cells[1:]
	}
	return ochazuke.ListItem{ID: id, Noun: "commit", Label: label, Data: c, Cells: cells, Section: section}
}

// ReadCommits never adds an ordering flag. Split output is a stable partition
// into Ahead/Behind; neither section changes git's order. Unsplit output
// retains the complete interleaved order from git.
func ReadCommits(ctx context.Context, dir string, pair Comparison, limit int, split bool, now time.Time) (CommitList, error) {
	result := CommitList{Items: []ochazuke.ListItem{}}
	if limit < 0 {
		return result, fmt.Errorf("commit limit must not be negative")
	}
	if !pair.Available {
		result.Reason = pair.Reason
		return result, nil
	}
	if isMutable(pair.From) || isMutable(pair.To) {
		result.Reason = "the working tree is not a commit"
		return result, nil
	}
	if endpointKey(pair.From) == endpointKey(pair.To) {
		result.Reason = "same commit: nothing differs"
		return result, nil
	}
	args := []string{"log", "--no-color", "--no-show-signature", "--decorate=short", "--left-right", "-z", "--format=" + commitFields + "%x00"}
	switch {
	case pair.From.Kind == "empty":
		args = append(args, pair.To.SHA)
	case pair.To.Kind == "empty":
		args = append(args, pair.From.SHA)
	default:
		args = append(args, pair.From.SHA+"..."+pair.To.SHA)
	}
	args = append(args, "--")
	out, err := git(ctx, readTimeout, dir, args...)
	if err != nil {
		return result, err
	}
	commits, err := parseCommits(out, dir, now)
	if err != nil {
		return result, err
	}
	seen, more := map[string]int{}, map[string]int{}
	for _, c := range commits {
		if pair.To.Kind == "empty" {
			c.Side = "behind"
		}
		if seen[c.Side] >= limit {
			more[c.Side]++
			continue
		}
		seen[c.Side]++
		result.Items = append(result.Items, commitItem(c, split))
	}
	for _, side := range []string{"ahead", "behind"} {
		if more[side] > 0 {
			result.Items = append(result.Items, commitItem(Commit{Repo: dir, Side: side, More: more[side]}, split))
		}
	}
	if split {
		grouped := make([]ochazuke.ListItem, 0, len(result.Items))
		for _, section := range []string{"Ahead", "Behind"} {
			for _, item := range result.Items {
				if item.Section == section {
					grouped = append(grouped, item)
				}
			}
		}
		result.Items = grouped
	}
	return result, nil
}

func ReadCommit(ctx context.Context, dir, sha string, now time.Time) (Commit, error) {
	if err := ValidRef(sha); err != nil {
		return Commit{}, err
	}
	out, err := git(ctx, readTimeout, dir, "log", "-1", "--no-color", "--no-show-signature", "--decorate=short", "-z", "--format="+commitFormat, sha+"^{commit}", "--")
	if err != nil {
		return Commit{}, err
	}
	commits, err := parseCommits(out, dir, now)
	if err != nil {
		return Commit{}, err
	}
	if len(commits) != 1 {
		return Commit{}, fmt.Errorf("no commit %q", sha)
	}
	c := commits[0]
	c.Side = ""
	return c, nil
}

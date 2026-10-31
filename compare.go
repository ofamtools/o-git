package main

import (
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"
)

// Comparison is a summary only. Commit, file and diff data load separately.
// Names and Members preserve every saved pair after visual collapse.
type Comparison struct {
	Repo          string       `json:"repo"`
	Name          string       `json:"name"`
	Source        string       `json:"source"`
	Names         []string     `json:"names"`
	Members       []PairMember `json:"members"`
	From          Endpoint     `json:"from"`
	To            Endpoint     `json:"to"`
	Ahead         int          `json:"ahead"`
	Behind        int          `json:"behind"`
	Available     bool         `json:"available"`
	Reason        string       `json:"reason"`
	State         string       `json:"state"`
	CommitsReason string       `json:"commits_reason,omitempty"`
	Reversed      bool         `json:"reversed"`
	Remotes       []string     `json:"remotes,omitempty"`
	Fetched       *time.Time   `json:"fetched,omitempty"`
	Stale         bool         `json:"stale"`
}

type PairMember struct {
	Name   string `json:"name"`
	Source string `json:"source"`
	From   string `json:"from"`
	To     string `json:"to"`
}

func isMutable(e Endpoint) bool { return e.Kind == "worktree" || e.Kind == "index" }

func endpointKey(e Endpoint) string {
	if isMutable(e) {
		return e.Kind + "\x00" + e.Path
	}
	return e.KindKey() + "\x00" + e.SHA
}

// Relative kinds still denote commits; an unborn HEAD denotes an empty tree.
func (e Endpoint) KindKey() string {
	if e.Kind == "merge-base" || e.Kind == "fork-point" {
		return "commit"
	}
	return e.Kind
}

func unionRemotes(a, b []string) []string {
	out := slices.Clone(a)
	for _, remote := range b {
		if !slices.Contains(out, remote) {
			out = append(out, remote)
		}
	}
	return out
}

func makeComparison(dir, name, from, to string, f, t Endpoint) Comparison {
	c := Comparison{Repo: dir, Name: name, Source: name, Names: []string{name}, Members: []PairMember{{Name: name, Source: name, From: from, To: to}}, From: f, To: t, Available: f.Available && t.Available, Remotes: unionRemotes(f.Remotes, t.Remotes)}
	for _, e := range []Endpoint{f, t} {
		if !e.Available {
			if c.Reason != "" {
				c.Reason += "; "
			}
			c.Reason += e.Spec + ": " + e.Reason
		}
		if e.Fetched != nil && (c.Fetched == nil || e.Fetched.Before(*c.Fetched)) {
			c.Fetched = e.Fetched
		}
		c.Stale = c.Stale || e.Stale
	}
	if !c.Available {
		c.State = c.Reason
		return c
	}
	c.State = "ready"
	if isMutable(f) || isMutable(t) {
		c.CommitsReason = "the working tree is not a commit"
		c.State = c.CommitsReason
		if isMutable(f) && !isMutable(t) {
			c.Reversed = true
			c.State = "reversed: the working tree is on the left"
		}
	}
	if endpointKey(f) == endpointKey(t) {
		c.State = "same commit: nothing differs"
	}
	if f.Kind == "empty" || t.Kind == "empty" {
		c.State = "unborn HEAD: empty tree"
	}
	for _, e := range []Endpoint{f, t} {
		if e.Note != "" && !strings.Contains(c.State, e.Note) {
			c.State += "; " + e.Note
		}
	}
	return c
}

func splitPair(value string) (from, to string) {
	// A revision can contain '..' inside a relative spec; only a top-level
	// delimiter separates the pair. Refs themselves cannot contain '..'.
	depth := 0
	for i := 0; i+1 < len(value); i++ {
		switch value[i] {
		case '(':
			depth++
		case ')':
			depth--
		}
		if depth == 0 && value[i:i+2] == ".." {
			return strings.TrimSpace(value[:i]), defaultTo(strings.TrimSpace(value[i+2:]))
		}
	}
	return strings.TrimSpace(value), "HEAD"
}

func defaultTo(s string) string {
	if s == "" {
		return "HEAD"
	}
	return s
}

func (r *EndpointResolver) pairRows(name, from, to string) []Comparison {
	f, fe := r.expandAlias(from, map[string]bool{})
	t, te := r.expandAlias(to, map[string]bool{})
	remotes := []string{""}
	expand := strings.Contains(name+f+t, "{remote}")
	if expand {
		remotes = r.remotes
		if len(remotes) == 0 {
			left, right := r.ResolvePair(from, to)
			if strings.Contains(f, "{remote}") {
				left = r.unavailable(from, "", "no remotes")
			}
			if strings.Contains(t, "{remote}") {
				right = r.unavailable(to, "", "no remotes")
			}
			if !strings.Contains(f+t, "{remote}") {
				left = r.unavailable(from, "", "no remotes")
			}
			return []Comparison{makeComparison(r.dir, name, from, to, left, right)}
		}
	}
	var out []Comparison
	for _, remote := range remotes {
		n := strings.ReplaceAll(name, "{remote}", remote)
		if expand && !strings.Contains(name, "{remote}") && len(remotes) > 1 {
			n += " (" + remote + ")"
		}
		fs, ts := strings.ReplaceAll(f, "{remote}", remote), strings.ReplaceAll(t, "{remote}", remote)
		inputFrom, inputTo := strings.ReplaceAll(from, "{remote}", remote), strings.ReplaceAll(to, "{remote}", remote)
		left, right := r.ResolvePair(fs, ts)
		if fe != nil {
			left = r.unavailable(from, "", fe.Error())
		}
		if te != nil {
			right = r.unavailable(to, "", te.Error())
		}
		// An expanded row depends on its remote even if an absent branch or
		// detached HEAD prevented its remote-tracking ref from resolving.
		if strings.Contains(f, "{remote}") {
			left.Remotes = unionRemotes(left.Remotes, []string{remote})
		}
		if strings.Contains(t, "{remote}") {
			right.Remotes = unionRemotes(right.Remotes, []string{remote})
		}
		left.Spec, right.Spec = inputFrom, inputTo
		c := makeComparison(r.dir, n, inputFrom, inputTo, left, right)
		c.Source, c.Members[0].Source = name, name
		out = append(out, c)
	}
	return out
}

// Pairs retains malformed saved records as unavailable rows too. Collapse
// happens before expensive graph walks, and only for available endpoints.
func (r *EndpointResolver) Pairs() []Comparison {
	var out []Comparison
	for _, entry := range r.cfg.Pairs {
		name, value, ok := strings.Cut(entry, "=")
		name = strings.TrimSpace(name)
		if !ok || name == "" {
			u := r.unavailable(entry, "", "expected NAME=FROM..TO")
			out = append(out, makeComparison(r.dir, entry, entry, "HEAD", u, r.resolve("HEAD", nil, 0)))
			continue
		}
		from, to := splitPair(value)
		out = append(out, r.pairRows(name, from, to)...)
	}
	if r.cfg.Collapse {
		var collapsed []Comparison
		keys := map[string]int{}
		for _, c := range out {
			key := endpointKey(c.From) + "\x01" + endpointKey(c.To)
			if index, exists := keys[key]; exists && c.Available {
				old := &collapsed[index]
				for _, name := range c.Names {
					if !slices.Contains(old.Names, name) {
						old.Names = append(old.Names, name)
					}
				}
				old.Name = strings.Join(old.Names, " · ")
				old.Members = append(old.Members, c.Members...)
				old.Remotes = unionRemotes(old.Remotes, c.Remotes)
				old.Stale = old.Stale || c.Stale
				if c.Fetched != nil && (old.Fetched == nil || c.Fetched.Before(*old.Fetched)) {
					old.Fetched = c.Fetched
				}
				continue
			}
			if c.Available {
				keys[key] = len(collapsed)
			}
			collapsed = append(collapsed, c)
		}
		out = collapsed
	}
	r.countPairs(out)
	return out
}

func (r *EndpointResolver) Show(pair, from, to string) []Comparison {
	if pair == "" && from == "" && to == "" {
		pair = r.cfg.DefaultPair
	}
	if pair != "" {
		// A saved name wins over an expanded template with the same label.
		for _, entry := range r.cfg.Pairs {
			name, value, ok := strings.Cut(entry, "=")
			if ok && strings.TrimSpace(name) == pair {
				f, t := splitPair(value)
				out := r.pairRows(pair, f, t)
				r.countPairs(out)
				return out
			}
		}
		var out []Comparison
		for _, entry := range r.cfg.Pairs {
			name, value, ok := strings.Cut(entry, "=")
			if !ok {
				continue
			}
			f, t := splitPair(value)
			for _, c := range r.pairRows(strings.TrimSpace(name), f, t) {
				if c.Name == pair {
					out = append(out, c)
				}
			}
		}
		if len(out) == 0 {
			u := r.unavailable(pair, "", "no saved pair named "+pair)
			out = []Comparison{makeComparison(r.dir, pair, pair, "HEAD", u, r.resolve("HEAD", nil, 0))}
		}
		r.countPairs(out)
		return out
	}
	if from == "" {
		for _, entry := range r.cfg.Pairs {
			name, value, ok := strings.Cut(entry, "=")
			if ok && strings.TrimSpace(name) == r.cfg.DefaultPair {
				from, _ = splitPair(value)
				break
			}
		}
		if from == "" {
			from = "HEAD"
		}
	}
	out := r.pairRows("(custom)", from, defaultTo(to))
	r.countPairs(out)
	return out
}

func (r *EndpointResolver) countKey(c Comparison) string {
	return r.dir + "\x00" + r.shallow + "\x00" + c.From.SHA + "\x00" + c.To.SHA
}

func (r *EndpointResolver) countPairs(pairs []Comparison) {
	groups := map[string][]int{}
	var keys []string
	for i, c := range pairs {
		if !c.Available || isMutable(c.From) || isMutable(c.To) || endpointKey(c.From) == endpointKey(c.To) {
			continue
		}
		key := r.countKey(c)
		if _, ok := groups[key]; !ok {
			keys = append(keys, key)
		}
		groups[key] = append(groups[key], i)
	}
	parallel(len(keys), func(i int) {
		key := keys[i]
		counts, err := r.count(pairs[groups[key][0]], key)
		for _, j := range groups[key] {
			if err != nil {
				pairs[j].Available, pairs[j].Reason, pairs[j].State = false, err.Error(), err.Error()
			} else {
				pairs[j].Ahead, pairs[j].Behind = counts.Ahead, counts.Behind
			}
		}
	})
}

func (r *EndpointResolver) count(c Comparison, key string) (divergence, error) {
	if r.cache != nil {
		r.cache.mu.Lock()
		cached, ok := r.cache.counts[key]
		r.cache.mu.Unlock()
		if ok {
			return cached, nil
		}
	}
	args := []string{"rev-list", "--left-right", "--count", c.From.SHA + "..." + c.To.SHA, "--"}
	if c.From.Kind == "empty" {
		args = []string{"rev-list", "--count", c.To.SHA, "--"}
	}
	if c.To.Kind == "empty" {
		args = []string{"rev-list", "--count", c.From.SHA, "--"}
	}
	out, err := r.run(r.ctx, readTimeout, r.dir, args...)
	if err != nil {
		return divergence{}, err
	}
	f := strings.Fields(string(out))
	var d divergence
	if c.From.Kind == "empty" || c.To.Kind == "empty" {
		if len(f) != 1 {
			return d, fmt.Errorf("invalid rev-list count: %q", out)
		}
		n, err := strconv.Atoi(f[0])
		if err != nil {
			return d, err
		}
		if c.From.Kind == "empty" {
			d.Ahead = n
		} else {
			d.Behind = n
		}
	} else {
		if len(f) != 2 {
			return d, fmt.Errorf("invalid rev-list counts: %q", out)
		}
		if d.Behind, err = strconv.Atoi(f[0]); err != nil {
			return d, err
		}
		if d.Ahead, err = strconv.Atoi(f[1]); err != nil {
			return d, err
		}
	}
	if r.cache != nil {
		r.cache.mu.Lock()
		if len(r.cache.counts) > 1024 {
			r.cache.counts = nil
		}
		if r.cache.counts == nil {
			r.cache.counts = map[string]divergence{}
		}
		r.cache.counts[key] = d
		r.cache.mu.Unlock()
	}
	return d, nil
}

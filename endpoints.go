package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Endpoint is a resolved spec. Non-commit sides carry their worktree's HEAD
// separately so relative specs can use it without pretending they are commits.
type Endpoint struct {
	Spec      string `json:"spec"`
	Kind      string `json:"kind"`
	Ref       string `json:"ref"`
	SHA       string `json:"sha"`
	Available bool   `json:"available"`
	Reason    string `json:"reason"`
	Name      string `json:"name,omitempty"`
	Source    string `json:"source,omitempty"` // original setting name, before remote expansion
	Repo      string `json:"repo,omitempty"`
	Head      string `json:"head,omitempty"`
	Remote    string `json:"remote,omitempty"`
	// Remotes also includes dependencies of relative specs, even when unavailable.
	Remotes  []string   `json:"remotes,omitempty"`
	Fetched  *time.Time `json:"fetched,omitempty"`
	Stale    bool       `json:"stale"`
	Note     string     `json:"note,omitempty"`
	Path     string     `json:"path,omitempty"`
	relative bool
}

type gitRunner func(context.Context, time.Duration, string, ...string) ([]byte, error)

type compareRef struct{ sha, objectType, peeled, peeledType, symref, upstream string }
type refRequest struct {
	ref    string
	verify bool
}

// A refresh takes one refs and worktrees snapshot. Named refs and aliases are
// resolved from it; arbitrary revision expressions are verified once per refresh.
type EndpointResolver struct {
	ctx context.Context
	dir string
	cfg CompareConfig
	now time.Time
	run gitRunner

	refs                          map[string]compareRef
	worktrees                     []Worktree
	remotes                       []string
	head, branch                  string
	commonDir, gitDir, fetchHead  string
	shallow                       string
	mainName, mainRef, mainReason string
	emptySHA                      string

	aliases  map[string]string
	verified map[refRequest]Endpoint
	ages     map[string]Endpoint
	cache    *CompareCache
}

func NewEndpointResolver(ctx context.Context, dir string, cfg CompareConfig, now time.Time, cache *CompareCache) (*EndpointResolver, error) {
	return newEndpointResolver(ctx, dir, cfg, now, cache, git)
}

func newEndpointResolver(ctx context.Context, dir string, cfg CompareConfig, now time.Time, cache *CompareCache, run gitRunner) (*EndpointResolver, error) {
	r := &EndpointResolver{ctx: ctx, dir: dir, cfg: cfg, now: now, run: run, refs: map[string]compareRef{}, aliases: map[string]string{}, verified: map[refRequest]Endpoint{}, ages: map[string]Endpoint{}, cache: cache}
	args := [][]string{
		{"for-each-ref", "--format=%(refname)%00%(objectname)%00%(objecttype)%00%(*objectname)%00%(*objecttype)%00%(symref)%00%(upstream)"},
		{"worktree", "list", "--porcelain", "-z"}, {"remote"},
		{"rev-parse", "--verify", "HEAD^{commit}"}, {"symbolic-ref", "--quiet", "HEAD"},
		{"rev-parse", "--path-format=absolute", "--git-common-dir", "--absolute-git-dir"},
		{"rev-parse", "--path-format=absolute", "--git-path", "FETCH_HEAD"},
	}
	outs := make([][]byte, len(args))
	errs := make([]error, len(args))
	parallel(len(args), func(i int) { outs[i], errs[i] = run(ctx, readTimeout, dir, args[i]...) })
	for _, i := range []int{0, 1, 2, 5, 6} {
		if errs[i] != nil {
			return nil, errs[i]
		}
	}
	for _, line := range strings.Split(strings.TrimSpace(string(outs[0])), "\n") {
		f := strings.Split(line, "\x00")
		if len(f) == 7 {
			r.refs[f[0]] = compareRef{f[1], f[2], f[3], f[4], f[5], f[6]}
		}
	}
	r.worktrees = parseWorktrees(outs[1])
	r.remotes = strings.Fields(string(outs[2]))
	r.head = strings.TrimSpace(string(outs[3]))
	r.branch = strings.TrimPrefix(strings.TrimSpace(string(outs[4])), "refs/heads/")
	if errs[3] != nil && (r.branch == "" || r.refs["refs/heads/"+r.branch].sha != "") {
		return nil, errs[3]
	}
	dirs := strings.Split(strings.TrimSpace(string(outs[5])), "\n")
	if len(dirs) != 2 {
		return nil, fmt.Errorf("invalid git directories: %q", outs[5])
	}
	r.commonDir, r.gitDir, r.fetchHead = dirs[0], dirs[1], strings.TrimSpace(string(outs[6]))
	if b, err := os.ReadFile(filepath.Join(r.commonDir, "shallow")); err == nil {
		r.shallow = string(b)
	}
	for _, entry := range cfg.Endpoints {
		name, spec, ok := strings.Cut(entry, "=")
		if ok && strings.TrimSpace(name) != "" {
			r.aliases[strings.TrimSpace(name)] = strings.TrimSpace(spec)
		}
	}
	r.detectMain()
	return r, nil
}

// ValidRef is adapted from tuios internal/review/diff.go (MIT, Gaurav
// Gosain); see LICENSE-tuios. Git alone decides which remaining expressions
// denote commits. In particular no named ref silently becomes a merge base.
func ValidRef(ref string) error {
	if ref == "" {
		return errors.New("empty ref")
	}
	if strings.HasPrefix(ref, "-") {
		return fmt.Errorf("%q starts with a dash", ref)
	}
	if len(ref) > 256 {
		return errors.New("ref is longer than 256 bytes")
	}
	for _, c := range ref {
		if c < 0x20 || c == 0x7f || c == ' ' {
			return fmt.Errorf("%q holds a space or a control character", ref)
		}
	}
	return nil
}

func (r *EndpointResolver) unavailable(spec, kind, reason string) Endpoint {
	return Endpoint{Spec: spec, Kind: kind, Repo: r.dir, Reason: reason}
}

func (r *EndpointResolver) detectMain() {
	choose := func(name, ref string) { r.mainName, r.mainRef = name, ref }
	if r.cfg.Main != "" {
		ref := r.cfg.Main
		name := strings.TrimPrefix(ref, "refs/heads/")
		for _, remote := range r.remotes {
			short := strings.TrimPrefix(ref, "refs/remotes/")
			if strings.HasPrefix(short, remote+"/") {
				name = strings.TrimPrefix(short, remote+"/")
				break
			}
		}
		choose(name, ref)
		return
	}
	remoteHeads := []string{"origin"}
	if len(r.remotes) == 1 && r.remotes[0] != "origin" {
		remoteHeads = append(remoteHeads, r.remotes[0])
	}
	for _, remote := range remoteHeads {
		target := r.refs["refs/remotes/"+remote+"/HEAD"].symref
		if target == "" {
			continue
		}
		name := strings.TrimPrefix(target, "refs/remotes/"+remote+"/")
		ref := target
		if r.refs["refs/heads/"+name].objectType == "commit" {
			ref = "refs/heads/" + name
		}
		choose(name, ref)
		return
	}
	for _, name := range r.cfg.MainNames {
		if r.refs["refs/heads/"+name].objectType == "commit" {
			choose(name, "refs/heads/"+name)
			return
		}
	}
	for _, name := range r.cfg.MainNames {
		for _, remote := range r.remotes {
			ref := "refs/remotes/" + remote + "/" + name
			if r.refs[ref].objectType == "commit" {
				choose(name, ref)
				return
			}
		}
	}
	r.mainReason = "no main branch (set compare.main or compare.main-names)"
}

// expandAlias keeps cycles visible rather than falling through to a different
// ref. Literal refs that clash with endpoint names remain reachable as refs/… .
func (r *EndpointResolver) expandAlias(spec string, seen map[string]bool) (string, error) {
	for _, c := range spec {
		if c < 0x20 || c == 0x7f {
			return spec, errors.New("endpoint holds a control character")
		}
	}
	spec = strings.TrimSpace(spec)
	if next, ok := r.aliases[spec]; ok {
		if seen[spec] {
			return spec, fmt.Errorf("endpoint alias cycle at %s", spec)
		}
		seen[spec] = true
		defer delete(seen, spec)
		if next == spec {
			return spec, nil
		}
		return r.expandAlias(next, seen)
	}
	parts, err := alternatives(spec)
	if err != nil {
		return spec, err
	}
	if len(parts) > 1 {
		for i, part := range parts {
			parts[i], err = r.expandAlias(part, seen)
			if err != nil {
				parts[i] = part
			}
		}
		return strings.Join(parts, " | "), nil
	}
	for _, kind := range []string{"merge-base", "fork-point"} {
		if strings.HasPrefix(spec, kind+"(") && strings.HasSuffix(spec, ")") {
			inner, err := r.expandAlias(spec[len(kind)+1:len(spec)-1], seen)
			return kind + "(" + inner + ")", err
		}
	}
	return spec, nil
}

// alternatives splits only outside parentheses, so a relative spec can have
// its own ordered policy (hiiro BranchComparison's candidate lists, as data).
func alternatives(spec string) ([]string, error) {
	depth, start := 0, 0
	var out []string
	for i, c := range spec {
		switch c {
		case '(':
			depth++
		case ')':
			depth--
			if depth < 0 {
				return nil, errors.New("unbalanced endpoint parentheses")
			}
		case '|':
			if depth == 0 {
				out = append(out, strings.TrimSpace(spec[start:i]))
				start = i + 1
			}
		}
	}
	if depth != 0 {
		return nil, errors.New("unbalanced endpoint parentheses")
	}
	return append(out, strings.TrimSpace(spec[start:])), nil
}

// Resolve expands {remote} to one endpoint per remote. With none, one row
// retains the spec and the reason. other defaults to HEAD at CLI call sites.
func (r *EndpointResolver) Resolve(spec, other string) []Endpoint {
	expanded, err := r.expandAlias(spec, map[string]bool{})
	if err != nil {
		return []Endpoint{r.unavailable(spec, "", err.Error())}
	}
	otherSpec, err := r.expandAlias(other, map[string]bool{})
	if err != nil {
		return []Endpoint{r.unavailable(spec, "", "other side: "+err.Error())}
	}
	remotes := []string{""}
	if strings.Contains(expanded+otherSpec, "{remote}") {
		remotes = r.remotes
		if len(remotes) == 0 {
			return []Endpoint{r.unavailable(spec, "", "no remotes")}
		}
	}
	var out []Endpoint
	for _, remote := range remotes {
		from, _ := r.ResolvePair(strings.ReplaceAll(expanded, "{remote}", remote), strings.ReplaceAll(otherSpec, "{remote}", remote))
		from.Spec = strings.ReplaceAll(spec, "{remote}", remote)
		if remote != "" {
			from.Remotes = unionRemotes(from.Remotes, []string{remote})
		}
		out = append(out, from)
	}
	return out
}

func (r *EndpointResolver) ResolvePair(from, to string) (Endpoint, Endpoint) {
	f := r.resolve(from, nil, 0)
	t := r.resolve(to, nil, 0)
	if f.relative && t.relative {
		f.Reason, t.Reason = "both sides are relative to each other", "both sides are relative to each other"
	} else {
		// Re-evaluate even an available fallback: an earlier relative
		// alternative may now resolve with the known opposite endpoint.
		if t.Available || f.relative {
			f = r.resolve(from, &t, 0)
		}
		if f.Available || t.relative {
			t = r.resolve(to, &f, 0)
		}
		if (f.Kind == "merge-base" || f.Kind == "fork-point") && (t.Kind == "merge-base" || t.Kind == "fork-point") {
			f.Available, t.Available = false, false
			f.Reason, t.Reason = "both sides are relative to each other", "both sides are relative to each other"
		}
	}
	return f, t
}

func (r *EndpointResolver) resolve(original string, other *Endpoint, depth int) Endpoint {
	if depth > 32 {
		return r.unavailable(original, "", "endpoint nesting exceeds 32")
	}
	spec, err := r.expandAlias(original, map[string]bool{})
	if err != nil {
		return r.unavailable(original, "", err.Error())
	}
	parts, err := alternatives(spec)
	if err != nil {
		return r.unavailable(original, "", err.Error())
	}
	if len(parts) > 1 {
		var reasons []string
		var pending Endpoint
		for _, part := range parts {
			e := r.resolve(part, other, depth+1)
			e.Spec = original
			if e.Available {
				return e
			}
			if e.relative {
				pending = e
			}
			reasons = append(reasons, e.Reason)
		}
		if pending.relative {
			return pending
		}
		return r.unavailable(original, "", strings.Join(reasons, "; "))
	}
	for _, kind := range []string{"merge-base", "fork-point"} {
		if strings.HasPrefix(spec, kind+"(") && strings.HasSuffix(spec, ")") {
			base := r.resolve(spec[len(kind)+1:len(spec)-1], nil, depth+1)
			e := r.unavailable(original, kind, "relative endpoint needs the other side")
			e.Remotes = base.Remotes
			if other == nil {
				e.relative = true
				return e
			}
			if !base.Available {
				e.Reason = base.Reason
				return e
			}
			if base.relative || (base.Kind != "commit" && base.Kind != "merge-base" && base.Kind != "fork-point") {
				e.Reason = "relative base must be a commit"
				return e
			}
			if !other.Available {
				e.Reason = "other side: " + other.Reason
				return e
			}
			sha := other.SHA
			if other.Kind == "worktree" || other.Kind == "index" {
				sha = other.Head
				e.Note = "relative to the worktree's HEAD"
			}
			if sha == "" || other.Kind == "empty" {
				e.Reason = "unborn HEAD has no common ancestor"
				return e
			}
			e = r.relativeEndpoint(e, base, sha)
			e.Remotes = unionRemotes(e.Remotes, other.Remotes)
			e.Stale = e.Stale || other.Stale
			if other.Fetched != nil && (e.Fetched == nil || other.Fetched.Before(*e.Fetched)) {
				e.Fetched = other.Fetched
			}
			return e
		}
	}
	switch spec {
	case "HEAD":
		if r.head == "" {
			if r.emptySHA == "" {
				out, err := r.run(r.ctx, readTimeout, r.dir, "hash-object", "-t", "tree", "--stdin")
				if err != nil {
					return r.unavailable(original, "empty", err.Error())
				}
				r.emptySHA = strings.TrimSpace(string(out))
			}
			return Endpoint{Spec: original, Kind: "empty", Ref: "HEAD", SHA: r.emptySHA, Available: true, Repo: r.dir, Note: "unborn HEAD: empty tree"}
		}
		return Endpoint{Spec: original, Kind: "commit", Ref: "HEAD", SHA: r.head, Available: true, Repo: r.dir}
	case "WORKTREE", "INDEX":
		return Endpoint{Spec: original, Kind: strings.ToLower(spec), Ref: spec, Available: true, Head: r.head, Repo: r.dir, Path: r.dir}
	case "{main}":
		if r.mainReason != "" {
			return r.unavailable(original, "commit", r.mainReason)
		}
		e := r.resolveRef(r.mainRef, false)
		e.Spec = original
		return e
	case "{branch}":
		if r.branch == "" {
			return r.unavailable(original, "commit", "HEAD is detached")
		}
		e := r.resolveRef("refs/heads/"+r.branch, false)
		e.Spec = original
		return e
	case "{upstream}":
		if r.branch == "" {
			return r.unavailable(original, "commit", "HEAD is detached: no upstream")
		}
		up := r.refs["refs/heads/"+r.branch].upstream
		if up == "" {
			return r.unavailable(original, "commit", "no upstream")
		}
		e := r.resolveRef(up, false)
		e.Spec = original
		if !e.Available {
			e.Reason = "upstream is gone: " + up
		}
		return e
	}
	if strings.HasPrefix(spec, "worktree:") {
		path := strings.TrimPrefix(spec, "worktree:")
		if !filepath.IsAbs(path) {
			path = filepath.Join(r.dir, path)
		}
		for _, w := range r.worktrees {
			if samePath(path, w.Path) {
				if _, err := os.Stat(w.Path); err != nil {
					return r.unavailable(original, "commit", "worktree "+path+" is gone")
				}
				if w.Bare || w.Head == "" || strings.Trim(w.Head, "0") == "" {
					return r.unavailable(original, "commit", "worktree has no HEAD commit")
				}
				return Endpoint{Spec: original, Kind: "commit", Ref: "worktree:" + w.Path, SHA: w.Head, Available: true, Repo: r.dir, Path: w.Path}
			}
		}
		return r.unavailable(original, "commit", "worktree "+path+" is gone or is not registered")
	}
	verify := !strings.Contains(spec, "{main}") && !strings.Contains(spec, "{branch}")
	if strings.Contains(spec, "{main}") {
		if r.mainReason != "" {
			return r.unavailable(original, "commit", r.mainReason)
		}
		spec = strings.ReplaceAll(spec, "{main}", r.mainName)
	}
	if strings.Contains(spec, "{branch}") {
		if r.branch == "" {
			return r.unavailable(original, "commit", "HEAD is detached")
		}
		spec = strings.ReplaceAll(spec, "{branch}", r.branch)
	}
	e := r.resolveRef(spec, verify)
	e.Spec = original
	return e
}

func (r *EndpointResolver) resolveRef(ref string, verify bool) Endpoint {
	if err := ValidRef(ref); err != nil {
		return r.unavailable(ref, "commit", err.Error())
	}
	key := refRequest{ref, verify}
	if e, ok := r.verified[key]; ok {
		return e
	}
	e := Endpoint{Spec: ref, Kind: "commit", Ref: ref, Repo: r.dir}
	// Match git's unambiguous full, local, tag and remote spellings. Unknown
	// expressions (SHA, ~, ^, @{…}) go through rev-parse --verify below.
	var matches []string
	for _, candidate := range []string{ref, "refs/" + ref, "refs/tags/" + ref, "refs/heads/" + ref, "refs/remotes/" + ref, "refs/remotes/" + ref + "/HEAD"} {
		if _, ok := r.refs[candidate]; ok && !slices.Contains(matches, candidate) {
			matches = append(matches, candidate)
		}
	}
	if len(matches) == 1 {
		full := matches[0]
		v := r.refs[full]
		e.Ref = full
		if v.symref != "" {
			e.Ref = v.symref
		}
		if v.objectType == "commit" {
			e.SHA = v.sha
		} else if v.peeledType == "commit" {
			e.SHA = v.peeled
		}
	}
	if e.SHA == "" || verify {
		out, err := r.run(r.ctx, readTimeout, r.dir, "rev-parse", "--verify", ref+"^{commit}")
		if err != nil {
			e.SHA = ""
			e.Reason = fmt.Sprintf("%s is not a commit here: %v", ref, err)
		} else {
			e.SHA = strings.TrimSpace(string(out))
		}
	}
	e.Available = e.SHA != ""
	// Git also accepts symbolic expressions such as @{upstream}. Recover
	// their canonical ref so fetch scope and staleness follow the same
	// remote dependency as the equivalent named endpoint. A SHA has none.
	if e.Available && len(matches) == 0 {
		out, err := r.run(r.ctx, readTimeout, r.dir, "rev-parse", "--symbolic-full-name", ref)
		if full := strings.TrimSpace(string(out)); err == nil && r.refs[full].sha != "" {
			e.Ref = full
		}
	}
	for _, remote := range r.remotes {
		if strings.HasPrefix(e.Ref, "refs/remotes/"+remote+"/") || strings.HasPrefix(ref, remote+"/") {
			e.Remote, e.Remotes = remote, []string{remote}
			if !strings.HasPrefix(e.Ref, "refs/") {
				e.Ref = "refs/remotes/" + ref
			}
			age := r.remoteAge(e.Ref)
			e.Fetched, e.Stale = age.Fetched, age.Stale
			break
		}
	}
	r.verified[key] = e
	return e
}

// Reflog timestamps describe the update, unlike the tracked commit's date.
// Read just its tail: a long reflog must not make the summary expensive.
func reflogTime(path string) *time.Time {
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return nil
	}
	start := max(int64(0), info.Size()-65536)
	if _, err := f.Seek(start, io.SeekStart); err != nil {
		return nil
	}
	b, err := io.ReadAll(f)
	if err != nil {
		return nil
	}
	lines := strings.Split(strings.TrimRight(string(b), "\n"), "\n")
	header, _, _ := strings.Cut(lines[len(lines)-1], "\t")
	fields := strings.Fields(header)
	if len(fields) < 2 {
		return nil
	}
	seconds, err := strconv.ParseInt(fields[len(fields)-2], 10, 64)
	if err != nil {
		return nil
	}
	t := time.Unix(seconds, 0).UTC()
	return &t
}

func (r *EndpointResolver) remoteAge(ref string) Endpoint {
	if age, ok := r.ages[ref]; ok {
		return age
	}
	age := Endpoint{Fetched: reflogTime(filepath.Join(r.commonDir, "logs", ref))}
	if age.Fetched == nil {
		if info, err := os.Stat(r.fetchHead); err == nil {
			t := info.ModTime().UTC()
			age.Fetched = &t
		}
	}
	age.Stale = age.Fetched != nil && r.cfg.StaleAfter > 0 && r.now.Sub(*age.Fetched) > r.cfg.StaleAfter
	r.ages[ref] = age
	return age
}

func (r *EndpointResolver) reflogPath(ref string) string {
	dir := r.commonDir
	if !strings.HasPrefix(ref, "refs/") {
		dir = r.gitDir
	}
	return filepath.Join(dir, "logs", ref)
}

type divergence struct{ Ahead, Behind int }

// CompareCache follows tuios gitstate's cost model: spend a graph walk only
// when the SHA pair changes. The repository's shallow boundary is part of the
// key, since deepening can change counts without moving either tip.
type CompareCache struct {
	mu        sync.Mutex
	counts    map[string]divergence
	relatives map[string]string
}

func (r *EndpointResolver) relativeEndpoint(e, base Endpoint, other string) Endpoint {
	e.Ref, e.Remote, e.Remotes, e.Fetched, e.Stale = base.Ref, base.Remote, base.Remotes, base.Fetched, base.Stale
	key := r.dir + "\x00" + r.shallow + "\x00" + e.Kind + "\x00" + base.SHA + "\x00" + other
	if e.Kind == "fork-point" {
		key += "\x00" + base.Ref
		logPath := r.reflogPath(base.Ref)
		if reflogTime(logPath) == nil {
			e.Reason = "no fork point in " + base.Ref + "'s reflog"
			return e
		}
		if info, err := os.Stat(logPath); err == nil {
			key += fmt.Sprint(info.ModTime().UnixNano(), ":", info.Size())
		}
	}
	if r.cache != nil {
		r.cache.mu.Lock()
		e.SHA = r.cache.relatives[key]
		r.cache.mu.Unlock()
	}
	if e.SHA == "" {
		args := []string{"merge-base"}
		left := base.SHA
		if e.Kind == "fork-point" {
			args = append(args, "--fork-point")
			left = base.Ref
		}
		out, err := r.run(r.ctx, readTimeout, r.dir, append(args, left, other)...)
		if err != nil {
			e.Reason = "no common ancestor"
			if e.Kind == "fork-point" {
				e.Reason = "no fork point in " + base.Ref + "'s reflog"
			}
			if r.shallow != "" {
				e.Reason += " (shallow clone)"
			}
			var ge *GitError
			if errors.As(err, &ge) && (strings.Contains(ge.Err.Error(), "timed out") || errors.Is(ge.Err, context.DeadlineExceeded)) {
				e.Reason = e.Kind + " timed out"
			}
			return e
		}
		e.SHA = strings.TrimSpace(string(out))
		if r.cache != nil {
			r.cache.mu.Lock()
			if len(r.cache.relatives) > 1024 {
				r.cache.relatives = nil
			}
			if r.cache.relatives == nil {
				r.cache.relatives = map[string]string{}
			}
			r.cache.relatives[key] = e.SHA
			r.cache.mu.Unlock()
		}
	}
	e.Available, e.Reason = e.SHA != "", ""
	return e
}

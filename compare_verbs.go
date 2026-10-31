package main

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	"ofam.dev/ochazuke"
	ocli "ofam.dev/ochazuke/cli"
)

func (a *App) registerCompareVerbs() {
	r := a.r
	r.Define("endpoint").Verb("list", a.endpointList, "named endpoint specs resolved in REPO; unavailable specs retain their reason").Rows("endpoint").Fetch().Quiet().Positional("repo").
		Verb("resolve", a.endpointResolve, "resolve SPEC relative to --other (default HEAD); {remote} expands per remote").Fetch().Quiet().Positional("spec", "repo").
		Verb("show", a.endpointResolve, "show a resolved endpoint (same as endpoint.resolve)").Fetch().Quiet().Positional("spec", "repo").
		Verb("copy", a.endpointCopy, "copy the resolved SHA through the shared unnamed register").Quiet().Positional("spec", "repo")
	r.Define("compare").Verb("list", a.compareList, "saved comparisons with resolved endpoints, counts, fetch age and reasons; identical pairs collapse when configured").Rows("compare").Fetch().Quiet().Positional("repo").
		Verb("show", a.compareShow, "one saved --pair or ad hoc --from/--to comparison (default compare.default-pair)").Fetch().Quiet().Positional("repo").
		Verb("copy", a.compareCopy, "copy the resolved A..B range through the shared unnamed register").Quiet().Positional("repo").
		Verb("fetch", func(c ochazuke.Ctx) ochazuke.Result { return a.fetch(c, true) }, "explicitly fetch --prune only the remotes the given comparison uses, then resolve it again").Positional("repo")
	r.Define("repo").Verb("fetch", func(c ochazuke.Ctx) ochazuke.Result { return a.fetch(c, false) }, "explicitly fetch --prune from --remote, or every configured remote; never prompts").Positional("repo")
	setHistory(r, ochazuke.HistoryAction, "endpoint.copy", "compare.copy")
	setHistory(r, ochazuke.HistoryChange, "repo.fetch", "compare.fetch")
	repo := ochazuke.Parameter{Type: "string", Description: "repository directory or o-proj name (default current repository)"}
	spec := ochazuke.Parameter{Type: "string", Description: "endpoint name, ref/SHA or endpoint spec"}
	for _, key := range []string{"endpoint.list", "endpoint.resolve", "endpoint.show", "endpoint.copy", "compare.list", "compare.show", "compare.copy", "compare.fetch", "repo.fetch"} {
		op := r.Operations[r.CanonicalKey(key)]
		op.Parameters = map[string]ochazuke.Parameter{"repo": repo}
		switch key {
		case "endpoint.resolve", "endpoint.show", "endpoint.copy":
			op.Parameters["spec"], op.Parameters["other"] = spec, spec
		case "compare.show", "compare.copy", "compare.fetch":
			op.Parameters["pair"] = ochazuke.Parameter{Type: "string", Description: "saved pair name; mutually exclusive with --from/--to"}
			op.Parameters["from"], op.Parameters["to"] = spec, spec
		case "repo.fetch":
			op.Parameters["remote"] = ochazuke.Parameter{Type: "string", Description: "configured remote name; omitted means all remotes"}
		}
		if strings.HasSuffix(key, ".list") {
			op.Result = "row[]"
		} else if strings.HasSuffix(key, ".copy") {
			op.Result = "string"
		} else if strings.HasSuffix(key, ".fetch") {
			op.Result = "{repo, remotes, comparisons?}"
		} else {
			op.Result = strings.Split(key, ".")[0] + " or array (remote expansion)"
		}
		if strings.HasSuffix(key, ".fetch") {
			op.Parameters["verbose"] = ochazuke.Parameter{Type: "boolean", Description: "include raw git diagnostics on failure"}
		}
		r.Operations[r.CanonicalKey(key)] = op
	}
}

func (a *App) comparisonRead(c ochazuke.Ctx, work func(*EndpointResolver) (any, error)) ochazuke.Result {
	word, cfg, now := a.repoWord(c), a.compareConfig(), a.Now()
	return ochazuke.Pending(func(ctx context.Context) (any, error) {
		_, dir, err := resolve(ctx, word)
		if err != nil {
			return nil, err
		}
		r, err := NewEndpointResolver(ctx, dir, cfg, now, &a.compareCache)
		if err != nil {
			return nil, err
		}
		return work(r)
	})
}

func endpointState(e Endpoint) string {
	if !e.Available {
		return e.Reason
	}
	if e.Note != "" {
		return e.Note
	}
	return "ready"
}

func fetchedText(at *time.Time, stale bool, now time.Time) string {
	if at == nil {
		return "-"
	}
	age := max(time.Duration(0), now.Sub(*at))
	var text string
	switch {
	case age >= 24*time.Hour:
		text = fmt.Sprintf("%dd", int(age/(24*time.Hour)))
	case age >= time.Hour:
		text = fmt.Sprintf("%dh", int(age/time.Hour))
	case age >= time.Minute:
		text = fmt.Sprintf("%dm", int(age/time.Minute))
	default:
		text = fmt.Sprintf("%ds", int(age/time.Second))
	}
	if stale {
		text += " !"
	}
	return text
}

func endpointRow(e Endpoint, now time.Time) ocli.Row {
	return ocli.Row{Noun: "endpoint", ID: "endpoint/" + e.Repo + ":" + e.Source + ":" + e.Name, Label: e.Name, Data: e,
		Cells: []string{e.Name, e.Spec, e.Kind, resolvedRef(e), shortSHA(e.SHA), fetchedText(e.Fetched, e.Stale, now), endpointState(e)}}
}

func comparisonRow(c Comparison, now time.Time) ocli.Row {
	ahead, behind := strconv.Itoa(c.Ahead), strconv.Itoa(c.Behind)
	if !c.Available || (c.CommitsReason != "" && endpointKey(c.From) != endpointKey(c.To)) {
		ahead, behind = "-", "-"
	}
	return ocli.Row{Noun: "compare", ID: "compare/" + c.Repo + ":" + c.Source + ":" + c.Names[0], Label: c.Name, Data: c,
		Cells: []string{c.Name, resolvedRef(c.From), resolvedRef(c.To), ahead, behind, fetchedText(c.Fetched, c.Stale, now), c.State, c.From.Spec + ".." + c.To.Spec}}
}

func (a *App) endpointList(c ochazuke.Ctx) ochazuke.Result {
	return a.comparisonRead(c, func(r *EndpointResolver) (any, error) {
		rows := []ocli.Row{}
		for _, entry := range r.cfg.Endpoints {
			name, spec, ok := strings.Cut(entry, "=")
			if !ok || strings.TrimSpace(name) == "" {
				e := r.unavailable(entry, "", "expected NAME=SPEC")
				e.Name = entry
				rows = append(rows, endpointRow(e, r.now))
				continue
			}
			resolved := r.Resolve(strings.TrimSpace(spec), "HEAD")
			expanded, _ := r.expandAlias(spec, map[string]bool{})
			for i, e := range resolved {
				e.Name = strings.TrimSpace(name)
				e.Source = strings.TrimSpace(name)
				if strings.Contains(expanded, "{remote}") && i < len(r.remotes) {
					if strings.Contains(name, "{remote}") {
						e.Name = strings.ReplaceAll(e.Name, "{remote}", r.remotes[i])
					} else if len(resolved) > 1 {
						e.Name += " (" + r.remotes[i] + ")"
					}
				}
				rows = append(rows, endpointRow(e, r.now))
			}
		}
		return rows, nil
	})
}

func endpointArgs(c ochazuke.Ctx) (string, string) {
	spec := ochazuke.StringArg(c.Args, "spec", "")
	if e, ok := rowData[Endpoint](c); ok && spec == "" {
		spec = e.Spec
	}
	return spec, ochazuke.StringArg(c.Args, "other", "HEAD")
}

func (a *App) endpointResolve(c ochazuke.Ctx) ochazuke.Result {
	spec, other := endpointArgs(c)
	return a.comparisonRead(c, func(r *EndpointResolver) (any, error) {
		rows := r.Resolve(spec, other)
		if len(rows) == 1 {
			return rows[0], nil
		}
		return rows, nil
	})
}

func (a *App) compareList(c ochazuke.Ctx) ochazuke.Result {
	return a.comparisonRead(c, func(r *EndpointResolver) (any, error) {
		rows := []ocli.Row{}
		for _, pair := range r.Pairs() {
			rows = append(rows, comparisonRow(pair, r.now))
		}
		return rows, nil
	})
}

func comparisonArgs(c ochazuke.Ctx) (pair, from, to string, err error) {
	pair, from, to = ochazuke.StringArg(c.Args, "pair", ""), ochazuke.StringArg(c.Args, "from", ""), ochazuke.StringArg(c.Args, "to", "")
	if pair != "" && (from != "" || to != "") {
		err = fmt.Errorf("use --pair or --from/--to")
	}
	if saved, ok := rowData[Comparison](c); ok && pair == "" && from == "" && to == "" {
		pair = saved.Names[0]
	}
	return
}

func (a *App) compareShow(c ochazuke.Ctx) ochazuke.Result {
	pair, from, to, err := comparisonArgs(c)
	if err != nil {
		return ochazuke.Failed(err)
	}
	return a.comparisonRead(c, func(r *EndpointResolver) (any, error) {
		pairs := r.Show(pair, from, to)
		if len(pairs) == 1 {
			return pairs[0], nil
		}
		return pairs, nil
	})
}

// The read happens in the pending job; register mutation happens on delivery,
// through the framework verb so yank history and clipboard policy still apply.
func (a *App) copyRead(c ochazuke.Ctx, work func(*EndpointResolver) (any, error), feedback ...func(string)) ochazuke.Result {
	return a.comparisonRead(c, work).Then(func(_ ochazuke.Ctx, value any, err error) {
		if err == nil {
			_, _ = a.r.Execute(ochazuke.Request{Noun: "ochazuke.register", Verb: "set", Args: ochazuke.Arguments{"name": "\"", "text": value.(string)}})
			for _, notify := range feedback {
				notify(value.(string))
			}
		}
	})
}

func (a *App) endpointCopy(c ochazuke.Ctx) ochazuke.Result {
	spec, other := endpointArgs(c)
	return a.copyRead(c, func(r *EndpointResolver) (any, error) {
		var shas []string
		for _, e := range r.Resolve(spec, other) {
			if !e.Available {
				return nil, fmt.Errorf("%s: %s", e.Spec, e.Reason)
			}
			if e.SHA == "" {
				return nil, fmt.Errorf("%s is not a commit", e.Spec)
			}
			shas = append(shas, e.SHA)
		}
		return strings.Join(shas, "\n"), nil
	})
}

func rangeEnd(e Endpoint) string {
	if e.SHA != "" {
		return e.SHA
	}
	return e.Ref
}

func (a *App) compareCopy(c ochazuke.Ctx) ochazuke.Result {
	pair, from, to, err := comparisonArgs(c)
	if err != nil {
		return ochazuke.Failed(err)
	}
	return a.copyRead(c, func(r *EndpointResolver) (any, error) {
		var ranges []string
		for _, p := range r.Show(pair, from, to) {
			if !p.Available {
				return nil, fmt.Errorf("%s", p.Reason)
			}
			ranges = append(ranges, rangeEnd(p.From)+".."+rangeEnd(p.To))
		}
		return strings.Join(ranges, "\n"), nil
	}, func(text string) {
		if a.tui && a.r.Settings().Bool("compare.copy-feedback") {
			lines := strings.Split(text, "\n")
			for i, line := range lines {
				from, to, _ := strings.Cut(line, "..")
				lines[i] = shortSHA(from) + ".." + shortSHA(to)
			}
			a.r.UI().Info("copied " + strings.Join(lines, ", "))
		}
	})
}

type FetchResult struct {
	Repo        string       `json:"repo"`
	Remotes     []string     `json:"remotes"`
	Comparisons []Comparison `json:"comparisons,omitempty"`
}

// FetchRemotes is the single mutation shared by both verbs. The runner is
// injectable so tests can assert --prune and failure handling without deleting
// remote-tracking refs. Remote names are checked before starting any fetch.
func FetchRemotes(ctx context.Context, dir string, remotes, configured []string, run gitRunner, record func(string) error) error {
	if len(remotes) == 0 {
		return fmt.Errorf("this repository has no remotes")
	}
	for _, remote := range remotes {
		if err := ValidRef(remote); err != nil {
			return err
		}
		if !slices.Contains(configured, remote) {
			return fmt.Errorf("no configured remote %q", remote)
		}
	}
	var failures, details []string
	for _, remote := range remotes {
		if _, err := run(ctx, writeTimeout, dir, "fetch", "--prune", "--", remote); err != nil {
			failures = append(failures, remote+": "+plainFetchReason(err))
			details = append(details, err.Error())
		} else if record != nil {
			if err := record(remote); err != nil {
				failures = append(failures, remote+": fetched, but could not record fetch time: "+err.Error())
			}
		}
	}
	if len(failures) > 0 {
		return &FetchError{Reason: strings.Join(failures, "; "), Detail: strings.Join(details, "\n")}
	}
	return nil
}

type FetchError struct{ Reason, Detail string }

func (e *FetchError) Error() string { return e.Reason }

func plainFetchReason(err error) string {
	if timedOut(err) {
		return "fetch timed out; check the remote and retry"
	}
	if errors.Is(err, context.Canceled) {
		return "fetch cancelled; retry when ready"
	}
	var ge *GitError
	if !errors.As(err, &ge) {
		line, _, _ := strings.Cut(err.Error(), "\n")
		return line + "; check git remote -v and remote access"
	}
	if errors.As(err, &ge) {
		for _, line := range strings.Split(ge.Stderr, "\n") {
			line = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(line), "fatal:"))
			if strings.Contains(line, "does not appear to be a git repository") {
				path := strings.TrimSpace(strings.TrimSuffix(line, "does not appear to be a git repository"))
				return "repository " + path + " not found; check git remote -v"
			}
		}
	}
	return "could not fetch; check git remote -v and remote access (use --verbose for details)"
}

func (a *App) fetch(c ochazuke.Ctx, compare bool) ochazuke.Result {
	pair, from, to, err := comparisonArgs(c)
	if err != nil {
		return ochazuke.Failed(err)
	}
	remote := ochazuke.StringArg(c.Args, "remote", "")
	word, cfg, now := a.repoWord(c), a.compareConfig(), a.Now()
	run := a.fetchRun
	clock := a.Now
	selectedRemotes, _ := c.Args["remotes"].([]string)
	verbose, _ := c.Args["verbose"].(bool)
	return ochazuke.Pending(func(ctx context.Context) (any, error) {
		_, dir, err := resolve(ctx, word)
		if err != nil {
			return nil, err
		}
		r, err := NewEndpointResolver(ctx, dir, cfg, now, &a.compareCache)
		if err != nil {
			return nil, err
		}
		remotes := r.remotes
		if compare {
			remotes = nil
			for _, p := range r.Show(pair, from, to) {
				remotes = unionRemotes(remotes, p.Remotes)
			}
			remotes = unionRemotes(remotes, selectedRemotes)
			if len(remotes) == 0 {
				remotes = r.remotes
			}
		} else if remote != "" {
			remotes = []string{remote}
		}
		record := func(remote string) error {
			return recordRemoteFetch(cfg.StateDir, r.commonDir, remote, r.remoteURLs[remote], clock())
		}
		if err := FetchRemotes(ctx, dir, remotes, r.remotes, run, record); err != nil {
			var failure *FetchError
			if verbose && errors.As(err, &failure) {
				return nil, fmt.Errorf("%s\n%s", failure.Reason, failure.Detail)
			}
			return nil, err
		}
		result := FetchResult{Repo: dir, Remotes: remotes}
		fresh, err := NewEndpointResolver(ctx, dir, cfg, clock(), &a.compareCache)
		if err != nil {
			return nil, err
		}
		if compare {
			result.Comparisons = fresh.Show(pair, from, to)
		} else {
			result.Comparisons = fresh.Pairs()
		}
		return result, nil
	})
}

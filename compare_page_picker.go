package main

import (
	"context"
	"fmt"
	"strings"

	"github.com/unixsuperhero/ochazuke"
)

// Graph connector lines belong to a commit, never to selectable fake rows.
type sideChoice struct{ Spec, Graph string }

func (a *App) pickCompareSide(c ochazuke.Ctx, side string) ochazuke.Result {
	repo, cfg, now := a.compareRepo(c), a.compareConfig(), a.Now()
	other := "HEAD"
	if pair, ok := a.selectedComparison(); ok {
		endpoint := pair.From
		if side == "from" {
			endpoint = pair.To
		}
		other = endpoint.Spec
		if endpoint.SHA != "" && endpoint.KindKey() == "commit" {
			other = endpoint.SHA
		}
	}
	choose := func(spec string) ochazuke.Result {
		return a.compareSet(ochazuke.Ctx{Args: ochazuke.Arguments{"repo": repo, "side": side, "endpoint": spec}})
	}
	sources := []ochazuke.ChooserSource{}
	for _, name := range []string{"Endpoints", "Log", "All", "Refs", "Graph", "Worktrees"} {
		sources = append(sources, ochazuke.ChooserSource{Name: name, Load: func(_ string) ochazuke.Result {
			return ochazuke.Pending(func(ctx context.Context) (any, error) {
				_, dir, err := resolve(ctx, repo)
				if err != nil {
					return nil, err
				}
				resolver, err := NewEndpointResolver(ctx, dir, cfg, now, &a.compareCache)
				if err != nil {
					return nil, err
				}
				return loadSideChoices(resolver, name, other)
			})
		}})
	}
	_, err := a.r.OpenChooser(ochazuke.ChooserSpec{
		Kind: "compare-side", Title: "Pick " + side, Sources: sources, CreateRow: true,
		Create: choose, CreateLabel: func(query string) string { return "Use ref/spec " + query },
		Choose: func(row ochazuke.ChooserRow) ochazuke.Result { return choose(row.Data.(sideChoice).Spec) },
		Preview: func(row ochazuke.ChooserRow) string {
			choice, _ := row.Data.(sideChoice)
			return strings.TrimSpace(choice.Graph + "\n" + choice.Spec)
		},
	})
	if err != nil {
		return ochazuke.Failed(err)
	}
	if err := a.bindSideChooser(); err != nil {
		return ochazuke.Failed(err)
	}
	return ochazuke.Done(nil)
}

func loadSideChoices(r *EndpointResolver, source, other string) ([]ochazuke.ChooserRow, error) {
	var rows []ochazuke.ChooserRow
	add := func(spec, label string, cells ...string) {
		rows = append(rows, ochazuke.ChooserRow{ID: fmt.Sprintf("%s/%d", source, len(rows)), Label: label, Cells: cells, Data: sideChoice{Spec: spec}})
	}
	switch source {
	case "Endpoints":
		for _, entry := range r.cfg.Endpoints {
			name, spec, ok := strings.Cut(entry, "=")
			if !ok {
				spec = entry
			}
			expanded, _ := r.expandAlias(spec, map[string]bool{})
			for i, e := range r.Resolve(spec, other) {
				label := name
				if strings.Contains(expanded, "{remote}") && i < len(r.remotes) {
					if strings.Contains(name, "{remote}") {
						label = strings.ReplaceAll(name, "{remote}", r.remotes[i])
					} else {
						label += " (" + r.remotes[i] + ")"
					}
				}
				add(e.Spec, label+" "+e.Spec+" "+resolvedRef(e)+" "+e.Reason, label, resolvedRef(e), endpointState(e))
			}
		}
	case "Worktrees":
		for _, wt := range r.worktrees {
			spec := "worktree:" + wt.Path
			add(spec, spec+" "+wt.Branch, wt.Branch, tilde(wt.Path), shortSHA(wt.Head))
		}
	case "Refs":
		out, err := r.run(r.ctx, readTimeout, r.dir, "for-each-ref", "--format=%(refname)%00%(objectname)%00%(subject)")
		if err != nil {
			return nil, err
		}
		for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
			parts := strings.SplitN(line, "\x00", 3)
			if len(parts) == 3 {
				add(parts[0], strings.Join(parts, " "), parts[0], shortSHA(parts[1]), parts[2])
			}
		}
	case "Log", "All", "Graph":
		args := []string{"log", "--format=%x00%H%x00%h%x00%D%x00%s"}
		if source == "Graph" {
			args = append(args, "--graph")
		}
		if source == "Log" {
			ends := r.Resolve(other, "HEAD")
			if len(ends) == 0 || !ends[0].Available {
				return nil, fmt.Errorf("other side is unavailable")
			}
			if ends[0].Kind == "empty" {
				return rows, nil
			}
			sha := ends[0].SHA
			if isMutable(ends[0]) {
				sha = r.head
			}
			if sha == "" {
				return rows, nil
			}
			args = append(args, sha)
		} else {
			args = append(args, "--all")
		}
		args = append(args, "--")
		out, err := r.run(r.ctx, readTimeout, r.dir, args...)
		if err != nil {
			return nil, err
		}
		for _, line := range strings.Split(strings.TrimSuffix(string(out), "\n"), "\n") {
			parts := strings.SplitN(line, "\x00", 5)
			if len(parts) != 5 {
				if len(rows) > 0 {
					last := &rows[len(rows)-1]
					value := last.Data.(sideChoice)
					value.Graph += "\n" + line
					last.Data = value
				}
				continue
			}
			// NUL separates the graph prefix from any Git object hash format.
			graph, full := parts[0], parts[1]
			add(full, parts[2]+" "+parts[3]+" "+parts[4], graph+parts[2], parts[3], parts[4])
			rows[len(rows)-1].Data = sideChoice{Spec: full, Graph: graph + parts[2] + " " + parts[4]}
		}
	}
	return rows, nil
}

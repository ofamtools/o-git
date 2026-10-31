package main

import (
	"testing"

	"github.com/unixsuperhero/ochazuke"
	ocli "github.com/unixsuperhero/ochazuke/cli"
)

// frameworkOperations are the operations ochazuke registers on its own for
// the parts o-git builds on (the runtime and the model): every other
// operation is a verb o-git defines.
func frameworkOperations() map[string]bool {
	r := ochazuke.NewRuntime()
	ochazuke.NewModel(r)
	out := map[string]bool{}
	for key := range r.Operations {
		out[key] = true
	}
	return out
}

// Every verb o-git defines declares its history kind (Operation.HistoryKind)
// rather than leaving it to the default, and the catalog's history is that
// kind. A new verb without one fails here. The TUI and the command line
// share one runtime, so walking the catalog covers every verb.
func TestEveryVerbDeclaresItsHistory(t *testing.T) {
	framework := frameworkOperations()
	a, err := New()
	if err != nil {
		t.Fatal(err)
	}
	doc := decode[ocli.CatalogDoc](t, run(t, "", "catalog"))
	seen := map[string]bool{}
	for _, noun := range doc.Nouns {
		for _, v := range noun.Verbs {
			key := noun.Name + "." + v.Name
			if framework[key] || a.r.Operations[a.r.CanonicalKey(key)].Builtin {
				continue
			}
			seen[key] = true
			if declared := a.r.Operations[a.r.CanonicalKey(key)].HistoryKind; declared == "" || v.History != declared {
				t.Errorf("catalog verb %s: history %q, declared %q", key, v.History, declared)
			}
		}
	}
	for key := range a.r.Operations {
		if !framework[key] && !a.r.Operations[key].Builtin && !seen[key] {
			t.Errorf("verb %s is not in the catalog", key)
		}
	}
}

// A spot check of the kinds: reads are fetches, repository changes are
// changes, the prompt's keys are actions.
func TestVerbHistoryKinds(t *testing.T) {
	a, err := New()
	if err != nil {
		t.Fatal(err)
	}
	for key, want := range map[string]string{
		"repo.list": ochazuke.HistoryFetch, "repo.status": ochazuke.HistoryFetch, "repo.show": ochazuke.HistoryFetch,
		"repo.state": ochazuke.HistoryFetch, "branch.list": ochazuke.HistoryFetch,
		"worktree.add": ochazuke.HistoryChange, "branch.switch": ochazuke.HistoryChange, "branch.delete": ochazuke.HistoryChange,
		"field.commit": ochazuke.HistoryAction, "field.cancel": ochazuke.HistoryAction,
		"endpoint.list": ochazuke.HistoryFetch, "endpoint.resolve": ochazuke.HistoryFetch, "endpoint.show": ochazuke.HistoryFetch,
		"compare.list": ochazuke.HistoryFetch, "compare.show": ochazuke.HistoryFetch,
		"change.diff": ochazuke.HistoryFetch, "commit.diff": ochazuke.HistoryFetch,
		"endpoint.copy": ochazuke.HistoryAction, "compare.copy": ochazuke.HistoryAction,
		"repo.fetch": ochazuke.HistoryChange, "compare.fetch": ochazuke.HistoryChange,
		"commit.list": ochazuke.HistoryFetch, "commit.show": ochazuke.HistoryFetch, "commit.files": ochazuke.HistoryFetch,
		"change.list": ochazuke.HistoryFetch, "change.show": ochazuke.HistoryFetch,
		"compare.commits": ochazuke.HistoryFetch, "compare.files": ochazuke.HistoryFetch,
		"commit.copy": ochazuke.HistoryAction, "change.copy": ochazuke.HistoryAction,
	} {
		if got := a.r.Operations[a.r.CanonicalKey(key)].HistoryKind; got != want {
			t.Errorf("%s: %q, want %q", key, got, want)
		}
	}
}

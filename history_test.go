package main

import (
	"testing"

	"ofam.dev/ochazuke"
	ocli "ofam.dev/ochazuke/cli"
)

// Every app and framework verb declares its history kind, and the catalog
// publishes that kind. Shell adds the lazy catalog/MCP verbs, so declarations
// and published verbs must be checked on the same Shell-backed runtime.
func TestEveryVerbDeclaresItsHistory(t *testing.T) {
	a, err := New()
	if err != nil {
		t.Fatal(err)
	}
	sh := a.Shell()
	data, err := ocli.Catalog(sh.Runtime, sh.Bin, sh.Version)
	if err != nil {
		t.Fatal(err)
	}
	doc := decode[ocli.CatalogDoc](t, string(data))
	r := sh.Runtime
	seen := map[string]bool{}
	for _, noun := range doc.Nouns {
		for _, v := range noun.Verbs {
			key := noun.Name + "." + v.Name
			seen[key] = true
			if declared := r.Operations[r.CanonicalKey(key)].HistoryKind; declared == "" || v.History != declared {
				t.Errorf("catalog verb %s: history %q, declared %q", key, v.History, declared)
			}
		}
	}
	for key := range r.Operations {
		if !seen[key] {
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

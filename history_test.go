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
			if declared := r.Operations.At(ochazuke.Key(r.CanonicalKey(key))).HistoryKind; declared == "" || v.History != declared {
				t.Errorf("catalog verb %s: history %q, declared %q", key, v.History, declared)
			}
		}
	}
	for key := range r.Operations.Entries() {
		if !seen[key] {
			t.Errorf("verb %s is not in the catalog", key)
		}
	}
}

// A spot check of the kinds: reads are fetches, repository changes are
// changes.
func TestVerbHistoryKinds(t *testing.T) {
	a, err := New()
	if err != nil {
		t.Fatal(err)
	}
	for key, want := range map[string]string{
		"repo.list": ochazuke.HistoryFetch, "repo.inspect": ochazuke.HistoryFetch,
		"repo.lists": ochazuke.HistoryFetch, "branch.list": ochazuke.HistoryFetch,
		"worktree.add": ochazuke.HistoryChange, "branch.switch": ochazuke.HistoryChange, "branch.delete": ochazuke.HistoryChange,
		"branch.names":  ochazuke.HistoryFetch,
		"endpoint.list": ochazuke.HistoryFetch, "endpoint.resolve": ochazuke.HistoryFetch,
		"comparison.list": ochazuke.HistoryFetch, "comparison.inspect": ochazuke.HistoryFetch,
		"change.diff": ochazuke.HistoryFetch, "commit.diff": ochazuke.HistoryFetch,
		"endpoint.yank": ochazuke.HistoryAction, "comparison.yank": ochazuke.HistoryAction,
		"repo.fetch": ochazuke.HistoryChange, "comparison.fetch": ochazuke.HistoryChange,
		"commit.list": ochazuke.HistoryFetch, "commit.inspect": ochazuke.HistoryFetch, "commit.files": ochazuke.HistoryFetch,
		"change.list": ochazuke.HistoryFetch, "change.inspect": ochazuke.HistoryFetch,
		"comparison.commits": ochazuke.HistoryFetch, "comparison.files": ochazuke.HistoryFetch,
		"commit.yank": ochazuke.HistoryAction, "change.yank": ochazuke.HistoryAction,
	} {
		if got := a.r.Operations.At(ochazuke.Key(a.r.CanonicalKey(key))).HistoryKind; got != want {
			t.Errorf("%s: %q, want %q", key, got, want)
		}
	}
}

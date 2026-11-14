package main

import (
	"strings"
	"testing"
)

// The list nouns are thing nouns: the standard get reads one row through
// the noun's own list, and a core verb the tool does not do refuses with a
// reason. The tool's own list, create and delete verbs stay.
func TestListNounsAreThingNouns(t *testing.T) {
	dir := newRepo(t, "r")
	t.Chdir(dir)
	run(t, "", "branch", "create", "topic")
	id := "branch/" + dir + ":topic"
	got := decode[map[string]any](t, run(t, "", "branch", "get", id))
	if got["name"] != "topic" || got["id"] != id || got["label"] != "topic" {
		t.Fatalf("branch get %+v", got)
	}
	if name := decode[string](t, run(t, "", "branch", "get", id, "name")); name != "topic" {
		t.Fatalf("branch get name %q", name)
	}
	if _, _, err := try("", "branch", "get", "nope"); err == nil || !strings.Contains(err.Error(), "no branch") {
		t.Fatalf("unknown branch: %v", err)
	}
	if _, _, err := try("", "branch", "archive", id); err == nil {
		t.Fatal("a branch is archived by no verb")
	}
	rows := decode[[]row[BranchRow]](t, run(t, "", "branch", "list"))
	if len(rows) != 2 {
		t.Fatalf("the tool's own list answers rows: %+v", rows)
	}
}

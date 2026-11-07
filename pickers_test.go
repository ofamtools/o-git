package main

import (
	"testing"
)

// The tool's noun pickers are declared, so its catalog lists them.
func TestDeclaredPickers(t *testing.T) {
	a, e := New()
	if e != nil {
		t.Fatal(e)
	}
	got := map[string]bool{}
	for _, d := range a.r.CatalogPickerDefs() {
		got[d.Name] = d.Noun != "" && d.About != ""
	}
	for _, name := range []string{"repo", "worktree", "branch", "commit", "change", "compare", "endpoint"} {
		if !got[name] {
			t.Errorf("catalog does not list the %s picker: %v", name, got)
		}
	}
}

package main

import (
	"reflect"
	"strings"
	"testing"
)

// jsonFields is the JSON field names of a row struct, embedded structs
// included.
func jsonFields(t reflect.Type) map[string]bool {
	out := map[string]bool{}
	for i := range t.NumField() {
		f := t.Field(i)
		if f.Anonymous && f.Type.Kind() == reflect.Struct {
			for name := range jsonFields(f.Type) {
				out[name] = true
			}
			continue
		}
		name, _, _ := strings.Cut(f.Tag.Get("json"), ",")
		if name != "" && name != "-" {
			out[name] = true
		}
	}
	return out
}

// A parameter's row source names a field its verb's own rows carry.
// ochazuke asks for a declared subject the focused row cannot fill, so a
// source no row of the noun has (repo on a repository row, whose directory
// is its path) opens the path chooser on every key that runs the verb, in
// place of the verb's own default.
func TestRowSourcesNameAFieldOfTheNounsRows(t *testing.T) {
	a, err := New()
	if err != nil {
		t.Fatal(err)
	}
	rows := map[string]any{
		"repo": Repo{}, "worktree": WorktreeRow{}, "branch": BranchRow{}, "commit": Commit{},
		"change": Change{}, "endpoint": Endpoint{}, "comparison": Comparison{}, "diff-file": DiffFileRow{},
	}
	checked := 0
	for key, op := range a.r.Operations.Entries() {
		noun, _, _ := strings.Cut(key, ".")
		row, ok := rows[noun]
		if !ok {
			continue
		}
		fields := jsonFields(reflect.TypeOf(row))
		for name, p := range op.Parameters {
			field, sourced := strings.CutPrefix(p.From, "row:")
			if !sourced {
				continue
			}
			checked++
			if !fields[field] {
				t.Errorf("%s: parameter %s reads %q, a field no %s row has", key, name, p.From, noun)
			}
		}
	}
	if checked == 0 {
		t.Fatal("no verb declares a row source: the check walked nothing")
	}
}

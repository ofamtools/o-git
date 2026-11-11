package main

import "ofam.dev/ochazuke"

// A row id names a path, not a revision. Carry endpoint provenance through the
// same Parameter.From mechanism on keys, palettes, CLI and mounted calls so a
// historical row cannot silently turn into today's working copy in a new process.
func fileProvenance(oldField string) map[string]ochazuke.Parameter {
	params := map[string]ochazuke.Parameter{}
	for _, field := range []struct{ arg, prop string }{
		{"source", "source"}, {"revision", "revision"},
		{"base-source", "base_source"}, {"base-revision", "base_revision"},
	} {
		params[field.arg] = ochazuke.Parameter{Type: "string", From: "row:"+field.prop, Plumbing: true, Description: "selected file's Git endpoint provenance"}
	}
	params["old-path"] = ochazuke.Parameter{Type: "path", Path: "file", New: true, From: "row:"+oldField, Plumbing: true, Description: "repository-relative path on the from side"}
	return params
}

func fillProvenance(in *pathInput, args ochazuke.Arguments) {
	if in.row == nil { in.row = ochazuke.Thing{} }
	for _, field := range []struct{ arg, prop string }{
		{"source", "source"}, {"revision", "revision"},
		{"base-source", "base_source"}, {"base-revision", "base_revision"},
		{"old-path", "old_path"}, {"old-path", "oldPath"},
	} {
		if value := ochazuke.StringArg(args, field.arg, ""); value != "" { in.row[field.prop] = value }
	}
	if in.row["path"] == nil { in.row["path"] = in.path }
}

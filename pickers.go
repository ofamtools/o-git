package main

import "ofam.dev/ochazuke"

// declarePickers declares the tool's noun pickers (ochazuke.PickerDef): the
// catalog lists them and a host mounts them under the tool's prefix. Rows come
// from each noun's list verb and a pick runs the noun's own open or get, as
// the derived pickers do; declaring them gives each an About line.
func declarePickers(r *ochazuke.Runtime) error {
	for _, d := range []ochazuke.PickerDef{
		{Name: "repo", Noun: "repo", About: "The repositories"},
		{Name: "worktree", Noun: "worktree", About: "The worktrees of the repository"},
		{Name: "branch", Noun: "branch", About: "The branches of the repository"},
		{Name: "commit", Noun: "commit", About: "The commits of the current log"},
		{Name: "change", Noun: "change", About: "The changed files of the working tree"},
		{Name: "compare", Noun: "comparison", About: "The saved comparisons"},
		{Name: "endpoint", Noun: "endpoint", About: "The named endpoint specs a comparison side can take"},
	} {
		if err := r.AddPicker(d); err != nil {
			return err
		}
	}
	return nil
}

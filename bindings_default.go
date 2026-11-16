package main

import (
	"embed"

	"ofam.dev/ochazuke"
)

// bindings.json holds more key rows as data, loaded into the default layer
// (below the user's bindings.json) after the Go rows of bindings.go, so a
// row there never replaces one of those.

//go:embed bindings.json
var bindingsFS embed.FS

func loadDefaultBindings(r *ochazuke.Runtime) error {
	return r.LoadDefaultBindingsFS(bindingsFS, "bindings.json")
}

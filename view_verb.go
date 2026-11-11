package main

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"

	"ofam.dev/ochazuke"
)

type viewOptions struct {
	Page, Repo, Paths string
	Smoke             bool
	Width, Height     int
}

func (a *App) registerViewVerb() {
	a.r.Define("repo").Verb("view", func(c ochazuke.Ctx) ochazuke.Result {
		if a.tui {
			bin, err := os.Executable()
			if err != nil {
				return ochazuke.Failed(err)
			}
			args, err := json.Marshal(c.Args)
			if err != nil {
				return ochazuke.Failed(err)
			}
			return ochazuke.Foreground(exec.Command(bin, "repo", "view", "--json", string(args)))
		}
		opt := viewOptions{Page: ochazuke.StringArg(c.Args, "page", "git"), Repo: ochazuke.StringArg(c.Args, "repo", ""), Paths: ochazuke.StringArg(c.Args, "path", ""), Smoke: boolArg(c.Args, "smoke"), Width: ochazuke.IntArg(c.Args, "width", 100), Height: ochazuke.IntArg(c.Args, "height", 30)}
		return ochazuke.Pending(func(context.Context) (any, error) {
			return ochazuke.Verbatim(""), a.view(opt)
		})
	}, "open the Git or Compare page, or render one frame with --smoke; bare view and bare flags call this verb").Foreground().Quiet().Positional("path")
	op := a.r.Operations.At(ochazuke.Key(a.r.CanonicalKey("repo.view")))
	op.HistoryKind, op.Result = ochazuke.HistoryAction, "text"
	op.Parameters = map[string]ochazuke.Parameter{
		"page": {Type: "string", Enum: []string{"git", "compare"}, Description: "page to open (default git)"},
		"repo": repoParameter(), "path": directoriesParameter(),
		"smoke":  {Type: "boolean", Description: "read repositories, render one frame, then exit"},
		"width":  {Type: "integer", Description: "smoke frame width (default 100)"},
		"height": {Type: "integer", Description: "smoke frame height (default 30)"},
	}
	a.r.Operations.Set(op)
}

func boolArg(args ochazuke.Arguments, name string) bool {
	value, _ := args[name].(bool)
	return value
}

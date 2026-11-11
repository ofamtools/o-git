package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"

	"ofam.dev/ochazuke"
)

type pathNoun struct {
	name string
	files, dirs []string
	primary string
	previous []string
	repository bool
}

type pathInput struct {
	path, repo, prop string
	row ochazuke.Thing
}

type resolvedPath struct {
	Path string `json:"path"`
	Exists bool `json:"exists"`
	Source string `json:"source,omitempty"`
}

func (a *App) registerPathVerbs() {
	for _, spec := range []pathNoun{
		{name: "repo", primary: "path", dirs: []string{"path"}, repository: true},
		{name: "worktree", primary: "path", dirs: []string{"path", "repo"}},
		{name: "change", primary: "path", previous: []string{"old_path"}, files: []string{"path", "old_path"}, dirs: []string{"repo"}},
		{name: "diff-file", primary: "path", previous: []string{"oldPath"}, files: []string{"path", "oldPath", "newPath"}, dirs: []string{"repo"}},
	} {
		params := map[string]ochazuke.Parameter{
			"repo": repoParameter(),
			"path": {Type: "path", Path: "dir", From: "row:path", Description: "directory of the subject"},
			"prop": {Type: "string", Enum: append(slices.Clone(spec.files), spec.dirs...), Description: "path property; default path"},
		}
		if len(spec.files) > 0 {
			params["path"] = changedPathParameter()
			for name, p := range fileProvenance(spec.previous[0]) { params[name] = p }
			for _, name := range []string{"pair", "from", "to"} {
				params[name] = ochazuke.Parameter{Type: "string", Description: "comparison selector; use --pair or --from/--to"}
			}
		}
		for _, verb := range []struct {
			name, description string
			existing bool
			agent string
			parameters map[string]ochazuke.Parameter
			foreground bool
			deliver func(ochazuke.Ctx, string) error
		}{
			{name: "reveal", description: "reveal the actual working path; historical or missing files are not working files", existing: true, agent: ochazuke.AgentAllow, deliver: func(_ ochazuke.Ctx, path string) error { return a.r.Opener().Reveal(path) }},
			{name: "open", description: "open the actual working path through the configured kind handler (open.openers)", existing: true, agent: ochazuke.AgentConfirm, deliver: func(_ ochazuke.Ctx, path string) error { _, err := a.r.Open(path); return err }},
			{name: "open-with", description: "open the actual working path with an explicit application", existing: true, agent: ochazuke.AgentConfirm, parameters: map[string]ochazuke.Parameter{"with": {Type: "string", Required: true, Free: true, Description: "application name (macOS) or executable (other systems)"}}, deliver: func(c ochazuke.Ctx, path string) error { return a.r.Opener().OpenWith(ochazuke.StringArg(c.Args, "with", ""), path) }},
			{name: "yank-path", description: "yank the absolute path reference; this does not assert that a file exists there", agent: ochazuke.AgentAllow, deliver: func(_ ochazuke.Ctx, path string) error { return a.yankText(path) }},
			{name: "edit-file", description: "open the actual working path in $VISUAL/$EDITOR (vi by default); never edit Git objects as working files", existing: true, agent: ochazuke.AgentConfirm, foreground: true, deliver: func(c ochazuke.Ctx, path string) error {
				cmd := editorCommand(path)
				if !a.tui { cmd.Stdin, cmd.Stdout, cmd.Stderr = a.Stdin, a.Stdout, a.Stderr }
				_, err := a.r.Launch("app", spec.name+".edit-file", ochazuke.Foreground(cmd), c)
				return err
			}},
		} {
			a.r.Define(spec.name).Verb(verb.name, func(c ochazuke.Ctx) ochazuke.Result {
				return a.readPath(c, spec, verb.existing).Continue(func(value any, err error) (func(context.Context) (any, error), any, error) {
					if err != nil { return nil, nil, err }
					err = verb.deliver(c, value.(resolvedPath).Path)
					return nil, value, err
				})
			}, verb.description).Quiet().Positional("path")
			op := a.r.Operations.At(ochazuke.Key(a.r.CanonicalKey(spec.name+"."+verb.name)))
			op.Parameters = clonePathParams(params)
			op.Agent, op.HistoryKind, op.Result = verb.agent, ochazuke.HistoryAction, "{path, exists, source}"
			for name, p := range verb.parameters { op.Parameters[name] = p }
			op.Foreground = verb.foreground
			a.r.Operations.Set(op)
		}
	}
	a.registerFileExports()
}

func clonePathParams(params map[string]ochazuke.Parameter) map[string]ochazuke.Parameter {
	out := make(map[string]ochazuke.Parameter, len(params))
	for name, p := range params { out[name] = p }
	return out
}

func thingText(t ochazuke.Thing, name string) string {
	if t[name] == nil { return "" }
	return fmt.Sprint(t[name])
}

func pathArguments(c ochazuke.Ctx, primary string) pathInput {
	in := pathInput{path: ochazuke.StringArg(c.Args, "path", ""), repo: ochazuke.StringArg(c.Args, "repo", ""), prop: ochazuke.StringArg(c.Args, "prop", primary)}
	if c.Instance != nil { in.row, _ = ochazuke.ThingOf(c.Instance.Data) }
	if in.repo == "" { in.repo = thingText(in.row, "repo") }
	fillProvenance(&in, c.Args)
	if selected := thingText(in.row, in.prop); in.path == "" || in.prop != primary && selected != "" { in.path = selected }
	return in
}

func (a *App) readPath(c ochazuke.Ctx, spec pathNoun, existing bool) ochazuke.Result {
	in := pathArguments(c, spec.primary)
	if in.repo == "" { in.repo = a.repoWord(listContext(c)) }
	if in.path == "" && spec.repository { in.path = in.repo }
	if in.path == "" && !spec.repository { return ochazuke.Failed(c.Need("path", "give the subject's path or target a file row")) }
	cfg, now := a.compareConfig(), a.Now()
	pair, from, to, err := comparisonArgs(c)
	if err != nil { return ochazuke.Failed(err) }
	return ochazuke.Pending(func(ctx context.Context) (any, error) {
		var path, source string
		if len(spec.files) == 0 {
			path = absPath(in.path)
			if spec.repository {
				_, dir, err := resolve(ctx, in.path)
				if err != nil { return nil, err }
				path = dir
			}
		} else {
			_, root, err := resolve(ctx, in.repo)
			if err != nil { return nil, err }
			if slices.Contains(spec.dirs, in.prop) { return inspectPath(root, "", existing) }
			rel, err := repositoryPath(root, in.path)
			if err != nil { return nil, err }
			file, err := selectedFile(ctx, root, rel, in, pair, from, to, cfg, now)
			if err != nil { return nil, err }
			root, source = file.Repo, file.Source
			if slices.Contains(spec.previous, in.prop) { source = file.BaseSource }
			path = filepath.Join(root, rel)
			if existing && source != "worktree" {
				return nil, fmt.Errorf("%s is a %s path, not a working file; export its content to a destination first", rel, source)
			}
		}
		return inspectPath(path, source, existing)
	})
}

func inspectPath(path, source string, required bool) (resolvedPath, error) {
	_, err := os.Lstat(path)
	if err != nil && (required || !os.IsNotExist(err)) { return resolvedPath{}, fmt.Errorf("working path %s: %w", path, err) }
	return resolvedPath{Path: path, Exists: err == nil, Source: source}, nil
}

// repositoryPath accepts chooser-produced absolute paths and literal relative
// paths without ever rebasing a Git path on the process's current directory.
func repositoryPath(root, path string) (string, error) {
	if path == "" { return "", nil }
	if strings.ContainsRune(path, 0) { return "", fmt.Errorf("file path contains NUL") }
	if filepath.IsAbs(path) {
		var err error
		path, err = filepath.Rel(root, path)
		if err != nil { return "", err }
	}
	path = filepath.Clean(path)
	if path == ".." || strings.HasPrefix(path, ".."+string(filepath.Separator)) { return "", fmt.Errorf("file path must be inside repository %s", root) }
	return path, nil
}

// Adapted from o-cmds/process.go: the editor is the user's shell command, but
// the filename is a separate positional argument, never interpolated shell text.
func editorCommand(path string) *exec.Cmd {
	editor := os.Getenv("VISUAL")
	if editor == "" { editor = os.Getenv("EDITOR") }
	if editor == "" { editor = "vi" }
	return exec.Command("sh", "-c", "exec "+editor+" \"$@\"", "o-git-editor", path)
}

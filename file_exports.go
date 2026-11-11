package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"ofam.dev/ochazuke"
)

func selectedFile(ctx context.Context, root, path string, in pathInput, pair, from, to string, cfg CompareConfig, now time.Time) (Change, error) {
	rowPath, err := repositoryPath(root, thingText(in.row, "path"))
	if err != nil {
		return Change{}, err
	}
	old := thingText(in.row, "old_path")
	if old == "" {
		old = thingText(in.row, "oldPath")
	}
	if source := thingText(in.row, "source"); source != "" && (path == rowPath || path == old) {
		return Change{Repo: root, Path: rowPath, OldPath: old, Source: source, Revision: thingText(in.row, "revision"), BaseSource: thingText(in.row, "base_source"), BaseRevision: thingText(in.row, "base_revision")}, nil
	}
	r, err := NewEndpointResolver(ctx, root, cfg, now, nil)
	if err != nil {
		return Change{}, err
	}
	return findChange(r, pair, from, to, path, false)
}

func (a *App) registerFileExports() {
	for _, spec := range []struct{ noun, old string }{{"change", "old_path"}, {"diff-file", "oldPath"}} {
		a.r.Define(spec.noun).Verb("export", a.exportFile, "stream the selected Git side's exact bytes to a file; historical/deleted content is never read from today's working copy").Quiet().Positional("path", "destination")
		op := a.r.Operations.At(ochazuke.Key(a.r.CanonicalKey(spec.noun + ".export")))
		op.HistoryKind, op.Result = ochazuke.HistoryAction, "{destination, bytes, source}"
		op.Parameters = map[string]ochazuke.Parameter{
			"repo": rowRepoParameter(), "path": changedPathParameter(),
			"destination": {Type: "path", Path: "file", New: true, Required: true, Description: "output file; refuses replacement unless --force"},
			"side":        {Type: "string", Enum: []string{"from", "to"}, Description: "side to export (default to); a deleted file's content is on from"},
			"force":       {Type: "boolean", Description: "replace an existing destination after the content has been written successfully"},
		}
		for _, name := range []string{"pair", "from", "to"} {
			op.Parameters[name] = ochazuke.Parameter{Type: "string", Description: "comparison selector; use --pair or --from/--to"}
		}
		for name, p := range fileProvenance(spec.old) {
			op.Parameters[name] = p
		}
		a.r.Operations.Set(op)
	}
}

type FileExport struct {
	Destination string `json:"destination"`
	Bytes       int64  `json:"bytes"`
	Source      string `json:"source"`
}

func (a *App) exportFile(c ochazuke.Ctx) ochazuke.Result {
	in := pathArguments(c, "path")
	if in.path == "" {
		return ochazuke.Failed(c.Need("path", "give a changed path or target its row"))
	}
	if in.repo == "" {
		in.repo = a.repoWord(listContext(c))
	}
	pair, from, to, err := comparisonArgs(c)
	if err != nil {
		return ochazuke.Failed(err)
	}
	destination := ochazuke.StringArg(c.Args, "destination", "")
	if destination == "" {
		return ochazuke.Failed(c.Need("destination", "give the output file"))
	}
	side := ochazuke.StringArg(c.Args, "side", "to")
	force, _ := c.Args["force"].(bool)
	cfg, now := a.compareConfig(), a.Now()
	return ochazuke.Pending(func(ctx context.Context) (any, error) {
		_, root, err := resolve(ctx, in.repo)
		if err != nil {
			return nil, err
		}
		path, err := repositoryPath(root, in.path)
		if err != nil {
			return nil, err
		}
		file, err := selectedFile(ctx, root, path, in, pair, from, to, cfg, now)
		if err != nil {
			return nil, err
		}
		destination = absPath(destination)
		var count int64
		err = exportToFile(destination, force, func(w io.Writer) error {
			var err error
			count, err = writeFileSide(ctx, w, file, side)
			return err
		})
		if err != nil {
			return nil, err
		}
		return FileExport{Destination: destination, Bytes: count, Source: side}, nil
	})
}

// writeFileSide is independent of the destination: the shared output sink can
// replace exportToFile without changing Git content handling or stream parsing.
func writeFileSide(ctx context.Context, w io.Writer, file Change, side string) (int64, error) {
	path, source, revision := file.Path, file.Source, file.Revision
	if side == "from" {
		source, revision = file.BaseSource, file.BaseRevision
		if file.OldPath != "" {
			path = file.OldPath
		}
	}
	if source == "worktree" {
		name := filepath.Join(file.Repo, path)
		info, err := os.Lstat(name)
		if err != nil {
			return 0, err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			target, err := os.Readlink(name)
			if err != nil {
				return 0, err
			}
			n, err := io.WriteString(w, target)
			return int64(n), err
		}
		f, err := os.Open(name)
		if err != nil {
			return 0, err
		}
		defer f.Close()
		return io.Copy(w, f)
	}
	object := revision + ":" + path
	if source == "index" {
		object = ":" + path
	}
	if source == "empty" || source == "" {
		return 0, fmt.Errorf("the %s side has no file content", side)
	}
	var count int64
	err := gitStream(ctx, file.Repo, false, func(r io.Reader) error {
		var err error
		count, err = io.Copy(w, r)
		return err
	}, "cat-file", "blob", object)
	return count, err
}

// Failed reads never replace an output file or leave a partial destination.
func exportToFile(destination string, force bool, write func(io.Writer) error) error {
	f, err := os.CreateTemp(filepath.Dir(destination), ".o-git-export-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if err := write(f); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	if force {
		return os.Rename(f.Name(), destination)
	}
	return os.Link(f.Name(), destination)
}

package main

import (
	"context"
	"fmt"
	"strings"

	"ofam.dev/ochazuke"
)

// List-backed facts need the repository encoded in a foreign row key when a
// fresh CLI process (or a mounted show's get) has no selected repository.
func (a *App) scopeFactGets() {
	for _, spec := range []struct {
		noun, scope string
		compound    bool
		read        func(ochazuke.Ctx, string) (ochazuke.Thing, error)
	}{
		{noun: "repo", read: a.repoFact},
		{noun: "worktree", scope: "repo"},
		{noun: "branch", scope: "repo", compound: true},
		{noun: "commit", read: a.commitFact},
		{noun: "change", scope: "repo", compound: true},
		{noun: "diff-file", scope: "repo", compound: true},
		{noun: "endpoint", scope: "repo", compound: true},
		{noun: "comparison", scope: "repo", compound: true},
	} {
		store := *a.r.Noun(spec.noun).Store
		if spec.read != nil {
			store.Get = spec.read
		} else {
			get := store.Get
			store.Get = func(c ochazuke.Ctx, key string) (ochazuke.Thing, error) {
				repo := strings.TrimPrefix(key, spec.noun+"/")
				if spec.compound {
					repo, _, _ = strings.Cut(repo, ":")
				}
				args := ochazuke.Arguments{}
				for name, value := range c.Args {
					args[name] = value
				}
				if args[spec.scope] == nil {
					args[spec.scope] = repo
				}
				c.Args = args
				return get(c, key)
			}
		}
		a.r.Define(spec.noun).Store(store)
		for _, verb := range []string{"get", "show"} {
			op := a.r.Operations.At(ochazuke.Key(a.r.CanonicalKey(spec.noun + "." + verb)))
			op.Quiet = true
			a.r.Operations.Set(op)
		}
	}
}

func (a *App) commitFact(c ochazuke.Ctx, key string) (ochazuke.Thing, error) {
	repo, sha, ok := strings.Cut(strings.TrimPrefix(key, "commit/"), ":")
	if !ok {
		return nil, fmt.Errorf("commit key must contain REPO:SHA")
	}
	now := a.Now()
	value, err := c.Await(ochazuke.StoreCall{Store: "commit", Op: "get", Key: key}, func() ochazuke.Result {
		return ochazuke.Pending(func(ctx context.Context) (any, error) {
			_, root, err := resolve(ctx, repo)
			if err != nil {
				return nil, err
			}
			return ReadCommit(ctx, root, sha, now)
		})
	}, 0)
	if err != nil {
		return nil, err
	}
	record := value.(Commit)
	thing, err := ochazuke.ThingOf(record)
	if err != nil {
		return nil, err
	}
	item := commitItem(record, false)
	thing["id"], thing["label"] = item.ID, item.Label
	return thing, nil
}

func (a *App) repoFact(c ochazuke.Ctx, key string) (ochazuke.Thing, error) {
	selector := strings.TrimPrefix(key, "repo/")
	value, err := c.Await(ochazuke.StoreCall{Store: "repo", Op: "get", Key: key}, func() ochazuke.Result {
		return ochazuke.Pending(func(ctx context.Context) (any, error) {
			name, root, err := resolve(ctx, selector)
			if err != nil {
				return nil, err
			}
			return inspect(ctx, name, root), nil
		})
	}, 0)
	if err != nil {
		return nil, err
	}
	record := value.(Repo)
	thing, err := ochazuke.ThingOf(record)
	if err != nil {
		return nil, err
	}
	thing["id"], thing["label"] = repoID(record.Path), record.Name
	return thing, nil
}

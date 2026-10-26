# o-git: agent guide

`o-git` (so `o git`) is the family's git extension: nouns `repo`, `worktree` and `branch` on the command line (JSON, a `catalog` and a JSON Lines `state` for a host) and the TUI page Git. Usage and keys are in README.md. Design: `~/hub/o-plans/plans/cli-extensions.md` section 6 (where it is called `mtm-git`).

It is an ochazuke application, so ochazuke's rules apply here unchanged. Read `~/proj/ochazuke/AGENTS.md` first, especially "Building an application on ochazuke" and the Invariants: keys are binding rows (no key-name switches), every row is atomic, motions are visual, hints derive from bindings, Bubble Tea v2 idioms only, never bind F1 to F12. `~/proj/o-csv` and `~/proj/o-json` are the siblings it copies its shape from.

## Plans and todos

Family plans, todos and the roadmap live in `~/hub/o-plans/` (`README.md` is the index). Read it before planning work; add or update items there. Do not add a TODO.md or docs/plans/ to this repo.

## Where does X live

| Concern | File |
|---|---|
| The git layer: `git` (timeouts, environment), status, worktree and branch parsing, the mutations | `git.go` |
| Repositories: `o-proj list`, `resolve` (REPO words), `inspect`, the worktree and branch rows, `detail` | `repos.go` |
| Row ids, labels, columns, cells, `revealed` (a `--target` id to a row, for the verbs that act on one) | `rows.go` |
| `App`, `New` (runtime, lists, page, model), the shell, following the selection, refresh | `app.go` |
| Verbs | `verbs.go` |
| Key rows | `bindings.go` |
| The page (the repo list's divider follows ochazuke's pane keys through `Runtime.PaneLayout`), and `dataPage`, the data page the catalog declares for hosts (`ocli.DeclarePage`; the TUI keeps its own renderer) | `view.go` |
| The name prompt (from o-csv) | `prompt.go` |
| `view`, flags, `--smoke`, the model wrapper | `tui.go` |
| `main`, oden dispatch | `main.go` |

## Invariants

- Local only: no fetch, pull, push or tag, and no network. Every git call goes through `git` in `git.go` with a timeout and the GIT_DIR-style variables removed.
- Git runs off the event loop: every verb that reads or changes a repository is `Pending`, and the page's follow reads are launched jobs. The verbs must stay pending so git never runs on the loop. The reads are `Quiet`; the mutations (worktree add/remove, branch create/switch/delete) show on the jobs ticker and print `job #N VERB done` on the CLI's stderr (stdout stays the JSON result).
- Every verb declares its history kind (`Operation.HistoryKind`), none left to the default: the reads are `.Fetch()`, the mutations `change` and the prompt keys `action` (`setHistory` in `verbs.go`). `TestEveryVerbDeclaresItsHistory` walks the catalog.
- Destructive verbs (`worktree.remove`, `branch.delete`) carry a confirm, take plain arguments (`worktree remove PATH`, `branch delete NAME [--repo R]`; the TUI's rows fill them with `Binding.FromRow`, so the confirm reads the same on both) or a host's `--target` row id (the arguments stay optional for that: `Required` would refuse a `--target` call), check first and refuse what git would lose without `--force`. They are tested on temp repos only.
- o-proj is reached through its command line (`o-proj list`); o-git never reads or writes the manifest.
- Tests create every repository under `t.TempDir()`, shut out the owner's git config (`GIT_CONFIG_GLOBAL`), and set `O_GIT_PROJ` (none, or a fake script); no test touches a real repository or `~/hub/repos.json`.

## Checks

```sh
gofmt -l . && go vet ./... && GOWORK=off go test -race ./...
GOWORK=off go run . --smoke --path .
GOWORK=off go run . catalog >/dev/null
```

ochazuke (`v0.9.0`) and oden (`v0.1.1`) are required at their tags. An untracked `go.work` (`go work init . ../ochazuke ../oden`) builds against the sibling checkouts, and `GOWORK=off` builds from the tags as a fresh clone does. Never commit `go.work` or a `replace`.

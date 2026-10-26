# o-git

`o-git` (so `o git`) is the family's git extension: the repositories o-proj knows, plus any directory you name, with their status, worktrees and branches. It has a command line that prints JSON (for people, scripts and a host such as mtm that mounts the bin through `o-git catalog`) and a TUI page, Git. Everything goes through the `git` CLI with timeouts. It is local only: it never fetches, pulls, pushes or tags.

o-proj keeps the repositories on disk (the manifest `~/hub/repos.json`, clone, sync, the family-wide status report); o-git looks inside one repository at a time and changes its worktrees and branches. It reads o-proj's list by running `o-proj list` (`$O_GIT_PROJ` names another bin; `none` turns it off), never the manifest file itself.

## Command line

```sh
o-git repo list [--path DIR,...]          # every repo o-proj lists (+ DIRs), with status: rows
o-git repo status [REPO]                  # one repo's status
o-git repo show [REPO]                    # {repo, worktrees, branches}
o-git repo state [--path DIR,...]         # every list's rows as JSON Lines (the catalog's state)
o-git worktree list [REPO]
o-git worktree add BRANCH [PATH] [--repo REPO] [--from REF]
o-git worktree remove PATH [--force] [--yes]
o-git branch list [REPO]
o-git branch create NAME [FROM] [--repo REPO] [--switch]
o-git branch switch NAME [--repo REPO]    # or --target branch/REPO:NAME
o-git branch delete NAME [--repo REPO] [--force] [--yes]
o-git catalog
o-git NOUN --help                         # a noun's verbs and parameters
```

`REPO` is a directory (anywhere inside a worktree) or a name o-proj lists; it defaults to the current directory. A list verb answers rows, `{noun, id, label, data, cells}`, where `id` is what `--target` takes:

| Noun | Row id | Columns |
|---|---|---|
| `repo` | `repo/DIR` | repo, branch, sync, changes, path |
| `worktree` | `worktree/PATH` | branch, state, path, head |
| `branch` | `branch/REPO:NAME` (`branch/NAME` is the current directory's) | branch, sync, age, upstream, merged, subject |

`--format table|markdown|toon` prints a list verb's rows (and `repo state`'s, one table per noun) as those columns, the cells the page draws; the branch cell keeps the page's `* ` mark on the checked-out branch (`data.current` in JSON). Verbs that answer one object (`repo status`, `repo show`, the mutations) print JSON with a note.

`sync` is `-` without an upstream, `gone`, `=` when even, else `↑ahead ↓behind`; `changes` is `clean` or `+staged ~unstaged ?untracked !conflicts`.

```sh
$ o-git worktree add feat
{
  "repo": "/Users/me/proj/app",
  "path": "/Users/me/proj/app-feat",
  "branch": "feat",
  "created": true
}
$ o-git repo state --path . | head -2
{"noun":"repo","id":"repo//Users/me/proj/app","label":"app","data":{"name":"app","path":"/Users/me/proj/app","state":"dirty",...},"cells":["app","main","-","?1","~/proj/app"]}
{"noun":"worktree","id":"worktree//Users/me/proj/app","label":"~/proj/app","data":{...},"cells":["main","dirty 1 main","~/proj/app","66fa358"]}
$ o-git branch delete topic
Delete branch topic? [y/N] n
o-git: declined: Delete branch topic?
```

The destructive verbs take plain arguments, the same ones the page's `d d` passes from the focused row, and confirm by name (`Remove worktree PATH?`, `Delete branch NAME?`; `--yes` answers, otherwise a line from stdin). They refuse what git would lose without `--force`:

- `worktree remove` refuses the main worktree always, and a worktree with any change, untracked files included, unless `--force`.
- `branch delete` refuses the branch checked out in the repo's worktree always (switch first), and a branch not merged into its upstream or HEAD unless `--force`.

`worktree add` checks out an existing branch, or creates it from `--from` (default HEAD); the default path is `REPO-BRANCH` beside the main worktree (`feat/x` becomes `app-feat-x`).

## The Git page

`o-git` (or `o-git view [--path DIR,...]`) opens it; `--smoke` renders one frame and exits.

The repo list is on the left; on the right are the selected repo's worktrees, its branches and the detail of the focused row. The worktree and branch lists follow the selection in the repo list (one read off the loop each time it moves).

The keys follow the family key map (`~/hub/o-plans/plans/mtm-keys-and-ux-review-2.md`, "Key map"): `n` is new, `d d` removes or deletes, `enter` opens or switches.

| Key | Where | Does |
|---|---|---|
| `j` `k` | a list | move (past the last row into the next list) |
| `}` `{` `tab` `shift+tab` | anywhere | next or previous list |
| `enter` | repo | show it and move to its worktrees |
| `n` | worktrees | add a worktree (asks for the branch) |
| `d d` / `d D` | worktree | remove it (confirm) / remove it even if dirty |
| `n` | branches | create a branch (asks for the name) |
| `enter` | branch | switch to it |
| `d d` / `d D` | branch | delete it (confirm) / delete it even if unmerged |
| `R` | anywhere | read every repo again (by hand for now; the map wants refresh to be automatic) |
| `:` | anywhere | the palette: any verb by name |
| `?` | anywhere | every key the focus answers |
| `q` | anywhere | quit |

## Build

```sh
go build -o ~/bin/o-git .
```

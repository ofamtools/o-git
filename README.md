# o-git

`o-git` (so `o git`) is the family's git extension: the repositories o-proj knows, plus any directory you name, with their status, worktrees, branches and comparisons. It has a command line that prints JSON (for people, scripts and a host such as mtm that mounts the bin through `o-git catalog`) and a TUI page, Git. Everything goes through the `git` CLI with timeouts. It fetches only through an explicit `repo fetch` or `compare fetch`; it never pulls, pushes or tags.

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

The destructive verbs take plain arguments, the same ones the page's `d d` passes from the focused row (a host's `--target worktree/PATH` or `--target branch/REPO:NAME` row id works too), and confirm by name (`Remove worktree PATH?`, `Delete branch NAME?`; `--yes` answers, otherwise a line from stdin). They refuse what git would lose without `--force`:

- `worktree remove` refuses the main worktree always, and a worktree with any change, untracked files included, unless `--force`.
- `branch delete` refuses the branch checked out in the repo's worktree always (switch first), and a branch not merged into its upstream or HEAD unless `--force`.

`worktree add` checks out an existing branch, or creates it from `--from` (default HEAD); the default path is `REPO-BRANCH` beside the main worktree (`feat/x` becomes `app-feat-x`).

## Compare command line

The `endpoint` and `compare` nouns provide the data layer for comparisons. The Compare page, commit/file lists and diffs follow in later lanes.

| Command | Result |
|---|---|
| `endpoint list [REPO]` | Named endpoint rows: spec, kind, ref, SHA, fetched age and state |
| `endpoint resolve SPEC [--other E] [REPO]` | A resolved endpoint; `show` has the same behavior. `--other` defaults to `HEAD` |
| `compare list [REPO]` | Saved pair rows: from, to, ahead, behind, fetched age and state |
| `compare show [--pair NAME \| --from E --to E] [REPO]` | Both endpoints and counts; defaults to `compare.default-pair` |
| `endpoint copy SPEC [--other E] [REPO]` | Resolved SHA through the shared unnamed register |
| `compare copy [--pair NAME \| --from E --to E] [REPO]` | Resolved `A..B` range through that register |
| `repo fetch [REPO] [--remote R]` | An explicit `git fetch --prune` job, one remote or all configured remotes |
| `compare fetch [--pair NAME \| --from E --to E] [REPO]` | Fetch only the remote endpoints the pair uses, then resolve it again |

The list commands accept `--format table`, `markdown` and `toon`. Comparison `from`/`to` cells show resolved short refs (`master`, `origin/master`, `HEAD`); the original specs remain in JSON and in a separate, lowest-priority `spec` column. This ochazuke version has no default-hidden column flag, so the CLI includes that column. Short human reasons appear in `state`/`reason`; raw Git failures are retained separately in JSON `detail` for a detail view. Resolve/show return one object for one result and an array when `{remote}` expands to multiple results. Copy joins multiple results with newlines. `--repo R` is available everywhere; it has the same meaning as the positional `REPO`.

Endpoints are `HEAD`, `WORKTREE`, `INDEX`, a ref/SHA, `{main}`, `{branch}`, `{upstream}`, `{remote}`, `worktree:PATH`, `merge-base(X)` or `fork-point(X)`. `A | B` takes the first available alternative. When **both sides** have alternatives, combinations are tried in this fixed order: left relative + right plain, left plain + right relative, then both plain; source order is preserved within each category. A single side's alternatives retain their written order. Two relative choices cannot resolve against each other, but available plain fallbacks are still tried. Named endpoints from `compare.endpoints` work anywhere an endpoint is accepted, including inside relative specs; fully qualified refs disambiguate names that match a named endpoint. Remote identity comes from the canonical full ref, so a local `josh/feature` remains local even with a remote named `josh`; Git's ref precedence also applies when `{remote}` expands. Use `refs/remotes/josh/feature` to explicitly choose its remote-tracking ref. `{remote}` expands to one result per configured remote, with both sides of a pair using the same remote. Remote names containing `|`, `(` or `)` remain unavailable rows with a reason, rather than being reinterpreted as endpoint syntax. `worktree:PATH` uses a registered worktree's full HEAD SHA; relative paths start at REPO.

`{main}` detects the branch name from `origin/HEAD`, then the only remote's HEAD, then `compare.main-names` locally and on remotes. It uses the local branch when present, otherwise the detected remote-tracking ref. `compare.main` overrides detection. A relative endpoint uses the other side's commit, or that worktree's HEAD for `WORKTREE` and `INDEX`. Fork-point requires a reflog and never substitutes a merge-base. Unborn HEAD is the empty tree. Unavailable endpoints and pairs remain visible with reasons; reversed and non-commit pairs carry explanatory state instead of failing.

| Setting | Default |
|---|---|
| `compare.endpoints` | `head=HEAD`, `worktree=WORKTREE`, `index=INDEX`, `main={main}`, `upstream={upstream}`, `origin/main=origin/{main}`, `{remote}/main={remote}/{main}`, `{remote}/branch={remote}/{branch}`, `merge-base=merge-base({main})`, `fork-point=fork-point(origin/{main} \| {main})` |
| `compare.pairs` | `dirty=HEAD..WORKTREE`, `upstream={upstream}..HEAD`, `main={main}..HEAD`, `origin/main=origin/{main}..HEAD`, `{remote}/main={remote}/{main}..HEAD`, `{remote}/branch={remote}/{branch}..HEAD`, `merge-base=merge-base({main})..HEAD`, `fork-point=fork-point(origin/{main} \| {main})..HEAD` |
| `compare.main` | Empty: detect |
| `compare.main-names` | `main`, `master`, in order |
| `compare.collapse` | `true`: join identical available pairs' labels, keeping every name/spec in `members` |
| `compare.default-pair` | `dirty` |
| `compare.stale-after` | `24h`; `0` disables stale marking |

Fetch age is the latest of the ref's reflog update, its remote's last successful explicit o-git fetch, and `FETCH_HEAD`'s mtime when its source URL uniquely identifies that remote. Successful fetches refresh age even when no ref moved; a different remote's fetch does not freshen it. Per-remote timestamps live under `compare-fetch/` in o-git's state directory, keyed by the shared Git directory, remote name and URL; URLs are hashed rather than stored. Aliases sharing a URL use explicit per-remote records because `FETCH_HEAD` cannot distinguish them. It describes the last local information; list and show never fetch. Fetch runs in a detached session, disables terminal/askpass and credential-manager interactivity, and defaults to `ssh -o BatchMode=yes` while preserving `GIT_SSH_COMMAND` or `core.sshCommand` overrides. Failures are reported and fetch appears on the jobs ticker. Divergence counts are memoised by repository, SHA pair and shallow boundary; fork points also track the reflog stamp. Successful counts from a shallow clone are labelled "counts limited by shallow clone". No worktree scan or commit/file list is needed for these summaries.

Compared with Vim: endpoint and comparison resolution are git domain operations without a Vim command counterpart. The shared registers retain ochazuke's Vim-style yank behavior; this lane adds no keys or motions.

## The Git page

The catalog declares it as a data page (`pages` in `o-git catalog`: layout `split`, the lists Repositories, Branches and Worktrees, and a detail of the selected row's record), so a host such as mtm draws this layout from the rows instead of a generic page. The host fills the lists from `repo state`, so there its branch and worktree lists hold every repository's rows; o-git's own page below shows one repository's at a time.

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
| `ctrl+w >` `ctrl+w <` `ctrl+w =` | anywhere | widen or narrow the repo list (taller or shorter when stacked; counts repeat, `3 ctrl+w >`), `ctrl+w =` resets; remembered in `~/.local/state/o-git/panes.json` |
| `R` | anywhere | read every repo again (by hand for now; the map wants refresh to be automatic) |
| `:` | anywhere | the palette: any verb by name |
| `?` | anywhere | every key the focus answers |
| `q` | anywhere | quit |

## Build

```sh
go build -o ~/bin/o-git .
```

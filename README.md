<p align="center">
  <picture>
    <source media="(prefers-color-scheme: dark)" srcset="assets/logo-animated-dark.svg">
    <img alt="o-git" src="assets/logo-animated-light.svg" width="360">
  </picture>
  <br>
  <a href="https://github.com/ofamtools/o-git/tags"><img alt="Latest tag" src="https://img.shields.io/github/v/tag/ofamtools/o-git"></a>
  <a href="https://pkg.go.dev/ofam.dev/o-git"><img alt="Go Reference" src="https://pkg.go.dev/badge/ofam.dev/o-git.svg"></a>
  <a href="go.mod"><img alt="Go version" src="https://img.shields.io/github/go-mod/go-version/ofamtools/o-git"></a>
  <a href="LICENSE"><img alt="License" src="https://img.shields.io/github/license/ofamtools/o-git"></a>
</p>

<p align="center">Repositories, worktrees, branches and comparisons, as JSON and a TUI page.</p>

`o-git` (so `o git`) is the family's git extension: the repositories o-proj knows, plus any directory you name, with their status, worktrees, branches and comparisons. It has a command line that prints JSON (for people, scripts and a host such as mtm that mounts the bin through `o-git catalog`) and a TUI page, Git. Everything goes through the `git` CLI with timeouts. It fetches only through an explicit `repo fetch` or `comparison fetch`; it never pulls, pushes or tags.

o-proj keeps the repositories on disk (the manifest `$HOME/hub/repos.json`, clone, sync, the family-wide status report); o-git looks inside one repository at a time and changes its worktrees and branches. It reads o-proj's list by running `o-proj list` (`$O_GIT_PROJ` names another bin; `none` turns it off), never the manifest file itself.

## Install

```sh
go install ofam.dev/oden/cmd/o@latest   # oden's o: o git runs o-git, no dash needed
go install ofam.dev/o-git@latest
mtm extension register git       # mounts the Git page and the repo, worktree, branch and compare verbs in mtm
```

`o-git` with no command words, or `o git view [--page git|compare]`, opens the pages on their own.

## Command line

```sh
o git repo list [--path DIR,...]          # every repo o-proj lists (+ DIRs), with status: rows
o git repo inspect [REPO]                 # {repo, worktrees, branches}
o git repo lists [--path DIR,...]         # every list's rows as JSON Lines (the catalog's state source)
o git worktree list [REPO]
o git worktree add BRANCH [PATH] [--repo REPO] [--from REF]
o git worktree remove PATH [--force] [--yes]
o git branch list [REPO]
o git branch create NAME [FROM] [--repo REPO] [--switch]
o git branch switch NAME [--repo REPO]    # or --target branch/REPO:NAME
o git branch delete NAME [--repo REPO] [--force] [--yes]
o git catalog
o git NOUN --help                         # a noun's verbs and parameters
```

`REPO` is a directory (anywhere inside a worktree) or a name o-proj lists; it defaults to the current directory. A list verb answers rows, `{noun, id, label, data, cells}`, where `id` is what `--target` takes:

| Noun | Row id | Columns |
|---|---|---|
| `repo` | `repo/DIR` | repo, branch, sync, changes, path |
| `worktree` | `worktree/PATH` | branch, status, path, head |
| `branch` | `branch/REPO:NAME` (`branch/NAME` is the current directory's) | branch, sync, age, upstream, merged, subject |

`--format table|markdown|toon` prints a list verb's rows (and `repo lists`'s, one table per noun) as those columns, the cells the page draws; the branch cell keeps the page's `* ` mark on the checked-out branch (`data.current` in JSON). Verbs that answer one object (`repo inspect`, the mutations) print JSON with a note.

`sync` is `-` without an upstream, `gone`, `=` when even, else `↑ahead ↓behind`; `changes` is `clean` or `+staged ~unstaged ?untracked !conflicts`.

```sh
$ o git worktree add feat
{
  "repo": "/Users/me/proj/app",
  "path": "/Users/me/proj/app-feat",
  "branch": "feat",
  "created": true
}
$ o git repo lists --path . | head -2
{"noun":"repo","id":"repo//Users/me/proj/app","label":"app","data":{"name":"app","path":"/Users/me/proj/app","status":"dirty",...},"cells":["app","main","-","?1","~/proj/app"]}
{"noun":"worktree","id":"worktree//Users/me/proj/app","label":"~/proj/app","data":{...},"cells":["main","dirty 1 main","~/proj/app","66fa358"]}
$ o git branch delete topic
Delete branch topic? [y/N] n
o-git: declined: Delete branch topic?
```

The destructive verbs take plain arguments, the same ones the page's `d d` passes from the focused row (a host's `--target worktree/PATH` or `--target branch/REPO:NAME` row id works too), and confirm by name (`Remove worktree PATH?`, `Delete branch NAME?`; `--yes` answers, otherwise a line from stdin). They refuse what git would lose without `--force`:

- `worktree remove` refuses the main worktree always, and a worktree with any change, untracked files included, unless `--force`.
- `branch delete` refuses the branch checked out in the repo's worktree always (switch first), and a branch not merged into its upstream or HEAD unless `--force`.

`worktree add` checks out an existing branch, or creates it from `--from` (default HEAD); the default path is `REPO-BRANCH` beside the main worktree (`feat/x` becomes `app-feat-x`). Its branch and directory (a `path` parameter) and `branch create`'s name are declared parameters: on the page `n` opens the args form for what is missing, and `branch names [REPO]` lists the branch names that complete them.

## Files and record actions

Requires published ochazuke v0.122.1 and oden v0.5.0. Repository selectors remain directories or o-proj names. Directory and file arguments now publish their path kind in the catalog, so the same picker and completion metadata reaches the standalone tool and mounted tools. `repo list`, `repo lists` and `repo view` keep their comma-separated `--path DIR,...` selector. A chooser supplies one directory; the existing multi-directory spelling still works. This selector is not a concrete path belonging to one record.

Every facts noun has the shared `show`, `history` and `yank` actions where supported. `show KEY` opens the shared record form in the UI and returns the record on the CLI. The existing domain reads are `repo inspect`, `comparison inspect`, `commit inspect`, `change inspect` and `endpoint resolve`. Repo Enter retains the inspect-and-focus-worktrees behavior; `v` or the palette reaches the standard show form. The mounted Git detail requests the same record form. `detail.show-form`, on by default in ochazuke, turns it off.

IDs remain keys and labels retain their label role. These are read-only Git facts, not editable documents. There is no invented rename, duplicate, annotation or relation store. Existing worktree and branch mutations remain available with their original safety checks. Mtm can add its own annotations, relations and history when it mounts the catalog.

| Subject | Filesystem actions |
|---|---|
| `repo`, `worktree` | `reveal`, `open`, `open-with --with APP`, `yank-path`, `edit-file` |
| `change`, `diff-file` | The same actions, plus `export PATH DESTINATION [--side from\|to] [--force]` |
| `branch`, `commit`, `endpoint`, `comparison` | Shared path actions on their typed directory properties, without replacing domain actions such as `comparison open` |

The first two rows accept a typed `--path`, a repository selector via `--repo`, or a row `--target`. `--prop` selects another path property. Repo and worktree prefer their own `path`, not the worktree's owning repo. `edit-file` invokes `$VISUAL`, then `$EDITOR`, then `vi`. It is separate from `edit`, which remains the shared record editor. The filename is a separate shell argument, so spaces and shell metacharacters in it remain literal. `open` uses `open.openers` and the system's kind handler by default; `open-with` names an application explicitly.

File paths remain repository-relative in Git records. File actions resolve them against that record's actual repository, never the caller's working directory. Absolute paths from the chooser work when they are inside the repository. `yank-path` returns the absolute reference and whether it exists. It does not claim that a deleted file still exists. Open, reveal and editor actions refuse historical/index paths rather than opening a current file at the same name. They also refuse missing working files.

`change export` and `diff-file export` write exact content from the selected side. Use `--side from` for a deleted file. Export a historical snapshot before opening it in another application. Git-side content streams through a reader into a writer; it is not reconstructed from displayed diff lines. Exports refuse an existing destination unless `--force` is given, and a failed read never replaces it. A row's source and revision travel as declared parameters when a host forwards an action, so the same path in a different revision cannot silently become a working file.

Symlink content is the link's literal target text, matching Git's symlink blob. Export does not follow the symlink or copy a target file outside the repository.

`diff-file list [--repo REPO] [--pair NAME | --from REF --to REF]` returns structured file rows with typed `oldPath`/`newPath`, hunks and source revisions. Files already shown in the Compare diff modal remain change rows and have the same filesystem actions.

`change diff` and `commit diff` accept `--destination FILE [--force]`. Their existing `--output plain|json` still chooses the representation. Without a destination they keep their original stdout result. Destination handling is writer-shaped for the shared output-sink parameter kind; Git path selectors are not input streams.

`repo view` registers all standalone view options: `--page`, `--repo`, `--path`, `--smoke`, `--width` and `--height`. Bare `view` and bare view flags are thin calls of that verb. It remains an application noun action in catalogs, rather than a framework-only launcher that hosts can omit.

Published v0.122.1 does not yet provide `PropSpec.Self` or typed-many metadata for the comma-directory selector. The actual repo/worktree path properties are typed and ordered first, with no fake Self flag or custom parameter kind. No URL property is declared because these records contain Git refs and remote names, not URL values.

## Compare command line

The `endpoint`, `comparison`, `commit`, `change` and `diff-file` nouns provide comparison data, commit/file lists and content diffs. The Compare page is available in o-git's TUI.

| Command | Result |
|---|---|
| `endpoint list [REPO]` | Named endpoint rows: spec, kind, ref, SHA, fetched age and status |
| `endpoint resolve SPEC [--other E] [REPO]` | A resolved endpoint. `--other` defaults to `HEAD`; standard `show` displays an endpoint record by key |
| `comparison list [REPO]` | Saved pair rows: from, to, ahead, behind, fetched age and status |
| `comparison inspect [--pair NAME \| --from E --to E] [REPO]` | Both endpoints and counts; defaults to `comparison.default-pair` |
| `endpoint yank SPEC [--other E] [REPO]` | Resolved SHA through the shared unnamed register |
| `comparison yank [--pair NAME \| --from E --to E] [REPO]` | Resolved `A..B` range through that register |
| `repo fetch [REPO] [--remote R]` | An explicit `git fetch --prune` job, one remote or all configured remotes |
| `comparison fetch [--pair NAME \| --from E --to E] [REPO]` | Fetch only the remote endpoints the pair uses, then resolve it again |
| `commit list [--pair NAME \| --from E --to E] [--limit N] [REPO]` | Ahead/Behind commit rows in git's order within each section; `comparison commits` is the same read |
| `commit inspect SHA [REPO]` | Full message, parents, decorations, author and age |
| `commit files SHA [REPO]` | Changes against its first parent; a root commit compares with the empty tree |
| `commit yank SHA [REPO]` | Full SHA through the shared unnamed register |
| `change list [--pair NAME \| --from E --to E] [REPO]` | Changed paths, status, numstats and a totals row; `comparison files` is the same read |
| `change inspect PATH [--pair NAME \| --from E --to E] [REPO]` | One change, including separate index/worktree counts for an XY row |
| `change yank PATH [--pair NAME \| --from E --to E] [REPO]` | Path through the shared unnamed register |

The list commands accept `--format table`, `markdown` and `toon`. Comparison `from`/`to` cells show resolved short refs (`master`, `origin/master`, `HEAD`); the original specs remain in JSON and in a separate, lowest-priority `spec` column. The CLI includes that column. Short human reasons appear in `status`/`reason`; raw Git failures remain separately in JSON `detail` for a detail view. Resolve and comparison inspect return one object for one result and an array when `{remote}` expands to multiple results. Copy joins multiple results with newlines. `--repo R` has the same meaning as the positional `REPO`.

Endpoints are `HEAD`, `WORKTREE`, `INDEX`, a ref/SHA, `{main}`, `{branch}`, `{upstream}`, `{remote}`, `worktree:PATH`, `merge-base(X)` or `fork-point(X)`. `A | B` takes the first available alternative. When **both sides** have alternatives, combinations are tried in this fixed order: left relative + right plain, left plain + right relative, then both plain; source order is preserved within each category. A single side's alternatives retain their written order. Two relative choices cannot resolve against each other, but available plain fallbacks are still tried. Named endpoints from `comparison.endpoints` work anywhere an endpoint is accepted, including inside relative specs; fully qualified refs disambiguate names that match a named endpoint. Remote identity comes from the canonical full ref, so a local `josh/feature` remains local even with a remote named `josh`; Git's ref precedence also applies when `{remote}` expands. Use `refs/remotes/josh/feature` to explicitly choose its remote-tracking ref. `{remote}` expands to one result per configured remote, with both sides of a pair using the same remote. Remote names containing `|`, `(` or `)` remain unavailable rows with a reason, rather than being reinterpreted as endpoint syntax. `worktree:PATH` uses a registered worktree's full HEAD SHA; relative paths start at REPO.

`{main}` detects the branch name from `origin/HEAD`, then the only remote's HEAD, then `comparison.main-names` locally and on remotes. It uses the local branch when present, otherwise the detected remote-tracking ref. `comparison.main` overrides detection. A relative endpoint uses the other side's commit, or that worktree's HEAD for `WORKTREE` and `INDEX`. Fork-point requires a reflog and never substitutes a merge-base. Unborn HEAD is the empty tree. Unavailable endpoints and pairs remain visible with reasons; reversed and non-commit pairs carry explanatory state instead of failing.

| Setting | Default |
|---|---|
| `comparison.endpoints` | `head=HEAD`, `worktree=WORKTREE`, `index=INDEX`, `main={main}`, `upstream={upstream}`, `origin/main=origin/{main}`, `{remote}/main={remote}/{main}`, `{remote}/branch={remote}/{branch}`, `merge-base=merge-base({main})`, `fork-point=fork-point(origin/{main} \| {main})` |
| `comparison.pairs` | `dirty=HEAD..WORKTREE`, `upstream={upstream}..HEAD`, `main={main}..HEAD`, `origin/main=origin/{main}..HEAD`, `{remote}/main={remote}/{main}..HEAD`, `{remote}/branch={remote}/{branch}..HEAD`, `merge-base=merge-base({main})..HEAD`, `fork-point=fork-point(origin/{main} \| {main})..HEAD` |
| `comparison.main` | Empty: detect |
| `comparison.main-names` | `main`, `master`, in order |
| `comparison.collapse` | `true`: join identical available pairs' labels, keeping every name/spec in `members` |
| `comparison.default-pair` | `dirty` |
| `comparison.stale-after` | `24h`; `0` disables stale marking |
| `comparison.commits.split` | `true`: Ahead/Behind sections; `false` keeps git's full interleaved order with a `<`/`>` column |
| `comparison.commits.limit` | `500` per side, followed by an `N more` row; `--limit` overrides it, and zero shows only the more rows |
| `comparison.files.find-copies-harder` | `false`: enable to search unmodified files as copy sources in commit comparisons; potentially slow on large repositories |

Commit rows retain full SHAs in `data.sha` and carry `section` metadata when split. No date/topology ordering flag is added. Non-commit endpoints produce an empty commit list and a reason on stderr. An unborn HEAD compares as empty history.

Changes between commits have one status letter (`A M D R C T`); copies and renames keep both paths, displayed as `old → new`. Working-tree comparisons have XY letters: index against `from`, then worktree against index. `HEAD..WORKTREE` uses git status's exact paths, letters and order, including untracked directories as git normally groups them. `HEAD..INDEX` includes only staged changes; `INDEX..WORKTREE` includes unstaged and untracked paths. With a mutable left side the read is reversed with `-R` and reports “reversed: the working tree is on the left”; untracked paths on that side appear as deletions. Identical endpoints have no file rows, only zero totals.

Against an older base, a path deleted from the index and recreated as untracked has one `D?` row: the index deletion and unknown worktree content are both retained. `data.untracked` remains true, including on reversed untracked deletions.

XY numstats sum the two layers and also retain them separately in `data.index` and `data.worktree`. Untracked paths have no numstat and show blank count cells; they count toward the file total. Binary counts display `bin` and contribute to `data.binary` in the totals, without invented line counts. Diff reads pin `core.quotePath=false`, `--no-color`, `--no-ext-diff`, `--no-textconv` and `--no-relative`. All use `-M`; commit comparisons also enable `-C`. Searching unmodified copy sources with `--find-copies-harder` requires `comparison.files.find-copies-harder=true`. Working-tree copies remain additions with their full added-line counts, as in git status.

Fetch age is the latest of the ref's reflog update, its remote's last successful explicit o-git fetch, and `FETCH_HEAD`'s mtime when its source URL uniquely identifies that remote. Successful fetches refresh age even when no ref moved; a different remote's fetch does not freshen it. Per-remote timestamps live under `compare-fetch/` in o-git's state directory, keyed by the shared Git directory, remote name and URL; URLs are hashed rather than stored. Aliases sharing a URL use explicit per-remote records because `FETCH_HEAD` cannot distinguish them. It describes the last local information; list and show never fetch. Fetch runs in a detached session, disables terminal/askpass and credential-manager interactivity, and defaults to `ssh -o BatchMode=yes` while preserving `GIT_SSH_COMMAND` or `core.sshCommand` overrides. Failures are reported and fetch appears on the jobs ticker. Divergence counts are memoised by repository, SHA pair and shallow boundary; fork points also track the reflog stamp. Successful counts from a shallow clone are labelled "counts limited by shallow clone". No worktree scan or commit/file list is needed for these summaries.

Compared with Vim: endpoint and comparison resolution are git domain operations without a Vim command counterpart. The shared registers retain ochazuke's Vim-style yank behavior; this lane adds no keys or motions.

### Content diffs

`change diff PATH [--pair NAME | --from E --to E] [--context N] [--repo R]`
prints the unified patch for a literal path relative to the repository root.
Either path selects a rename and preserves both names. The default pair is
`comparison.default-pair` (`dirty=HEAD..WORKTREE`). `commit diff SHA [--context N]
[--repo R]` compares a commit with its first parent, or the empty tree for a root
commit. Merge commits use the first parent, yielding an ordinary unified patch.

Plain CLI output is byte-for-byte Git patch text with pinned
`core.quotePath=false`, `diff.noprefix=false`, `--no-color`, `--no-ext-diff`,
`--no-textconv`, `--no-relative`, `-M` and `GIT_PAGER=cat`. External diff programs,
textconv and pagers are disabled. Commit, index and worktree pairs work in either
direction; mutable-left patches use `-R`. Untracked files use read-only
`git diff --no-index -- /dev/null PATH` semantics and are appended in Git's
`ls-files --others --exclude-standard` order. No index or object is written.

`--output json` returns `{comparison, reversed, files}` (an array of results for remote
expansion). Each file uses the ochazuke diffview model's JSON names: `oldPath`,
`newPath`, `status`, `binary`, `oldSize`, `newSize`, `truncated`, `added`, `removed`,
`hunks`; hunks use `oldStart`, `oldCount`, `newStart`, `newCount`, `header`, `lines`.
Line `kind` is numeric: context 0, addition 1, deletion 2; `old`, `new`, `text`
and `noNewline` preserve source locations and missing final newlines. Binary
sizes come from object sizes or file stat; -1 means unknown. Unavailable and
equal comparisons retain their state with an empty `files` array.

`comparison.diff.context` defaults to 3; `--context 0` is supported.
`comparison.diff.max-lines` defaults to 20000 source lines per file. Structured
hunks also have a fixed 2 MiB per-file byte limit. Exceeding either limit clears
that file's hunks and marks `truncated: true`, keeping complete `added`/`removed`
counts. Plain patch text is complete. CLI and host/API calls default to plain
patch text; `--output plain|json` explicitly selects either result. The shell's
bare `--json` and `--format json` do not change the verb's `output` parameter;
use `--output json` for structured hunks. Ochazuke v0.66 through v0.69 does not
offer a verb result with separate text and structured forms selected by format.

Compared with Vim: these commands read Git content and add no editor, hunk
motions or bindings. The Compare page uses the shared diffview Vim `]c`/`[c` interaction.

## The Git page

The catalog declares it as a data page (`pages` in `o git catalog`: layout `split`, the lists Repositories, Branches and Worktrees, and a detail of the selected row's record), so a host such as mtm draws this layout from the rows instead of a generic page. The host fills the lists from `repo lists`, so there its branch and worktree lists hold every repository's rows; o-git's own page below shows one repository's at a time.

`o-git` (or `o git view [--repo DIR] [--path DIR,...]`) opens it; `--smoke` renders one frame and exits. `--repo DIR` selects the repository containing DIR, including a subdirectory or linked worktree, independently of o-proj. It puts that repository first and keeps o-proj's other repositories; `--path` adds directories without changing the selection. If discovery fails, the explicit repository and extra paths still open. A directory outside a repository remains visible with its reason.

The repo list is on the left; on the right are the selected repo's worktrees, its branches and the detail of the focused row. The worktree and branch lists follow the selection in the repo list (one read off the loop each time it moves).

The keys follow the family key map: `n` is new, `d d` removes or deletes, `enter` opens or switches.

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

## The Compare page

`o git view --page compare [--repo DIR] [--path DIR]` opens Compare on the selected repository, using the current directory's repository when neither flag is given. An unavailable repository opens the page with a reason. The noun-only `o git comparison` remains the CLI noun's help; use the view flag to open the page. `c` on a repository, worktree or branch opens it with that row as the `to` side and the configured default pair's `from` side. The page is local to o-git's TUI; native host mounting is a later plan.

The catalog declares a `diff` side titled **Side diff**, with default terminal leader key `g`. Once mtm supports sides as data and o-git is registered as its Git extension, leader `g` opens this Compare page beside the current pane, using that pane's directory and environment. The command template is `{"command":["{bin}","view","--page","compare","--repo","{dir}"]}`; the host supplies the values as arguments. `q` on Compare quits o-git and closes the side; in a file diff, `q` first closes that modal. The repository stays fixed until the host restarts the side.

The declaration adds no standalone key or process. In mtm, the side is enabled when the extension is registered; a `sides.json` override `{"name":"diff","disabled":true}` disables it, and `{"name":"diff","key":""}` removes its default leader key. Host bindings remain overridable. Compared with Vim's `:terminal` split (`:help :terminal`), the host chooses the side's program from a catalog row; o-git keeps its own Compare bindings inside it.

Pairs sit on the left with from/to, ahead/behind, file counts, fetched age and status. Every pair occupies one row: cells truncate with an ellipsis; collapsed labels show the first name plus `+N`, with every name and the full status/reason in Detail. Low-value columns drop first when space is tight: fetched when all rows have no fetch time, then to when every target is HEAD. Unavailable pairs remain dimmed with reasons. The initial selection is `comparison.default-pair` (`dirty`). At 80 columns the pairs list stacks above the other panels.

A replacing read off the event loop follows pair selection, filling Commits with native Ahead/Behind sections (or interleaved rows when `comparison.commits.split=false`) and Files with statuses, paths, numstats and totals. While Commits has focus, Files follows the selected commit against its first parent, or the empty tree for a root commit. `enter` focuses those files. The Files title identifies that commit; Escape in Files or focusing Pairs restores comparison files. Detail follows the focused pair, commit, file or totals row. Fetch ages update in compact units, with the last-fetch age also shown in pair Detail. Raw endpoint diagnostics remain in JSON `detail`; fetch failures have a plain reason and next step, with raw diagnostics available through `--verbose`.

`comparison pair-files` returns Files from a drilled-into commit to the selected comparison; Escape in Files invokes this verb.

`enter` on a file opens its structured diff beside a file list in a modal. The same path works for commit files. Diffview owns rendering, hunk/line scrolling and file-navigation keys; its `diff.file` callback loads the next or previous actual file without visiting totals. `q` or Escape closes the diff and restores its file selection. The app calls the merged Go readers directly; no subprocess invokes o-git itself. Fetch remains explicit only.

| Key | Where | Does |
|---|---|---|
| `f` / `t` | Compare | choose the from / to endpoint |
| `tab` / `shift+tab` | side picker, automatic commit search disabled | next / previous match mode |
| `ctrl+o` / `alt+o` | side picker | next / previous source: Endpoints, Log, All, Refs, Graph, Worktrees |
| `enter` | side picker | take the highlighted row, including the typed ref/spec create row |
| `~` | Compare | swap the two sides |
| `F` | Compare | fetch the union of remotes used by every name in the selected group, or all configured remotes when no dependency is known; re-resolve on completion |
| `F` | Git rows | fetch that repository's configured remotes |
| `enter` | pair | focus Commits |
| `enter` | commit | show that commit's files |
| `enter` | file | open its diff |
| `esc` | Files | return to the pair's files |
| `f` / `t` | commit | set from / to to that commit |
| `y y` | commit / file | copy SHA / path through the shared register |
| `] f` / `[ f` | diff | next / previous file (counts supported) |
| `] c` / `[ c` | diff | next / previous hunk |
| `J` / `K`, `ctrl+d` / `ctrl+u` | diff | scroll lines / half pages |
| `}` / `{`, `tab` / `shift+tab` | diff modal | walk between the file list and diff |
| `q` / `esc` | diff modal | close and return to the file row |
| `y y` | pair | copy its resolved range through the shared unnamed register |
| `R` | Compare | resolve the pairs again |

The picker's Log/All/Graph sources preserve git's order. Graph connector lines stay attached to the commit in its preview; ochazuke v0.72.1 picker cells have no `Wrap` column API yet. A typed ref, SHA, `merge-base(...)` or `fork-point(...)` becomes an ad hoc `(custom) A..B` row at the top, including an unavailable spec and its reason. Edits are remembered per repository in `compare.json` in o-git's state directory, without changing settings. `comparison save NAME` appends the selected comparison to `comparison.pairs` through the settings API. `pages.hidden` containing `compare` hides the page from the tabs and cycling; direct opening remains available, following the framework's behavior. Bindings are individually replaceable or removable through `bindings.json`.

When a nonempty query matches no existing source row, the side picker searches local commits from the repository's HEAD, pinned when the search starts. A SHA prefix of at least four hex characters matches the full SHA; text matches a case-insensitive substring of the commit subject. Results show short SHA, subject, author and date, and picking one sets that side to the full SHA. Reads run off the UI loop in chunks of 25, with each matching chunk displayed immediately. The default search stops after 100 commits. **Search 100 more commits…** continues from that offset, retaining matches; it stays available whenever a full block was scanned, even if that block found results. It may take one more read to establish that the history ended exactly at the block boundary. Editing the query or closing the picker cancels pending work. These are local history reads, with no network fetch.

Automatic search uses ochazuke's live source API. Existing source rows use the configured `picker.match` and `search.case`; history completion uses literal subject substrings and SHA prefixes. Live sources own their matching in ochazuke v0.79, so Tab's interactive match-mode cycling is unavailable while automatic search is enabled. Set `comparison.commit-search-depth` to `0` to restore ordinary picker filtering and Tab cycling. The explicit ref/spec row remains available after search results.

| Diff setting | Default |
|---|---|
| `comparison.diff.layout` | `auto`: unified below 140 diff-content columns, split at 140 or more; `unified` and `split` force either layout |
| `comparison.diff.highlight` | `true`: syntax highlighting; false disables it |
| `comparison.pairs.width-percent` | `40`: maximum wide-layout share for Pairs; `60` restores the former share |
| `comparison.files.follow-commit` | `true`: Files follows the focused commit; false loads only on Enter |
| `comparison.chooser.resolve-typed` | `true`: preview and prioritize the exact typed spec; false keeps the ordinary create row |
| `comparison.commit-search-depth` | `100`: commits scanned per automatic search block; `0` disables commit search and restores ordinary picker matching |
| `comparison.copy-feedback` | `true`: show a plain notice after copying a comparison range; false silences it |

The diff view reads layout and highlighting every frame; the merged reader's `comparison.diff.context` and `comparison.diff.max-lines` apply when a file loads. The existing `comparison.files.find-copies-harder` and commit split/limit settings apply on refresh. All bindings remain individually overridable or removable.

Compared with Vim: Vim's `f`/`t` (`:help f`, `:help t`) find characters in text; these lists have no text cursor, so the keys pick from/to. Vim's `~` (`:help ~`) changes character case; Compare uses it to swap endpoints. List `j`/`k` retain the framework's visual row motion and spill rules. Diff hunk `] c`/`[ c` follow Vim `:help ]c`/`:help [c`, with counts. Split layout is similar to `:diffsplit`, comparing Git endpoints instead of editor buffers. No staging or diff-get/put action is added.

| Vim form | Help tag | Compare behavior |
|---|---|---|
| `CTRL-W w` / `CTRL-W W` | `CTRL-W_w`, `CTRL-W_W` | Similar: `}` / `{` walk lists in screen order, Pairs then Commits then Files |
| `CTRL-W j` / `CTRL-W k` | `CTRL-W_j`, `CTRL-W_k` | Similar: list `j` / `k` spill at their edges in screen order; they do not wrap at the page edge |
| Git endpoint selection and fetch | n/a | Domain operations with no Vim counterpart |
| Command-line completion | `cmdline-completion`, `c_CTRL-D` | Similar: narrow candidates while typing; Git SHA/subject history search streams bounded chunks and offers explicit continuation instead of Vim's command/file completion |

## Build

```sh
go build -o ~/bin/o-git .        # from a checkout
```

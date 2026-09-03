# treehouse

[![release](https://img.shields.io/github/v/release/AirConditionedSoftware/treehouse)](https://github.com/AirConditionedSoftware/treehouse/releases)
[![build](https://github.com/AirConditionedSoftware/treehouse/actions/workflows/release.yml/badge.svg)](https://github.com/AirConditionedSoftware/treehouse/actions/workflows/release.yml)
[![license](https://img.shields.io/badge/license-MIT-blue.svg)](LICENSE)

treehouse (command: `th`) is a small CLI for managing git worktrees.
Worktree placement is driven by a JSON config file and can be overridden
per repository.

```console
$ th add feature/login
Creating worktree with new branch "feature/login" from main
/Users/you/worktrees/myapp/feature-login

$ th list
* myapp [main]
  1a2b3c4d Merge pull request #7 (2 hours ago) | 0 unstaged
  feature-login [feature/login]
  8ca50149 Add OAuth flow (3 days ago) | 2 unstaged | ↑1 | ✗ not merged into main
```

## Install

```sh
brew install AirConditionedSoftware/tap/treehouse
```

Or grab a binary from
[GitHub Releases](https://github.com/AirConditionedSoftware/treehouse/releases), or
install with Go:

```sh
go install github.com/AirConditionedSoftware/treehouse/cmd/th@latest
```

## Usage

- `th list [--json]` — list all worktrees of the current repository, two
  lines per worktree: the worktree name with its `[branch]`, then the last
  commit (hash, subject, age), the number of pending files (`N unstaged` —
  staged, unstaged, and untracked), how the branch relates to its upstream
  (`↑N` commits to push / `↓N` to pull, shown only when out of sync;
  `upstream gone` when the branch was deleted on the remote; nothing for
  branches without an upstream), and whether the branch is merged into
  the default branch (`✓ merged` green / `✗ not merged` yellow; omitted for
  the default branch itself). The upstream counts use local knowledge only —
  list never fetches, so `git fetch` first for fresh numbers. The `*` marks
  the worktree you're in; locked and prunable worktrees carry inline tags,
  and `--json` has full paths and flags plus `ahead`/`behind` (present —
  including at 0 — only for branches with a live upstream) and
  `upstream_gone`. The open and remove pickers show the same entries.
- `th add <branch> [--base <ref>] [--path <dir>]` — create a worktree:
  - branch exists locally → checked out as-is
  - branch exists on `origin` → local branch created tracking it (fetches
    once if the remote ref isn't known yet)
  - otherwise → new branch created from `--base`, the config's
    `default_base`, or the current HEAD — named with the config's
    `branch_prefix` if one is set (`--no-prefix` skips it)
  - `--copy-hooks` copies the repo's git hooks into the new worktree;
    `--no-copy-hooks` overrides a `copy_hooks` config that enables it
  - `--copy-file <path-or-glob>` (repeatable) copies untracked files into
    the new worktree on top of the config's `copy_files` list;
    `--no-copy-files` skips the config list
  - `--link-file <path-or-glob>` (repeatable) symlinks untracked files or
    directories into the new worktree on top of the config's `link_files`
    list; `--no-link-files` skips the config list
  - `--open` opens the new worktree in VS Code (the `.code-workspace` file
    if one was written, the folder otherwise); `--no-open` overrides a
    `vscode.open` config that enables it
  - the config's `pre_create` commands run in the main worktree before
    anything is created (the worktree doesn't exist yet); a failure aborts
    the add. `--no-pre-create` skips them once
  - `--no-post-create` skips the config's `post_create` commands
- `th add pr <number|url>` — create a worktree from a GitHub pull request,
  given its number, `#number`, or web URL (the PR must belong to `origin`):
  - with the [gh CLI](https://cli.github.com) installed and authenticated,
    the worktree is created on the PR's actual head branch; a same-repo PR
    tracks `origin`, so pushed fixups land on the PR
  - without gh, the PR head is fetched directly via `refs/pull/<n>/head`
    into a branch named `pr-<n>`
  - a PR from a fork is checked out from `refs/pull/<n>/head`; pushes from
    that worktree won't reach the fork (use `gh pr checkout` for that
    workflow), and merged/closed PRs warn but still check out
  - the configured `branch_prefix` never applies — the branch belongs to the
    PR author — and re-running with the branch still around reuses it as-is
  - shares `th add`'s `--path`, copy, open, and post-create flags (`--base`
    and `--no-prefix` don't apply). One limitation: a branch literally named
    `pr` can't be added with `th add pr` — use `git worktree add` for it
- `th refresh [branch...]` — re-run the config-driven provisioning on
  worktrees that already exist, so they catch up with config changes made
  since they were created: the hooks copy, `copy_files`/`link_files`
  placement, and the workspace file (window color included) are applied
  again. With no arguments the worktree you're standing in is refreshed;
  paths work too. The main worktree is never refreshed — it's the source
  copies and links come from. Files git tracks are never touched; copied
  files are overwritten, so an updated `.env` in the main worktree
  propagates; `link_files` paths that already exist in the worktree are
  noted and left alone, while missing ones get their symlink.
  `post_create` doesn't re-run by default (setup commands like
  `npm install` can be expensive or stateful) — `--post-create` opts in,
  and commands a repo's `.thrc` supplied still go through the usual
  approval gate. `pre_create` never re-runs; its before-anything-is-created
  contract can't hold for an existing worktree. Shares `th add`'s
  `--copy-hooks`/`--no-copy-hooks`, `--copy-file`/`--no-copy-files`, and
  `--link-file`/`--no-link-files` flags. Prints nothing on stdout — all
  narration goes to stderr, ending with a `Refreshed <path>` line per
  worktree.
- `th remove [branch...] [--force] [--delete-branch]` (aliases: `th rm`,
  `th -r`) — remove the worktrees that have the given branches checked out;
  the branches themselves are kept unless `--delete-branch`/`-d` is passed.
  Paths work too. With no arguments, an interactive picker lets you select
  one or more worktrees to delete, showing each branch's last commit and how
  long ago it was made. Refuses to remove the main worktree or the one
  you're in. A worktree with modified or untracked files prompts you to
  force or skip it — per worktree, so multi-removals decide each one;
  `--force`/`-f` skips the prompt, and non-interactive use errors and asks
  for `--force` instead of hanging. `--delete-branch` deletes each removed
  worktree's branch with `git branch -d`; when git refuses because the
  branch isn't fully merged, an interactive run asks before forcing with
  `-D`, and a non-interactive run keeps the branch with a note (the removal
  still succeeds). The default branch is never deleted, and `--force`
  doesn't extend to branch deletion. Each removal announces its target and
  size up front — counted `[2/5]` when removing several — and on a terminal
  the line ticks with elapsed time while a big worktree is deleted. When
  `vscode.workspace_file` is enabled, the th-generated `.code-workspace`
  sibling is deleted along with the worktree. The config's `pre_remove`
  commands run inside the worktree once removal is decided — a dirty
  worktree's `--force` doesn't skip them — and before anything is deleted;
  a failure blocks that removal (and stops a multi-removal at the first
  failure), so teardown can rely on the files still being there. The
  `post_remove` commands run last, in the main worktree, after the removal
  (and any branch deletion) has settled; a failure is reported but the
  removal stands. Skip them once with `--no-pre-remove` /
  `--no-post-remove`. One edge: a `pre_remove` hook that dirties a clean
  worktree can make the non-forced git removal fail — the remedy is
  `--force`.
- `th clean [--dry-run] [--yes] [--force] [--delete-branches]` — find
  worktrees that look finished — the branch is merged into the default
  branch, or was deleted on `origin` — and offer to remove them in an
  interactive multi-select with every candidate preselected. Branches are
  kept unless `--delete-branches`/`-d` is passed; only the worktrees go,
  with the same dirty-prompt, progress, workspace-file cleanup, and
  `pre_remove`/`post_remove` hooks as `th remove` — each cleaned worktree
  runs both. Remote-tracking refs are refreshed with a best-effort
  `fetch --prune` first. The main worktree, the one you're in, and the
  default branch are never candidates. `--dry-run`/`-n` lists candidates
  and reasons without removing; `--yes` removes all candidates without the
  picker (the non-interactive mode); `--force` skips the dirty prompt.
  `--delete-branches` works like `th remove --delete-branch` per candidate;
  under `--yes` an unmerged branch is never force-deleted — git's refusal
  keeps it with a note, so a gone-from-origin branch carrying local-only
  commits survives. Complements `th prune`, which only cleans bookkeeping
  for directories that are already gone.
- `th open [branch]` — open a worktree in VS Code: with no argument an
  interactive picker lists the worktrees, with a branch (or path) it opens
  that one directly. Opens the th-generated `.code-workspace` file when the
  worktree has one, the folder otherwise. Only available when `vscode.open`
  is enabled for the repo — without it the command tells you what to set.
- `th cd [branch]` — change directory to a worktree: with no argument an
  interactive picker lists the worktrees, with a branch (or path) it
  resolves that one directly. The resolved path is the only stdout output,
  so `cd "$(th cd my-branch)"` works anywhere; with the wrapper from
  `th shell-init` installed, plain `th cd my-branch` changes your shell's
  directory itself — always, regardless of the `auto_cd` and `vscode.open`
  settings, because `th cd` is an explicit request to navigate.
- `th run` — run the command configured as `run` — the one command you keep
  coming back to in a repository (a dev server, a test watcher) — in the
  root of the current worktree, from any subdirectory. The command runs in
  the foreground with your terminal's stdin/stdout/stderr: th prints
  nothing on stdout (`th run | grep …` pipes only the command's output),
  Ctrl-C reaches the command, and `th run` exits with the command's exit
  code — with no `th:` line added, since the command already reported its
  own failure. A `run` supplied by the repo's `.thrc` needs your approval
  first, like the lifecycle hooks — but unapproved without a terminal to
  ask at, `th run` errors instead of skipping, because the command is the
  whole job (see "Repo-local config" below).
- `th prune [--dry-run]` — clean up git's bookkeeping for stale worktrees,
  i.e. entries whose directories were deleted manually (tagged `prunable`
  in `th list`). Prints what it prunes; branches and existing directories
  are untouched.
- `th du [--unit KB|MB|GB]` (alias: `th disk`) — disk space used by each
  worktree, largest first, plus a total. Sizes count the working files (the
  shared repository database in `.git` isn't attributed to any worktree);
  by default each row picks a readable unit, `--unit`/`-u` forces one.
- `th config` — print the config file location (stderr) and its content
  (stdout, so `th config | jq` works). Prints the built-in defaults if no
  file exists, and fails loudly if the file is invalid. Run inside a repo
  that has a `.thrc`, it prints and validates that file too, after the
  global one.
- `th config --effective` — print the fully merged settings for the
  current repository as a table, with the layer each value came from:
  `default`, `top-level`, `repos[N]`, or `.thrc`. The layers consulted
  (config file, matching `repos` entry, `.thrc`) go to stderr. The
  debuggable view of the [four-layer merge](#per-repository-overrides-repos).
- `th migrate [--global] [--dry-run] [--yes] [--backup|--no-backup]` —
  update the repository's `.thrc` to the current config schema on demand,
  instead of waiting for the next interactive command to offer it (see
  "Schema versioning" below). It asks twice — to confirm the update, then
  whether to back the file up first — and the flags pre-answer both
  questions, so a script can run it without a terminal; without them a
  non-interactive run errors instead of guessing. `--global`/`-g` migrates
  the global config file as well, backing that one up automatically.
  `--dry-run`/`-n` prints, per file, the schema versions, the backup it
  would create, and a diff of the rewrite, and changes nothing.
- `th schema [--global]` — print the JSON Schema for a repository's `.thrc`
  on stdout, with the track and its schema version on stderr, so
  `th schema | jq` and `th schema > thrc.schema.json` both work. `--global`
  prints the schema for the global `config.json` instead.
- `th schema install [--dry-run] [--settings-path <file>]` — write both
  schemas to `~/.th/` and point VS Code's user settings at them, so editing
  a `.thrc` or a `config.json` gets completion, hover documentation, and
  validation. Idempotent: re-run it after upgrading th. A `settings.json`
  that isn't strict JSON is never rewritten — th prints the snippet to paste
  and exits non-zero. See "Editor integration" below.
- `th init [flags]` — create a starter repo-local `.thrc` at the main
  worktree root (refuses to overwrite an existing one) and print its path,
  so `$EDITOR "$(th init)"` opens it. Flags pre-fill fields — `--name`,
  `--worktree-dir`, `--base`, `--prefix`, `--separator`, the repeatable
  `--copy-file` and `--post-create`, and the booleans `--copy-hooks`,
  `--open`, `--workspace-file` (written as `true` only when passed; an
  explicit `--copy-hooks=false` writes `false`, which overrides a global
  `true`) — as in
  `th init --prefix peter --separator - --base develop`. See "Repo-local
  config" below.
- `th completion` — interactive wizard that sets up shell completion: pick
  your shell (preselected from `$SHELL`), get the line to add to its startup
  file copied to your clipboard, and the steps to finish.
  `th completion <bash|zsh|fish|powershell>` prints the raw script that the
  installed line sources.
- `th shell-init [zsh|bash|fish]` — print the shell wrapper that lets th
  change your shell's directory (see "Shell integration" below); with no
  argument the shell is detected from `$SHELL`. Run bare in a terminal it
  also prints the line to add to your startup file. PowerShell is not
  supported yet.

Output is colored when stdout is a terminal: in `th list` and the pickers,
worktree names are bright, branches green, commit metadata gray, `↑N` cyan
and `↓N` yellow, merge status green (merged) or yellow (not merged), and
`locked`/`prunable`/`upstream gone` show as cyan/yellow/yellow inline
tags — every status is also a plain word or glyph, so piped output keeps
the information. `th du` colors its header and TOTAL row.
Disable with `--no-color` or the [`NO_COLOR`](https://no-color.org)
environment variable; piped output is always plain text.

Paths under your home directory display as `~/...`; show absolute paths with
the global `--full-paths` flag or the `full_paths` config setting.
Machine-facing output — the path `th add` prints on stdout and everything
`--json` — always uses full paths.

### Shell integration

A program can't change its parent shell's directory, so th ships a wrapper.
Add one line to your shell's startup file:

```sh
eval "$(th shell-init zsh)"     # ~/.zshrc
eval "$(th shell-init bash)"    # ~/.bashrc
th shell-init fish | source     # ~/.config/fish/config.fish
```

The wrapper runs the real th binary with a private temp file (`TH_CD_FILE`)
and cds to whatever th writes there; stdout and stderr pass through
untouched. With it installed, `th cd` jumps between worktrees and `th add`
leaves your terminal inside the worktree it just created.

**When the add opens VS Code, the open wins**: if `vscode.open` is enabled
(or `--open` is passed), the terminal does not move — even with an explicit
`--cd` — so creating a worktree from an integrated terminal never yanks the
base repo's shell around. Auto-cd applies only when no window is opening,
and can be turned off with `"auto_cd": false` (globally or per repo) or
`--no-cd` for one invocation.

`th add` prints only the created path on stdout (everything else goes to
stderr), so even without the wrapper you can hop into a new worktree with a
shell function:

```sh
thcd() { cd "$(th add "$1")"; }
```

## Configuration

Config lives at `~/.th/config.json`, or wherever `$TH_CONFIG` points. A missing
file at the default location just means defaults; a missing file at an
explicit `$TH_CONFIG` path is an error.

The full schema — every option shown, everything optional. All settings can
be set at the top level (applying to every repo) and overridden per repo:

```json
{
  "version": 2,
  "worktree_dir": "~/worktrees/{repo}/{branch}",
  "default_base": "main",
  "branch_prefix": "peter",
  "prefix_separator": "/",
  "copy_hooks": false,
  "copy_files": [".env*"],
  "link_files": ["node_modules"],
  "vscode": {
    "open": false,
    "workspace_file": false,
    "workspace_prefix": "ws-",
    "window_title": "${rootName} — ${activeEditorShort}",
    "window_color": "auto",
    "workspace_paths": [
      { "name": "notes", "path": "~/notes" }
    ]
  },
  "full_paths": false,
  "auto_cd": false,
  "pre_create": [],
  "post_create": ["direnv allow"],
  "pre_remove": [],
  "post_remove": [],
  "run": "npm run dev",
  "update_check": false,
  "repos": [
    {
      "name": "myapp",
      "path": "~/code/myapp",
      "worktree_dir": "~/code/myapp-trees/{branch}",
      "default_base": "develop",
      "branch_prefix": "team",
      "prefix_separator": "-",
      "copy_hooks": true,
      "copy_files": [".env*", "config/local.json"],
      "link_files": ["node_modules"],
      "vscode": {
        "open": true,
        "workspace_file": true,
        "workspace_prefix": "acs-",
        "window_title": "myapp — ${activeEditorShort}${separator}${branchName}",
        "window_color": "#336699",
        "workspace_paths": [
          { "name": "docs", "path": "~/notes/myapp" },
          { "path": "~/code/shared-lib" }
        ]
      },
      "full_paths": true,
      "auto_cd": false,
      "pre_create": [],
      "post_create": ["npm install", "direnv allow"],
      "pre_remove": ["docker compose down"],
      "post_remove": [],
      "run": "npm install && npm run dev"
    }
  ]
}
```

A repository can also carry a `.thrc` of its own, accepting the same
settings fields plus `name` — see "Repo-local config" below.

One nuance when overriding: an explicit `false` in a repo entry *does*
override a top-level `true` for the boolean fields (`copy_hooks`,
`vscode.open`, `vscode.workspace_file`, `full_paths`, `auto_cd`), and `"copy_files": []`
clears an inherited list — but an empty *string* is treated as unset and
falls through. The `vscode` object merges field by field like everything
else: an entry that sets only `window_color` keeps the inherited rest of
the object rather than replacing it.

- `version` — the config schema this file is written in. th maintains it;
  absent means 1. See "Schema versioning" below.
- `worktree_dir` — path template for new worktrees. `{repo}` is the directory
  basename of the main worktree, `{branch}` is the branch name with `/`
  replaced by `-` (so `feature/login` → `feature-login`). A leading `~`
  expands to your home directory. Default: `~/worktrees/{repo}/{branch}`.
- `default_base` — ref that brand-new branches start from. Default: current
  HEAD.
- `branch_prefix` — prefix for branch names that `th add` creates, joined to
  the name with `prefix_separator`: with `"branch_prefix": "peter"`,
  `th add fix-login` creates the branch `peter/fix-login`. A prefix that
  already ends in the separator isn't doubled, so `"peter/"` works the same.
  Branches that already exist — with or without the prefix — are used as-is,
  and typing an already-prefixed name won't double-prefix it. Bypass for one
  invocation with `--no-prefix`.
- `prefix_separator` — what joins `branch_prefix` to the branch name.
  Default: `/`. Set `"-"` for branches like `peter-fix-login`.
- `copy_hooks` — copy the repo's git hooks into each new worktree. This
  matters when `core.hooksPath` points inside the worktree (husky's
  `.husky`, a `.githooks` dir): git resolves such a path per worktree, so
  new worktrees silently lose the hooks. Plain `.git/hooks` needs no copying
  — git already shares it across all worktrees, and th says so instead of
  copying. `--copy-hooks` / `--no-copy-hooks` override the config per
  invocation.
- `copy_files` — untracked files to copy into each new worktree, as paths or
  globs relative to the main worktree (`.env*`, `config/local.json`). A
  matched directory copies recursively; permissions are preserved; a pattern
  that matches nothing prints a note. On copy-on-write filesystems (APFS on
  macOS; Btrfs/XFS on Linux) copies are cloned — metadata-only reflinks — so
  big caches copy near-instantly; elsewhere the bytes stream as before.
  Large copies show live per-match
  progress (files and bytes) on the terminal, so big directories don't sit
  silent. **Files git tracks are never copied**: the checkout already put
  them in the worktree, and overwriting them with the main worktree's
  (possibly modified) copy would silently dirty a fresh checkout — a
  tracked match is skipped with a note instead. A repo entry's list
  *replaces* the global one (it doesn't append), and an explicit `[]` turns
  copying off for that repo. `--copy-file` adds one-off entries;
  `--no-copy-files` skips the config list. Worktrees created before an
  entry was added catch up with `th refresh`.
- `link_files` — like `copy_files`, but each match becomes a **symlink to
  the main worktree's copy** instead of a duplicate — the way to share a
  multi-gigabyte `node_modules` or build cache across worktrees at zero
  cost. The link is absolute, created after copies (a path in both lists
  gets the copy), and never placed over tracked content or an existing
  path — both are skipped with a note. Shared means shared: an install run
  in one worktree is visible in all of them, and the target should be
  gitignored or it shows as an untracked file in every worktree. A repo
  entry's list *replaces* the global one; `[]` disables. `--link-file`
  adds one-off entries; `--no-link-files` skips the config list. Worktrees
  created before an entry was added catch up with `th refresh`.
- `vscode` — every VS Code setting, grouped in one object. Inside it the
  keys are short (`"open"`, `"window_color"`); everywhere else — this
  reference, th's messages, `th config --effective` — they're named dotted:
  `vscode.open`, `vscode.window_color`.
  - `vscode.open` — open each new worktree in VS Code after creation (needs
    the `code` CLI on PATH; a missing CLI is a warning, not a failure). Opens
    the `.code-workspace` file when one is written, the folder otherwise.
    `--open` / `--no-open` override per invocation. Also gates the `th open`
    command.
  - `vscode.workspace_file` — write a
    `<vscode.workspace_prefix><branch>.code-workspace` file for each new
    worktree, containing a `folders` array with the worktree path and
    `settings["window.title"]`. The file is created *next to* the worktree
    directory (a sibling, not inside it), so it never shows up as an untracked
    file in git; `th remove` cleans it up along with the worktree.
  - `vscode.workspace_prefix` — prefix for the workspace file's name, e.g.
    `"acs-"` → `acs-fix-login.code-workspace`. Default: none (just the
    sanitized branch name).
  - `vscode.window_title` — the `window.title` value written into the
    workspace file, taken **verbatim**, so VS Code title variables like
    `${activeEditorShort}` or `${dirty}` pass straight through. Default: the
    repo name.
  - `vscode.window_color` — colors the worktree window's title bar and status
    bar (via `workbench.colorCustomizations` in the generated workspace file),
    so each worktree window is visibly distinct. `"auto"` derives a stable
    color from the repo and branch — the same worktree gets the same color on
    every machine, and every worktree gets its own; a fixed `"#rrggbb"` colors
    all th worktrees alike. The text color is picked automatically (white or
    black) for contrast, and the inactive title bar gets the same color at
    60% opacity. Requires `vscode.workspace_file`. Default: off.
  - `vscode.workspace_paths` — extra folders to include in generated workspace
    files, as `{name, path}` objects (`name` is the display name in VS Code
    and optional; `path` is required, with `~` expanding to your home
    directory). They're appended to the `folders` array after the worktree
    itself, so the workspace spans multiple folders. A repo entry's list
    replaces the global one.
- `full_paths` — show absolute paths in tables, prompts, and messages
  instead of abbreviating your home directory to `~`. Same effect as the
  global `--full-paths` flag. (`th add`'s stdout path and `--json` output
  always use full paths regardless.)
- `auto_cd` — with the `th shell-init` wrapper installed, `th add` leaves
  your terminal inside the worktree it just created. Default: **true** —
  installing the wrapper is the real opt-in; without it the setting has no
  effect. An effective VS Code open always wins: when the add opens a
  window (`vscode.open` or `--open`), the terminal stays put even with an
  explicit `--cd`. Override once with `--cd`/`--no-cd`. Settable in a
  repo's `.thrc` (harmless there, unlike `update_check`: it runs nothing —
  it only moves a shell whose owner installed the wrapper).
- `post_create` — shell commands run inside each newly created worktree, in
  order via `sh -c`, after hooks and files are copied and before VS Code
  opens (so `npm install` finishes first). Each command is printed before it
  runs; the first failure stops the rest and reports it, but the worktree
  survives. Skip once with `--no-post-create`. A repo entry's list
  *replaces* the global one; `[]` disables. **Security posture**: these are
  arbitrary commands by design. Commands in this file are user-owned and
  run as written; commands that come from a repo's `.thrc` run only
  after you approve them (see "Repo-local config" below), so a cloned repo
  can't inject commands silently. Worktree metadata reaches the commands as
  environment variables (`TH_WORKTREE`, `TH_MAIN`, `TH_REPO`, `TH_BRANCH`)
  rather than being interpolated into the command string, so branch names
  containing shell metacharacters are inert. Do note that a command like
  `npm install` executes the checked-out branch's own install scripts — the
  same risk as running it yourself.
- `pre_create`, `pre_remove`, `post_remove` — the rest of the lifecycle
  hooks, sharing `post_create`'s mechanics: sequential `sh -c`, each
  command printed before it runs, the first failure stops the rest, the
  `TH_*` environment variables, a repo entry's list *replaces* the global
  one and `[]` disables, and the same approval gate for `.thrc`-supplied
  lists. They differ only in when and where they fire:

  | hook          | runs                                     | working directory | a failure means                        |
  | ------------- | ---------------------------------------- | ----------------- | -------------------------------------- |
  | `pre_create`  | before the worktree is created           | main worktree     | the add aborts; nothing was created    |
  | `post_create` | after creation, hooks, and file copying  | the new worktree  | reported; the worktree survives        |
  | `pre_remove`  | once removal is decided, before deletion | the worktree      | the removal is blocked                 |
  | `post_remove` | after the removal has settled            | main worktree     | reported; the removal stands           |

  `TH_WORKTREE` always names the worktree the operation is about — for
  `pre_create` the target that doesn't exist yet, for `post_remove` the
  path that just stopped existing — which is exactly what external cleanup
  keys on (`docker volume rm "app-$TH_BRANCH"`). The two hooks on the
  "doesn't exist" side of their transition run in the main worktree, the
  one directory every repo operation can count on; the other two run in
  the worktree itself. `--force` never skips a hook (it answers the dirty
  question, not teardown); the skip flags — `--no-pre-create`,
  `--no-pre-remove`, `--no-post-remove` — skip one hook for one
  invocation.
- `run` — the repository's one foreground command for `th run` (see usage
  above): run via `sh -c` in the current worktree's root, with the
  terminal's stdin/stdout/stderr attached and the same `TH_*` environment
  variables as the hooks. A single string, not a list — it is one process
  you attach to, and compound commands come free via `sh -c`
  (`"npm install && npm run dev"`). An empty string falls through the
  layers like every other string field. Like the hooks, a `run` that comes
  from a repo's `.thrc` requires your approval before it executes.
- `update_check` — opt in to `th --version` querying GitHub for the latest
  release and mentioning (on stderr) when a newer one exists. Off by
  default; th makes no network calls beyond git otherwise. Failures are
  silent — a version query never breaks because the network did. Top-level
  only: this is the one setting a repo's `.thrc` cannot set, so a cloned
  repository can never switch on network calls.
- `repos` — per-repository overrides, explained below.

Unknown keys are rejected so typos fail loudly.

### Schema versioning

Both config files carry a top-level `"version"` naming the schema they were
written in — **2** today, on independent tracks that are free to drift
apart. A file without a `version` is read as version 1, so every config
already on disk keeps working untouched.

When th loads a file written in an older schema it migrates it forward
automatically, one version at a time, so a file several versions behind
catches up in a single run. `config.json` is rewritten in place, with the
original copied beside it first as
`config.json.v<old>.<YYYYMMDD-HHMMSS>.bak` — next to the file itself, so a
relocated `$TH_CONFIG` keeps its backup with it, and the timestamp means a
backup never overwrites an earlier one. `.thrc` is migrated in memory and
rewritten only after it asks — see "Repo-local config" below. You can also
run the update explicitly with `th migrate` (add `--global`/`-g` for the
global file); `--dry-run` previews the rewrite as a diff without changing
anything.

Migration is forward-only: a file whose `version` is newer than the running
binary understands is an error telling you to upgrade th, rather than a
guess at what the newer schema meant. Read-only commands like `th list`
carry on, as they do with any config they can't read.

**Upgrading from v1**: nothing to do — th does it for you the first time it
reads the file. Version 2 is version 1 with the six VS Code settings moved
into the `vscode` object, on both tracks:

| v1                        | v2                          |
| ------------------------- | --------------------------- |
| `vscode_open`             | `vscode.open`               |
| `vscode_workspace_file`   | `vscode.workspace_file`     |
| `vscode_workspace_prefix` | `vscode.workspace_prefix`   |
| `vscode_window_title`     | `vscode.window_title`       |
| `vscode_window_color`     | `vscode.window_color`       |
| `workspace_paths`         | `vscode.workspace_paths`    |

Values carry over untouched, in `repos` entries as well as at the top
level; nothing else about the file changes.

### Command-line flags with config equivalents

Flags always win over the config for that one invocation:

| flag                             | config field    | notes                                                                                    |
| -------------------------------- | --------------- | ---------------------------------------------------------------------------------------- |
| `th add --base <ref>`            | `default_base`  | one-off base for the new branch                                                           |
| `th add --path <dir>`            | `worktree_dir`  | flag is a literal one-off location; the config field is a `{repo}`/`{branch}` template    |
| `th add --no-prefix`             | `branch_prefix` | skips the configured prefix once                                                          |
| `th add --copy-hooks` / `--no-copy-hooks` | `copy_hooks` | force hook copying on or off once; also on `th refresh`                          |
| `th add --copy-file <glob>` / `--no-copy-files` | `copy_files` | `--copy-file` (repeatable) adds one-off entries; `--no-copy-files` skips the config list; also on `th refresh` |
| `th add --link-file <glob>` / `--no-link-files` | `link_files` | `--link-file` (repeatable) adds one-off entries; `--no-link-files` skips the config list; also on `th refresh` |
| `th add --open` / `--no-open`    | `vscode.open`   | force opening in VS Code on or off once                                                   |
| `th add --no-pre-create`         | `pre_create`    | skip the configured commands once                                                          |
| `th add --no-post-create`        | `post_create`   | skip the configured commands once                                                          |
| `th refresh --post-create`       | `post_create`   | opt in to re-running the configured commands on a refresh                                  |
| `th remove --no-pre-remove`      | `pre_remove`    | skip the configured commands once; also on `th clean` and `th -r`                         |
| `th remove --no-post-remove`     | `post_remove`   | skip the configured commands once; also on `th clean` and `th -r`                         |
| `th add --cd` / `--no-cd`        | `auto_cd`       | force the shell-integration cd on or off once; an effective VS Code open still wins       |
| `--full-paths` (any command)     | `full_paths`    | show absolute paths instead of `~`                                                        |

`--no-color`, `th remove --force`, and `th remove --delete-branch` are
flag-only; `vscode.workspace_file`, `vscode.workspace_prefix`,
`vscode.window_title`, `vscode.window_color`, `vscode.workspace_paths`, and
`prefix_separator` are config-only.

### Repo-local config (`.thrc`)

A repository can keep its own config in a `.thrc` at the root of its
**main worktree**. It is never read from a linked worktree, but it applies
whenever th runs from any of the repo's worktrees — th finds the main
worktree through git. No file means nothing changes. `th init` scaffolds
one (writing to the main worktree root even when run from a linked
worktree) and prints its path, so `$EDITOR "$(th init)"` opens it directly.
Its flags pre-fill fields, e.g.
`th init --prefix peter --separator - --base develop`.

It holds the same settings fields as the global config, plus `name` (what
`{repo}` expands to) and a `version` of its own (see below). `repos` and
`path` are rejected, because the file already *is* repo-specific; unknown
keys fail loudly as usual, and a broken `.thrc` is an error like a broken
global config. Since it lives in the repo, it can be committed and shared
with a team:

```json
{
  "version": 2,
  "name": "myapp",
  "branch_prefix": "team",
  "prefix_separator": "-",
  "vscode": {
    "workspace_file": true,
    "window_color": "auto"
  },
  "post_create": ["npm ci"]
}
```

A ready-made starting point lives at [examples/.thrc](examples/.thrc) — copy
it to the repo root (`cp examples/.thrc .thrc`) and trim what you don't need.

`.thrc` is the **top layer**: its values override both the global
top-level fields and the repo's global `repos` entry, field by field, with
the same merge rules as everywhere else — an empty string falls through to
the layer below, a list replaces the list below it, and an explicit `false`
overrides an inherited `true`.

`.thrc` tracks its schema version independently of the global config (see
"Schema versioning" above); absent means 1 there too, and `th init` writes
the current version. It is the one file th won't rewrite behind your back:
when the schema has moved on, th migrates the file in memory and the next
interactive command (`th add`, `th run`, `th open`, `th cd`, `th config`)
offers to update it on disk, asking first whether to back it up. Saying yes
writes `.thrc.v<old>.<YYYYMMDD-HHMMSS>.bak` beside it in the repo root —
worth covering with a `.thrc.v*.bak` line in `.gitignore`, and a committed
`.thrc` shows up as modified in git once the update lands. Non-interactive
runs can't ask: they use the migrated settings in memory for that run,
leave the file exactly as it is, and say so on stderr —
`th migrate --yes --backup` (or `--no-backup`) performs the update from a
script.

Because a committed `.thrc` arrives with the repository, commands that
come from it — the lifecycle hooks `pre_create`, `post_create`,
`pre_remove`, `post_remove`, and the `run` command alike — run only after
you approve them, and each is approved separately. The first time a hook's
list (or the run command) would run, th prompts, showing the commands;
approvals are remembered in `~/.th/trust.json`, keyed by the repo's main
worktree and the hook, so approving `pre_remove` never approves
`post_remove`. Any later change to a hook's commands — including one that
arrives with a `git pull` — prompts again, showing a diff of the approved
commands against the new ones:

```
post_create in ~/code/myapp/.thrc changed:

    direnv allow
  - npm install
  + npm ci

Allow these commands to run after th add? [Allow and remember / Skip this time]
```

Declining skips that hook for that run and nothing else — the worktree is
still created or removed — and records nothing, so the next run asks
again. Non-interactive runs can't ask, so they skip unapproved commands
with a warning and carry on. That posture is deliberate for the remove
side: an unapproved or declined `pre_remove`/`post_remove` is skipped,
never a reason to refuse the removal, so a committed `.thrc` can gain code
execution only through your approval and can never block you from deleting
your own worktree (a `th clean --yes` in automation stays unwedgeable).
Hooks in the global config are user-owned and never prompt.

`th run` differs in one way: an unapproved (or declined) `run` command is
an **error**, not a skip. `th add` can skip an unapproved `post_create`
because the worktree was still created; for `th run` the command is the
whole job, and a warn-but-exit-0 would masquerade as success. Approving
works the same — run `th run` in a terminal, review the command, and the
approval is remembered until the `.thrc`'s `run` changes.

### Per-repository overrides (`repos`)

`repos` is an array of entries, each tying one repository — identified by
the **filesystem path of its main worktree** — to settings that override the
top-level ones:

- `path` (required) — the path of the repo's main worktree, e.g.
  `~/code/myapp`. Before comparing, `~` is expanded and symlinks are
  resolved on both sides, and because th finds the main worktree through
  git, the entry applies no matter which of the repo's worktrees you run th
  from. The first matching entry wins.
- `name` (optional) — what `{repo}` expands to in `worktree_dir` templates
  for this repo. Default: the directory basename of the main worktree.
- any of the settings fields above: `worktree_dir`, `default_base`,
  `branch_prefix`, `prefix_separator`, `copy_hooks`, `copy_files`,
  `link_files`, `full_paths`, the lifecycle hooks (`pre_create`,
  `post_create`, `pre_remove`, `post_remove`), `run`, and the `vscode`
  settings.

Settings resolve in four layers, field by field:

1. built-in defaults, overlaid with
2. the top-level fields in th.json, overlaid with
3. the `repos` entry whose `path` matches, overlaid with
4. the repo's own `.thrc`, if it has one.

A layer only needs the fields it wants to change; anything it omits falls
through to the layer below. With the example config above — and the
`.thrc` from the previous section sitting in `~/code/myapp` — running
`th add fix-login` inside `~/code/myapp` (or any of its worktrees) resolves
to:

| field              | value                         | comes from |
| ------------------ | ----------------------------- | ---------- |
| `worktree_dir`     | `~/code/myapp-trees/{branch}` | repo entry |
| `default_base`     | `develop`                     | repo entry |
| `branch_prefix`    | `team`                        | `.thrc` |
| `prefix_separator` | `-`                           | `.thrc` |
| `post_create`      | `["npm ci"]`                  | `.thrc` |

`.thrc` has the last word on the bottom three rows: it repeats the
entry's `branch_prefix` and `prefix_separator`, and its `post_create`
replaces the entry's `["npm install", "direnv allow"]` — a repo-sourced
list, so it needs approval before it runs. The created branch is
`team-fix-login` (and the copy and `vscode` behaviors come from the repo
entry too), while any other repo gets `~/worktrees/{repo}/{branch}`,
`main`, and `peter` with the default `/` separator (branches like
`peter/fix-login`) from the top level. Note that an empty string in a repo
entry does not clear an inherited value — it's treated the same as omitting
the field (use `--no-prefix` to skip a prefix per invocation).

Repos without an entry need no configuration at all; `repos` is purely
opt-in per repo. An entry without a `path` is rejected when the config is
loaded.

When the merge surprises you, `th config --effective` prints exactly this
table for the current repository — every setting's effective value and the
layer it came from — plus which config file, `repos` entry, and `.thrc`
were consulted.

### Editor integration

Both config files are plain JSON, and th ships a JSON Schema for each, so an
editor can offer completion, hover documentation, and validation while you
write one. The schemas are built into the binary — nothing is hosted and th
makes no network call to set this up.

`th schema` prints the `.thrc` schema on stdout and names the track and its
schema version on stderr, so `th schema | jq` and
`th schema > thrc.schema.json` both work; `--global` prints the schema for
the global `config.json` instead.

`th schema install` does the wiring for VS Code. It writes
`~/.th/thrc.schema.json` and `~/.th/config.schema.json`, then patches your VS
Code **user** `settings.json` with a `files.associations` entry mapping
`.thrc` to `json` — the file has no extension VS Code recognizes — and two
`json.schemas` entries pointing at those local files:

```json
{
  "files.associations": { ".thrc": "json" },
  "json.schemas": [
    { "fileMatch": ["**/.thrc"], "url": "/Users/you/.th/thrc.schema.json" },
    { "fileMatch": ["**/.th/config.json"], "url": "/Users/you/.th/config.schema.json" }
  ]
}
```

The paths are absolute because VS Code does not expand `~` in a schema
`url`, and the schema files always land in `~/.th` even when `$TH_CONFIG`
points the config elsewhere — they're machine state, like `trust.json`. When
`$TH_CONFIG` is set, its expanded path joins `**/.th/config.json` in the
global schema's `fileMatch`, so a relocated config is covered too. User
settings are the right scope: `.thrc` files turn up in many repositories,
and nobody has to commit a `.vscode/settings.json` for this.

Install is idempotent — re-running rewrites the schema files and updates the
same settings entries in place. Do that **after upgrading th**: the schemas
describe the config schema that version of th understands.

- `--dry-run` reports the files it would write and how the settings would
  change, and touches nothing.
- `--settings-path <file>` names the `settings.json` to patch. The default is
  VS Code's own user settings
  (`~/Library/Application Support/Code/User/settings.json` on macOS,
  `~/.config/Code/User/settings.json` on Linux,
  `%APPDATA%\Code\User\settings.json` on Windows); the flag is how you point
  th at Insiders, VSCodium, or Cursor.

A `settings.json` with comments or trailing commas — JSONC, which VS Code
accepts and plenty of people use — is **never rewritten**. th refuses,
prints the exact snippet to paste, and exits non-zero rather than
reformatting your settings behind your back; the schema files are written
first, so the snippet works the moment you paste it. When the file is strict
JSON it is rewritten, with a timestamped backup beside it first.

One caveat: VS Code settings sync carries the absolute schema `url` to your
other machines, where a different home directory turns it into "unable to
load schema" on hover. Running `th schema install` there fixes it.

## Development

```sh
go test ./...
go build ./cmd/th
```

Requires the `git` binary on PATH (all git operations shell out; worktree
porcelain output is a stable interface).

## Releasing

Releases are automated with [goreleaser](https://goreleaser.com) via GitHub
Actions. Every push to `main` runs the tests and, when code shipped in the
binaries has changed since the last release (`*.go`, `go.mod`, `go.sum`, or
`.goreleaser.yaml` — docs-only pushes don't release), bumps the patch
version from the latest `v*` tag (`v0.2.0` → `v0.2.1`), pushes the new tag,
and publishes a GitHub release with darwin/linux binaries for amd64/arm64.
A commit message containing `#minor` bumps the minor instead (`v0.2.x` →
`v0.3.0`). For a major bump, push a tag yourself (`git tag v1.0.0 && git
push origin main v1.0.0`) — the workflow releases exactly that version and
later `main` pushes continue from it. Validate the goreleaser config locally with
`goreleaser release --snapshot --clean`.

Each release also updates the Homebrew cask in
[AirConditionedSoftware/homebrew-tap](https://github.com/AirConditionedSoftware/homebrew-tap),
pushed by goreleaser using the `TAP_GITHUB_TOKEN` repository secret — a
token with write access to the tap repository.

## Contributing

Issues and pull requests are welcome — see
[CONTRIBUTING.md](CONTRIBUTING.md) for the project layout and conventions.
`go test ./...` runs the unit and end-to-end tests (a real `git` on PATH is
all they need). Keep in mind that every push to `main` publishes a release,
so changes should land through pull requests.

## AI assistance

treehouse was built in collaboration with Claude (Anthropic's Claude Code): the
design and feature decisions are human, most of the code was written by the
model, and everything is human-reviewed, covered by the test suite, and
verified end-to-end before release. Commits carry `Co-Authored-By` trailers
reflecting this.

## License

[MIT](LICENSE)

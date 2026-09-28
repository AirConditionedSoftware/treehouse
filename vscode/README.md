# Treehouse for VS Code

Manage your git worktrees from the Activity Bar. Treehouse puts every worktree of every
open repository in one tree — with its branch, ahead/behind counts, change count and lock
state — and lets you create, open and remove worktrees without leaving the editor.

The extension is a thin client over the [`th`](https://github.com/AirConditionedSoftware/treehouse)
CLI. It never mutates git itself: every create and remove runs the same `th` command you
would type in a terminal, so your `config.json`, your `.thrc` and your hooks all apply
exactly as they do on the command line.

## Features

- **Worktrees view** — one entry per worktree, grouped by repository in multi-root
  workspaces, flat when a single repository is open. The current worktree is marked, dirty
  worktrees show a change count, locked and prunable ones are called out.
- **New Worktree from Branch…** — pick an existing local or remote branch, or type a new
  name; `th add` places it wherever your config says.
- **New Worktree from Pull Request…** — a PR number, `#number` or full GitHub URL, via
  `th add pr`.
- **Remove Worktree…** — with a confirmation modal, an optional "Also delete branch", and
  a clear report when `th` keeps an unmerged branch.
- **Open**, **Open in New Window**, **Copy Path**, **Reveal in Finder**, **Open in
  Integrated Terminal** on any worktree. A sibling `.code-workspace` file wins over the
  plain folder when there is one.

## Requirements

The `th` binary must be installed and on your `PATH`:

```sh
brew install AirConditionedSoftware/tap/treehouse
```

Other install options are in the [project README](https://github.com/AirConditionedSoftware/treehouse#install).

This extension **requires `th` ≥ 0.5.0**. With an older binary the tree still lists
worktrees, but the change count and merge status are unavailable and the mutating commands
warn once with an upgrade hint.

If `th` is not on `PATH`, point the `treehouse.path` setting at it.

## Settings

| Setting | Type | Default | Description |
|---|---|---|---|
| `treehouse.path` | string | `"th"` | Absolute path to the `th` binary, or a name on `PATH`. |
| `treehouse.openAfterCreate` | `newWindow` \| `currentWindow` \| `ask` \| `none` | `"ask"` | What to do after a worktree is created. |
| `treehouse.removeDefaultDeleteBranch` | boolean | `false` | Pre-check "Also delete branch" when removing. |
| `treehouse.refreshOnFocus` | boolean | `true` | Refresh the list when the window regains focus. |
| `treehouse.showRemoteBranches` | boolean | `true` | Seed the branch picker from `refs/remotes/origin` too. |
| `treehouse.commandTimeoutSeconds` | number | `180` | Timeout for one `th` invocation — `th add` may fetch. |

## Notes

- Worktrees created from this extension pass `--no-open` to `th`, so the extension —
  not your `vscode.open` config setting — decides whether and how a new worktree opens.
  Use `treehouse.openAfterCreate` for that.
- Run **Treehouse: Show Treehouse Log** to see every `th` invocation, its exit code and its
  stderr.
- **Windows is untested.** The extension spawns `th` without a shell and uses portable path
  handling, so it should work, but `th` itself has no Windows support story yet.

## License

MIT — see [LICENSE](./LICENSE).

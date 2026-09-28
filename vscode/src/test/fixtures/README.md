# Test fixtures

Real `th` output, captured against a throwaway git repository with a scratch
`TH_CONFIG`, then rewritten so no absolute path from the capturing machine
leaks in: the scratch root was replaced with `/repo`, making the main worktree
`/repo/myapp` and the worktree directory `/repo/worktrees/{repo}/{branch}`.
Nothing else was edited.

| file | captured from |
|---|---|
| `list.json` | `th list --json` — main + clean, dirty, merged, unmerged and locked worktrees |
| `remove-success.json` | `th remove --json spare-clean` (exit 0) |
| `remove-kept-not-merged.json` | `th remove --json -d spare-unmerged` (exit 0; branch kept) |
| `remove-missing.json` | `th remove --json done-work no-such-branch` (exit 1; the document is still printed) |
| `config-effective.json` | `th config --effective --json` with a `repos` entry and a `.thrc` |
| `stderr/th-error.txt` | `th remove nonexistent-branch` |
| `stderr/trust-warning.txt` | `th add trust-demo` with unapproved `.thrc` hooks and no TTY on stdin |

Hand-written, because they cannot be produced locally:

| file | source |
|---|---|
| `list-old-th.json` | `th list --json` as th < 0.5.0 printed it — the same shape without `dirty`/`merged` |
| `stderr/pr-gh-unavailable.txt` | the `fmt.Fprintf` at `internal/cmd/add_pr.go:54` |
| `stderr/pr-merged.txt` | the `fmt.Fprintf` at `internal/cmd/add_pr.go:56` |

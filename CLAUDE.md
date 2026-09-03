# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## What this is

**treehouse** (binary: `th`, module `github.com/AirConditionedSoftware/treehouse`) is a
Go CLI that manages git worktrees, placing them via a JSON config that can be
overridden per repository. Go 1.25+, cobra for commands, huh for interactive
prompts, and a real `git` binary on PATH. No other runtime dependencies.

The repo directory is `wt` but the project, module, and docs are all "treehouse".

## Commands

```sh
go build ./cmd/th   # produces ./th (gitignored)
go test ./...       # unit + end-to-end
go vet ./...
gofmt -l .          # must print nothing — CI fails on any output

# a single test / subtest
go test ./internal/config -run TestResolveLayers
go test ./internal/cmd -run 'TestEndToEnd/add_new_branch'   # spaces in subtest names become _
go test ./internal/cmd -run TestLifecycleHooks -v

goreleaser release --snapshot --clean   # validate the release config locally
```

CI (`.github/workflows/ci.yml`) runs gofmt, `go vet`, and `go test` on Linux and
macOS for every PR.

## Architecture

Four packages, one direction of dependency: `cmd/th` → `internal/cmd` →
`internal/{config,gitx,forge}`.

- **`internal/gitx`** — the only place that touches git. Everything shells out to
  the `git` binary; `git worktree list --porcelain` is parsed as the interface.
  Deliberately no go-git. `ListWorktrees` always returns the main worktree first,
  which is what anchors config resolution everywhere else.
- **`internal/config`** — loads and merges configuration, plus the schema-migration
  engine and the hook trust store.
- **`internal/forge`** — pull-request metadata for `th add pr`. `Forge` answers "what
  is this PR" and "which ref fetches its head"; running git stays with the caller.
  GitHub via the `gh` CLI, with a `refs/pull/<n>/head` fallback when gh is absent.
- **`internal/cmd`** — one file per cobra command, each registering itself on
  `rootCmd` in an `init()`. Shared helpers live alongside: `display.go`
  (`displayPath` home abbreviation), `color.go` (ANSI + tabwriter-safe styling),
  `format.go` (the two-line worktree rendering shared by list and the pickers),
  `hooks.go` (lifecycle hook execution and the trust gate), `progress.go`,
  `windowcolor.go`, `reflink_{darwin,linux,other}.go` (build-tagged CoW clone).

`th add` and `th refresh` share `provisionWorktree` in `add.go` — hooks copy,
`copy_files`/`link_files`, `post_create`, workspace file. Changes to provisioning
belong there so both commands stay in step.

## Conventions that matter

**stdout discipline.** stdout is machine output; everything human-facing goes to
stderr. `th add`/`th cd` print *only* the resolved path so `cd "$(th add x)"`
works; `th config` puts the JSON on stdout and its location on stderr so
`th config | jq` works; `th run` reserves nothing on stdout and adopts the child's
exit code via `exitCodeError`. A huh form in a command with a stdout contract is
built with `Form.WithOutput(os.Stderr)`: huh already renders there, but `TERM=dumb`
flips it into accessible mode, which would otherwise write the whole transcript to
the machine channel. Don't add stdout output to a command that doesn't already have
a machine contract.

**Four-layer config merge.** Built-in defaults ← global `config.json` top level ←
matching `repos` entry ← the repo's `.thrc`. Every setting lives in
`config.Settings` and merges field by field: empty strings fall through, set lists
and pointers replace the layer below. A new setting needs the field, a case in
`Settings.merge` (and `VSCode.merge` if nested), and a line in `Settings.setFields`
— `TestSetFieldsCoversEveryField` fails if you skip the last one. Use a pointer
type (`*bool`) whenever a per-repo `false` must override a global `true`.

**Config schema versioning is forward-only.** `config.json` and `.thrc` each carry
a `"version"` and their own registry (`migrations_global.go`, `migrations_local.go`);
a track's current version is derived as registry length + 1, so appending a step
bumps the version. To add one: write `internal/config/migrate_<track>_vN.go` with
`migrate<Track>VN(m map[string]any) (map[string]any, error)` — steps transform the
raw JSON map, never a typed struct, and never touch `"version"` — append
`{from: N-1, apply: migrate<Track>VN}` to the registry, and add a fixture test.
**Never edit or reorder a shipped step**: someone's file is partway down the chain.

The two files are migrated with different policies: the global config is rewritten
in place (backup first) whenever `Load` runs, while a `.thrc` is migrated *in memory*
and comes back as a `PendingMigration` — it belongs to the repository and may be
committed, so only interactive commands (`finalizeLocalMigration`) or `th migrate`
write it back. Read-only inspection (`PendingGlobalMigration`,
`PendingLocalMigration`) exists precisely so `th migrate --dry-run` doesn't rewrite
anything as a side effect; don't route it through `Load`/`Resolve`.

**Repo-supplied commands are gated.** Hook commands (`pre_create`, `post_create`,
`pre_remove`, `post_remove`) and `run` that came from a repo's `.thrc` — as opposed
to the user-owned global config — need approval, recorded per hook in
`~/.th/trust.json` (always there, never next to `$TH_CONFIG`: it's machine state,
not config). Approval is re-asked whenever the exact command list changes. The two
gates differ on purpose: an unapproved *hook* is skipped with a warning (a committed
`.thrc` must not be able to block removing a worktree), while an unapproved *run
command* is a hard error (it's the entire job; exiting 0 would fake success).

**Hook metadata never gets interpolated.** `TH_WORKTREE`, `TH_MAIN`, `TH_REPO`, and
`TH_BRANCH` reach `sh -c` commands as environment variables — branch names may
legally contain `$( )`.

**Interactive prompts need a non-TTY path.** Every huh form must have a fallback
that errors or warns clearly instead of hanging. Some checks are stdin-only; the
migration prompts also require stdout to be a TTY, because `cd "$(th cd b)"` has a
TTY stdin while the shell blocks on captured stdout. A `*bool` setting can never be
asked with `huh.NewConfirm`: nil means *inherit*, which is not false — `auto_cd` is
even true when unset — so ask it as a three-way `Select` whose first option names
the value that would be inherited and the layer it comes from.

**Shell integration.** A process can't cd its parent, so `th shell-init` prints a
wrapper that passes `TH_CD_FILE`; `writeCDFile` writes the destination there. An
effective VS Code open always beats auto-cd, even with an explicit `--cd`.

**Copy vs. link.** Files git tracks are never copied or linked over — the checkout
already placed them. Copies run before links so a copy can't write through a symlink
into the main worktree. `copyFile` tries a filesystem clone (APFS/Btrfs/XFS) and
falls back to streaming.

## Docs and releases

`README.md` is the reference documentation — update it whenever you add or change a
command, flag, or config field. `CONTRIBUTING.md` carries the same conventions in
short form.

Every push to `main` publishes a release: the workflow bumps the patch version when
`*.go`, `go.mod`, `go.sum`, or `.goreleaser.yaml` changed since the last tag
(docs-only pushes don't release), `#minor` in the commit message bumps the minor,
and a major bump means pushing a `v*` tag by hand. So changes land through PRs, and
a merge is a release.

Implementation plans live in the gitignored `.plans/<topic>.md`, one file per topic.

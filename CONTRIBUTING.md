# Contributing to treehouse

Thanks for taking an interest — issues and pull requests are welcome.

## Development setup

Go 1.25+ and git are the only requirements.

```sh
go build ./cmd/th  # produces ./th
go test ./...     # unit + end-to-end tests
go vet ./...
gofmt -l .        # should print nothing
```

The end-to-end tests (`cmd/e2e_test.go`) build the binary and drive it
against scratch repositories in temp directories — they need a real `git` on
PATH but never touch your own repositories or config.

## Project layout

- `cmd/` — cobra commands (`add`, `list`, `remove`, `open`, `prune`, `du`,
  `config`)
- `internal/gitx/` — thin wrapper that shells out to git and parses
  `git worktree list --porcelain`
- `internal/config/` — th.json loading, per-repo resolution, path templating

Conventions worth keeping:

- All git operations shell out to the `git` binary; porcelain output is the
  interface. No go-git.
- stdout discipline: `th add` prints only the created path on stdout so
  `cd "$(th add x)"` works; everything else goes to stderr.
- Config fields merge in three layers (built-in defaults ← top-level ←
  matching `repos` entry). New settings should follow the existing merge
  pattern — use a pointer type when a per-repo `false` must override a
  global `true`.
- A new or renamed config field also belongs in the JSON Schemas
  `internal/config/thrc.schema.json` and `internal/config/config.schema.json`
  (with a `description` — it's the hover text editors show); a sync test in
  `internal/config` compares the structs against both schemas and fails if
  they drift apart.
- Config schema changes are versioned, not breaking. `config.json` and
  `.thrc` each carry a `"version"` and their own registry of forward-only
  migration steps (`internal/config/migrations_global.go` and
  `migrations_local.go`); a track's current version is derived as its
  registry length + 1, so appending a step bumps the version. To add one:
  write `internal/config/migrate_<track>_vN.go` holding
  `migrate<Track>VN(m map[string]any) (map[string]any, error)` — steps
  transform the raw JSON map, never a typed struct, and leave `"version"`
  alone — append a single `{from: N-1, apply: migrate<Track>VN}` line to
  that track's registry, and add a fixture test that runs an old file
  through it. Never edit or reorder a shipped step: migrations are
  forward-only history, and someone's file is partway down the chain.
- Interactive prompts (huh) need a non-TTY fallback that errors clearly
  instead of hanging.

## Pull requests

- CI runs gofmt, `go vet`, and the tests on Linux and macOS; keep them
  green.
- Every push to `main` automatically publishes a patch release, so changes
  land through PRs and get released on merge.
- Update `README.md` when you add or change commands, flags, or config
  fields — the README is the reference documentation.

## Releases

Handled by GitHub Actions + goreleaser: merges to `main` auto-increment the
patch version; maintainers push a `v*` tag manually for a minor or major
bump. See the Releasing section of the README.

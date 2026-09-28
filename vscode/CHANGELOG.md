# Changelog

All notable changes to the Treehouse VS Code extension are documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [0.1.0] - Unreleased

### Added

- Worktrees view in the Activity Bar, listing every worktree of every open repository —
  branch, ahead/behind, change count, merge status, lock and prunable state.
- Multi-root support: one node per repository, deduped by main-worktree path; flat when a
  single repository is open.
- Commands: Refresh, New Worktree from Branch…, New Worktree from Pull Request…, Remove
  Worktree…, Open, Open in New Window, Copy Path, Reveal in Finder, Open in Integrated
  Terminal, Show Treehouse Log, Open Treehouse Settings.
- Settings: `treehouse.path`, `treehouse.openAfterCreate`,
  `treehouse.removeDefaultDeleteBranch`, `treehouse.refreshOnFocus`,
  `treehouse.showRemoteBranches`, `treehouse.commandTimeoutSeconds`.
- Automatic refresh on window focus, workspace-folder changes, and filesystem changes under
  the repository's `worktrees` directory — so `git worktree add` from a terminal shows up.

[0.1.0]: https://github.com/AirConditionedSoftware/treehouse/releases

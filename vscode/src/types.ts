/**
 * TypeScript mirrors of the Go shapes `th` prints with `--json`.
 * One interface per Go struct; the comment names the Go source.
 */

/** mirrors internal/cmd/list.go listEntry + internal/gitx/worktree.go Worktree */
export interface WorktreeEntry {
  path: string;
  head?: string;
  branch?: string;
  bare?: boolean;
  detached?: boolean;
  locked?: boolean;
  locked_reason?: string;
  prunable?: boolean;
  prunable_reason?: string;
  ahead?: number;
  behind?: number;
  upstream_gone?: boolean;
  /** Phase 0.2; absent on th < 0.5.0 or when not probed. */
  dirty?: number;
  /** Phase 0.2; absent when it does not apply (bare, detached, the default branch). */
  merged?: boolean;
}

/** mirrors internal/cmd/remove.go removeResult */
export interface RemoveResult {
  target: string;
  path?: string;
  branch?: string;
  removed: boolean;
  skipped_dirty?: boolean;
  branch_action?: 'deleted' | 'kept';
  branch_reason?: 'default_branch' | 'not_fully_merged' | 'declined' | 'delete_failed' | 'forced';
  branch_error?: string;
  workspace_file?: string;
  error?: string;
}

/** mirrors internal/config/config.go VSCode */
export interface EffectiveVSCode {
  open: boolean;
  workspace_file: boolean;
  workspace_prefix: string;
  window_title: string;
  window_color: string;
  workspace_paths: string[];
}

/** mirrors internal/config/config.go Settings, with every pointer resolved */
export interface EffectiveSettings {
  worktree_dir: string;
  default_base: string;
  branch_prefix: string;
  prefix_separator: string;
  copy_hooks: boolean;
  copy_files: string[];
  link_files: string[];
  vscode: EffectiveVSCode;
  full_paths: boolean;
  auto_cd: boolean;
  pre_create: string[];
  post_create: string[];
  pre_remove: string[];
  post_remove: string[];
  run: string;
}

/** mirrors the `config --effective --json` document (Phase 0.4) */
export interface EffectiveConfig {
  config_file: { path: string; exists: boolean; from_env: boolean };
  repo?: {
    name: string;
    main_path: string;
    local_file?: string;
    repos_index?: number;
    repos_path?: string;
  };
  settings: EffectiveSettings;
  sources: Record<string, string>;
}

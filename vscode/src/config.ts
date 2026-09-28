import * as fs from 'fs/promises';
import * as os from 'os';
import * as path from 'path';
import type { EffectiveConfig } from './types';

/**
 * Configuration the extension reads back out of `th`, plus the two small pure
 * mirrors of `th`'s own path logic that the UI needs before a worktree exists.
 *
 * Nothing here imports `vscode`: these functions are the parsing seams Phase 3
 * unit-tests, and the cache takes its loader as a function so it stays testable
 * too.
 */

/** `internal/cmd/add.go` writes the workspace file with this extension. */
const WORKSPACE_SUFFIX = '.code-workspace';

/** mirrors config.SanitizeBranch (internal/config/config.go) */
export function sanitizeBranch(branch: string): string {
  return branch.split('/').join('-');
}

/** mirrors config.ExpandTilde (internal/config/config.go): only a leading `~` or `~/`. */
export function expandTilde(p: string, home: string = os.homedir()): string {
  if (p === '~') {
    return home;
  }
  if (p.startsWith('~/') || (path.sep === '\\' && p.startsWith('~\\'))) {
    return path.join(home, p.slice(2));
  }
  return p;
}

/** filepath.Clean's shape: normalized, no trailing separator, "" becomes ".". */
function cleanPath(p: string): string {
  if (p.length === 0) {
    return '.';
  }
  const normalized = path.normalize(p);
  if (normalized.length > 1 && normalized.endsWith(path.sep)) {
    return normalized.slice(0, -1);
  }
  return normalized;
}

/**
 * Where `th add` would put a worktree — a mirror of `Settings.WorktreePath`
 * (internal/config/config.go:647). `{repo}` goes in verbatim, `{branch}` is
 * sanitized, then `~` expands and the result is cleaned.
 *
 * Preview only: `th` computes the real target itself, and `--path` overrides it.
 */
export function predictTarget(template: string, repo: string, branch: string, home: string = os.homedir()): string {
  // split/join, not replaceAll: a repo or branch name containing `$&` would be
  // reinterpreted as a replacement pattern.
  let p = template.split('{repo}').join(repo);
  p = p.split('{branch}').join(sanitizeBranch(branch));
  return cleanPath(expandTilde(p, home));
}

/**
 * mirrors Settings.EffectivePrefix (internal/config/config.go:635): the branch
 * prefix with its separator applied, never doubled. `th add` applies it to
 * branches that don't exist yet.
 */
export function effectivePrefix(branchPrefix: string, prefixSeparator: string): string {
  if (!branchPrefix) {
    return '';
  }
  const sep = prefixSeparator || '/';
  const trimmed = branchPrefix.endsWith(sep) ? branchPrefix.slice(0, -sep.length) : branchPrefix;
  return trimmed + sep;
}

/**
 * The `th`-generated `.code-workspace` for a worktree, if it exists. It is a
 * *sibling* of the worktree directory named
 * `<vscode.workspace_prefix><sanitized branch>.code-workspace`
 * (`workspaceFilePath`, internal/cmd/add.go:320) — matching on the suffix means
 * the prefix needs no config lookup (plan §0.4).
 */
export async function findWorkspaceFile(worktreePath: string, branch: string | undefined): Promise<string | undefined> {
  if (!branch) {
    return undefined;
  }
  const suffix = sanitizeBranch(branch) + WORKSPACE_SUFFIX;
  const dir = path.dirname(worktreePath);
  let names: string[];
  try {
    const entries = await fs.readdir(dir, { withFileTypes: true });
    names = entries.filter((entry) => !entry.isDirectory() && entry.name.endsWith(suffix)).map((entry) => entry.name);
  } catch {
    return undefined;
  }
  if (names.length === 0) {
    return undefined;
  }
  // An unprefixed file is the exact name; otherwise take the shortest match, so
  // a longer branch ending in the same text can't shadow it.
  names.sort((a, b) => a.length - b.length || a.localeCompare(b));
  const chosen = names.includes(suffix) ? suffix : names[0];
  return chosen ? path.join(dir, chosen) : undefined;
}

/** What the cache needs to identify and locate a repository. */
export interface RepoRef {
  key: string;
  cwd: string;
}

export type EffectiveConfigLoader = (cwd: string) => Promise<EffectiveConfig>;

function isEffectiveConfig(value: unknown): value is EffectiveConfig {
  if (!value || typeof value !== 'object') {
    return false;
  }
  const settings = (value as { settings?: unknown }).settings;
  return !!settings && typeof settings === 'object' && typeof (settings as { worktree_dir?: unknown }).worktree_dir === 'string';
}

/**
 * `th config --effective --json` per repo, so a picker can preview the target
 * path without re-forking `th` on every keystroke.
 *
 * The whole thing is severable (plan §0.4): an older `th` rejects the flag with
 * `unknown flag: --json`, which surfaces here as `undefined` and costs only the
 * preview.
 */
export class EffectiveConfigCache {
  private readonly entries = new Map<string, EffectiveConfig | undefined>();
  private readonly inflight = new Map<string, Promise<EffectiveConfig | undefined>>();

  constructor(
    private readonly load: EffectiveConfigLoader,
    private readonly log: (message: string) => void = () => {},
  ) {}

  /** Drop the cache — one repo's entry, or everything. */
  clear(key?: string): void {
    if (key) {
      this.entries.delete(key);
      this.inflight.delete(key);
      return;
    }
    this.entries.clear();
    this.inflight.clear();
  }

  /** The repo's effective config, or undefined when `th` can't produce it. */
  async get(repo: RepoRef): Promise<EffectiveConfig | undefined> {
    if (this.entries.has(repo.key)) {
      return this.entries.get(repo.key);
    }
    const pending = this.inflight.get(repo.key);
    if (pending) {
      return await pending;
    }
    const promise = this.fetch(repo);
    this.inflight.set(repo.key, promise);
    try {
      return await promise;
    } finally {
      this.inflight.delete(repo.key);
    }
  }

  private async fetch(repo: RepoRef): Promise<EffectiveConfig | undefined> {
    let value: EffectiveConfig | undefined;
    try {
      const loaded = await this.load(repo.cwd);
      value = isEffectiveConfig(loaded) ? loaded : undefined;
    } catch (err) {
      this.log(`  effective config unavailable: ${err instanceof Error ? err.message : String(err)}`);
      value = undefined;
    }
    this.entries.set(repo.key, value);
    return value;
  }
}

import * as fs from 'fs/promises';
import * as os from 'os';
import * as path from 'path';
import * as vscode from 'vscode';
import { gitCommonDir, toplevel } from './git';
import { logLine } from './log';
import { Th, ThMissingError } from './th';
import type { WorktreeEntry } from './types';

export interface RepoNode {
  kind: 'repo';
  /** Stable identity: the realpath of the main worktree. */
  key: string;
  name: string;
  /** Main worktree path as git reports it (not realpath-ed). */
  mainPath: string;
  /** Absolute `.git` common dir — the filesystem-watcher anchor. */
  commonDir: string;
  /** Working directory for `th` invocations about this repo. */
  cwd: string;
}

export interface WorktreeNode {
  kind: 'worktree';
  repo: RepoNode;
  entry: WorktreeEntry;
  isMain: boolean;
  isCurrent: boolean;
  removable: boolean;
}

export type TreeNode = RepoNode | WorktreeNode;

export interface TreeState {
  repoCount: number;
  worktreeCount: number;
  /** Set when a `th` invocation failed; `missing` means the binary is not there. */
  error?: 'missing' | 'failed';
}

interface ListRun {
  cts: vscode.CancellationTokenSource;
  promise: Promise<WorktreeEntry[]>;
}

function displayPath(p: string): string {
  const home = os.homedir();
  if (home && (p === home || p.startsWith(home + path.sep))) {
    return '~' + p.slice(home.length);
  }
  return p;
}

async function realpathOrSelf(p: string): Promise<string> {
  try {
    return await fs.realpath(p);
  } catch {
    return p;
  }
}

function isWithin(child: string, parent: string): boolean {
  if (child === parent) {
    return true;
  }
  const rel = path.relative(parent, child);
  return rel.length > 0 && !rel.startsWith('..') && !path.isAbsolute(rel);
}

export class WorktreeTreeProvider implements vscode.TreeDataProvider<TreeNode>, vscode.Disposable {
  private readonly changeEmitter = new vscode.EventEmitter<TreeNode | undefined>();
  readonly onDidChangeTreeData = this.changeEmitter.event;

  private readonly stateEmitter = new vscode.EventEmitter<TreeState>();
  readonly onDidChangeState = this.stateEmitter.event;

  private reposPromise: Promise<RepoNode[]> | undefined;
  private readonly inflight = new Map<string, ListRun>();
  private readonly worktreesByRepo = new Map<string, WorktreeNode[]>();
  private lastError: 'missing' | 'failed' | undefined;

  constructor(private readonly th: Th) {}

  dispose(): void {
    for (const run of this.inflight.values()) {
      run.cts.cancel();
      run.cts.dispose();
    }
    this.inflight.clear();
    this.changeEmitter.dispose();
    this.stateEmitter.dispose();
  }

  /** Re-read one repo, or everything (which also rediscovers the repo set). */
  refresh(repo?: RepoNode): void {
    if (!repo) {
      this.reposPromise = undefined;
      this.worktreesByRepo.clear();
    } else {
      this.worktreesByRepo.delete(repo.key);
    }
    this.lastError = undefined;
    this.changeEmitter.fire(undefined);
  }

  /** The repos discovered on the last render. */
  async repos(): Promise<RepoNode[]> {
    if (!this.reposPromise) {
      this.reposPromise = this.discoverRepos();
    }
    return await this.reposPromise;
  }

  /** The worktree nodes rendered for a repo on the last render, if any. */
  nodesFor(repo: RepoNode): WorktreeNode[] {
    return this.worktreesByRepo.get(repo.key) ?? [];
  }

  async getChildren(element?: TreeNode): Promise<TreeNode[]> {
    if (element && element.kind === 'worktree') {
      return [];
    }
    const repos = await this.repos();
    if (element) {
      return await this.worktreeNodes(element);
    }
    if (repos.length === 0) {
      this.publishState(0, 0);
      return [];
    }
    // A single repository renders flat — no group node to expand.
    const only = repos[0];
    if (repos.length === 1 && only) {
      const nodes = await this.worktreeNodes(only);
      this.publishState(1, nodes.length);
      return nodes;
    }
    const counts = await Promise.all(repos.map(async (repo) => (await this.worktreeNodes(repo)).length));
    this.publishState(
      repos.length,
      counts.reduce((sum, n) => sum + n, 0),
    );
    return repos;
  }

  getParent(element: TreeNode): TreeNode | undefined {
    if (element.kind === 'worktree') {
      return element.repo;
    }
    return undefined;
  }

  getTreeItem(element: TreeNode): vscode.TreeItem {
    return element.kind === 'repo' ? this.repoItem(element) : this.worktreeItem(element);
  }

  private repoItem(repo: RepoNode): vscode.TreeItem {
    const item = new vscode.TreeItem(repo.name, vscode.TreeItemCollapsibleState.Expanded);
    item.id = `repo:${repo.key}`;
    item.contextValue = 'repo';
    item.iconPath = new vscode.ThemeIcon('repo');
    item.description = displayPath(repo.mainPath);
    item.resourceUri = vscode.Uri.file(repo.mainPath);
    item.tooltip = new vscode.MarkdownString(`**${repo.name}**\n\n\`${repo.mainPath}\``);
    return item;
  }

  private worktreeItem(node: WorktreeNode): vscode.TreeItem {
    const { entry } = node;
    const item = new vscode.TreeItem(path.basename(entry.path) || entry.path);
    item.id = `wt:${node.repo.key}:${entry.path}`;
    item.collapsibleState = vscode.TreeItemCollapsibleState.None;
    item.resourceUri = vscode.Uri.file(entry.path);
    item.description = describe(entry, node.isMain);
    item.tooltip = tooltip(node);
    item.iconPath = icon(node);
    item.contextValue = contextValue(node);
    item.command = {
      command: 'treehouse.openWorktree',
      title: 'Open',
      arguments: [node],
    };
    return item;
  }

  private async worktreeNodes(element: TreeNode): Promise<WorktreeNode[]> {
    const repo = element.kind === 'repo' ? element : element.repo;
    let entries: WorktreeEntry[];
    try {
      entries = await this.listEntries(repo);
      this.lastError = undefined;
    } catch (err) {
      if (err instanceof vscode.CancellationError) {
        return this.worktreesByRepo.get(repo.key) ?? [];
      }
      this.lastError = err instanceof ThMissingError ? 'missing' : 'failed';
      logLine(`  listing worktrees for ${repo.name} failed: ${err instanceof Error ? err.message : String(err)}`);
      this.worktreesByRepo.set(repo.key, []);
      return [];
    }

    const folders = await Promise.all(
      (vscode.workspace.workspaceFolders ?? []).map(async (folder) => await realpathOrSelf(folder.uri.fsPath)),
    );
    const nodes: WorktreeNode[] = [];
    for (const [index, entry] of entries.entries()) {
      // gitx.ListWorktrees always returns the main worktree first.
      const isMain = index === 0;
      const real = await realpathOrSelf(entry.path);
      const isCurrent = folders.some((folder) => isWithin(folder, real));
      nodes.push({
        kind: 'worktree',
        repo,
        entry,
        isMain,
        isCurrent,
        removable: !isMain && !isCurrent,
      });
    }
    this.worktreesByRepo.set(repo.key, nodes);
    return nodes;
  }

  /** One in-flight `th list` per repo; a later request supersedes an earlier one. */
  private async listEntries(repo: RepoNode): Promise<WorktreeEntry[]> {
    const previous = this.inflight.get(repo.key);
    if (previous) {
      previous.cts.cancel();
    }
    const cts = new vscode.CancellationTokenSource();
    const run: ListRun = { cts, promise: this.th.list(repo.cwd, cts.token) };
    this.inflight.set(repo.key, run);
    try {
      return await run.promise;
    } catch (err) {
      const current = this.inflight.get(repo.key);
      if (current && current !== run) {
        // Superseded — adopt the newer listing rather than surfacing the cancellation.
        return await current.promise.catch(() => [] as WorktreeEntry[]);
      }
      throw err;
    } finally {
      if (this.inflight.get(repo.key) === run) {
        this.inflight.delete(repo.key);
      }
      cts.dispose();
    }
  }

  private async discoverRepos(): Promise<RepoNode[]> {
    const folders = vscode.workspace.workspaceFolders ?? [];
    const byKey = new Map<string, RepoNode>();
    for (const folder of folders) {
      if (folder.uri.scheme !== 'file') {
        continue;
      }
      const cwd = folder.uri.fsPath;
      const top = await toplevel(cwd);
      if (!top) {
        continue;
      }
      const commonDir = await gitCommonDir(cwd);
      if (!commonDir) {
        continue;
      }
      const mainPath = mainWorktreeFor(commonDir);
      const key = await realpathOrSelf(mainPath);
      if (byKey.has(key)) {
        continue;
      }
      byKey.set(key, {
        kind: 'repo',
        key,
        name: path.basename(mainPath) || mainPath,
        mainPath,
        commonDir,
        cwd,
      });
    }
    return [...byKey.values()].sort((a, b) => a.name.localeCompare(b.name));
  }

  private publishState(repoCount: number, worktreeCount: number): void {
    const state: TreeState = { repoCount, worktreeCount };
    if (this.lastError) {
      state.error = this.lastError;
    }
    this.stateEmitter.fire(state);
  }
}

/** `<main>/.git` -> `<main>`; a bare repo's common dir is the repo itself. */
export function mainWorktreeFor(commonDir: string): string {
  const normalized = path.normalize(commonDir.replace(/[\\/]+$/, ''));
  return path.basename(normalized) === '.git' ? path.dirname(normalized) : normalized;
}

/** `worktree` plus `.`-joined tokens in a fixed order. */
export function contextValue(node: WorktreeNode): string {
  const tokens = ['worktree'];
  if (node.isMain) {
    tokens.push('main');
  }
  if (node.isCurrent) {
    tokens.push('current');
  }
  if (node.removable) {
    tokens.push('removable');
  }
  if ((node.entry.dirty ?? 0) > 0) {
    tokens.push('dirty');
  }
  if (node.entry.locked) {
    tokens.push('locked');
  }
  if (node.entry.prunable) {
    tokens.push('prunable');
  }
  if (node.entry.detached) {
    tokens.push('detached');
  }
  return tokens.join('.');
}

/** The one-line facts `th list` shows, in the same order. */
export function describe(entry: WorktreeEntry, isMain: boolean): string {
  const parts: string[] = [];
  if (entry.bare) {
    parts.push('(bare)');
  } else if (entry.detached) {
    parts.push(`(detached ${shortHead(entry.head)})`);
  } else if (entry.branch) {
    parts.push(`[${entry.branch}]`);
  }
  if (isMain) {
    parts.push('main');
  }
  if (entry.upstream_gone) {
    parts.push('upstream gone');
  } else {
    const sync: string[] = [];
    if (entry.ahead) {
      sync.push(`↑${entry.ahead}`);
    }
    if (entry.behind) {
      sync.push(`↓${entry.behind}`);
    }
    if (sync.length > 0) {
      parts.push(sync.join(' '));
    }
  }
  // `dirty` and `merged` are absent on th < 0.5.0 — say nothing rather than guess.
  if (typeof entry.dirty === 'number' && entry.dirty > 0) {
    parts.push(`${entry.dirty} change${entry.dirty === 1 ? '' : 's'}`);
  }
  if (entry.merged === true) {
    parts.push('merged');
  }
  if (entry.locked) {
    parts.push('locked');
  }
  if (entry.prunable) {
    parts.push('prunable');
  }
  return parts.join('  ');
}

function shortHead(head: string | undefined): string {
  return head ? head.slice(0, 7) : '';
}

function tooltip(node: WorktreeNode): vscode.MarkdownString {
  const { entry } = node;
  const lines: string[] = [`\`${entry.path}\``, ''];
  if (entry.branch) {
    lines.push(`- branch: \`${entry.branch}\``);
  }
  if (entry.detached) {
    lines.push('- detached HEAD');
  }
  if (entry.head) {
    lines.push(`- head: \`${shortHead(entry.head)}\``);
  }
  if (node.isMain) {
    lines.push('- main worktree');
  }
  if (node.isCurrent) {
    lines.push('- current worktree');
  }
  if (entry.upstream_gone) {
    lines.push('- upstream gone');
  } else if (entry.ahead || entry.behind) {
    lines.push(`- ahead ${entry.ahead ?? 0}, behind ${entry.behind ?? 0}`);
  }
  if (typeof entry.dirty === 'number') {
    lines.push(entry.dirty === 0 ? '- clean' : `- ${entry.dirty} uncommitted change${entry.dirty === 1 ? '' : 's'}`);
  }
  if (typeof entry.merged === 'boolean') {
    lines.push(entry.merged ? '- merged into the default branch' : '- not fully merged');
  }
  if (entry.locked) {
    lines.push(`- locked${entry.locked_reason ? `: ${entry.locked_reason}` : ''}`);
  }
  if (entry.prunable) {
    lines.push(`- prunable${entry.prunable_reason ? `: ${entry.prunable_reason}` : ''}`);
  }
  const markdown = new vscode.MarkdownString(lines.join('\n'));
  markdown.supportThemeIcons = true;
  return markdown;
}

function icon(node: WorktreeNode): vscode.ThemeIcon {
  if (node.entry.locked) {
    return new vscode.ThemeIcon('lock');
  }
  if (node.entry.prunable) {
    return new vscode.ThemeIcon('warning');
  }
  if (node.isCurrent) {
    return new vscode.ThemeIcon('check', new vscode.ThemeColor('charts.green'));
  }
  return new vscode.ThemeIcon('git-branch');
}

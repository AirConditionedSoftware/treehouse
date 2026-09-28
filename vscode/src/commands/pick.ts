import * as vscode from 'vscode';
import type { RepoNode, TreeNode, WorktreeNode } from '../worktreeTree';
import type { CommandContext } from './index';

export interface PickWorktreeOptions {
  /** Only offer worktrees the provider marked removable (not main, not current). */
  removableOnly?: boolean;
  placeHolder?: string;
}

/** Every worktree node the tree currently knows about, across all repos. */
export async function allWorktreeNodes(ctx: CommandContext): Promise<WorktreeNode[]> {
  const top = (await ctx.provider.getChildren(undefined)) ?? [];
  const out: WorktreeNode[] = [];
  for (const node of top) {
    if (node.kind === 'worktree') {
      out.push(node);
    } else {
      const children = (await ctx.provider.getChildren(node)) ?? [];
      for (const child of children) {
        if (child.kind === 'worktree') {
          out.push(child);
        }
      }
    }
  }
  return out;
}

/** The repo a command should act on: the node's repo, else the single repo, else a pick. */
export async function resolveRepo(ctx: CommandContext, node?: TreeNode): Promise<RepoNode | undefined> {
  if (node?.kind === 'repo') {
    return node;
  }
  if (node?.kind === 'worktree') {
    return node.repo;
  }
  const worktrees = await allWorktreeNodes(ctx);
  const repos = new Map<string, RepoNode>();
  for (const wt of worktrees) {
    repos.set(wt.repo.key, wt.repo);
  }
  if (repos.size === 0) {
    void vscode.window.showWarningMessage('Treehouse: open a folder inside a git repository first.');
    return undefined;
  }
  if (repos.size === 1) {
    return [...repos.values()][0];
  }
  const picked = await vscode.window.showQuickPick(
    [...repos.values()].map((repo) => ({ label: repo.name, description: repo.mainPath, repo })),
    { placeHolder: 'Select a repository' },
  );
  return picked?.repo;
}

/**
 * Resolve the worktree a command should act on. A node passed by the tree wins;
 * from the palette the user picks one. Returns undefined when cancelled or empty.
 */
export async function pickWorktreeNode(
  ctx: CommandContext,
  node: TreeNode | undefined,
  options: PickWorktreeOptions = {},
): Promise<WorktreeNode | undefined> {
  if (node?.kind === 'worktree' && (!options.removableOnly || node.removable)) {
    return node;
  }
  let candidates = await allWorktreeNodes(ctx);
  if (options.removableOnly) {
    candidates = candidates.filter((wt) => wt.removable);
  }
  if (candidates.length === 0) {
    void vscode.window.showInformationMessage('Treehouse: no worktrees to choose from.');
    return undefined;
  }
  const items = candidates.map((wt) => ({
    label: wt.entry.branch ?? (wt.entry.detached ? '(detached)' : wt.entry.path),
    description: wt.entry.path,
    detail: candidates.some((c) => c.repo.key !== wt.repo.key) ? wt.repo.name : undefined,
    wt,
  }));
  const picked = await vscode.window.showQuickPick(items, {
    placeHolder: options.placeHolder ?? 'Select a worktree',
    matchOnDescription: true,
  });
  return picked?.wt;
}

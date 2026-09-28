import * as path from 'path';
import * as vscode from 'vscode';
import { findWorkspaceFile } from '../config';
import { logLine } from '../log';
import type { TreeNode } from '../worktreeTree';
import type { CommandContext } from './index';
import { pickWorktreeNode } from './pick';

export interface OpenOptions {
  forceNewWindow?: boolean;
}

/** `treehouse.openWorktree` / `treehouse.openWorktreeInNewWindow`. */
export async function openWorktree(ctx: CommandContext, node?: TreeNode, options: OpenOptions = {}): Promise<void> {
  const worktree = await pickWorktreeNode(ctx, node, { placeHolder: 'Select a worktree to open' });
  if (!worktree) {
    return;
  }
  await openPath(worktree.entry.path, worktree.entry.branch, options.forceNewWindow ? 'newWindow' : 'currentWindow');
}

/**
 * Open a worktree, preferring its `th`-generated `.code-workspace` sibling over
 * the plain folder — the same preference `openTargetFor` encodes at
 * internal/cmd/open.go:67.
 *
 * Opening in the **current** window restarts the extension host: nothing queued
 * after this call is guaranteed to run, so it must be the last action of any
 * command (refresh first, open last).
 */
export async function openPath(
  worktreePath: string,
  branch: string | undefined,
  mode: 'newWindow' | 'currentWindow',
): Promise<void> {
  // Without a branch (detached, or a fresh `th add pr` whose branch th chose),
  // the directory name is the sanitized branch, which is all the lookup needs.
  const hint = branch && branch.length > 0 ? branch : path.basename(worktreePath);
  const target = (await findWorkspaceFile(worktreePath, hint)) ?? worktreePath;

  if (mode === 'currentWindow' && alreadyOpen(worktreePath, target)) {
    logLine(`  ${target} is already open in this window.`);
    return;
  }

  logLine(`  opening ${target} (${mode})`);
  await vscode.commands.executeCommand('vscode.openFolder', vscode.Uri.file(target), {
    forceNewWindow: mode === 'newWindow',
  });
}

/** Reloading the window onto what it already shows is pure disruption. */
function alreadyOpen(worktreePath: string, target: string): boolean {
  if (vscode.workspace.workspaceFile?.fsPath === target) {
    return true;
  }
  const folders = vscode.workspace.workspaceFolders ?? [];
  return folders.length === 1 && folders[0]?.uri.fsPath === worktreePath && target === worktreePath;
}

import * as path from 'path';
import * as vscode from 'vscode';
import type { TreeNode, WorktreeNode } from '../worktreeTree';
import type { CommandContext } from './index';
import { pickWorktreeNode } from './pick';

const STATUS_MESSAGE_MS = 3000;

/** Copy a worktree's absolute path to the clipboard. */
export async function copyPath(ctx: CommandContext, node?: TreeNode): Promise<void> {
  const target = await pickWorktreeNode(ctx, node);
  if (!target) {
    return;
  }
  await vscode.env.clipboard.writeText(target.entry.path);
  vscode.window.setStatusBarMessage(`Treehouse: copied ${target.entry.path}`, STATUS_MESSAGE_MS);
}

/** Show the worktree directory in the OS file manager. */
export async function revealInOS(ctx: CommandContext, node?: TreeNode): Promise<void> {
  const target = await pickWorktreeNode(ctx, node);
  if (!target) {
    return;
  }
  await vscode.commands.executeCommand('revealFileInOS', vscode.Uri.file(target.entry.path));
}

/** Open an integrated terminal rooted in the worktree. */
export async function openInTerminal(ctx: CommandContext, node?: TreeNode): Promise<void> {
  const target = await pickWorktreeNode(ctx, node);
  if (!target) {
    return;
  }
  vscode.window.createTerminal({ cwd: target.entry.path, name: terminalName(target) }).show();
}

function terminalName(node: WorktreeNode): string {
  return path.basename(node.entry.path) || node.entry.path;
}

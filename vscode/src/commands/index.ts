import * as vscode from 'vscode';
import { EffectiveConfigCache } from '../config';
import { logLine, showLog } from '../log';
import type { RefreshController } from '../refresh';
import type { Th } from '../th';
import type { TreeNode, WorktreeTreeProvider } from '../worktreeTree';
import { addWorktree } from './add';
import { addWorktreeFromPR } from './addPr';
import { copyPath, openInTerminal, revealInOS } from './misc';
import { openWorktree } from './open';
import { removeWorktree } from './remove';

/** Everything a command implementation may need. */
export interface CommandContext {
  th: Th;
  provider: WorktreeTreeProvider;
  refresh: RefreshController;
  treeView: vscode.TreeView<TreeNode>;
  config: EffectiveConfigCache;
}

/** What `activate` builds; the config cache belongs to the command layer. */
export type CommandContextInput = Omit<CommandContext, 'config'>;

/** Commands invoked from the view are handed the clicked node. */
type Handler = (node?: TreeNode) => void | Promise<void>;

export function registerCommands(context: vscode.ExtensionContext, input: CommandContextInput): void {
  const ctx: CommandContext = {
    ...input,
    config: new EffectiveConfigCache(async (cwd) => await input.th.effectiveConfig(cwd), logLine),
  };

  const handlers: Record<string, Handler> = {
    'treehouse.refresh': () => {
      ctx.refresh.refreshNow();
    },
    'treehouse.showOutput': () => {
      showLog();
    },
    'treehouse.openSettings': () => {
      void vscode.commands.executeCommand('workbench.action.openSettings', '@ext:AirConditionedSoftware.treehouse');
    },
    'treehouse.addWorktree': async (node) => await addWorktree(ctx, node),
    'treehouse.addWorktreeFromPR': async (node) => await addWorktreeFromPR(ctx, node),
    'treehouse.removeWorktree': async (node) => await removeWorktree(ctx, node),
    'treehouse.openWorktree': async (node) => await openWorktree(ctx, node),
    'treehouse.openWorktreeInNewWindow': async (node) => await openWorktree(ctx, node, { forceNewWindow: true }),
    'treehouse.copyPath': async (node) => await copyPath(ctx, node),
    'treehouse.revealInOS': async (node) => await revealInOS(ctx, node),
    'treehouse.openInTerminal': async (node) => await openInTerminal(ctx, node),
  };

  for (const [id, handler] of Object.entries(handlers)) {
    context.subscriptions.push(
      // The returned promise is what `executeCommand` awaits, so it must settle
      // — and never reject, or the rejection is unhandled.
      vscode.commands.registerCommand(id, async (...args: unknown[]): Promise<void> => {
        // No refresh hook to hang invalidation on, so the effective config is
        // re-read once per command rather than served stale.
        ctx.config.clear();
        try {
          await handler(asTreeNode(args[0]));
        } catch (err) {
          await reportError(id, err);
        }
      }),
    );
  }
}

/** The view passes a `TreeNode`; the palette passes nothing. Trust neither. */
function asTreeNode(value: unknown): TreeNode | undefined {
  if (!value || typeof value !== 'object' || !('kind' in value)) {
    return undefined;
  }
  const kind = (value as { kind: unknown }).kind;
  return kind === 'repo' || kind === 'worktree' ? (value as TreeNode) : undefined;
}

/** A cancelled command is not a failure; everything else gets the log offered. */
async function reportError(id: string, err: unknown): Promise<void> {
  if (err instanceof vscode.CancellationError) {
    logLine(`  ${id} cancelled`);
    return;
  }
  const message = err instanceof Error ? err.message : String(err);
  logLine(`  ${id} failed: ${message}`);
  const choice = await vscode.window.showErrorMessage(`Treehouse: ${message}`, 'Show Log');
  if (choice === 'Show Log') {
    showLog();
  }
}

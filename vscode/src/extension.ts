import * as vscode from 'vscode';
import { registerCommands } from './commands';
import { disposeLog, log, logLine } from './log';
import { RefreshController } from './refresh';
import { MIN_VERSION, Th } from './th';
import type { TreeNode, TreeState } from './worktreeTree';
import { WorktreeTreeProvider } from './worktreeTree';

const BREW_INSTALL = 'brew install AirConditionedSoftware/tap/treehouse';

function setContext(key: string, value: boolean): void {
  void vscode.commands.executeCommand('setContext', key, value);
}

export function activate(context: vscode.ExtensionContext): void {
  context.subscriptions.push(log());
  logLine('Treehouse extension activated.');

  const th = new Th();
  const provider = new WorktreeTreeProvider(th);
  context.subscriptions.push(provider);

  const treeView = vscode.window.createTreeView<TreeNode>('treehouse.worktrees', {
    treeDataProvider: provider,
    showCollapseAll: true,
  });
  context.subscriptions.push(treeView);

  const refresh = new RefreshController(provider);
  context.subscriptions.push(refresh);

  // Optimistic until preflight says otherwise, so the "th not found" welcome
  // doesn't flash on every activation.
  setContext('treehouse.inRepo', false);
  setContext('treehouse.binaryOk', true);
  setContext('treehouse.empty', false);

  context.subscriptions.push(
    provider.onDidChangeState((state: TreeState) => {
      setContext('treehouse.inRepo', state.repoCount > 0);
      setContext('treehouse.empty', state.repoCount > 0 && state.worktreeCount === 0);
      if (state.error === 'missing') {
        setContext('treehouse.binaryOk', false);
      }
    }),
  );

  registerCommands(context, { th, provider, refresh, treeView });
  context.subscriptions.push(refresh.install());

  context.subscriptions.push(
    vscode.workspace.onDidChangeConfiguration((event) => {
      if (event.affectsConfiguration('treehouse.path')) {
        void runPreflight(th);
        refresh.refreshNow();
      }
    }),
  );

  // Never awaited: `th --version` goes through cobra's update notice, which makes a
  // GitHub call with a 2.5s timeout. The first tree render must not wait for it.
  void runPreflight(th);
}

async function runPreflight(th: Th): Promise<void> {
  const cwd = vscode.workspace.workspaceFolders?.[0]?.uri.fsPath;
  const result = await th.preflight(cwd);
  setContext('treehouse.binaryOk', result.ok);
  if (!result.ok) {
    logLine(`  preflight: \`${th.binary}\` could not be run.`);
    return;
  }
  logLine(`  preflight: th ${result.version ?? '(unknown version)'}`);
  if (result.meetsMinimum) {
    return;
  }
  const choice = await vscode.window.showErrorMessage(
    `Treehouse needs th ${MIN_VERSION} or newer${result.version ? ` (found ${result.version})` : ''}. Some features are unavailable.`,
    'Update th',
  );
  if (choice === 'Update th') {
    await vscode.env.clipboard.writeText(BREW_INSTALL);
    void vscode.window.showInformationMessage(`Copied to the clipboard: ${BREW_INSTALL}`);
  }
}

export function deactivate(): void {
  disposeLog();
}

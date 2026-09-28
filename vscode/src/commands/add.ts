import * as os from 'os';
import * as path from 'path';
import * as vscode from 'vscode';
import { effectivePrefix, predictTarget } from '../config';
import { branches } from '../git';
import { logLine } from '../log';
import { surfaceTrustWarning } from '../trust';
import type { RepoNode, TreeNode } from '../worktreeTree';
import type { CommandContext } from './index';
import { openPath } from './open';
import { resolveRepo } from './pick';

type OpenAfterCreate = 'newWindow' | 'currentWindow' | 'ask' | 'none';

interface BranchItem extends vscode.QuickPickItem {
  /** Absent on separators. */
  branch?: string;
  /** The synthetic "create this branch" row pinned above the list. */
  create?: boolean;
}

interface BranchChoice {
  branch: string;
  isNew: boolean;
  /** The user asked for the base step before creating. */
  askBase: boolean;
}

/** `treehouse.addWorktree` — pick or name a branch, then `th add`. */
export async function addWorktree(ctx: CommandContext, node?: TreeNode): Promise<void> {
  const repo = await resolveRepo(ctx, node);
  if (!repo) {
    return;
  }

  // Severable (plan §0.4): without it the picker simply shows no target preview.
  const config = await ctx.config.get(repo);
  const template = config?.settings.worktree_dir ?? '';
  const repoName = config?.repo?.name || path.basename(repo.mainPath);
  const prefix = config ? effectivePrefix(config.settings.branch_prefix, config.settings.prefix_separator) : '';

  const choice = await pickBranch(repo, { template, repoName, prefix });
  if (!choice) {
    return;
  }

  let base = '';
  if (choice.askBase) {
    const answer = await vscode.window.showInputBox({
      title: `Base for ${choice.branch}`,
      prompt: 'Branch, tag or commit the new branch starts from. Empty uses the configured default_base.',
      placeHolder: config?.settings.default_base || 'HEAD',
      ignoreFocusOut: true,
    });
    if (answer === undefined) {
      return;
    }
    base = answer.trim();
  }

  const created = await vscode.window.withProgress(
    {
      location: vscode.ProgressLocation.Notification,
      title: `Treehouse: creating a worktree for ${choice.branch}…`,
      cancellable: true,
    },
    async (_progress, token) => await ctx.th.add(repo.cwd, choice.branch, base ? { base } : {}, token),
  );

  await afterCreate(ctx, repo, created.path, choice.branch, created.result.stderr, created.result.args);
}

interface PickContext {
  /** `worktree_dir`, or '' when the effective config wasn't available. */
  template: string;
  repoName: string;
  prefix: string;
}

/**
 * A constructed QuickPick, not `showQuickPick`: the typed text has to become a
 * selectable item so a branch that doesn't exist yet can be created.
 */
async function pickBranch(repo: RepoNode, context: PickContext): Promise<BranchChoice | undefined> {
  const showRemotes = vscode.workspace.getConfiguration('treehouse').get<boolean>('showRemoteBranches', true);
  const known = await branches(repo.cwd, showRemotes);
  const names = new Set(known.map((branch) => branch.name));

  const listed: BranchItem[] = [];
  for (const branch of known.filter((b) => !b.remote)) {
    listed.push({ label: branch.name, description: branch.date, branch: branch.name });
  }
  const remoteOnly = known.filter((b) => b.remote);
  if (remoteOnly.length > 0) {
    listed.push({ label: 'Remote branches', kind: vscode.QuickPickItemKind.Separator });
    for (const branch of remoteOnly) {
      listed.push({ label: branch.name, description: branch.date, branch: branch.name });
    }
  }

  const changeBase: vscode.QuickInputButton = {
    iconPath: new vscode.ThemeIcon('git-commit'),
    tooltip: 'Change base…',
  };

  const picker = vscode.window.createQuickPick<BranchItem>();
  picker.title = 'New Worktree';
  picker.placeholder = 'Pick a branch, or type a new branch name';
  picker.matchOnDescription = false;
  picker.ignoreFocusOut = true;
  picker.buttons = [changeBase];
  picker.items = listed;

  let createLabel = '';
  const syncItems = (): void => {
    const typed = picker.value.trim();
    const wanted = typed.length > 0 && !names.has(typed) ? typed : '';
    if (wanted === createLabel) {
      return;
    }
    createLabel = wanted;
    if (!wanted) {
      picker.items = listed;
      return;
    }
    const full = applyPrefix(wanted, context.prefix);
    picker.items = [
      {
        label: `$(add) Create branch "${wanted}"`,
        description: full === wanted ? undefined : `→ ${full}`,
        alwaysShow: true,
        create: true,
        branch: wanted,
      },
      ...listed,
    ];
  };

  const titleFor = (item: BranchItem | undefined): string => {
    const branch = item?.branch;
    if (!context.template || !branch) {
      return 'New Worktree';
    }
    const full = item?.create ? applyPrefix(branch, context.prefix) : branch;
    return displayPath(predictTarget(context.template, context.repoName, full));
  };

  return await new Promise<BranchChoice | undefined>((resolve) => {
    let settled = false;
    const finish = (choice: BranchChoice | undefined): void => {
      if (settled) {
        return;
      }
      settled = true;
      resolve(choice);
      picker.hide();
    };

    const chosen = (askBase: boolean): BranchChoice | undefined => {
      const item = picker.selectedItems[0] ?? picker.activeItems[0];
      const typed = picker.value.trim();
      const branch = item?.branch ?? typed;
      if (!branch) {
        return undefined;
      }
      const isNew = item?.create === true || (!item && !names.has(branch));
      return { branch: isNew ? applyPrefix(branch, context.prefix) : branch, isNew, askBase };
    };

    picker.onDidChangeValue(() => syncItems());
    picker.onDidChangeActive((items) => {
      picker.title = titleFor(items[0]);
    });
    picker.onDidAccept(() => {
      const choice = chosen(false);
      if (choice) {
        finish(choice);
      }
    });
    picker.onDidTriggerButton((button) => {
      if (button !== changeBase) {
        return;
      }
      const choice = chosen(true);
      if (choice) {
        finish(choice);
        return;
      }
      picker.placeholder = 'Pick or type a branch name first, then choose a base';
    });
    picker.onDidHide(() => {
      finish(undefined);
      picker.dispose();
    });

    picker.show();
  });
}

/**
 * mirrors internal/cmd/add.go:106 — the branch prefix names branches that don't
 * exist yet, and is never doubled.
 */
export function applyPrefix(branch: string, prefix: string): string {
  if (!prefix || branch.startsWith(prefix)) {
    return branch;
  }
  return prefix + branch;
}

/**
 * Everything `th add` and `th add pr` do once a worktree exists: surface a
 * skipped-hook warning, refresh the tree, then open per
 * `treehouse.openAfterCreate`.
 *
 * The open goes last on purpose — a current-window open restarts the extension
 * host, so anything after it may never run.
 */
export async function afterCreate(
  ctx: CommandContext,
  repo: RepoNode,
  worktreePath: string,
  branch: string | undefined,
  stderr: string,
  args: string[],
): Promise<void> {
  surfaceTrustWarning(repo.cwd, args, stderr);
  ctx.refresh.refreshNow(repo);

  if (!worktreePath) {
    logLine('  th printed no worktree path; nothing to open.');
    return;
  }

  const mode = openAfterCreate();
  if (mode === 'none') {
    void vscode.window.showInformationMessage(`Treehouse: created ${displayPath(worktreePath)}`);
    return;
  }
  if (mode === 'ask') {
    const answer = await vscode.window.showInformationMessage(
      `Created ${path.basename(worktreePath)}`,
      'Open',
      'Open in New Window',
      'Not now',
    );
    if (answer === 'Open') {
      await openPath(worktreePath, branch, 'currentWindow');
    } else if (answer === 'Open in New Window') {
      await openPath(worktreePath, branch, 'newWindow');
    }
    return;
  }
  await openPath(worktreePath, branch, mode);
}

function openAfterCreate(): OpenAfterCreate {
  const value = vscode.workspace.getConfiguration('treehouse').get<string>('openAfterCreate', 'ask');
  switch (value) {
    case 'newWindow':
    case 'currentWindow':
    case 'none':
      return value;
    default:
      return 'ask';
  }
}

/** `~` abbreviation, matching `displayPath` in internal/cmd/display.go. */
function displayPath(p: string): string {
  const home = os.homedir();
  if (home && (p === home || p.startsWith(home + path.sep))) {
    return '~' + p.slice(home.length);
  }
  return p;
}

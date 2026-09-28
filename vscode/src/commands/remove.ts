import * as path from 'path';
import * as vscode from 'vscode';
import { showLog } from '../log';
import type { RemoveOptions } from '../th';
import { ThError, ThMissingError } from '../th';
import { surfaceTrustWarning } from '../trust';
import type { RemoveResult } from '../types';
import type { TreeNode, WorktreeNode } from '../worktreeTree';
import type { CommandContext } from './index';
import { pickWorktreeNode } from './pick';

const VIEW_ID = 'treehouse.worktrees';

/** What is known about the worktree's cleanliness when the modal is raised. */
type DirtyState =
  /** `dirty` was 0, or absent (th < 0.5.0) and th has not complained yet. */
  | { kind: 'clean' }
  /** `th list --json` reported this many changed paths. */
  | { kind: 'count'; count: number }
  /** th refused the removal and asked for --force. */
  | { kind: 'reported' };

/** One `th remove` invocation, successful or not. */
interface Attempt {
  /** The `--json` document, also recovered from a failed run (finishRemove prints it first). */
  results: RemoveResult[];
  stderr: string;
  /** The argv th was spawned with, for the trust gate's "Run in Terminal". */
  args: string[];
  failure?: ThError;
}

/**
 * Remove one worktree: confirm, ask about the branch, run `th remove --json`,
 * then say what th reported. Every mutation goes through the binary.
 */
export async function removeWorktree(ctx: CommandContext, node?: TreeNode): Promise<void> {
  const target = await pickWorktreeNode(ctx, node, {
    removableOnly: true,
    placeHolder: 'Select a worktree to remove',
  });
  if (!target) {
    return;
  }

  const { entry, repo } = target;
  const dirtyCount = typeof entry.dirty === 'number' ? entry.dirty : undefined;
  const dirty: DirtyState =
    dirtyCount !== undefined && dirtyCount > 0 ? { kind: 'count', count: dirtyCount } : { kind: 'clean' };

  if (!(await confirmRemoval(target, dirty))) {
    return;
  }
  let force = dirty.kind === 'count';

  let deleteBranch = false;
  if (entry.branch) {
    // A detached or branchless worktree has nothing to ask about.
    const answer = await askDeleteBranch(entry.branch);
    if (answer === undefined) {
      return;
    }
    deleteBranch = answer;
  }

  // remove.go findWorktree matches branch names first and absolute paths second,
  // so either identifies the worktree; the branch is th's documented argument
  // (`remove [branch...]`), and the path is the only handle a detached worktree has.
  const targetArg = entry.branch ?? entry.path;

  let useJson = true;
  let attempt = await runRemove(ctx, repo.cwd, targetArg, { force, deleteBranch }, useJson);

  // th < 0.5.0 has no `remove --json`. Retry plainly and fall back to the exit code.
  if (attempt.failure && isUnknownFlag(attempt.failure)) {
    useJson = false;
    attempt = await runRemove(ctx, repo.cwd, targetArg, { force, deleteBranch }, useJson);
  }

  // Off-TTY, a dirty worktree is a hard error rather than a prompt (remove.go:288).
  // With no `dirty` count to have asked about up front, ask now and retry.
  if (attempt.failure && !force && needsForce(attempt.failure)) {
    if (!(await confirmRemoval(target, { kind: 'reported' }))) {
      surfaceTrustWarning(repo.cwd, attempt.args, attempt.stderr);
      ctx.refresh.refreshNow(repo);
      return;
    }
    force = true;
    attempt = await runRemove(ctx, repo.cwd, targetArg, { force, deleteBranch }, useJson);
  }

  surfaceTrustWarning(repo.cwd, attempt.args, attempt.stderr);
  report(target, attempt, useJson);
  ctx.refresh.refreshNow(repo);
}

/** The modal. A dirty worktree gets the Force Remove button, because th will refuse otherwise. */
async function confirmRemoval(node: WorktreeNode, dirty: DirtyState): Promise<boolean> {
  const name = displayName(node);
  const branch = node.entry.branch;
  const message = branch ? `Remove worktree ${name} (branch ${branch})?` : `Remove worktree ${name}?`;

  const detail = [node.entry.path];
  if (dirty.kind === 'count') {
    detail.push(
      `It has ${dirty.count} uncommitted change${dirty.count === 1 ? '' : 's'}; removing it discards them.`,
    );
  } else if (dirty.kind === 'reported') {
    detail.push('It has modified or untracked files; removing it discards them.');
  }

  const button = dirty.kind === 'clean' ? 'Remove' : 'Force Remove';
  const choice = await vscode.window.showWarningMessage(
    message,
    { modal: true, detail: detail.join('\n\n') },
    button,
  );
  return choice === button;
}

/** undefined means the user dismissed the step, which aborts the whole removal. */
async function askDeleteBranch(branch: string): Promise<boolean | undefined> {
  const preferDelete = vscode.workspace
    .getConfiguration('treehouse')
    .get<boolean>('removeDefaultDeleteBranch', false);

  const keep = { label: 'Keep branch', description: branch, deleteBranch: false };
  const remove = {
    label: `Also delete branch ${branch}`,
    description: 'git branch -d; an unmerged branch is kept',
    deleteBranch: true,
  };
  // The first item is the pre-selected one, which is how the setting expresses itself.
  const items = preferDelete ? [remove, keep] : [keep, remove];

  const picked = await vscode.window.showQuickPick(items, {
    placeHolder: `Also delete branch ${branch}?`,
  });
  return picked?.deleteBranch;
}

/** Progress lives on the view: the tree is what the user is looking at. */
async function runRemove(
  ctx: CommandContext,
  cwd: string,
  target: string,
  options: RemoveOptions,
  useJson: boolean,
): Promise<Attempt> {
  return await vscode.window.withProgress({ location: { viewId: VIEW_ID } }, async () => {
    return await attemptRemove(ctx, cwd, target, options, useJson);
  });
}

async function attemptRemove(
  ctx: CommandContext,
  cwd: string,
  target: string,
  options: RemoveOptions,
  useJson: boolean,
): Promise<Attempt> {
  if (useJson) {
    try {
      const { results, result } = await ctx.th.remove(cwd, [target], options);
      return { results, stderr: result.stderr, args: result.args };
    } catch (err) {
      if (!(err instanceof ThError)) {
        throw err;
      }
      // finishRemove prints the document before propagating, so a failed run
      // still describes what it managed to do.
      return { results: parseResults(err.stdout), stderr: err.stderr, args: err.args, failure: err };
    }
  }

  const args = ['remove'];
  if (options.force) {
    args.push('--force');
  }
  if (options.deleteBranch) {
    args.push('--delete-branch');
  }
  args.push(target);
  try {
    const result = await ctx.th.run(args, { cwd });
    return { results: [], stderr: result.stderr, args: result.args };
  } catch (err) {
    if (!(err instanceof ThError)) {
      throw err;
    }
    return { results: [], stderr: err.stderr, args: err.args, failure: err };
  }
}

function parseResults(stdout: string): RemoveResult[] {
  const text = stdout.trim();
  if (text.length === 0) {
    return [];
  }
  try {
    const parsed: unknown = JSON.parse(text);
    return Array.isArray(parsed) ? (parsed as RemoveResult[]) : [];
  } catch {
    return [];
  }
}

function isUnknownFlag(err: ThError): boolean {
  return /unknown flag/i.test(err.message) || /unknown flag/i.test(err.stderr);
}

function needsForce(err: ThError): boolean {
  return /re-run with --force/.test(err.message) || /re-run with --force/.test(err.stderr);
}

function displayName(node: WorktreeNode): string {
  return path.basename(node.entry.path) || node.entry.path;
}

function report(target: WorktreeNode, attempt: Attempt, useJson: boolean): void {
  if (attempt.failure instanceof ThMissingError) {
    void vscode.window.showErrorMessage(attempt.failure.message);
    return;
  }
  if (!useJson || attempt.results.length === 0) {
    // Old th, or a failure th could not describe: the exit code is the whole story.
    if (attempt.failure) {
      showFailure(attempt.failure.message);
      return;
    }
    void vscode.window.showInformationMessage(`Removed worktree ${displayName(target)}.`);
    return;
  }
  for (const result of attempt.results) {
    reportResult(result, displayName(target));
  }
}

function reportResult(result: RemoveResult, fallbackName: string): void {
  const name = result.path ? path.basename(result.path) || result.path : result.target || fallbackName;

  if (result.removed) {
    if (result.error) {
      // The worktree is gone but teardown failed — a success that must not read as one.
      withLog(vscode.window.showWarningMessage('Worktree removed, but a post_remove hook failed.', 'Show Log'));
      return;
    }
    void vscode.window.showInformationMessage(`Removed worktree ${name}${branchNote(result)}.`);
    return;
  }
  if (result.skipped_dirty) {
    void vscode.window.showWarningMessage(`Skipped ${name}: it has uncommitted changes.`);
    return;
  }
  if (result.error) {
    showFailure(result.error);
    return;
  }
  void vscode.window.showWarningMessage(`${name} was not removed.`);
}

/** The branch half of the outcome, matching the notes th prints on its Removed line. */
function branchNote(result: RemoveResult): string {
  const branch = result.branch;
  if (!branch || !result.branch_action) {
    return '';
  }
  if (result.branch_action === 'deleted') {
    return `; deleted branch "${branch}"`;
  }
  switch (result.branch_reason) {
    case 'not_fully_merged':
      return `; kept branch "${branch}" (not fully merged)`;
    case 'default_branch':
      return `; kept branch "${branch}" (the default branch)`;
    case 'delete_failed':
      return `; kept branch "${branch}" (${result.branch_error ?? 'deletion failed'})`;
    default:
      // "kept" with no reason is the ordinary no --delete-branch case: nothing to say.
      return '';
  }
}

function showFailure(message: string): void {
  withLog(vscode.window.showErrorMessage(message, 'Show Log'));
}

function withLog(message: Thenable<string | undefined>): void {
  void message.then((choice) => {
    if (choice === 'Show Log') {
      showLog();
    }
  });
}

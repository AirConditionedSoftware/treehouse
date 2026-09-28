import * as vscode from 'vscode';
import type { TreeNode } from '../worktreeTree';
import { afterCreate } from './add';
import type { CommandContext } from './index';
import { resolveRepo } from './pick';

/** `123` or `#123`. */
const PR_NUMBER = /^#?\d+$/;
/** A GitHub pull request URL, with or without a trailing `/files`, query or fragment. */
const PR_URL = /^https?:\/\/[^/]*github\.com\/[^/]+\/[^/]+\/pull\/\d+(?:[/?#].*)?$/;

/** Both forms `forge.ParsePRArg` accepts; the validation is purely UX. */
export function isPrRef(value: string): boolean {
  const trimmed = value.trim();
  return PR_NUMBER.test(trimmed) || PR_URL.test(trimmed);
}

/**
 * The two stderr lines `th add pr` prints that change what the user gets
 * (internal/cmd/add_pr.go:53-56): no `gh`, so the branch is `pr-<n>` off
 * `refs/pull/<n>/head`; or a PR that is already merged or closed.
 */
export function prWarnings(stderr: string): string[] {
  const out: string[] = [];
  for (const raw of stderr.split(/\r?\n/)) {
    const line = raw.trim();
    if (/^gh unavailable for PR #\d+/.test(line) || /^Warning: PR #\d+ is (merged|closed)\b/.test(line)) {
      out.push(line);
    }
  }
  return out;
}

/** `treehouse.addWorktreeFromPR` — ask for a PR reference, then `th add pr`. */
export async function addWorktreeFromPR(ctx: CommandContext, node?: TreeNode): Promise<void> {
  const repo = await resolveRepo(ctx, node);
  if (!repo) {
    return;
  }

  const answer = await vscode.window.showInputBox({
    title: 'New Worktree from Pull Request',
    prompt: 'Pull request number or GitHub URL',
    placeHolder: '123, #123, or https://github.com/owner/repo/pull/123',
    ignoreFocusOut: true,
    validateInput: (value) =>
      isPrRef(value) ? undefined : 'Enter a pull request number (123 or #123) or a GitHub pull request URL.',
  });
  if (answer === undefined) {
    return;
  }
  // Passed through verbatim: `forge.ParsePRArg` accepts all three forms.
  const ref = answer.trim();

  const created = await vscode.window.withProgress(
    {
      location: vscode.ProgressLocation.Notification,
      title: `Treehouse: creating a worktree for PR ${ref}…`,
      cancellable: true,
    },
    async (_progress, token) => await ctx.th.addPr(repo.cwd, ref, token),
  );

  for (const warning of prWarnings(created.result.stderr)) {
    void vscode.window.showWarningMessage(`Treehouse: ${warning}`);
  }

  // `th` names the branch from the PR head (or `pr-<n>`), and never tells us
  // which — the worktree directory is that name sanitized, which is all the
  // workspace-file lookup in `openPath` needs.
  await afterCreate(ctx, repo, created.path, undefined, created.result.stderr, created.result.args);
}

import * as path from 'path';
import * as vscode from 'vscode';
import { logLine, runInTerminal, showLog } from './log';
import { hasTrustWarning } from './th';

/**
 * The warning `approveRepoCommands` prints off-TTY (internal/cmd/hooks.go):
 *
 *   Warning: post_create from ~/src/myapp/.thrc is not approved; skipping (run th add interactively to review).
 *
 * The hook name and the file it came from are the two parts worth repeating back.
 */
const TRUST_WARNING_LINE = /^\s*Warning:\s+(\S+)\s+from\s+(.*?)\s+is not approved; skipping/;

export interface TrustWarning {
  /** Hook names th refused to run, in the order they were warned about. */
  hooks: string[];
  /** The files those hooks came from, as th displayed them (`~`-abbreviated). */
  files: string[];
}

/** Pull the hook names and source files out of th's stderr. */
export function parseTrustWarnings(stderr: string): TrustWarning {
  const hooks: string[] = [];
  const files: string[] = [];
  for (const line of stderr.split(/\r?\n/)) {
    const match = TRUST_WARNING_LINE.exec(line);
    if (!match) {
      continue;
    }
    const hook = match[1];
    const file = match[2];
    if (hook && !hooks.includes(hook)) {
      hooks.push(hook);
    }
    if (file && !files.includes(file)) {
      files.push(file);
    }
  }
  return { hooks, files };
}

/** Mirrors `Th.binary`, which is an instance getter this module has no handle on. */
function thBinary(): string {
  const configured = vscode.workspace.getConfiguration('treehouse').get<string>('path', 'th').trim();
  return configured.length > 0 ? configured : 'th';
}

/**
 * An unapproved `.thrc` hook is skipped with a warning and the operation still
 * exits 0 (a committed `.thrc` must not be able to block removing a worktree).
 * Silence would be wrong here: say what was skipped, and offer the one thing
 * that can approve it — the same command run where huh's confirm has a TTY.
 *
 * Non-blocking by design: the caller has already finished its work.
 */
export function surfaceTrustWarning(cwd: string, args: string[], stderr: string): void {
  if (!hasTrustWarning(stderr)) {
    return;
  }
  const { hooks, files } = parseTrustWarnings(stderr);
  const what = hooks.length > 0 ? hooks.join(', ') : 'repository';
  const where = files.length > 0 ? files.map((file) => path.basename(file) || file).join(', ') : '.thrc';
  logLine(`  trust gate: th skipped ${what} from ${where}`);

  // --json is dropped so th can render its huh confirm without transcribing it
  // onto the machine channel.
  const command = [thBinary(), ...args.filter((arg) => arg !== '--json')];
  const message = `th skipped unapproved ${what} commands from ${where}. Approve them by running the command in a terminal.`;

  void vscode.window.showWarningMessage(message, 'Run in Terminal', 'Show Log').then((choice) => {
    if (choice === 'Run in Terminal') {
      runInTerminal(cwd, command);
    } else if (choice === 'Show Log') {
      showLog();
    }
  });
}

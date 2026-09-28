import * as vscode from 'vscode';

let channel: vscode.OutputChannel | undefined;

/** The one `Treehouse` output channel. Created lazily, disposed by `disposeLog`. */
export function log(): vscode.OutputChannel {
  if (!channel) {
    channel = vscode.window.createOutputChannel('Treehouse');
  }
  return channel;
}

export function logLine(message: string): void {
  log().appendLine(message);
}

export function showLog(preserveFocus = false): void {
  log().show(preserveFocus);
}

export function disposeLog(): void {
  channel?.dispose();
  channel = undefined;
}

/**
 * Quote one argument for a POSIX shell. The terminal is the only place the extension
 * hands `th` a command line as *text*; everywhere else it spawns without a shell.
 */
export function shellQuote(arg: string): string {
  if (arg.length > 0 && /^[A-Za-z0-9_@%+=:,./-]+$/.test(arg)) {
    return arg;
  }
  return `'${arg.replace(/'/g, `'\\''`)}'`;
}

export function shellCommand(args: string[]): string {
  return args.map(shellQuote).join(' ');
}

/**
 * Open an integrated terminal in `cwd` and send `args` as a shell-quoted command line.
 * Used by the trust gate, where `th`'s own confirmation prompt needs a TTY to render.
 */
export function runInTerminal(cwd: string, args: string[]): vscode.Terminal {
  const command = shellCommand(args);
  const terminal = vscode.window.createTerminal({ cwd, name: 'Treehouse' });
  terminal.show();
  terminal.sendText(command, true);
  logLine(`$ ${command}  (terminal, cwd=${cwd})`);
  return terminal;
}

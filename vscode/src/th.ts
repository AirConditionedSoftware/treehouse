import { spawn } from 'child_process';
import * as os from 'os';
import * as vscode from 'vscode';
import { logLine } from './log';
import type { EffectiveConfig, RemoveResult, WorktreeEntry } from './types';

/** The `th` release that added `list --json` dirty/merged and `remove --json`. */
export const MIN_VERSION = '0.4.0';

/** How long a cancelled child gets to honour SIGINT before SIGKILL. */
const KILL_GRACE_MS = 5000;

export interface RunResult {
  args: string[];
  stdout: string;
  stderr: string;
  exitCode: number;
}

export interface RunOptions {
  cwd: string;
  token?: vscode.CancellationToken;
  /** Overrides `treehouse.commandTimeoutSeconds`. */
  timeoutMs?: number;
}

/** A `th` invocation that exited non-zero (or was killed). */
export class ThError extends Error {
  readonly exitCode: number;
  readonly stdout: string;
  readonly stderr: string;
  readonly args: string[];

  constructor(args: string[], exitCode: number, stdout: string, stderr: string, message?: string) {
    super(message ?? parseThErrorLine(stderr) ?? `th exited with code ${exitCode}`);
    this.name = 'ThError';
    this.args = args;
    this.exitCode = exitCode;
    this.stdout = stdout;
    this.stderr = stderr;
  }
}

/** The configured `th` could not be spawned at all (ENOENT). */
export class ThMissingError extends ThError {
  readonly binary: string;

  constructor(binary: string, args: string[]) {
    super(args, -1, '', '', `The \`th\` binary was not found (${binary}).`);
    this.name = 'ThMissingError';
    this.binary = binary;
  }
}

/**
 * Pull the human message out of `th`'s stderr. `root.go` prints `th: <message>`;
 * anything else falls back to the last non-empty line.
 */
export function parseThErrorLine(stderr: string): string | undefined {
  const lines = stderr.split(/\r?\n/);
  for (let i = lines.length - 1; i >= 0; i--) {
    const line = (lines[i] ?? '').trim();
    if (line.startsWith('th: ')) {
      return line.slice(4).trim();
    }
  }
  for (let i = lines.length - 1; i >= 0; i--) {
    const line = (lines[i] ?? '').trim();
    if (line.length > 0) {
      return line;
    }
  }
  return undefined;
}

/**
 * True when `th` skipped repo-supplied hook commands that are not approved
 * (hooks.go warns and continues off-TTY, exiting 0).
 */
export function hasTrustWarning(stderr: string): boolean {
  return /is not approved; skipping/.test(stderr);
}

/** `th version 0.5.0` -> `0.5.0`. */
export function parseVersion(stdout: string): string | undefined {
  const match = /(\d+)\.(\d+)\.(\d+)(?:[-+][0-9A-Za-z.-]+)?/.exec(stdout);
  return match ? match[0] : undefined;
}

/** -1 / 0 / 1, comparing the numeric `x.y.z` prefix only. */
export function compareVersions(a: string, b: string): number {
  const parse = (v: string): number[] => {
    const match = /(\d+)\.(\d+)\.(\d+)/.exec(v);
    if (!match) {
      return [0, 0, 0];
    }
    return [Number(match[1]), Number(match[2]), Number(match[3])];
  };
  const left = parse(a);
  const right = parse(b);
  for (let i = 0; i < 3; i++) {
    const l = left[i] ?? 0;
    const r = right[i] ?? 0;
    if (l !== r) {
      return l < r ? -1 : 1;
    }
  }
  return 0;
}

export interface Preflight {
  ok: boolean;
  version?: string;
  meetsMinimum: boolean;
}

export interface AddOptions {
  base?: string;
}

export interface RemoveOptions {
  force?: boolean;
  deleteBranch?: boolean;
}

function config(): vscode.WorkspaceConfiguration {
  return vscode.workspace.getConfiguration('treehouse');
}

/** The process layer: the only place in the extension that spawns `th`. */
export class Th {
  /** The configured binary, read fresh so a settings change takes effect immediately. */
  get binary(): string {
    const configured = config().get<string>('path', 'th').trim();
    return configured.length > 0 ? configured : 'th';
  }

  private get defaultTimeoutMs(): number {
    const seconds = config().get<number>('commandTimeoutSeconds', 180);
    return Math.max(1, seconds) * 1000;
  }

  /**
   * Spawn `th` with `args`. Never uses a shell: branch names may legally contain
   * `$( )` or spaces, and a shell would reinterpret them.
   */
  async run(args: string[], options: RunOptions): Promise<RunResult> {
    const binary = this.binary;
    const timeoutMs = options.timeoutMs ?? this.defaultTimeoutMs;
    logLine(`$ ${binary} ${args.join(' ')}  (cwd=${options.cwd})`);

    const env: NodeJS.ProcessEnv = { ...process.env, NO_COLOR: '1', GIT_OPTIONAL_LOCKS: '0' };
    // th writes the destination here for the shell wrapper; the extension has no
    // wrapper and must not have th touch a stale file.
    delete env.TH_CD_FILE;

    return await new Promise<RunResult>((resolve, reject) => {
      const child = spawn(binary, args, { cwd: options.cwd, env, shell: false });

      let stdout = '';
      let stderr = '';
      let settled = false;
      let killTimer: NodeJS.Timeout | undefined;
      let timeoutTimer: NodeJS.Timeout | undefined;
      let timedOut = false;
      let cancelled = false;

      const cleanup = (): void => {
        if (killTimer) {
          clearTimeout(killTimer);
          killTimer = undefined;
        }
        if (timeoutTimer) {
          clearTimeout(timeoutTimer);
          timeoutTimer = undefined;
        }
        subscription?.dispose();
      };

      const terminate = (): void => {
        if (settled || killTimer) {
          return;
        }
        child.kill('SIGINT');
        killTimer = setTimeout(() => child.kill('SIGKILL'), KILL_GRACE_MS);
      };

      const subscription = options.token?.onCancellationRequested(() => {
        cancelled = true;
        terminate();
      });

      if (timeoutMs > 0) {
        timeoutTimer = setTimeout(() => {
          timedOut = true;
          terminate();
        }, timeoutMs);
      }

      child.stdout?.on('data', (chunk: Buffer) => {
        stdout += chunk.toString();
      });
      child.stderr?.on('data', (chunk: Buffer) => {
        stderr += chunk.toString();
      });

      child.on('error', (err: NodeJS.ErrnoException) => {
        if (settled) {
          return;
        }
        settled = true;
        cleanup();
        if (err.code === 'ENOENT') {
          logLine(`  th could not be spawned: ${binary} not found`);
          reject(new ThMissingError(binary, args));
          return;
        }
        logLine(`  th could not be spawned: ${err.message}`);
        reject(new ThError(args, -1, stdout, stderr, err.message));
      });

      child.on('close', (code, signal) => {
        if (settled) {
          return;
        }
        settled = true;
        cleanup();
        const exitCode = code ?? -1;
        logLine(`  exit ${signal ? `signal ${signal}` : exitCode}`);
        if (stderr.trim().length > 0) {
          logLine(stderr.trimEnd());
        }
        if (cancelled) {
          reject(new vscode.CancellationError());
          return;
        }
        if (timedOut) {
          reject(
            new ThError(
              args,
              exitCode,
              stdout,
              stderr,
              `th timed out after ${Math.round(timeoutMs / 1000)}s (treehouse.commandTimeoutSeconds).`,
            ),
          );
          return;
        }
        if (exitCode !== 0) {
          reject(new ThError(args, exitCode, stdout, stderr));
          return;
        }
        resolve({ args, stdout, stderr, exitCode });
      });
    });
  }

  private async runJSON<T>(args: string[], options: RunOptions): Promise<{ value: T; result: RunResult }> {
    const result = await this.run(args, options);
    const text = result.stdout.trim();
    try {
      return { value: JSON.parse(text) as T, result };
    } catch {
      throw new ThError(args, result.exitCode, result.stdout, result.stderr, 'th printed output that is not valid JSON.');
    }
  }

  /** `th list --json` */
  async list(cwd: string, token?: vscode.CancellationToken): Promise<WorktreeEntry[]> {
    const { value } = await this.runJSON<WorktreeEntry[]>(['list', '--json'], { cwd, token });
    return Array.isArray(value) ? value : [];
  }

  /**
   * `th add --no-open [--base <ref>] <branch>` — resolves to the worktree path th
   * printed on stdout, with the raw result for stderr inspection (the trust gate).
   */
  async add(
    cwd: string,
    branch: string,
    options: AddOptions = {},
    token?: vscode.CancellationToken,
  ): Promise<{ path: string; result: RunResult }> {
    const args = ['add', '--no-open'];
    if (options.base) {
      args.push('--base', options.base);
    }
    args.push(branch);
    const result = await this.run(args, { cwd, token });
    return { path: result.stdout.trim(), result };
  }

  /** `th add pr --no-open <number|#number|url>` */
  async addPr(
    cwd: string,
    ref: string,
    token?: vscode.CancellationToken,
  ): Promise<{ path: string; result: RunResult }> {
    const result = await this.run(['add', 'pr', '--no-open', ref], { cwd, token });
    return { path: result.stdout.trim(), result };
  }

  /** `th remove --json [--force] [--delete-branch] <target>…` */
  async remove(
    cwd: string,
    targets: string[],
    options: RemoveOptions = {},
    token?: vscode.CancellationToken,
  ): Promise<{ results: RemoveResult[]; result: RunResult }> {
    const args = ['remove', '--json'];
    if (options.force) {
      args.push('--force');
    }
    if (options.deleteBranch) {
      args.push('--delete-branch');
    }
    args.push(...targets);
    const { value, result } = await this.runJSON<RemoveResult[]>(args, { cwd, token });
    return { results: Array.isArray(value) ? value : [], result };
  }

  /** `th config --effective --json` */
  async effectiveConfig(cwd: string, token?: vscode.CancellationToken): Promise<EffectiveConfig> {
    const { value } = await this.runJSON<EffectiveConfig>(['config', '--effective', '--json'], { cwd, token });
    return value;
  }

  /**
   * `th --version`. Beware: cobra's version path triggers th's update notice, a
   * GitHub call with a 2.5s timeout — never await this on the activation path.
   */
  async version(cwd?: string): Promise<string | undefined> {
    const result = await this.run(['--version'], { cwd: cwd ?? os.homedir(), timeoutMs: 15000 });
    return parseVersion(result.stdout);
  }

  /** Is the binary there, and is it new enough? Never throws. */
  async preflight(cwd?: string): Promise<Preflight> {
    try {
      const version = await this.version(cwd);
      if (!version) {
        return { ok: true, meetsMinimum: false };
      }
      return { ok: true, version, meetsMinimum: compareVersions(version, MIN_VERSION) >= 0 };
    } catch {
      return { ok: false, meetsMinimum: false };
    }
  }
}

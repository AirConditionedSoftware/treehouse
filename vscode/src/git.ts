import { spawn } from 'child_process';
import { logLine } from './log';

/**
 * Read-only git. Every *mutation* goes through `th`; these are the few reads `th`
 * has no machine contract for.
 */

export interface BranchInfo {
  /** Short name — `main`, `feature/x`. Remote-only branches lose the `origin/` prefix. */
  name: string;
  /** True when only a remote ref exists for this name. */
  remote: boolean;
  /** Relative commit date, e.g. `3 days ago`. */
  date: string;
}

export interface GitResult {
  stdout: string;
  stderr: string;
  exitCode: number;
}

const GIT_TIMEOUT_MS = 15000;

async function git(cwd: string, args: string[]): Promise<GitResult> {
  return await new Promise<GitResult>((resolve, reject) => {
    const env: NodeJS.ProcessEnv = { ...process.env, NO_COLOR: '1', GIT_OPTIONAL_LOCKS: '0' };
    const child = spawn('git', args, { cwd, env, shell: false });
    let stdout = '';
    let stderr = '';
    let settled = false;

    const timer = setTimeout(() => child.kill('SIGKILL'), GIT_TIMEOUT_MS);

    child.stdout?.on('data', (chunk: Buffer) => {
      stdout += chunk.toString();
    });
    child.stderr?.on('data', (chunk: Buffer) => {
      stderr += chunk.toString();
    });
    child.on('error', (err) => {
      if (settled) {
        return;
      }
      settled = true;
      clearTimeout(timer);
      logLine(`  git ${args.join(' ')} failed to spawn: ${err.message}`);
      reject(err);
    });
    child.on('close', (code) => {
      if (settled) {
        return;
      }
      settled = true;
      clearTimeout(timer);
      resolve({ stdout, stderr, exitCode: code ?? -1 });
    });
  });
}

/** The main working tree of the repository containing `cwd`, or undefined if there is none. */
export async function toplevel(cwd: string): Promise<string | undefined> {
  try {
    const result = await git(cwd, ['rev-parse', '--show-toplevel']);
    if (result.exitCode !== 0) {
      return undefined;
    }
    const line = result.stdout.trim();
    return line.length > 0 ? line : undefined;
  } catch {
    return undefined;
  }
}

/**
 * The repository's common git dir, absolute. `--path-format=absolute` matters:
 * plain `--git-common-dir` may answer a relative `.git`.
 */
export async function gitCommonDir(cwd: string): Promise<string | undefined> {
  try {
    const result = await git(cwd, ['rev-parse', '--path-format=absolute', '--git-common-dir']);
    if (result.exitCode !== 0) {
      return undefined;
    }
    const line = result.stdout.trim();
    return line.length > 0 ? line : undefined;
  } catch {
    return undefined;
  }
}

/**
 * Local branches first, then remote-only ones, newest commit first. A remote branch
 * whose short name already exists locally is dropped.
 */
export async function branches(cwd: string, includeRemotes = true): Promise<BranchInfo[]> {
  const refs = includeRemotes ? ['refs/heads', 'refs/remotes/origin'] : ['refs/heads'];
  let result: GitResult;
  try {
    result = await git(cwd, [
      'for-each-ref',
      '--sort=-committerdate',
      // %(refname) leads so `refs/remotes/origin/HEAD` (whose short name is just
      // `origin`) can be filtered and remote-ness read from the ref, not the name.
      '--format=%(refname)%09%(refname:short)%09%(objecttype)%09%(committerdate:relative)',
      ...refs,
    ]);
  } catch {
    return [];
  }
  if (result.exitCode !== 0) {
    return [];
  }

  const locals: BranchInfo[] = [];
  const remotes: BranchInfo[] = [];
  const localNames = new Set<string>();

  for (const line of result.stdout.split(/\r?\n/)) {
    if (line.trim().length === 0) {
      continue;
    }
    const parts = line.split('\t');
    const refname = (parts[0] ?? '').trim();
    const short = (parts[1] ?? '').trim();
    const date = (parts[3] ?? '').trim();
    if (refname.length === 0 || short.length === 0 || refname.endsWith('/HEAD')) {
      continue;
    }
    if (refname.startsWith('refs/remotes/origin/')) {
      remotes.push({ name: refname.slice('refs/remotes/origin/'.length), remote: true, date });
    } else {
      localNames.add(short);
      locals.push({ name: short, remote: false, date });
    }
  }

  const seenRemote = new Set<string>();
  const remoteOnly = remotes.filter((branch) => {
    if (localNames.has(branch.name) || seenRemote.has(branch.name)) {
      return false;
    }
    seenRemote.add(branch.name);
    return true;
  });

  return [...locals, ...remoteOnly];
}

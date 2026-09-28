import * as fs from 'fs';
import * as os from 'os';
import * as path from 'path';
import { afterAll, beforeAll, describe, expect, it, vi } from 'vitest';
import {
  EffectiveConfigCache,
  effectivePrefix,
  expandTilde,
  findWorkspaceFile,
  predictTarget,
  sanitizeBranch,
} from '../../config';
import type { EffectiveConfig } from '../../types';

/** A home that is the same on every machine, so the table below can be exact. */
const HOME = path.join(path.sep + 'home', 'dev');

describe('sanitizeBranch', () => {
  it('mirrors config.SanitizeBranch: every slash becomes a dash', () => {
    expect(sanitizeBranch('fix-login')).toBe('fix-login');
    expect(sanitizeBranch('feature/login')).toBe('feature-login');
    expect(sanitizeBranch('team/peter/fix/login')).toBe('team-peter-fix-login');
    expect(sanitizeBranch('')).toBe('');
  });
});

describe('expandTilde', () => {
  it('expands a bare ~ and a ~/ prefix, and nothing else', () => {
    expect(expandTilde('~', HOME)).toBe(HOME);
    expect(expandTilde('~/trees', HOME)).toBe(path.join(HOME, 'trees'));
    expect(expandTilde('/trees/~/x', HOME)).toBe('/trees/~/x');
    // Go's ExpandTilde only handles the current user's home.
    expect(expandTilde('~other/trees', HOME)).toBe('~other/trees');
    expect(expandTilde('trees/~', HOME)).toBe('trees/~');
    expect(expandTilde('', HOME)).toBe('');
  });
});

describe('predictTarget', () => {
  // The table is TestWorktreePath in internal/config/config_test.go, verbatim.
  const cases: { tmpl: string; repo: string; branch: string; want: string }[] = [
    { tmpl: '/trees/{repo}/{branch}', repo: 'myapp', branch: 'fix-login', want: '/trees/myapp/fix-login' },
    { tmpl: '/trees/{repo}/{branch}', repo: 'myapp', branch: 'feature/login', want: '/trees/myapp/feature-login' },
    { tmpl: '~/trees/{branch}', repo: 'myapp', branch: 'x', want: path.join(HOME, 'trees', 'x') },
  ];

  for (const { tmpl, repo, branch, want } of cases) {
    it(`${tmpl} + ${repo} + ${branch} -> ${want}`, () => {
      expect(predictTarget(tmpl, repo, branch, HOME)).toBe(want);
    });
  }

  it('cleans the result the way filepath.Clean does', () => {
    expect(predictTarget('/trees/{repo}/', 'myapp', 'x', HOME)).toBe('/trees/myapp');
    expect(predictTarget('/trees//{repo}/./{branch}', 'myapp', 'x', HOME)).toBe('/trees/myapp/x');
  });

  it('substitutes literally, so $& in a name is not a replacement pattern', () => {
    expect(predictTarget('/trees/{repo}/{branch}', 'my$&app', 'x', HOME)).toBe('/trees/my$&app/x');
  });

  it('leaves a template with no placeholders alone', () => {
    expect(predictTarget('/trees/fixed', 'myapp', 'x', HOME)).toBe('/trees/fixed');
  });
});

describe('effectivePrefix', () => {
  // Mirrors Settings.EffectivePrefix (internal/config/config.go:635).
  it('is empty without a branch_prefix', () => {
    expect(effectivePrefix('', '/')).toBe('');
    expect(effectivePrefix('', '')).toBe('');
  });

  it('appends the separator', () => {
    expect(effectivePrefix('peter', '/')).toBe('peter/');
    expect(effectivePrefix('peter', '-')).toBe('peter-');
  });

  it('never doubles a separator the prefix already ends with', () => {
    expect(effectivePrefix('peter/', '/')).toBe('peter/');
    expect(effectivePrefix('peter-', '-')).toBe('peter-');
  });

  it('falls back to / when no separator is configured', () => {
    expect(effectivePrefix('peter', '')).toBe('peter/');
    expect(effectivePrefix('peter/', '')).toBe('peter/');
  });
});

describe('findWorkspaceFile', () => {
  let dir = '';
  let trees = '';

  beforeAll(() => {
    dir = fs.mkdtempSync(path.join(os.tmpdir(), 'treehouse-ws-'));
    trees = path.join(dir, 'worktrees', 'myapp');
    fs.mkdirSync(trees, { recursive: true });
  });

  afterAll(() => {
    fs.rmSync(dir, { recursive: true, force: true });
  });

  const worktree = (branch: string): string => path.join(trees, sanitizeBranch(branch));

  it('is undefined when no sibling matches', async () => {
    await expect(findWorkspaceFile(worktree('lonely'), 'lonely')).resolves.toBeUndefined();
  });

  it('finds the unprefixed sibling', async () => {
    const file = path.join(trees, 'fix-login.code-workspace');
    fs.writeFileSync(file, '{}');
    await expect(findWorkspaceFile(worktree('fix-login'), 'fix-login')).resolves.toBe(file);
  });

  it('prefers the exact name over a prefixed one', async () => {
    fs.writeFileSync(path.join(trees, 'myapp-fix-login.code-workspace'), '{}');
    await expect(findWorkspaceFile(worktree('fix-login'), 'fix-login')).resolves.toBe(
      path.join(trees, 'fix-login.code-workspace'),
    );
  });

  it('finds a prefixed sibling when it is the only match', async () => {
    const file = path.join(trees, 'myapp-solo.code-workspace');
    fs.writeFileSync(file, '{}');
    await expect(findWorkspaceFile(worktree('solo'), 'solo')).resolves.toBe(file);
  });

  it('sanitizes the branch before matching', async () => {
    const file = path.join(trees, 'feature-login.code-workspace');
    fs.writeFileSync(file, '{}');
    await expect(findWorkspaceFile(worktree('feature/login'), 'feature/login')).resolves.toBe(file);
  });

  it('ignores a directory with the matching name', async () => {
    fs.mkdirSync(path.join(trees, 'dirlike.code-workspace'));
    await expect(findWorkspaceFile(worktree('dirlike'), 'dirlike')).resolves.toBeUndefined();
  });

  it('is undefined without a branch, rather than guessing from the basename', async () => {
    await expect(findWorkspaceFile(worktree('fix-login'), undefined)).resolves.toBeUndefined();
  });

  it('is undefined when the parent directory does not exist', async () => {
    await expect(findWorkspaceFile(path.join(dir, 'nope', 'x'), 'x')).resolves.toBeUndefined();
  });
});

function sampleConfig(worktreeDir: string): EffectiveConfig {
  return {
    config_file: { path: '/home/dev/.th/config.json', exists: true, from_env: false },
    settings: {
      worktree_dir: worktreeDir,
      default_base: '',
      branch_prefix: '',
      prefix_separator: '/',
      copy_hooks: false,
      copy_files: [],
      link_files: [],
      vscode: {
        open: false,
        workspace_file: true,
        workspace_prefix: '',
        window_title: '',
        window_color: '',
        workspace_paths: [],
      },
      full_paths: false,
      auto_cd: true,
      pre_create: [],
      post_create: [],
      pre_remove: [],
      post_remove: [],
      run: '',
    },
    sources: {},
  };
}

describe('EffectiveConfigCache', () => {
  const repoA = { key: 'a', cwd: '/repo/a' };
  const repoB = { key: 'b', cwd: '/repo/b' };

  it('calls the loader once per repo key', async () => {
    const load = vi.fn(async (cwd: string) => sampleConfig(`${cwd}/{branch}`));
    const cache = new EffectiveConfigCache(load);

    await expect(cache.get(repoA)).resolves.toMatchObject({ settings: { worktree_dir: '/repo/a/{branch}' } });
    await cache.get(repoA);
    await cache.get(repoB);
    await cache.get(repoB);

    expect(load).toHaveBeenCalledTimes(2);
    expect(load).toHaveBeenCalledWith('/repo/a');
    expect(load).toHaveBeenCalledWith('/repo/b');
  });

  it('shares one in-flight load between concurrent callers', async () => {
    const load = vi.fn(async () => sampleConfig('/trees/{branch}'));
    const cache = new EffectiveConfigCache(load);

    await Promise.all([cache.get(repoA), cache.get(repoA), cache.get(repoA)]);
    expect(load).toHaveBeenCalledTimes(1);
  });

  it('refetches after clear()', async () => {
    const load = vi.fn(async () => sampleConfig('/trees/{branch}'));
    const cache = new EffectiveConfigCache(load);

    await cache.get(repoA);
    cache.clear();
    await cache.get(repoA);
    expect(load).toHaveBeenCalledTimes(2);
  });

  it('clear(key) drops only that repo', async () => {
    const load = vi.fn(async () => sampleConfig('/trees/{branch}'));
    const cache = new EffectiveConfigCache(load);

    await cache.get(repoA);
    await cache.get(repoB);
    cache.clear(repoA.key);
    await cache.get(repoA);
    await cache.get(repoB);
    expect(load).toHaveBeenCalledTimes(3);
  });

  it('turns a loader failure into undefined and logs it, without retrying until cleared', async () => {
    const load = vi.fn(async () => {
      throw new Error('unknown flag: --json');
    });
    const logged: string[] = [];
    const cache = new EffectiveConfigCache(load, (message) => logged.push(message));

    await expect(cache.get(repoA)).resolves.toBeUndefined();
    await expect(cache.get(repoA)).resolves.toBeUndefined();
    expect(load).toHaveBeenCalledTimes(1);
    expect(logged.join('\n')).toContain('unknown flag: --json');
  });

  it('retries a previously failed repo after clear()', async () => {
    let fail = true;
    const load = vi.fn(async () => {
      if (fail) {
        throw new Error('unknown flag: --json');
      }
      return sampleConfig('/trees/{branch}');
    });
    const cache = new EffectiveConfigCache(load);

    await expect(cache.get(repoA)).resolves.toBeUndefined();
    fail = false;
    cache.clear();
    await expect(cache.get(repoA)).resolves.toMatchObject({ settings: { worktree_dir: '/trees/{branch}' } });
    expect(load).toHaveBeenCalledTimes(2);
  });

  it('rejects a document that is not an effective config', async () => {
    const load = vi.fn(async () => ({ nope: true }) as unknown as EffectiveConfig);
    const cache = new EffectiveConfigCache(load);
    await expect(cache.get(repoA)).resolves.toBeUndefined();
  });
});

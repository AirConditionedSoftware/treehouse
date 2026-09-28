import { describe, expect, it } from 'vitest';
import type { RepoNode, WorktreeNode } from '../../worktreeTree';
import { contextValue, describe as describeEntry, mainWorktreeFor } from '../../worktreeTree';
import type { WorktreeEntry } from '../../types';

const repo: RepoNode = {
  kind: 'repo',
  key: '/repo/myapp',
  name: 'myapp',
  mainPath: '/repo/myapp',
  commonDir: '/repo/myapp/.git',
  cwd: '/repo/myapp',
};

function node(entry: WorktreeEntry, flags: Partial<Pick<WorktreeNode, 'isMain' | 'isCurrent'>> = {}): WorktreeNode {
  const isMain = flags.isMain ?? false;
  const isCurrent = flags.isCurrent ?? false;
  return { kind: 'worktree', repo, entry, isMain, isCurrent, removable: !isMain && !isCurrent };
}

const plain: WorktreeEntry = { path: '/repo/worktrees/myapp/fix-login', branch: 'fix-login' };

describe('contextValue', () => {
  it('is bare `worktree` for a plain, non-current linked worktree that is removable', () => {
    expect(contextValue(node(plain))).toBe('worktree.removable');
  });

  it('marks the main worktree and never calls it removable', () => {
    expect(contextValue(node(plain, { isMain: true }))).toBe('worktree.main');
  });

  it('marks the current worktree and never calls it removable', () => {
    expect(contextValue(node(plain, { isCurrent: true }))).toBe('worktree.current');
  });

  it('is neither main nor current before it is removable', () => {
    expect(contextValue(node(plain, { isMain: true, isCurrent: true }))).toBe('worktree.main.current');
  });

  it('emits the tokens in a fixed order', () => {
    const entry: WorktreeEntry = { ...plain, dirty: 3, locked: true, prunable: true, detached: true };
    expect(contextValue(node(entry, { isCurrent: true }))).toBe(
      'worktree.current.dirty.locked.prunable.detached',
    );
    expect(contextValue(node(entry))).toBe('worktree.removable.dirty.locked.prunable.detached');
  });

  it('treats a clean or unknown dirty count as not dirty', () => {
    expect(contextValue(node({ ...plain, dirty: 0 }))).toBe('worktree.removable');
    expect(contextValue(node(plain))).toBe('worktree.removable');
  });

  it('matches the menu when-clause regexes', () => {
    const removable = contextValue(node({ ...plain, locked: true }));
    expect(/^worktree\b/.test(removable)).toBe(true);
    expect(/\.removable\b/.test(removable)).toBe(true);
    expect(/\.removable\b/.test(contextValue(node(plain, { isMain: true })))).toBe(false);
  });
});

describe('describe', () => {
  it('shows the branch in brackets', () => {
    expect(describeEntry(plain, false)).toBe('[fix-login]');
  });

  it('tags the main worktree', () => {
    expect(describeEntry({ ...plain, branch: 'main' }, true)).toBe('[main]  main');
  });

  it('shows a bare repository as such', () => {
    expect(describeEntry({ path: '/repo/bare.git', bare: true }, true)).toBe('(bare)  main');
  });

  it('shows a short head for a detached worktree', () => {
    expect(describeEntry({ path: '/x', detached: true, head: '77560d77d9579f0b55455174ca00928dc4239507' }, false)).toBe(
      '(detached 77560d7)',
    );
  });

  it('shows ahead and behind', () => {
    expect(describeEntry({ ...plain, ahead: 2, behind: 1 }, false)).toBe('[fix-login]  ↑2 ↓1');
    expect(describeEntry({ ...plain, ahead: 2 }, false)).toBe('[fix-login]  ↑2');
    expect(describeEntry({ ...plain, behind: 1 }, false)).toBe('[fix-login]  ↓1');
  });

  it('replaces ahead/behind with `upstream gone`', () => {
    expect(describeEntry({ ...plain, ahead: 2, behind: 1, upstream_gone: true }, false)).toBe(
      '[fix-login]  upstream gone',
    );
  });

  it('counts changes, singular and plural', () => {
    expect(describeEntry({ ...plain, dirty: 1 }, false)).toBe('[fix-login]  1 change');
    expect(describeEntry({ ...plain, dirty: 3 }, false)).toBe('[fix-login]  3 changes');
  });

  it('says nothing about facts th did not report', () => {
    // th < 0.5.0 sends neither key; a clean worktree sends dirty: 0.
    expect(describeEntry(plain, false)).toBe('[fix-login]');
    expect(describeEntry({ ...plain, dirty: 0 }, false)).toBe('[fix-login]');
    expect(describeEntry({ ...plain, merged: false }, false)).toBe('[fix-login]');
  });

  it('shows merged, locked and prunable', () => {
    expect(describeEntry({ ...plain, merged: true }, false)).toBe('[fix-login]  merged');
    expect(describeEntry({ ...plain, locked: true }, false)).toBe('[fix-login]  locked');
    expect(describeEntry({ ...plain, prunable: true }, false)).toBe('[fix-login]  prunable');
  });

  it('orders every segment the way th list does', () => {
    const entry: WorktreeEntry = {
      ...plain,
      ahead: 2,
      behind: 1,
      dirty: 4,
      merged: true,
      locked: true,
      prunable: true,
    };
    expect(describeEntry(entry, true)).toBe('[fix-login]  main  ↑2 ↓1  4 changes  merged  locked  prunable');
  });

  it('is empty when there is nothing to say', () => {
    expect(describeEntry({ path: '/x' }, false)).toBe('');
  });
});

describe('mainWorktreeFor', () => {
  it('drops the .git directory a main worktree reports', () => {
    expect(mainWorktreeFor('/repo/myapp/.git')).toBe('/repo/myapp');
  });

  it('ignores a trailing separator', () => {
    expect(mainWorktreeFor('/repo/myapp/.git/')).toBe('/repo/myapp');
  });

  it('leaves a bare repository alone', () => {
    expect(mainWorktreeFor('/repo/myapp.git')).toBe('/repo/myapp.git');
  });

  it('normalizes the path it is given', () => {
    expect(mainWorktreeFor('/repo/myapp/./.git')).toBe('/repo/myapp');
    expect(mainWorktreeFor('/repo//myapp/.git')).toBe('/repo/myapp');
  });
});

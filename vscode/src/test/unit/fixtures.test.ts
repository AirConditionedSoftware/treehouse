import { describe, expect, it } from 'vitest';
import type { EffectiveConfig, RemoveResult, WorktreeEntry } from '../../types';
import configEffectiveFixture from '../fixtures/config-effective.json';
import listOldThFixture from '../fixtures/list-old-th.json';
import listFixture from '../fixtures/list.json';
import removeKeptFixture from '../fixtures/remove-kept-not-merged.json';
import removeMissingFixture from '../fixtures/remove-missing.json';
import removeSuccessFixture from '../fixtures/remove-success.json';
import { at, hasKey } from './support';

/**
 * TypeScript widens every string in an imported JSON module to `string`, so the
 * two fields `RemoveResult` narrows to a union can't be checked at the type
 * level. They are asserted against the union members at runtime below instead.
 */
type CapturedRemoveResult = Omit<RemoveResult, 'branch_action' | 'branch_reason'> & {
  branch_action?: string;
  branch_reason?: string;
};

const BRANCH_ACTIONS = ['deleted', 'kept'];
const BRANCH_REASONS = ['default_branch', 'not_fully_merged', 'declined', 'delete_failed', 'forced'];

// The type-level half of this suite: a captured document that stopped fitting
// the interface `th.ts` casts to would fail `npm run typecheck`, not a test.
// The annotation is what the assertions below read through — `satisfies` keeps
// the check honest if the annotation is ever loosened.
const list: WorktreeEntry[] = listFixture satisfies WorktreeEntry[];
const listOldTh: WorktreeEntry[] = listOldThFixture satisfies WorktreeEntry[];
const removeSuccess: CapturedRemoveResult[] = removeSuccessFixture satisfies CapturedRemoveResult[];
const removeKept: CapturedRemoveResult[] = removeKeptFixture satisfies CapturedRemoveResult[];
const removeMissing: CapturedRemoveResult[] = removeMissingFixture satisfies CapturedRemoveResult[];
const configEffective: EffectiveConfig = configEffectiveFixture satisfies EffectiveConfig;

describe('list --json fixture', () => {
  it('puts the main worktree first, as gitx.ListWorktrees guarantees', () => {
    expect(at(list, 0).branch).toBe('main');
    expect(at(list, 0).path).toBe('/repo/myapp');
  });

  it('reports dirty as a number on every non-bare worktree', () => {
    for (const entry of list) {
      expect(hasKey(entry, 'dirty')).toBe(true);
      expect(typeof entry.dirty).toBe('number');
    }
    expect(list.find((entry) => entry.branch === 'dirty-work')?.dirty).toBe(1);
    expect(list.find((entry) => entry.branch === 'spare-clean')?.dirty).toBe(0);
  });

  it('omits merged on the default branch and reports it as a boolean elsewhere', () => {
    const main = at(list, 0);
    expect(hasKey(main, 'merged')).toBe(false);
    expect(main.merged).toBeUndefined();

    for (const entry of list.slice(1)) {
      expect(hasKey(entry, 'merged')).toBe(true);
      expect(typeof entry.merged).toBe('boolean');
    }
    expect(list.find((entry) => entry.branch === 'done-work')?.merged).toBe(true);
    expect(list.find((entry) => entry.branch === 'feature/login')?.merged).toBe(false);
  });

  it('carries the lock reason git reported', () => {
    const locked = list.find((entry) => entry.locked === true);
    expect(locked?.branch).toBe('feature/login');
    expect(locked?.locked_reason).toBe('pinned for review');
  });
});

describe('list --json fixture from th < 0.5.0', () => {
  it('has neither dirty nor merged on any entry', () => {
    expect(listOldTh.length).toBeGreaterThan(0);
    for (const entry of listOldTh) {
      expect(hasKey(entry, 'dirty')).toBe(false);
      expect(hasKey(entry, 'merged')).toBe(false);
      expect(entry.dirty).toBeUndefined();
      expect(entry.merged).toBeUndefined();
    }
  });
});

describe('remove --json fixtures', () => {
  it('reports a plain success with the branch kept', () => {
    expect(removeSuccess).toHaveLength(1);
    const result = at(removeSuccess, 0);
    expect(result.target).toBe('spare-clean');
    expect(result.removed).toBe(true);
    expect(result.branch_action).toBe('kept');
    expect(hasKey(result, 'branch_reason')).toBe(false);
    expect(hasKey(result, 'error')).toBe(false);
  });

  it('names not_fully_merged when --delete-branch could not delete', () => {
    const result = at(removeKept, 0);
    expect(result.removed).toBe(true);
    expect(result.branch_action).toBe('kept');
    expect(result.branch_reason).toBe('not_fully_merged');
  });

  it('still emits a document for every target when one fails', () => {
    expect(removeMissing).toHaveLength(2);
    expect(at(removeMissing, 0).removed).toBe(true);
    const failed = at(removeMissing, 1);
    expect(failed.target).toBe('no-such-branch');
    expect(failed.removed).toBe(false);
    expect(failed.error).toContain('no worktree found');
    expect(hasKey(failed, 'path')).toBe(false);
  });

  it('only uses branch_action/branch_reason values the union declares', () => {
    for (const result of [...removeSuccess, ...removeKept, ...removeMissing]) {
      if (result.branch_action !== undefined) {
        expect(BRANCH_ACTIONS).toContain(result.branch_action);
      }
      if (result.branch_reason !== undefined) {
        expect(BRANCH_REASONS).toContain(result.branch_reason);
      }
    }
  });
});

describe('config --effective --json fixture', () => {
  it('describes where the config came from', () => {
    expect(configEffective.config_file.exists).toBe(true);
    expect(configEffective.config_file.from_env).toBe(true);
    expect(configEffective.repo?.name).toBe('myapp');
    expect(configEffective.repo?.repos_index).toBe(0);
    expect(configEffective.repo?.local_file).toBe('/repo/myapp/.thrc');
  });

  it('resolves every pointer setting to a concrete value', () => {
    const { settings } = configEffective;
    expect(typeof settings.copy_hooks).toBe('boolean');
    expect(typeof settings.auto_cd).toBe('boolean');
    expect(typeof settings.full_paths).toBe('boolean');
    expect(typeof settings.vscode.open).toBe('boolean');
    expect(typeof settings.vscode.workspace_file).toBe('boolean');
    // The lazy default the table applies, not the empty string it is stored as.
    expect(settings.prefix_separator).toBe('/');
  });

  it('labels each setting with the layer it came from', () => {
    const { sources } = configEffective;
    expect(sources['worktree_dir']).toBe('top-level');
    expect(sources['default_base']).toBe('repos[0]');
    expect(sources['branch_prefix']).toBe('.thrc');
    expect(sources['copy_hooks']).toBe('default');
    expect(sources['vscode.window_color']).toBe('repos[0]');
  });

  it('gives every settings leaf a source', () => {
    const leaves = Object.keys(configEffective.settings).flatMap((key) =>
      key === 'vscode' ? Object.keys(configEffective.settings.vscode).map((sub) => `vscode.${sub}`) : [key],
    );
    expect(Object.keys(configEffective.sources).sort()).toEqual(leaves.sort());
  });
});

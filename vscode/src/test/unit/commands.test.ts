import { describe, expect, it } from 'vitest';
import { applyPrefix } from '../../commands/add';
import { isPrRef, prWarnings } from '../../commands/addPr';
import { fixtureText } from './support';

describe('isPrRef', () => {
  it('accepts the three forms forge.ParsePRArg understands', () => {
    expect(isPrRef('12')).toBe(true);
    expect(isPrRef('#12')).toBe(true);
    expect(isPrRef('https://github.com/owner/repo/pull/12')).toBe(true);
  });

  it('tolerates surrounding whitespace and URL tails', () => {
    expect(isPrRef('  #12  ')).toBe(true);
    expect(isPrRef('https://github.com/owner/repo/pull/12/files')).toBe(true);
    expect(isPrRef('https://github.com/owner/repo/pull/12#issuecomment-1')).toBe(true);
    expect(isPrRef('https://github.com/owner/repo/pull/12?w=1')).toBe(true);
  });

  it('rejects anything else', () => {
    expect(isPrRef('abc')).toBe(false);
    expect(isPrRef('')).toBe(false);
    expect(isPrRef('#')).toBe(false);
    expect(isPrRef('12abc')).toBe(false);
    expect(isPrRef('##12')).toBe(false);
    // GitLab merge requests are not a forge th supports.
    expect(isPrRef('https://gitlab.com/owner/repo/-/merge_requests/12')).toBe(false);
    // A GitHub issue is not a pull request.
    expect(isPrRef('https://github.com/owner/repo/issues/12')).toBe(false);
  });
});

describe('prWarnings', () => {
  it('surfaces the gh fallback line', () => {
    expect(prWarnings(fixtureText('stderr/pr-gh-unavailable.txt'))).toEqual([
      'gh unavailable for PR #12 (exec: "gh": executable file not found in $PATH); falling back to plain git',
    ]);
  });

  it('surfaces a merged or closed PR', () => {
    expect(prWarnings(fixtureText('stderr/pr-merged.txt'))).toEqual(['Warning: PR #12 is merged']);
    expect(prWarnings('Warning: PR #7 is closed\n')).toEqual(['Warning: PR #7 is closed']);
  });

  it('ignores ordinary progress output', () => {
    expect(prWarnings('Creating worktree with new branch "fix-login" from origin/main\n')).toEqual([]);
    expect(prWarnings('')).toEqual([]);
    // Not one of the two lines the plan calls out.
    expect(prWarnings('Warning: PR #7 is draft\n')).toEqual([]);
  });
});

describe('applyPrefix', () => {
  // Mirrors internal/cmd/add.go: the prefix names branches that do not exist yet.
  it('prepends the effective prefix', () => {
    expect(applyPrefix('fix-login', 'peter/')).toBe('peter/fix-login');
  });

  it('never doubles a prefix the branch already carries', () => {
    expect(applyPrefix('peter/fix-login', 'peter/')).toBe('peter/fix-login');
  });

  it('is a no-op without a prefix', () => {
    expect(applyPrefix('fix-login', '')).toBe('fix-login');
  });

  it('keeps slashes inside the branch name', () => {
    expect(applyPrefix('feature/login', 'peter/')).toBe('peter/feature/login');
  });
});

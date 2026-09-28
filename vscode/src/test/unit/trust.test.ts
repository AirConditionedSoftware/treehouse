import { describe, expect, it } from 'vitest';
import { parseTrustWarnings } from '../../trust';
import { fixtureText } from './support';

describe('parseTrustWarnings', () => {
  it('pulls every skipped hook and its file out of a real th add run', () => {
    const { hooks, files } = parseTrustWarnings(fixtureText('stderr/trust-warning.txt'));
    // Warned in the order th reaches them: pre_create before post_create.
    expect(hooks).toEqual(['pre_create', 'post_create']);
    // One .thrc, two warnings — the file is reported once.
    expect(files).toEqual(['/repo/myapp/.thrc']);
  });

  it('reads a single warning', () => {
    const stderr = 'Warning: post_remove from ~/src/myapp/.thrc is not approved; skipping (run th remove interactively to review).\n';
    expect(parseTrustWarnings(stderr)).toEqual({ hooks: ['post_remove'], files: ['~/src/myapp/.thrc'] });
  });

  it('keeps distinct files apart', () => {
    const stderr = [
      'Warning: post_create from ~/src/a/.thrc is not approved; skipping (run th add interactively to review).',
      'Warning: post_create from ~/src/b/.thrc is not approved; skipping (run th add interactively to review).',
    ].join('\n');
    expect(parseTrustWarnings(stderr)).toEqual({
      hooks: ['post_create'],
      files: ['~/src/a/.thrc', '~/src/b/.thrc'],
    });
  });

  it('tolerates a path with spaces', () => {
    const stderr =
      'Warning: run from ~/src/my app/.thrc is not approved; skipping (run th run interactively to review).\n';
    expect(parseTrustWarnings(stderr)).toEqual({ hooks: ['run'], files: ['~/src/my app/.thrc'] });
  });

  it('finds nothing in ordinary stderr', () => {
    expect(parseTrustWarnings('Creating worktree with new branch "fix" from main\n')).toEqual({
      hooks: [],
      files: [],
    });
    expect(parseTrustWarnings('')).toEqual({ hooks: [], files: [] });
  });
});

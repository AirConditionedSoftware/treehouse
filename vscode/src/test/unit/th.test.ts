import { describe, expect, it } from 'vitest';
import { MIN_VERSION, compareVersions, hasTrustWarning, parseThErrorLine, parseVersion } from '../../th';
import { fixtureText } from './support';

describe('parseThErrorLine', () => {
  it('takes the th: line root.go prints', () => {
    expect(parseThErrorLine(fixtureText('stderr/th-error.txt'))).toBe(
      'no worktree found for "nonexistent-branch" (see th list)',
    );
  });

  it('prefers the last th: line when progress came before it', () => {
    const stderr = [
      'th: an earlier complaint',
      '[1/2] Removing /repo/worktrees/myapp/done-work (6 B)',
      'th: no worktree found for "no-such-branch" (see th list)',
      '',
    ].join('\n');
    expect(parseThErrorLine(stderr)).toBe('no worktree found for "no-such-branch" (see th list)');
  });

  it('falls back to the last non-empty line', () => {
    expect(parseThErrorLine('Removing /repo/worktrees/myapp/x\nfatal: not a git repository\n\n')).toBe(
      'fatal: not a git repository',
    );
  });

  it('handles CRLF line endings', () => {
    expect(parseThErrorLine('noise\r\nth: something broke\r\n')).toBe('something broke');
  });

  it('is undefined for empty stderr', () => {
    expect(parseThErrorLine('')).toBeUndefined();
    expect(parseThErrorLine('\n  \n\t\n')).toBeUndefined();
  });
});

describe('hasTrustWarning', () => {
  it('matches the warning hooks.go prints off-TTY', () => {
    expect(hasTrustWarning(fixtureText('stderr/trust-warning.txt'))).toBe(true);
  });

  it('is false for ordinary stderr', () => {
    expect(hasTrustWarning('Creating worktree with new branch "fix-login" from main\n')).toBe(false);
    expect(hasTrustWarning('')).toBe(false);
  });
});

describe('parseVersion', () => {
  it('reads cobra’s version line', () => {
    expect(parseVersion('th version 0.5.0\n')).toBe('0.5.0');
    expect(parseVersion('th version 1.12.3\n')).toBe('1.12.3');
  });

  it('keeps a pre-release suffix', () => {
    expect(parseVersion('th version 0.5.0-rc.1\n')).toBe('0.5.0-rc.1');
  });

  it('is undefined for a dev build or garbage', () => {
    // A `go build` with no ldflags stamps this.
    expect(parseVersion('th version dev\n')).toBeUndefined();
    expect(parseVersion('command not found: th')).toBeUndefined();
    expect(parseVersion('')).toBeUndefined();
  });
});

describe('compareVersions against MIN_VERSION', () => {
  it('pins the minimum the extension gates on', () => {
    expect(MIN_VERSION).toBe('0.5.0');
  });

  it('accepts the minimum and anything above it', () => {
    expect(compareVersions('0.5.0', MIN_VERSION)).toBe(0);
    expect(compareVersions('0.5.1', MIN_VERSION)).toBe(1);
    expect(compareVersions('0.12.0', MIN_VERSION)).toBe(1);
    expect(compareVersions('1.0.0', MIN_VERSION)).toBe(1);
  });

  it('rejects anything below it', () => {
    expect(compareVersions('0.4.0', MIN_VERSION)).toBe(-1);
    expect(compareVersions('0.4.9', MIN_VERSION)).toBe(-1);
    expect(compareVersions('0.0.1', MIN_VERSION)).toBe(-1);
  });

  it('compares the numeric prefix only, so a pre-release counts as its release', () => {
    expect(compareVersions('0.5.0-rc.1', '0.5.0')).toBe(0);
  });

  it('treats an unparseable version as 0.0.0', () => {
    expect(compareVersions('dev', MIN_VERSION)).toBe(-1);
  });

  it('orders minor above patch', () => {
    expect(compareVersions('0.10.0', '0.9.9')).toBe(1);
  });
});

import * as fs from 'fs';
import * as path from 'path';

/** The captured `th` output this suite pins its parsers against. */
export const fixturesDir = path.resolve(__dirname, '../fixtures');

/** A fixture read verbatim — used for the stderr captures, which are not JSON. */
export function fixtureText(name: string): string {
  return fs.readFileSync(path.join(fixturesDir, name), 'utf8');
}

/**
 * Indexing with `noUncheckedIndexedAccess` widens to `T | undefined`, and a
 * fixture that lost an entry should fail loudly rather than as a property access
 * on undefined three assertions later.
 */
export function at<T>(items: readonly T[], index: number): T {
  const value = items[index];
  if (value === undefined) {
    throw new Error(`expected an element at index ${index}, got ${items.length} element(s)`);
  }
  return value;
}

/** `Object.hasOwn` over an unknown-shaped record, for absent-key assertions. */
export function hasKey(value: object, key: string): boolean {
  return Object.prototype.hasOwnProperty.call(value, key);
}

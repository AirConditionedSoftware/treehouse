/**
 * A stand-in for the `vscode` module, aliased in by `vitest.config.ts`.
 *
 * The unit suite only exercises pure exports, but several of the modules that
 * hold them (`th.ts`, `trust.ts`, `worktreeTree.ts`, `commands/*`) sit in files
 * that `import * as vscode from 'vscode'` — a module that exists only inside the
 * extension host. This gives that import something to resolve to; it is
 * deliberately thin, and anything a test actually depends on belongs in the test,
 * not here.
 */

export class EventEmitter<T> {
  private readonly listeners: ((value: T) => void)[] = [];

  readonly event = (listener: (value: T) => void): { dispose(): void } => {
    this.listeners.push(listener);
    return {
      dispose: (): void => {
        const index = this.listeners.indexOf(listener);
        if (index >= 0) {
          this.listeners.splice(index, 1);
        }
      },
    };
  };

  fire(value: T): void {
    for (const listener of [...this.listeners]) {
      listener(value);
    }
  }

  dispose(): void {
    this.listeners.length = 0;
  }
}

export class CancellationError extends Error {
  constructor() {
    super('Canceled');
    this.name = 'CancellationError';
  }
}

export class ThemeColor {
  constructor(readonly id: string) {}
}

export class ThemeIcon {
  constructor(
    readonly id: string,
    readonly color?: ThemeColor,
  ) {}
}

export class MarkdownString {
  supportThemeIcons = false;
  constructor(public value = '') {}
}

export enum TreeItemCollapsibleState {
  None = 0,
  Collapsed = 1,
  Expanded = 2,
}

export class TreeItem {
  constructor(
    public label: string,
    public collapsibleState: TreeItemCollapsibleState = TreeItemCollapsibleState.None,
  ) {}
}

export const Uri = {
  file: (fsPath: string): { fsPath: string; scheme: string } => ({ fsPath, scheme: 'file' }),
};

export const workspace = {
  workspaceFolders: undefined as { uri: { fsPath: string; scheme: string } }[] | undefined,
  getConfiguration: (): { get<T>(key: string, fallback: T): T } => ({
    get: <T>(_key: string, fallback: T): T => fallback,
  }),
};

export const window = {
  createOutputChannel: (): { appendLine(line: string): void; dispose(): void } => ({
    appendLine: (): void => {},
    dispose: (): void => {},
  }),
};

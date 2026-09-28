import * as path from 'path';
import * as vscode from 'vscode';
import { logLine } from './log';
import type { RepoNode, WorktreeTreeProvider } from './worktreeTree';

const DEBOUNCE_MS = 300;
const FOCUS_INTERVAL_MS = 10_000;

/**
 * Every path to a refresh, funnelled through one debounce so a burst of
 * filesystem events costs one `th list` per repo.
 */
export class RefreshController implements vscode.Disposable {
  private timer: NodeJS.Timeout | undefined;
  private pending = new Set<RepoNode>();
  private pendingAll = false;
  private lastFocusRefresh = 0;
  private watchers: vscode.FileSystemWatcher[] = [];
  private readonly disposables: vscode.Disposable[] = [];

  constructor(private readonly provider: WorktreeTreeProvider) {}

  dispose(): void {
    if (this.timer) {
      clearTimeout(this.timer);
      this.timer = undefined;
    }
    this.disposeWatchers();
    for (const d of this.disposables) {
      d.dispose();
    }
    this.disposables.length = 0;
  }

  /** Refresh after the debounce window. `repo` omitted means the whole tree. */
  scheduleRefresh(repo?: RepoNode): void {
    if (repo) {
      this.pending.add(repo);
    } else {
      this.pendingAll = true;
    }
    if (this.timer) {
      clearTimeout(this.timer);
    }
    this.timer = setTimeout(() => {
      this.timer = undefined;
      this.flush();
    }, DEBOUNCE_MS);
  }

  /** Refresh right now — what a completed mutating command wants. */
  refreshNow(repo?: RepoNode): void {
    if (this.timer) {
      clearTimeout(this.timer);
      this.timer = undefined;
    }
    this.pending.clear();
    this.pendingAll = false;
    this.provider.refresh(repo);
    if (!repo) {
      void this.syncWatchers();
    }
  }

  /** Wire up the event sources. Call once from `activate`. */
  install(): vscode.Disposable {
    this.disposables.push(
      vscode.workspace.onDidChangeWorkspaceFolders(() => {
        this.scheduleRefresh();
      }),
    );

    this.disposables.push(
      vscode.window.onDidChangeWindowState((state) => {
        if (!state.focused) {
          return;
        }
        if (!vscode.workspace.getConfiguration('treehouse').get<boolean>('refreshOnFocus', true)) {
          return;
        }
        const now = Date.now();
        if (now - this.lastFocusRefresh < FOCUS_INTERVAL_MS) {
          return;
        }
        this.lastFocusRefresh = now;
        this.scheduleRefresh();
      }),
    );

    void this.syncWatchers();
    return this;
  }

  /**
   * One watcher per repo over `<common-dir>/worktrees/**`. Git writes a directory
   * there per linked worktree, so a `git worktree add` in a terminal shows up too.
   */
  async syncWatchers(): Promise<void> {
    const repos = await this.provider.repos();
    this.disposeWatchers();
    for (const repo of repos) {
      const root = vscode.Uri.file(path.join(repo.commonDir, 'worktrees'));
      const watcher = vscode.workspace.createFileSystemWatcher(new vscode.RelativePattern(root, '**'));
      const onEvent = (): void => this.scheduleRefresh(repo);
      watcher.onDidCreate(onEvent);
      watcher.onDidChange(onEvent);
      watcher.onDidDelete(onEvent);
      this.watchers.push(watcher);
      logLine(`  watching ${root.fsPath}`);
    }
  }

  private flush(): void {
    const all = this.pendingAll;
    const repos = [...this.pending];
    this.pendingAll = false;
    this.pending.clear();
    if (all) {
      this.provider.refresh();
      void this.syncWatchers();
      return;
    }
    for (const repo of repos) {
      this.provider.refresh(repo);
    }
  }

  private disposeWatchers(): void {
    for (const watcher of this.watchers) {
      watcher.dispose();
    }
    this.watchers = [];
  }
}

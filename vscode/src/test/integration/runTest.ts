import { execFileSync } from 'child_process';
import * as fs from 'fs';
import * as os from 'os';
import * as path from 'path';
import { runTests } from '@vscode/test-electron';

/**
 * A throwaway git repository for the extension host to open. It has to be a real
 * one: the manifest activates on `workspaceContains:.git`, and a bare directory
 * would never load the extension at all.
 */
function makeFixtureWorkspace(): string {
  const dir = fs.mkdtempSync(path.join(os.tmpdir(), 'treehouse-it-'));
  const git = (...args: string[]): void => {
    execFileSync('git', args, {
      cwd: dir,
      stdio: 'ignore',
      env: {
        ...process.env,
        GIT_CONFIG_GLOBAL: path.join(dir, 'gitconfig'),
        GIT_CONFIG_SYSTEM: '/dev/null',
        GIT_AUTHOR_NAME: 'Treehouse Tests',
        GIT_AUTHOR_EMAIL: 'tests@example.invalid',
        GIT_COMMITTER_NAME: 'Treehouse Tests',
        GIT_COMMITTER_EMAIL: 'tests@example.invalid',
      },
    });
  };

  fs.writeFileSync(path.join(dir, 'README.md'), '# fixture\n');
  git('init', '--quiet', '--initial-branch=main', '.');
  git('add', 'README.md');
  git('commit', '--quiet', '-m', 'init');

  // Point the extension at a specific `th` when one was built for this run;
  // without it the suite still asserts activation and command registration,
  // neither of which needs the binary.
  const binary = process.env['TREEHOUSE_TEST_TH'];
  if (binary) {
    fs.mkdirSync(path.join(dir, '.vscode'), { recursive: true });
    fs.writeFileSync(
      path.join(dir, '.vscode', 'settings.json'),
      JSON.stringify({ 'treehouse.path': binary }, null, 2) + '\n',
    );
  }
  return dir;
}

async function main(): Promise<void> {
  const extensionDevelopmentPath = path.resolve(__dirname, '../../../');
  const extensionTestsPath = path.resolve(__dirname, './index');
  const workspace = makeFixtureWorkspace();

  try {
    await runTests({
      extensionDevelopmentPath,
      extensionTestsPath,
      launchArgs: [
        workspace,
        '--disable-extensions',
        '--disable-gpu',
        // An untrusted workspace would hold the extension in its restricted
        // mode and never fire the activation event.
        '--disable-workspace-trust',
      ],
      extensionTestsEnv: { TREEHOUSE_TEST_WORKSPACE: workspace },
    });
  } finally {
    fs.rmSync(workspace, { recursive: true, force: true });
  }
}

main().catch((err: unknown) => {
  console.error('integration tests failed:', err);
  process.exit(1);
});

import * as assert from 'assert';
import * as vscode from 'vscode';

const EXTENSION_ID = 'AirConditionedSoftware.treehouse';

/** Every `treehouse.*` id the manifest contributes. */
const COMMAND_IDS = [
  'treehouse.refresh',
  'treehouse.addWorktree',
  'treehouse.addWorktreeFromPR',
  'treehouse.removeWorktree',
  'treehouse.openWorktree',
  'treehouse.openWorktreeInNewWindow',
  'treehouse.copyPath',
  'treehouse.revealInOS',
  'treehouse.openInTerminal',
  'treehouse.showOutput',
  'treehouse.openSettings',
];

suite('treehouse extension', () => {
  suiteSetup(async () => {
    const extension = vscode.extensions.getExtension(EXTENSION_ID);
    assert.ok(extension, `${EXTENSION_ID} is not installed in the test host`);
    await extension.activate();
  });

  test('opens the fixture workspace as a git repository', () => {
    const folders = vscode.workspace.workspaceFolders ?? [];
    assert.strictEqual(folders.length, 1, 'expected exactly one workspace folder');
  });

  test('activates', () => {
    const extension = vscode.extensions.getExtension(EXTENSION_ID);
    assert.ok(extension);
    assert.strictEqual(extension.isActive, true, 'extension did not activate');
  });

  test('contributes every command the manifest declares', () => {
    const extension = vscode.extensions.getExtension(EXTENSION_ID);
    assert.ok(extension);
    const contributed = (
      extension.packageJSON as { contributes?: { commands?: { command: string }[] } }
    ).contributes?.commands?.map((entry) => entry.command);
    assert.deepStrictEqual(contributed, COMMAND_IDS, 'manifest commands drifted from the test list');
  });

  test('registers every command with VS Code', async () => {
    const registered = new Set(await vscode.commands.getCommands(true));
    const missing = COMMAND_IDS.filter((id) => !registered.has(id));
    assert.deepStrictEqual(missing, [], `commands not registered: ${missing.join(', ')}`);
  });

  test('treehouse.refresh resolves', async () => {
    await vscode.commands.executeCommand('treehouse.refresh');
  });
});

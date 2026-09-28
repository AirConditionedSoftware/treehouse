import * as path from 'path';
import { defineConfig } from 'vitest/config';

export default defineConfig({
  resolve: {
    alias: {
      // `vscode` only exists inside the extension host; the unit suite gets a stub.
      vscode: path.resolve(import.meta.dirname, 'src/test/stubs/vscode.ts'),
    },
  },
  test: {
    include: ['src/test/unit/**/*.test.ts', 'src/**/*.test.ts'],
    // The integration suite is mocha inside a real VS Code — `npm run test:integration`.
    exclude: ['node_modules/**', 'dist/**', 'out/**', 'src/test/integration/**'],
    environment: 'node',
  },
});

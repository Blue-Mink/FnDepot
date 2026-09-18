import { defineConfig, devices } from '@playwright/test';
import { e2eMockApps, repoRoot } from './tests/e2e/fixture';

/**
 * E2E harness for the fnos-store frontend.
 *
 * Two servers, started in this order-independent pair:
 *   1. `make dev` at the repo root — the Go backend on :38011. On macOS it uses
 *      the mock appcenter (dev/mock-appcenter-cli.sh). APPS_DIR + MOCK_APPS_DIR
 *      redirect it to a disposable copy of dev/mock-apps so the destructive
 *      uninstall tests never touch the git-tracked fixture.
 *   2. `npm run dev` — Vite on :5173, which proxies /api to :38011.
 *
 * Tests target http://localhost:5173.
 *
 * workers: 1 / fullyParallel: false is deliberate — the backend is a single
 * stateful process whose OperationQueue serializes app operations anyway, and
 * uninstall mutates shared fixture state.
 */
export default defineConfig({
  testDir: './tests/e2e',
  globalSetup: './tests/e2e/global-setup.ts',
  fullyParallel: false,
  workers: 1,
  forbidOnly: !!process.env.CI,
  retries: 0,
  reporter: [['list']],
  timeout: 90_000,
  expect: { timeout: 15_000 },
  use: {
    baseURL: 'http://localhost:5173',
    trace: 'retain-on-failure',
    video: 'off',
  },
  projects: [
    { name: 'chromium', use: { ...devices['Desktop Chrome'] } },
  ],
  webServer: [
    {
      command: 'make dev',
      cwd: repoRoot,
      url: 'http://localhost:38011/api/status',
      reuseExistingServer: false,
      timeout: 120_000,
      stdout: 'pipe',
      stderr: 'pipe',
      env: {
        APPS_DIR: e2eMockApps,
        MOCK_APPS_DIR: e2eMockApps,
      },
    },
    {
      command: 'npm run dev',
      url: 'http://localhost:5173',
      reuseExistingServer: false,
      timeout: 120_000,
      stdout: 'pipe',
      stderr: 'pipe',
    },
  ],
});

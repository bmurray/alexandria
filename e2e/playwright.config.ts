import { defineConfig } from '@playwright/test'

// Runs against an already-running server: `make dev` (+ `make web-dev`) or
// the production binary with CONFIG_FILE=configs/e2e.pkl (what CI does).
// The server must have the Dev sign-in provider enabled. Set BASE_URL to
// override the target.
export default defineConfig({
  testDir: '.',
  testMatch: /.*\.spec\.ts/,
  timeout: 30_000,
  expect: { timeout: 10_000 },
  retries: 0,
  workers: 1, // one SQLite writer, one seeded catalog: keep tests ordered
  reporter: [['list'], ['html', { outputFolder: 'playwright-report', open: 'never' }]],
  use: {
    baseURL: process.env.BASE_URL ?? 'http://localhost:24380',
    trace: 'retain-on-failure',
  },
  projects: [{ name: 'chromium', use: { browserName: 'chromium' } }],
})

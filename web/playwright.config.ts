import { defineConfig, devices } from '@playwright/test'

export default defineConfig({
  testDir: './e2e',
  timeout: 30_000,
  expect: { timeout: 10_000 },
  fullyParallel: false,
  forbidOnly: !!process.env.CI,
  retries: process.env.CI ? 1 : 0,
  workers: 1,
  reporter: process.env.CI
    ? [['list'], ['html', { outputFolder: '../test/e2e/_artifacts/ui/report', open: 'never' }]]
    : [['list']],
  outputDir: '../test/e2e/_artifacts/ui/results',
  use: {
    baseURL: process.env.PGLENS_UI_BASE_URL ?? 'http://127.0.0.1:8080',
    trace: 'retain-on-failure',
    screenshot: 'only-on-failure',
    video: 'retain-on-failure',
    actionTimeout: 10_000,
    timezoneId: 'UTC',
    locale: 'en-US',
    colorScheme: 'dark',
  },
  projects: [
    // Legacy specs, run by test/e2e/ui_test.go with --grep and no --project.
    {
      name: 'chromium',
      use: { ...devices['Desktop Chrome'] },
      testMatch: /(smoke|ui_acceptance)\.spec\.ts/,
    },
    // Node-only QA harness and traceability checks: no browser.
    { name: 'harness', testMatch: /qa\/(harness|traceability)\.spec\.ts/ },
    ...(
      [
        ['desktop-fullhd', 1920, 1080],
        ['desktop-maximized', 1920, 950],
        ['laptop', 1440, 900],
        ['tablet', 768, 1024],
        ['phone', 390, 844],
      ] as const
    ).map(([name, width, height]) => ({
      name,
      use: { ...devices['Desktop Chrome'], viewport: { width, height } },
      testIgnore: [/(smoke|ui_acceptance)\.spec\.ts/, /qa\/(harness|traceability)\.spec\.ts/],
    })),
  ],
})

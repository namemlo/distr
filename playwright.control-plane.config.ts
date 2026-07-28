import {defineConfig, devices} from '@playwright/test';

export default defineConfig({
  testDir: './frontend/ui/e2e',
  testMatch: 'control-plane.spec.ts',
  fullyParallel: false,
  forbidOnly: Boolean(process.env['CI']),
  retries: process.env['CI'] ? 1 : 0,
  workers: 1,
  timeout: 30_000,
  expect: {
    timeout: 5_000,
  },
  outputDir: 'output/playwright/control-plane-artifacts',
  reporter: [
    ['line'],
    ['html', {outputFolder: 'output/playwright/control-plane-html', open: 'never'}],
    ['junit', {outputFile: 'output/playwright/control-plane-junit.xml'}],
  ],
  use: {
    ...devices['Desktop Chrome'],
    baseURL: 'http://127.0.0.1:4200',
    trace: 'retain-on-failure',
    screenshot: 'only-on-failure',
    video: 'retain-on-failure',
  },
  webServer: {
    command: 'pnpm exec ng serve --host 127.0.0.1 --port 4200',
    url: 'http://127.0.0.1:4200',
    reuseExistingServer: !process.env['CI'],
    timeout: 120_000,
  },
});

import {defineConfig} from '@playwright/test';
import baseConfig from './playwright.control-plane.config';

export default defineConfig({
  ...baseConfig,
  grep: /@evidence/,
  timeout: 90_000,
  outputDir: 'output/playwright/control-plane-evidence',
  reporter: [
    ['line'],
    ['html', {outputFolder: 'output/playwright/control-plane-evidence-html', open: 'never'}],
    ['junit', {outputFile: 'output/playwright/control-plane-evidence-junit.xml'}],
  ],
  projects: [{name: 'chromium'}],
  webServer: {
    command:
      'node hack/update-frontend-version.js && node hack/agent-changelog.mjs CHANGELOG.md frontend/ui/src/data/agent-changelog.json && node node_modules/@angular/cli/bin/ng.js serve --host 127.0.0.1 --port 4200',
    url: 'http://127.0.0.1:4200',
    reuseExistingServer: !process.env['CI'],
    timeout: 120_000,
  },
  use: {
    ...baseConfig.use,
    viewport: {width: 1440, height: 1200},
    locale: 'en-US',
    timezoneId: 'UTC',
    colorScheme: 'light',
    reducedMotion: 'reduce',
    trace: 'retain-on-failure',
    screenshot: 'only-on-failure',
    video: 'off',
  },
});

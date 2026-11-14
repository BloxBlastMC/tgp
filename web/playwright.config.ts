import { defineConfig } from '@playwright/test'

// E2E tests run against a real tgp-server binary (built with the UI
// embedded) that each spec file starts on a temp data dir. Build first:
//   make web build && cd web && pnpm e2e
export default defineConfig({
  testDir: 'e2e',
  fullyParallel: false,
  workers: 1,
  timeout: 60_000,
  reporter: [['list']],
  use: {
    headless: true,
    // TGP_THEME=light runs (and screenshots) the light theme.
    colorScheme: process.env.TGP_THEME === 'light' ? 'light' : 'dark',
    trace: 'retain-on-failure',
    launchOptions: process.env.CHROMIUM_PATH ? { executablePath: process.env.CHROMIUM_PATH } : {},
  },
})

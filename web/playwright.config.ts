/**
 * playwright.config.ts
 *
 * Playwright end-to-end test configuration for CatalogRO.
 *
 * DUAL-MODE SETUP
 * ───────────────
 * This config supports two modes of operation:
 *
 *  1. LOCAL DEV (default)
 *     - Developer must already have the app running via `make dev`
 *     - No webServer block — Playwright connects to localhost:3000 directly
 *     - retries = 0, so failures are reported immediately
 *
 *  2. CI (when process.env.CI is set, e.g. GitHub Actions)
 *     - Playwright spins up `npm run dev` automatically before running tests
 *     - retries = 2, giving transient network/timing failures a chance to pass
 *
 * HOW TO RUN
 * ──────────
 * Local:  make dev (in another terminal) → npx playwright test
 * CI:     CI=true npx playwright test   (webServer auto-started)
 */

import { defineConfig, devices } from '@playwright/test';

/**
 * Whether we are running inside a CI environment.
 * GitHub Actions, GitLab CI, CircleCI, etc. all set CI=true automatically.
 */
const isCI = Boolean(process.env['CI']);

export default defineConfig({
  // ── Test discovery ────────────────────────────────────────────────────────
  /**
   * testDir: where Playwright looks for *.spec.ts files.
   * All E2E tests live under web/test/e2e/
   */
  testDir: 'test/e2e',

  /**
   * outputDir: where Playwright writes test artifacts (screenshots, traces, videos).
   * This folder is .gitignored so it never ends up in version control.
   */
  outputDir: 'test/e2e/results',

  // ── Timing ────────────────────────────────────────────────────────────────
  /**
   * timeout: maximum time (ms) a single test can run before it is marked as failed.
   * 30 seconds is generous enough for page navigations in a dev SSR app.
   */
  timeout: 30_000,

  /**
   * expect.timeout: maximum time (ms) a single `expect(locator).toBeVisible()` etc.
   * will poll before failing. 5 seconds is the recommended Playwright default.
   */
  expect: {
    timeout: 5_000,
  },

  // ── Retry strategy ────────────────────────────────────────────────────────
  /**
   * retries:
   *   - 0 locally  → fail fast, developer sees the error immediately
   *   - 2 in CI    → tolerate flaky network or startup timing issues
   */
  retries: isCI ? 2 : 0,

  // ── Parallelism ───────────────────────────────────────────────────────────
  /**
   * workers: number of parallel test workers.
   * Set to 1 so tests run sequentially — important because:
   *   - The dev server only has one instance
   *   - Tests that create/modify data won't race each other
   */
  workers: 1,

  // ── Reporters ─────────────────────────────────────────────────────────────
  /**
   * reporter: how Playwright formats test output.
   *   - 'list'  → concise line-per-test output (good for both local & CI)
   *   - 'html'  → also generate an HTML report in playwright-report/ (CI only)
   */
  reporter: isCI ? [['list'], ['html']] : 'list',

  // ── Shared browser settings (applied to ALL projects/browsers) ────────────
  use: {
    /**
     * baseURL: the root URL that `page.goto('/')` resolves against.
     * Always localhost:3000 — the Nuxt dev server port.
     */
    baseURL: 'http://localhost:3000',

    /**
     * trace: when to capture a Playwright Trace (network, DOM snapshots, actions).
     * 'on-first-retry' → only record a trace when a test is being retried,
     * which keeps disk usage low while still giving diagnostic data for flakes.
     */
    trace: 'on-first-retry',

    /**
     * screenshot: when to save a PNG screenshot of the browser viewport.
     * 'only-on-failure' → only capture on failed tests to save disk space.
     */
    screenshot: 'only-on-failure',
  },

  // ── Browser projects ──────────────────────────────────────────────────────
  /**
   * projects: each entry runs the entire test suite in a different browser.
   * We only run Chromium to keep CI fast and the setup minimal.
   * Firefox / WebKit can be added later if cross-browser coverage is needed.
   */
  projects: [
    {
      name: 'chromium',
      use: { ...devices['Desktop Chrome'] },
    },
  ],

  // ── Web server (CI only) ──────────────────────────────────────────────────
  /**
   * webServer: Playwright will start this command before running tests,
   * wait for localhost:3000 to respond, then tear it down afterwards.
   *
   * Only active in CI — locally the developer is expected to have `make dev`
   * already running.
   *
   * reuseExistingServer: false in CI so we always start a clean server,
   * but true locally (as a fallback) so Playwright reuses an already-running dev server.
   */
  ...(isCI
    ? {
        webServer: {
          command: 'npm run dev',
          url: 'http://localhost:3000',
          timeout: 120_000, // 2 minutes — Nuxt SSR startup can be slow on first run
          reuseExistingServer: false,
        },
      }
    : {}),
});

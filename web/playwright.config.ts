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
   * globalSetup: runs once before any test.
   * Resets the database (drop + create + migrate + seed) and waits for
   * API + Nuxt servers to be healthy. See test/e2e/global-setup.ts.
   */
  globalSetup: './test/e2e/global-setup.ts',

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
   * projects: ordered groups that enforce a dependency chain so tests run in
   * a predictable, safe sequence.
   *
   * WHY ORDERED PROJECTS?
   * ─────────────────────
   * The test suite mutates shared database state (grades, absences, students).
   * Running tests in the wrong order causes false failures — e.g. a write test
   * might run before the auth token it depends on has been provisioned, or a
   * read test might see stale data from a write that hasn't happened yet.
   *
   * Playwright's `dependencies` field guarantees that a project only starts
   * after all its listed dependencies have completed successfully.
   *
   * EXECUTION ORDER
   * ───────────────
   *   auth → read-tests → write-tests → integration-tests
   *
   *   auth              Login flows, token refresh, 2FA. No prior state assumed.
   *   read-tests        Dashboard, navigation, catalog views. Auth must succeed first.
   *   write-tests       Grade CRUD, edge cases. The catalog must be readable first.
   *   integration-tests Sync, error handling, edge cases. All writes must complete first.
   *
   * All projects target Desktop Chrome only — Firefox/WebKit can be added later.
   */
  projects: [
    {
      // ── auth ──────────────────────────────────────────────────────────────
      // Login, logout, token refresh, and 2FA tests.
      // These run first because every other project needs a working auth flow.
      name: 'auth',
      testMatch: ['**/auth/**'],
      use: { ...devices['Desktop Chrome'] },
    },
    {
      // ── read-tests ────────────────────────────────────────────────────────
      // Dashboard rendering, navigation flows, catalog page structure, and
      // the grade grid display — all read-only, no data mutations.
      // Depends on 'auth' so the session is valid when these tests run.
      name: 'read-tests',
      testMatch: [
        '**/dashboard/**',
        '**/navigation/**',
        '**/catalog/navigation*',
        '**/catalog/grade-grid*',
      ],
      dependencies: ['auth'],
      use: { ...devices['Desktop Chrome'] },
    },
    {
      // ── write-tests ───────────────────────────────────────────────────────
      // Grade creation, editing, deletion (CRUD), and grade edge cases.
      // Must run after read-tests so the catalog is confirmed to load correctly
      // before we start mutating grades.
      name: 'write-tests',
      testMatch: ['**/catalog/grade-crud*', '**/catalog/grade-edge*'],
      dependencies: ['read-tests'],
      use: { ...devices['Desktop Chrome'] },
    },
    {
      // ── integration-tests ─────────────────────────────────────────────────
      // Offline sync, API error handling, and cross-cutting edge cases.
      // These run last because they may leave the database in an unusual state
      // (e.g. sync conflicts, partial writes) that would confuse earlier tests.
      name: 'integration-tests',
      testMatch: ['**/sync/**', '**/error/**', '**/edge/**'],
      dependencies: ['write-tests'],
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

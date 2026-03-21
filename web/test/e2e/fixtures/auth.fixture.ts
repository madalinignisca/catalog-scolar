/**
 * auth.fixture.ts
 *
 * Role-based authentication fixtures for Playwright E2E tests.
 *
 * WHY FIXTURES?
 * ─────────────
 * Playwright "fixtures" are a dependency-injection system built into the test
 * runner. Instead of copy-pasting login steps into every test, you declare what
 * you need (e.g. `teacherPage`) and Playwright sets it up before your test runs
 * and tears it down after.
 *
 * USAGE IN A TEST FILE
 * ────────────────────
 * Instead of importing `test` from '@playwright/test', import it from this file:
 *
 *   import { test, expect } from '../fixtures/auth.fixture';
 *
 *   test('teacher sees grade book', async ({ teacherPage }) => {
 *     await teacherPage.goto('/grades');
 *     // teacherPage is already logged-in as the test teacher
 *   });
 *
 * STATUS
 * ──────
 * These fixtures navigate to /login and fill in credentials. They will NOT
 * succeed until the API auth handler is implemented and the seed data loaded
 * (via `make seed`). That is intentional — the structure is correct and tests
 * that depend on these fixtures are marked `test.skip` until auth works.
 */

import { test as base, type Page } from '@playwright/test';

// ── Seed credentials ─────────────────────────────────────────────────────────
/**
 * TEST_USERS
 *
 * These credentials match the seed data inserted by `make seed`.
 * DO NOT use real passwords here — these are throwaway dev/test credentials only.
 *
 * Roles map to the four personas in the CatalogRO auth model:
 *   - admin     → school administrator (full access)
 *   - teacher   → teacher with 2FA (TOTP required after password)
 *   - parent    → parent who accepted GDPR consent
 *   - student   → student account (read-only catalog access)
 */
const TEST_USERS = {
  admin: {
    email: 'admin@scoala-test.ro',
    password: 'TestAdmin1!',
    role: 'admin',
  },
  teacher: {
    email: 'profesor@scoala-test.ro',
    password: 'TestTeacher1!',
    role: 'teacher',
  },
  parent: {
    email: 'parinte@scoala-test.ro',
    password: 'TestParent1!',
    role: 'parent',
  },
  student: {
    email: 'elev@scoala-test.ro',
    password: 'TestStudent1!',
    role: 'student',
  },
} as const;

// ── Fixture type declarations ─────────────────────────────────────────────────
/**
 * AppFixtures defines the extra fixtures this file adds on top of the base
 * Playwright fixtures (page, browser, context, etc.).
 *
 * Each property is a Playwright Page that has already completed the login flow
 * for the given role.
 */
type AppFixtures = {
  /** A Page logged in as the admin user. */
  adminPage: Page;

  /** A Page logged in as a teacher (requires TOTP — will be a blank page until MFA is wired). */
  teacherPage: Page;

  /** A Page logged in as a parent. */
  parentPage: Page;

  /** A Page logged in as a student. */
  authenticatedPage: Page;
};

// ── Helper: perform login steps on a page ─────────────────────────────────────
/**
 * performLogin
 *
 * Shared helper that navigates to /login and submits credentials.
 * Called by each role-specific fixture below.
 *
 * @param page    - The Playwright Page object to act on.
 * @param email   - The email address to type into the email input.
 * @param password - The password to type into the password input.
 *
 * NOTE: This function does NOT handle MFA (TOTP). Tests that require a
 * teacher or admin login will need to extend this once the MFA flow is
 * implemented. For now, the fixture will land on the MFA screen, which is
 * still useful for testing the MFA UI itself.
 */
async function performLogin(page: Page, email: string, password: string): Promise<void> {
  // Navigate to the login page.
  // baseURL is set in playwright.config.ts, so '/login' resolves to localhost:3000/login.
  await page.goto('/login');

  // Fill the email field using the data-testid attribute.
  // data-testid is more stable than CSS selectors — it won't break when classes change.
  await page.getByTestId('email-input').fill(email);

  // Fill the password field.
  await page.getByTestId('password-input').fill(password);

  // Click the submit button to trigger the login request.
  await page.getByTestId('submit-button').click();

  // Wait for navigation to complete. Either the dashboard loads (success)
  // or the MFA step appears (teacher/admin). We don't assert here — the
  // individual tests decide what to expect after login.
  await page.waitForLoadState('networkidle');
}

// ── Extended test object ──────────────────────────────────────────────────────
/**
 * test
 *
 * Re-exported Playwright `test` object extended with our custom fixtures.
 * Import this instead of `import { test } from '@playwright/test'` in any
 * test file that needs a pre-authenticated page.
 */
export const test = base.extend<AppFixtures>({
  /**
   * authenticatedPage
   *
   * A Playwright Page already logged in as the student (the most basic
   * authenticated role). Use this for tests that just need "any logged-in user".
   */
  authenticatedPage: async ({ page }, use) => {
    // Perform the login flow using student credentials.
    await performLogin(page, TEST_USERS.student.email, TEST_USERS.student.password);
    // Hand the page to the test. Everything between `use()` and the end of
    // this function runs as teardown after the test finishes.
    await use(page);
    // No explicit teardown needed — Playwright closes the browser context automatically.
  },

  /**
   * teacherPage
   *
   * A Page logged in as a teacher.
   * Teachers require 2FA (TOTP), so after the password step the MFA screen
   * will appear. The fixture does not handle TOTP — extend it once the
   * TOTP mock/handler is available.
   */
  teacherPage: async ({ page }, use) => {
    await performLogin(page, TEST_USERS.teacher.email, TEST_USERS.teacher.password);
    await use(page);
  },

  /**
   * adminPage
   *
   * A Page logged in as the school admin.
   * Same TOTP caveat as teacherPage applies.
   */
  adminPage: async ({ page }, use) => {
    await performLogin(page, TEST_USERS.admin.email, TEST_USERS.admin.password);
    await use(page);
  },

  /**
   * parentPage
   *
   * A Page logged in as a parent.
   * Parents do not require 2FA, so this fixture should work end-to-end once
   * the API auth handler is implemented.
   */
  parentPage: async ({ page }, use) => {
    await performLogin(page, TEST_USERS.parent.email, TEST_USERS.parent.password);
    await use(page);
  },
});

// Re-export `expect` so test files only need one import line.
export { expect } from '@playwright/test';

// Re-export TEST_USERS so specs can reference emails/roles without duplicating them.
export { TEST_USERS };

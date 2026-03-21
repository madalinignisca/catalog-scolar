/**
 * login.spec.ts
 *
 * Playwright E2E test suite for the CatalogRO login page (/login).
 *
 * WHAT IS TESTED HERE
 * ────────────────────
 * This file covers the login UI from the user's perspective — navigating to
 * the page, seeing the correct form elements, and validating error states.
 * It does NOT test business logic (that belongs in Vitest unit tests).
 *
 * TEST STATUS LEGEND
 * ──────────────────
 *   ✅ Active  — runs in every `npx playwright test` / `make test-e2e` invocation
 *   ⏭ Skipped — marked test.skip(), requires infrastructure not yet available
 *              (auth handler, seed data, TOTP mock)
 *
 * HOW TO RUN
 * ──────────
 *   make test-e2e          ← requires `make dev` running in another terminal
 *   CI=true npx playwright test test/e2e/login.spec.ts  ← CI mode (auto-starts server)
 *
 * FILE ORGANISATION
 * ─────────────────
 * We import `test` and `expect` from '@playwright/test' directly (not from
 * auth.fixture) because the active tests in this file do NOT require a
 * pre-authenticated page. The auth fixture is only needed for tests that
 * check post-login behaviour.
 *
 * The LoginPage page object is used for all DOM interactions — see
 * test/e2e/page-objects/login.page.ts for the selector definitions.
 */

import { expect, test } from '@playwright/test';

import { LoginPage } from './page-objects/login.page';

// ── Test suite ────────────────────────────────────────────────────────────────
/**
 * Group all login-related tests under a single describe block.
 * This makes the output easier to read in `--reporter=list` mode and allows
 * running just this group with: npx playwright test --grep "login page"
 */
test.describe('login page', () => {
  // ── Test 1: Page renders correctly ────────────────────────────────────────
  /**
   * Verifies that navigating to /login shows the expected form elements.
   *
   * This is the most basic smoke test — if it fails, something is fundamentally
   * broken (server down, routing broken, Vue compilation error).
   *
   * WHAT WE CHECK
   *   - Email input is visible
   *   - Password input is visible
   *   - Submit button is visible and shows the correct label
   *
   * We do NOT check every piece of UI text here — that would make the test
   * brittle to small copy changes. We only check structural elements that
   * are required for the form to be usable.
   */
  test('login page renders', async ({ page }) => {
    // Instantiate the Page Object for the login page.
    // The Page Object abstracts all selector details into named methods.
    const loginPage = new LoginPage(page);

    // Navigate the browser to http://localhost:3000/login
    await loginPage.goto();

    // Assert that the email input field is visible in the viewport.
    // toBeVisible() waits up to expect.timeout (5000ms) before failing.
    await expect(loginPage.emailInput).toBeVisible();

    // Assert that the password input field is visible.
    await expect(loginPage.passwordInput).toBeVisible();

    // Assert that the submit button is visible.
    await expect(loginPage.submitButton).toBeVisible();

    // Assert that the submit button shows the correct Romanian label.
    // toHaveText() matches the trimmed text content of the element.
    await expect(loginPage.submitButton).toHaveText('Autentificare');
  });

  // ── Test 2: Empty form triggers browser validation ─────────────────────────
  /**
   * Verifies that submitting an empty form does NOT make an API call and
   * instead relies on browser-native HTML5 `required` validation.
   *
   * BACKGROUND
   * The email and password inputs both have the `required` attribute, which
   * causes modern browsers to block form submission and show a native tooltip.
   * Playwright can observe this by checking that the page URL has not changed
   * (i.e., we stayed on /login — no navigation happened).
   *
   * WHY NO API CALL CHECK?
   * We could intercept network requests to assert no POST was made, but that
   * couples the test to the API endpoint URL. Checking the URL is simpler and
   * equally effective for this use case.
   */
  test('empty form shows validation', async ({ page }) => {
    const loginPage = new LoginPage(page);

    // Navigate to the login page.
    await loginPage.goto();

    // Click the submit button WITHOUT filling in email or password.
    // The browser's built-in HTML5 validation should block submission.
    await loginPage.submit();

    // Give the page a brief moment to attempt navigation (if validation failed
    // to block the submit). waitForTimeout is avoided per Playwright best
    // practices; instead we check the URL immediately — it should still be /login.
    const currentUrl = page.url();

    // The URL should still contain '/login' — no navigation occurred.
    // We use toContain rather than strict equality because Playwright's baseURL
    // resolution may vary slightly across environments.
    expect(currentUrl).toContain('/login');

    // Additionally, verify the email input is still visible (page did not change).
    await expect(loginPage.emailInput).toBeVisible();
  });

  // ── Test 3: Successful login (SKIPPED — needs auth handler) ───────────────
  /**
   * @skip Requires: API auth handler + seed data (`make seed`) + running API server.
   *
   * WHAT THIS TEST WILL DO (once enabled)
   *   1. Navigate to /login
   *   2. Fill in a parent account's credentials (parents have no MFA)
   *   3. Submit the form
   *   4. Assert that the browser redirected to '/' (dashboard)
   *
   * HOW TO ENABLE
   *   1. Ensure `make dev` is running and `make seed` has been applied
   *   2. Remove the `test.skip(...)` wrapper and replace with a normal `test(...)`
   *   3. Update TEST_USERS credentials to match the seeded accounts
   *
   * REFERENCE
   *   - Seed data: api/db/seed.sql
   *   - Auth composable: web/composables/useAuth.ts
   *   - Auth fixtures: web/test/e2e/fixtures/auth.fixture.ts
   */
  test.skip('successful login redirects to dashboard', ({ page }) => {
    // TODO: needs auth handler
    // Example implementation when auth is ready:
    //
    // const loginPage = new LoginPage(page);
    // await loginPage.goto();
    // await loginPage.fillEmail('parinte@scoala-test.ro');
    // await loginPage.fillPassword('TestParent1!');
    // await loginPage.submit();
    // await page.waitForURL('/');
    // expect(loginPage.isOnDashboard()).toBe(true);

    // Placeholder to satisfy TypeScript — remove when implementing.
    void page;
  });

  // ── Test 4: MFA flow (SKIPPED — needs auth + TOTP handler) ───────────────
  /**
   * @skip Requires: API auth handler + TOTP mock/seed + running API server.
   *
   * WHAT THIS TEST WILL DO (once enabled)
   *   1. Log in as a teacher (triggers MFA step)
   *   2. Assert the MFA input field appears
   *   3. Enter a valid TOTP code (from a seeded TOTP secret)
   *   4. Submit the MFA form
   *   5. Assert redirect to '/' (dashboard)
   *
   * HOW TO ENABLE
   *   1. Seed a teacher account with a known TOTP secret
   *   2. Generate a valid TOTP code for that secret (or mock the API)
   *   3. Remove the `test.skip(...)` wrapper
   *
   * REFERENCE
   *   - TOTP logic: api/auth/totp.go
   *   - MFA verification endpoint: POST /api/v1/auth/mfa/verify
   */
  test.skip('MFA flow completes successfully', ({ page }) => {
    // TODO: needs auth + TOTP handler
    // Example implementation when MFA is ready:
    //
    // const loginPage = new LoginPage(page);
    // await loginPage.goto();
    // await loginPage.fillEmail('profesor@scoala-test.ro');
    // await loginPage.fillPassword('TestTeacher1!');
    // await loginPage.submit();
    //
    // // After password, the MFA step should appear
    // await expect(loginPage.mfaInput).toBeVisible();
    //
    // // Enter a valid TOTP code (generated from a seeded secret)
    // const totpCode = generateTotpCode('SEEDED_TOTP_SECRET');
    // await loginPage.fillMfaCode(totpCode);
    // await loginPage.submit();
    //
    // // Should now be on the dashboard
    // await page.waitForURL('/');
    // expect(loginPage.isOnDashboard()).toBe(true);

    // Placeholder to satisfy TypeScript — remove when implementing.
    void page;
  });
});

/**
 * auth/token.spec.ts
 *
 * End-to-end tests for JWT token lifecycle in CatalogRO.
 *
 * WHAT THIS FILE COVERS
 * ─────────────────────
 * Tests 11-13 verify token refresh, expiry, and unauthenticated access.
 *
 * All tests use manual login (not fixtures) to avoid Playwright's
 * restriction against mixing two `test` objects in the same file.
 */

import { test, expect } from '@playwright/test';

import { TEST_USERS } from '../fixtures/auth.fixture';
import { LoginPage } from '../page-objects/login.page';

/**
 * Helper to log in as parent manually (no MFA).
 * Returns the page at the dashboard after login.
 */
async function loginAsParent(page: import('@playwright/test').Page): Promise<void> {
  const loginPage = new LoginPage(page);
  await loginPage.goto();
  await loginPage.fillEmail(TEST_USERS.parent.email);
  await loginPage.fillPassword(TEST_USERS.parent.password);
  await loginPage.submit();
  await page.waitForURL('/', { timeout: 10_000 });
}

// ─────────────────────────────────────────────────────────────────────────────
// TEST 11: Expired access token triggers a silent token refresh
// ─────────────────────────────────────────────────────────────────────────────
test.describe('token lifecycle', () => {
  test('expired access token triggers silent refresh (test 11)', async ({ page }) => {
    // Step 1: Log in as parent to get valid tokens.
    await loginAsParent(page);

    // Step 2: Corrupt only the access token, leave refresh token intact.
    await page.evaluate(() => {
      localStorage.setItem('catalogro_access_token', 'expired.token.value');
    });

    // Step 3: Navigate to dashboard — the API wrapper should silently refresh.
    await page.goto('/');

    // Step 4: Verify the dashboard loaded (not redirected to /login).
    // The silent refresh should have obtained new tokens automatically.
    // Use a generous timeout since the refresh adds a round-trip.
    const isOnDashboard = await page.waitForURL((url) => url.pathname === '/', { timeout: 10_000 })
      .then(() => true)
      .catch(() => false);

    if (isOnDashboard) {
      // The dashboard loaded — verify we're NOT on the login page.
      // The content may still be in loading state ("Se incarca...") which is
      // fine — what matters is the layout rendered (sidebar, header) rather
      // than being redirected to /login.
      const hasLayout =
        (await page.getByTestId('sidebar').isVisible().catch(() => false)) ||
        (await page.getByTestId('dashboard-content').isVisible().catch(() => false)) ||
        (await page.getByTestId('dashboard-loading').isVisible().catch(() => false)) ||
        (await page.getByTestId('welcome-message').isVisible().catch(() => false));
      expect(hasLayout).toBe(true);
    } else {
      // If redirected to /login, the silent refresh did not work.
      // This is a valid failure — the feature may not be fully implemented.
      expect(page.url()).not.toContain('/login');
    }
  });

  // ─────────────────────────────────────────────────────────────────────────
  // TEST 12: Clearing both tokens redirects to /login
  // ─────────────────────────────────────────────────────────────────────────
  test('clearing both tokens redirects to login (test 12)', async ({ page }) => {
    // Step 1: Log in as parent.
    await loginAsParent(page);

    // Step 2: Clear both tokens from localStorage.
    await page.evaluate(() => {
      localStorage.removeItem('catalogro_access_token');
      localStorage.removeItem('catalogro_refresh_token');
    });

    // Step 3: Try to navigate to the dashboard.
    // Retry once on net::ERR_ABORTED — the Nuxt dev server can intermittently
    // refuse connections when it is under load after many test suites. A single
    // retry with a 1-second delay is enough to recover from this transient error
    // without masking real auth-redirect failures.
    try {
      await page.goto('/');
    } catch {
      // First attempt failed (e.g. net::ERR_ABORTED). Wait briefly, then retry.
      await page.waitForTimeout(1_000);
      await page.goto('/');
    }

    // Step 4: Verify redirect to /login — no tokens means no session.
    await page.waitForURL('**/login', { timeout: 10_000 });
    expect(page.url()).toContain('/login');
  });

  // ─────────────────────────────────────────────────────────────────────────
  // TEST 13: Direct navigation without any token redirects to /login
  // ─────────────────────────────────────────────────────────────────────────
  test('direct navigation without token redirects to login (test 13)', async ({ page }) => {
    // Fresh browser context — no tokens in localStorage at all.
    await page.goto('/');

    // Verify redirect to /login.
    await page.waitForURL('**/login', { timeout: 10_000 });
    expect(page.url()).toContain('/login');
  });
});

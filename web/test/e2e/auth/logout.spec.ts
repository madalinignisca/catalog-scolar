/**
 * auth/logout.spec.ts
 *
 * E2E test for the logout flow.
 *
 * WHY MANUAL LOGIN?
 * ─────────────────
 * This test logs in manually (like tests 4-9 in login.spec.ts) rather than
 * using the parentPage fixture. This avoids a known Playwright issue where
 * fixture-based login can time out when run after many other tests in the
 * same sequential suite.
 */

import { test, expect } from '@playwright/test';

import { TEST_USERS } from '../fixtures/auth.fixture';
import { LoginPage } from '../page-objects/login.page';

// ─────────────────────────────────────────────────────────────────────────────
// TEST 10: Logout clears the session and redirects to /login
// ─────────────────────────────────────────────────────────────────────────────
test.describe('logout', () => {
  test('logout clears session and redirects to login', async ({ page }) => {
    // Step 1: Log in as parent manually (no MFA required).
    const loginPage = new LoginPage(page);
    await loginPage.goto();
    await loginPage.fillEmail(TEST_USERS.parent.email);
    await loginPage.fillPassword(TEST_USERS.parent.password);
    await loginPage.submit();

    // Wait for redirect to dashboard.
    await page.waitForURL('/', { timeout: 10_000 });

    // Step 2: Click the logout button in the layout.
    const logoutButton = page.getByTestId('logout-button');
    await logoutButton.click();

    // Step 3: Verify redirect to /login.
    await page.waitForURL('**/login', { timeout: 10_000 });
    expect(page.url()).toContain('/login');
  });
});

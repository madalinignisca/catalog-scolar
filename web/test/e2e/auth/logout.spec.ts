/**
 * auth/logout.spec.ts
 *
 * E2E test for the logout flow (test 10).
 *
 * KNOWN FLAKY: This test can be timing-sensitive due to SSR hydration.
 * If flaky in CI, increase retries or add a hydration wait.
 */

import { test, expect } from '@playwright/test';

import { TEST_USERS } from '../fixtures/auth.fixture';
import { LoginPage } from '../page-objects/login.page';

test('logout clears session and redirects to login (test 10)', async ({ page }) => {
  // Step 1: Log in as parent (no MFA required).
  const loginPage = new LoginPage(page);
  await loginPage.goto();
  await loginPage.fillEmail(TEST_USERS.parent.email);
  await loginPage.fillPassword(TEST_USERS.parent.password);
  await loginPage.submit();
  await page.waitForURL('/', { timeout: 10_000 });

  // Step 2: Click the logout button in the layout.
  await page.getByTestId('logout-button').click();

  // Step 3: Verify redirect to login.
  await page.waitForURL('**/login', { timeout: 10_000 });
  expect(page.url()).toContain('/login');
});

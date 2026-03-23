/**
 * auth/logout.spec.ts
 *
 * E2E test for the logout flow.
 *
 * WHY A SEPARATE FILE?
 * ────────────────────
 * This test uses the auth fixture (parentPage) to get a pre-authenticated
 * session. Mixing the fixture's `test` object with the standard `test` from
 * @playwright/test in the same file causes Playwright to misconfigure
 * contexts. The official recommendation is: one `test` object per file.
 */

import { test, expect } from '../fixtures/auth.fixture';

// ─────────────────────────────────────────────────────────────────────────────
// TEST 10: Logout clears the session and redirects to /login
//
// PURPOSE: After a successful login, clicking the logout button must:
//   1. Call the logout API endpoint (or clear the local session state)
//   2. Redirect the browser back to /login
//   3. Leave the user unable to access protected routes
//
// ROLE: ion.moldovan@gmail.com — parent, no MFA (fastest fixture login).
// ─────────────────────────────────────────────────────────────────────────────
test.describe('logout', () => {
  test('logout clears session and redirects to login', async ({ parentPage }) => {
    // `parentPage` is a Playwright Page that auth.fixture.ts has already logged
    // in as Ion Moldovan (parent). The fixture called performLogin() before this
    // test function ran, so we start on the dashboard at '/'.

    // The logout button lives in the global app layout (layouts/default.vue).
    const logoutButton = parentPage.getByTestId('logout-button');

    // Click the logout button. This should:
    //   - Invalidate the session (clear JWT tokens from localStorage)
    //   - Trigger a Nuxt navigation back to /login
    await logoutButton.click();

    // Wait for the redirect to complete.
    await parentPage.waitForURL('**/login', { timeout: 10_000 });

    // Final assertion: the URL must contain "/login".
    expect(parentPage.url()).toContain('/login');
  });
});

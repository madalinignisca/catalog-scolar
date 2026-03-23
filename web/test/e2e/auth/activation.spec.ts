/**
 * auth/activation.spec.ts
 *
 * End-to-end tests for the CatalogRO account activation flow (/activate/[token]).
 *
 * ⚠️  ALL TESTS IN THIS FILE ARE DEFERRED (test.skip)  ⚠️
 * ──────────────────────────────────────────────────────
 * The activation API endpoints are NOT yet implemented on the backend:
 *
 *   GET  /auth/activate/{token}   → currently returns 501 Not Implemented
 *   POST /auth/activate           → currently returns 501 Not Implemented
 *
 * Running these tests now would produce misleading failures (the page cannot
 * load the token, so the form never renders). They are skipped until the Go
 * handlers in api/internal/handler/auth_activate.go are implemented.
 *
 * WHAT THE ACTIVATION FLOW DOES (for context)
 * ────────────────────────────────────────────
 * CatalogRO does NOT allow self-registration. Every user account is
 * provisioned by a secretary or admin. Once created, the user receives an
 * activation link via email or SMS. The link looks like:
 *
 *   https://catalogro.ro/activate/eyJhbGciOi...  (a signed JWT)
 *
 * On landing at /activate/[token] the page:
 *   1. Calls GET /auth/activate/{token} to validate the token and retrieve
 *      the user's pre-filled identity (name, role, school).
 *   2. Displays the identity for the user to confirm they are on the
 *      correct account.
 *   3. Shows a password + confirm-password form.
 *   4. For parent accounts: shows a GDPR consent checkbox (per ROFUIP rules).
 *   5. On submit, calls POST /auth/activate with the token + chosen password.
 *   6. On success: redirects to /login with a "contul tău a fost activat"
 *      confirmation message.
 *
 * TESTS IN THIS FILE
 * ──────────────────
 *   Test 14 — Valid token shows user identity and the password form.
 *   Test 15 — Invalid / expired token shows the error banner.
 *   Test 16 — Complete activation: fill password, submit, redirect to /login.
 *   Test 17 — Password validation: mismatched confirm shows inline error.
 *
 * HOW TO UN-SKIP
 * ──────────────
 * When the backend activation endpoints are implemented:
 *   1. Remove the `test.skip` wrapper from each test (keep the test body).
 *   2. Ensure api/db/seed.sql inserts at least one pending activation token
 *      so tests 14 and 16 can use a real token.
 *   3. Run `make test` to verify all four tests pass before merging.
 */

// ── External: Standard Playwright test runner ─────────────────────────────────
// We use the raw `test` from @playwright/test (not the auth fixture) because
// activation is a pre-login flow — no authenticated session is needed.
import { test } from '@playwright/test';

// ── Internal: Page object for the activation page ────────────────────────────
// ActivationPage wraps all /activate/[token] interactions: goto(token),
// locators for identity block, password inputs, GDPR checkbox, error banner,
// and success message.
import { ActivationPage } from '../page-objects/activation.page';

// ─────────────────────────────────────────────────────────────────────────────
// TEST SUITE: account activation
//
// Grouped under a describe block so Playwright's HTML reporter shows them
// as a distinct section and they can be filtered with --grep "activation".
// ─────────────────────────────────────────────────────────────────────────────
test.describe('account activation', () => {
  // ───────────────────────────────────────────────────────────────────────────
  // TEST 14 (DEFERRED): Valid token shows user identity and password form
  //
  // SCENARIO
  // ────────
  // A user receives an activation link and clicks it. The token in the URL is
  // valid (not expired, not already used). The page should fetch the user's
  // pre-filled identity from GET /auth/activate/{token} and display it
  // alongside the password-setting form.
  //
  // WHAT WE ASSERT
  // ──────────────
  // - The identity confirmation block is visible (shows name + role + school).
  // - The password input is visible (the user can set a password).
  // - The error banner is NOT visible.
  //
  // DEFERRED BECAUSE
  // ────────────────
  // GET /auth/activate/{token} returns 501 Not Implemented. The page cannot
  // fetch the token data, so identityConfirmation and passwordInput never render.
  // ───────────────────────────────────────────────────────────────────────────
  test.skip('valid activation token shows user info and password form (test 14)', ({ page }) => {
    // DEFERRED: Activation API endpoints not yet implemented (return 501)
    // When implemented (remove test.skip and restore async, add awaits):
    // const activationPage = new ActivationPage(page);
    //
    // Navigate to the activation URL. The token below is a placeholder — replace
    // it with a real token inserted by api/db/seed.sql when the feature ships.
    // await activationPage.goto('test-activation-token-radu');
    //
    // Wait for the loading spinner to disappear (token validation complete).
    // await activationPage.loadingIndicator.waitFor({ state: 'hidden', timeout: 10_000 });
    //
    // The identity block should now be visible with the user's pre-filled info.
    // await expect(activationPage.identityConfirmation).toBeVisible();
    //
    // The password input should be rendered so the user can set their password.
    // await expect(activationPage.passwordInput).toBeVisible();
    //
    // No error should be shown for a valid token.
    // await expect(activationPage.errorBanner).not.toBeVisible();
    void page; // suppress "unused variable" TypeScript warning until un-skipped
    void ActivationPage; // imported above — referenced in the commented implementation
  });

  // ───────────────────────────────────────────────────────────────────────────
  // TEST 15 (DEFERRED): Invalid or expired token shows the error banner
  //
  // SCENARIO
  // ────────
  // A user clicks an old activation link (token already used, or expired after
  // 48 hours). The server validates the token and returns an error response.
  // The page should display the error banner and NOT show the password form.
  //
  // WHAT WE ASSERT
  // ──────────────
  // - The error banner is visible (informing the user the link is invalid).
  // - The password input is NOT visible (no point setting a password).
  // - The identity block is NOT visible.
  //
  // DEFERRED BECAUSE
  // ────────────────
  // GET /auth/activate/{token} returns 501 Not Implemented regardless of
  // whether the token is valid or not. Error-path behaviour cannot be tested
  // until the endpoint distinguishes between valid and invalid tokens.
  // ───────────────────────────────────────────────────────────────────────────
  test.skip('invalid activation token shows error banner (test 15)', ({ page }) => {
    // DEFERRED: Activation API endpoints not yet implemented (return 501)
    // When implemented:
    // const activationPage = new ActivationPage(page);
    //
    // Use a deliberately invalid token — the API should return 400 or 404.
    // await activationPage.goto('this-token-does-not-exist');
    //
    // Wait for the loading state to resolve (the error response arrives).
    // await activationPage.loadingIndicator.waitFor({ state: 'hidden', timeout: 10_000 });
    //
    // The error banner must appear with a user-friendly Romanian message.
    // await expect(activationPage.errorBanner).toBeVisible();
    //
    // The form should be hidden — there is nothing to activate.
    // await expect(activationPage.passwordInput).not.toBeVisible();
    // await expect(activationPage.identityConfirmation).not.toBeVisible();
    void page;
  });

  // ───────────────────────────────────────────────────────────────────────────
  // TEST 16 (DEFERRED): Complete activation flow — password set, redirect to login
  //
  // SCENARIO
  // ────────
  // A user with a valid token successfully sets a password. After submitting,
  // the API activates the account and the page redirects to /login (possibly
  // with a success query param like ?activated=1 to show a toast).
  //
  // WHAT WE ASSERT
  // ──────────────
  // - After form submission, the success message is visible OR
  //   the browser redirects to /login.
  //
  // DEFERRED BECAUSE
  // ────────────────
  // POST /auth/activate returns 501 Not Implemented. Submitting the form
  // will receive a 501 response, which the frontend will treat as an error.
  // The redirect to /login on success cannot be tested yet.
  // ───────────────────────────────────────────────────────────────────────────
  test.skip('complete activation flow sets password and redirects to login (test 16)', ({ page }) => {
    // DEFERRED: Activation API endpoints not yet implemented (return 501)
    // When implemented:
    // const activationPage = new ActivationPage(page);
    //
    // Navigate with a valid seed token.
    // await activationPage.goto('test-activation-token-radu');
    //
    // Wait for the identity to load.
    // await activationPage.loadingIndicator.waitFor({ state: 'hidden', timeout: 10_000 });
    //
    // Fill in a password that meets the policy (min 8 chars, mixed case, symbol).
    // await activationPage.fillPassword('SecurePass123!');
    // await activationPage.fillPasswordConfirm('SecurePass123!');
    //
    // For a parent account, accept the GDPR consent checkbox:
    // await activationPage.acceptGdpr();
    //
    // Submit the activation form.
    // await activationPage.submit();
    //
    // The backend activates the account. The page either shows a success
    // message or navigates to /login directly.
    // await page.waitForURL('**/login', { timeout: 10_000 });
    //
    // Alternatively, if the success state stays on /activate before redirecting:
    // await expect(activationPage.successMessage).toBeVisible({ timeout: 10_000 });
    void page;
  });

  // ───────────────────────────────────────────────────────────────────────────
  // TEST 17 (DEFERRED): Password mismatch shows inline validation error
  //
  // SCENARIO
  // ────────
  // A user enters two different strings in the password and confirm-password
  // fields. The client-side validation should catch this before making any
  // API call and display an inline error message on the form.
  //
  // WHAT WE ASSERT
  // ──────────────
  // - An inline password validation error element becomes visible.
  // - The page does NOT navigate away (still on /activate/[token]).
  // - The success message is NOT shown.
  //
  // DEFERRED BECAUSE
  // ────────────────
  // The form that contains the password fields is only rendered after a
  // successful GET /auth/activate/{token} response. Since that endpoint
  // returns 501, the form never renders and we cannot test its validation.
  // ───────────────────────────────────────────────────────────────────────────
  test.skip('password mismatch shows inline validation error (test 17)', ({ page }) => {
    // DEFERRED: Activation API endpoints not yet implemented (return 501)
    // When implemented:
    // const activationPage = new ActivationPage(page);
    //
    // Navigate with a valid token so the password form renders.
    // await activationPage.goto('test-activation-token-radu');
    // await activationPage.loadingIndicator.waitFor({ state: 'hidden', timeout: 10_000 });
    //
    // Enter two passwords that do NOT match.
    // await activationPage.fillPassword('SecurePass123!');
    // await activationPage.fillPasswordConfirm('DifferentPass456@');
    //
    // Submit the form — client-side validation should block the API call.
    // await activationPage.submit();
    //
    // An inline password error element should appear.
    // The exact testid depends on the Vue component implementation.
    // await expect(page.getByTestId('password-match-error')).toBeVisible();
    //
    // The form should still be on the activation page (no redirect).
    // expect(page.url()).toContain('/activate/');
    //
    // The success message must NOT be visible.
    // await expect(activationPage.successMessage).not.toBeVisible();
    void page;
  });
});

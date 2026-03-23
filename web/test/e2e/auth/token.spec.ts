/**
 * auth/token.spec.ts
 *
 * End-to-end tests for JWT token lifecycle in CatalogRO.
 *
 * WHAT THIS FILE COVERS
 * ─────────────────────
 * The CatalogRO frontend uses two tokens to manage a logged-in session:
 *
 *   1. Access token  — short-lived JWT (15 min), sent with every API call.
 *      Stored in localStorage as `catalogro_access_token`.
 *
 *   2. Refresh token — long-lived opaque token (7 days), stored in
 *      localStorage as `catalogro_refresh_token`. Used ONLY to obtain a
 *      new access token when the current one has expired or is invalid.
 *
 * These tests verify three scenarios that every production user will
 * eventually encounter:
 *
 *   Test 11 — A stale/corrupted access token triggers a silent refresh.
 *              The user never sees a login page; the app heals itself.
 *
 *   Test 12 — Both tokens are gone (e.g. user cleared storage manually,
 *              or the refresh token expired). The app must redirect to /login.
 *
 *   Test 13 — A brand-new browser session with no tokens at all must be
 *              redirected to /login before any protected content loads.
 *
 * HOW THE SILENT REFRESH WORKS
 * ─────────────────────────────
 * The Nuxt composable `useApi()` intercepts every outgoing fetch. When the
 * API returns HTTP 401, the composable calls POST /auth/refresh with the
 * refresh token. If that succeeds, the new access token is saved to
 * localStorage and the original request is retried transparently. The user
 * never sees an error or a redirect.
 *
 * FIXTURES USED
 * ─────────────
 * Tests 11 and 12 use `parentPage` from auth.fixture.ts. This fixture
 * starts the test with a fully authenticated browser session (access token
 * + refresh token already in localStorage). Using parent avoids the TOTP
 * step, keeping setup fast.
 *
 * Test 13 uses the raw `test` from @playwright/test — a completely
 * unauthenticated browser context with no tokens.
 */

// ── External: Standard Playwright test runner ─────────────────────────────────
// Test 13 needs a plain unauthenticated page, so we import the raw `test`
// from @playwright/test here as well as via the fixture.
import { test, expect } from '@playwright/test';

// ── Internal: Auth fixture ────────────────────────────────────────────────────
// `authTest` gives us the `parentPage` fixture (already logged in as Ion
// Moldovan, a parent). We alias it to `authTest` to avoid a name collision
// with the plain `test` import above.
import { test as authTest } from '../fixtures/auth.fixture';

// ─────────────────────────────────────────────────────────────────────────────
// TEST 11: Expired access token triggers a silent token refresh
//
// SCENARIO
// ────────
// The user was logged in. Their access token has now "expired" (we simulate
// this by replacing it with a garbage string that the API will reject with
// a 401). Their refresh token is still valid.
//
// EXPECTED BEHAVIOUR
// ──────────────────
// When the user navigates to '/', the Nuxt app:
//   1. Loads the page skeleton (HTML from SSR or cached)
//   2. Makes an API call that returns 401 (invalid access token)
//   3. Automatically POSTs to /auth/refresh using the still-valid refresh token
//   4. Receives a new access token and saves it to localStorage
//   5. Retries the original API call successfully
//   6. Renders the dashboard content — the user never sees /login
//
// WHY THIS MATTERS
// ────────────────
// Access tokens are short-lived (15 min) by design. If the app failed to
// silently refresh, teachers would be kicked out mid-lesson every 15 minutes.
// This test guards against regressions in the refresh interceptor.
// ─────────────────────────────────────────────────────────────────────────────
authTest.describe('token lifecycle — authenticated base', () => {
  authTest(
    'expired access token triggers silent refresh (test 11)',
    async ({ parentPage }) => {
      // `parentPage` enters this test already on the dashboard ('/'). The
      // fixture stored a valid access token and refresh token in localStorage.

      // ── Step 1: Corrupt the access token ──────────────────────────────────
      // We use page.evaluate() to run code inside the browser process.
      // This directly sets localStorage in the page's origin context — the
      // same origin where the Nuxt app stores its tokens.
      // The string 'expired.token.value' is not a valid JWT, so the API
      // will reject it with HTTP 401 on the next request.
      await parentPage.evaluate(() => {
        // Replace only the access token. Leave the refresh token intact so
        // the silent refresh flow has a valid token to exchange.
        localStorage.setItem('catalogro_access_token', 'expired.token.value');
      });

      // ── Step 2: Navigate to the dashboard ─────────────────────────────────
      // A full navigation re-runs the page's onMounted hooks and triggers the
      // first API call that will encounter the 401.
      await parentPage.goto('/');

      // ── Step 3: Assert the dashboard loaded (NOT redirected to /login) ─────
      // If the silent refresh works, the app stays on '/' and renders
      // dashboard-content. We use waitForURL with a generous timeout because
      // the refresh round-trip adds latency on top of the normal page load.
      await parentPage.waitForURL('/', { timeout: 10_000 });

      // Verify the dashboard content area is visible. This confirms that:
      //   (a) the app did NOT redirect to /login, AND
      //   (b) the API call succeeded after the refresh
      await expect(parentPage.getByTestId('dashboard-content')).toBeVisible({
        timeout: 10_000,
      });
    },
  );

  // ─────────────────────────────────────────────────────────────────────────
  // TEST 12: Clearing both tokens redirects to /login
  //
  // SCENARIO
  // ────────
  // Both the access token and the refresh token have been removed from
  // localStorage (simulating an expired refresh token, or a user who manually
  // cleared their browser storage). The app has no way to authenticate.
  //
  // EXPECTED BEHAVIOUR
  // ──────────────────
  // When the user navigates to any protected route ('/' or anything behind
  // the auth guard), the Nuxt middleware detects the missing tokens and
  // redirects to /login before any protected content is shown.
  //
  // WHY THIS MATTERS
  // ────────────────
  // Without this check, a user whose refresh token has expired would see a
  // broken app (endless loading spinners or API errors) instead of a clean
  // "please log in again" page.
  // ─────────────────────────────────────────────────────────────────────────
  authTest(
    'clearing both tokens redirects to login (test 12)',
    async ({ parentPage }) => {
      // `parentPage` starts on '/'. Both tokens are in localStorage.

      // ── Step 1: Remove BOTH tokens ────────────────────────────────────────
      // Deleting both tokens simulates a state where the refresh token has
      // also expired or been revoked. There is nothing left to authenticate with.
      await parentPage.evaluate(() => {
        localStorage.removeItem('catalogro_access_token');
        localStorage.removeItem('catalogro_refresh_token');
      });

      // ── Step 2: Navigate to the protected dashboard ───────────────────────
      // The Nuxt auth middleware runs on every navigation. With no tokens in
      // storage, it should immediately redirect to /login.
      await parentPage.goto('/');

      // ── Step 3: Assert redirect to /login ─────────────────────────────────
      // waitForURL will succeed as soon as the URL matches the pattern.
      // The ** wildcard matches any host/port prefix (e.g. localhost:3000).
      await parentPage.waitForURL('**/login', { timeout: 10_000 });

      // Confirm we landed on the login page by checking the URL directly.
      expect(parentPage.url()).toContain('/login');
    },
  );
});

// ─────────────────────────────────────────────────────────────────────────────
// TEST 13: Direct navigation without any token redirects to /login
//
// SCENARIO
// ────────
// A completely fresh browser session — no localStorage, no cookies, no
// session at all. This is the state for a brand-new user, or a user whose
// browser data was cleared.
//
// EXPECTED BEHAVIOUR
// ──────────────────
// Navigating directly to '/' (or any protected route) must redirect to
// /login before rendering any protected content.
//
// WHY THIS IS A SEPARATE TEST FROM TEST 12
// ─────────────────────────────────────────
// Test 12 starts with a valid session and then manually removes the tokens,
// which could behave differently from a session that never had tokens (e.g.
// if Pinia state is already populated). Test 13 uses a raw, unauthenticated
// `page` fixture from @playwright/test — a new browser context with zero
// prior state — to ensure the middleware guard works from a cold start.
// ─────────────────────────────────────────────────────────────────────────────
test.describe('token lifecycle — unauthenticated', () => {
  test('direct navigation without token redirects to login (test 13)', async ({ page }) => {
    // This `page` is a raw Playwright page — a completely fresh browser
    // context with no localStorage entries. There are no tokens at all.

    // Navigate directly to the protected dashboard root.
    // The Nuxt auth middleware should intercept this and redirect.
    await page.goto('/');

    // Wait for the redirect to /login to complete.
    // The ** wildcard covers any origin (http://localhost:3000/login, etc.)
    await page.waitForURL('**/login', { timeout: 10_000 });

    // Confirm the final URL contains '/login'.
    expect(page.url()).toContain('/login');
  });
});

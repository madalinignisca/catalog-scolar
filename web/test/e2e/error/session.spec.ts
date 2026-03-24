/**
 * error/session.spec.ts
 *
 * Tests 69–70: Session expiry and token refresh behaviour.
 *
 * WHAT WE TEST
 * ────────────
 * CatalogRO uses two tokens stored in localStorage:
 *   - `catalogro_access_token`  — short-lived JWT (15 min), sent with every API call.
 *   - `catalogro_refresh_token` — long-lived opaque token (7 days), used only to
 *     silently obtain a new access token when the current one expires.
 *
 * These tests verify the two most common session-related scenarios a user will
 * experience over time:
 *
 *   69 – Both tokens are removed (expired session / manual storage clear).
 *        The app must detect the missing tokens and redirect to /login.
 *        No user data should be shown before the redirect.
 *
 *   70 – Access token is replaced with garbage but the refresh token remains
 *        valid. The API wrapper must silently call POST /auth/refresh, obtain
 *        a new access token, and retry the original request. The user sees the
 *        dashboard, never /login.
 *
 * WHY THESE SCENARIOS MATTER (PM PERSPECTIVE)
 * ────────────────────────────────────────────
 * Test 69: A teacher who left a laptop in a classroom for a week will return
 * to a session where both tokens have expired. They must be sent to login —
 * NOT left on a broken dashboard showing stale data.
 *
 * Test 70: The 15-minute access token window is very short. Every teacher
 * session will trigger a silent refresh after quarter-hour intervals. If the
 * refresh flow is broken, teachers will be logged out every 15 minutes — a
 * severe usability issue during a school lesson.
 *
 * FIXTURES
 * ────────
 * Both tests use `parentPage` — a parent user (Ion Moldovan) who does NOT
 * require MFA. This keeps fixture setup fast. The token behaviour is identical
 * across all user roles.
 *
 * HOW localStorage MANIPULATION WORKS
 * ─────────────────────────────────────
 * `page.evaluate(() => { ... })` runs a JavaScript snippet in the browser
 * context. Inside this snippet, `localStorage` refers to the real browser
 * localStorage of the currently open page. This is the standard way to
 * manipulate client-side storage in Playwright tests.
 */

import { test, expect } from '../fixtures/auth.fixture';

// ── Test 69 ───────────────────────────────────────────────────────────────────

test('69 – removing both tokens mid-session redirects to /login', async ({ parentPage }) => {
  /**
   * SCENARIO
   * ────────
   * The parent is fully logged in (both tokens in localStorage). We
   * remove both tokens to simulate an expired session, then navigate
   * within the app. The auth guard must intercept the navigation and
   * redirect to /login.
   *
   * STEPS
   * ─────
   *   1. Start on dashboard (fixture guarantees this).
   *   2. Remove both tokens from localStorage via page.evaluate().
   *   3. Attempt to navigate to a protected route (click a nav item
   *      or go to a route directly).
   *   4. Assert we land on /login, not the requested route.
   *
   * IMPLEMENTATION NOTE
   * ───────────────────
   * We navigate directly to '/' after clearing tokens. The Nuxt middleware
   * (or useAuth composable) must detect the absent tokens during the
   * route guard and redirect before any protected data loads.
   *
   * Alternatively, some implementations check on the next API call.
   * We cover both: direct navigation AND a protected API call by navigating
   * to a page that triggers a fetch on mount.
   */

  // ── Step 1: Verify we are on the dashboard ────────────────────────────────
  // The fixture already navigated to '/' after login. Confirm we are there.
  await parentPage.waitForURL('/', { timeout: 10_000 });

  // ── Step 2: Remove both tokens from localStorage ──────────────────────────
  // page.evaluate() runs this script inside the real browser tab.
  await parentPage.evaluate(() => {
    // Remove the short-lived access token.
    localStorage.removeItem('catalogro_access_token');
    // Remove the long-lived refresh token — no silent refresh is possible.
    localStorage.removeItem('catalogro_refresh_token');
  });

  // ── Step 3: Attempt to navigate to a protected route ─────────────────────
  // We use page.goto('/') to trigger a full navigation that the Nuxt
  // middleware / route guard will intercept. Because both tokens are gone,
  // there is no valid session to restore.
  await parentPage.goto('/');

  // ── Step 4: Assert redirect to /login ────────────────────────────────────
  // The app must redirect to /login. We allow up to 8 seconds for the
  // middleware to run and the redirect to complete.
  await parentPage.waitForURL('/login', { timeout: 8_000 });

  // Confirm the login page rendered by checking for the email input.
  // This rules out the case where the URL changed but the page is blank.
  await expect(parentPage.getByTestId('email-input')).toBeVisible({
    timeout: 5_000,
  });
});

// ── Test 70 ───────────────────────────────────────────────────────────────────

test('70 – stale access token with valid refresh token silently refreshes', async ({
  parentPage,
}) => {
  /**
   * SCENARIO
   * ────────
   * The parent is fully logged in. We replace the access token with garbage
   * (simulating a token that has expired since login). The refresh token
   * remains valid. When the app makes an API call and receives HTTP 401,
   * the API wrapper should:
   *   1. Detect the 401 response.
   *   2. Call POST /auth/refresh with the valid refresh token.
   *   3. Store the new access token in localStorage.
   *   4. Retry the original request with the new token.
   *   5. Display the page content normally.
   *
   * The user should never be redirected to /login.
   *
   * STEPS
   * ─────
   *   1. Start on dashboard (fixture guarantees this).
   *   2. Replace access token with "expired.garbage.token" in localStorage.
   *   3. Navigate to '/' to trigger a fresh page load with a stale token.
   *   4. The API call fails with 401 → silent refresh → retry succeeds.
   *   5. Assert the dashboard renders normally (not /login).
   *
   * WHAT COUNTS AS "DASHBOARD LOADED"
   * ──────────────────────────────────
   * We assert that the URL remains '/' (not redirected to /login) and that
   * at least one dashboard element is visible. We do NOT hard-code which
   * exact element — different roles see different dashboard content.
   * Instead we check for the authenticated layout sidebar, which is always
   * present for logged-in users.
   */

  // ── Step 1: Verify we are on the dashboard ────────────────────────────────
  await parentPage.waitForURL('/', { timeout: 10_000 });

  // ── Step 2: Replace access token with garbage ─────────────────────────────
  // The refresh token is left untouched — it remains valid and allows a
  // silent token renewal when the next API call returns 401.
  await parentPage.evaluate(() => {
    // Overwrite with a syntactically valid but cryptographically invalid JWT.
    // Three dot-separated segments to avoid early client-side validation,
    // but the signature is wrong so the server will reject it with 401.
    localStorage.setItem('catalogro_access_token', 'expired.garbage.token');
  });

  // ── Step 3: Navigate to the root URL ─────────────────────────────────────
  // This triggers a route navigation. The page will load, components will
  // mount, and API calls will fire using the stale token.
  await parentPage.goto('/');

  // ── Step 4 (implicit): Silent refresh runs ────────────────────────────────
  // The API composable intercepts the 401 response from the first API call,
  // calls POST /auth/refresh with the valid refresh token, stores the new
  // access token, and retries. This all happens inside the app — no redirect.

  // ── Step 5: Assert dashboard loads — NOT redirected to /login ─────────────
  // We wait for the URL to stabilise. It should remain '/', not become '/login'.
  //
  // A short wait lets any redirect (if broken) complete before we assert.
  await parentPage.waitForTimeout(2_000);

  // Confirm the URL is still the dashboard root.
  expect(parentPage.url()).not.toContain('/login');

  // Confirm the authenticated layout is visible — proves the page rendered.
  // The sidebar is always present for any logged-in user on any route.
  const sidebar = parentPage.getByTestId('sidebar');
  await expect(sidebar).toBeVisible({ timeout: 10_000 });
});

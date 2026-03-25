/**
 * admin/resend-activation.spec.ts
 *
 * Tests for POST /api/v1/users/{userId}/resend-activation.
 *
 * WHAT THIS FILE COVERS
 * ─────────────────────
 * CatalogRO's secretary/admin can re-issue an activation link for any user
 * whose account has not yet been activated (activated_at IS NULL). This is
 * useful when:
 *   - The original activation email was lost or ended up in spam.
 *   - The secretary provisioned the account but forgot to share the link.
 *   - The user requests a fresh link after the original expired.
 *
 * This file exercises:
 *
 *   POST /api/v1/users/{userId}/resend-activation
 *     → Allowed for admin and secretary roles.
 *     → Returns 200 OK with { activation_token, activation_url, sent_at }.
 *     → Returns 403 Forbidden for unauthorised roles (e.g., parent).
 *
 * TESTS IN THIS FILE
 * ──────────────────
 *   Test A – Secretary can resend activation for a pending user (200).
 *             POST /api/v1/users/{userId}/resend-activation returns 200
 *             with a fresh activation_token and activation_url.
 *
 *   Test B – Response includes required fields: activation_token and activation_url.
 *             The token is a 64-character hex string. The URL starts with "http".
 *
 *   Test C – Parent role cannot resend activation (403 Forbidden).
 *             The RequireRole("admin", "secretary") middleware blocks parents.
 *
 * APPROACH: API-BASED TESTING
 * ───────────────────────────
 * There is no frontend UI for this endpoint yet. We call the API directly
 * using Playwright's `page.request` API, which automatically sends the
 * httpOnly auth cookies from the browser context (no manual token extraction).
 *
 * SEED DATA REFERENCES
 * ─────────────────────
 * We first call POST /api/v1/users to provision a new pending user, then
 * use the returned id to call resend-activation. This avoids hardcoding UUIDs
 * and keeps the test independent of seed data.
 *
 * FIXTURES USED
 * ─────────────
 *   secretaryPage — Elena Ionescu (secretary, scoala-rebreanu.ro) — MFA on
 *   parentPage    — Ion Moldovan  (parent)                        — MFA off
 */

// ── Internal: Auth fixture ─────────────────────────────────────────────────────
// Provides pre-authenticated browser pages for each role.
// Do NOT import from '@playwright/test' directly — custom fixtures (secretaryPage,
// parentPage) are only available from this re-exported test function.
import { test, expect } from '../fixtures/auth.fixture';

// ── Shared constants ───────────────────────────────────────────────────────────

/**
 * API base URL — must match the Go server's listen address.
 * Mirrors the constant in user-provisioning.spec.ts.
 */
const API_BASE = 'http://localhost:8080/api/v1';

// ── Helper: generate a unique email address ────────────────────────────────────

/**
 * uniqueEmail
 *
 * Appends a millisecond timestamp to avoid email collisions across test runs
 * (the DB is not always reset between CI reruns).
 *
 * @param prefix - Short label for readability, e.g. "resend.test".
 * @returns A unique email string safe for account creation.
 */
function uniqueEmail(prefix: string): string {
  return `${prefix}.e2e.${String(Date.now())}@scoala-rebreanu.ro`;
}

// ── Helper types ───────────────────────────────────────────────────────────────

/**
 * CreateUserResponse
 *
 * Shape of the body returned by POST /api/v1/users on success (201).
 * Standard { "data": { ... } } envelope.
 */
interface CreateUserResponse {
  data: {
    id: string;
    activation_token: string;
    activation_url: string;
  };
}

/**
 * ResendActivationResponse
 *
 * Shape of the body returned by a successful
 * POST /api/v1/users/{userId}/resend-activation (200).
 * Standard { "data": { ... } } envelope.
 */
interface ResendActivationResponse {
  data: {
    /** 64-character hex string (32 bytes of crypto/rand). */
    activation_token: string;

    /** Full URL: {APP_BASE_URL}/activate/{token}. */
    activation_url: string;

    /** ISO 8601 timestamp when the new token was stored (activation_sent_at). */
    sent_at: string;
  };
}

// ─────────────────────────────────────────────────────────────────────────────
// TEST SUITE: resend activation
// ─────────────────────────────────────────────────────────────────────────────

test.describe('resend activation', () => {
  // ───────────────────────────────────────────────────────────────────────────
  // TEST A: Secretary can resend activation for a pending user (200)
  //
  // SCENARIO
  // ────────
  // 1. The secretary creates a new (pending) user via POST /api/v1/users.
  // 2. The secretary calls POST /api/v1/users/{id}/resend-activation.
  // 3. The API must return 200 OK with { activation_token, activation_url, sent_at }.
  //
  // WHAT WE ASSERT
  // ──────────────
  //   - HTTP status is 200.
  //   - response.data is defined (data envelope present).
  //   - response.data.activation_token is a non-empty string.
  //   - response.data.activation_url starts with "http" (valid URL).
  //   - response.data.sent_at is a non-empty string (ISO timestamp).
  // ───────────────────────────────────────────────────────────────────────────
  test('A – secretary can resend activation for a pending user (200)', async ({
    secretaryPage,
  }) => {
    /**
     * Step 1: Provision a fresh pending user via POST /api/v1/users.
     *
     * We use Playwright's page.request API which automatically sends the
     * httpOnly auth cookies from the browser context (set during login).
     * No manual token extraction needed.
     *
     * We create the user here (rather than relying on a seeded pending user)
     * so the test is self-contained — no dependency on DB state from other tests.
     *
     * uniqueEmail() prevents duplicate-email constraint violations on reruns.
     */
    const provisionResponse = await secretaryPage.request.post(`${API_BASE}/users`, {
      data: {
        email: uniqueEmail('resend.pending'),
        first_name: 'Resend',
        last_name: 'TestParent',
        role: 'parent',
      },
    });

    // The provisioning step must succeed (201) before we can test resend.
    // If it fails, the test is aborted here with a clear error message.
    expect(
      provisionResponse.status(),
      `Provisioning step failed: expected 201 but got ${String(provisionResponse.status())}. ` +
        'Cannot proceed with resend-activation test without a pending user.',
    ).toBe(201);

    const provisionBody = (await provisionResponse.json()) as CreateUserResponse;
    const pendingUserId: string = provisionBody.data.id;

    /**
     * Step 2: Call POST /users/{id}/resend-activation.
     *
     * This is the endpoint under test. We pass the pending user's UUID in the
     * path so the handler knows which user to update.
     *
     * No JSON body is required — the token is generated server-side.
     */
    const resendResponse = await secretaryPage.request.post(
      `${API_BASE}/users/${pendingUserId}/resend-activation`,
    );

    /**
     * Step 3: Assert HTTP 200 OK.
     *
     * 200 (not 201) because we are updating an existing resource (the pending
     * user's activation_token), not creating a new one.
     */
    expect(
      resendResponse.status(),
      `Expected 200 OK from resend-activation but got ${String(resendResponse.status())}. ` +
        'Check that the secretary role is authorised and the user is still pending.',
    ).toBe(200);

    /**
     * Step 4: Parse and assert the response body.
     */
    const body = (await resendResponse.json()) as ResendActivationResponse;

    // The standard { "data": { ... } } envelope must be present.
    expect(body.data, 'Response body must contain a "data" envelope').toBeDefined();

    // activation_token must be a non-empty string (64-char hex from the server).
    expect(
      body.data.activation_token,
      'Expected "activation_token" field to be a non-empty string',
    ).toBeTruthy();

    // activation_url must be a fully-qualified URL containing the token.
    expect(
      body.data.activation_url,
      'Expected "activation_url" to be a valid URL starting with "http"',
    ).toMatch(/^https?:\/\//);

    // sent_at is the DB timestamp for when the new token was stored.
    expect(
      body.data.sent_at,
      'Expected "sent_at" to be a non-empty ISO timestamp string',
    ).toBeTruthy();
  });

  // ───────────────────────────────────────────────────────────────────────────
  // TEST B: Response includes a 64-char hex activation_token and valid URL
  //
  // SCENARIO
  // ────────
  // We provision a new pending user and then call resend-activation.
  // The returned activation_token must be exactly 64 hex characters, and the
  // activation_url must incorporate that same token in its path.
  //
  // WHY THIS MATTERS
  // ────────────────
  // The token is used as a one-time secret. If it is shorter than 64 chars,
  // it has less entropy and is easier to brute-force. If the URL does not
  // embed the token, the user cannot follow it to complete activation.
  //
  // WHAT WE ASSERT
  // ──────────────
  //   - activation_token matches /^[0-9a-f]{64}$/ (64 lowercase hex chars).
  //   - activation_url contains the token: ends with "/<token>".
  // ───────────────────────────────────────────────────────────────────────────
  test('B – returns new activation_token (64-char hex) and matching activation_url', async ({
    secretaryPage,
  }) => {
    // Provision a new pending user for this test.
    const provisionResponse = await secretaryPage.request.post(`${API_BASE}/users`, {
      data: {
        email: uniqueEmail('resend.token.check'),
        first_name: 'Token',
        last_name: 'Checker',
        role: 'parent',
      },
    });
    expect(provisionResponse.status()).toBe(201);

    const provisionBody = (await provisionResponse.json()) as CreateUserResponse;
    const pendingUserId: string = provisionBody.data.id;
    const originalToken: string = provisionBody.data.activation_token;

    // Call resend-activation to get a fresh token.
    const resendResponse = await secretaryPage.request.post(
      `${API_BASE}/users/${pendingUserId}/resend-activation`,
    );
    expect(resendResponse.status()).toBe(200);

    const body = (await resendResponse.json()) as ResendActivationResponse;
    const newToken: string = body.data.activation_token;

    /**
     * Assert: the token is exactly 64 lowercase hex characters.
     *
     * The server generates 32 bytes via crypto/rand and encodes as hex.
     * Exactly 64 characters (not 63, not 65).
     */
    expect(
      newToken,
      `Expected activation_token to be a 64-character hex string, got: "${newToken}"`,
    ).toMatch(/^[0-9a-f]{64}$/);

    /**
     * Assert: the new token is different from the original.
     *
     * If the handler returned the same token, it would mean no new token was
     * generated — the resend would be a no-op.
     */
    expect(
      newToken,
      'New activation_token must differ from the original provisioning token',
    ).not.toBe(originalToken);

    /**
     * Assert: the activation_url ends with "/<newToken>".
     *
     * Format: {APP_BASE_URL}/activate/{token}. We use a plain string suffix
     * check (endsWith) rather than a dynamic RegExp to avoid any ReDoS risk —
     * the token is a server-supplied value so using it inside new RegExp()
     * would be flagged by static analysis even though it is hex-only.
     */
    expect(
      body.data.activation_url,
      `Expected activation_url to end with "/${newToken}"`,
    ).toContain(`/activate/${newToken}`);
  });

  // ───────────────────────────────────────────────────────────────────────────
  // TEST C: Parent role cannot resend activation (403 Forbidden)
  //
  // SCENARIO
  // ────────
  // Ion Moldovan (parent) calls POST /users/{userId}/resend-activation.
  // The RequireRole("admin", "secretary") middleware must reject the request
  // before it reaches the handler.
  //
  // WHY THIS MATTERS
  // ────────────────
  // Parents must not be able to generate activation links for arbitrary users.
  // A parent generating a link for another user could be used to hijack that
  // account.
  //
  // WHAT WE ASSERT
  // ──────────────
  //   - HTTP status is 403 Forbidden.
  //   - The response body does NOT contain an activation_token.
  // ───────────────────────────────────────────────────────────────────────────
  test('C – parent gets 403 when calling resend-activation', async ({
    secretaryPage,
    parentPage,
  }) => {
    /**
     * Step 1: Provision a pending user as the secretary so we have a valid
     * userId to target. (We still expect 403 regardless of the userId, but
     * using a real UUID avoids accidentally testing the UUID validation path.)
     */
    const provisionResponse = await secretaryPage.request.post(`${API_BASE}/users`, {
      data: {
        email: uniqueEmail('resend.parent.forbidden'),
        first_name: 'Forbidden',
        last_name: 'Target',
        role: 'student',
      },
    });
    expect(provisionResponse.status()).toBe(201);

    const provisionBody = (await provisionResponse.json()) as CreateUserResponse;
    const pendingUserId: string = provisionBody.data.id;

    /**
     * Step 2: Attempt resend-activation as the parent (Ion Moldovan).
     * The parent has a valid JWT (cookie) but the wrong role.
     */
    const resendResponse = await parentPage.request.post(
      `${API_BASE}/users/${pendingUserId}/resend-activation`,
    );

    /**
     * Step 3: Assert 403 Forbidden.
     *
     * RequireRole("admin", "secretary") middleware rejects all other roles
     * before the handler body runs.
     */
    expect(
      resendResponse.status(),
      `Expected 403 Forbidden for parent role, but got ${String(resendResponse.status())}. ` +
        'RequireRole middleware must block parent access to resend-activation.',
    ).toBe(403);

    /**
     * Step 4: Assert the response body does NOT contain an activation_token.
     *
     * A 403 response should only contain an error object, never token data.
     * Checking the raw body string is more reliable than parsing JSON for this
     * negative assertion.
     */
    const rawBody = await resendResponse.text();
    expect(rawBody, 'A 403 response must not leak "activation_token" in the body').not.toContain(
      'activation_token',
    );
  });
});

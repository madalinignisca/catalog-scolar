/**
 * admin/profile-update.spec.ts
 *
 * Tests for: Profile update via PUT /api/v1/users/me.
 *
 * WHAT THIS FILE COVERS
 * ─────────────────────
 * Any authenticated user in CatalogRO can update their own email and/or phone
 * number via PUT /api/v1/users/me. This endpoint is NOT restricted by role —
 * parents, teachers, students, and admins all have access to update their own
 * profile.
 *
 * SECURITY INVARIANT
 * ──────────────────
 * The PUT /users/me endpoint ONLY allows changing email and phone.
 * Fields like role, school_id, and is_active are server-enforced immutable:
 * even if a client sends them in the request body, they are silently ignored.
 * The underlying SQL query (UpdateUserProfile) only touches email, phone,
 * and updated_at.
 *
 * These tests exercise one API endpoint:
 *
 *   PUT /api/v1/users/me  → Update own email and/or phone (200)
 *
 * TESTS IN THIS FILE
 * ──────────────────
 *   Test A – Parent can update their phone number.
 *              PUT /api/v1/users/me with phone returns 200 OK.
 *
 *   Test B – Updated phone appears in GET /users/me response.
 *              After the PUT, GET /api/v1/users/me must reflect the new phone.
 *
 *   Test C – Invalid email returns 400.
 *              PUT /api/v1/users/me with an email missing "@" returns 400 Bad Request
 *              with error code "INVALID_EMAIL".
 *
 * APPROACH: API-BASED TESTING
 * ───────────────────────────
 * There is no dedicated frontend UI for profile editing yet. We call the API
 * directly from the test (Node.js side) using the global `fetch()` available
 * in Node 18+. Authentication tokens are extracted from localStorage after the
 * auth fixture completes login.
 *
 * FIXTURES USED
 * ─────────────
 *   parentPage — Ion Moldovan (parent role, no MFA required)
 *                These tests use the parent fixture because:
 *                  a) Parents have no MFA, so login is faster.
 *                  b) This exercises the "any role can update their own profile"
 *                     behaviour — not just admin/secretary.
 *
 * SEED DATA REFERENCES
 * ─────────────────────
 * Credentials match api/db/seed.sql and auth.fixture.ts → TEST_USERS.
 *   Ion Moldovan (parent)
 *     id:    b1000000-0000-0000-0000-000000000301
 *     email: ion.moldovan@gmail.com
 */

// ── Internal: Auth fixture ─────────────────────────────────────────────────────
// Re-export `test` and `expect` from the fixture so custom pages are available.
// Do NOT import from '@playwright/test' directly or the custom fixtures
// (parentPage, secretaryPage, etc.) will not be available.
import { test, expect } from '../fixtures/auth.fixture';

// ── Shared constants ───────────────────────────────────────────────────────────

/**
 * API base URL — must match the Go server's listen address.
 * The Go server listens on :8080 by default (see api/cmd/server/main.go).
 */
const API_BASE = 'http://localhost:8080/api/v1';

// ── Helper: extract the access token from the authenticated browser ────────────

/**
 * getAccessToken
 *
 * Reads the JWT access token that the auth fixture stored in localStorage.
 * The auth fixture logs in via the real API and writes tokens to:
 *   localStorage['catalogro_access_token']  → short-lived JWT (15 min)
 *   localStorage['catalogro_refresh_token'] → long-lived refresh (7 days)
 *
 * We read the access token here to attach it as a Bearer header when calling
 * the API directly from the test's Node.js process.
 *
 * NOTE: `page.evaluate()` runs JavaScript inside the browser context —
 * that is where localStorage lives. The result is returned to Node.js.
 *
 * @param page - A Playwright Page instance that is already authenticated.
 * @returns The JWT access token string, or throws if it is missing.
 */
async function getAccessToken(page: import('@playwright/test').Page): Promise<string> {
  const token = await page.evaluate(() => localStorage.getItem('catalogro_access_token'));

  if (token === null || token === '') {
    // This should never happen if the auth fixture ran successfully.
    throw new Error(
      'catalogro_access_token not found in localStorage. ' +
        'Did the auth fixture complete login successfully?',
    );
  }

  return token;
}

// ── Helper types ───────────────────────────────────────────────────────────────

/**
 * UpdateProfileResponse
 *
 * Shape of the JSON body returned by PUT /api/v1/users/me on success (200).
 * The API wraps responses in a `data` envelope:
 *   { "data": { "id": "...", "email": "...", "phone": "...", "role": "..." } }
 *
 * Only the fields we assert on are listed here. TypeScript strict mode
 * requires explicit types — `any` is forbidden per project rules.
 */
interface UpdateProfileResponse {
  data: {
    id: string;
    email?: string;
    phone?: string;
    role: string;
    school_id: string;
    first_name: string;
    last_name: string;
    is_active: boolean;
  };
}

/**
 * GetProfileResponse
 *
 * Shape of the JSON body returned by GET /api/v1/users/me.
 * Same structure as UpdateProfileResponse — both return the user's safe fields.
 */
interface GetProfileResponse {
  data: {
    id: string;
    email?: string;
    phone?: string;
    role: string;
  };
}

/**
 * ErrorResponse
 *
 * Shape of an error response from the API.
 * The API wraps errors in an `error` envelope:
 *   { "error": { "code": "...", "message": "..." } }
 */
interface ErrorResponse {
  error: {
    code: string;
    message: string;
  };
}

// ─────────────────────────────────────────────────────────────────────────────
// TEST SUITE: profile update
// ─────────────────────────────────────────────────────────────────────────────

test.describe('profile update (PUT /api/v1/users/me)', () => {
  // ───────────────────────────────────────────────────────────────────────────
  // TEST A: Parent can update their phone number
  //
  // SCENARIO
  // ────────
  // Ion Moldovan (parent) calls PUT /api/v1/users/me with a new phone number.
  // The API should:
  //   1. Accept the request (any authenticated user can update their own profile).
  //   2. Return HTTP 200 OK.
  //   3. Return the updated user object with the new phone value.
  //
  // This test verifies the happy path: phone-only update, no email change.
  //
  // WHAT WE ASSERT
  // ──────────────
  //   - HTTP status is 200 (not 400, not 403, not 500).
  //   - response.data.phone equals the value we sent.
  //   - response.data.role is still "parent" (role was not changed).
  //   - Sensitive fields (password_hash, totp_secret) are absent from response.
  // ───────────────────────────────────────────────────────────────────────────
  test('A – parent can update their phone number', async ({ parentPage }) => {
    /**
     * Step 1: Get Ion Moldovan's access token from the authenticated browser.
     * The auth fixture completed login (email + password, no MFA for parents)
     * and stored the token in localStorage.
     */
    const token = await getAccessToken(parentPage);

    /**
     * Step 2: Choose a test phone number.
     * We use a fixed value because the test is idempotent — repeating it
     * should not cause any conflict. Unlike user creation, updating a phone
     * does not have a uniqueness constraint.
     */
    const newPhone = '0741-000-042';

    /**
     * Step 3: Call PUT /api/v1/users/me from the Node.js test process.
     *
     * We send only the phone field. The email field is intentionally omitted —
     * the server should preserve the existing email (COALESCE SQL semantics).
     *
     * Using Node's global fetch() keeps this outside the browser context,
     * which avoids CORS pre-flight complications in the test environment.
     */
    const response = await fetch(`${API_BASE}/users/me`, {
      method: 'PUT',
      headers: {
        'Content-Type': 'application/json',
        Authorization: `Bearer ${token}`,
      },
      body: JSON.stringify({ phone: newPhone }),
    });

    /**
     * Step 4: Assert HTTP 200 OK.
     * Any authenticated user can update their own profile. If we get 403, the
     * endpoint is incorrectly applying role restrictions. If we get 400, the
     * payload validation is too strict. If we get 500, it is a server bug.
     */
    expect(
      response.status,
      `Expected 200 OK from PUT /api/v1/users/me but got ${String(response.status)}. ` +
        'Verify that the parent role can access this endpoint (no RequireRole restriction).',
    ).toBe(200);

    /**
     * Step 5: Parse and assert the response body.
     */
    const body = (await response.json()) as UpdateProfileResponse;

    // The response must have the standard data envelope.
    expect(body.data, 'Response body must have a "data" key').toBeDefined();

    // The phone must be the value we sent.
    expect(body.data.phone, `Expected response.data.phone to be "${newPhone}" after the PUT`).toBe(
      newPhone,
    );

    // The role must still be "parent" — it must NOT have been altered.
    // This is a security assertion: the handler must not allow role changes
    // even if a malicious body sends a different role (it is ignored by design).
    expect(
      body.data.role,
      'Expected response.data.role to remain "parent" — role is immutable via this endpoint',
    ).toBe('parent');

    /**
     * Step 6: Verify that sensitive fields are absent from the response.
     * password_hash and totp_secret must NEVER be returned, even for the user's
     * own profile. These fields are stripped by mapUserToResponse in the handler.
     */
    const responseKeys = Object.keys(body.data as Record<string, unknown>);

    expect(
      responseKeys,
      'Response must not expose "password_hash" — even for the user\'s own profile',
    ).not.toContain('password_hash');

    expect(
      responseKeys,
      'Response must not expose "totp_secret" — even for the user\'s own profile',
    ).not.toContain('totp_secret');
  });

  // ───────────────────────────────────────────────────────────────────────────
  // TEST B: Updated phone appears in GET /users/me response
  //
  // SCENARIO
  // ────────
  // After calling PUT /api/v1/users/me to update the phone, a subsequent call
  // to GET /api/v1/users/me must return the new phone value.
  //
  // This end-to-end round-trip verifies:
  //   1. The PUT endpoint actually persisted the change to the database.
  //   2. The GET endpoint reads and returns the updated value.
  //   3. The two endpoints use consistent data (same DB row, same mapping).
  //
  // WHAT WE ASSERT
  // ──────────────
  //   - PUT /users/me returns 200 (prerequisite step).
  //   - GET /users/me returns 200.
  //   - response.data.phone from GET matches the value sent in the PUT.
  // ───────────────────────────────────────────────────────────────────────────
  test('B – updated phone appears in GET /users/me response', async ({ parentPage }) => {
    const token = await getAccessToken(parentPage);

    /**
     * Step 1: Update the phone via PUT.
     * We use a different phone value than Test A to make this test's assertion
     * independently verifiable (not relying on Test A having run first).
     */
    const phoneForThisTest = '0741-000-099';

    const putResponse = await fetch(`${API_BASE}/users/me`, {
      method: 'PUT',
      headers: {
        'Content-Type': 'application/json',
        Authorization: `Bearer ${token}`,
      },
      body: JSON.stringify({ phone: phoneForThisTest }),
    });

    // The PUT must succeed before we check GET — fail fast if it does not.
    expect(
      putResponse.status,
      `Prerequisite failed: PUT /api/v1/users/me returned ${String(putResponse.status)}. ` +
        'Cannot verify GET round-trip without a successful PUT first.',
    ).toBe(200);

    /**
     * Step 2: Fetch the current user profile via GET /api/v1/users/me.
     * This endpoint returns the stored record for the authenticated user.
     * It is distinct from the PUT handler but reads the same DB row.
     */
    const getResponse = await fetch(`${API_BASE}/users/me`, {
      method: 'GET',
      headers: {
        Authorization: `Bearer ${token}`,
      },
    });

    expect(
      getResponse.status,
      `Expected 200 OK from GET /api/v1/users/me but got ${String(getResponse.status)}.`,
    ).toBe(200);

    /**
     * Step 3: Assert that the GET response contains the phone we just set.
     * If the PUT did not persist, or if the GET reads from a stale cache,
     * this assertion will fail.
     */
    const getBody = (await getResponse.json()) as GetProfileResponse;

    expect(getBody.data, 'GET /users/me response must have a "data" key').toBeDefined();

    expect(
      getBody.data.phone,
      `Expected GET /api/v1/users/me to return phone="${phoneForThisTest}" ` +
        `after PUT updated it. Got: "${String(getBody.data.phone)}". ` +
        'This means the PUT did not persist, or GET reads from a stale view.',
    ).toBe(phoneForThisTest);
  });

  // ───────────────────────────────────────────────────────────────────────────
  // TEST C: Invalid email returns 400
  //
  // SCENARIO
  // ────────
  // Ion Moldovan (parent) calls PUT /api/v1/users/me with an email value that
  // does not contain "@". The API must reject this with 400 Bad Request.
  //
  // The handler validates: if email is provided AND does not contain "@", return
  // 400 with error code "INVALID_EMAIL". This prevents storing garbage strings
  // in the email column that would break future activation / notification flows.
  //
  // WHAT WE ASSERT
  // ──────────────
  //   - HTTP status is 400 (not 200, not 500).
  //   - response.error.code is "INVALID_EMAIL".
  //
  // NOTE: We do NOT assert the exact message text — only the machine-readable
  // code. This follows the convention used in other test files and lets us
  // change the user-facing message without breaking this test.
  // ───────────────────────────────────────────────────────────────────────────
  test('C – invalid email (missing @) returns 400 INVALID_EMAIL', async ({ parentPage }) => {
    const token = await getAccessToken(parentPage);

    /**
     * Step 1: Call PUT /api/v1/users/me with an email that lacks "@".
     * "notanemailaddress" has no "@" character, which is our validation rule.
     * The handler must reject this BEFORE calling the database.
     */
    const response = await fetch(`${API_BASE}/users/me`, {
      method: 'PUT',
      headers: {
        'Content-Type': 'application/json',
        Authorization: `Bearer ${token}`,
      },
      body: JSON.stringify({ email: 'notanemailaddress' }),
    });

    /**
     * Step 2: Assert HTTP 400 Bad Request.
     *
     * 200 would mean the invalid email was stored — that is a data quality bug.
     * 500 would mean the DB rejected it — we want the Go layer to validate first.
     * 403 would mean the route has wrong role restrictions.
     * 400 is the correct response for a client validation failure.
     */
    expect(
      response.status,
      `Expected 400 Bad Request for an email without "@", ` +
        `but got ${String(response.status)}. ` +
        'The handler must validate email format before calling the database.',
    ).toBe(400);

    /**
     * Step 3: Parse and assert the error code.
     * The error envelope must contain code="INVALID_EMAIL" so the frontend can
     * show a user-friendly message like "Please enter a valid email address."
     */
    const body = (await response.json()) as ErrorResponse;

    expect(body.error, 'Response body must have an "error" key for 400 responses').toBeDefined();

    expect(
      body.error.code,
      `Expected error code "INVALID_EMAIL" but got "${body.error.code}". ` +
        'The frontend uses this code to display the correct validation message.',
    ).toBe('INVALID_EMAIL');
  });
});

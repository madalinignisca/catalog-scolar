/**
 * admin/gdpr-compliance.spec.ts
 *
 * Tests for GDPR compliance endpoints (Issue #32):
 *
 *   POST /api/v1/users/me/gdpr/consent  — Record GDPR consent
 *   POST /api/v1/users/me/gdpr/export   — Export personal data (Art. 20 portability)
 *   POST /api/v1/users/me/gdpr/delete   — Anonymise account (Art. 17 erasure)
 *
 * WHAT THIS FILE COVERS
 * ─────────────────────
 * All three GDPR self-service endpoints are accessible to ANY authenticated user
 * with no role restriction. The user ID always comes from the JWT, so a user can
 * only manage their own GDPR data — never another user's.
 *
 * TESTS IN THIS FILE
 * ──────────────────
 *   Test 99  – Parent can record GDPR consent (200 + consent_recorded=true).
 *              POST /api/v1/users/me/gdpr/consent returns 200 OK with a
 *              consent_recorded=true field and a timestamp.
 *
 *   Test 100 – Parent can export their data (200 + profile included).
 *              POST /api/v1/users/me/gdpr/export returns 200 OK with a
 *              "profile" object containing id, role, first_name, last_name.
 *
 *   Test 101 – Data export does NOT contain password_hash or totp_secret.
 *              The export response body must not include sensitive security
 *              fields anywhere in the JSON output, even if nested deeply.
 *
 * APPROACH: API-BASED TESTING
 * ───────────────────────────
 * There is no dedicated frontend UI for GDPR controls yet. We call the API
 * directly from the test (Node.js side) using the global `fetch()` available
 * in Node 18+. Authentication tokens are extracted from localStorage after the
 * auth fixture completes login.
 *
 * NOTE: We deliberately do NOT test the delete endpoint in the E2E suite because
 * it would permanently anonymise the test user's session, breaking subsequent
 * tests that depend on the same authenticated user (Ion Moldovan / parent).
 * The deletion happy path is covered in Go integration tests (handler_test.go).
 *
 * FIXTURES USED
 * ─────────────
 *   parentPage — Ion Moldovan (parent role, no MFA required).
 *                Parents are ideal for GDPR tests because:
 *                  a) They are the primary consent-recording users in CatalogRO
 *                     (GDPR consent at activation unlocks their children's data).
 *                  b) They have no MFA, so login is faster in the test suite.
 *                  c) They have linked children, which exercises the children
 *                     array in the data export response.
 *
 * SEED DATA REFERENCES
 * ─────────────────────
 * All data comes from api/db/seed.sql.
 *
 *   Ion Moldovan (parent)
 *     id:    b1000000-0000-0000-0000-000000000301
 *     email: ion.moldovan@gmail.com
 *     Linked to: Andrei Moldovan (student)
 */

// ── Internal: Auth fixture ─────────────────────────────────────────────────────
// Re-export `test` and `expect` from the fixture so custom pages (parentPage, etc.)
// are available. Do NOT import from '@playwright/test' directly.
import { test, expect } from '../fixtures/auth.fixture';

// ── Shared constants ───────────────────────────────────────────────────────────

/**
 * API base URL — must match the Go server's listen address.
 * The Go server listens on :8080 by default (see api/cmd/server/main.go).
 */
const API_BASE = 'http://localhost:8080/api/v1';

/**
 * PARENT_ION_ID — Ion Moldovan's UUID in the seed database.
 * Used to verify the export profile's id field matches the authenticated user.
 */
const PARENT_ION_ID = 'b1000000-0000-0000-0000-000000000301';

// ── Helper: extract the access token from the authenticated browser ────────────

/**
 * getAccessToken
 *
 * Reads the JWT access token stored in localStorage by the auth fixture.
 * The auth fixture logs in via the real Go API and writes tokens to:
 *   - localStorage.catalogro_access_token  (the JWT)
 *   - localStorage.catalogro_refresh_token (the refresh token)
 *
 * @param page - A Playwright Page that is already authenticated.
 * @returns The JWT access token string, or throws if it is missing.
 */
async function getAccessToken(page: import('@playwright/test').Page): Promise<string> {
  const token = await page.evaluate(() => localStorage.getItem('catalogro_access_token'));

  if (token === null || token === '') {
    throw new Error(
      'catalogro_access_token not found in localStorage. ' +
        'Did the auth fixture complete login successfully?',
    );
  }

  return token;
}

// ── Helper types ───────────────────────────────────────────────────────────────

/**
 * ConsentResponse
 *
 * Shape of the response body from POST /api/v1/users/me/gdpr/consent (200).
 * The API wraps the result in a `data` envelope.
 */
interface ConsentResponse {
  data: {
    /** True if the consent was successfully recorded. */
    consent_recorded: boolean;
    /** RFC3339 timestamp of when consent was recorded (server-side now()). */
    timestamp: string;
  };
}

/**
 * GdprExportProfile
 *
 * Safe subset of the user's profile as returned in the data export.
 * Sensitive fields (password_hash, totp_secret, activation_token) are
 * intentionally absent from this type — they must never appear in the export.
 */
interface GdprExportProfile {
  id: string;
  school_id: string;
  role: string;
  email?: string;
  phone?: string;
  first_name: string;
  last_name: string;
  is_active: boolean;
  gdpr_consent_at?: string;
  activated_at?: string;
  last_login_at?: string;
  created_at: string;
}

/**
 * GdprExportChild
 *
 * A child entry as returned in the GDPR data export's children array.
 */
interface GdprExportChild {
  id: string;
  first_name: string;
  last_name: string;
  email?: string;
  role: string;
  class_id?: string;
  class_name?: string;
  class_education_level?: string;
}

/**
 * ExportResponse
 *
 * Shape of the response body from POST /api/v1/users/me/gdpr/export (200).
 */
interface ExportResponse {
  data: {
    profile: GdprExportProfile;
    children: GdprExportChild[];
  };
}

// ─────────────────────────────────────────────────────────────────────────────
// TEST SUITE: GDPR compliance endpoints
// ─────────────────────────────────────────────────────────────────────────────

test.describe('GDPR compliance endpoints', () => {
  // ───────────────────────────────────────────────────────────────────────────
  // TEST 99: Parent can record GDPR consent (200)
  //
  // SCENARIO
  // ────────
  // Ion Moldovan (parent, no MFA) calls POST /api/v1/users/me/gdpr/consent.
  // The endpoint must:
  //   1. Return HTTP 200 OK.
  //   2. Return consent_recorded=true in the data envelope.
  //   3. Return a non-empty timestamp string (RFC3339 format).
  //
  // WHAT WE ASSERT
  // ──────────────
  //   - HTTP status is 200 OK.
  //   - response.data.consent_recorded is true.
  //   - response.data.timestamp is a non-empty string.
  //
  // GDPR CONTEXT
  // ────────────
  // GDPR Art. 7 requires that consent is freely given, specific, informed, and
  // unambiguous. The act of calling this endpoint represents the affirmative
  // consent action. The timestamp is stored for compliance auditing.
  // ───────────────────────────────────────────────────────────────────────────
  test('99 – parent can record GDPR consent', async ({ parentPage }) => {
    /**
     * Step 1: Get the parent's JWT access token.
     * The auth fixture already logged in as Ion Moldovan (parent, no MFA).
     */
    const token = await getAccessToken(parentPage);

    /**
     * Step 2: Call POST /api/v1/users/me/gdpr/consent.
     * No request body is needed — the authenticated POST is the consent act itself.
     * The handler reads the user ID from the JWT and updates gdpr_consent_at.
     */
    const response = await fetch(`${API_BASE}/users/me/gdpr/consent`, {
      method: 'POST',
      headers: {
        Authorization: `Bearer ${token}`,
        'Content-Type': 'application/json',
      },
    });

    /**
     * Step 3: Assert HTTP 200 OK.
     *
     * A 501 means the route placeholder was not replaced in main.go.
     * A 401 means the auth fixture did not produce a valid JWT.
     * A 500 means the handler encountered a DB or context error.
     */
    expect(
      response.status,
      `Expected 200 OK from POST /users/me/gdpr/consent, got ${String(response.status)}. ` +
        'Check that the route is wired to userHandler.RecordConsent in main.go.',
    ).toBe(200);

    /**
     * Step 4: Parse the response body and verify consent_recorded=true.
     */
    const body = (await response.json()) as ConsentResponse;

    expect(
      body.data,
      'Response must have a "data" envelope with consent_recorded and timestamp fields.',
    ).toBeDefined();

    expect(
      body.data.consent_recorded,
      `Expected consent_recorded=true, got ${String(body.data.consent_recorded)}. ` +
        'SetGDPRConsent should have updated gdpr_consent_at and the handler should return true.',
    ).toBe(true);

    /**
     * Step 5: Verify the timestamp field is a non-empty RFC3339 string.
     * The timestamp is set by the Go handler using time.Now().UTC().Format(time.RFC3339).
     */
    expect(
      body.data.timestamp,
      'Expected a non-empty timestamp string in the response. ' +
        'The handler should return the current UTC time as RFC3339.',
    ).toBeTruthy();

    expect(
      typeof body.data.timestamp,
      `Expected timestamp to be a string, got ${typeof body.data.timestamp}.`,
    ).toBe('string');
  });

  // ───────────────────────────────────────────────────────────────────────────
  // TEST 100: Parent can export their data (200 + profile included)
  //
  // SCENARIO
  // ────────
  // Ion Moldovan (parent) calls POST /api/v1/users/me/gdpr/export.
  // The endpoint must:
  //   1. Return HTTP 200 OK.
  //   2. Return a "profile" object in the data envelope with id, role,
  //      first_name, last_name (at minimum).
  //   3. The profile.id must match Ion Moldovan's seeded UUID.
  //   4. The profile.role must be "parent".
  //
  // WHAT WE ASSERT
  // ──────────────
  //   - HTTP status is 200 OK.
  //   - response.data.profile is an object.
  //   - response.data.profile.id equals PARENT_ION_ID.
  //   - response.data.profile.role equals "parent".
  //   - response.data.children is an array (may contain Andrei Moldovan).
  //
  // GDPR CONTEXT
  // ────────────
  // GDPR Art. 15 (Right of access) and Art. 20 (Right to data portability)
  // require that we provide the data subject's personal data on request, in a
  // structured, commonly used, machine-readable format (JSON qualifies).
  // ───────────────────────────────────────────────────────────────────────────
  test('100 – parent can export their data', async ({ parentPage }) => {
    /**
     * Step 1: Get Ion Moldovan's access token.
     */
    const token = await getAccessToken(parentPage);

    /**
     * Step 2: Call POST /api/v1/users/me/gdpr/export.
     * No body required — the handler uses the JWT to identify the requesting user.
     */
    const response = await fetch(`${API_BASE}/users/me/gdpr/export`, {
      method: 'POST',
      headers: {
        Authorization: `Bearer ${token}`,
        'Content-Type': 'application/json',
      },
    });

    /**
     * Step 3: Assert HTTP 200 OK.
     */
    expect(
      response.status,
      `Expected 200 OK from POST /users/me/gdpr/export, got ${String(response.status)}. ` +
        'Check that the route is wired to userHandler.ExportData in main.go.',
    ).toBe(200);

    /**
     * Step 4: Parse the response and verify the profile is present.
     */
    const body = (await response.json()) as ExportResponse;

    expect(body.data, 'Response must have a "data" envelope.').toBeDefined();

    expect(
      body.data.profile,
      'Response data must contain a "profile" object with the user\'s personal data.',
    ).toBeDefined();

    /**
     * Step 5: Verify the profile belongs to Ion Moldovan.
     * profile.id must match the seeded UUID for Ion Moldovan (parent).
     */
    expect(
      body.data.profile.id,
      `Expected profile.id="${PARENT_ION_ID}" (Ion Moldovan), ` +
        `got "${body.data.profile.id}". ` +
        'The export must be scoped to the authenticated user — never another user.',
    ).toBe(PARENT_ION_ID);

    /**
     * Step 6: Verify the profile.role is "parent".
     * Ion Moldovan is a parent — the export must reflect his actual role.
     */
    expect(
      body.data.profile.role,
      `Expected profile.role="parent", got "${body.data.profile.role}". ` +
        "The GetUserDataExport query should return the user's actual role.",
    ).toBe('parent');

    /**
     * Step 7: Verify children is an array.
     * For parents, the export includes their linked children. Ion Moldovan
     * is linked to Andrei Moldovan (student) in the seed data.
     */
    expect(
      Array.isArray(body.data.children),
      'Expected response.data.children to be an array. ' +
        'All exports include a children key (empty array for non-parents).',
    ).toBe(true);
  });

  // ───────────────────────────────────────────────────────────────────────────
  // TEST 101: Data export does NOT contain password_hash or totp_secret
  //
  // SCENARIO
  // ────────
  // Ion Moldovan calls POST /api/v1/users/me/gdpr/export.
  // The response JSON body must NOT contain "password_hash" or "totp_secret"
  // as keys, anywhere in the response (not even if they are null).
  //
  // WHAT WE ASSERT
  // ──────────────
  //   - HTTP status is 200 OK.
  //   - The raw JSON response string does not contain "password_hash".
  //   - The raw JSON response string does not contain "totp_secret".
  //
  // SECURITY CONTEXT
  // ────────────────
  // Exposing the password_hash (even as a bcrypt hash) would allow an attacker
  // with access to the export to perform offline brute-force attacks.
  // Exposing the totp_secret would allow an attacker to clone the user's TOTP
  // authenticator and bypass 2FA.
  //
  // The gdprExportProfile Go struct deliberately omits both fields. This test
  // acts as a canary: if a future refactor accidentally adds them back, this
  // test will fail immediately, preventing a security regression from shipping.
  // ───────────────────────────────────────────────────────────────────────────
  test('101 – data export does not contain password_hash or totp_secret', async ({
    parentPage,
  }) => {
    /**
     * Step 1: Get Ion Moldovan's access token.
     */
    const token = await getAccessToken(parentPage);

    /**
     * Step 2: Call the data export endpoint.
     */
    const response = await fetch(`${API_BASE}/users/me/gdpr/export`, {
      method: 'POST',
      headers: {
        Authorization: `Bearer ${token}`,
        'Content-Type': 'application/json',
      },
    });

    /**
     * Step 3: Assert HTTP 200 OK.
     * If this fails, the security assertions below are irrelevant.
     */
    expect(
      response.status,
      `Expected 200 OK from POST /users/me/gdpr/export, got ${String(response.status)}.`,
    ).toBe(200);

    /**
     * Step 4: Get the raw response text (before parsing).
     * We intentionally check the raw string to catch the field name appearing
     * anywhere in the JSON — including in nested objects, as a null-valued key,
     * or as part of a field inside a child record.
     *
     * JSON.stringify(await response.json()) would also work, but reading the
     * raw text avoids any re-serialisation differences (e.g., key ordering).
     */
    const rawBody = await response.text();

    /**
     * Step 5: Assert that "password_hash" does not appear as a JSON key.
     *
     * In JSON output, object keys always appear as "key": value.
     * If password_hash were included, the raw string would contain '"password_hash"'.
     * We check for the quoted form to avoid false positives (e.g., a user whose
     * first_name happened to contain the substring "password_hash" literally).
     */
    expect(
      rawBody.includes('"password_hash"'),
      'SECURITY: The GDPR export must NOT contain "password_hash". ' +
        'Exposing the bcrypt hash would allow offline brute-force attacks. ' +
        `Raw response body: ${rawBody.substring(0, 500)}`,
    ).toBe(false);

    /**
     * Step 6: Assert that "totp_secret" does not appear as a JSON key.
     *
     * The TOTP secret is the seed used to generate time-based one-time passwords.
     * Exposing it would allow an attacker to clone the user's authenticator app
     * and bypass two-factor authentication entirely.
     */
    expect(
      rawBody.includes('"totp_secret"'),
      'SECURITY: The GDPR export must NOT contain "totp_secret". ' +
        "Exposing the TOTP seed would allow an attacker to clone the user's 2FA device. " +
        `Raw response body: ${rawBody.substring(0, 500)}`,
    ).toBe(false);
  });
});

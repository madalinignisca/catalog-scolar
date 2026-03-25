/**
 * admin/2fa-setup.spec.ts
 *
 * Tests 102–104: 2FA setup flow via the teacher API.
 *
 * WHAT THIS FILE COVERS
 * ─────────────────────
 * CatalogRO requires 2FA for teacher, admin, and secretary roles. The setup
 * flow consists of two API calls:
 *
 *   POST /api/v1/auth/2fa/setup   → Generate a new TOTP secret + QR URL (200)
 *   POST /api/v1/auth/2fa/verify  → Validate a code and enable 2FA (200 or 400)
 *
 * The setup endpoint does NOT save the secret — it is only persisted after a
 * successful verify call. This two-step design prevents orphaned secrets.
 *
 * TESTS IN THIS FILE
 * ──────────────────
 *   Test 102 – Teacher can call setup and receive a secret and otpauth URL.
 *              POST /api/v1/auth/2fa/setup returns 200 with non-empty
 *              data.secret (base32) and data.url (starts with "otpauth://totp").
 *
 *   Test 103 – Teacher can verify with a valid TOTP code and enable 2FA.
 *              After calling /setup, generate a valid code from the returned
 *              secret and POST it to /verify with the secret. Expect 200 with
 *              { "data": { "enabled": true } }.
 *
 *   Test 104 – Invalid code returns 400 INVALID_CODE.
 *              Send the correct secret but the static wrong code "000000".
 *              Expect HTTP 400 with { "error": { "code": "INVALID_CODE" } }.
 *
 * APPROACH: API-BASED TESTING
 * ───────────────────────────
 * The 2FA setup UI does not yet exist. We therefore call the API directly
 * from Node.js using the global fetch() available in Node 18+.
 *
 * Both endpoints are protected (require a valid JWT Bearer token). We extract
 * page.request which includes httpOnly auth cookies after the teacherPage fixture
 * has completed login.
 *
 * NOTE ON TEST ISOLATION
 * ──────────────────────
 * Tests 102 and 103 call /setup, which generates a NEW random secret each time.
 * The secret from Test 102 is unrelated to any secret that Test 103 generates.
 * Each test that needs to verify calls /setup internally so it has a known
 * secret it can derive a valid code from.
 *
 * NOTE ON THE SEED TEACHER
 * ────────────────────────
 * The seed teacher (Ana Dumitrescu, b1000000-0000-0000-0000-000000000010)
 * already has 2FA enabled (totp_enabled=true, totp_secret set). The /verify
 * endpoint calls SetTOTPSecret regardless of current state, so it will update
 * the secret. This is intentional: re-enrollment (re-scanning the QR code) is
 * a supported use case (e.g., after getting a new phone).
 *
 * TOTP CODE GENERATION
 * ────────────────────
 * We use the generateTOTP helper from test/e2e/helpers/totp.ts, which wraps
 * the `otpauth` npm library (already installed as a devDependency). The helper
 * accepts an arbitrary base32 secret so we can generate codes for any secret
 * returned by /setup.
 *
 * FIXTURES USED
 * ─────────────
 *   teacherPage — Ana Dumitrescu (teacher role, MFA already enabled)
 *
 * SEED DATA REFERENCES
 * ─────────────────────
 * All constants taken from api/db/seed.sql and web/test/e2e/fixtures/auth.fixture.ts.
 */

// ── Internal: Auth fixture ─────────────────────────────────────────────────────
// ── TOTP code generator ────────────────────────────────────────────────────────
import { test, expect } from '../fixtures/auth.fixture';
import { generateTOTP } from '../helpers/totp';

// ── Shared constants ───────────────────────────────────────────────────────────

/**
 * API base URL — must match the Go server's listen address.
 */
const API_BASE = 'http://localhost:8080/api/v1';
// ── Helper types ───────────────────────────────────────────────────────────────

/**
 * SetupResponse
 *
 * JSON body returned by a successful POST /auth/2fa/setup (200).
 * The API wraps the payload in a `data` envelope.
 */
interface SetupResponse {
  data: {
    /** Base32-encoded TOTP secret (e.g. "JBSWY3DPEHPK3PXP"). */
    secret: string;
    /** otpauth:// URI for QR code generation. */
    url: string;
  };
}

/**
 * VerifyResponse
 *
 * JSON body returned by a successful POST /auth/2fa/verify (200).
 */
interface VerifyResponse {
  data: {
    /** True when 2FA was successfully enabled. */
    enabled: boolean;
  };
}

/**
 * ErrorResponse
 *
 * JSON body returned by a failed POST /auth/2fa/verify (400).
 */
interface ErrorResponse {
  error: {
    /** Machine-readable error code (e.g. "INVALID_CODE"). */
    code: string;
    /** Human-readable description of the error. */
    message: string;
  };
}

// ─────────────────────────────────────────────────────────────────────────────
// TEST SUITE: 2FA setup flow
// ─────────────────────────────────────────────────────────────────────────────

test.describe('2FA setup flow', () => {
  // ───────────────────────────────────────────────────────────────────────────
  // TEST 102: Teacher can call setup and receive a secret and otpauth URL
  //
  // SCENARIO
  // ────────
  // Ana Dumitrescu (teacher) calls POST /api/v1/auth/2fa/setup.
  // The API should:
  //   1. Generate a new TOTP secret and otpauth:// URL.
  //   2. Return HTTP 200.
  //   3. Include data.secret (non-empty, base32) and data.url (starts with
  //      "otpauth://totp/").
  //   4. NOT save the secret to the database — this is only step 1.
  //
  // WHAT WE ASSERT
  // ──────────────
  //   - HTTP status is 200 OK.
  //   - response.data.secret is a non-empty string.
  //   - response.data.url starts with "otpauth://totp/".
  // ───────────────────────────────────────────────────────────────────────────
  test('102 – teacher receives secret and otpauth URL from setup', async ({ teacherPage }) => {
    /**
     * Step 1: Call POST /api/v1/auth/2fa/setup.
     * No request body is needed — the server derives the account name from the JWT.
     */
    const response = await teacherPage.request.post(`${API_BASE}/auth/2fa/setup`);

    /**
     * Step 3: Assert HTTP 200 OK.
     *
     * A 401 here means the JWT was rejected or is missing.
     * A 500 here likely means the TOTP library failed to generate a key.
     */
    expect(
      response.status(),
      `Expected 200 OK from POST /auth/2fa/setup, got ${String(response.status())}. ` +
        'Check that the route is wired (not notImplemented) and the JWT is valid.',
    ).toBe(200);

    /**
     * Step 4: Parse and validate the response body.
     */
    const body = (await response.json()) as SetupResponse;

    expect(
      body.data,
      'Expected response body to have a "data" key with the secret and URL',
    ).toBeDefined();

    expect(
      body.data.secret,
      'Expected data.secret to be a non-empty base32-encoded TOTP secret',
    ).toBeTruthy();

    expect(body.data.url, 'Expected data.url to be a non-empty otpauth:// URI').toBeTruthy();

    expect(
      body.data.url.startsWith('otpauth://totp/'),
      `Expected data.url to start with "otpauth://totp/", got: "${body.data.url}"`,
    ).toBe(true);
  });

  // ───────────────────────────────────────────────────────────────────────────
  // TEST 103: Teacher can verify with a valid TOTP code and enable 2FA
  //
  // SCENARIO
  // ────────
  // Ana Dumitrescu (teacher) calls:
  //   1. POST /api/v1/auth/2fa/setup  → receives { secret, url }.
  //   2. Generates a valid TOTP code from the returned secret using otpauth.
  //   3. POST /api/v1/auth/2fa/verify with { secret, code }.
  //      Expects 200 with { "data": { "enabled": true } }.
  //
  // WHAT WE ASSERT
  // ──────────────
  //   - Setup returns 200 with a non-empty secret.
  //   - Verify returns HTTP 200 OK.
  //   - response.data.enabled is true.
  // ───────────────────────────────────────────────────────────────────────────
  test('103 – teacher can verify with a valid TOTP code', async ({ teacherPage }) => {
    /**
     * Step 1: Generate a new TOTP secret via /setup.
     * We call /setup fresh here rather than reusing Test 102's result to keep
     * this test fully self-contained.
     */
    const setupResponse = await teacherPage.request.post(`${API_BASE}/auth/2fa/setup`);

    expect(
      setupResponse.status(),
      `Setup prerequisite failed: expected 200, got ${String(setupResponse.status())}.`,
    ).toBe(200);

    const setupBody = (await setupResponse.json()) as SetupResponse;

    expect(
      setupBody.data.secret,
      'Setup response must include a non-empty secret for the verify step',
    ).toBeTruthy();

    const { secret } = setupBody.data;

    /**
     * Step 2: Generate a valid TOTP code from the secret.
     * generateTOTP accepts an arbitrary base32 secret and returns a 6-digit code
     * valid at the current time (with the race-condition mitigation built in).
     */
    const code = await generateTOTP(secret);

    /**
     * Step 3: Call POST /api/v1/auth/2fa/verify with the secret + code.
     */
    const verifyResponse = await teacherPage.request.post(`${API_BASE}/auth/2fa/verify`, {
      data: { secret, code },
    });

    /**
     * Step 4: Assert HTTP 200 and enabled=true.
     *
     * A 400 here means the code was rejected — check for clock skew or that
     * the otpauth library is using the same algorithm (SHA1, 6 digits, 30s).
     * A 500 means the DB update failed.
     */
    expect(
      verifyResponse.status(),
      `Expected 200 OK from POST /auth/2fa/verify, got ${String(verifyResponse.status())}. ` +
        'Ensure the TOTP code was generated from the correct secret ' +
        'and that both client and server clocks are in sync.',
    ).toBe(200);

    const verifyBody = (await verifyResponse.json()) as VerifyResponse;

    expect(verifyBody.data, 'Expected response body to have a "data" key').toBeDefined();

    expect(
      verifyBody.data.enabled,
      'Expected data.enabled=true after successful 2FA verification',
    ).toBe(true);
  });

  // ───────────────────────────────────────────────────────────────────────────
  // TEST 104: Invalid code returns 400 INVALID_CODE
  //
  // SCENARIO
  // ────────
  // Ana Dumitrescu (teacher) calls:
  //   1. POST /api/v1/auth/2fa/setup  → receives { secret, url }.
  //   2. POST /api/v1/auth/2fa/verify with the correct secret but the static
  //      wrong code "000000".
  //      Expects HTTP 400 with { "error": { "code": "INVALID_CODE" } }.
  //
  // "000000" is chosen because the probability of it being a valid TOTP code
  // for a random secret at any given 30-second window is 1/1,000,000 — the
  // test would only be a false positive once per ~34 years of continuous runs.
  //
  // WHAT WE ASSERT
  // ──────────────
  //   - HTTP status is 400 Bad Request (NOT 200 or 500).
  //   - response.error.code is "INVALID_CODE".
  //   - response.error.message is a non-empty string.
  // ───────────────────────────────────────────────────────────────────────────
  test('104 – invalid code returns 400 INVALID_CODE', async ({ teacherPage }) => {
    /**
     * Step 1: Get a fresh TOTP secret from /setup.
     * We still need the correct secret to ensure the request is well-formed —
     * we want to test that the *code* is wrong, not that the *request* is malformed.
     */
    const setupResponse = await teacherPage.request.post(`${API_BASE}/auth/2fa/setup`);

    expect(
      setupResponse.status(),
      `Setup prerequisite failed: expected 200, got ${String(setupResponse.status())}.`,
    ).toBe(200);

    const setupBody = (await setupResponse.json()) as SetupResponse;
    const { secret } = setupBody.data;

    /**
     * Step 2: Send the correct secret with a deliberately wrong code.
     * "000000" is a static invalid TOTP code.
     */
    const verifyResponse = await teacherPage.request.post(`${API_BASE}/auth/2fa/verify`, {
      data: { secret, code: '000000' },
    });

    /**
     * Step 3: Assert HTTP 400 Bad Request.
     *
     * A 200 here would be a critical security failure — any code would enable 2FA.
     * A 500 would indicate the server crashed instead of handling the invalid code gracefully.
     */
    expect(
      verifyResponse.status(),
      `Expected 400 Bad Request for an invalid TOTP code, ` +
        `got ${String(verifyResponse.status())}. ` +
        'The server must reject wrong codes, not accept or crash on them.',
    ).toBe(400);

    /**
     * Step 4: Assert the error body contains the INVALID_CODE code.
     */
    const body = (await verifyResponse.json()) as ErrorResponse;

    expect(body.error, 'Expected a JSON error body with an "error" key').toBeDefined();

    expect(
      body.error.code,
      `Expected error.code="INVALID_CODE", got "${body.error.code}". ` +
        'The INVALID_CODE code lets the client show a clear message to the user.',
    ).toBe('INVALID_CODE');

    expect(
      body.error.message,
      'Expected a non-empty error.message describing the problem',
    ).toBeTruthy();
  });
});

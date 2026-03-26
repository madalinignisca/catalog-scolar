/**
 * admin/school-year.spec.ts
 *
 * Tests 105–106: Current school year endpoint.
 *
 * WHAT THIS FILE COVERS
 * ─────────────────────
 * Any authenticated user can retrieve the school year that is currently
 * marked as active (is_current = true) for their school. The endpoint:
 *
 *   GET /api/v1/schools/current/year
 *
 * returns the year's label, start/end dates, and semester date boundaries.
 * These dates are consumed by every part of the frontend that needs to
 * display or validate date ranges (grade entry, absence recording, reports).
 *
 * TESTS IN THIS FILE
 * ──────────────────
 *   Test 105 – Authenticated user can get the current school year.
 *              GET /api/v1/schools/current/year with a valid Bearer token
 *              returns 200 OK with the school year data envelope.
 *
 *   Test 106 – Response includes semester dates and label "2026-2027".
 *              The seed data inserts a school year labelled "2026-2027"
 *              with specific ISO 8601 semester date ranges. This test
 *              verifies that all expected fields are present and correct.
 *
 * APPROACH: API-BASED TESTING
 * ───────────────────────────
 * There is no frontend UI for the school year display yet — only the API
 * endpoint is implemented. We call the API directly from the test (Node.js
 * side) using the `fetch()` global available in Node 18+.
 *
 * Authentication tokens are extracted from localStorage via page.evaluate()
 * after the auth fixture has completed login.
 *
 * FIXTURES USED
 * ─────────────
 *   adminPage — school director (admin role, MFA required)
 *
 * SEED DATA REFERENCES
 * ─────────────────────
 * Credentials and school year data match api/db/seed.sql.
 * The seed inserts school year "2026-2027" for "Școala Gimnazială Liviu Rebreanu":
 *   id:         e0000000-0000-0000-0000-000000000001
 *   label:      "2026-2027"
 *   start_date: "2026-09-14"
 *   end_date:   "2027-06-20"
 *   sem1_start: "2026-09-14"
 *   sem1_end:   "2027-01-31"
 *   sem2_start: "2027-02-10"
 *   sem2_end:   "2027-06-20"
 *   is_current: true
 */

// ── Internal: Auth fixture ─────────────────────────────────────────────────────
// Provides pre-authenticated browser pages for each role.
// Re-export `test` and `expect` from this fixture — do NOT import from
// '@playwright/test' directly, or the custom fixtures will not be available.
import { test, expect } from '../fixtures/auth.fixture';

// ── Shared constants ───────────────────────────────────────────────────────────

/**
 * API base URL — must match the Go server's listen address.
 */
const API_BASE = 'http://localhost:8080/api/v1';

// ── Helper: extract the access token from the authenticated browser ────────────

/**
 * getAccessToken
 *
 * Reads the JWT access token that the auth fixture stored in localStorage.
 *
 * The auth fixture logs in via the real API and writes the tokens to:
 *   localStorage['catalogro_access_token']  → short-lived JWT (15 min)
 *
 * @param page - A Playwright Page instance that is already authenticated.
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
 * SchoolYearRecord
 *
 * The shape of the school year object returned inside the `data` envelope
 * by GET /api/v1/schools/current/year.
 *
 * TypeScript strict mode requires explicit types — no `any`.
 * All date fields are ISO 8601 strings ("YYYY-MM-DD").
 */
interface SchoolYearRecord {
  /** UUID of the school year row. */
  id: string;
  /** Human-readable label, e.g. "2026-2027". */
  label: string;
  /** First day of the school year (ISO 8601). */
  start_date: string;
  /** Last day of the school year (ISO 8601). */
  end_date: string;
  /** First day of the first semester (ISO 8601). */
  sem1_start: string;
  /** Last day of the first semester (ISO 8601). */
  sem1_end: string;
  /** First day of the second semester (ISO 8601). */
  sem2_start: string;
  /** Last day of the second semester (ISO 8601). */
  sem2_end: string;
  /** Whether this is the active school year for the school. */
  is_current: boolean;
}

/**
 * SchoolYearResponse
 *
 * The full JSON body returned by GET /api/v1/schools/current/year (200 OK).
 * The API wraps the school year record in the standard data envelope.
 */
interface SchoolYearResponse {
  data: SchoolYearRecord;
}

// ─────────────────────────────────────────────────────────────────────────────
// TEST SUITE: school year
// ─────────────────────────────────────────────────────────────────────────────

test.describe('school year', () => {
  // ───────────────────────────────────────────────────────────────────────────
  // TEST 105: Authenticated user can get the current school year
  //
  // SCENARIO
  // ────────
  // The school director (admin role) calls GET /api/v1/schools/current/year
  // with a valid Bearer token. The API should:
  //   1. Look up the school year with is_current=true for the admin's school.
  //   2. Return HTTP 200 OK.
  //   3. Wrap the result in the standard { "data": { ... } } envelope.
  //   4. Include all expected date fields.
  //
  // WHAT WE ASSERT
  // ──────────────
  //   - HTTP status is 200 OK.
  //   - response.data is an object (not null or an array).
  //   - response.data.id is a non-empty string.
  //   - response.data.is_current is true.
  // ───────────────────────────────────────────────────────────────────────────
  test('105 – authenticated user can get current school year', async ({ adminPage }) => {
    /**
     * Step 1: Get the admin's access token from the authenticated browser
     * session. The auth fixture has already completed login + MFA for us.
     */
    const token = await getAccessToken(adminPage);

    /**
     * Step 2: Call GET /api/v1/schools/current/year directly from Node.js.
     * The Authorization header carries the admin's Bearer token.
     */
    const response = await fetch(`${API_BASE}/schools/current/year`, {
      method: 'GET',
      headers: {
        Authorization: `Bearer ${token}`,
      },
    });

    /**
     * Step 3: Assert HTTP 200 OK.
     *
     * A 404 here means the seed data is missing a school year with
     * is_current=true. Check api/db/seed.sql for the INSERT statement.
     *
     * A 401 here means the Bearer token was not recognised — check that
     * the auth fixture logged in correctly.
     *
     * A 501 means the route was not wired in main.go. Check that
     * r.Get("/schools/current/year", notImplemented) was replaced with
     * the real handler.
     */
    expect(
      response.status,
      `Expected 200 OK from GET /api/v1/schools/current/year but got ${String(response.status)}. ` +
        'Check that: (1) seed.sql inserts a school year with is_current=true, ' +
        '(2) the route is wired to schoolHandler.GetCurrentYear in main.go.',
    ).toBe(200);

    /**
     * Step 4: Parse and assert the response envelope.
     */
    const body = (await response.json()) as SchoolYearResponse;

    expect(
      body.data,
      'Response body must have a "data" key with the school year object',
    ).toBeDefined();

    expect(typeof body.data).toBe('object');

    expect(body.data.id, 'Expected "id" field — the school year UUID').toBeTruthy();

    expect(
      body.data.is_current,
      'Expected is_current=true — only the current year should be returned',
    ).toBe(true);
  });

  // ───────────────────────────────────────────────────────────────────────────
  // TEST 106: Response includes semester dates and label "2026-2027"
  //
  // SCENARIO
  // ────────
  // The seed data in api/db/seed.sql inserts a school year for
  // "Școala Gimnazială Liviu Rebreanu" with these specific values:
  //
  //   label      "2026-2027"
  //   start_date "2026-09-14"
  //   end_date   "2027-06-20"
  //   sem1_start "2026-09-14"
  //   sem1_end   "2027-01-31"
  //   sem2_start "2027-02-10"
  //   sem2_end   "2027-06-20"
  //
  // This test calls GET /api/v1/schools/current/year and verifies that all
  // eight date fields are present and match the seeded values exactly.
  //
  // WHAT WE ASSERT
  // ──────────────
  //   - response.data.label is "2026-2027".
  //   - response.data.start_date is "2026-09-14".
  //   - response.data.end_date is "2027-06-20".
  //   - response.data.sem1_start is "2026-09-14".
  //   - response.data.sem1_end is "2027-01-31".
  //   - response.data.sem2_start is "2027-02-10".
  //   - response.data.sem2_end is "2027-06-20".
  // ───────────────────────────────────────────────────────────────────────────
  test('106 – response includes semester dates and label "2026-2027"', async ({ adminPage }) => {
    /**
     * Step 1: Authenticate and fetch the current school year.
     * We reuse the same fetch pattern as Test 105 — this avoids duplicating
     * the login step and keeps the test focused on field assertions.
     */
    const token = await getAccessToken(adminPage);

    const response = await fetch(`${API_BASE}/schools/current/year`, {
      method: 'GET',
      headers: {
        Authorization: `Bearer ${token}`,
      },
    });

    /**
     * Step 2: Require 200 OK as a prerequisite.
     * If the endpoint fails here, the test is treated as a setup failure
     * rather than a field-assertion failure, so the message is explicit.
     */
    expect(
      response.status,
      `Prerequisite failed: GET /api/v1/schools/current/year returned ${String(response.status)} ` +
        'instead of 200. Cannot assert individual fields without a successful response.',
    ).toBe(200);

    /**
     * Step 3: Parse the response body.
     */
    const body = (await response.json()) as SchoolYearResponse;
    const year = body.data;

    /**
     * Step 4: Assert the label.
     * The seed data labels the current year "2026-2027". If this assertion
     * fails, check that seed.sql has been applied with `make seed`.
     */
    expect(
      year.label,
      `Expected label="2026-2027" but got "${year.label}". ` +
        'Check that api/db/seed.sql inserts the school year with this label ' +
        'and that `make seed` was run before the test.',
    ).toBe('2026-2027');

    /**
     * Step 5: Assert the year-level date boundaries.
     * These are the first and last day of the entire school year.
     */
    expect(year.start_date, `Expected start_date="2026-09-14" but got "${year.start_date}"`).toBe(
      '2026-09-14',
    );

    expect(year.end_date, `Expected end_date="2027-06-20" but got "${year.end_date}"`).toBe(
      '2027-06-20',
    );

    /**
     * Step 6: Assert semester one date boundaries.
     * Semester I runs from the first school day in September to the end
     * of January in the Romanian calendar.
     */
    expect(year.sem1_start, `Expected sem1_start="2026-09-14" but got "${year.sem1_start}"`).toBe(
      '2026-09-14',
    );

    expect(year.sem1_end, `Expected sem1_end="2027-01-31" but got "${year.sem1_end}"`).toBe(
      '2027-01-31',
    );

    /**
     * Step 7: Assert semester two date boundaries.
     * Semester II resumes in February after the inter-semester vacation.
     */
    expect(year.sem2_start, `Expected sem2_start="2027-02-10" but got "${year.sem2_start}"`).toBe(
      '2027-02-10',
    );

    expect(year.sem2_end, `Expected sem2_end="2027-06-20" but got "${year.sem2_end}"`).toBe(
      '2027-06-20',
    );
  });
});

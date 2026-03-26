/**
 * admin/subject-management.spec.ts
 *
 * Tests 79–82: Subject management via the admin API.
 *
 * WHAT THIS FILE COVERS
 * ─────────────────────
 * School admins can create subjects (materii) for their school. Each subject
 * is scoped to an education level (primary / middle / high) and optionally
 * has a semester thesis (teză).
 *
 * These tests exercise two API endpoints:
 *
 *   POST /api/v1/subjects   → Create a new subject (admin only, returns 201)
 *   GET  /api/v1/subjects   → List all active subjects for the school (200)
 *
 * Only the admin role is authorised to call POST /api/v1/subjects.
 * Parents and other non-admin roles must receive 403 Forbidden.
 *
 * TESTS IN THIS FILE
 * ──────────────────
 *   Test 79 – Admin can create a subject.
 *              POST /api/v1/subjects with valid body returns 201 with id,
 *              name, education_level, has_thesis, and short_name.
 *
 *   Test 80 – Created subject appears in GET /subjects list.
 *              After creating a subject via POST, GET /api/v1/subjects must
 *              include the newly created subject in the response array.
 *
 *   Test 81 – Non-admin (parent) gets 403 on POST /subjects.
 *              Ion Moldovan (role: parent) must receive 403 Forbidden when
 *              trying to create a subject.
 *
 *   Test 82 – POST /subjects with missing name returns 400.
 *              Validation: omitting the required "name" field must result
 *              in a 400 Bad Request with a descriptive error body.
 *
 * APPROACH: API-BASED TESTING
 * ───────────────────────────
 * There is no frontend UI for subject management yet — only the API endpoints
 * are implemented. We call the API directly from the test (Node.js side)
 * using the `fetch()` global available in Node 18+.
 *
 * Authentication tokens are extracted from localStorage via page.evaluate()
 * after the auth fixture has completed login.
 *
 * FIXTURES USED
 * ─────────────
 *   adminPage  — school director (admin role, MFA required)
 *   parentPage — Ion Moldovan (parent role, no MFA)
 *
 * SEED DATA REFERENCES
 * ─────────────────────
 * Credentials and user IDs match api/db/seed.sql.
 * See auth.fixture.ts → TEST_USERS for the full list.
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

// ── Helper: generate a unique subject name ─────────────────────────────────────

/**
 * uniqueSubjectName
 *
 * Returns a unique subject name using a timestamp suffix.
 * This prevents conflicts when tests run multiple times against the same DB.
 *
 * Example output: "Matematică E2E 1711234567890"
 *
 * @param prefix - A label for the subject (e.g. "Matematică E2E").
 * @returns A unique subject name string.
 */
function uniqueSubjectName(prefix: string): string {
  return `${prefix} ${String(Date.now())}`;
}

// ── Helper types ───────────────────────────────────────────────────────────────

/**
 * SubjectRecord
 *
 * A single subject entry as returned by the API.
 * TypeScript strict mode requires explicit types — no `any`.
 */
interface SubjectRecord {
  id: string;
  name: string;
  short_name: string | null;
  education_level: string;
  has_thesis: boolean;
}

/**
 * CreateSubjectResponse
 *
 * The JSON body returned by POST /api/v1/subjects on success (201).
 * The API wraps the created subject in a `data` envelope.
 */
interface CreateSubjectResponse {
  data: SubjectRecord;
}

/**
 * ListSubjectsResponse
 *
 * The JSON body returned by GET /api/v1/subjects (200).
 * Array of subject records wrapped in the standard data envelope.
 */
interface ListSubjectsResponse {
  data: SubjectRecord[];
}

// ─────────────────────────────────────────────────────────────────────────────
// TEST SUITE: subject management
// ─────────────────────────────────────────────────────────────────────────────

test.describe('subject management', () => {
  // ───────────────────────────────────────────────────────────────────────────
  // TEST 79: Admin can create a subject
  //
  // SCENARIO
  // ────────
  // The school director (admin role) calls POST /api/v1/subjects with a valid
  // payload. The API should:
  //   1. Create the subject in the database scoped to the admin's school.
  //   2. Return HTTP 201 Created.
  //   3. Include in the response body:
  //        id             — the new subject's UUID
  //        name           — the subject name as stored
  //        education_level — the level as stored ("middle")
  //        has_thesis     — true (as sent)
  //        short_name     — "MAT" (as sent)
  //
  // WHAT WE ASSERT
  // ──────────────
  //   - HTTP status is 201 Created.
  //   - response.data.id is a non-empty string (UUID).
  //   - response.data.name matches the name we sent.
  //   - response.data.education_level is "middle".
  //   - response.data.has_thesis is true.
  //   - response.data.short_name is "MAT".
  // ───────────────────────────────────────────────────────────────────────────
  test('79 – admin can create a subject', async ({ adminPage }) => {
    /**
     * Step 1: Get the admin's access token from the authenticated browser
     * session. The auth fixture already completed login + MFA.
     */
    const token = await getAccessToken(adminPage);

    /**
     * Step 2: Build the payload for a new subject.
     * We use uniqueSubjectName() to avoid name collisions across test reruns.
     */
    const subjectName = uniqueSubjectName('Matematică E2E');
    const payload = {
      name: subjectName,
      short_name: 'MAT',
      education_level: 'middle',
      has_thesis: true,
    };

    /**
     * Step 3: Call POST /api/v1/subjects directly from Node.js.
     * The Authorization header carries the admin's Bearer token.
     */
    const response = await fetch(`${API_BASE}/subjects`, {
      method: 'POST',
      headers: {
        'Content-Type': 'application/json',
        Authorization: `Bearer ${token}`,
      },
      body: JSON.stringify(payload),
    });

    /**
     * Step 4: Assert HTTP 201 Created.
     */
    expect(
      response.status,
      `Expected 201 Created but got ${String(response.status)}. ` +
        'Check that the admin role is authorised for POST /api/v1/subjects.',
    ).toBe(201);

    /**
     * Step 5: Parse and assert the response fields.
     */
    const body = (await response.json()) as CreateSubjectResponse;

    expect(body.data, 'Response body must have a "data" key').toBeDefined();
    expect(body.data.id, 'Expected "id" field — the new subject UUID').toBeTruthy();

    expect(body.data.name, `Expected name="${subjectName}" in response`).toBe(subjectName);

    expect(body.data.education_level, 'Expected education_level="middle" in response').toBe(
      'middle',
    );

    expect(body.data.has_thesis, 'Expected has_thesis=true in response').toBe(true);

    expect(body.data.short_name, 'Expected short_name="MAT" in response').toBe('MAT');
  });

  // ───────────────────────────────────────────────────────────────────────────
  // TEST 80: Created subject appears in GET /subjects list
  //
  // SCENARIO
  // ────────
  // After an admin creates a subject via POST /api/v1/subjects, a subsequent
  // call to GET /api/v1/subjects must include the newly created subject in the
  // response array.
  //
  // This verifies end-to-end persistence: the POST write must be committed to
  // the database and the GET read must return it.
  //
  // WHAT WE ASSERT
  // ──────────────
  //   - POST /api/v1/subjects returns 201 (prerequisite).
  //   - GET /api/v1/subjects returns 200.
  //   - response.data is an array.
  //   - The array contains an entry whose "name" matches the subject we created.
  // ───────────────────────────────────────────────────────────────────────────
  test('80 – created subject appears in GET /subjects list', async ({ adminPage }) => {
    const token = await getAccessToken(adminPage);

    /**
     * Step 1: Create a new subject with a unique name so we can identify it
     * in the list without ambiguity.
     */
    const subjectName = uniqueSubjectName('Fizică E2E');

    const createResponse = await fetch(`${API_BASE}/subjects`, {
      method: 'POST',
      headers: {
        'Content-Type': 'application/json',
        Authorization: `Bearer ${token}`,
      },
      body: JSON.stringify({
        name: subjectName,
        education_level: 'high',
        has_thesis: false,
      }),
    });

    expect(
      createResponse.status,
      `Prerequisite failed: POST /api/v1/subjects returned ${String(createResponse.status)}. ` +
        'Cannot test list without a freshly created subject.',
    ).toBe(201);

    /**
     * Step 2: Fetch the list of subjects.
     * GET /api/v1/subjects returns all active subjects for the current school.
     */
    const listResponse = await fetch(`${API_BASE}/subjects`, {
      method: 'GET',
      headers: {
        Authorization: `Bearer ${token}`,
      },
    });

    expect(
      listResponse.status,
      `Expected 200 OK from GET /api/v1/subjects but got ${String(listResponse.status)}.`,
    ).toBe(200);

    const listBody = (await listResponse.json()) as ListSubjectsResponse;

    expect(Array.isArray(listBody.data), 'Expected response.data to be an array of subjects').toBe(
      true,
    );

    /**
     * Step 3: Verify the subject we just created is in the list.
     * We search by the unique name — the timestamp suffix makes it unique.
     */
    const found = listBody.data.some((s) => s.name === subjectName);

    expect(
      found,
      `Expected to find "${subjectName}" in GET /api/v1/subjects list. ` +
        `List has ${String(listBody.data.length)} entries: ` +
        listBody.data.map((s) => s.name).join(', '),
    ).toBe(true);
  });

  // ───────────────────────────────────────────────────────────────────────────
  // TEST 81: Non-admin (parent) gets 403 on POST /subjects
  //
  // SCENARIO
  // ────────
  // Ion Moldovan (role: parent) tries to call POST /api/v1/subjects. The API
  // must reject this with HTTP 403 Forbidden.
  //
  // Parents can only view their child's grades — they have no administrative
  // privileges. Allowing a parent to create subjects would be a serious
  // access-control failure.
  //
  // WHAT WE ASSERT
  // ──────────────
  //   - HTTP status is 403 Forbidden.
  //   - The API must NOT return 201 or 200 (would be an access-control bug).
  // ───────────────────────────────────────────────────────────────────────────
  test('81 – parent gets 403 Forbidden on POST /subjects', async ({ parentPage }) => {
    /**
     * The parent fixture is logged in as Ion Moldovan (role: parent, no MFA).
     */
    const token = await getAccessToken(parentPage);

    /**
     * Attempt to create a subject with the parent's Bearer token.
     * The API must inspect the `role` claim in the JWT and reject it.
     */
    const response = await fetch(`${API_BASE}/subjects`, {
      method: 'POST',
      headers: {
        'Content-Type': 'application/json',
        Authorization: `Bearer ${token}`,
      },
      body: JSON.stringify({
        name: uniqueSubjectName('Chimie Forbidden'),
        education_level: 'middle',
        has_thesis: false,
      }),
    });

    /**
     * Assert 403 Forbidden.
     *
     * 401 would mean the token was not recognised — that would indicate the
     * token is invalid, not just unauthorised. The correct response for a
     * valid token from a non-admin role is 403.
     *
     * If the API returns 201 here, the RequireRole("admin") middleware is
     * not working as expected — a serious access-control bug.
     */
    expect(
      response.status,
      `Expected 403 Forbidden for a parent calling POST /api/v1/subjects, ` +
        `but got ${String(response.status)}. ` +
        'This is an access-control failure — the parent role must be rejected.',
    ).toBe(403);
  });

  // ───────────────────────────────────────────────────────────────────────────
  // TEST 82: POST /subjects with missing name returns 400
  //
  // SCENARIO
  // ────────
  // The admin sends a POST /api/v1/subjects request without the required "name"
  // field. The API must validate the request body and return HTTP 400 Bad Request
  // before attempting any database operation.
  //
  // WHAT WE ASSERT
  // ──────────────
  //   - HTTP status is 400 Bad Request.
  //   - The response body is a JSON object (not an empty body or HTML error page).
  // ───────────────────────────────────────────────────────────────────────────
  test('82 – POST /subjects without name returns 400 Bad Request', async ({ adminPage }) => {
    const token = await getAccessToken(adminPage);

    /**
     * Send a payload that is missing the required "name" field.
     * education_level is present and valid — so the only issue is the missing name.
     */
    const response = await fetch(`${API_BASE}/subjects`, {
      method: 'POST',
      headers: {
        'Content-Type': 'application/json',
        Authorization: `Bearer ${token}`,
      },
      body: JSON.stringify({
        // "name" intentionally omitted
        education_level: 'primary',
        has_thesis: false,
      }),
    });

    /**
     * Assert HTTP 400 Bad Request.
     *
     * The handler should validate the request body before touching the DB.
     * A 201 here means validation is broken and a nameless subject was created.
     * A 500 here means the validation is missing and the DB rejected it instead.
     * Both are bugs — only 400 is correct.
     */
    expect(
      response.status,
      `Expected 400 Bad Request for missing "name" field, ` +
        `but got ${String(response.status)}. ` +
        'The handler must validate required fields before inserting into the DB.',
    ).toBe(400);

    /**
     * The response must be parseable JSON with an error structure.
     * We only check that the body is valid JSON — not the exact message text,
     * because the exact wording may change without breaking the contract.
     */
    const body: unknown = await response.json().catch(() => null);

    expect(
      body,
      'Expected a JSON error body from 400 response, got null (body was not valid JSON)',
    ).not.toBeNull();
  });
});

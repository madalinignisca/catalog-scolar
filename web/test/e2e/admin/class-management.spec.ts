/**
 * admin/class-management.spec.ts
 *
 * Tests 83–86: Class management via the admin API.
 *
 * WHAT THIS FILE COVERS
 * ─────────────────────
 * School admins can create and update classes (clase) scoped to a school year.
 * Each class has a name (e.g. "5A"), an education level (primary/middle/high),
 * a grade number (1–12), and optionally a homeroom teacher (diriginte) and a
 * maximum number of students.
 *
 * These tests exercise two API endpoints:
 *
 *   POST /api/v1/classes           → Create a new class (admin only, returns 201)
 *   GET  /api/v1/classes           → List all classes for the current school year (200)
 *   PUT  /api/v1/classes/{classId} → Update an existing class (admin only, returns 200)
 *
 * Only the admin role is authorised to call POST and PUT /api/v1/classes.
 * Parents and other non-admin roles must receive 403 Forbidden on those mutations.
 *
 * TESTS IN THIS FILE
 * ──────────────────
 *   Test 83 – Admin can create a class via API (201).
 *              POST /api/v1/classes with a valid body returns 201 and the
 *              created class object (id, name, education_level, grade_number).
 *
 *   Test 84 – Created class appears in GET /classes list.
 *              After creating a class via POST, GET /api/v1/classes must
 *              include the newly created class in the response array.
 *
 *   Test 85 – Admin can update class name (200).
 *              PUT /api/v1/classes/{classId} renames an existing class and
 *              returns the updated class with the new name.
 *
 *   Test 86 – Parent gets 403 on POST /classes.
 *              Ion Moldovan (role: parent) must receive 403 Forbidden when
 *              trying to create a class. Mutations are admin-only.
 *
 * APPROACH: API-BASED TESTING
 * ───────────────────────────
 * There is no frontend UI for class management yet — only the API endpoints
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

// ── Helper: generate a unique class name ───────────────────────────────────────

/**
 * uniqueClassName
 *
 * Returns a unique class name using a timestamp suffix.
 * This prevents conflicts when tests run multiple times against the same DB.
 * Class names are short (e.g. "5A"), so we append a 4-digit millisecond
 * suffix to stay within realistic naming patterns.
 *
 * Example output: "5A-1234"
 *
 * @param base - Base class label (e.g. "5A", "9B").
 * @returns A unique class name string.
 */
function uniqueClassName(base: string): string {
  // Use the last 4 digits of the timestamp to keep names short.
  return `${base}-${String(Date.now()).slice(-4)}`;
}

// ── Helper types ───────────────────────────────────────────────────────────────

/**
 * ClassRecord
 *
 * A single class entry as returned by the API.
 * TypeScript strict mode requires explicit types — no `any`.
 */
interface ClassRecord {
  id: string;
  school_year_id: string;
  name: string;
  education_level: string;
  grade_number: number;
  homeroom_teacher_id: string | null;
  max_students: number | null;
}

/**
 * CreateClassResponse
 *
 * The JSON body returned by POST /api/v1/classes on success (201).
 * The API wraps the created class in a `data` envelope.
 */
interface CreateClassResponse {
  data: ClassRecord;
}

/**
 * UpdateClassResponse
 *
 * The JSON body returned by PUT /api/v1/classes/{classId} on success (200).
 */
interface UpdateClassResponse {
  data: ClassRecord;
}

/**
 * ListClassesResponse
 *
 * The JSON body returned by GET /api/v1/classes (200).
 * Array of class records wrapped in the standard data envelope.
 */
interface ListClassesResponse {
  data: ClassRecord[];
}

// ── Helper: fetch the current school year ID from the API ──────────────────────

/**
 * getCurrentSchoolYearId
 *
 * Calls GET /api/v1/schools/current/year (or derives the year from GET /classes)
 * to get the active school_year_id for use when creating a class.
 *
 * Since GET /schools/current/year is not yet implemented (returns 501), we
 * create a class and read its school_year_id back from the response, then
 * use that ID for subsequent calls. This is a pragmatic bootstrapping approach
 * for tests while the school year endpoint is pending.
 *
 * ALTERNATIVE: seed.sql must contain a known school_year_id that we can
 * hard-code here. We use the dynamic approach to avoid coupling to a seed UUID.
 *
 * @param token - The admin's JWT access token.
 * @returns The school_year_id UUID string.
 */
async function getSchoolYearId(token: string): Promise<string> {
  // Probe by listing classes — if any exist, take the first one's school_year_id.
  const listResp = await fetch(`${API_BASE}/classes`, {
    headers: { Authorization: `Bearer ${token}` },
  });

  if (listResp.ok) {
    const body = (await listResp.json()) as ListClassesResponse;
    if (Array.isArray(body.data) && body.data.length > 0 && body.data[0].school_year_id) {
      return body.data[0].school_year_id;
    }
  }

  // GET /classes succeeded but returned an empty list — no school year to infer.
  // This means the seed data has not been loaded yet. The tests that call this
  // helper will skip themselves with a clear message via test.skip().
  throw new Error(
    `Cannot determine current school_year_id: GET /classes returned ${String(listResp.status)} ` +
      `with no class data. Make sure the database seed has run (make seed).`,
  );
}

// ─────────────────────────────────────────────────────────────────────────────
// TEST SUITE: class management
// ─────────────────────────────────────────────────────────────────────────────

test.describe('class management', () => {
  // ───────────────────────────────────────────────────────────────────────────
  // TEST 83: Admin can create a class via API (201)
  //
  // SCENARIO
  // ────────
  // The school director (admin role) calls POST /api/v1/classes with a valid
  // payload. The API should:
  //   1. Create the class in the database scoped to the admin's school.
  //   2. Return HTTP 201 Created.
  //   3. Include in the response body:
  //        id             — the new class's UUID
  //        name           — the class name as stored
  //        education_level — "middle"
  //        grade_number   — 5
  //
  // We obtain the school_year_id by first listing existing classes (from seed
  // data) or by using the seed's known school year. If GET /classes returns an
  // empty list, the test is skipped with a clear message.
  //
  // WHAT WE ASSERT
  // ──────────────
  //   - HTTP status is 201 Created.
  //   - response.data.id is a non-empty string (UUID).
  //   - response.data.name matches the name we sent.
  //   - response.data.education_level is "middle".
  //   - response.data.grade_number is 5.
  // ───────────────────────────────────────────────────────────────────────────
  test('83 – admin can create a class', async ({ adminPage }) => {
    /**
     * Step 1: Get the admin's access token from the authenticated browser session.
     */
    const token = await getAccessToken(adminPage);

    /**
     * Step 2: Discover the current school_year_id by listing existing classes.
     * The seed data (make seed) creates at least one class, so this should succeed.
     */
    let schoolYearId: string;
    try {
      schoolYearId = await getSchoolYearId(token);
    } catch (err) {
      // If we cannot determine the school year, skip the test with a clear message.
      test.skip(true, String(err));
      return;
    }

    /**
     * Step 3: Build the payload for a new class.
     * We use uniqueClassName() to avoid name collisions across test reruns.
     */
    const className = uniqueClassName('5A');
    const payload = {
      school_year_id: schoolYearId,
      name: className,
      education_level: 'middle',
      grade_number: 5,
    };

    /**
     * Step 4: Call POST /api/v1/classes.
     */
    const response = await fetch(`${API_BASE}/classes`, {
      method: 'POST',
      headers: {
        'Content-Type': 'application/json',
        Authorization: `Bearer ${token}`,
      },
      body: JSON.stringify(payload),
    });

    /**
     * Step 5: Assert HTTP 201 Created.
     */
    expect(
      response.status,
      `Expected 201 Created but got ${String(response.status)}. ` +
        'Check that the admin role is authorised for POST /api/v1/classes.',
    ).toBe(201);

    /**
     * Step 6: Parse and assert the response fields.
     */
    const body = (await response.json()) as CreateClassResponse;

    expect(body.data, 'Response body must have a "data" key').toBeDefined();
    expect(body.data.id, 'Expected "id" field — the new class UUID').toBeTruthy();

    expect(body.data.name, `Expected name="${className}" in response`).toBe(className);

    expect(body.data.education_level, 'Expected education_level="middle" in response').toBe(
      'middle',
    );

    expect(body.data.grade_number, 'Expected grade_number=5 in response').toBe(5);
  });

  // ───────────────────────────────────────────────────────────────────────────
  // TEST 84: Created class appears in GET /classes list
  //
  // SCENARIO
  // ────────
  // After an admin creates a class via POST /api/v1/classes, a subsequent call
  // to GET /api/v1/classes must include the newly created class in the response.
  //
  // This verifies end-to-end persistence: the POST write must be committed to
  // the database and the GET read must return it.
  //
  // WHAT WE ASSERT
  // ──────────────
  //   - POST /api/v1/classes returns 201 (prerequisite).
  //   - GET /api/v1/classes returns 200.
  //   - response.data is an array.
  //   - The array contains an entry whose "name" matches the class we created.
  // ───────────────────────────────────────────────────────────────────────────
  test('84 – created class appears in GET /classes list', async ({ adminPage }) => {
    const token = await getAccessToken(adminPage);

    let schoolYearId: string;
    try {
      schoolYearId = await getSchoolYearId(token);
    } catch (err) {
      test.skip(true, String(err));
      return;
    }

    /**
     * Step 1: Create a new class with a unique name so we can identify it
     * in the list without ambiguity.
     */
    const className = uniqueClassName('7B');

    const createResponse = await fetch(`${API_BASE}/classes`, {
      method: 'POST',
      headers: {
        'Content-Type': 'application/json',
        Authorization: `Bearer ${token}`,
      },
      body: JSON.stringify({
        school_year_id: schoolYearId,
        name: className,
        education_level: 'middle',
        grade_number: 7,
      }),
    });

    expect(
      createResponse.status,
      `Prerequisite failed: POST /api/v1/classes returned ${String(createResponse.status)}. ` +
        'Cannot test list without a freshly created class.',
    ).toBe(201);

    /**
     * Step 2: Fetch the list of classes for the current school year.
     * GET /api/v1/classes returns all classes the user is authorised to see.
     */
    const listResponse = await fetch(`${API_BASE}/classes`, {
      method: 'GET',
      headers: {
        Authorization: `Bearer ${token}`,
      },
    });

    expect(
      listResponse.status,
      `Expected 200 OK from GET /api/v1/classes but got ${String(listResponse.status)}.`,
    ).toBe(200);

    const listBody = (await listResponse.json()) as ListClassesResponse;

    expect(Array.isArray(listBody.data), 'Expected response.data to be an array of classes').toBe(
      true,
    );

    /**
     * Step 3: Verify the class we just created is in the list.
     */
    const found = listBody.data.some((c) => c.name === className);

    expect(
      found,
      `Expected to find "${className}" in GET /api/v1/classes list. ` +
        `List has ${String(listBody.data.length)} entries: ` +
        listBody.data.map((c) => c.name).join(', '),
    ).toBe(true);
  });

  // ───────────────────────────────────────────────────────────────────────────
  // TEST 85: Admin can update class name (200)
  //
  // SCENARIO
  // ────────
  // The school director (admin role) calls PUT /api/v1/classes/{classId} to
  // rename a class. The API should:
  //   1. Update the class name in the database.
  //   2. Return HTTP 200 OK.
  //   3. Include in the response body the updated name.
  //
  // WHAT WE ASSERT
  // ──────────────
  //   - POST /api/v1/classes returns 201 (prerequisite: create the class first).
  //   - PUT /api/v1/classes/{id} returns 200 OK.
  //   - response.data.name matches the new name we sent.
  //   - response.data.id matches the class we updated.
  // ───────────────────────────────────────────────────────────────────────────
  test('85 – admin can update class name', async ({ adminPage }) => {
    const token = await getAccessToken(adminPage);

    let schoolYearId: string;
    try {
      schoolYearId = await getSchoolYearId(token);
    } catch (err) {
      test.skip(true, String(err));
      return;
    }

    /**
     * Step 1: Create a class to update.
     */
    const originalName = uniqueClassName('8C');

    const createResponse = await fetch(`${API_BASE}/classes`, {
      method: 'POST',
      headers: {
        'Content-Type': 'application/json',
        Authorization: `Bearer ${token}`,
      },
      body: JSON.stringify({
        school_year_id: schoolYearId,
        name: originalName,
        education_level: 'middle',
        grade_number: 8,
      }),
    });

    expect(
      createResponse.status,
      `Prerequisite failed: POST /api/v1/classes returned ${String(createResponse.status)}.`,
    ).toBe(201);

    const createBody = (await createResponse.json()) as CreateClassResponse;
    const classId = createBody.data.id;

    /**
     * Step 2: Rename the class.
     * We send a PUT with only the "name" field — max_students and
     * homeroom_teacher_id are omitted (will be set to null by the handler,
     * since the JSON null/absent decodes to nil and clears the optional fields).
     */
    const newName = uniqueClassName('8D');

    const updateResponse = await fetch(`${API_BASE}/classes/${classId}`, {
      method: 'PUT',
      headers: {
        'Content-Type': 'application/json',
        Authorization: `Bearer ${token}`,
      },
      body: JSON.stringify({
        name: newName,
      }),
    });

    /**
     * Step 3: Assert HTTP 200 OK.
     */
    expect(
      updateResponse.status,
      `Expected 200 OK from PUT /api/v1/classes/${classId}, ` +
        `but got ${String(updateResponse.status)}.`,
    ).toBe(200);

    /**
     * Step 4: Assert the response body reflects the update.
     */
    const updateBody = (await updateResponse.json()) as UpdateClassResponse;

    expect(updateBody.data, 'Response body must have a "data" key').toBeDefined();

    expect(updateBody.data.id, 'Response "id" must match the class we updated').toBe(classId);

    expect(
      updateBody.data.name,
      `Expected updated name="${newName}" in response, got "${updateBody.data.name}"`,
    ).toBe(newName);
  });

  // ───────────────────────────────────────────────────────────────────────────
  // TEST 86: Parent gets 403 on POST /classes
  //
  // SCENARIO
  // ────────
  // Ion Moldovan (role: parent) tries to call POST /api/v1/classes. The API
  // must reject this with HTTP 403 Forbidden.
  //
  // Parents can only view their child's grades — they have no administrative
  // privileges. Allowing a parent to create classes would be a serious
  // access-control failure.
  //
  // WHAT WE ASSERT
  // ──────────────
  //   - HTTP status is 403 Forbidden.
  //   - The API must NOT return 201 (would be an access-control bug).
  // ───────────────────────────────────────────────────────────────────────────
  test('86 – parent gets 403 Forbidden on POST /classes', async ({ parentPage }) => {
    /**
     * The parent fixture is logged in as Ion Moldovan (role: parent, no MFA).
     */
    const token = await getAccessToken(parentPage);

    /**
     * Attempt to create a class with the parent's Bearer token.
     * The API must inspect the `role` claim in the JWT and reject it.
     * We use a random school_year_id so the payload at least looks structurally
     * valid — we want to test the role check, not the validation logic.
     */
    const response = await fetch(`${API_BASE}/classes`, {
      method: 'POST',
      headers: {
        'Content-Type': 'application/json',
        Authorization: `Bearer ${token}`,
      },
      body: JSON.stringify({
        school_year_id: '00000000-0000-0000-0000-000000000001',
        name: uniqueClassName('ForbiddenClass'),
        education_level: 'middle',
        grade_number: 5,
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
     * not working — a serious access-control bug.
     */
    expect(
      response.status,
      `Expected 403 Forbidden for a parent calling POST /api/v1/classes, ` +
        `but got ${String(response.status)}. ` +
        'This is an access-control failure — the parent role must be rejected.',
    ).toBe(403);
  });
});

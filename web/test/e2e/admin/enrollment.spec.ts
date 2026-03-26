/**
 * admin/enrollment.spec.ts
 *
 * Tests 87–91: Student enrollment management via the admin/secretary API.
 *
 * WHAT THIS FILE COVERS
 * ─────────────────────
 * Secretaries (and admins) can enrol and unenrol students from classes. Each
 * enrollment is scoped to a (class_id, student_id) pair — the same student
 * may not appear in the same class twice. Parents have no access to these
 * endpoints.
 *
 * These tests exercise two API endpoints:
 *
 *   POST   /api/v1/classes/{classId}/enroll            → Enrol a student (201)
 *   DELETE /api/v1/classes/{classId}/enroll/{studentId} → Unenrol a student (204)
 *
 * Only the admin and secretary roles are authorised to call these endpoints.
 * Parents must receive 403 Forbidden.
 *
 * TESTS IN THIS FILE
 * ──────────────────
 *   Test 87 – Secretary can enrol a student (201).
 *              POST /api/v1/classes/{classId}/enroll with a valid student_id
 *              returns 201 with the new enrollment record in the data envelope.
 *
 *   Test 88 – Enrolled student appears in class roster.
 *              After enrolling via POST, GET /api/v1/classes/{classId} must
 *              include the newly-enrolled student in the students array.
 *
 *   Test 89 – Secretary can unenrol a student (204).
 *              DELETE /api/v1/classes/{classId}/enroll/{studentId} returns
 *              204 No Content with an empty body.
 *
 *   Test 90 – Duplicate enrollment returns 409 Conflict.
 *              Enrolling a student who is already in the class triggers the
 *              UNIQUE(class_id, student_id) constraint → 409 with
 *              error code DUPLICATE_ENROLLMENT.
 *
 *   Test 91 – Parent gets 403 Forbidden on POST /enroll.
 *              Ion Moldovan (role: parent) must be rejected with HTTP 403.
 *
 * APPROACH: API-BASED TESTING
 * ───────────────────────────
 * There is no frontend UI for enrollment management yet. We call the API
 * directly from the test (Node.js side) using the `fetch()` global available
 * in Node 18+.
 *
 * Authentication tokens are extracted from localStorage via page.evaluate()
 * after the auth fixture has completed login.
 *
 * FIXTURES USED
 * ─────────────
 *   secretaryPage — Elena Ionescu (secretary role, MFA required)
 *   parentPage    — Ion Moldovan (parent role, no MFA)
 *
 * SEED DATA REFERENCES
 * ─────────────────────
 * All IDs are taken from api/db/seed.sql and web/test/e2e/fixtures/auth.fixture.ts.
 *
 *   class6B  → id: f1000000-0000-0000-0000-000000000002 (middle school class)
 *   Andrei Moldovan (student, userId: b1000000-0000-0000-0000-000000000101)
 *     — is enrolled in class2A but NOT in class6B at seed time, so we can
 *       freely enrol/unenrol him against class6B without conflicting with
 *       other test runs (as long as tests in this file run sequentially).
 *   Student 201  → b1000000-0000-0000-0000-000000000201
 *     — already enrolled in class6B in seed data → perfect for the duplicate test.
 */

// ── Internal: Auth fixture ─────────────────────────────────────────────────────
// Re-export `test` and `expect` from the fixture so custom pages are available.
import { test, expect } from '../fixtures/auth.fixture';

// ── Shared constants ───────────────────────────────────────────────────────────

/**
 * API base URL — must match the Go server's listen address.
 */
const API_BASE = 'http://localhost:8080/api/v1';

/**
 * CLASS_6B_ID — the seeded "6B" middle-school class.
 * Seeded in api/db/seed.sql. Also available as TEST_CLASSES.class6B.id in
 * the auth fixture, but we inline the string here so this file is self-contained
 * and readable without jumping to another file.
 */
const CLASS_6B_ID = 'f1000000-0000-0000-0000-000000000002';

/**
 * STUDENT_ANDREI_ID — Andrei Moldovan (student role).
 * Enrolled in class2A at seed time but NOT in class6B.
 * This makes him a safe target for enrol/unenrol tests against class6B.
 */
const STUDENT_ANDREI_ID = 'b1000000-0000-0000-0000-000000000101';

/**
 * STUDENT_201_ID — a seeded student who is already enrolled in class6B.
 * Used for the duplicate-enrollment test (Test 90).
 */
const STUDENT_201_ID = 'b1000000-0000-0000-0000-000000000201';

// ── Helper: extract the access token from the authenticated browser ────────────

/**
 * getAccessToken
 *
 * Reads the JWT access token stored in localStorage by the auth fixture.
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
 * EnrollmentRecord
 *
 * A single enrollment entry as returned by POST /api/v1/classes/{classId}/enroll.
 * TypeScript strict mode requires explicit types — no `any`.
 */
interface EnrollmentRecord {
  id: string;
  class_id: string;
  student_id: string;
}

/**
 * EnrollResponse
 *
 * JSON body returned by a successful enrollment (201).
 * The API wraps the created enrollment in a `data` envelope.
 */
interface EnrollResponse {
  data: EnrollmentRecord;
}

/**
 * StudentBrief
 *
 * A compact student entry as returned inside GET /classes/{classId}.students.
 */
interface StudentBrief {
  id: string;
  first_name: string;
  last_name: string;
}

/**
 * ClassDetailResponse
 *
 * JSON body returned by GET /api/v1/classes/{classId} (200).
 */
interface ClassDetailResponse {
  data: {
    id: string;
    name: string;
    students: StudentBrief[];
  };
}

// ─────────────────────────────────────────────────────────────────────────────
// TEST SUITE: enrollment management
// ─────────────────────────────────────────────────────────────────────────────

test.describe('enrollment management', () => {
  // ───────────────────────────────────────────────────────────────────────────
  // TEST 87: Secretary can enrol a student (201)
  //
  // SCENARIO
  // ────────
  // Elena Ionescu (secretary) calls POST /api/v1/classes/{classId}/enroll with
  // a valid student_id. The student (Andrei Moldovan) is currently enrolled in
  // class2A but NOT class6B. The API should:
  //   1. Insert a class_enrollment row into the database.
  //   2. Return HTTP 201 Created.
  //   3. Include in the response body: id, class_id, student_id.
  //
  // WHAT WE ASSERT
  // ──────────────
  //   - HTTP status is 201 Created.
  //   - response.data.id is a non-empty UUID string.
  //   - response.data.class_id matches CLASS_6B_ID.
  //   - response.data.student_id matches STUDENT_ANDREI_ID.
  // ───────────────────────────────────────────────────────────────────────────
  test('87 – secretary can enrol a student', async ({ secretaryPage }) => {
    /**
     * Step 1: Get the secretary's JWT access token.
     * The auth fixture already completed login + MFA for Elena Ionescu.
     */
    const token = await getAccessToken(secretaryPage);

    /**
     * Step 2: Call POST /api/v1/classes/{classId}/enroll.
     * We enrol Andrei Moldovan (who is NOT yet in class6B) so there is no
     * pre-existing enrollment to conflict with.
     */
    const response = await fetch(`${API_BASE}/classes/${CLASS_6B_ID}/enroll`, {
      method: 'POST',
      headers: {
        'Content-Type': 'application/json',
        Authorization: `Bearer ${token}`,
      },
      body: JSON.stringify({ student_id: STUDENT_ANDREI_ID }),
    });

    /**
     * Step 3: Assert HTTP 201 Created.
     */
    expect(
      response.status,
      `Expected 201 Created but got ${String(response.status)}. ` +
        'Check that the secretary role is authorised for POST /enroll and ' +
        'that the student is not already enrolled.',
    ).toBe(201);

    /**
     * Step 4: Parse and assert the response fields.
     */
    const body = (await response.json()) as EnrollResponse;

    expect(body.data, 'Response body must have a "data" key').toBeDefined();
    expect(body.data.id, 'Expected "id" field — the new enrollment UUID').toBeTruthy();

    expect(body.data.class_id, `Expected class_id="${CLASS_6B_ID}" in response`).toBe(CLASS_6B_ID);

    expect(body.data.student_id, `Expected student_id="${STUDENT_ANDREI_ID}" in response`).toBe(
      STUDENT_ANDREI_ID,
    );
  });

  // ───────────────────────────────────────────────────────────────────────────
  // TEST 88: Enrolled student appears in class roster
  //
  // SCENARIO
  // ────────
  // After enrolling Andrei Moldovan in class6B via POST /enroll, a subsequent
  // call to GET /api/v1/classes/{classId} must include him in the students array.
  //
  // This verifies end-to-end persistence: the POST write must be committed and
  // the GET must return it in the same request scope (not just in-memory).
  //
  // WHAT WE ASSERT
  // ──────────────
  //   - POST /enroll returns 201 (prerequisite).
  //   - GET /classes/{classId} returns 200.
  //   - response.data.students is an array.
  //   - The array contains an entry whose "id" matches STUDENT_ANDREI_ID.
  // ───────────────────────────────────────────────────────────────────────────
  test('88 – enrolled student appears in class roster', async ({ secretaryPage }) => {
    const token = await getAccessToken(secretaryPage);

    /**
     * Step 1: Enrol Andrei Moldovan in class6B.
     * We use the same student/class combination as Test 87.
     * If this test runs after Test 87 within the same test session, the student
     * may already be enrolled (from Test 87's data committed to DB). We allow
     * both 201 (fresh enrolment) and 409 (already enrolled from previous run)
     * as valid prerequisites — what matters is that the student IS enrolled.
     */
    const enrollResponse = await fetch(`${API_BASE}/classes/${CLASS_6B_ID}/enroll`, {
      method: 'POST',
      headers: {
        'Content-Type': 'application/json',
        Authorization: `Bearer ${token}`,
      },
      body: JSON.stringify({ student_id: STUDENT_ANDREI_ID }),
    });

    // Accept 201 (newly enrolled) or 409 (already enrolled from a previous test).
    // Either way, the student is in the class — which is the precondition we need.
    expect(
      [201, 409].includes(enrollResponse.status),
      `Prerequisite: expected 201 or 409 from POST /enroll, got ${String(enrollResponse.status)}.`,
    ).toBe(true);

    /**
     * Step 2: Fetch the class detail which includes the students array.
     * GET /api/v1/classes/{classId} returns the class with its enrolled students.
     */
    const classResponse = await fetch(`${API_BASE}/classes/${CLASS_6B_ID}`, {
      method: 'GET',
      headers: { Authorization: `Bearer ${token}` },
    });

    expect(
      classResponse.status,
      `Expected 200 OK from GET /classes/${CLASS_6B_ID}, got ${String(classResponse.status)}.`,
    ).toBe(200);

    const classBody = (await classResponse.json()) as ClassDetailResponse;

    expect(
      Array.isArray(classBody.data.students),
      'Expected response.data.students to be an array',
    ).toBe(true);

    /**
     * Step 3: Verify Andrei Moldovan appears in the students array.
     */
    const found = classBody.data.students.some((s) => s.id === STUDENT_ANDREI_ID);

    expect(
      found,
      `Expected student "${STUDENT_ANDREI_ID}" to appear in class6B students. ` +
        `Found ${String(classBody.data.students.length)} students: ` +
        classBody.data.students.map((s) => s.id).join(', '),
    ).toBe(true);
  });

  // ───────────────────────────────────────────────────────────────────────────
  // TEST 89: Secretary can unenrol a student (204)
  //
  // SCENARIO
  // ────────
  // After Andrei Moldovan has been enrolled in class6B (from Tests 87/88),
  // the secretary calls DELETE /classes/{classId}/enroll/{studentId}.
  // The API should return HTTP 204 No Content with an empty body.
  //
  // WHAT WE ASSERT
  // ──────────────
  //   - HTTP status is 204 No Content.
  //   - Response body is empty (no JSON, no error text).
  // ───────────────────────────────────────────────────────────────────────────
  test('89 – secretary can unenrol a student', async ({ secretaryPage }) => {
    const token = await getAccessToken(secretaryPage);

    /**
     * Step 1: Ensure the student is enrolled before we try to remove them.
     * This makes the test independent of whether Test 87 ran first.
     * A 409 here means they are already enrolled — that is fine for our purpose.
     */
    await fetch(`${API_BASE}/classes/${CLASS_6B_ID}/enroll`, {
      method: 'POST',
      headers: {
        'Content-Type': 'application/json',
        Authorization: `Bearer ${token}`,
      },
      body: JSON.stringify({ student_id: STUDENT_ANDREI_ID }),
    });
    // We intentionally do not assert the status here — we only care that the
    // student exists in the class before the DELETE runs.

    /**
     * Step 2: Call DELETE /classes/{classId}/enroll/{studentId}.
     */
    const response = await fetch(`${API_BASE}/classes/${CLASS_6B_ID}/enroll/${STUDENT_ANDREI_ID}`, {
      method: 'DELETE',
      headers: { Authorization: `Bearer ${token}` },
    });

    /**
     * Step 3: Assert HTTP 204 No Content.
     */
    expect(
      response.status,
      `Expected 204 No Content but got ${String(response.status)}. ` +
        'Check that the secretary role is authorised for DELETE /enroll/{studentId}.',
    ).toBe(204);

    /**
     * Step 4: Assert the response body is empty.
     * A 204 response MUST NOT include a message body (RFC 9110 §15.3.5).
     */
    const bodyText = await response.text();
    expect(bodyText, `Expected empty body for 204 response, got: "${bodyText}"`).toBe('');
  });

  // ───────────────────────────────────────────────────────────────────────────
  // TEST 90: Duplicate enrollment returns 409 Conflict
  //
  // SCENARIO
  // ────────
  // Student 201 (b1000000-...-000000000201) is already enrolled in class6B
  // in the seed data. Attempting to enrol them again must trigger the
  // UNIQUE(class_id, student_id) constraint and return HTTP 409 Conflict
  // with error code DUPLICATE_ENROLLMENT.
  //
  // WHAT WE ASSERT
  // ──────────────
  //   - HTTP status is 409 Conflict (NOT 201, 200, or 500).
  //   - Response body contains { "error": { "code": "DUPLICATE_ENROLLMENT", ... } }.
  // ───────────────────────────────────────────────────────────────────────────
  test('90 – duplicate enrollment returns 409 Conflict', async ({ secretaryPage }) => {
    const token = await getAccessToken(secretaryPage);

    /**
     * Try to enrol student 201 in class6B.
     * This student is already enrolled via seed data, so the unique constraint
     * fires immediately — no setup needed.
     */
    const response = await fetch(`${API_BASE}/classes/${CLASS_6B_ID}/enroll`, {
      method: 'POST',
      headers: {
        'Content-Type': 'application/json',
        Authorization: `Bearer ${token}`,
      },
      body: JSON.stringify({ student_id: STUDENT_201_ID }),
    });

    /**
     * Assert 409 Conflict.
     *
     * A 201 here means the unique constraint is not being enforced — the
     * school would end up with the same student listed twice in the class,
     * breaking grade entry and the class roster.
     * A 500 here means the handler is not catching the pgconn 23505 error.
     */
    expect(
      response.status,
      `Expected 409 Conflict for duplicate enrollment, got ${String(response.status)}. ` +
        'The handler must detect pgconn error 23505 and return 409.',
    ).toBe(409);

    /**
     * Assert the error body contains the expected error code.
     */
    const body = (await response.json()) as { error: { code: string; message: string } };
    expect(body, 'Expected a JSON error body from 409 response').toBeDefined();

    expect(
      body.error.code,
      `Expected error.code="DUPLICATE_ENROLLMENT", got "${body.error.code}"`,
    ).toBe('DUPLICATE_ENROLLMENT');
  });

  // ───────────────────────────────────────────────────────────────────────────
  // TEST 91: Parent gets 403 Forbidden on POST /enroll
  //
  // SCENARIO
  // ────────
  // Ion Moldovan (role: parent) tries to call POST /classes/{classId}/enroll.
  // The API must reject this with HTTP 403 Forbidden.
  //
  // Parents can only view their child's grades — they have no administrative
  // privileges. Allowing a parent to enrol students would be a serious
  // access-control failure.
  //
  // WHAT WE ASSERT
  // ──────────────
  //   - HTTP status is 403 Forbidden.
  //   - The API must NOT return 201 or 200 (would be an access-control bug).
  // ───────────────────────────────────────────────────────────────────────────
  test('91 – parent gets 403 Forbidden on POST /enroll', async ({ parentPage }) => {
    /**
     * The parent fixture is logged in as Ion Moldovan (role: parent, no MFA).
     */
    const token = await getAccessToken(parentPage);

    /**
     * Attempt to enrol a student with the parent's Bearer token.
     * The API must inspect the `role` claim in the JWT and reject it with 403.
     */
    const response = await fetch(`${API_BASE}/classes/${CLASS_6B_ID}/enroll`, {
      method: 'POST',
      headers: {
        'Content-Type': 'application/json',
        Authorization: `Bearer ${token}`,
      },
      body: JSON.stringify({ student_id: STUDENT_ANDREI_ID }),
    });

    /**
     * Assert 403 Forbidden.
     *
     * 401 would mean the token was not recognised at all.
     * The correct response for a valid token from a non-admin/secretary role
     * is 403 Forbidden (the RequireRole middleware in main.go enforces this).
     *
     * A 201 here is a critical access-control bug — a parent should never be
     * able to enrol or unenrol students.
     */
    expect(
      response.status,
      `Expected 403 Forbidden for a parent calling POST /enroll, ` +
        `but got ${String(response.status)}. ` +
        'This is an access-control failure — only admin and secretary may enrol students.',
    ).toBe(403);
  });
});

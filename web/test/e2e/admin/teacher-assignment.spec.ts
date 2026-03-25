/**
 * admin/teacher-assignment.spec.ts
 *
 * Tests 92–95: Teacher-subject assignment management via the admin API.
 *
 * WHAT THIS FILE COVERS
 * ─────────────────────
 * Admins can assign teachers to subject-class pairs. Each assignment is scoped
 * to a (class_id, subject_id, teacher_id) triple — the same teacher may not be
 * assigned to the same subject in the same class twice. Parents have no access
 * to this endpoint.
 *
 * This file exercises one API endpoint:
 *
 *   POST /api/v1/classes/{classId}/teachers  → Assign a teacher (201)
 *
 * The GET /api/v1/classes/{classId}/teachers endpoint is also exercised to
 * verify that a new assignment appears in the list.
 *
 * Only the admin role is authorised to call POST. Parents must receive 403.
 *
 * TESTS IN THIS FILE
 * ──────────────────
 *   Test 92 – Admin can assign a teacher to a subject (201).
 *              POST /api/v1/classes/{classId}/teachers with a valid body
 *              returns 201 with the new assignment record in the data envelope.
 *
 *   Test 93 – Assignment appears in GET /classes/{classId}/teachers list.
 *              After assigning via POST, GET must include the newly-assigned
 *              teacher in the assignments array.
 *
 *   Test 94 – Duplicate assignment returns 409 Conflict.
 *              Assigning the same teacher to the same subject in the same class
 *              a second time triggers the UNIQUE constraint → 409 with error
 *              code DUPLICATE_ASSIGNMENT.
 *
 *   Test 95 – Parent gets 403 Forbidden on POST /teachers.
 *              Ion Moldovan (role: parent) must be rejected with HTTP 403.
 *
 * APPROACH: API-BASED TESTING
 * ───────────────────────────
 * There is no frontend UI for teacher assignment yet. We call the API directly
 * from the test (Node.js side) using the `fetch()` global available in Node 18+.
 *
 * Authentication uses httpOnly cookies sent automatically by page.request
 * after the auth fixture has completed login.
 *
 * FIXTURES USED
 * ─────────────
 *   adminPage  — Maria Popescu (admin role, MFA required)
 *   parentPage — Ion Moldovan (parent role, no MFA)
 *
 * SEED DATA REFERENCES
 * ─────────────────────
 * All IDs are taken from api/db/seed.sql and web/test/e2e/fixtures/auth.fixture.ts.
 *
 *   CLASS_6B_ID     → f1000000-0000-0000-0000-000000000002 (middle school class)
 *   SUBJECT_IST_ID  → f1000000-0000-0000-0000-000000000005 (Istorie — no existing teacher assigned to it in class6B at seed time)
 *   TEACHER_ION_ID  → b1000000-0000-0000-0000-000000000011 (Ion Vasilescu — teaches ROM in 6B already)
 *   TEACHER_GAB_ID  → b1000000-0000-0000-0000-000000000012 (Gabriela Marin — teaches MAT in 6B already)
 *
 * The seed inserts these class_subject_teachers rows for 6B:
 *   (6B, ROM, Ion Vasilescu)
 *   (6B, MAT, Gabriela Marin)
 *   (6B, Istorie, Ion Vasilescu)   ← already assigned at seed time → good for duplicate test
 *
 * For Test 92 (success) we assign Gabriela Marin (TEACHER_GAB_ID) to EFS
 * (SUBJECT_EFS_ID = f1000000-0000-0000-0000-000000000006), which has NO existing
 * assignment in 6B — so there is no pre-existing triple to conflict with.
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
 * Seeded in api/db/seed.sql.
 */
const CLASS_6B_ID = 'f1000000-0000-0000-0000-000000000002';

/**
 * SUBJECT_EFS_ID — "Educație fizică" (EFS) for middle school.
 * Seeded in api/db/seed.sql. NOT yet assigned a teacher in class6B at seed time,
 * which makes it a safe target for the success and roster-check tests.
 */
const SUBJECT_EFS_ID = 'f1000000-0000-0000-0000-000000000006';

/**
 * SUBJECT_IST_ID — "Istorie" (IST) for middle school.
 * Seeded in api/db/seed.sql. Already assigned to TEACHER_ION_ID in class6B
 * at seed time — used for the duplicate-assignment test.
 */
const SUBJECT_IST_ID = 'f1000000-0000-0000-0000-000000000005';

/**
 * TEACHER_ION_ID — Ion Vasilescu (teacher role).
 * Teaches ROM and Istorie in class6B at seed time.
 * Re-assigning him to Istorie in class6B will trigger the duplicate constraint.
 */
const TEACHER_ION_ID = 'b1000000-0000-0000-0000-000000000011';

/**
 * TEACHER_GAB_ID — Gabriela Marin (teacher role).
 * Teaches MAT in class6B but NOT EFS. Safe target for the success test.
 */
const TEACHER_GAB_ID = 'b1000000-0000-0000-0000-000000000012';
// ── Helper types ───────────────────────────────────────────────────────────────

/**
 * AssignmentRecord
 *
 * A single teacher-subject assignment as returned by
 * POST /api/v1/classes/{classId}/teachers.
 * TypeScript strict mode requires explicit types — no `any`.
 */
interface AssignmentRecord {
  id: string;
  class_id: string;
  subject_id: string;
  teacher_id: string;
  hours_per_week: number;
}

/**
 * AssignResponse
 *
 * JSON body returned by a successful assignment (201).
 * The API wraps the created assignment in a `data` envelope.
 */
interface AssignResponse {
  data: AssignmentRecord;
}

/**
 * TeacherAssignmentEntry
 *
 * A single entry in the GET /classes/{classId}/teachers list.
 */
interface TeacherAssignmentEntry {
  id: string;
  teacher_id: string;
  subject_id: string;
  hours_per_week: number;
}

/**
 * TeachersListResponse
 *
 * JSON body returned by GET /api/v1/classes/{classId}/teachers (200).
 * The API wraps the list in a `data` envelope.
 */
interface TeachersListResponse {
  data: TeacherAssignmentEntry[];
}

// ─────────────────────────────────────────────────────────────────────────────
// TEST SUITE: teacher assignment management
// ─────────────────────────────────────────────────────────────────────────────

test.describe('teacher assignment management', () => {
  // ───────────────────────────────────────────────────────────────────────────
  // TEST 92: Admin can assign a teacher to a subject (201)
  //
  // SCENARIO
  // ────────
  // Maria Popescu (admin) calls POST /api/v1/classes/{classId}/teachers with a
  // valid body. We assign Gabriela Marin to "Educație fizică" in class6B — a
  // combination that does NOT exist in the seed data. The API should:
  //   1. Insert a class_subject_teachers row into the database.
  //   2. Return HTTP 201 Created.
  //   3. Include in the response body: id, class_id, subject_id, teacher_id,
  //      hours_per_week.
  //
  // WHAT WE ASSERT
  // ──────────────
  //   - HTTP status is 201 Created.
  //   - response.data.id is a non-empty UUID string.
  //   - response.data.class_id matches CLASS_6B_ID.
  //   - response.data.subject_id matches SUBJECT_EFS_ID.
  //   - response.data.teacher_id matches TEACHER_GAB_ID.
  //   - response.data.hours_per_week is 2 (what we sent).
  // ───────────────────────────────────────────────────────────────────────────
  test('92 – admin can assign a teacher to a subject', async ({ adminPage }) => {
    /**
     * Step 1: Get the admin's JWT access token.
     * The auth fixture already completed login + MFA for Maria Popescu.
     */
    /**
     * Step 2: Call POST /api/v1/classes/{classId}/teachers.
     * Assign Gabriela Marin to EFS in class6B with 2 hours per week.
     */
    const response = await adminPage.request.post(`${API_BASE}/classes/${CLASS_6B_ID}/teachers`, {
      data: {
        subject_id: SUBJECT_EFS_ID,
        teacher_id: TEACHER_GAB_ID,
        hours_per_week: 2,
      },
    });

    /**
     * Step 3: Assert HTTP 201 Created.
     */
    expect(
      response.status(),
      `Expected 201 Created but got ${String(response.status())}. ` +
        'Check that the admin role is authorised for POST /teachers and ' +
        'that the (class, subject, teacher) triple does not already exist.',
    ).toBe(201);

    /**
     * Step 4: Parse and assert the response fields.
     */
    const body = (await response.json()) as AssignResponse;

    expect(body.data, 'Response body must have a "data" key').toBeDefined();
    expect(body.data.id, 'Expected "id" field — the new assignment UUID').toBeTruthy();

    expect(body.data.class_id, `Expected class_id="${CLASS_6B_ID}" in response`).toBe(CLASS_6B_ID);

    expect(body.data.subject_id, `Expected subject_id="${SUBJECT_EFS_ID}" in response`).toBe(
      SUBJECT_EFS_ID,
    );

    expect(body.data.teacher_id, `Expected teacher_id="${TEACHER_GAB_ID}" in response`).toBe(
      TEACHER_GAB_ID,
    );

    expect(
      body.data.hours_per_week,
      'Expected hours_per_week=2 in response (matching what we sent)',
    ).toBe(2);
  });

  // ───────────────────────────────────────────────────────────────────────────
  // TEST 93: Assignment appears in GET /classes/{classId}/teachers list
  //
  // SCENARIO
  // ────────
  // After assigning Gabriela Marin to EFS in class6B via POST /teachers, a
  // subsequent call to GET /api/v1/classes/{classId}/teachers must include her
  // in the assignments array.
  //
  // This verifies end-to-end persistence: the POST write must be committed and
  // the GET must return it (not just echo back an in-memory value).
  //
  // WHAT WE ASSERT
  // ──────────────
  //   - POST returns 201 or 409 (prerequisite — the assignment must exist).
  //   - GET /classes/{classId}/teachers returns 200.
  //   - response.data is an array.
  //   - The array contains an entry with teacher_id matching TEACHER_GAB_ID
  //     and subject_id matching SUBJECT_EFS_ID.
  // ───────────────────────────────────────────────────────────────────────────
  test('93 – assignment appears in GET teachers list', async ({ adminPage }) => {
    /**
     * Step 1: Ensure the assignment exists.
     * Accept both 201 (newly created) and 409 (already assigned from Test 92
     * in the same test session) — either way the assignment exists in the DB.
     */
    const assignResponse = await adminPage.request.post(
      `${API_BASE}/classes/${CLASS_6B_ID}/teachers`,
      {
        data: {
          subject_id: SUBJECT_EFS_ID,
          teacher_id: TEACHER_GAB_ID,
          hours_per_week: 2,
        },
      },
    );

    expect(
      [201, 409].includes(assignResponse.status()),
      `Prerequisite: expected 201 or 409 from POST /teachers, got ${String(assignResponse.status())}.`,
    ).toBe(true);

    /**
     * Step 2: Fetch the teacher assignments for class6B.
     * GET /api/v1/classes/{classId}/teachers returns all teacher-subject pairs.
     */
    const listResponse = await adminPage.request.get(`${API_BASE}/classes/${CLASS_6B_ID}/teachers`);

    expect(
      listResponse.status(),
      `Expected 200 OK from GET /classes/${CLASS_6B_ID}/teachers, got ${String(listResponse.status())}.`,
    ).toBe(200);

    const listBody = (await listResponse.json()) as TeachersListResponse;

    expect(
      Array.isArray(listBody.data),
      'Expected response.data to be an array of teacher assignments',
    ).toBe(true);

    /**
     * Step 3: Verify Gabriela Marin appears for EFS in the list.
     */
    const found = listBody.data.some(
      (entry) => entry.teacher_id === TEACHER_GAB_ID && entry.subject_id === SUBJECT_EFS_ID,
    );

    expect(
      found,
      `Expected teacher "${TEACHER_GAB_ID}" assigned to subject "${SUBJECT_EFS_ID}" ` +
        `to appear in class6B teachers list. ` +
        `Found ${String(listBody.data.length)} entries: ` +
        listBody.data.map((e) => `(t:${e.teacher_id},s:${e.subject_id})`).join(', '),
    ).toBe(true);
  });

  // ───────────────────────────────────────────────────────────────────────────
  // TEST 94: Duplicate assignment returns 409 Conflict
  //
  // SCENARIO
  // ────────
  // Ion Vasilescu (TEACHER_ION_ID) is already assigned to "Istorie" (SUBJECT_IST_ID)
  // in class6B via the seed data. Attempting to assign the same triple again must
  // trigger the UNIQUE(class_id, subject_id, teacher_id) constraint and return
  // HTTP 409 Conflict with error code DUPLICATE_ASSIGNMENT.
  //
  // WHAT WE ASSERT
  // ──────────────
  //   - HTTP status is 409 Conflict (NOT 201, 200, or 500).
  //   - Response body contains { "error": { "code": "DUPLICATE_ASSIGNMENT", ... } }.
  // ───────────────────────────────────────────────────────────────────────────
  test('94 – duplicate assignment returns 409 Conflict', async ({ adminPage }) => {
    /**
     * Try to assign Ion Vasilescu to Istorie in class6B.
     * This combination is already present via seed data, so the unique constraint
     * fires immediately — no additional setup is needed.
     */
    const response = await adminPage.request.post(`${API_BASE}/classes/${CLASS_6B_ID}/teachers`, {
      data: {
        subject_id: SUBJECT_IST_ID,
        teacher_id: TEACHER_ION_ID,
      },
    });

    /**
     * Assert 409 Conflict.
     *
     * A 201 here means the unique constraint is not being enforced — the school
     * would end up with the same teacher listed twice for the same subject in the
     * same class, breaking grade entry and reporting.
     * A 500 here means the handler is not catching pgconn error code 23505.
     */
    expect(
      response.status(),
      `Expected 409 Conflict for duplicate assignment, got ${String(response.status())}. ` +
        'The handler must detect pgconn error 23505 and return 409.',
    ).toBe(409);

    /**
     * Assert the error body contains the expected error code.
     */
    const body = (await response.json()) as { error: { code: string; message: string } };
    expect(body, 'Expected a JSON error body from 409 response').toBeDefined();

    expect(
      body.error.code,
      `Expected error.code="DUPLICATE_ASSIGNMENT", got "${body.error.code}"`,
    ).toBe('DUPLICATE_ASSIGNMENT');
  });

  // ───────────────────────────────────────────────────────────────────────────
  // TEST 95: Parent gets 403 Forbidden on POST /teachers
  //
  // SCENARIO
  // ────────
  // Ion Moldovan (role: parent) tries to call POST /classes/{classId}/teachers.
  // The API must reject this with HTTP 403 Forbidden.
  //
  // Parents can only view their child's grades — they have no administrative
  // privileges. Allowing a parent to assign teachers would be a serious
  // access-control failure.
  //
  // WHAT WE ASSERT
  // ──────────────
  //   - HTTP status is 403 Forbidden.
  //   - The API must NOT return 201 or 200 (would be an access-control bug).
  // ───────────────────────────────────────────────────────────────────────────
  test('95 – parent gets 403 Forbidden on POST /teachers', async ({ parentPage }) => {
    /**
     * The parent fixture is logged in as Ion Moldovan (role: parent, no MFA).
     */
    /**
     * Attempt to assign a teacher with the parent's Bearer token.
     * The API must inspect the `role` claim in the JWT and reject it with 403.
     */
    const response = await parentPage.request.post(`${API_BASE}/classes/${CLASS_6B_ID}/teachers`, {
      data: {
        subject_id: SUBJECT_EFS_ID,
        teacher_id: TEACHER_GAB_ID,
      },
    });

    /**
     * Assert 403 Forbidden.
     *
     * 401 would mean the token was not recognised at all.
     * The correct response for a valid token from a non-admin role is 403
     * Forbidden (the RequireRole("admin") middleware in main.go enforces this).
     *
     * A 201 here is a critical access-control bug — a parent should never be
     * able to assign teachers to subjects in a class.
     */
    expect(
      response.status(),
      `Expected 403 Forbidden for a parent calling POST /teachers, ` +
        `but got ${String(response.status())}. ` +
        'This is an access-control failure — only admin may assign teachers.',
    ).toBe(403);
  });
});

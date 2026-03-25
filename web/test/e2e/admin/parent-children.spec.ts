/**
 * admin/parent-children.spec.ts
 *
 * Tests 96–98: Parent children view via GET /api/v1/users/me/children.
 *
 * WHAT THIS FILE COVERS
 * ─────────────────────
 * Parents need to see their linked children together with class enrollment
 * information. The GET /users/me/children endpoint returns a list of student
 * records (with class_id, class_name, class_education_level) for all students
 * linked to the currently authenticated user via parent_student_links.
 *
 * This file exercises one API endpoint:
 *
 *   GET /api/v1/users/me/children  → List children for current user (200)
 *
 * The endpoint is accessible to ALL authenticated users (no role restriction).
 * A teacher calling it will receive an empty list; a parent with linked children
 * will receive those children with their current class enrollment data.
 *
 * TESTS IN THIS FILE
 * ──────────────────
 *   Test 96 – Parent can list their children via API.
 *              GET /api/v1/users/me/children as Ion Moldovan (parent) must
 *              return HTTP 200 with a non-empty data array.
 *
 *   Test 97 – Children response includes correct class info.
 *              The response must include class_name, class_education_level,
 *              and class_id for children who are enrolled in a class.
 *
 *   Test 98 – Response includes correct child name (Andrei Moldovan).
 *              The seed data links Ion Moldovan (parent) to Andrei Moldovan
 *              (student, enrolled in class 2A — primary). The response must
 *              contain a child with first_name="Andrei" and last_name="Moldovan".
 *
 * APPROACH: API-BASED TESTING
 * ───────────────────────────
 * There is no frontend UI for the parent children view yet. We call the API
 * directly from the test (Node.js side) using the `fetch()` global available
 * in Node 18+.
 *
 * Authentication uses httpOnly cookies sent automatically by page.request
 * after the auth fixture has completed login.
 *
 * FIXTURES USED
 * ─────────────
 *   parentPage — Ion Moldovan (parent role, no MFA)
 *
 * SEED DATA REFERENCES
 * ─────────────────────
 * All data comes from api/db/seed.sql.
 *
 *   Ion Moldovan (parent)
 *     id:    b1000000-0000-0000-0000-000000000301
 *     email: ion.moldovan@gmail.com
 *     Linked to: Andrei Moldovan (student)
 *
 *   Andrei Moldovan (student)
 *     id:    b1000000-0000-0000-0000-000000000101
 *     Enrolled in: class 2A (primary, grade 2)
 *     class id: f1000000-0000-0000-0000-000000000001
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
 * STUDENT_ANDREI_ID — Andrei Moldovan, the child linked to Ion Moldovan.
 * Seeded in api/db/seed.sql. Enrolled in class 2A (primary).
 */
const STUDENT_ANDREI_ID = 'b1000000-0000-0000-0000-000000000101';

/**
 * CLASS_2A_ID — the seeded "2A" primary class (grade 2, education_level=primary).
 * Seeded in api/db/seed.sql. Andrei Moldovan is enrolled in this class.
 */
const CLASS_2A_ID = 'f1000000-0000-0000-0000-000000000001';
// ── Helper types ───────────────────────────────────────────────────────────────

/**
 * ChildRecord
 *
 * A single child entry as returned by GET /api/v1/users/me/children.
 * TypeScript strict mode requires explicit types — no `any`.
 *
 * Class fields are optional because a child who is not enrolled in any class
 * will have them absent from the JSON (omitempty in Go).
 */
interface ChildRecord {
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
 * ChildrenListResponse
 *
 * JSON body returned by GET /api/v1/users/me/children (200).
 * The API wraps the list in a `data` envelope.
 */
interface ChildrenListResponse {
  data: ChildRecord[];
}

// ─────────────────────────────────────────────────────────────────────────────
// TEST SUITE: parent children view
// ─────────────────────────────────────────────────────────────────────────────

test.describe('parent children view', () => {
  // ───────────────────────────────────────────────────────────────────────────
  // TEST 96: Parent can list their children via API (200)
  //
  // SCENARIO
  // ────────
  // Ion Moldovan (parent, no MFA) calls GET /api/v1/users/me/children.
  // The seed data links him to Andrei Moldovan (student). The endpoint must:
  //   1. Return HTTP 200 OK.
  //   2. Return a non-empty array in the data envelope.
  //   3. Include the child's id, first_name, last_name, and role fields.
  //
  // WHAT WE ASSERT
  // ──────────────
  //   - HTTP status is 200 OK.
  //   - response.data is a non-empty array (at least 1 child).
  //   - response.data[0].id is a non-empty string.
  //   - response.data[0].role is "student".
  // ───────────────────────────────────────────────────────────────────────────
  test('96 – parent can list their children', async ({ parentPage }) => {
    /**
     * Step 1: Get the parent's JWT access token.
     * The auth fixture already completed login for Ion Moldovan (no MFA required).
     */
    /**
     * Step 2: Call GET /api/v1/users/me/children with the parent's token.
     * The handler reads the user ID from the JWT and queries parent_student_links.
     */
    const response = await parentPage.request.get(`${API_BASE}/users/me/children`);

    /**
     * Step 3: Assert HTTP 200 OK.
     *
     * A 501 Not Implemented means the route was not wired in main.go.
     * A 401 means the auth fixture did not produce a valid token.
     * A 500 means the handler encountered a DB or context error.
     */
    expect(
      response.status(),
      `Expected 200 OK from GET /users/me/children, got ${String(response.status())}. ` +
        'Check that the route is wired and the handler is implemented.',
    ).toBe(200);

    /**
     * Step 4: Parse the response and verify the data envelope.
     */
    const body = (await response.json()) as ChildrenListResponse;

    expect(body.data, 'Response body must have a "data" key').toBeDefined();

    expect(Array.isArray(body.data), 'Expected response.data to be an array of child records').toBe(
      true,
    );

    expect(
      body.data.length,
      'Expected at least 1 child in the response (Ion Moldovan is linked to Andrei Moldovan)',
    ).toBeGreaterThan(0);

    /**
     * Step 5: Verify the first child has the required identity fields.
     */
    const firstChild = body.data[0];

    expect(firstChild.id, 'Expected child.id to be a non-empty UUID string').toBeTruthy();

    expect(firstChild.role, `Expected child.role="student", got "${firstChild.role}"`).toBe(
      'student',
    );

    expect(firstChild.first_name, 'Expected child.first_name to be non-empty').toBeTruthy();

    expect(firstChild.last_name, 'Expected child.last_name to be non-empty').toBeTruthy();
  });

  // ───────────────────────────────────────────────────────────────────────────
  // TEST 97: Children response includes class info
  //
  // SCENARIO
  // ────────
  // Ion Moldovan calls GET /api/v1/users/me/children. Andrei Moldovan (his child)
  // is enrolled in class 2A (primary, grade 2) as of the seed data. The response
  // must include class_id, class_name, and class_education_level for this child.
  //
  // This test specifically validates the enhanced ListChildrenForParent query that
  // LEFT JOINs class_enrollments and classes. The original query only returned
  // user rows with no class context.
  //
  // WHAT WE ASSERT
  // ──────────────
  //   - HTTP status is 200 OK.
  //   - At least one child in the response has class_id matching CLASS_2A_ID.
  //   - The class_name for that child is "2A".
  //   - The class_education_level for that child is "primary".
  // ───────────────────────────────────────────────────────────────────────────
  test('97 – children response includes class info', async ({ parentPage }) => {
    /**
     * Call the endpoint as Ion Moldovan.
     */
    const response = await parentPage.request.get(`${API_BASE}/users/me/children`);

    expect(
      response.status(),
      `Expected 200 OK from GET /users/me/children, got ${String(response.status())}.`,
    ).toBe(200);

    const body = (await response.json()) as ChildrenListResponse;

    expect(body.data, 'Response body must have a "data" key').toBeDefined();

    /**
     * Find Andrei Moldovan's entry by student ID.
     * The seed data guarantees exactly one link for Ion Moldovan → Andrei Moldovan,
     * but we search by ID to be resilient to ordering changes.
     */
    const andrei = body.data.find((child) => child.id === STUDENT_ANDREI_ID);

    expect(
      andrei,
      `Expected to find child with id="${STUDENT_ANDREI_ID}" (Andrei Moldovan) in response. ` +
        `Got ${String(body.data.length)} children: ` +
        body.data.map((c) => `(id:${c.id})`).join(', '),
    ).toBeDefined();

    if (andrei === undefined) {
      // Type guard: andrei is guaranteed defined after the above assertion,
      // but TypeScript doesn't narrow through the toBeDefined() call.
      return;
    }

    /**
     * Assert the class fields for Andrei Moldovan.
     * class_id must match CLASS_2A_ID (f1000000-0000-0000-0000-000000000001).
     */
    expect(
      andrei.class_id,
      `Expected class_id="${CLASS_2A_ID}" for Andrei Moldovan, got "${String(andrei.class_id)}". ` +
        'Check that the ListChildrenForParent query LEFT JOINs class_enrollments and classes.',
    ).toBe(CLASS_2A_ID);

    /**
     * class_name must be "2A" — the seeded primary class.
     */
    expect(
      andrei.class_name,
      `Expected class_name="2A" for Andrei Moldovan, got "${String(andrei.class_name)}".`,
    ).toBe('2A');

    /**
     * class_education_level must be "primary" — class 2A is a primary school class.
     */
    expect(
      andrei.class_education_level,
      `Expected class_education_level="primary" for Andrei Moldovan, ` +
        `got "${String(andrei.class_education_level)}".`,
    ).toBe('primary');
  });

  // ───────────────────────────────────────────────────────────────────────────
  // TEST 98: Response includes the correct child name (Andrei Moldovan)
  //
  // SCENARIO
  // ────────
  // Ion Moldovan (parent, seed id: b1000000-0000-0000-0000-000000000301) is linked
  // to Andrei Moldovan (student, seed id: b1000000-0000-0000-0000-000000000101)
  // via parent_student_links as defined in api/db/seed.sql.
  //
  // When Ion calls GET /users/me/children, the response must include exactly the
  // child named "Andrei Moldovan" — confirming that the parent_id filter in the
  // query is working correctly and not returning all students in the school.
  //
  // WHAT WE ASSERT
  // ──────────────
  //   - HTTP status is 200 OK.
  //   - A child with id=STUDENT_ANDREI_ID is in the response.
  //   - That child has first_name="Andrei".
  //   - That child has last_name="Moldovan".
  // ───────────────────────────────────────────────────────────────────────────
  test('98 – response includes correct child name (Andrei Moldovan)', async ({ parentPage }) => {
    /**
     * Step 1: Call GET /api/v1/users/me/children as Ion Moldovan.
     */
    const response = await parentPage.request.get(`${API_BASE}/users/me/children`);

    expect(response.status(), `Expected 200 OK, got ${String(response.status())}.`).toBe(200);

    const body = (await response.json()) as ChildrenListResponse;

    /**
     * Step 2: Find Andrei Moldovan in the list by his seeded UUID.
     * This ensures we are not accidentally matching a different student
     * who happens to have the same name in a future seed data change.
     */
    const andrei = body.data.find((child) => child.id === STUDENT_ANDREI_ID);

    expect(
      andrei,
      `Expected to find Andrei Moldovan (id=${STUDENT_ANDREI_ID}) in the children list. ` +
        `The seed data links Ion Moldovan (parent) to Andrei Moldovan (student) via parent_student_links. ` +
        `Got ${String(body.data.length)} children: ` +
        body.data.map((c) => `${c.first_name} ${c.last_name} (${c.id})`).join(', '),
    ).toBeDefined();

    if (andrei === undefined) {
      return;
    }

    /**
     * Step 3: Assert the child's name fields match the seed data exactly.
     */
    expect(andrei.first_name, `Expected first_name="Andrei", got "${andrei.first_name}".`).toBe(
      'Andrei',
    );

    expect(andrei.last_name, `Expected last_name="Moldovan", got "${andrei.last_name}".`).toBe(
      'Moldovan',
    );

    /**
     * Step 4: Confirm that only Ion Moldovan's children are returned.
     * The seed data has 2 parent accounts; each parent has their own child.
     * Ion Moldovan has exactly 1 child (Andrei). We verify the total count
     * is 1 — any more would mean the parent_id filter is not working.
     */
    expect(
      body.data.length,
      `Expected exactly 1 child for Ion Moldovan (only Andrei is linked), ` +
        `but got ${String(body.data.length)}. ` +
        `This could indicate the parent_id WHERE clause is missing from the query.`,
    ).toBe(1);
  });
});

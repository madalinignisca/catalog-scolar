/**
 * auth/access-control.spec.ts
 *
 * End-to-end tests for role-based access control (RBAC) in CatalogRO.
 *
 * WHAT THIS FILE COVERS
 * ─────────────────────
 * CatalogRO has five user roles: admin, secretary, teacher, parent, student.
 * Each role can access a different subset of routes and data. These tests
 * verify that the application correctly DENIES access when a lower-privileged
 * role tries to reach a resource they should not see.
 *
 * Access control is enforced at two layers:
 *
 *   1. API layer  — The Go backend applies Row-Level Security (RLS) via
 *      PostgreSQL policies. Every query is automatically scoped to the
 *      authenticated user's school_id and role. A teacher requesting another
 *      teacher's class data simply gets an empty result set (not a 403).
 *
 *   2. Frontend layer — Nuxt middleware and page guards hide UI elements and
 *      redirect unauthorised navigations. For example, navigating to
 *      /admin/users as a student should show a 404 or redirect to '/'.
 *
 * TESTS IN THIS FILE
 * ──────────────────
 *   Test 18 — Parent navigates to a class catalog.
 *              Parents have no class assignments, so the grade grid must be
 *              empty or an error/redirect must occur. No student grades should
 *              be readable by a parent browsing to an arbitrary class URL.
 *
 *   Test 19 — Student navigates to an admin-only route (/admin/users).
 *              Students have no admin privileges. The app must deny access
 *              (404 page, redirect to dashboard, or generic error).
 *
 *   Test 20 — Teacher navigates to a class they do not teach.
 *              Ana Dumitrescu teaches class 2A (primary). She must NOT see
 *              student grades for class 6B (middle school). RLS will return
 *              an empty grade grid or a permission error.
 *
 * SEED DATA REFERENCES
 * ─────────────────────
 * Class IDs match api/db/seed.sql:
 *   f1000000-0000-0000-0000-000000000001 → class 2A  (primary,  Ana's class)
 *   f1000000-0000-0000-0000-000000000002 → class 6B  (middle,   Ion Vasilescu's class)
 *
 * FIXTURES USED
 * ─────────────
 * All three tests use role-specific fixtures from auth.fixture.ts:
 *   parentPage  — Ion Moldovan (parent, no class assignments)
 *   studentPage — Andrei Moldovan (student, class 2A)
 *   teacherPage — Ana Dumitrescu (teacher, teaches ONLY class 2A)
 */

// ── Internal: Auth fixture (re-exports test, expect, and role fixtures) ───────
// We import from auth.fixture so every test starts with a pre-authenticated
// browser session for the relevant role. No manual login steps needed.
// CatalogPage is imported in the same group — both are project-local modules.
import { test, expect } from '../fixtures/auth.fixture';
import { CatalogPage } from '../page-objects/catalog.page';

// ─────────────────────────────────────────────────────────────────────────────
// TEST SUITE: role-based access control
// ─────────────────────────────────────────────────────────────────────────────
test.describe('access control', () => {
  // ───────────────────────────────────────────────────────────────────────────
  // TEST 18: Parent cannot read student grades by navigating to a class catalog
  //
  // SCENARIO
  // ────────
  // A parent user (Ion Moldovan) is logged in. Parents can only see their own
  // child's grades through the dedicated parent view — they must NOT be able
  // to browse to /catalog/[classId] and read the full class grade grid.
  //
  // Ion Moldovan is the parent of Andrei Moldovan (class 2A). Even though his
  // child is in class 2A, navigating to the catalog URL directly should not
  // give him access to the teacher-grade-grid view.
  //
  // EXPECTED BEHAVIOUR
  // ──────────────────
  // One of the following must occur (acceptable outcomes):
  //   (a) The API returns a 403/404 and the catalog-error banner is shown.
  //   (b) The grade grid is empty (0 student rows — RLS filtered everything).
  //   (c) The Nuxt route guard redirects the parent away from /catalog/* entirely.
  //
  // We check all three possible outcomes. As long as one of them is true,
  // the access control is working correctly. The exact behaviour depends on
  // whether access control is enforced in the middleware or in the API layer.
  //
  // WHY THIS MATTERS
  // ────────────────
  // A parent with the URL /catalog/[classId] must never see other students'
  // grades — that would be a serious GDPR violation (personal data of minors).
  // ───────────────────────────────────────────────────────────────────────────
  test('parent cannot access class catalog grade grid (test 18)', async ({ parentPage }) => {
    // `parentPage` is logged in as Ion Moldovan (role: parent).
    // Parents are NOT assigned as teachers or class managers for any class.
    const catalogPage = new CatalogPage(parentPage);

    // Navigate directly to class 2A's catalog URL using page.goto() rather than
    // CatalogPage.goto(). We MUST use page.goto() here because:
    //   - CatalogPage.goto() works by clicking a class card on the dashboard.
    //   - The parent's dashboard has NO class cards (parents have no assignments).
    //   - Clicking a non-existent card would throw "No class card found".
    // Using page.goto() simulates a parent who typed or bookmarked the URL
    // directly — the exact scenario this access-control test covers.
    await parentPage.goto('/catalog/f1000000-0000-0000-0000-000000000001');

    // Wait for the page to finish loading (loading spinner gone or content appears).
    // We give 10 seconds to account for API latency and any SSR redirect.
    await parentPage
      .getByTestId('catalog-loading')
      .waitFor({ state: 'hidden', timeout: 10_000 })
      .catch(() => {
        // If the loading indicator never appeared, that is fine — the page may
        // have redirected immediately via middleware before rendering the loader.
      });

    // ── Outcome A: Redirect away from the catalog ──────────────────────────
    // The route guard may redirect the parent back to '/' or to an error page.
    // If we are no longer on /catalog/*, the access control worked.
    const finalUrl = parentPage.url();
    const wasRedirected = !finalUrl.includes('/catalog/');

    // ── Outcome B: API error banner is shown ──────────────────────────────
    // The frontend rendered the /catalog page but the API returned an error
    // (403 or 404) because the parent has no teacher permissions.
    const hasErrorBanner = await catalogPage.errorBanner.isVisible();

    // ── Outcome C: Grade grid has zero student rows ────────────────────────
    // The page rendered but RLS filtered out all students and grades.
    // The grade grid should have no student-row elements.
    const studentRowCount = await catalogPage.studentRows.count();
    const hasNoStudentData = studentRowCount === 0;

    // At least one of the three acceptable outcomes must hold.
    // Using a soft OR assertion so the test message is descriptive on failure.
    // String() is used around booleans and numbers to satisfy TypeScript strict
    // template literal rules (@typescript-eslint/restrict-template-expressions).
    expect(
      wasRedirected || hasErrorBanner || hasNoStudentData,
      [
        'Expected parent to be denied access to the class catalog. Acceptable outcomes:',
        `  (a) Redirected away from /catalog/* — was: ${String(wasRedirected)} (url: ${finalUrl})`,
        `  (b) catalog-error banner visible — was: ${String(hasErrorBanner)}`,
        `  (c) grade grid has 0 student rows — was: ${String(hasNoStudentData)} (count: ${String(studentRowCount)})`,
      ].join('\n'),
    ).toBe(true);
  });

  // ───────────────────────────────────────────────────────────────────────────
  // TEST 19: Student cannot access admin-only routes
  //
  // SCENARIO
  // ────────
  // A student user (Andrei Moldovan, class 2A) is logged in. The /admin/users
  // route is only accessible to admin and secretary roles. A student navigating
  // to it must be denied access — either by a route guard redirect or by a
  // 404/403 error page.
  //
  // EXPECTED BEHAVIOUR
  // ──────────────────
  // One of the following must occur:
  //   (a) Nuxt middleware redirects the student away (e.g. to '/' or '/login').
  //   (b) The app renders a 404 or generic error page (URL stays at /admin/users
  //       but content shows "pagina nu a fost găsită" or similar).
  //
  // WHAT WE DO NOT EXPECT
  // ──────────────────────
  // We must NOT see any admin UI: user list, management controls, or sensitive
  // account data. This is both a security and a privacy requirement.
  //
  // WHY THIS MATTERS
  // ────────────────
  // If a student can access /admin/users, they could see the personal data
  // (name, email, role) of every staff member at their school.
  // ───────────────────────────────────────────────────────────────────────────
  test('student cannot access admin user management route (test 19)', async ({ studentPage }) => {
    // `studentPage` is logged in as Andrei Moldovan (role: student).

    // Navigate to the admin user-management page.
    // A student has no admin privileges and should be denied access.
    await studentPage.goto('/admin/users');

    // Allow up to 10 seconds for any middleware redirect to complete.
    // We use a catch-all waitForURL pattern that accepts any URL — we just
    // want to ensure the navigation resolves before we make assertions.
    await studentPage.waitForLoadState('networkidle', { timeout: 10_000 }).catch(() => {
      // networkidle may time out in some environments; ignore and proceed.
    });

    const finalUrl = studentPage.url();

    // ── Outcome A: Redirected away from /admin/* ──────────────────────────
    // The middleware detected the student role and redirected to a safe page.
    const wasRedirectedFromAdmin = !finalUrl.includes('/admin/');

    // ── Outcome B: Error or 404 page is shown ─────────────────────────────
    // The page rendered an error (e.g. the Nuxt error.vue or a custom 404
    // component). We check for common error testids used in the project.
    // Nuxt renders its default error page for non-existent routes. We check
    // for common indicators: testid-based error pages, status code text, or
    // the page body containing "404" or "not found".
    const pageText = (await studentPage.textContent('body')) ?? '';
    const hasPageError =
      (await studentPage.getByTestId('error-page').isVisible().catch(() => false)) ||
      (await studentPage.getByTestId('not-found-page').isVisible().catch(() => false)) ||
      pageText.includes('404') ||
      /not found/i.test(pageText);

    // At least one of the acceptable outcomes must hold.
    expect(
      wasRedirectedFromAdmin || hasPageError,
      [
        'Expected student to be denied access to admin routes. Acceptable outcomes:',
        `  (a) Redirected away from /admin/* — was: ${String(wasRedirectedFromAdmin)} (url: ${finalUrl})`,
        `  (b) Error or 404 page visible — was: ${String(hasPageError)}`,
      ].join('\n'),
    ).toBe(true);
  });

  // ───────────────────────────────────────────────────────────────────────────
  // TEST 20: Teacher cannot see grade data for a class they do not teach
  //
  // SCENARIO
  // ────────
  // Ana Dumitrescu is a primary school teacher who teaches CLR and MEM in
  // class 2A only. She has NO assignment for class 6B (middle school, Ion
  // Vasilescu's class). Navigating to /catalog/[class6B-id] should return
  // no student or grade data because PostgreSQL RLS filters the query results
  // to only rows where the teacher is assigned to that class.
  //
  // This test verifies that the RLS policy on the `grades` and `students`
  // tables correctly restricts Ana's view to her own class.
  //
  // EXPECTED BEHAVIOUR
  // ──────────────────
  // One of the following must occur:
  //   (a) The catalog renders but the grade grid has 0 student rows (RLS
  //       filtered all results because Ana has no access to class 6B).
  //   (b) The catalog-error banner is shown (API returned 403/404).
  //   (c) The empty-state message is shown ("no grades yet" or similar).
  //   (d) The Nuxt route guard redirected Ana away from the page.
  //
  // NOTE: The catalog page WILL render (Ana is a valid teacher-role user).
  // The restriction is at the DATA level — the grade grid should simply show
  // nothing. This is different from test 19 where the whole route is blocked.
  //
  // WHY THIS MATTERS
  // ────────────────
  // Without RLS, a teacher could URL-hack to any class ID and read other
  // teachers' students' grades — a serious privacy and pedagogical concern.
  // ───────────────────────────────────────────────────────────────────────────
  test('teacher cannot see grades for unassigned class (test 20)', async ({ teacherPage }) => {
    // `teacherPage` is logged in as Ana Dumitrescu (role: teacher, class 2A).
    const catalogPage = new CatalogPage(teacherPage);

    // Navigate directly to class 6B's catalog URL using page.goto() rather than
    // CatalogPage.goto(). We MUST use page.goto() here because:
    //   - CatalogPage.goto() works by clicking a class card on the dashboard.
    //   - Ana only has ONE card (class 2A). Clicking it navigates to 2A, NOT 6B.
    //   - waitForURL('**/catalog/f1000000-...-000000000002') would then time out.
    // Using page.goto() simulates Ana manually typing the URL for a class she
    // is not assigned to — the exact scenario this RLS test covers.
    await teacherPage.goto('/catalog/f1000000-0000-0000-0000-000000000002');

    // Wait for the page to finish loading.
    await teacherPage
      .getByTestId('catalog-loading')
      .waitFor({ state: 'hidden', timeout: 10_000 })
      .catch(() => {
        // Loading indicator may never appear if the page redirected immediately.
      });

    const finalUrl = teacherPage.url();

    // ── Outcome A: Grade grid has 0 student rows ──────────────────────────
    // The page rendered the catalog shell but RLS returned no student data
    // because Ana has no assignments for class 6B.
    const studentRowCount = await catalogPage.studentRows.count();
    const hasNoStudentRows = studentRowCount === 0;

    // ── Outcome B: API error banner is visible ────────────────────────────
    // The backend returned an explicit error (403 or 404) for this class.
    const hasErrorBanner = await catalogPage.errorBanner.isVisible();

    // ── Outcome C: Empty state message is visible ─────────────────────────
    // The grade-grid-empty element is rendered when the API returns an empty
    // list (no grades, or no subjects assigned to this teacher for this class).
    const emptyStateText = await catalogPage.getEmptyState();
    const hasEmptyState = emptyStateText !== null;

    // ── Outcome D: Redirected away from the catalog ───────────────────────
    // The route guard detected the lack of assignment and redirected Ana.
    const wasRedirected = !finalUrl.includes('/catalog/f1000000-0000-0000-0000-000000000002');

    // At least one acceptable outcome must hold.
    // String() wraps booleans, numbers, and nullable strings to satisfy
    // @typescript-eslint/restrict-template-expressions in strict mode.
    expect(
      hasNoStudentRows || hasErrorBanner || hasEmptyState || wasRedirected,
      [
        'Expected teacher Ana to see no data for unassigned class 6B. Acceptable outcomes:',
        `  (a) 0 student rows in grade grid — was: ${String(hasNoStudentRows)} (count: ${String(studentRowCount)})`,
        `  (b) catalog-error banner visible — was: ${String(hasErrorBanner)}`,
        `  (c) empty-state element visible — was: ${String(hasEmptyState)} (text: "${emptyStateText ?? ''}")`,
        `  (d) Redirected away from class 6B catalog — was: ${String(wasRedirected)} (url: ${finalUrl})`,
      ].join('\n'),
    ).toBe(true);
  });
});

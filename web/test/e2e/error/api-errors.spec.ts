/**
 * error/api-errors.spec.ts
 *
 * Tests 66–68: API error handling — server errors, authorization failures,
 * and network timeouts.
 *
 * WHAT WE TEST
 * ────────────
 * A production app must handle API failures gracefully. Teachers and parents
 * should see clear error messages rather than broken UIs, infinite spinners,
 * or silent data loss. These tests inject failures at the network layer using
 * Playwright's `page.route()` API to intercept and override real API calls.
 *
 * TEST OVERVIEW
 * ─────────────
 *   66 – API 500 on grade fetch: the catalog page shows a grade-grid-error
 *        element rather than an empty or broken grid.
 *   67 – API 403 on grade creation: after the modal save fails with HTTP 403
 *        (Forbidden), an error message appears inside or near the modal.
 *   68 – Network timeout on login: if the /auth/login request is aborted,
 *        the login page shows a visible error state (not an infinite spinner).
 *
 * HOW PLAYWRIGHT ROUTE INTERCEPTION WORKS
 * ────────────────────────────────────────
 * `page.route(urlPattern, handler)` intercepts outgoing fetch/XHR requests
 * whose URL matches the pattern. Inside the handler:
 *   - `route.fulfill({ status, body })` — responds with a fake HTTP response.
 *   - `route.abort(reason)` — simulates a network failure (no response).
 *   - `route.continue()` — passes the request through to the real server.
 *
 * URL patterns support glob wildcards (*) and exact string matching.
 * We use the "**\/path\/segment" form to match any origin and path combination.
 *
 * TEST 68 NOTE
 * ────────────
 * Test 68 uses the raw `test` from @playwright/test (no auth fixture) because
 * it tests the login page itself — there is no logged-in session to start from.
 */

// ── External: Standard Playwright test runner ─────────────────────────────────
// Test 68 needs a plain unauthenticated page, so we import the raw `test` and
// `expect` from @playwright/test. `test` is used directly for test 68 only.
import { test, expect } from '@playwright/test';

// ── Internal: Auth fixture ────────────────────────────────────────────────────
// Tests 66 and 67 use `teacherPage` (already logged in as Ana Dumitrescu).
// We alias the fixture test to `authTest` to avoid a name collision with the
// plain `test` import above. Pattern mirrors auth/token.spec.ts.
import { test as authTest, TEST_CLASSES } from '../fixtures/auth.fixture';
import { CatalogPage } from '../page-objects/catalog.page';
import { GradeInputModal } from '../page-objects/grade-input.page';

// ── Helpers ───────────────────────────────────────────────────────────────────

/**
 * todayISO
 *
 * Returns today's date as an ISO 8601 string (YYYY-MM-DD).
 */
function todayISO(): string {
  return new Date().toISOString().split('T')[0];
}

// ── Test 66 ───────────────────────────────────────────────────────────────────

authTest(
  '66 – API 500 on grade fetch shows grade-grid-error element',
  async ({ teacherPage }) => {
    /**
     * SCENARIO
     * ────────
     * The server returns HTTP 500 (Internal Server Error) when the catalog
     * page tries to load grades. The user should see a clearly visible error
     * element rather than a blank grid or an infinite loading spinner.
     *
     * IMPLEMENTATION
     * ──────────────
     * We register a route intercept BEFORE navigating to the catalog page.
     * When the page loads and sends its grades fetch request, our handler
     * returns a 500 instead of forwarding the request to the real server.
     *
     * The error element [data-testid="grade-grid-error"] must become visible.
     * This element should contain a human-readable error message in Romanian
     * that explains the failure to the user.
     */
    // ── Register the interceptor ──────────────────────────────────────────────
    // Intercept any request whose URL matches the grades endpoint pattern.
    // The double-star prefix (**) matches any origin and optional subdirectory.
    await teacherPage.route(
      '**/catalog/classes/*/subjects/*/grades*',
      (route) =>
        route.fulfill({
          status: 500,
          contentType: 'text/plain',
          body: 'Internal Server Error',
        }),
    );

    // ── Navigate to the catalog ───────────────────────────────────────────────
    // The catalog page will try to fetch grades and receive our fake 500.
    const catalogPage = new CatalogPage(teacherPage);
    await catalogPage.goto(TEST_CLASSES.class2A.id);

    // ── Wait for subject tabs to load (page shell rendered) ───────────────────
    // The class header and tabs load from a separate endpoint that is NOT
    // intercepted. We wait for them to confirm the page reached a stable state.
    await expect(catalogPage.subjectTabs.first()).toBeVisible({ timeout: 15_000 });
    await catalogPage.clickSubjectTab('Comunicare');

    // ── Assert error element is visible ───────────────────────────────────────
    // The grade grid error element should appear in response to the 500.
    // We allow up to 8 seconds for the error state to render after the
    // failed fetch resolves.
    const gradeGridError = teacherPage.getByTestId('grade-grid-error');
    await expect(gradeGridError).toBeVisible({ timeout: 8_000 });

    // ── Assert the loading indicator is gone ──────────────────────────────────
    // The catalog must not be stuck in a loading state — the error must
    // resolve the loading spinner.
    await expect(catalogPage.loadingIndicator).not.toBeVisible();
  },
);

// ── Test 67 ───────────────────────────────────────────────────────────────────

authTest(
  '67 – API 403 on grade creation shows an error message',
  async ({ teacherPage }) => {
    // This test involves route interception + catalog navigation + modal
    // interaction, all of which add latency on a slow CI box. 60 s gives
    // the full sequence enough headroom without masking real regressions.
    test.setTimeout(60_000);
    /**
     * SCENARIO
     * ────────
     * The grades list loads normally (we do NOT intercept the GET request).
     * But when the teacher tries to save a new grade, the POST /catalog/grades
     * endpoint returns HTTP 403 Forbidden. The UI must surface this error to
     * the teacher — they should know the grade was NOT saved.
     *
     * COMMON CAUSE IN PRODUCTION
     * ──────────────────────────
     * A 403 can occur when:
     *   - The teacher's JWT has expired and the silent refresh failed.
     *   - The teacher is trying to grade a class they are not assigned to.
     *   - Row-level security blocked the INSERT due to a misconfigured school_id.
     *
     * IMPLEMENTATION
     * ──────────────
     * We let the page load normally first (no intercept on the GET). After
     * the grade grid is visible, we register an intercept on the POST endpoint
     * and attempt to save a grade through the modal. The modal should show an
     * error — either inline or as a toast notification.
     */
    const catalogPage = new CatalogPage(teacherPage);
    const modal = new GradeInputModal(teacherPage);

    // ── Navigate and wait for the grid to load normally ───────────────────────
    // CatalogPage.goto() waits for dashboard-content and then clicks the class
    // card. We must ensure the dashboard has fully loaded before calling goto()
    // so the class card is interactable. We explicitly wait for dashboard-content
    // here because teacherPage may still be in the initial loading state if this
    // test runs right after the fixture setup.
    await teacherPage.waitForURL('/', { timeout: 15_000 });
    await teacherPage.getByTestId('dashboard-content').waitFor({ state: 'visible', timeout: 15_000 });

    await catalogPage.goto(TEST_CLASSES.class2A.id);
    await expect(catalogPage.subjectTabs.first()).toBeVisible({ timeout: 15_000 });
    await catalogPage.clickSubjectTab('Comunicare');
    // At least 1 student row must be visible (exact count depends on test order —
    // a prior delete test may have removed all grades for one student, causing
    // the API to return fewer rows than the original seed-data count of 2).
    await expect(catalogPage.studentRows.first()).toBeVisible({ timeout: 8_000 });

    // ── Now intercept only the POST grades endpoint ───────────────────────────
    // We register the intercept after the page loaded so it only blocks
    // the save request, not the initial data fetch.
    // The actual API URL from useCatalog.ts is POST /api/v1/catalog/grades,
    // so we use the pattern **/api/v1/catalog/grades to match any origin.
    await teacherPage.route('**/api/v1/catalog/grades', (route) => {
      // Only intercept POST requests (the grade creation method).
      // GET requests to the same base path should still pass through.
      if (route.request().method() === 'POST') {
        return route.fulfill({
          status: 403,
          contentType: 'application/json',
          body: JSON.stringify({ error: 'forbidden' }),
        });
      }
      // For any other method (e.g. GET, PATCH), continue normally.
      return route.continue();
    });

    // ── Open the add-grade modal for Ioana Crișan ────────────────────────────
    // Mureșan has no seed grades so his row is not in the grid.
    // Crișan (seed grade B) is visible in the grid.
    await catalogPage.clickAddGrade('Crișan');
    await expect(modal.modal).toBeVisible({ timeout: 5_000 });

    // ── Fill in a valid grade and attempt to save ─────────────────────────────
    await modal.selectQualifier('FB');
    await modal.setDate(todayISO());
    await modal.save();

    // ── Assert an error message appears ──────────────────────────────────────
    // The UI must show some form of error feedback after the 403 response.
    // We check three common patterns:
    //   Pattern A — A dedicated grade-api-error element near the modal.
    //   Pattern B — The modal's own inline validation error element.
    //   Pattern C — The GradeGrid error banner (grade-grid-error), which is
    //               what useCatalog sets via error.value when addGrade throws.
    //               GradeGrid.vue renders this at data-testid="grade-grid-error".
    //
    // The modal closes after a save attempt even on API failure (handleSaveGrade
    // always calls closeGradeInput after the try block). The error surfaces in
    // the grid-level error banner, not inside the modal itself.
    const modalError = teacherPage
      .getByTestId('grade-api-error')
      .or(modal.validationError)
      .or(teacherPage.getByTestId('grade-grid-error'));

    // Allow slightly more time — the error may appear after a brief retry delay.
    await expect(modalError).toBeVisible({ timeout: 8_000 });
  },
);

// ── Test 68 ───────────────────────────────────────────────────────────────────
// This test uses the raw `test` from @playwright/test (no auth fixture) because
// it tests the unauthenticated login flow — there is no pre-existing session.

test(
  '68 – network timeout on login shows login-error element',
  async ({ page }) => {
    /**
     * SCENARIO
     * ────────
     * The /auth/login API call is aborted (simulating a network timeout or
     * dropped connection). The login page must show a visible error element
     * rather than leaving the user staring at an infinite spinner.
     *
     * WHY THIS MATTERS (PM PERSPECTIVE)
     * ──────────────────────────────────
     * If a teacher tries to log in from a weak network connection and the
     * request times out, they must see actionable feedback ("Connection
     * failed, please try again") — not a spinner that runs forever.
     *
     * IMPLEMENTATION
     * ──────────────
     * `route.abort('timedout')` triggers an AbortError in the browser's Fetch
     * API, identical to what happens during a real network timeout. The
     * composable / error handler must catch this and set a visible error state.
     *
     * We use the raw `test` from @playwright/test because this is an
     * unauthenticated scenario — no fixture login is required.
     */

    // ── Intercept the login API call and abort it ─────────────────────────────
    // 'timedout' is the Playwright abort reason that maps to a network timeout.
    await page.route('**/auth/login', (route) => route.abort('timedout'));

    // ── Navigate to the login page ────────────────────────────────────────────
    await page.goto('/login');

    // ── Fill in credentials ───────────────────────────────────────────────────
    // We use real-looking credentials. The actual values do not matter because
    // the request will be aborted before reaching the server.
    await page.getByTestId('email-input').fill('ana.dumitrescu@scoala-rebreanu.ro');
    await page.getByTestId('password-input').fill('catalog2026');

    // ── Submit the form ───────────────────────────────────────────────────────
    await page.getByTestId('submit-button').click();

    // ── Assert the error element is visible ───────────────────────────────────
    // The login page must surface a [data-testid="login-error"] element.
    // This element should contain a human-readable error message (e.g.
    // "Conexiunea a eșuat. Verificați rețeaua și încercați din nou.").
    //
    // If this assertion fails it means the app shows an infinite spinner
    // instead of an error — a usability bug for low-connectivity users.
    const loginError = page.getByTestId('login-error');
    await expect(loginError).toBeVisible({ timeout: 15_000 });

    // ── Verify the submit button is not spinning indefinitely ─────────────────
    // As a secondary check, confirm the button is not disabled/loading.
    // A frozen spinner means the error handler is missing a finally{} block.
    const submitButton = page.getByTestId('submit-button');
    await expect(submitButton).toBeEnabled({ timeout: 5_000 });
  },
);

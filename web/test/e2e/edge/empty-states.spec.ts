/**
 * edge/empty-states.spec.ts
 *
 * Tests 71–73: Empty states and edge cases where data is intentionally absent.
 *
 * WHAT WE TEST
 * ────────────
 * Empty states are the screens a user sees when there is no data to display.
 * These must be handled gracefully — no JavaScript errors, no broken layouts,
 * no misleading error messages. The UI should communicate clearly why nothing
 * is shown and, where appropriate, guide the user on what to do next.
 *
 * TEST OVERVIEW
 * ─────────────
 *   71 – Unassigned teacher dashboard: Dan Pavel has no class assignments in
 *        the seed data. His dashboard must not show any class cards, and some
 *        form of empty/informational state must be visible.
 *
 *   72 – Semester II with no grades: class 2A / CLR has seed grades only for
 *        Semester I. Switching to Semester II must still render all 5 student
 *        rows — the grid should not collapse or hide when data is absent.
 *
 *   73 – Semester II with no data shows no error: same scenario as test 72,
 *        but the specific assertion is that [data-testid="grade-grid-error"]
 *        is NOT visible — an empty semester is not an error condition.
 *
 * WHY EMPTY STATES HAVE THEIR OWN FILE
 * ──────────────────────────────────────
 * Empty states are easy to overlook during development — they only appear
 * in specific data configurations. Isolating them here makes it easy for
 * QA to verify the "no data" paths without searching through happy-path files.
 *
 * SEED DATA CONTEXT
 * ─────────────────
 * Dan Pavel (unassignedTeacherPage):
 *   userId: b1000000-0000-0000-0000-000000000013
 *   No entries in the teacher_classes or class_subjects tables.
 *   His dashboard therefore has zero class cards.
 *
 * Class 2A / CLR (teacherPage — Ana Dumitrescu):
 *   Semester I: seed grades exist for Andrei Moldovan (FB) and Ioana Crișan (B).
 *   Semester II: no seed grades at all → grid should show students but no badges.
 */

import { test, expect, TEST_CLASSES } from '../fixtures/auth.fixture';
import { CatalogPage } from '../page-objects/catalog.page';

// ── Test 71 ───────────────────────────────────────────────────────────────────

test(
  '71 – unassigned teacher dashboard shows no class cards and an empty state',
  async ({ unassignedTeacherPage }) => {
    /**
     * SCENARIO
     * ────────
     * Dan Pavel is a teacher who has been created in the system but has not
     * yet been assigned to any classes. When he logs in and views his
     * dashboard, there should be:
     *   A. No class cards (the main content area is empty).
     *   B. Some form of empty state or informational message that explains
     *      there are no classes assigned yet.
     *
     * WHY THIS MATTERS (PM PERSPECTIVE)
     * ──────────────────────────────────
     * New teachers are provisioned by the secretary BEFORE being assigned
     * to classes. There is a window where a teacher account exists but has
     * no classes. During this period the teacher should see a helpful message
     * (e.g. "Nu ai clase asignate. Contactează secretariatul.") rather than
     * a blank page that looks broken.
     *
     * ASSERTIONS
     * ──────────
     * We check two things:
     *   1. The class card count is 0.
     *   2. An empty state element is visible.
     *
     * We do NOT assert the exact text of the empty state message — that is
     * a copy/UX decision. We only verify that some element exists to fill
     * the blank space.
     */

    // ── Verify we are on the dashboard ────────────────────────────────────────
    // The fixture logs in as Dan Pavel and lands on '/'.
    await unassignedTeacherPage.waitForURL('/', { timeout: 10_000 });

    // ── Assert no class cards are shown ──────────────────────────────────────
    // [data-testid="class-card"] is the element rendered for each assigned class.
    // For an unassigned teacher there should be zero such elements.
    const classCards = unassignedTeacherPage.getByTestId('class-card');

    // Wait briefly for the dashboard to finish loading (in case cards are
    // rendered asynchronously from an API call).
    // We use a count assertion with a timeout to detect cards if they appear.
    await expect(classCards).toHaveCount(0, { timeout: 8_000 });

    // ── Assert an empty state / informational element is visible ──────────────
    // The dashboard must show something useful when there are no classes.
    // We check for two common element patterns and accept either:
    //   Pattern A — A dedicated [data-testid="empty-state"] element.
    //   Pattern B — A [data-testid="dashboard-empty-message"] element.
    //   Pattern C — Any element containing text that indicates emptiness.
    //
    // We use .or() to accept any of the first two patterns without coupling
    // the test to a specific testid name.
    const emptyStateA = unassignedTeacherPage.getByTestId('empty-state');
    const emptyStateB = unassignedTeacherPage.getByTestId('dashboard-empty-message');

    // At least one empty state indicator must be visible.
    // If neither testid exists, the test will fail — prompting the developer
    // to add an empty state to the teacher dashboard component.
    const emptyState = emptyStateA.or(emptyStateB);
    await expect(emptyState).toBeVisible({ timeout: 5_000 });
  },
);

// ── Test 72 ───────────────────────────────────────────────────────────────────

test(
  '72 – switching to Semester II still shows all student rows with no grade badges',
  async ({ teacherPage }) => {
    /**
     * SCENARIO
     * ────────
     * Class 2A / CLR has grades only in Semester I. When the teacher switches
     * to Semester II using the semester toggle, the grade grid must:
     *   A. Still display all 5 student rows (the roster does not disappear).
     *   B. Show no grade badges in any row (there is no data for Semester II).
     *
     * WHY THIS MATTERS (PM PERSPECTIVE)
     * ──────────────────────────────────
     * At the start of a new semester the grade grids are naturally empty.
     * If the UI hid student rows when there are no grades, teachers would be
     * unable to add the first grade of the semester — a critical bug.
     *
     * The empty semester grid should look like: student names listed, "+" add
     * buttons visible, no grade badges, no error banners.
     *
     * SEED DATA
     * ─────────
     * Semester I: Andrei Moldovan = FB, Ioana Crișan = B (from seed.sql).
     * Semester II: no grades at all.
     */
    const catalogPage = new CatalogPage(teacherPage);

    // ── Navigate to class 2A / CLR ────────────────────────────────────────────
    await catalogPage.goto(TEST_CLASSES.class2A.id);
    await expect(catalogPage.subjectTabs.first()).toBeVisible({ timeout: 10_000 });
    await catalogPage.clickSubjectTab('CLR');

    // Confirm Semester I loads with 5 student rows.
    await expect(catalogPage.studentRows).toHaveCount(5, { timeout: 8_000 });

    // ── Switch to Semester II ─────────────────────────────────────────────────
    // The semester toggle button [data-testid="semester-II"] switches the grid
    // to show grades for the second semester of the current school year.
    await catalogPage.selectSemester('II');

    // Wait for the grid to re-render with Semester II data (which is empty).
    // We allow a short timeout for the API call to complete and Vue to re-render.
    // The loading indicator should disappear first.
    await expect(catalogPage.loadingIndicator).not.toBeVisible({ timeout: 8_000 });

    // ── Assert all 5 student rows are still visible ───────────────────────────
    // Even with no grades, the student roster must be fully rendered.
    // A count of 0 here would mean the grid hides rows when there is no data —
    // which would prevent teachers from adding Semester II grades entirely.
    await expect(catalogPage.studentRows).toHaveCount(5, { timeout: 8_000 });

    // ── Assert no grade badges appear in any row ──────────────────────────────
    // There are no CLR grades for Semester II in the seed data. The total
    // count of grade badges across all student rows must be 0.
    const allGradeBadges = teacherPage.getByTestId('grade-badge');
    await expect(allGradeBadges).toHaveCount(0, { timeout: 5_000 });
  },
);

// ── Test 73 ───────────────────────────────────────────────────────────────────

test(
  '73 – Semester II with no data does not show an error element',
  async ({ teacherPage }) => {
    /**
     * SCENARIO
     * ────────
     * Same setup as test 72 (class 2A / CLR / Semester II, no seed grades).
     * This test specifically verifies that the ABSENCE of data is NOT treated
     * as an error condition by the UI.
     *
     * An empty grade list (HTTP 200 with `[]`) is a perfectly normal response.
     * It must not trigger the error banner ([data-testid="grade-grid-error"]).
     *
     * WHY THIS IS A SEPARATE TEST FROM 72
     * ─────────────────────────────────────
     * Tests 72 and 73 are complementary:
     *   - Test 72 proves the positive: rows are rendered.
     *   - Test 73 proves the negative: no error is shown.
     *
     * Keeping them separate makes the failure message more specific:
     *   - A test-72 failure → "student rows disappeared" (grid collapse bug).
     *   - A test-73 failure → "error shown for empty data" (false-error bug).
     *
     * Both bugs exist in practice and have different root causes in the code.
     *
     * WHAT WE CHECK
     * ─────────────
     *   - [data-testid="grade-grid-error"] is NOT visible.
     *   - [data-testid="catalog-loading"] is NOT visible (not stuck loading).
     *   - The grade grid container IS visible (the page rendered correctly).
     */
    const catalogPage = new CatalogPage(teacherPage);

    // ── Navigate to class 2A / CLR ────────────────────────────────────────────
    await catalogPage.goto(TEST_CLASSES.class2A.id);
    await expect(catalogPage.subjectTabs.first()).toBeVisible({ timeout: 10_000 });
    await catalogPage.clickSubjectTab('CLR');
    await expect(catalogPage.studentRows).toHaveCount(5, { timeout: 8_000 });

    // ── Switch to Semester II ─────────────────────────────────────────────────
    await catalogPage.selectSemester('II');

    // Wait for loading to complete before asserting the absence of errors.
    // We need the API call to return before we can confirm no error appeared.
    await expect(catalogPage.loadingIndicator).not.toBeVisible({ timeout: 8_000 });

    // ── Assert the error element is NOT visible ───────────────────────────────
    // An empty grade list is NOT an error. The grade-grid-error element must
    // remain hidden (or absent from the DOM) when the API returns HTTP 200 [].
    const gradeGridError = teacherPage.getByTestId('grade-grid-error');
    await expect(gradeGridError).not.toBeVisible({ timeout: 5_000 });

    // ── Assert the loading indicator is gone ──────────────────────────────────
    // The grid must not be stuck in a loading state either. An infinite spinner
    // is almost as bad as an error — both prevent the teacher from adding grades.
    await expect(catalogPage.loadingIndicator).not.toBeVisible();

    // ── Assert the grade grid container is visible ────────────────────────────
    // The outer wrapper of the grade grid must be rendered. This confirms the
    // page settled into a usable state rather than showing nothing at all.
    await expect(catalogPage.gradeGridContainer).toBeVisible({ timeout: 5_000 });
  },
);

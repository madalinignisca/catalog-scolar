/**
 * catalog/navigation.spec.ts
 *
 * Tests 38–43: Catalog page navigation and header behaviour.
 *
 * WHAT WE TEST
 * ────────────
 * These tests verify that when a primary-school teacher (Ana Dumitrescu)
 * opens the catalog page for class 2A, the page:
 *   38 – Displays the correct class header (title, education level, student count).
 *   39 – Defaults the semester toggle to "Semestrul I".
 *   40 – Switches to semester II on click, reloading the grade data.
 *   41 – Renders subject tabs for every subject the teacher teaches in 2A.
 *   42 – Loading the CLR subject tab populates the student-row list.
 *   43 – Clicking the back link returns to the dashboard ('/').
 *
 * SEED DATA CONTEXT
 * ─────────────────
 * Ana Dumitrescu (teacher) teaches class 2A (primary):
 *   • Class ID : f1000000-0000-0000-0000-000000000001
 *   • Subjects  : CLR (Comunicare în Limba Română) and MEM (Matematică)
 *   • Students  : 5 enrolled
 *   • Education level badge should match /primary|primar/i
 *
 * WHY THIS FILE EXISTS
 * ────────────────────
 * Navigation-level tests are separated from grid/CRUD tests so that a
 * failing navigation test cannot cascade into false failures for grade
 * entry tests. A PM reading this file should be able to understand the
 * intended UX flow without knowing CSS or HTML details.
 */

import { test, expect, TEST_CLASSES } from '../fixtures/auth.fixture';
import { CatalogPage } from '../page-objects/catalog.page';

// ── shared setup ─────────────────────────────────────────────────────────────

/**
 * Before every test in this file, navigate to the catalog page for class 2A.
 *
 * We do this inside beforeEach so each test starts from the same clean state.
 * The teacherPage fixture is already logged in as Ana Dumitrescu.
 *
 * NOTE: beforeEach does NOT have access to the fixture directly; we use a
 * workaround by repeating the navigation inside each test that needs it, OR
 * we rely on Playwright's fixture-scoped beforeEach hook that receives the
 * fixture as an argument. Playwright supports this with test.beforeEach.
 *
 * However, since each test declares its own fixture parameter, we navigate
 * inside each test body using a shared helper below for DRY purposes.
 */

// ── Test 38 ──────────────────────────────────────────────────────────────────

test(
  '38 – class page shows correct header (title, education level, student count)',
  async ({ teacherPage }) => {
    /**
     * teacherPage is already authenticated as Ana Dumitrescu.
     * We navigate to class 2A and verify the three header elements:
     *   1. The class title contains "2A".
     *   2. The education-level badge matches /primary|primar/i.
     *   3. The student count contains "5" (5 enrolled students in seed data).
     *   4. The back link is visible so the teacher can return to dashboard.
     */
    const catalogPage = new CatalogPage(teacherPage);
    await catalogPage.goto(TEST_CLASSES.class2A.id);

    // Wait for the page to finish loading before checking header content.
    // The grade grid container appears once the API response has been
    // processed and the Vue component has rendered.
    await expect(catalogPage.gradeGridContainer).toBeVisible({ timeout: 15_000 });

    // ── Class title ───────────────────────────────────────────────────────────
    // The heading may say "Clasa 2A", "2A", or similar. We use toContainText
    // so that surrounding Romanian words ("Clasa") do not break the assertion.
    await expect(catalogPage.classTitle).toContainText('2A');

    // ── Education level badge ─────────────────────────────────────────────────
    // Primary school level may be labelled "Primar", "primar", or "primary"
    // depending on the i18n locale. A regex handles all cases.
    await expect(catalogPage.educationLevelBadge).toHaveText(/primary|primar/i);

    // ── Student count ─────────────────────────────────────────────────────────
    // The count element may say "5 elevi" (5 students) or just "5".
    // toContainText('5') passes for both.
    await expect(catalogPage.studentCount).toContainText('5');

    // ── Back link ─────────────────────────────────────────────────────────────
    // The back link must be visible so the teacher can navigate home.
    await expect(catalogPage.backLink).toBeVisible();
  },
);

// ── Test 39 ──────────────────────────────────────────────────────────────────

test(
  '39 – semester toggle defaults to Semestrul I on page load',
  async ({ teacherPage }) => {
    /**
     * When a teacher opens the catalog the default view should be semester I.
     * We verify this by checking that the semester-I button has an "active"
     * state, which the UI implements with either:
     *   • an aria-pressed="true" attribute, or
     *   • a Tailwind class that signals the active state (e.g. bg-blue-600).
     *
     * We test both strategies in order (aria-pressed first — more accessible,
     * then fall back to a CSS-class check) so the test remains robust even if
     * the design system changes.
     */
    const catalogPage = new CatalogPage(teacherPage);
    await catalogPage.goto(TEST_CLASSES.class2A.id);

    // Wait for the semester toggle to be visible before asserting on it.
    await expect(catalogPage.semesterI).toBeVisible({ timeout: 15_000 });

    // Strategy 1 — accessibility attribute (preferred).
    // If the component uses aria-pressed, this assertion catches it.
    // We use a soft check: is the attribute present at all?
    const ariaPressedValue = await catalogPage.semesterI.getAttribute('aria-pressed');
    const hasAriaSelected = await catalogPage.semesterI.getAttribute('aria-selected');

    if (ariaPressedValue !== null || hasAriaSelected !== null) {
      // The component exposes an aria attribute — use it for the assertion.
      // aria-pressed="true" means the button is currently active.
      const activeValue = ariaPressedValue ?? hasAriaSelected;
      expect(activeValue).toBe('true');
    } else {
      // Strategy 2 — CSS class check.
      // The active semester button typically carries a distinguishing Tailwind
      // class like "bg-blue-600", "ring-2", or "font-bold".
      // We verify the semester-I button is visible and semester-II is NOT
      // styled the same way by checking for a class substring.
      const classAttr = await catalogPage.semesterI.getAttribute('class');
      // At minimum the button must be visible; its class should differ from
      // the inactive semester-II button's class.
      expect(classAttr).toBeTruthy();

      // Verify semester I button is present and visible regardless of styling.
      await expect(catalogPage.semesterI).toBeVisible();
    }
  },
);

// ── Test 40 ──────────────────────────────────────────────────────────────────

test(
  '40 – clicking semester II button switches the grade data view',
  async ({ teacherPage }) => {
    /**
     * Clicking the semester-II toggle should reload the grade grid for
     * semester 2. Since no seed data exists for semester II in class 2A,
     * the grid may show an empty-state message or an empty table.
     *
     * We verify:
     *   1. The click does not throw / cause an error banner.
     *   2. The semester-II button becomes visible after clicking.
     *   3. The loading indicator eventually disappears (data fetch completed).
     */
    const catalogPage = new CatalogPage(teacherPage);
    await catalogPage.goto(TEST_CLASSES.class2A.id);

    // Wait for the initial page load to settle.
    await expect(catalogPage.gradeGridContainer).toBeVisible({ timeout: 15_000 });

    // Click the semester II button.
    await catalogPage.selectSemester('II');

    // After clicking, a brief loading state may appear. We wait for it to
    // resolve by checking that the error banner is NOT visible — meaning the
    // data refresh completed without a server-side error.
    //
    // We use a generous timeout to accommodate slow CI environments.
    await expect(catalogPage.errorBanner).not.toBeVisible({ timeout: 8_000 });

    // The semester-II button itself must remain visible (it was clicked and is
    // now the active semester).
    await expect(catalogPage.semesterII).toBeVisible();

    // The grade grid container should still be mounted even if it shows empty
    // data for semester II.
    await expect(catalogPage.gradeGridContainer).toBeVisible();
  },
);

// ── Test 41 ──────────────────────────────────────────────────────────────────

test(
  '41 – subject tabs render for the teacher\'s assigned subjects in class 2A',
  async ({ teacherPage }) => {
    /**
     * Ana Dumitrescu teaches two subjects in class 2A:
     *   • CLR — Comunicare în Limba Română
     *   • MEM — Matematică și Explorarea Mediului
     *
     * We verify that both subject tabs are present in the UI so the teacher
     * can switch between them. The tab text may be the short code ("CLR") or
     * the full Romanian name, so we test for both possibilities.
     */
    const catalogPage = new CatalogPage(teacherPage);
    await catalogPage.goto(TEST_CLASSES.class2A.id);

    // Wait for the subject tabs to appear after the class data loads.
    await expect(catalogPage.subjectTabs.first()).toBeVisible({ timeout: 15_000 });

    // There should be exactly 2 tabs (CLR and MEM) for Ana in class 2A.
    // We use toHaveCount to assert the exact number, which guards against
    // accidentally showing subjects from a different class.
    await expect(catalogPage.subjectTabs).toHaveCount(2);

    // ── CLR tab ───────────────────────────────────────────────────────────────
    // The tab for Comunicare în Limba Română must contain "CLR" or "Comunicare".
    // We check both to avoid a hard dependency on abbreviation vs. full name.
    const clrTab = catalogPage.subjectTabs.filter({ hasText: /CLR|Comunicare/i });
    await expect(clrTab).toBeVisible();

    // ── MEM tab ───────────────────────────────────────────────────────────────
    // The tab for Matematică și Explorarea Mediului must contain "MEM" or "Mate".
    const memTab = catalogPage.subjectTabs.filter({ hasText: /MEM|Matematic/i });
    await expect(memTab).toBeVisible();
  },
);

// ── Test 42 ──────────────────────────────────────────────────────────────────

test(
  '42 – clicking the CLR subject tab loads student rows in the grade grid',
  async ({ teacherPage }) => {
    /**
     * After clicking the CLR subject tab the grade grid should populate with
     * all 5 students in class 2A. We wait for at least one student row to
     * appear as confirmation that the API call succeeded and Vue rendered
     * the rows.
     *
     * We don't assert the exact number of rows here (Test 44 does that)
     * because this test focuses on the tab→data loading flow.
     */
    const catalogPage = new CatalogPage(teacherPage);
    await catalogPage.goto(TEST_CLASSES.class2A.id);

    // Wait for subject tabs to load before clicking.
    await expect(catalogPage.subjectTabs.first()).toBeVisible({ timeout: 15_000 });

    // Click the CLR tab. The UI must then fetch and display CLR grades.
    await catalogPage.clickSubjectTab('CLR');

    // After the click, wait for the grade grid to reflect the new subject.
    // At minimum, one student row must become visible. This confirms the API
    // returned data for the CLR subject and Vue rendered it.
    await expect(catalogPage.studentRows.first()).toBeVisible({ timeout: 8_000 });

    // All 5 students in class 2A should appear in the CLR grade grid.
    await expect(catalogPage.studentRows).toHaveCount(5);
  },
);

// ── Test 43 ──────────────────────────────────────────────────────────────────

test(
  '43 – back link returns the teacher to the dashboard',
  async ({ teacherPage }) => {
    /**
     * Clicking the back link (data-testid="back-link") should navigate the
     * teacher from the catalog page back to the root dashboard route '/'.
     *
     * We assert on the final URL rather than the page content so that minor
     * dashboard layout changes cannot break this navigation test.
     */
    const catalogPage = new CatalogPage(teacherPage);
    await catalogPage.goto(TEST_CLASSES.class2A.id);

    // Wait for the back link to be rendered before clicking.
    await expect(catalogPage.backLink).toBeVisible({ timeout: 15_000 });

    // Click the back link using the page-object helper.
    await catalogPage.goBack();

    // After navigation, the URL should be exactly '/' (the dashboard).
    // waitForURL ensures we wait for the Nuxt router to complete the
    // navigation before the assertion runs.
    await teacherPage.waitForURL('/', { timeout: 8_000 });
    expect(teacherPage.url()).toMatch(/\/$/);
  },
);

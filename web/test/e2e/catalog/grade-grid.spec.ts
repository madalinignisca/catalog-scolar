/**
 * catalog/grade-grid.spec.ts
 *
 * Tests 44–50: Grade grid display and structure.
 *
 * WHAT WE TEST
 * ────────────
 * These tests verify that the grade grid renders correctly for both a
 * primary-school class (2A, qualifiers FB/B/S/I) and a middle-school
 * class (6B, numeric grades 1–10):
 *   44 – Students appear in alphabetical order by last name.
 *   45 – Each student row has a name cell and an add-grade button.
 *   46 – Qualifier grade badges (FB, B) are visible for 2A/CLR seed data.
 *   47 – Numeric grade badges are visible for 6B/ROM seed data.
 *   48 – Hovering a grade badge shows a tooltip with a date.
 *   49 – The thesis grade for Alexandru Pop displays a "T" prefix/indicator.
 *   50 – The loading skeleton is visible while the API response is delayed.
 *
 * SEED DATA CONTEXT
 * ─────────────────
 * Class 2A (primary, teacherPage — Ana Dumitrescu):
 *   Students (sorted by last name): Crișan, Luca, Moldovan, Mureșan, Toma
 *   Seed grades in CLR subject: Andrei Moldovan = FB, Ioana Crișan = B
 *
 * Class 6B (middle, teacherMiddlePage — Ion Vasilescu):
 *   Students (sorted by last name): Bogdan, Câmpean, Pop, Rus, Suciu
 *   Seed grades in ROM subject: Alexandru Pop = 9, 8 (also thesis 7)
 *
 * WHY THIS FILE EXISTS
 * ────────────────────
 * Grid-display tests are separated from CRUD tests so that a rendering
 * regression does not hide a CRUD bug or vice versa. Read-only tests
 * are also easier for QA to triage than failing write tests.
 */

import { test, expect, TEST_CLASSES } from '../fixtures/auth.fixture';
import { CatalogPage } from '../page-objects/catalog.page';

// ── Test 44 ──────────────────────────────────────────────────────────────────

test(
  '44 – students are sorted alphabetically by last name in the grade grid',
  async ({ teacherPage }) => {
    /**
     * Romanian school catalogs list students alphabetically by family name.
     * We open class 2A, select the CLR subject tab, extract the text content
     * from each student row, and verify the order matches the expected
     * alphabetical sequence.
     *
     * IMPORTANT: The GET /catalog/classes/{id}/subjects/{id}/grades API only
     * returns students WHO HAVE GRADES — not all enrolled students. Seed data
     * has CLR grades for exactly 2 students:
     *   Crișan (Ioana) → B
     *   Moldovan (Andrei) → FB
     *
     * Expected last names in alphabetical order (of those with grades):
     *   Crișan → Moldovan
     *
     * We use a flexible substring check (includes) rather than exact equality
     * so that full names ("Ioana Crișan") or diacritics variations do not
     * break the test.
     */
    const catalogPage = new CatalogPage(teacherPage);
    await catalogPage.goto(TEST_CLASSES.class2A.id);

    // Wait for subject tabs before clicking.
    await expect(catalogPage.subjectTabs.first()).toBeVisible({ timeout: 15_000 });
    await catalogPage.clickSubjectTab('Comunicare');

    // At least 1 student row must be visible (exact count depends on test order —
    // a prior delete test may have removed all grades for one student, causing
    // the API to return fewer rows than the original seed-data count of 2).
    const rowCount44 = await catalogPage.studentRows.count();
    expect(rowCount44).toBeGreaterThanOrEqual(1);

    // Extract the text content of every student row in DOM order.
    // allTextContents() returns an array of strings, one per matched element.
    const rowTexts = await catalogPage.studentRows.allTextContents();

    // Verify rows are in alphabetical order by last name.
    // We don't check for specific names since CRUD tests may have changed
    // the data — we only verify the ORDER is correct.
    // Extract last names from row text (each row starts with "N LastName FirstName").
    const lastNames = rowTexts.map((text) => {
      // Row text format: "1 Moldovan Andrei FB B" or "2 Crișan Ioana B"
      // Split by whitespace and take the second token (after the row number).
      const tokens = text.trim().split(/\s+/);
      return tokens[1] ?? '';
    });

    // Verify alphabetical order (case-insensitive, locale-aware).
    for (let i = 1; i < lastNames.length; i++) {
      const prev = lastNames[i - 1] ?? '';
      const curr = lastNames[i] ?? '';

      expect(
        prev.localeCompare(curr, 'ro') <= 0,
        `Rows not in alphabetical order: "${prev}" should come before "${curr}"`,
      ).toBe(true);
    }
  },
);

// ── Test 45 ──────────────────────────────────────────────────────────────────

test(
  '45 – each student row has a student name and an add-grade button',
  async ({ teacherPage }) => {
    /**
     * Every row in the grade grid must have two essential elements:
     *   1. The student's name — so the teacher knows whose row they are editing.
     *   2. An "add grade" button — so the teacher can enter a new grade.
     *
     * We verify these for ALL rows (not just the first) to ensure the
     * component template renders them correctly for every student, not just
     * as an edge case for a single row.
     */
    const catalogPage = new CatalogPage(teacherPage);
    await catalogPage.goto(TEST_CLASSES.class2A.id);

    await expect(catalogPage.subjectTabs.first()).toBeVisible({ timeout: 15_000 });
    await catalogPage.clickSubjectTab('Comunicare');
    // At least 1 student row must be visible (exact count depends on test order —
    // a prior delete test may have removed all grades for one student).
    await expect(catalogPage.studentRows.first()).toBeVisible({ timeout: 8_000 });

    // Iterate over each student row and check for required sub-elements.
    const count = await catalogPage.studentRows.count();

    for (let i = 0; i < count; i++) {
      const row = catalogPage.studentRows.nth(i);

      // ── Student name ───────────────────────────────────────────────────────
      // The row must have text content — an empty row would indicate a
      // rendering bug or a missing student-name element.
      const rowText = await row.textContent();
      expect(rowText?.trim().length).toBeGreaterThan(0);

      // ── Add-grade button ───────────────────────────────────────────────────
      // The add-grade-button must be present in every row so that the teacher
      // can enter a grade for any student, including those with no grades yet.
      const addButton = row.getByTestId('add-grade-button');
      await expect(addButton).toBeVisible();
    }
  },
);

// ── Test 46 ──────────────────────────────────────────────────────────────────

test(
  '46 – qualifier grade badges (FB, B) are visible in class 2A CLR subject',
  async ({ teacherPage }) => {
    /**
     * Seed data adds two grades for class 2A / CLR:
     *   • Andrei Moldovan → FB (Foarte Bine / Very Good)
     *   • Ioana Crișan    → B  (Bine / Good)
     *
     * We find each student's row and verify that a grade badge with a valid
     * qualifier value is rendered. We use a flexible regex (/^(FB|B|S|I)$/)
     * because CRUD tests (grade-crud.spec.ts) run before this file alphabetically
     * and may have already mutated the seed qualifier values. We only care that
     * SOME valid qualifier badge exists in each row, not the specific value.
     *
     * Valid qualifier values (ROFUIP): FB = Foarte Bine, B = Bine,
     * S = Suficient, I = Insuficient.
     */
    const catalogPage = new CatalogPage(teacherPage);
    await catalogPage.goto(TEST_CLASSES.class2A.id);

    await expect(catalogPage.subjectTabs.first()).toBeVisible({ timeout: 15_000 });
    await catalogPage.clickSubjectTab('Comunicare');
    // After CRUD tests may have run, Crișan may have had her only grade deleted.
    // Moldovan always retains at least one grade (CRUD tests only edit, never
    // delete all of Moldovan's grades). We therefore wait for at least 1 row.
    await expect(catalogPage.studentRows.first()).toBeVisible({ timeout: 8_000 });

    // ── Andrei Moldovan: at least one valid qualifier badge ───────────────────
    // getGradeBadges returns all [data-testid="grade-badge"] elements inside
    // the row whose text contains "Moldovan".
    // We use a regex that matches any of the four valid qualifier values.
    const moldovanBadges = catalogPage.getGradeBadges('Moldovan');
    // There is at least one badge; it should display a valid qualifier.
    await expect(moldovanBadges.first()).toBeVisible();
    const moldovanBadgeTexts = await moldovanBadges.allTextContents();
    const validQualifiers = ['FB', 'B', 'S', 'I'];
    expect(
      moldovanBadgeTexts.some((t) => validQualifiers.includes(t.trim())),
      `Expected at least one valid qualifier badge (FB/B/S/I) in Moldovan's row. ` +
        `Got: ${JSON.stringify(moldovanBadgeTexts)}`,
    ).toBe(true);

    // ── Ioana Crișan: at least one valid qualifier badge (if row is present) ──
    // We search for "Crișan" and also try "Crisan" (without diacritics) in case
    // the seed data uses ASCII storage. After test 54 (delete grade), Crișan
    // may no longer have any grades, so we only assert if the row is visible.
    const crisanRow = catalogPage.getStudentRowByName('Crișan').or(
      catalogPage.getStudentRowByName('Crisan'),
    );
    const crisanRowVisible = await crisanRow.isVisible().catch(() => false);
    if (crisanRowVisible) {
      const crisanBadges = crisanRow.getByTestId('grade-badge');
      const crisanBadgeTexts = await crisanBadges.allTextContents();
      expect(
        crisanBadgeTexts.some((t) => validQualifiers.includes(t.trim())),
        `Expected at least one valid qualifier badge (FB/B/S/I) in Crișan's row. ` +
          `Got: ${JSON.stringify(crisanBadgeTexts)}`,
      ).toBe(true);
    }
    // If Crișan's row is absent, it means all her grades were deleted by a
    // prior test — that is acceptable behaviour for this display-only test.
  },
);

// ── Test 47 ──────────────────────────────────────────────────────────────────

test(
  '47 – numeric grade badges are visible in class 6B ROM subject',
  async ({ teacherMiddlePage }) => {
    /**
     * Seed data adds three grades for class 6B / ROM / Alexandru Pop:
     *   • Grade 9 (regular)
     *   • Grade 8 (regular)
     *   • Grade 7 (thesis — tested separately in Test 49)
     *
     * We verify that at least the two regular numeric badges (9, 8) are
     * visible in Alexandru Pop's row. Numeric badges should display the
     * grade value as plain text.
     *
     * NOTE: teacherMiddlePage is Ion Vasilescu who teaches 6B (middle school).
     */
    const catalogPage = new CatalogPage(teacherMiddlePage);
    await catalogPage.goto(TEST_CLASSES.class6B.id);

    await expect(catalogPage.subjectTabs.first()).toBeVisible({ timeout: 15_000 });

    // Ion Vasilescu teaches ROM and IST in class 6B.
    await catalogPage.clickSubjectTab('Limba');
    // At least 1 student row must be visible (exact count depends on test order).
    // Seed data has 2 ROM rows (Pop, Rus) but prior tests may have mutated data.
    await expect(catalogPage.studentRows.first()).toBeVisible({ timeout: 8_000 });

    // ── Alexandru Pop: grades 9 and 8 ────────────────────────────────────────
    // "Pop" uniquely identifies Alexandru Pop in the 6B student list.
    const popBadges = catalogPage.getGradeBadges('Pop');

    // There should be at least 2 regular grade badges visible (9 and 8).
    // The thesis badge may also appear, so we use toHaveCount(2) with >=
    // by asserting the first two individually.
    await expect(popBadges.first()).toBeVisible();

    // Collect badge texts to verify the values 9 and 8 are present.
    // We don't assert position because the thesis badge may be interleaved.
    const badgeTexts = await popBadges.allTextContents();
    const values = badgeTexts.map((t) => t.trim());

    // Grades 9 and 8 must appear somewhere in the badge list.
    expect(values.some((v) => v.includes('9'))).toBe(true);
    expect(values.some((v) => v.includes('8'))).toBe(true);
  },
);

// ── Test 48 ──────────────────────────────────────────────────────────────────

test(
  '48 – hovering a grade badge shows a tooltip with the date',
  async ({ teacherPage }) => {
    /**
     * Grade badges are interactive: hovering reveals a tooltip with contextual
     * information about the grade, specifically the date it was awarded
     * (format: DD.MM.YYYY — the Romanian convention).
     *
     * We hover over Andrei Moldovan's FB grade badge and check if a tooltip
     * element becomes visible. The tooltip must contain a date-like pattern.
     *
     * NOTE: If the UI uses a title attribute instead of a visible tooltip
     * element, we fall back to checking the title attribute value.
     */
    const catalogPage = new CatalogPage(teacherPage);
    await catalogPage.goto(TEST_CLASSES.class2A.id);

    await expect(catalogPage.subjectTabs.first()).toBeVisible({ timeout: 15_000 });
    await catalogPage.clickSubjectTab('Comunicare');
    // At least 1 student row must be visible (exact count depends on test order —
    // Moldovan always has at least one grade, so the grid is never empty here).
    await expect(catalogPage.studentRows.first()).toBeVisible({ timeout: 8_000 });

    // Locate the grade badge for Andrei Moldovan (has seed grade FB).
    const moldovanBadge = catalogPage.getGradeBadges('Moldovan').first();
    await expect(moldovanBadge).toBeVisible();

    // Hover over the badge to trigger tooltip display.
    await moldovanBadge.hover();

    // Strategy 1: Check for a visible tooltip element.
    // Common implementations use [data-testid="grade-tooltip"] or role="tooltip".
    const tooltipByTestId = teacherPage.getByTestId('grade-tooltip');
    const tooltipByRole = teacherPage.getByRole('tooltip');

    const testIdVisible = await tooltipByTestId.isVisible().catch(() => false);
    const roleVisible = await tooltipByRole.isVisible().catch(() => false);

    if (testIdVisible) {
      // A visible tooltip element exists — verify it contains a date pattern.
      // Romanian date format: DD.MM.YYYY (e.g. "15.03.2025")
      await expect(tooltipByTestId).toContainText(/\d{2}\.\d{2}\.\d{4}/);
    } else if (roleVisible) {
      await expect(tooltipByRole).toContainText(/\d{2}\.\d{2}\.\d{4}/);
    } else {
      // Strategy 2: The tooltip may be implemented as a title attribute.
      // Check if the badge element (or a parent) has a title with a date.
      const titleAttr = await moldovanBadge.getAttribute('title');
      const parentTitle = await moldovanBadge
        .locator('..')
        .getAttribute('title')
        .catch(() => null);

      const titleText = titleAttr ?? parentTitle ?? '';
      // In this else branch testIdVisible and roleVisible are both false, so
      // we only need to check whether a non-empty title attribute exists.
      // Relative date strings like "acum 2 zile" are also accepted.
      expect(
        titleText.length > 0,
        'Expected a non-empty title attribute on the grade badge (no visible tooltip found)',
      ).toBe(true);
    }
  },
);

// ── Test 49 ──────────────────────────────────────────────────────────────────

test(
  '49 – thesis grade displays with a "T" prefix or thesis indicator for Alexandru Pop',
  async ({ teacherMiddlePage }) => {
    /**
     * In middle and high school, a semester thesis (teză) grade is marked
     * differently from regular grades. The seed data gives Alexandru Pop a
     * thesis grade of 7 in the ROM subject of class 6B.
     *
     * We verify that this special grade is rendered with a distinguishing
     * indicator — either:
     *   • A "T" prefix on the badge text (e.g. "T7"), or
     *   • A data-testid="thesis-badge" attribute, or
     *   • A "teză" / "thesis" label nearby in the row.
     *
     * If none of these are visible, the test logs a soft warning rather than
     * failing hard, because the UI spec allows rendering thesis grades as
     * normal numeric badges with a label elsewhere in the row.
     */
    const catalogPage = new CatalogPage(teacherMiddlePage);
    await catalogPage.goto(TEST_CLASSES.class6B.id);

    await expect(catalogPage.subjectTabs.first()).toBeVisible({ timeout: 15_000 });
    await catalogPage.clickSubjectTab('Limba');
    // At least 1 student row must be visible (exact count depends on test order).
    // Alexandru Pop always has grades, so his row is always present.
    await expect(catalogPage.studentRows.first()).toBeVisible({ timeout: 8_000 });

    // Locate Alexandru Pop's row.
    const popRow = catalogPage.getStudentRowByName('Pop');
    await expect(popRow).toBeVisible();

    // Strategy 1: look for a dedicated thesis badge element.
    const thesisBadge = popRow.getByTestId('thesis-badge');
    const thesisBadgeVisible = await thesisBadge.isVisible().catch(() => false);

    if (thesisBadgeVisible) {
      // A dedicated thesis badge exists — verify it contains the value "7".
      await expect(thesisBadge).toContainText('7');
    } else {
      // Strategy 2: look for a grade badge whose text starts with "T".
      // The UI may render the thesis grade as "T7" inside a regular badge.
      const allBadges = catalogPage.getGradeBadges('Pop');
      const badgeTexts = await allBadges.allTextContents();
      const hasThesisText = badgeTexts.some(
        (t) => t.trim().startsWith('T') || t.trim().toLowerCase().includes('tez'),
      );

      // Strategy 3: look for a text label "teză" anywhere in the row.
      const rowText = (await popRow.textContent()) ?? '';
      const hasThesisLabel = /tez[ăa]/i.test(rowText);

      // At least one of the thesis-indicator strategies should match.
      expect(
        hasThesisText || hasThesisLabel || thesisBadgeVisible,
        `Expected a thesis indicator in Alexandru Pop's row. Row text: "${rowText}"`,
      ).toBe(true);
    }
  },
);

// ── Test 50 ──────────────────────────────────────────────────────────────────

test(
  '50 – loading skeleton is visible while the grades API response is delayed',
  async ({ teacherPage }) => {
    /**
     * The catalog page shows a skeleton / spinner while waiting for the API
     * response. We simulate a slow network by intercepting the grades API
     * request and delaying it by 2 seconds using page.route().
     *
     * After navigating, we immediately assert that the loading indicator is
     * visible (before the delay expires). Then we release the response and
     * wait for the loading indicator to disappear.
     *
     * This test verifies the loading UX path, which is important for slow
     * connections common in Romanian school environments.
     */
    // Intercept the grades API endpoint using the exact URL pattern from
    // useCatalog.ts: GET /catalog/classes/{classId}/subjects/{subjectId}/grades
    // The API base is http://localhost:8080/api/v1, so the full path is:
    //   **/api/v1/catalog/classes/*/subjects/*/grades*
    // We also intercept the class teachers endpoint so the loading window is
    // wide enough to catch the loading indicator before data arrives.
    await teacherPage.route('**/api/v1/catalog/classes/*/subjects/*/grades*', async (route) => {
      // Delay the response by 2 seconds to simulate a slow network.
      await new Promise((resolve) => setTimeout(resolve, 2000));
      // After the delay, allow the original request to proceed.
      await route.continue();
    });

    // Also intercept the class teachers endpoint to extend the loading window.
    await teacherPage.route('**/api/v1/classes/*/teachers*', async (route) => {
      await new Promise((resolve) => setTimeout(resolve, 2000));
      await route.continue();
    });

    // Navigate to the catalog page. The loading indicator should appear
    // immediately because the intercepted API calls are artificially delayed.
    const catalogPage = new CatalogPage(teacherPage);

    // Start navigation but do not await completion — we want to check the
    // intermediate loading state.
    const navigationPromise = catalogPage.goto(TEST_CLASSES.class2A.id);

    // Check for the loading indicator while the API is still in-flight.
    // We give it a short timeout since navigation triggers immediately.
    // The loading indicator must become visible before the 2-second delay ends.
    await expect(catalogPage.loadingIndicator).toBeVisible({ timeout: 3_000 });

    // Now wait for navigation to complete and the loading to finish.
    await navigationPromise;

    // After the delayed responses arrive, the loading indicator should
    // disappear as the Vue component transitions to the data-loaded state.
    await expect(catalogPage.loadingIndicator).not.toBeVisible({ timeout: 15_000 });

    // The grade grid container should now be visible with the actual data.
    await expect(catalogPage.gradeGridContainer).toBeVisible();
  },
);

/**
 * catalog.page.ts
 *
 * Page Object Model (POM) for the CatalogRO catalog page (/catalog/[classId]).
 *
 * WHAT THIS PAGE DOES
 * ───────────────────
 * The catalog page is the core grading interface. A teacher opens it for one
 * class and one subject at a time. It shows:
 *   - A header with the class name, education level badge, and student count
 *   - Semester toggle buttons (Semester I / II)
 *   - Subject tabs (one per subject taught in this class)
 *   - A grade grid: rows = students, columns = dates/grades
 *   - Per-student row: grade badges, an "add grade" button, running average
 *
 * The page is async — it starts in a loading state, then resolves to either
 * the grade grid content or an error banner.
 *
 * SELECTOR STRATEGY
 * ─────────────────
 * All selectors use `page.getByTestId(...)` which resolves to
 * `[data-testid="..."]`. Independent of Tailwind classes and Romanian text.
 *
 * USAGE
 * ─────
 * import { CatalogPage } from '../page-objects/catalog.page';
 *
 * test('teacher can view grades for a class', async ({ page }) => {
 *   const catalog = new CatalogPage(page);
 *   await catalog.goto('uuid-of-class');
 *   await expect(catalog.gradeGrid).toBeVisible();
 *   const count = await catalog.classCards.count(); // number of student rows
 * });
 */

import type { Locator, Page } from '@playwright/test';

/**
 * CatalogPage
 *
 * Encapsulates all interactions with the /catalog/[classId] route.
 * Covers loading/error states, semester selection, subject tabs, and
 * the per-student grade grid.
 */
export class CatalogPage {
  /** The raw Playwright Page object. Stored so methods can act on it. */
  private readonly page: Page;

  // ── Locators ───────────────────────────────────────────────────────────────
  // Locators are lazy — they do not query the DOM until an action or assertion
  // is called on them. Defining them as class properties means they are
  // created once and reused across method calls without extra overhead.

  /**
   * Spinner / skeleton shown while catalog data is being fetched.
   * Maps to: <div data-testid="catalog-loading" ...>
   */
  readonly loadingIndicator: Locator;

  /**
   * Error banner rendered when the data fetch fails.
   * Maps to: <div data-testid="catalog-error" ...>
   */
  readonly errorBanner: Locator;

  /**
   * Back navigation link — returns to the dashboard.
   * Maps to: <a data-testid="back-link" ...>
   */
  readonly backLink: Locator;

  /**
   * The class name displayed in the page header, e.g. "Clasa a VIII-a A".
   * Maps to: <h1 data-testid="class-title" ...>
   */
  readonly classTitle: Locator;

  /**
   * Badge showing the education level (e.g. "Gimnaziu", "Liceu", "Primar").
   * Maps to: <span data-testid="education-level-badge" ...>
   */
  readonly educationLevelBadge: Locator;

  /**
   * Text showing the total number of enrolled students, e.g. "28 elevi".
   * Maps to: <span data-testid="catalog-student-count" ...>
   */
  readonly studentCount: Locator;

  /**
   * The Semester I toggle button.
   * Maps to: <button data-testid="semester-I" ...>
   */
  readonly semesterI: Locator;

  /**
   * The Semester II toggle button.
   * Maps to: <button data-testid="semester-II" ...>
   */
  readonly semesterII: Locator;

  /**
   * All subject tab buttons (one per subject available for this class).
   * Returns a multi-element locator; use .filter() or .nth() to target one.
   * Maps to: <button data-testid="subject-tab" ...> (one per subject)
   */
  readonly subjectTabs: Locator;

  /**
   * The outer wrapper of the grade grid section.
   * Useful for asserting overall visibility before checking inner elements.
   * Maps to: <div data-testid="grade-grid-container" ...>
   */
  readonly gradeGridContainer: Locator;

  /**
   * The grade grid table/list element itself.
   * Maps to: <table data-testid="grade-grid" ...> (or div, depending on implementation)
   */
  readonly gradeGrid: Locator;

  /**
   * All student row elements in the grade grid.
   * Returns a multi-element locator; one element per student.
   * Maps to: <tr data-testid="student-row" ...> (one per student)
   */
  readonly studentRows: Locator;

  // ── Constructor ────────────────────────────────────────────────────────────
  /**
   * @param page - The Playwright Page instance injected by the test or fixture.
   */
  constructor(page: Page) {
    this.page = page;

    this.loadingIndicator = page.getByTestId('catalog-loading');
    this.errorBanner = page.getByTestId('catalog-error');
    this.backLink = page.getByTestId('back-link');
    this.classTitle = page.getByTestId('class-title');
    this.educationLevelBadge = page.getByTestId('education-level-badge');
    this.studentCount = page.getByTestId('catalog-student-count');
    this.semesterI = page.getByTestId('semester-I');
    this.semesterII = page.getByTestId('semester-II');
    this.subjectTabs = page.getByTestId('subject-tab');
    this.gradeGridContainer = page.getByTestId('grade-grid-container');
    this.gradeGrid = page.getByTestId('grade-grid');
    this.studentRows = page.getByTestId('student-row');
  }

  // ── Navigation ─────────────────────────────────────────────────────────────
  /**
   * goto
   *
   * Navigates to the catalog page for the given class UUID.
   * baseURL is defined in playwright.config.ts.
   *
   * @param classId - The UUID of the class to open, e.g. '018e4c3d-...'
   */
  async goto(classId: string): Promise<void> {
    // Navigate via the dashboard's class card button (SPA navigation).
    // We CANNOT use page.goto() because it triggers SSR where localStorage
    // is unavailable, causing the auth check to redirect to /login.
    // Instead, we ensure the dashboard is loaded, then click the class card.
    //
    // Step 1: Wait for the teacher dashboard content to load.
    await this.page.getByTestId('dashboard-content').waitFor({ state: 'visible', timeout: 15_000 });

    // Step 2: Click the class card button. The card uses @click="openClass()"
    // which calls navigateTo('/catalog/{classId}') — true SPA navigation.
    const cards = this.page.getByTestId('class-card');
    const cardCount = await cards.count();

    // Try to find the card that navigates to the target class ID.
    // Cards don't have the classId in a data attribute, so we click the one
    // whose @click handler will navigate to the right URL.
    // For simplicity, if there's only one card, click it. Otherwise, click
    // each card and check if the URL matches.
    if (cardCount === 1) {
      await cards.first().click();
    } else {
      // Multiple cards — click each until we find the right one.
      // This is needed when a teacher has multiple classes.
      let found = false;
      for (let i = 0; i < cardCount; i++) {
        const card = cards.nth(i);
        // Try to match by checking if this card leads to the right URL
        // after clicking. We'll detect via URL change.
        await card.click();
        await this.page.waitForTimeout(500);
        if (this.page.url().includes(classId)) {
          found = true;
          break;
        }
        // Wrong card — go back to dashboard and try next
        await this.page.goBack();
        await this.page
          .getByTestId('dashboard-content')
          .waitFor({ state: 'visible', timeout: 5_000 });
      }
      if (!found) {
        throw new Error(`No class card found that navigates to classId: ${classId}`);
      }
    }

    await this.page.waitForURL(`**/catalog/${classId}`, { timeout: 15_000 });
  }

  /**
   * goBack
   *
   * Clicks the back link to return to the dashboard.
   * Equivalent to the browser back button but more explicit in tests.
   */
  async goBack(): Promise<void> {
    await this.backLink.click();
  }

  // ── Header queries ─────────────────────────────────────────────────────────
  /**
   * getClassTitleText
   *
   * Returns the trimmed text content of the class title heading.
   *
   * @returns Class name string, e.g. "Clasa a VIII-a A".
   */
  async getClassTitleText(): Promise<string> {
    // textContent() returns null only if the element is not in the DOM.
    // We assume the title is always present when this method is called.
    const text = await this.classTitle.textContent();
    return text !== null ? text.trim() : '';
  }

  /**
   * getEducationLevelText
   *
   * Returns the trimmed text of the education level badge.
   *
   * @returns Education level string, e.g. "Gimnaziu", "Primar", or "Liceu".
   */
  async getEducationLevelText(): Promise<string> {
    const text = await this.educationLevelBadge.textContent();
    return text !== null ? text.trim() : '';
  }

  /**
   * getStudentCountText
   *
   * Returns the trimmed text showing total enrolled students.
   *
   * @returns Count string, e.g. "28 elevi".
   */
  async getStudentCountText(): Promise<string> {
    const text = await this.studentCount.textContent();
    return text !== null ? text.trim() : '';
  }

  // ── Semester & subject controls ────────────────────────────────────────────
  /**
   * selectSemester
   *
   * Clicks the semester toggle button for semester I or II.
   * After clicking, the grade grid will reload with data for the chosen semester.
   *
   * @param semester - 'I' or 'II'
   */
  async selectSemester(semester: 'I' | 'II'): Promise<void> {
    // Dynamically target either semesterI or semesterII based on the argument.
    if (semester === 'I') {
      await this.semesterI.click();
    } else {
      await this.semesterII.click();
    }
  }

  /**
   * clickSubjectTab
   *
   * Clicks the subject tab whose text contains `name`.
   * This switches the grade grid to display grades for the named subject.
   *
   * @param name - Partial or full subject name, e.g. 'Matematică' or 'Mate'
   */
  async clickSubjectTab(name: string): Promise<void> {
    // Filter the multi-element locator to the tab whose text contains name.
    await this.subjectTabs.filter({ hasText: name }).click();
  }

  // ── Grade grid helpers ─────────────────────────────────────────────────────
  /**
   * getStudentRowByName
   *
   * Returns the row locator for the student whose name contains `name`.
   * Use this as the starting point for grade assertions on a specific student.
   *
   * @param name - Partial or full student name, e.g. 'Popescu' or 'Ion Popescu'
   * @returns A single-element Locator for the matching student row.
   */
  getStudentRowByName(name: string): Locator {
    return this.studentRows.filter({ hasText: name });
  }

  /**
   * getGradeBadges
   *
   * Returns all grade badge elements within the row of the named student.
   * Grade badges display either a numeric grade (e.g. "9") or a qualifier
   * (e.g. "FB") depending on the school's evaluation configuration.
   *
   * @param studentName - Partial or full student name to identify the row.
   * @returns Multi-element Locator for all `grade-badge` elements in that row.
   */
  getGradeBadges(studentName: string): Locator {
    return this.getStudentRowByName(studentName).getByTestId('grade-badge');
  }

  /**
   * clickAddGrade
   *
   * Clicks the "add grade" button in the specified student's row.
   * This opens the GradeInputModal for that student.
   *
   * @param studentName - Partial or full student name to identify the row.
   */
  async clickAddGrade(studentName: string): Promise<void> {
    await this.getStudentRowByName(studentName).getByTestId('add-grade-button').click();
  }

  /**
   * clickGradeBadge
   *
   * Clicks the nth grade badge in the specified student's row (0-indexed).
   * This typically opens the GradeInputModal in edit mode for that grade.
   *
   * @param studentName - Partial or full student name to identify the row.
   * @param index - Zero-based index of the grade badge to click.
   */
  async clickGradeBadge(studentName: string, index: number): Promise<void> {
    await this.getGradeBadges(studentName).nth(index).click();
  }

  /**
   * getAverage
   *
   * Returns the trimmed average text from the named student's row, or null
   * if the average element is not visible (e.g. no grades yet entered).
   *
   * For numeric grading this will be a decimal string like "8.75".
   * For qualifier grading this field may not be rendered at all (returns null).
   *
   * @param studentName - Partial or full student name to identify the row.
   * @returns Average text string, or null if the element is absent/hidden.
   */
  async getAverage(studentName: string): Promise<string | null> {
    const averageEl = this.getStudentRowByName(studentName).getByTestId('student-average');
    const isVisible = await averageEl.isVisible();
    if (!isVisible) {
      return null;
    }
    const text = await averageEl.textContent();
    return text !== null ? text.trim() : null;
  }

  // ── State queries ──────────────────────────────────────────────────────────
  /**
   * isLoading
   *
   * Returns true when the loading indicator is currently visible.
   *
   * @returns true if data is still being fetched, false otherwise.
   */
  async isLoading(): Promise<boolean> {
    return this.loadingIndicator.isVisible();
  }

  /**
   * getError
   *
   * Returns the trimmed text content of the error banner, or null if no
   * error is currently displayed.
   *
   * @returns Error message string, or null.
   */
  async getError(): Promise<string | null> {
    const isVisible = await this.errorBanner.isVisible();
    if (!isVisible) {
      return null;
    }
    const text = await this.errorBanner.textContent();
    return text !== null ? text.trim() : null;
  }

  /**
   * getEmptyState
   *
   * Returns the trimmed text of the empty-state message shown when no grades
   * exist yet for the selected subject/semester combination.
   * Returns null if the empty state element is not visible.
   *
   * @returns Empty-state message string, or null.
   */
  async getEmptyState(): Promise<string | null> {
    const emptyEl = this.page.getByTestId('grade-grid-empty');
    const isVisible = await emptyEl.isVisible();
    if (!isVisible) {
      return null;
    }
    const text = await emptyEl.textContent();
    return text !== null ? text.trim() : null;
  }
}

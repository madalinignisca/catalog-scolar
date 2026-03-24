/**
 * dashboard.page.ts
 *
 * Page Object Model (POM) for the CatalogRO dashboard page (/).
 *
 * WHAT THIS PAGE DOES
 * ───────────────────
 * The dashboard is the landing page after a successful login. It shows:
 *   - A personalised welcome message with the logged-in user's name/role
 *   - A grid of "class cards" (for teachers/admins who own classes)
 *   - A grid of "admin cards" (shortcuts to admin-only sections)
 *
 * The page fetches data asynchronously on mount, so it can be in one of
 * three states: loading → content | error. The locators and methods below
 * cover all three states.
 *
 * SELECTOR STRATEGY
 * ─────────────────
 * All selectors use `page.getByTestId(...)` which resolves to
 * `[data-testid="..."]`. These are immune to Tailwind class churn and
 * independent of Romanian text content.
 *
 * USAGE
 * ─────
 * import { DashboardPage } from '../page-objects/dashboard.page';
 *
 * test('teacher sees their class on dashboard', async ({ page }) => {
 *   const dashboard = new DashboardPage(page);
 *   // (assume already logged in via fixture)
 *   await page.goto('/');
 *   const card = dashboard.getClassCardByName('Clasa a VIII-a A');
 *   await expect(card).toBeVisible();
 * });
 */

import type { Locator, Page } from '@playwright/test';

/**
 * DashboardPage
 *
 * Encapsulates all interactions with the root route ('/') dashboard.
 * Covers loading state, error state, class cards, and admin cards.
 */
export class DashboardPage {
  /** The raw Playwright Page object. Stored so methods can act on it. */
  private readonly page: Page;

  // ── Locators ───────────────────────────────────────────────────────────────
  // Locators are lazy — they do not query the DOM until an action or assertion
  // is called on them. Defining them as class properties means they are
  // created once and reused across method calls without extra overhead.

  /**
   * Spinner / skeleton shown while dashboard data is being fetched.
   * Maps to: <div data-testid="dashboard-loading" ...>
   * NOTE: Only present while the async fetch is in-flight (v-if).
   */
  readonly loadingIndicator: Locator;

  /**
   * Error banner rendered when the data fetch fails (network error, 403, etc.).
   * Maps to: <div data-testid="dashboard-error" ...>
   * NOTE: Only present when `error` ref is non-null (v-if).
   */
  readonly errorBanner: Locator;

  /**
   * The main content area that wraps class cards and admin cards.
   * Visible only after a successful data fetch.
   * Maps to: <div data-testid="dashboard-content" ...>
   */
  readonly content: Locator;

  /**
   * All class card elements on the dashboard. Returns a multi-element locator
   * (may match 0–N elements depending on the logged-in user's classes).
   * Maps to: <div data-testid="class-card" ...> (one per class)
   */
  readonly classCards: Locator;

  /**
   * All admin shortcut cards. Only rendered for admin/secretary roles.
   * Maps to: <div data-testid="admin-card" ...> (one per admin action)
   */
  readonly adminCards: Locator;

  /**
   * Personalised greeting shown at the top of the dashboard,
   * e.g. "Bun venit, Prof. Ionescu!".
   * Maps to: <p data-testid="welcome-message" ...>
   */
  readonly welcomeMessage: Locator;

  // ── Constructor ────────────────────────────────────────────────────────────
  /**
   * @param page - The Playwright Page instance injected by the test or fixture.
   *               Each test gets its own isolated browser context, so there is
   *               no state leakage between tests.
   */
  constructor(page: Page) {
    this.page = page;

    // Initialise locators. getByTestId('x') is shorthand for
    // page.locator('[data-testid="x"]').
    this.loadingIndicator = page.getByTestId('dashboard-loading');
    this.errorBanner = page.getByTestId('dashboard-error');
    this.content = page.getByTestId('dashboard-content');
    this.classCards = page.getByTestId('class-card');
    this.adminCards = page.getByTestId('admin-card');
    this.welcomeMessage = page.getByTestId('welcome-message');
  }

  // ── Locator helpers ────────────────────────────────────────────────────────
  /**
   * getClassCardByName
   *
   * Filters the `classCards` multi-element locator to find the single card
   * whose visible text contains `name`. This lets tests target a specific
   * class without knowing its position in the list.
   *
   * @param name - Partial or full class name, e.g. 'VIII-a A' or 'Clasa a VIII-a A'
   * @returns A single-element Locator for the matching class card.
   *
   * @example
   *   const card = dashboard.getClassCardByName('VIII-a A');
   *   await expect(card).toBeVisible();
   */
  getClassCardByName(name: string): Locator {
    // Playwright's .filter({ hasText }) narrows a multi-element locator
    // to only those elements whose text content contains the given string.
    return this.classCards.filter({ hasText: name });
  }

  /**
   * getClassCardName
   *
   * Returns the locator for the class name element *inside* a specific card.
   * Pass the card locator obtained from `getClassCardByName()` or by indexing
   * `classCards` with `.nth(n)`.
   *
   * @param card - A single-element Locator for the class card container.
   * @returns Locator for the `class-card-name` element within that card.
   *
   * @example
   *   const card = dashboard.classCards.nth(0);
   *   const name = dashboard.getClassCardName(card);
   *   await expect(name).toHaveText('Clasa a V-a B');
   */
  getClassCardName(card: Locator): Locator {
    // Scoped locator: searches only within the given card element.
    return card.getByTestId('class-card-name');
  }

  /**
   * getClassCardStudentCount
   *
   * Returns the locator for the student-count element inside a specific card.
   * This element typically shows text like "24 elevi".
   *
   * @param card - A single-element Locator for the class card container.
   * @returns Locator for the `student-count` element within that card.
   */
  getClassCardStudentCount(card: Locator): Locator {
    // Scoped lookup — avoids false matches from other student-count elements
    // on the page if multiple cards are rendered.
    // Template uses data-testid="class-card-student-count" (not "student-count").
    return card.getByTestId('class-card-student-count');
  }

  // ── Interaction ────────────────────────────────────────────────────────────
  /**
   * clickClassCard
   *
   * Finds the class card whose text contains `name` and clicks it,
   * navigating to the catalog page for that class.
   *
   * @param name - Partial or full class name string to locate the card.
   */
  async clickClassCard(name: string): Promise<void> {
    // Re-uses getClassCardByName to find the card, then clicks it.
    await this.getClassCardByName(name).click();
  }

  // ── State queries ──────────────────────────────────────────────────────────
  /**
   * isLoading
   *
   * Returns true when the loading indicator is currently visible,
   * meaning the dashboard data fetch is still in progress.
   *
   * Use this to wait for or assert the loading state in tests.
   *
   * @returns true if the loading indicator is visible, false otherwise.
   */
  async isLoading(): Promise<boolean> {
    return this.loadingIndicator.isVisible();
  }

  /**
   * getError
   *
   * Returns the trimmed text content of the error banner, or null if the
   * error banner is not currently rendered.
   *
   * @returns Error message string, or null if no error is displayed.
   */
  async getError(): Promise<string | null> {
    // isVisible() returns false if the element is absent from the DOM
    // (e.g. when rendered with v-if="error" and error is null).
    const isVisible = await this.errorBanner.isVisible();
    if (!isVisible) {
      return null;
    }
    const text = await this.errorBanner.textContent();
    return text !== null ? text.trim() : null;
  }

  /**
   * getWelcomeText
   *
   * Returns the trimmed text content of the welcome message, or null if
   * the element is not visible (e.g. during loading or on error state).
   *
   * @returns Welcome message string (e.g. "Bun venit, Prof. Ionescu!"), or null.
   */
  async getWelcomeText(): Promise<string | null> {
    const isVisible = await this.welcomeMessage.isVisible();
    if (!isVisible) {
      return null;
    }
    const text = await this.welcomeMessage.textContent();
    return text !== null ? text.trim() : null;
  }
}

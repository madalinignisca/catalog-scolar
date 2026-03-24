/**
 * layout.page.ts
 *
 * Page Object Model (POM) for the CatalogRO app shell / layout component.
 *
 * WHAT THIS COMPONENT DOES
 * ────────────────────────
 * The layout wraps every authenticated page. It provides:
 *   - A sidebar with navigation links (desktop)
 *   - A hamburger button that opens the sidebar as a drawer (mobile)
 *   - An overlay that closes the sidebar on mobile when clicked
 *   - A user info panel: school name, logged-in user's name and role
 *   - A logout button
 *   - A sync status indicator showing whether offline changes are pending
 *
 * Because the layout is present on EVERY authenticated route, tests for any
 * page can use this POM to assert navigation state, current user identity,
 * or trigger a logout.
 *
 * SELECTOR STRATEGY
 * ─────────────────
 * All selectors use `page.getByTestId(...)` which resolves to
 * `[data-testid="..."]`. Independent of Tailwind utility classes and
 * Romanian text content.
 *
 * USAGE
 * ─────
 * import { LayoutPage } from '../page-objects/layout.page';
 *
 * test('logged-in user sees their name in the sidebar', async ({ page }) => {
 *   const layout = new LayoutPage(page);
 *   const name = await layout.getUserNameText();
 *   expect(name).toBe('Prof. Ionescu');
 * });
 *
 * test('mobile: hamburger opens sidebar', async ({ page }) => {
 *   await page.setViewportSize({ width: 375, height: 812 });
 *   const layout = new LayoutPage(page);
 *   expect(await layout.isSidebarVisible()).toBe(false);
 *   await layout.openMobileMenu();
 *   expect(await layout.isSidebarVisible()).toBe(true);
 * });
 */

import type { Locator, Page } from '@playwright/test';

/**
 * LayoutPage
 *
 * Encapsulates all interactions with the authenticated app shell layout.
 * Works on any route that renders inside the default Nuxt layout.
 */
export class LayoutPage {
  /** The raw Playwright Page object. Stored so methods can act on it. */
  private readonly page: Page;

  // ── Locators ───────────────────────────────────────────────────────────────
  // Locators are lazy — they do not query the DOM until an action or assertion
  // is called on them.

  /**
   * The sidebar navigation container.
   * On desktop it is always visible; on mobile it slides in as a drawer.
   * Maps to: <nav data-testid="sidebar" ...>
   */
  readonly sidebar: Locator;

  /**
   * Semi-transparent overlay rendered behind the sidebar on mobile.
   * Clicking it closes the sidebar drawer.
   * Maps to: <div data-testid="sidebar-overlay" ...>
   * NOTE: Only rendered when the mobile sidebar is open (v-if / v-show).
   */
  readonly sidebarOverlay: Locator;

  /**
   * All navigation link items inside the sidebar.
   * Returns a multi-element locator; one element per route link.
   * Maps to: <a data-testid="nav-item" ...> (one per navigation link)
   */
  readonly navItems: Locator;

  /**
   * The school name displayed in the sidebar header.
   * Populated from the JWT `school_name` claim after login.
   * Maps to: <span data-testid="school-name" ...>
   */
  readonly schoolName: Locator;

  /**
   * The logged-in user's display name.
   * Maps to: <span data-testid="user-name" ...>
   */
  readonly userName: Locator;

  /**
   * The logged-in user's role label (e.g. "Profesor", "Administrator",
   * "Secretar", "Elev", "Părinte").
   * Maps to: <span data-testid="user-role" ...>
   */
  readonly userRole: Locator;

  /**
   * The logout button. Clicking it invalidates the session and redirects
   * the user to /login.
   * Maps to: <button data-testid="logout-button" ...>
   */
  readonly logoutButton: Locator;

  /**
   * The hamburger / mobile menu toggle button. Only visible on small screens.
   * Maps to: <button data-testid="mobile-menu-button" ...>
   */
  readonly mobileMenuButton: Locator;

  /**
   * The sync status icon/badge container. Shows whether there are unsynced
   * offline changes pending in the local IndexedDB queue.
   * Maps to: <div data-testid="sync-status" ...>
   */
  readonly syncStatus: Locator;

  /**
   * The human-readable sync status label, e.g. "Sincronizat", "Se sincronizează…",
   * or "3 modificări în așteptare".
   * Maps to: <span data-testid="sync-status-label" ...>
   */
  readonly syncStatusLabel: Locator;

  // ── Constructor ────────────────────────────────────────────────────────────
  /**
   * @param page - The Playwright Page instance injected by the test or fixture.
   */
  constructor(page: Page) {
    this.page = page;

    this.sidebar = page.getByTestId('sidebar');
    this.sidebarOverlay = page.getByTestId('sidebar-overlay');
    this.navItems = page.getByTestId('nav-item');
    this.schoolName = page.getByTestId('school-name');
    this.userName = page.getByTestId('user-name');
    this.userRole = page.getByTestId('user-role');
    this.logoutButton = page.getByTestId('logout-button');
    this.mobileMenuButton = page.getByTestId('mobile-menu-button');
    this.syncStatus = page.getByTestId('sync-status');
    this.syncStatusLabel = page.getByTestId('sync-status-label');
  }

  // ── Navigation queries ─────────────────────────────────────────────────────
  /**
   * getSidebarItemTexts
   *
   * Returns the trimmed text content of every navigation item in the sidebar.
   * Useful for asserting that the correct set of links is shown for a given role
   * (e.g. admins see more items than students).
   *
   * @returns Array of nav item label strings, in DOM order.
   *
   * @example
   *   const items = await layout.getSidebarItemTexts();
   *   expect(items).toContain('Catalog');
   *   expect(items).toContain('Absențe');
   */
  async getSidebarItemTexts(): Promise<string[]> {
    // allTextContents() collects textContent from every matched element in
    // one round-trip, then we trim each entry to remove whitespace padding.
    const texts = await this.navItems.allTextContents();
    return texts.map((t) => t.trim());
  }

  /**
   * getActiveNavItem
   *
   * Returns the trimmed text of the currently active/highlighted navigation
   * item, or null if no item is marked as active.
   *
   * Active state is detected by checking for:
   *   - aria-current="page" attribute (accessibility-correct pattern)
   *   - OR an "active" CSS class (fallback for class-based highlighting)
   *
   * @returns Active nav item label, or null if none is active.
   */
  async getActiveNavItem(): Promise<string | null> {
    // Try aria-current first — this is the accessible, recommended approach.
    const ariaActive = this.navItems.filter({ has: this.page.locator('[aria-current="page"]') });
    if ((await ariaActive.count()) > 0) {
      const text = await ariaActive.first().textContent();
      return text !== null ? text.trim() : null;
    }

    // Fallback: look for an element with an "active" class.
    const classActive = this.navItems.filter({ has: this.page.locator('.active') });
    if ((await classActive.count()) > 0) {
      const text = await classActive.first().textContent();
      return text !== null ? text.trim() : null;
    }

    // No active item found.
    return null;
  }

  /**
   * clickNavItem
   *
   * Clicks the navigation item whose text contains `label`.
   * This navigates the app to the corresponding route.
   *
   * @param label - Partial or full nav item label, e.g. 'Catalog', 'Absențe'.
   */
  async clickNavItem(label: string): Promise<void> {
    await this.navItems.filter({ hasText: label }).click();
  }

  // ── User identity queries ──────────────────────────────────────────────────
  /**
   * getUserNameText
   *
   * Returns the trimmed display name of the currently logged-in user.
   *
   * @returns User display name string, e.g. "Prof. Ionescu".
   */
  async getUserNameText(): Promise<string> {
    const text = await this.userName.textContent();
    return text !== null ? text.trim() : '';
  }

  /**
   * getUserRoleText
   *
   * Returns the trimmed role label of the currently logged-in user.
   *
   * @returns Role string, e.g. "Profesor", "Administrator", "Secretar".
   */
  async getUserRoleText(): Promise<string> {
    const text = await this.userRole.textContent();
    return text !== null ? text.trim() : '';
  }

  /**
   * getSchoolNameText
   *
   * Returns the trimmed school name shown in the sidebar.
   *
   * @returns School name string, e.g. "Școala Gimnazială nr. 5 Cluj".
   */
  async getSchoolNameText(): Promise<string> {
    const text = await this.schoolName.textContent();
    return text !== null ? text.trim() : '';
  }

  // ── Actions ────────────────────────────────────────────────────────────────
  /**
   * clickLogout
   *
   * Clicks the logout button.
   * After this call the session is invalidated, access/refresh tokens are
   * cleared from storage, and the user is redirected to /login.
   */
  async clickLogout(): Promise<void> {
    await this.logoutButton.click();
  }

  /**
   * isHamburgerVisible
   *
   * Returns true when the mobile hamburger button is visible on screen.
   * This is only true on small viewports (e.g. width < 768px in Tailwind's
   * default breakpoints) where the sidebar is hidden by default.
   *
   * @returns true if the hamburger button is visible, false otherwise.
   */
  async isHamburgerVisible(): Promise<boolean> {
    return this.mobileMenuButton.isVisible();
  }

  /**
   * openMobileMenu
   *
   * Clicks the hamburger button to open the sidebar drawer on mobile.
   * After this call the sidebar should become visible and the overlay
   * should be rendered.
   */
  async openMobileMenu(): Promise<void> {
    await this.mobileMenuButton.click();
  }

  /**
   * closeMobileMenu
   *
   * Clicks the sidebar overlay to close the mobile sidebar drawer.
   * This simulates a user tapping outside the menu to dismiss it — the
   * most common close gesture on touch devices.
   */
  async closeMobileMenu(): Promise<void> {
    await this.sidebarOverlay.click();
  }

  /**
   * isSidebarVisible
   *
   * Returns true when the sidebar is currently visible on screen.
   * On desktop this is always true; on mobile it is only true when the
   * drawer is open.
   *
   * @returns true if the sidebar element is visible, false otherwise.
   */
  async isSidebarVisible(): Promise<boolean> {
    return this.sidebar.isVisible();
  }

  // ── Sync status ────────────────────────────────────────────────────────────
  /**
   * getSyncStatusText
   *
   * Returns the trimmed text of the sync status label.
   * This reflects the current state of the Dexie.js offline sync queue:
   *   - "Sincronizat" — all changes are uploaded
   *   - "Se sincronizează…" — upload in progress
   *   - "N modificări în așteptare" — N pending offline mutations
   *
   * @returns Sync status label string.
   */
  async getSyncStatusText(): Promise<string> {
    const text = await this.syncStatusLabel.textContent();
    return text !== null ? text.trim() : '';
  }
}

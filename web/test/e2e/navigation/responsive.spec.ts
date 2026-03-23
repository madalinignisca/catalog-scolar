/**
 * navigation/responsive.spec.ts
 *
 * Tests 34–37: Mobile responsive behaviour of the sidebar drawer.
 *
 * WHAT WE TEST
 * ────────────
 * These tests verify that the sidebar navigation adapts correctly to a mobile
 * viewport (375 × 667 px — the canonical iPhone SE size used in Playwright
 * responsive testing):
 *   34 – On mobile, the sidebar is hidden and the hamburger button is visible.
 *   35 – Clicking the hamburger opens the sidebar drawer and shows an overlay.
 *   36 – Clicking the semi-transparent overlay closes the sidebar drawer.
 *   37 – Tapping a nav item inside the open mobile drawer navigates correctly.
 *
 * WHY MOBILE TESTS MATTER
 * ───────────────────────
 * CatalogRO is used daily by teachers on phones in Romanian classrooms with
 * limited desk space. If the mobile sidebar breaks, teachers cannot navigate
 * the app. These tests protect that critical code path.
 *
 * HOW THE MOBILE SIDEBAR WORKS
 * ─────────────────────────────
 * On small screens (Tailwind's default breakpoint: < 768 px):
 *   - The sidebar is hidden via CSS (translated off-screen or display:none).
 *   - A hamburger button ([data-testid="mobile-menu-button"]) appears in the
 *     top navigation bar.
 *   - Clicking the hamburger opens the sidebar as a drawer overlay.
 *   - A semi-transparent backdrop ([data-testid="sidebar-overlay"]) is rendered
 *     behind the drawer. Clicking it closes the drawer.
 *
 * VIEWPORT CONFIGURATION
 * ──────────────────────
 * test.use({ viewport: ... }) applies to all tests in this file.
 * 375 × 667 matches iPhone SE / common Android baseline.
 * Playwright resets this to the playwright.config.ts default between files.
 */

import { test, expect } from '../fixtures/auth.fixture';
import { LayoutPage } from '../page-objects/layout.page';

// ── Apply mobile viewport to every test in this file ─────────────────────────
//
// test.use() at the describe/file level is the recommended Playwright pattern
// for setting a shared viewport without repeating it in every beforeEach.
// This overrides the global viewport for all tests below.
test.use({
  viewport: { width: 375, height: 667 },
});

// ── Test 34 ────────────────────────────────────────────────────────────────────

test(
  '34 – mobile viewport hides the sidebar and shows the hamburger button',
  async ({ teacherPage }) => {
    /**
     * On a 375 px wide screen the desktop sidebar should not be visible —
     * it is either translated off-screen (transform: translateX(-100%)) or
     * hidden with display:none / visibility:hidden via Tailwind responsive classes.
     *
     * Playwright's isVisible() returns false for any of those CSS states,
     * which is the correct observable behaviour from a user's perspective.
     *
     * Conversely, the hamburger toggle button must be visible so the user has
     * a way to open the navigation menu.
     */
    const layout = new LayoutPage(teacherPage);

    // Wait for the page to fully render before checking visibility.
    // We wait for the mobile menu button specifically, as it is the
    // element that must appear on mobile. Allow 15 s for the fixture-based
    // login to complete and the page content to finish loading.
    await expect(layout.mobileMenuButton).toBeVisible({ timeout: 15_000 });

    // ── Sidebar should be hidden ──────────────────────────────────────────────
    // isSidebarVisible() calls Playwright's isVisible() on the sidebar locator.
    // At mobile widths the sidebar must NOT be visible before the menu is opened.
    const sidebarVisible = await layout.isSidebarVisible();
    expect(
      sidebarVisible,
      'Sidebar should be hidden on mobile before the menu is opened',
    ).toBe(false);

    // ── Hamburger must be visible ─────────────────────────────────────────────
    // isHamburgerVisible() calls isVisible() on [data-testid="mobile-menu-button"].
    const hamburgerVisible = await layout.isHamburgerVisible();
    expect(
      hamburgerVisible,
      'Hamburger button should be visible on mobile viewports',
    ).toBe(true);
  },
);

// ── Test 35 ────────────────────────────────────────────────────────────────────

test(
  '35 – clicking the hamburger button opens the sidebar drawer and backdrop',
  async ({ teacherPage }) => {
    /**
     * After the hamburger is clicked the sidebar slides in from the left and
     * a semi-transparent overlay is rendered behind it.
     *
     * We assert both:
     *   a) The sidebar itself becomes visible (the drawer is open).
     *   b) The overlay/backdrop is rendered (so the user can dismiss the menu
     *      by tapping outside it).
     *
     * openMobileMenu() is a LayoutPage helper that clicks the hamburger button.
     * After the click, Nuxt's v-if / CSS transition runs; we use toBeVisible()
     * which automatically waits for the element to appear (up to the default
     * Playwright timeout of 5 s).
     */
    const layout = new LayoutPage(teacherPage);

    // Precondition: sidebar is closed and hamburger is available.
    // Allow 15 s for the fixture-based login and initial page render to settle.
    await expect(layout.mobileMenuButton).toBeVisible({ timeout: 15_000 });

    // Open the mobile sidebar drawer.
    await layout.openMobileMenu();

    // ── Sidebar must now be visible ───────────────────────────────────────────
    await expect(
      layout.sidebar,
      'Sidebar drawer should be visible after opening',
    ).toBeVisible();

    // ── Backdrop/overlay must be rendered ─────────────────────────────────────
    // The overlay element ([data-testid="sidebar-overlay"]) is rendered via
    // v-if when the drawer is open, so toBeVisible() also confirms it was
    // inserted into the DOM, not just that it has a non-zero opacity.
    await expect(
      layout.sidebarOverlay,
      'Sidebar overlay/backdrop should be visible when the drawer is open',
    ).toBeVisible();
  },
);

// ── Test 36 ────────────────────────────────────────────────────────────────────

test(
  '36 – clicking the backdrop closes the mobile sidebar drawer',
  async ({ teacherPage }) => {
    /**
     * The semi-transparent overlay behind the open sidebar drawer doubles as a
     * "click outside to close" affordance — the most natural mobile gesture for
     * dismissing a drawer.
     *
     * Test flow:
     *   1. Open the sidebar via the hamburger button.
     *   2. Wait for the sidebar to be visible (drawer is fully open).
     *   3. Click the overlay via closeMobileMenu().
     *   4. Assert the sidebar is no longer visible.
     *
     * closeMobileMenu() clicks [data-testid="sidebar-overlay"]. After the
     * click, Nuxt removes or hides the drawer via v-if / CSS transition.
     */
    const layout = new LayoutPage(teacherPage);

    // ── Step 1: Open the drawer ───────────────────────────────────────────────
    // Allow 15 s for the fixture-based login and initial page render to settle.
    await expect(layout.mobileMenuButton).toBeVisible({ timeout: 15_000 });
    await layout.openMobileMenu();

    // ── Step 2: Wait for the drawer to be fully open ──────────────────────────
    // We wait explicitly so we are not racing the open animation.
    await expect(layout.sidebar).toBeVisible();

    // ── Step 3: Click the overlay to close ───────────────────────────────────
    await layout.closeMobileMenu();

    // ── Step 4: Sidebar must be hidden again ──────────────────────────────────
    // toBeHidden() is the inverse of toBeVisible() and waits for the element
    // to disappear (transition out) rather than asserting synchronously.
    await expect(
      layout.sidebar,
      'Sidebar drawer should be hidden after clicking the overlay',
    ).toBeHidden();
  },
);

// ── Test 37 ────────────────────────────────────────────────────────────────────

test(
  '37 – tapping a nav item in the mobile drawer navigates to the correct route',
  async ({ teacherPage }) => {
    /**
     * This test verifies the full mobile navigation flow:
     *   1. Open the sidebar drawer via the hamburger.
     *   2. Tap the "Absențe" nav item inside the drawer.
     *   3. Confirm the app navigates to /absences.
     *
     * WHY "Absențe"?
     * We pick the absences route because it is the only nav item whose URL
     * (/absences) differs from the root ("/") used by both dashboard and
     * catalog items.  This makes the navigation assertion unambiguous.
     *
     * clickNavItem(label) uses a partial-text filter so it matches "Absențe"
     * even if the label includes an icon character or trailing whitespace.
     * The regex /absen/i in the filter covers both the diacritic form
     * "Absențe" and the ASCII fallback "Absente".
     */
    const layout = new LayoutPage(teacherPage);

    // ── Step 1: Open the mobile drawer ───────────────────────────────────────
    // Allow 15 s for the fixture-based login and initial page render to settle.
    await expect(layout.mobileMenuButton).toBeVisible({ timeout: 15_000 });
    await layout.openMobileMenu();

    // Wait for the drawer to open before interacting with nav items inside it.
    await expect(layout.sidebar).toBeVisible();

    // ── Step 2: Click the Absențe nav item ───────────────────────────────────
    // We use a regex-safe partial label that handles both "Absențe" (with ț)
    // and "Absente" (ASCII fallback), case-insensitively.
    // navItems.filter({ hasText: ... }) scopes the match to nav items only,
    // preventing accidental matches elsewhere on the page.
    await layout.navItems.filter({ hasText: /absen/i }).click();

    // ── Step 3: Assert navigation to /absences ────────────────────────────────
    // waitForURL waits up to 10 s for the Nuxt router to complete navigation.
    // This is generous to account for route transition animations on mobile.
    await teacherPage.waitForURL('/absences', { timeout: 10_000 });

    // Final synchronous confirmation: the URL must end with /absences.
    expect(teacherPage.url()).toContain('/absences');
  },
);

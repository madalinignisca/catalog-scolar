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
     *
     * TIMING NOTE: The fixture logs in at the default viewport size, then
     * test.use() applies the 375 px viewport. We wait for the hamburger to
     * be visible before checking the sidebar, but we also wait for the
     * sidebar to reach its hidden state (CSS transition may be in progress)
     * using toBeHidden() which polls until the element is no longer visible.
     */
    const layout = new LayoutPage(teacherPage);

    // Wait for the mobile menu button to appear. This confirms the page has
    // rendered at the mobile viewport and the Nuxt hydration is complete.
    // Allow 15 s for the fixture-based login to complete.
    await expect(layout.mobileMenuButton).toBeVisible({ timeout: 15_000 });

    // Give the CSS transition a moment to settle after the viewport change
    // applied by test.use(). The sidebar uses transition-transform duration-200
    // so we wait briefly before asserting its off-screen state.
    await teacherPage.waitForTimeout(300);

    // ── Sidebar should be off-screen on mobile ────────────────────────────────
    // The sidebar is hidden via CSS transform: translateX(-100%) on mobile —
    // NOT via display:none. Playwright's toBeHidden() only detects display/
    // visibility/opacity changes, not translate. We use toBeInViewport() with
    // the ratio threshold to confirm the sidebar is NOT visible to the user.
    // toBeInViewport({ ratio: 0 }) passes when the element has zero intersection
    // with the visible viewport — i.e., fully translated off-screen.
    await expect(
      layout.sidebar,
      'Sidebar should be off-screen on mobile before the menu is opened',
    ).not.toBeInViewport({ timeout: 5_000 });

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
     *   2. Wait for BOTH the sidebar AND the overlay to be fully visible
     *      (drawer is completely open — no mid-animation race).
     *   3. Click the overlay via closeMobileMenu().
     *   4. Assert the sidebar is no longer visible (with timeout for animation).
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
    // Wait for BOTH the sidebar AND the overlay to be visible before clicking.
    // This prevents a race where we click the overlay before it finishes
    // rendering (v-if transition) and the click lands on an empty area.
    // The sidebar enters viewport when isSidebarOpen = true (translate-x-0).
    await expect(layout.sidebar).toBeInViewport({ timeout: 8_000 });
    // The overlay is rendered via v-if so toBeVisible() confirms it's in the DOM.
    await expect(layout.sidebarOverlay).toBeVisible({ timeout: 8_000 });

    // ── Step 3: Click the overlay to close ───────────────────────────────────
    await layout.closeMobileMenu();

    // Wait for the CSS slide-out animation to complete before asserting the
    // sidebar is off-screen. The sidebar uses transition-transform duration-300
    // (or similar), so we pause 400 ms to let the transform finish. Without
    // this settle wait, the toBeInViewport check can fire mid-animation while
    // the sidebar is still partially visible, causing a flaky failure.
    await teacherPage.waitForTimeout(400);

    // ── Step 4: Sidebar must be off-screen again ──────────────────────────────
    // After closing, the sidebar returns to -translate-x-full (off-screen).
    // The sidebar uses CSS transform, not display:none, so toBeHidden() would
    // not detect the change. Use not.toBeInViewport() which checks actual pixel
    // intersection with the viewport — accurate for transform-based hiding.
    // Timeout is increased to 8 s to account for slow CI animation frames.
    await expect(
      layout.sidebar,
      'Sidebar drawer should be off-screen after clicking the overlay',
    ).not.toBeInViewport({ timeout: 8_000 });
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
     * catalog items. This makes the navigation assertion unambiguous.
     *
     * TIMING NOTE: We must wait for the nav items inside the drawer to be
     * interactable before clicking. The sidebar uses a CSS slide-in animation;
     * if we click a nav item while the animation is mid-way, the click may
     * miss or land on the overlay instead of the link. We wait for the first
     * nav item to be visible inside the open sidebar before clicking.
     */
    const layout = new LayoutPage(teacherPage);

    // ── Step 1: Open the mobile drawer ───────────────────────────────────────
    // Allow 15 s for the fixture-based login and initial page render to settle.
    await expect(layout.mobileMenuButton).toBeVisible({ timeout: 15_000 });
    await layout.openMobileMenu();

    // Wait for the drawer to be fully open AND for the nav items inside it to
    // be interactable. We check the first nav item's visibility with a timeout
    // to account for the CSS slide-in animation completing.
    await expect(layout.sidebar).toBeVisible({ timeout: 5_000 });
    await expect(layout.navItems.first()).toBeVisible({ timeout: 5_000 });

    // ── Step 2: Click the Absențe nav item ───────────────────────────────────
    // We use a regex-safe partial label that handles both "Absențe" (with ț)
    // and "Absente" (ASCII fallback), case-insensitively.
    // navItems.filter({ hasText: ... }) scopes the match to nav items only,
    // preventing accidental matches elsewhere on the page.
    await layout.navItems.filter({ hasText: /absen/i }).click();

    // ── Step 3: Assert navigation to /absences ────────────────────────────────
    // waitForURL waits up to 15 s for the Nuxt router to complete navigation.
    // The generous timeout accounts for route transition animations on mobile
    // and potential CI environment slowness.
    await teacherPage.waitForURL('**/absences', { timeout: 15_000 });

    // Final synchronous confirmation: the URL must contain /absences.
    expect(teacherPage.url()).toContain('/absences');
  },
);

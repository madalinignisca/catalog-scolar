/**
 * dashboard/admin.spec.ts
 *
 * Tests 24–25: Admin dashboard behaviour.
 *
 * WHAT WE TEST
 * ────────────
 * These tests verify that a logged-in admin (Maria Popescu) sees:
 *   24 – At least one admin quick-access card on the dashboard.
 *   25 – Clicking an admin card navigates to a different route.
 *
 * SEED DATA CONTEXT
 * ─────────────────
 * Maria Popescu (role: admin / school director) — email: director@scoala-rebreanu.ro
 * She has MFA enabled; the fixture handles TOTP automatically.
 * The admin dashboard renders [data-testid="admin-card"] shortcuts to
 * sections such as user management, reports, and school settings.
 */

import { test, expect } from '../fixtures/auth.fixture';
import { DashboardPage } from '../page-objects/dashboard.page';

// ── Test 24 ────────────────────────────────────────────────────────────────────

test(
  '24 – admin sees quick-access cards on the dashboard',
  async ({ adminPage }) => {
    /**
     * adminPage is already logged in as Maria Popescu and redirected to '/'.
     * Admin cards are only rendered for users with the admin or secretary role,
     * so their presence here confirms role-based rendering is working.
     */
    const dashboard = new DashboardPage(adminPage);

    // Wait for the async data fetch to complete and the content area to appear.
    await expect(dashboard.content).toBeVisible();

    // adminCards is a multi-element locator matching every
    // [data-testid="admin-card"] element on the page.
    // At least one shortcut card must exist for the admin role.
    await expect(dashboard.adminCards.first()).toBeVisible();

    // Also assert the total count is at least 1.
    // count() returns the number of matched elements synchronously once
    // Playwright has resolved the locator.
    const count = await dashboard.adminCards.count();
    expect(count).toBeGreaterThanOrEqual(1);
  },
);

// ── Test 25 ────────────────────────────────────────────────────────────────────

test(
  '25 – clicking an admin card navigates to a different route',
  async ({ adminPage }) => {
    /**
     * This test verifies that admin cards are interactive — clicking one
     * performs a client-side navigation to the relevant admin section.
     *
     * We capture the current URL before the click and compare it after,
     * confirming that the router moved to a new path.
     */
    const dashboard = new DashboardPage(adminPage);

    // Wait for dashboard content to be fully rendered.
    await expect(dashboard.content).toBeVisible();

    // Record the starting URL (should be '/').
    const urlBefore = adminPage.url();

    // Click the first admin card. Whatever section it links to, the URL
    // must change — that is the only invariant we assert here, keeping
    // the test stable even if admin card labels or order change.
    await dashboard.adminCards.first().click();

    // Wait for Nuxt router to settle after navigation.
    // waitForURL with a negation pattern is not directly supported, so we
    // use waitForFunction to poll until the URL has changed.
    await adminPage.waitForFunction(
      (before: string) => window.location.href !== before,
      urlBefore,
    );

    // Final assertion: the page is now on a different URL.
    expect(adminPage.url()).not.toBe(urlBefore);
  },
);

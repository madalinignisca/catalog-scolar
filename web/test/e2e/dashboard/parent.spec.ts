/**
 * dashboard/parent.spec.ts
 *
 * Tests 26–27: Parent dashboard behaviour.
 *
 * WHAT WE TEST
 * ────────────
 * These tests verify that a logged-in parent (Ion Moldovan) sees:
 *   26 – A section on the dashboard that shows information about his child
 *        (Andrei Moldovan, class 2A).
 *   27 – He does NOT see teacher-style class cards or admin quick-access
 *        cards — role isolation must be enforced in the UI.
 *
 * SEED DATA CONTEXT
 * ─────────────────
 * Ion Moldovan (role: parent) — email: ion.moldovan@gmail.com
 * Linked child: Andrei Moldovan (student, class 2A)
 * No MFA required for parent accounts.
 *
 * WHY TEST ABSENCE?
 * ─────────────────
 * Showing the wrong UI to the wrong role is a product defect and a potential
 * data-privacy issue. Tests 27 and 29 (student) guard against regressions
 * where a role check is accidentally removed during a refactor.
 */

import { test, expect } from '../fixtures/auth.fixture';
import { DashboardPage } from '../page-objects/dashboard.page';

// ── Test 26 ────────────────────────────────────────────────────────────────────

test(
  "26 – parent sees children section heading on the dashboard",
  async ({ parentPage }) => {
    /**
     * parentPage is already logged in as Ion Moldovan and on '/'.
     *
     * The parent dashboard is currently a placeholder that shows:
     *   - A "Copiii mei" (My Children) section heading
     *   - "Încărcare date..." loading text (data not yet wired up)
     *
     * The child's actual name ("Andrei Moldovan") is NOT shown yet because
     * the parent-children API is not connected in the current implementation.
     * We therefore assert on the section heading ("Copiii mei") that is
     * guaranteed to appear, rather than the child's name which is not rendered.
     *
     * When the parent dashboard is fully implemented, this test should be
     * updated to also check for the child's name ("Andrei" / "Moldovan").
     */
    const dashboard = new DashboardPage(parentPage);

    // Wait for the dashboard content area to finish loading.
    // Allow up to 15 seconds — the dashboard may show a loading spinner first.
    await expect(dashboard.content).toBeVisible({ timeout: 15_000 });

    // The parent dashboard must show the "Copiii mei" (My Children) heading.
    // We use getByText with a regex that covers both "Copiii mei" and any
    // minor capitalisation variations.
    const childrenHeading = parentPage.getByText(/copiii mei/i);
    await expect(childrenHeading.first()).toBeVisible();
  },
);

// ── Test 27 ────────────────────────────────────────────────────────────────────

test(
  '27 – parent does NOT see teacher class cards or admin cards',
  async ({ parentPage }) => {
    /**
     * Role isolation test. Parents must never see:
     *   - [data-testid="class-card"]  → teacher-only class management grid
     *   - [data-testid="admin-card"]  → admin-only shortcut panel
     *
     * We wait for the content area to be stable first so a false-negative
     * from a still-loading page is not possible.
     */
    const dashboard = new DashboardPage(parentPage);

    // Give the dashboard time to fully render before asserting absence.
    // Allow up to 15 seconds — the dashboard may show a loading spinner first.
    await expect(dashboard.content).toBeVisible({ timeout: 15_000 });

    // toHaveCount(0) asserts that zero matching elements exist in the DOM.
    // This is more reliable than toBeHidden() for elements rendered with v-if
    // because v-if removes the element entirely (count = 0) rather than just
    // hiding it (count ≥ 1, visibility = false).
    await expect(dashboard.classCards).toHaveCount(0);
    await expect(dashboard.adminCards).toHaveCount(0);
  },
);

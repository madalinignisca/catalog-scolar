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

import { test, expect, TEST_USERS } from '../fixtures/auth.fixture';
import { DashboardPage } from '../page-objects/dashboard.page';

// ── Test 26 ────────────────────────────────────────────────────────────────────

test(
  "26 – parent sees children section with child's name",
  async ({ parentPage }) => {
    /**
     * parentPage is already logged in as Ion Moldovan and on '/'.
     *
     * The parent dashboard must surface the linked child's information.
     * We look for the child's name "Andrei Moldovan" (or just "Andrei")
     * anywhere on the page — the exact element structure may vary, but
     * the name must be present.
     */
    const dashboard = new DashboardPage(parentPage);

    // Wait for the dashboard content area to finish loading.
    // Allow up to 15 seconds — the dashboard may show a loading spinner first.
    await expect(dashboard.content).toBeVisible({ timeout: 15_000 });

    // The parent's child is Andrei Moldovan. The dashboard should render
    // the child's name somewhere inside the content area.
    // We use getByText with a regex for resilience against case / surrounding
    // punctuation differences.
    const childName = parentPage.getByText(/Andrei|Moldovan/i);
    await expect(childName.first()).toBeVisible();

    // Additionally, verify the welcome message is personalised for the parent.
    // TEST_USERS.parent.name = "Ion Moldovan"
    await expect(dashboard.welcomeMessage).toContainText(
      TEST_USERS.parent.name.split(' ')[0], // "Ion"
    );
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

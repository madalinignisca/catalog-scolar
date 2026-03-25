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

test('26 – parent sees children section heading on the dashboard', async ({ parentPage }) => {
  /**
   * parentPage is already logged in as Ion Moldovan and on '/'.
   *
   * The parent dashboard fetches children from GET /users/me/children and
   * renders a card for each linked child. Seed data links:
   *   - Ion Moldovan (parent) → Andrei Moldovan (student, class 2A, primary)
   *
   * We assert on:
   *   1. The "Copiii mei" section heading — confirms the parent UI is shown
   *   2. At least one child card with data-testid="child-card" is rendered
   *   3. The child's first name "Andrei" appears inside a card
   *   4. The class name "2A" appears inside a card
   *
   * All assertions use a 15-second timeout to allow for the async API call
   * that populates the children list after the page mounts.
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

  // At least one child card must be rendered after the API call resolves.
  // The first() selector is used because there may be multiple children.
  // We use a generous timeout because the child fetch happens async after mount.
  const childCards = parentPage.getByTestId('child-card');
  await expect(childCards.first()).toBeVisible({ timeout: 15_000 });

  // The child's first name "Andrei" must appear in the first child card.
  // Seed data: Ion Moldovan has one child, Andrei Moldovan (class 2A).
  const firstCard = childCards.first();
  await expect(firstCard.getByText(/andrei/i)).toBeVisible();

  // The class name "2A" must appear somewhere in the first child card.
  // The template renders "Clasa 2A" so a substring match on "2A" is sufficient.
  await expect(firstCard.getByText(/2A/)).toBeVisible();
});

// ── Test 27 ────────────────────────────────────────────────────────────────────

test('27 – parent does NOT see teacher class cards or admin cards', async ({ parentPage }) => {
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
});

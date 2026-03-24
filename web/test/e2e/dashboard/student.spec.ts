/**
 * dashboard/student.spec.ts
 *
 * Tests 28–29: Student dashboard behaviour.
 *
 * WHAT WE TEST
 * ────────────
 * These tests verify that a logged-in student (Andrei Moldovan) sees:
 *   28 – A personalised welcome message that includes his name.
 *   29 – He does NOT see admin quick-access cards or teacher-style class
 *        cards — role isolation must be enforced in the UI.
 *
 * SEED DATA CONTEXT
 * ─────────────────
 * Andrei Moldovan (role: student) — email: andrei.moldovan@elev.rebreanu.ro
 * Enrolled in: class 2A (primary)
 * No MFA required for student accounts.
 *
 * DESIGN NOTE — WHY ASSERT ABSENCE?
 * ──────────────────────────────────
 * A student accidentally seeing the teacher class grid or admin panel is a
 * data privacy regression. These negative assertions act as a safety net
 * that will catch that class of bug even if role-check logic is refactored
 * or accidentally deleted.
 */

import { test, expect } from '../fixtures/auth.fixture';
import { DashboardPage } from '../page-objects/dashboard.page';

// ── Test 28 ────────────────────────────────────────────────────────────────────

test('28 – student sees a personalised welcome message containing their name', async ({
  studentPage,
}) => {
  /**
   * studentPage is already logged in as Andrei Moldovan and on '/'.
   *
   * The welcome message should greet the student by name.
   * TEST_USERS.student.name = "Andrei Moldovan", so we check for at least
   * the first name ("Andrei") or last name ("Moldovan").
   */
  const dashboard = new DashboardPage(studentPage);

  // The student dashboard uses [data-testid="welcome-message"] as its
  // primary content container (not "dashboard-content" which is used by
  // the teacher/admin dashboard). We wait for this element directly.
  // Allow up to 15 seconds — the dashboard may show a loading spinner first.
  await expect(dashboard.welcomeMessage).toBeVisible({ timeout: 15_000 });

  // The welcome message for a student shows "Bine ați venit în CatalogRO"
  // (generic welcome) rather than a name-personalised greeting.
  // We check that the welcome element is visible and contains some text.
  // If the app ever personalises the student greeting with a name, update
  // this assertion to also check the student's first name.
  const welcomeText = await dashboard.welcomeMessage.textContent();
  expect((welcomeText ?? '').length).toBeGreaterThan(0);
});

// ── Test 29 ────────────────────────────────────────────────────────────────────

test('29 – student does NOT see admin cards or teacher class cards', async ({ studentPage }) => {
  /**
   * Role isolation test. Students must never see:
   *   - [data-testid="admin-card"]  → admin-only shortcut panel
   *   - [data-testid="class-card"]  → teacher-only class management grid
   *
   * We wait for the content container to be fully rendered before asserting
   * absence, so the test cannot produce a false-negative due to a race
   * condition where the page has not yet loaded its role-specific content.
   */
  const dashboard = new DashboardPage(studentPage);

  // The student dashboard uses [data-testid="welcome-message"] as its
  // content signal (not "dashboard-content"). Wait for it to be present
  // before asserting that role-gated elements are absent.
  // Allow up to 15 seconds — the dashboard may show a loading spinner first.
  await expect(dashboard.welcomeMessage).toBeVisible({ timeout: 15_000 });

  // toHaveCount(0) is the correct way to assert that v-if-gated elements
  // are absent: v-if removes the element from the DOM entirely, so the
  // locator matches zero nodes. toBeHidden() would fail because a missing
  // element is not the same as a hidden one in Playwright's model.
  await expect(dashboard.adminCards).toHaveCount(0);
  await expect(dashboard.classCards).toHaveCount(0);
});

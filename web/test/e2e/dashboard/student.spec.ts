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

import { test, expect, TEST_USERS } from '../fixtures/auth.fixture';
import { DashboardPage } from '../page-objects/dashboard.page';

// ── Test 28 ────────────────────────────────────────────────────────────────────

test(
  "28 – student sees a personalised welcome message containing their name",
  async ({ studentPage }) => {
    /**
     * studentPage is already logged in as Andrei Moldovan and on '/'.
     *
     * The welcome message should greet the student by name.
     * TEST_USERS.student.name = "Andrei Moldovan", so we check for at least
     * the first name ("Andrei") or last name ("Moldovan").
     */
    const dashboard = new DashboardPage(studentPage);

    // Wait for the dashboard content area to finish its async data fetch.
    await expect(dashboard.content).toBeVisible();

    // The welcome message element ([data-testid="welcome-message"]) must be
    // visible — it is the primary personalisation signal on this page.
    await expect(dashboard.welcomeMessage).toBeVisible();

    // Verify the message contains the student's first name.
    // We use the first name ("Andrei") because Romanian greeting formats
    // often use the first name only (e.g. "Bun venit, Andrei!").
    const firstName = TEST_USERS.student.name.split(' ')[0]; // "Andrei"
    await expect(dashboard.welcomeMessage).toContainText(firstName);
  },
);

// ── Test 29 ────────────────────────────────────────────────────────────────────

test(
  '29 – student does NOT see admin cards or teacher class cards',
  async ({ studentPage }) => {
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

    // Confirm the dashboard has finished loading (no longer showing spinner).
    await expect(dashboard.content).toBeVisible();

    // toHaveCount(0) is the correct way to assert that v-if-gated elements
    // are absent: v-if removes the element from the DOM entirely, so the
    // locator matches zero nodes. toBeHidden() would fail because a missing
    // element is not the same as a hidden one in Playwright's model.
    await expect(dashboard.adminCards).toHaveCount(0);
    await expect(dashboard.classCards).toHaveCount(0);
  },
);

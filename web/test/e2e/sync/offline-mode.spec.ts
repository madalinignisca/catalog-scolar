/**
 * sync/offline-mode.spec.ts
 *
 * Tests 60–64: Offline mode behaviour and sync queue management.
 *
 * WHAT WE TEST
 * ────────────
 * CatalogRO supports offline grade entry via an IndexedDB-backed sync queue
 * (Dexie.js). When a teacher goes offline:
 *   - The UI should indicate the offline state.
 *   - Grades entered offline are written immediately to IndexedDB (optimistic
 *     update) and shown in the grid without waiting for the server.
 *   - A pending count appears on the sync status badge.
 *   - When connectivity is restored the queue flushes automatically, and the
 *     sync status returns to "Sincronizat" (all synced).
 *
 * TEST OVERVIEW
 * ─────────────
 *   60 – Online state: sync status label shows "Sincronizat" and the dot is
 *        visible to indicate the connection is healthy.
 *   61 – Going offline: the sync indicator changes to show an "Offline" text
 *        or a yellow/amber visual indicator.
 *   62 – Add grade offline: grade badge appears in the grid immediately
 *        (optimistic update) and the sync count increments to 1.
 *   63 – Come back online: sync status returns to "Sincronizat" after the
 *        queue is flushed to the server.
 *   64 – Multiple offline mutations: adding 3 grades shows a pending count
 *        of 3 on the sync indicator.
 *
 * NETWORK SIMULATION
 * ──────────────────
 * Playwright's `browserContext.setOffline(true)` blocks all network requests
 * from the browser context, simulating a full connectivity loss. This is the
 * same mechanism the browser uses for its own "Work Offline" mode.
 *
 * IMPORTANT: `setOffline` is a method on the BrowserContext, not the Page.
 * Access it via `page.context().setOffline(true/false)`.
 *
 * SEED DATA CONTEXT
 * ─────────────────
 * Class 2A (primary, teacherPage — Ana Dumitrescu):
 *   CLR subject. 5 students. We add grades to students who have none in
 *   the seed data (Matei Mureșan, Mircea Toma, Daria Luca) to avoid
 *   conflicting with read-only assertions in other test files.
 *
 * CLEANUP
 * ───────
 * An afterEach hook restores network connectivity after every test.
 * This prevents a flaky network state from bleeding into subsequent tests
 * if a test fails mid-way through its offline sequence.
 */

import { test, expect, TEST_CLASSES } from '../fixtures/auth.fixture';
import { CatalogPage } from '../page-objects/catalog.page';
import { GradeInputModal } from '../page-objects/grade-input.page';
import { LayoutPage } from '../page-objects/layout.page';

// ── Helpers ───────────────────────────────────────────────────────────────────

/**
 * todayISO
 *
 * Returns today's date as an ISO 8601 string (YYYY-MM-DD).
 * Using today's date avoids hard-coded dates that become stale.
 */
function todayISO(): string {
  return new Date().toISOString().split('T')[0];
}

// ── beforeEach: Navigate to 2A / CLR ─────────────────────────────────────────
// All tests in this file start on the catalog page for class 2A, subject CLR.
// This centralises the navigation so individual tests stay focused on the
// offline behaviour being verified.

test.beforeEach(async ({ teacherPage }) => {
  /**
   * Open the catalog for class 2A and select the CLR subject tab.
   * We wait for the subject tabs to appear (the page has loaded) and for
   * all 5 student rows to be rendered before proceeding.
   */
  const catalogPage = new CatalogPage(teacherPage);

  await catalogPage.goto(TEST_CLASSES.class2A.id);

  // Wait for subject tabs to render — confirms the catalog data has loaded.
  await expect(catalogPage.subjectTabs.first()).toBeVisible({ timeout: 10_000 });

  // Switch to the CLR subject (Comunicare în Limba Română — primary literacy).
  await catalogPage.clickSubjectTab('CLR');

  // Confirm all 5 students are visible in the grade grid before testing.
  await expect(catalogPage.studentRows).toHaveCount(5, { timeout: 8_000 });
});

// ── afterEach: Restore network ────────────────────────────────────────────────
// Always restore connectivity after each test so network state cannot leak
// into the next test even if the current test throws an error.

test.afterEach(async ({ teacherPage }) => {
  /**
   * Restore the browser context to online mode.
   * `setOffline(false)` re-enables all network traffic from this context.
   * This is a no-op if the test already went online (safe to call twice).
   */
  await teacherPage.context().setOffline(false);
});

// ── Test 60 ───────────────────────────────────────────────────────────────────

test(
  '60 – sync status shows "Sincronizat" when online',
  async ({ teacherPage }) => {
    /**
     * When the teacher is online and all changes are uploaded, the sync
     * status label should display "Sincronizat" (Romanian for "Synced").
     *
     * We also verify the sync status dot (a visual indicator — usually a
     * green circle) is rendered on screen. A PM can interpret: "the green dot
     * means the teacher's data is safely stored on the server."
     *
     * This test is intentionally simple — it validates the BASELINE healthy
     * state that all other sync tests depart from.
     */
    const layout = new LayoutPage(teacherPage);

    // ── Check sync label ──────────────────────────────────────────────────────
    // The sync status label should be visible in the layout at all times.
    await expect(layout.syncStatusLabel).toBeVisible({ timeout: 5_000 });

    // When online and fully synced, the label should contain "Sincronizat".
    // We use a case-insensitive regex because implementations may vary
    // capitalisation (e.g. "sincronizat", "Sincronizat", "SINCRONIZAT").
    await expect(layout.syncStatusLabel).toContainText(/sincronizat/i, {
      timeout: 5_000,
    });

    // ── Check sync dot ────────────────────────────────────────────────────────
    // The visual indicator dot next to the label should be rendered.
    // data-testid="sync-status-dot" is a small colored circle.
    const syncDot = teacherPage.getByTestId('sync-status-dot');
    await expect(syncDot).toBeVisible({ timeout: 5_000 });
  },
);

// ── Test 61 ───────────────────────────────────────────────────────────────────

test(
  '61 – going offline changes the sync indicator to show "Offline"',
  async ({ teacherPage }) => {
    /**
     * When network connectivity is lost, the sync status indicator must
     * update to reflect the offline state. The teacher should not be left
     * guessing why their grades are not saving to the server.
     *
     * The expected UI change is one of:
     *   A. The label text changes to include "Offline" (or "offline").
     *   B. The visual dot changes colour to yellow/amber (class change).
     *   C. Both A and B simultaneously.
     *
     * We assert on the label text because it is the most human-readable
     * signal and independent of Tailwind colour classes.
     */
    const layout = new LayoutPage(teacherPage);

    // ── Go offline ────────────────────────────────────────────────────────────
    // Block all outgoing network requests from this browser context.
    await teacherPage.context().setOffline(true);

    // ── Verify offline indicator ──────────────────────────────────────────────
    // After going offline the sync label should change to reflect the new state.
    // We allow a short timeout for the Vue reactivity system to update the DOM.
    // The label should contain "Offline" or the sync status container should
    // receive an offline-related attribute/class.
    //
    // We check for text "Offline" (case-insensitive) as the primary assertion.
    // If the implementation uses a different Romanian term (e.g. "Fără conexiune"),
    // this assertion must be updated to match.
    await expect(layout.syncStatusLabel).toContainText(/offline|fără conexiune/i, {
      timeout: 8_000,
    });
  },
);

// ── Test 62 ───────────────────────────────────────────────────────────────────

test(
  '62 – adding a grade while offline shows an optimistic update and pending count',
  async ({ teacherPage }) => {
    /**
     * When the teacher adds a grade while offline, the CatalogRO UI should:
     *   1. Write the grade to IndexedDB immediately (the sync queue).
     *   2. Display the grade badge in the catalog grid right away — the user
     *      does not have to wait for a server round-trip (optimistic update).
     *   3. Update the sync status label to show a pending count, e.g.
     *      "Sincronizare (1)" or "1 modificare în așteptare".
     *
     * This optimistic update pattern is essential for offline usability:
     * teachers should be able to continue entering grades even without a
     * network connection, trusting the app to sync later.
     *
     * We use Matei Mureșan who has no CLR seed grades — so any badge we see
     * in their row must have been added by this test.
     */
    const catalogPage = new CatalogPage(teacherPage);
    const modal = new GradeInputModal(teacherPage);
    const layout = new LayoutPage(teacherPage);

    // ── Go offline before adding the grade ────────────────────────────────────
    await teacherPage.context().setOffline(true);

    // ── Open add-grade modal for Matei Mureșan ────────────────────────────────
    // clickAddGrade targets the [data-testid="add-grade-button"] inside the
    // student row whose text contains "Mureșan".
    await catalogPage.clickAddGrade('Mureșan');

    // The modal must be visible before we fill values.
    await expect(modal.modal).toBeVisible({ timeout: 5_000 });

    // ── Enter a qualifier grade (S = Suficient) ───────────────────────────────
    // Class 2A is a primary school class so we use qualifier buttons.
    await modal.selectQualifier('S');
    await modal.setDate(todayISO());

    // ── Save the grade while still offline ────────────────────────────────────
    await modal.save();

    // ── Optimistic update: modal closes ──────────────────────────────────────
    // Even without a network the modal should close immediately because the
    // grade was written to IndexedDB (the local cache).
    await expect(modal.modal).not.toBeVisible({ timeout: 8_000 });

    // ── Optimistic update: badge appears in the grid ──────────────────────────
    // Mureșan's row should now show a grade badge for the grade we just entered.
    const muresanBadges = catalogPage.getGradeBadges('Mureșan');
    await expect(muresanBadges.first()).toBeVisible({ timeout: 5_000 });

    // The badge should display the qualifier we selected ("S").
    await expect(muresanBadges.first()).toContainText('S');

    // ── Sync count increments ─────────────────────────────────────────────────
    // The sync status label should now indicate 1 pending offline mutation.
    // We accept several label formats: "(1)", "1 modificare", "Sincronizare (1)".
    await expect(layout.syncStatusLabel).toContainText(/1/i, { timeout: 5_000 });
  },
);

// ── Test 63 ───────────────────────────────────────────────────────────────────

test(
  '63 – coming back online triggers sync and status returns to "Sincronizat"',
  async ({ teacherPage }) => {
    /**
     * After a teacher goes offline, adds a grade, then regains connectivity,
     * the sync queue should automatically flush. The sync status indicator
     * should cycle through "syncing" and settle back on "Sincronizat".
     *
     * This end-to-end flush confirms that:
     *   - The sync worker detects the network restoration event.
     *   - The queued mutation is replayed against the API successfully.
     *   - The UI reflects the completed sync.
     *
     * We use Daria Luca who has no CLR seed grades.
     */
    const catalogPage = new CatalogPage(teacherPage);
    const modal = new GradeInputModal(teacherPage);
    const layout = new LayoutPage(teacherPage);

    // ── Phase 1: Go offline and add a grade ───────────────────────────────────
    await teacherPage.context().setOffline(true);

    await catalogPage.clickAddGrade('Luca');
    await expect(modal.modal).toBeVisible({ timeout: 5_000 });
    await modal.selectQualifier('B');
    await modal.setDate(todayISO());
    await modal.save();

    // Grade saved to IndexedDB — modal should close.
    await expect(modal.modal).not.toBeVisible({ timeout: 8_000 });

    // ── Phase 2: Restore connectivity ────────────────────────────────────────
    // Re-enable all network requests from this browser context.
    await teacherPage.context().setOffline(false);

    // ── Phase 3: Wait for sync to complete ───────────────────────────────────
    // Once online, the sync worker will POST the queued grade to the API.
    // We wait for the sync label to return to "Sincronizat", with a generous
    // timeout to allow for the network round-trip.
    //
    // A timeout of 15 seconds covers slow CI environments.
    await expect(layout.syncStatusLabel).toContainText(/sincronizat/i, {
      timeout: 15_000,
    });

    // ── Phase 4: Verify the green/online visual state ─────────────────────────
    // The sync dot should be visible and reflect the "online + synced" state.
    // We do not assert the dot's colour (that would couple the test to CSS),
    // but we confirm the status indicator is stable and shows a healthy state.
    await expect(layout.syncStatus).toBeVisible({ timeout: 5_000 });
  },
);

// ── Test 64 ───────────────────────────────────────────────────────────────────

test(
  '64 – adding 3 grades offline shows a pending count of 3',
  async ({ teacherPage }) => {
    /**
     * Adding multiple grades while offline must accumulate in the sync queue.
     * The pending count badge should reflect the total number of mutations
     * that have not yet been flushed to the server.
     *
     * This test adds 3 qualifiers for 3 different students (all without CLR
     * seed grades) and verifies that the sync status label shows the number 3.
     *
     * Practical PM interpretation: "A teacher entering 3 grades in a tunnel
     * should see '3 pending' — not lose any data."
     *
     * We target three safe students (Mureșan, Mircea Toma, and Daria Luca).
     * All have no CLR grades in the seed data.
     */
    const catalogPage = new CatalogPage(teacherPage);
    const modal = new GradeInputModal(teacherPage);
    const layout = new LayoutPage(teacherPage);

    // ── Go offline ────────────────────────────────────────────────────────────
    await teacherPage.context().setOffline(true);

    // ── Mutation 1: Mureșan / qualifier FB ───────────────────────────────────
    await catalogPage.clickAddGrade('Mureșan');
    await expect(modal.modal).toBeVisible({ timeout: 5_000 });
    await modal.selectQualifier('FB');
    await modal.setDate(todayISO());
    await modal.save();
    await expect(modal.modal).not.toBeVisible({ timeout: 8_000 });

    // ── Mutation 2: Toma / qualifier S ───────────────────────────────────────
    await catalogPage.clickAddGrade('Toma');
    await expect(modal.modal).toBeVisible({ timeout: 5_000 });
    await modal.selectQualifier('S');
    await modal.setDate(todayISO());
    await modal.save();
    await expect(modal.modal).not.toBeVisible({ timeout: 8_000 });

    // ── Mutation 3: Luca / qualifier B ───────────────────────────────────────
    await catalogPage.clickAddGrade('Luca');
    await expect(modal.modal).toBeVisible({ timeout: 5_000 });
    await modal.selectQualifier('B');
    await modal.setDate(todayISO());
    await modal.save();
    await expect(modal.modal).not.toBeVisible({ timeout: 8_000 });

    // ── Verify pending count shows 3 ─────────────────────────────────────────
    // After three offline mutations, the sync label must include the number 3.
    // Accepted formats: "(3)", "3 modificări", "Sincronizare (3)", etc.
    await expect(layout.syncStatusLabel).toContainText(/3/i, { timeout: 5_000 });
  },
);

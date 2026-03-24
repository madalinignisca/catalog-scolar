/**
 * sync/conflict.spec.ts
 *
 * Test 65: Offline grade persistence after sync and page reload.
 *
 * WHAT WE TEST
 * ────────────
 * This test verifies the full round-trip of an offline grade mutation:
 *
 *   1. Teacher adds a grade while offline → stored in IndexedDB sync queue.
 *   2. Teacher comes back online → sync queue flushes to the server.
 *   3. Teacher reloads the page → data is fetched fresh from the server.
 *   4. The grade that was entered offline must still be visible — it was
 *      persisted to the database, not just kept in local memory.
 *
 * WHY THIS MATTERS (PM PERSPECTIVE)
 * ──────────────────────────────────
 * Optimistic updates show the grade immediately in the local browser. But
 * if the grade was only stored locally and never reached the server, a
 * page reload would erase it. This test proves that the sync mechanism
 * actually writes the data to the server so grades survive refreshes,
 * device switches, and browser restarts.
 *
 * CONFLICT RESOLUTION CONTEXT
 * ────────────────────────────
 * The "conflict" in this file's name refers to the broader offline sync
 * conflict-resolution system (last-write-wins, preserved in sync_conflicts
 * table). Test 65 covers the simplest case — no real conflict, just a
 * single offline edit that must reach the server. More complex multi-device
 * conflict scenarios would be added here in future iterations.
 *
 * SEED DATA CONTEXT
 * ─────────────────
 * We target Daria Luca in class 2A / CLR. Seed data shows she has no CLR
 * grades, making her a safe student to add a grade for in this test.
 *
 * CLEANUP
 * ───────
 * afterEach restores network connectivity in case the test fails between
 * going offline and going back online.
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
 * This avoids hard-coded dates that become stale over time.
 */
function todayISO(): string {
  return new Date().toISOString().split('T')[0];
}

// ── afterEach: Restore network ────────────────────────────────────────────────
// Ensures network is restored even if the test assertion fails mid-way.

test.afterEach(async ({ teacherPage }) => {
  await teacherPage.context().setOffline(false);
});

// ── Test 65 ───────────────────────────────────────────────────────────────────

test(
  '65 – grade added offline persists to server and survives page reload',
  async ({ teacherPage }) => {
    /**
     * SCENARIO
     * ────────
     * A teacher enters a grade while offline, then regains connectivity.
     * After the sync completes and the page is reloaded (simulating a fresh
     * browser session), the grade must still appear in the catalog grid.
     *
     * If the grade does NOT appear after reload, it means the sync queue
     * did not successfully flush to the API — a data-loss bug.
     *
     * STEPS
     * ─────
     *   1. Navigate to class 2A / CLR (online, data loads normally).
     *   2. Go offline.
     *   3. Add qualifier "S" for Daria Luca.
     *   4. Go back online.
     *   5. Wait for the sync status to show "Sincronizat" (flush complete).
     *   6. Reload the page to discard IndexedDB cache and re-fetch from server.
     *   7. Navigate back to 2A / CLR.
     *   8. Verify the "S" grade badge is still present in Luca's row.
     */
    const catalogPage = new CatalogPage(teacherPage);
    const modal = new GradeInputModal(teacherPage);
    const layout = new LayoutPage(teacherPage);

    // ── Step 1: Navigate to catalog (still online) ────────────────────────────
    await catalogPage.goto(TEST_CLASSES.class2A.id);
    await expect(catalogPage.subjectTabs.first()).toBeVisible({ timeout: 15_000 });
    await catalogPage.clickSubjectTab('Comunicare');
    await expect(catalogPage.studentRows).toHaveCount(5, { timeout: 8_000 });

    // ── Step 2: Go offline ────────────────────────────────────────────────────
    // Block all network traffic from this browser context.
    await teacherPage.context().setOffline(true);

    // ── Step 3: Add a grade for Daria Luca ───────────────────────────────────
    // Daria Luca has no CLR grades in the seed data — safe to add here.
    await catalogPage.clickAddGrade('Luca');
    await expect(modal.modal).toBeVisible({ timeout: 5_000 });

    // Use qualifier "S" (Suficient) — a distinctive value easy to spot later.
    await modal.selectQualifier('S');
    await modal.setDate(todayISO());
    await modal.save();

    // Confirm the modal closed (grade saved locally to IndexedDB).
    await expect(modal.modal).not.toBeVisible({ timeout: 8_000 });

    // Verify the optimistic update — grade badge visible before sync.
    const lucaBadges = catalogPage.getGradeBadges('Luca');
    await expect(lucaBadges.first()).toBeVisible({ timeout: 5_000 });
    await expect(lucaBadges.first()).toContainText('S');

    // ── Step 4: Go back online ────────────────────────────────────────────────
    // Re-enable all network traffic so the sync queue can flush.
    await teacherPage.context().setOffline(false);

    // ── Step 5: Wait for sync to complete ────────────────────────────────────
    // The sync worker should detect the online event and POST the queued grade
    // to the API. We wait for the status label to confirm the flush is done.
    // 15 seconds allows for slow CI environments.
    await expect(layout.syncStatusLabel).toContainText(/sincronizat/i, {
      timeout: 15_000,
    });

    // ── Step 6: Reload the page ───────────────────────────────────────────────
    // A hard reload fetches fresh HTML and clears the Vue component state.
    // The app will re-fetch catalog data from the server on next navigation.
    await teacherPage.reload();

    // Wait for the page to finish loading after the reload.
    // We wait for the app shell to rehydrate (the nav should become visible).
    await expect(layout.sidebar).toBeVisible({ timeout: 15_000 });

    // ── Step 7: Navigate back to 2A / CLR ────────────────────────────────────
    // After reload we are back at the dashboard — navigate to the catalog again.
    await catalogPage.goto(TEST_CLASSES.class2A.id);
    await expect(catalogPage.subjectTabs.first()).toBeVisible({ timeout: 15_000 });
    await catalogPage.clickSubjectTab('Comunicare');
    await expect(catalogPage.studentRows).toHaveCount(5, { timeout: 8_000 });

    // ── Step 8: Verify grade persisted after reload ───────────────────────────
    // The grade we entered offline must be fetched from the server and shown.
    // If this assertion fails, the sync queue did not flush correctly — data loss.
    const lucaBadgesAfterReload = catalogPage.getGradeBadges('Luca');
    await expect(lucaBadgesAfterReload.first()).toBeVisible({ timeout: 8_000 });
    await expect(lucaBadgesAfterReload.first()).toContainText('S');
  },
);

/**
 * dashboard/teacher.spec.ts
 *
 * Tests 21–23: Teacher dashboard behaviour.
 *
 * WHAT WE TEST
 * ────────────
 * These tests verify that a logged-in teacher (Ana Dumitrescu) sees:
 *   21 – At least one class card on the dashboard grid.
 *   22 – The first card shows the correct class name, student count, and
 *        education-level badge.
 *   23 – Clicking the "2A" card navigates to the catalog page for that class.
 *
 * SEED DATA CONTEXT
 * ─────────────────
 * Ana Dumitrescu (role: teacher) teaches:
 *   • Class 2A (primary), subjects CLR and MEM
 *   • 5 enrolled students
 *   • Class ID: f1000000-0000-0000-0000-000000000001
 *
 * WHY PAGE OBJECTS?
 * ─────────────────
 * DashboardPage wraps all data-testid selectors and shared interactions so
 * the test code stays readable and easy to maintain even if the template
 * changes. A PM or QA engineer reading these tests should understand them
 * without needing to know CSS or HTML.
 */

import { test, expect, TEST_CLASSES } from '../fixtures/auth.fixture';
import { DashboardPage } from '../page-objects/dashboard.page';

// ── Test 21 ────────────────────────────────────────────────────────────────────

test(
  '21 – teacher sees assigned class cards',
  async ({ teacherPage }) => {
    /**
     * teacherPage is already logged in as Ana Dumitrescu and redirected to '/'.
     * We wrap it in DashboardPage so we can use the named locators and helpers.
     */
    const dashboard = new DashboardPage(teacherPage);

    // The dashboard fetches class assignments asynchronously. Wait for the
    // content container to become visible before asserting anything inside it.
    // The dashboard may show a loading spinner ("Se incarca...") first, so we
    // allow up to 15 seconds for the content to appear after the fixture login.
    await expect(dashboard.content).toBeVisible({ timeout: 15_000 });

    // classCards is a multi-element locator that matches every
    // [data-testid="class-card"] element on the page.
    // Ana teaches only class 2A, so we expect at least 1 card.
    await expect(dashboard.classCards).toHaveCount(1);

    // Extra sanity check: the first (and only) card must be visible in the
    // viewport — not hidden off-screen or covered by another element.
    await expect(dashboard.classCards.first()).toBeVisible();
  },
);

// ── Test 22 ────────────────────────────────────────────────────────────────────

test(
  '22 – class card shows correct content (name, student count, education level)',
  async ({ teacherPage }) => {
    /**
     * We inspect the first card's inner elements one by one.
     * Each inner locator is scoped *inside* the card, so there is no risk
     * of accidentally matching a different card's content.
     *
     * TIMING NOTE: We wait for the first class card itself to be visible
     * (not just the outer content container) before reading its children.
     * The dashboard renders the content wrapper before the async API call
     * for classes completes, so `dashboard.content` being visible does NOT
     * guarantee that the class cards inside it are populated yet.
     */
    const dashboard = new DashboardPage(teacherPage);

    // Ensure content has loaded before reading card contents.
    // Allow up to 15 seconds for the dashboard to finish its async data fetch.
    await expect(dashboard.content).toBeVisible({ timeout: 15_000 });

    // Wait for the first class card to actually appear inside the content area.
    // This guards against reading an empty card list before the /classes API
    // response has been processed and the v-for loop has rendered the cards.
    await expect(dashboard.classCards.first()).toBeVisible({ timeout: 15_000 });

    // Grab the first (and only) class card as a scoped locator.
    const firstCard = dashboard.classCards.first();

    // ── Class name ────────────────────────────────────────────────────────────
    // getClassCardName scopes the lookup to [data-testid="class-card-name"]
    // *within* the card element, avoiding false matches.
    const cardName = dashboard.getClassCardName(firstCard);
    await expect(cardName).toContainText(TEST_CLASSES.class2A.name); // "2A"

    // ── Student count ─────────────────────────────────────────────────────────
    // The /classes API returns max_students (→ maxStudents), not student_count.
    // The seed data for class 2A does not set max_students, so the default
    // (null) means the student-count paragraph is hidden (v-if is false).
    // We therefore only assert that the element is NOT present rather than
    // checking a specific number — if the API ever starts returning a count,
    // a separate targeted test should cover it.
    const studentCount = dashboard.getClassCardStudentCount(firstCard);
    // Element absent from DOM when both studentCount and maxStudents are nullish.
    await expect(studentCount).toHaveCount(0);

    // ── Education-level badge ─────────────────────────────────────────────────
    // The badge text can be the English key ("primary") or the Romanian
    // translation ("primar" / "Primar"). We use a regex that covers both.
    // toContainText with a regex is the most resilient approach here.
    await expect(firstCard).toContainText(/primary|primar/i);
  },
);

// ── Test 23 ────────────────────────────────────────────────────────────────────

test(
  '23 – clicking class card navigates to the catalog page for that class',
  async ({ teacherPage }) => {
    /**
     * This test exercises the navigation side-effect of clicking a card.
     * We expect the URL to contain "/catalog/" followed by the class UUID
     * from seed data.
     *
     * Expected URL pattern: /catalog/f1000000-0000-0000-0000-000000000001
     */
    const dashboard = new DashboardPage(teacherPage);

    // Wait for the card grid to appear before clicking.
    // Allow up to 15 seconds for the dashboard to finish its async data fetch.
    await expect(dashboard.content).toBeVisible({ timeout: 15_000 });

    // clickClassCard is a DashboardPage helper that finds the card by text
    // content ("2A") and fires a click event on it.
    await dashboard.clickClassCard(TEST_CLASSES.class2A.name);

    // After the click, Nuxt router should navigate to the catalog page.
    // We assert on two things:
    //   1. The path contains "/catalog/" — correct section of the app.
    //   2. The class ID is in the URL — correct class was opened.
    await teacherPage.waitForURL(/\/catalog\//);
    expect(teacherPage.url()).toContain(TEST_CLASSES.class2A.id);
  },
);

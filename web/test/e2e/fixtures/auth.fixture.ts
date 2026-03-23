/**
 * auth.fixture.ts
 *
 * Role-based authentication fixtures for Playwright E2E tests.
 *
 * WHAT ARE FIXTURES?
 * ──────────────────
 * Playwright fixtures are a dependency-injection system. Instead of
 * copy-pasting login steps into every test, declare what you need
 * (e.g., `teacherPage`) and Playwright logs in before your test runs.
 *
 * AVAILABLE FIXTURES
 * ──────────────────
 * - adminPage            → logged in as school director (MFA required)
 * - secretaryPage        → logged in as secretary (MFA required)
 * - teacherPage          → logged in as Ana Dumitrescu, primary teacher (MFA required)
 * - teacherMiddlePage    → logged in as Ion Vasilescu, middle school teacher (MFA required)
 * - parentPage           → logged in as parent Ion Moldovan (no MFA)
 * - studentPage          → logged in as student Andrei Moldovan (no MFA)
 * - unassignedTeacherPage → logged in as Dan Pavel, teacher with no classes (MFA required)
 *
 * USAGE
 * ─────
 * import { test, expect } from '../fixtures/auth.fixture';
 *
 * test('teacher sees class cards', async ({ teacherPage }) => {
 *   // teacherPage is already authenticated and on the dashboard
 *   await expect(teacherPage.getByTestId('class-card')).toBeVisible();
 * });
 *
 * CREDENTIALS
 * ───────────
 * All credentials match the seed data in api/db/seed.sql.
 * All passwords: "catalog2026". TOTP secret: "JBSWY3DPEHPK3PXP".
 */

import { test as base, type Page } from '@playwright/test';

import { generateTOTP, TEST_TOTP_SECRET } from '../helpers/totp';

// ── Seed credentials ──────────────────────────────────────────────────────────
// These MUST match api/db/seed.sql exactly. If seed data changes, update here.

export const TEST_USERS = {
  admin: {
    email: 'director@scoala-rebreanu.ro',
    password: 'catalog2026',
    role: 'admin' as const,
    mfaRequired: true,
    name: 'Maria Popescu',
    userId: 'b1000000-0000-0000-0000-000000000001',
  },
  secretary: {
    email: 'secretar@scoala-rebreanu.ro',
    password: 'catalog2026',
    role: 'secretary' as const,
    mfaRequired: true,
    name: 'Elena Ionescu',
    userId: 'b1000000-0000-0000-0000-000000000002',
  },
  teacher: {
    email: 'ana.dumitrescu@scoala-rebreanu.ro',
    password: 'catalog2026',
    role: 'teacher' as const,
    mfaRequired: true,
    name: 'Ana Dumitrescu',
    userId: 'b1000000-0000-0000-0000-000000000010',
    // Teaches: class 2A (primary) — CLR and MEM
  },
  teacherMiddle: {
    email: 'ion.vasilescu@scoala-rebreanu.ro',
    password: 'catalog2026',
    role: 'teacher' as const,
    mfaRequired: true,
    name: 'Ion Vasilescu',
    userId: 'b1000000-0000-0000-0000-000000000011',
    // Teaches: class 6B (middle) — ROM and IST
  },
  parent: {
    email: 'ion.moldovan@gmail.com',
    password: 'catalog2026',
    role: 'parent' as const,
    mfaRequired: false,
    name: 'Ion Moldovan',
    userId: 'b1000000-0000-0000-0000-000000000301',
  },
  student: {
    email: 'andrei.moldovan@elev.rebreanu.ro',
    password: 'catalog2026',
    role: 'student' as const,
    mfaRequired: false,
    name: 'Andrei Moldovan',
    userId: 'b1000000-0000-0000-0000-000000000101',
  },
  unassignedTeacher: {
    email: 'dan.pavel@scoala-rebreanu.ro',
    password: 'catalog2026',
    role: 'teacher' as const,
    mfaRequired: true,
    name: 'Dan Pavel',
    userId: 'b1000000-0000-0000-0000-000000000013',
    // No class assignments — for empty dashboard test
  },
} as const;

// ── Seed entity IDs ───────────────────────────────────────────────────────────
// Commonly referenced in tests for navigation and assertions.

export const TEST_CLASSES = {
  class2A: {
    id: 'f1000000-0000-0000-0000-000000000001',
    name: '2A',
    educationLevel: 'primary',
  },
  class6B: {
    id: 'f1000000-0000-0000-0000-000000000002',
    name: '6B',
    educationLevel: 'middle',
  },
} as const;

// ── Login helper ──────────────────────────────────────────────────────────────

/**
 * performLogin
 *
 * Navigates to /login, enters credentials, handles MFA if required,
 * and waits for the dashboard to load.
 *
 * @param page - Playwright Page instance.
 * @param user - User credentials from TEST_USERS.
 */
async function performLogin(
  page: Page,
  user: (typeof TEST_USERS)[keyof typeof TEST_USERS],
): Promise<void> {
  // Navigate to the login page and wait for DOM to be ready.
  await page.goto('/login');
  await page.getByTestId('email-input').waitFor({ state: 'visible' });

  // Fill email and password using data-testid selectors.
  await page.getByTestId('email-input').fill(user.email);
  await page.getByTestId('password-input').fill(user.password);

  // Submit the login form.
  await page.getByTestId('submit-button').click();

  if (user.mfaRequired) {
    // Wait for the MFA input to appear (API returned mfaRequired: true).
    await page.getByTestId('mfa-input').waitFor({ state: 'visible' });

    // Generate a valid TOTP code from the seeded secret.
    const code = await generateTOTP(TEST_TOTP_SECRET);

    // Fill and submit the MFA form.
    // The MFA form has its own submit button with testid 'mfa-submit-button'
    // (different from the login form's 'submit-button').
    await page.getByTestId('mfa-input').fill(code);
    await page.getByTestId('mfa-submit-button').click();
  }

  // Wait for navigation to the dashboard (successful login redirect).
  // Use a URL pattern that matches exactly '/' but not '/login'.
  await page.waitForURL((url) => url.pathname === '/', { timeout: 15_000 });
}

// ── Fixture type declarations ─────────────────────────────────────────────────

type AppFixtures = {
  /** A Page logged in as the school admin/director. */
  adminPage: Page;
  /** A Page logged in as the school secretary. */
  secretaryPage: Page;
  /** A Page logged in as teacher Ana Dumitrescu (primary, class 2A). */
  teacherPage: Page;
  /** A Page logged in as teacher Ion Vasilescu (middle, class 6B). */
  teacherMiddlePage: Page;
  /** A Page logged in as parent Ion Moldovan. */
  parentPage: Page;
  /** A Page logged in as student Andrei Moldovan. */
  studentPage: Page;
  /** A Page logged in as teacher Dan Pavel (no class assignments). */
  unassignedTeacherPage: Page;
};

// ── Extended test object ──────────────────────────────────────────────────────

export const test = base.extend<AppFixtures>({
  adminPage: async ({ page }, use) => {
    await performLogin(page, TEST_USERS.admin);
    await use(page);
  },

  secretaryPage: async ({ page }, use) => {
    await performLogin(page, TEST_USERS.secretary);
    await use(page);
  },

  teacherPage: async ({ page }, use) => {
    await performLogin(page, TEST_USERS.teacher);
    await use(page);
  },

  teacherMiddlePage: async ({ page }, use) => {
    await performLogin(page, TEST_USERS.teacherMiddle);
    await use(page);
  },

  parentPage: async ({ page }, use) => {
    await performLogin(page, TEST_USERS.parent);
    await use(page);
  },

  studentPage: async ({ page }, use) => {
    await performLogin(page, TEST_USERS.student);
    await use(page);
  },

  unassignedTeacherPage: async ({ page }, use) => {
    await performLogin(page, TEST_USERS.unassignedTeacher);
    await use(page);
  },
});

// Re-export `expect` so test files only need one import line.
export { expect } from '@playwright/test';

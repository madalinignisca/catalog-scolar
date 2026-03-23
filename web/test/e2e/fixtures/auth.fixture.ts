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

// ── API base URL ──────────────────────────────────────────────────────────────
const API_BASE = 'http://localhost:8080/api/v1';

// ── Login helper ──────────────────────────────────────────────────────────────

/**
 * performLogin
 *
 * Authenticates by calling the API directly (not through the UI) and injects
 * the resulting JWT tokens into the browser's localStorage. Then navigates
 * to the dashboard.
 *
 * WHY API-BASED LOGIN?
 * ────────────────────
 * Fixtures need a fast, reliable way to get an authenticated browser session.
 * Driving the login UI in fixtures was unreliable — the MFA form's submit
 * button click intermittently failed to trigger Vue's @submit.prevent handler
 * due to SSR hydration timing. Calling the API directly bypasses all UI
 * interaction issues while still using real authentication (real tokens from
 * the real backend).
 *
 * The login UI itself is thoroughly tested in auth/login.spec.ts (tests 1-9).
 *
 * @param page - Playwright Page instance.
 * @param user - User credentials from TEST_USERS.
 */
async function performLogin(
  page: Page,
  user: (typeof TEST_USERS)[keyof typeof TEST_USERS],
): Promise<void> {
  // Step 1: Call the login API endpoint directly from Node.js.
  const loginResponse = await fetch(`${API_BASE}/auth/login`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ email: user.email, password: user.password }),
  });

  if (!loginResponse.ok) {
    throw new Error(`Login API failed for ${user.email}: ${String(loginResponse.status)}`);
  }

  const loginData = (await loginResponse.json()) as {
    data: {
      access_token?: string;
      refresh_token?: string;
      mfa_required?: boolean;
      mfa_token?: string;
    };
  };

  let accessToken = loginData.data.access_token;
  let refreshToken = loginData.data.refresh_token;

  // Step 2: If MFA is required, call the 2FA verification endpoint.
  if (loginData.data.mfa_required === true && loginData.data.mfa_token !== undefined) {
    const code = await generateTOTP(TEST_TOTP_SECRET);

    const mfaResponse = await fetch(`${API_BASE}/auth/2fa/login`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({
        mfa_token: loginData.data.mfa_token,
        totp_code: code,
      }),
    });

    if (!mfaResponse.ok) {
      throw new Error(`MFA API failed for ${user.email}: ${String(mfaResponse.status)}`);
    }

    const mfaData = (await mfaResponse.json()) as {
      data: { access_token: string; refresh_token: string };
    };
    accessToken = mfaData.data.access_token;
    refreshToken = mfaData.data.refresh_token;
  }

  if (accessToken === undefined || refreshToken === undefined) {
    throw new Error(`No tokens received for ${user.email}`);
  }

  // Step 3: Navigate to the app and inject tokens into localStorage.
  // We must navigate first because localStorage is origin-scoped — we need
  // the page to be on localhost:3000 before we can set localStorage keys.
  await page.goto('/login');

  await page.evaluate(
    ({ access, refresh }) => {
      localStorage.setItem('catalogro_access_token', access);
      localStorage.setItem('catalogro_refresh_token', refresh);
    },
    { access: accessToken, refresh: refreshToken },
  );

  // Step 4: Navigate to the dashboard. The Nuxt app will read the tokens
  // from localStorage and render the authenticated view.
  await page.goto('/');
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

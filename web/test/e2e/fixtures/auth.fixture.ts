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
 * Authenticates a Playwright browser page by calling the login API via the
 * browser's fetch (not Node.js fetch). This ensures the httpOnly cookies
 * set by the API response are stored in the browser's cookie jar.
 *
 * WHY BROWSER-BASED API LOGIN?
 * ────────────────────────────
 * With cookie-based auth (#35), the Go API sets httpOnly cookies on the
 * login response. These cookies are automatically sent with all subsequent
 * requests (via credentials: 'include'). To get these cookies into the
 * Playwright browser context, we must call the login API from within the
 * browser (page.evaluate + fetch), not from Node.js.
 *
 * This approach is simpler and more reliable than the old localStorage
 * injection method:
 *   - No need to navigate to /login first to set up the origin
 *   - No localStorage injection → no race with SSR hydration
 *   - No retry/reinject logic needed — cookies just work
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
  // Step 1: Navigate to the app so the browser has the correct origin.
  // We need to be on localhost:3000 before making cross-origin requests
  // to localhost:8080, so the browser can store the response cookies.
  await page.goto('/login');

  // Step 2: Call the login API from within the browser context.
  // This ensures httpOnly cookies from the response are stored in the
  // browser's cookie jar (not accessible from Node.js, only from the browser).
  const loginResult = await page.evaluate(
    async ({ apiBase, email, password }) => {
      const res = await fetch(`${apiBase}/auth/login`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ email, password }),
        credentials: 'include',
      });
      if (!res.ok) return { ok: false as const, status: res.status };
      const json = (await res.json()) as {
        data: {
          mfa_required?: boolean;
          mfa_token?: string;
        };
      };
      return { ok: true as const, data: json.data };
    },
    { apiBase: API_BASE, email: user.email, password: user.password },
  );

  if (!loginResult.ok) {
    throw new Error(`Login API failed for ${user.email}: ${String(loginResult.status)}`);
  }

  // Step 3: If MFA is required, generate TOTP code and call 2FA endpoint.
  if (loginResult.data.mfa_required === true && loginResult.data.mfa_token !== undefined) {
    const code = await generateTOTP(TEST_TOTP_SECRET);

    const mfaResult = await page.evaluate(
      async ({ apiBase, mfaToken, totpCode }) => {
        const res = await fetch(`${apiBase}/auth/2fa/login`, {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({ mfa_token: mfaToken, totp_code: totpCode }),
          credentials: 'include',
        });
        return { ok: res.ok, status: res.status };
      },
      { apiBase: API_BASE, mfaToken: loginResult.data.mfa_token, totpCode: code },
    );

    if (!mfaResult.ok) {
      throw new Error(`MFA API failed for ${user.email}: ${String(mfaResult.status)}`);
    }
  }

  // Step 4: Navigate to the dashboard. The browser now has httpOnly auth
  // cookies, so the Nuxt SSR request will include them automatically.
  // No localStorage injection needed — cookies are sent with every request.
  await page.goto('/');
  await page
    .waitForURL((url) => url.pathname === '/', { timeout: 15_000 })
    .catch(() => {
      /* swallow — test assertions will catch real issues */
    });
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

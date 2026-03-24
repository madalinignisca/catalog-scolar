# E2E Playwright Test Suite Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build 73 exhaustive E2E Playwright tests (69 active, 4 deferred) covering all 5 user roles, full auth flow with real TOTP, grade CRUD, offline sync, and error handling.

**Architecture:** Full integration tests hitting the real backend. Fresh DB per test run via globalSetup. Real TOTP code generation using `otpauth` library. Page Object Model with role-based auth fixtures. Sequential execution (1 worker) to prevent data races.

**Tech Stack:** Playwright, TypeScript, otpauth (TOTP), Nuxt 3, Go API, PostgreSQL

**Spec:** `docs/superpowers/specs/2026-03-23-e2e-playwright-tests-design.md`

---

## File Map

### Files to Create

| File | Responsibility |
|------|---------------|
| `web/test/e2e/global-setup.ts` | DB reset + health checks before test suite |
| `web/test/e2e/helpers/totp.ts` | TOTP code generation from seeded secrets |
| `web/test/e2e/page-objects/dashboard.page.ts` | Dashboard page interactions |
| `web/test/e2e/page-objects/catalog.page.ts` | Catalog page + grade grid interactions |
| `web/test/e2e/page-objects/grade-input.page.ts` | Grade add/edit modal interactions |
| `web/test/e2e/page-objects/activation.page.ts` | Activation flow interactions |
| `web/test/e2e/page-objects/layout.page.ts` | Sidebar + navigation interactions |
| `web/test/e2e/auth/login.spec.ts` | Auth login tests (replaces existing) |
| `web/test/e2e/auth/token.spec.ts` | Token lifecycle tests |
| `web/test/e2e/auth/activation.spec.ts` | Account activation tests (deferred) |
| `web/test/e2e/auth/access-control.spec.ts` | Role-based access denial |
| `web/test/e2e/dashboard/teacher.spec.ts` | Teacher dashboard tests |
| `web/test/e2e/dashboard/admin.spec.ts` | Admin dashboard tests |
| `web/test/e2e/dashboard/parent.spec.ts` | Parent dashboard tests |
| `web/test/e2e/dashboard/student.spec.ts` | Student dashboard tests |
| `web/test/e2e/navigation/sidebar.spec.ts` | Sidebar nav tests |
| `web/test/e2e/navigation/responsive.spec.ts` | Mobile responsive tests |
| `web/test/e2e/catalog/navigation.spec.ts` | Catalog page structure tests |
| `web/test/e2e/catalog/grade-grid.spec.ts` | Grade display tests |
| `web/test/e2e/catalog/grade-crud.spec.ts` | Grade CRUD tests |
| `web/test/e2e/catalog/grade-edge-cases.spec.ts` | Grade edge case tests |
| `web/test/e2e/sync/offline-mode.spec.ts` | Offline sync tests |
| `web/test/e2e/sync/conflict.spec.ts` | Sync conflict tests |
| `web/test/e2e/error/api-errors.spec.ts` | API error handling tests |
| `web/test/e2e/error/session.spec.ts` | Session management tests |
| `web/test/e2e/edge/empty-states.spec.ts` | Empty state tests |

### Files to Modify

| File | Changes |
|------|---------|
| `api/db/seed.sql` | Add TOTP secrets, student passwords, activation token, unassigned teacher |
| `web/package.json` | Add `otpauth` devDependency |
| `web/playwright.config.ts` | Add `globalSetup` reference |
| `web/test/e2e/fixtures/auth.fixture.ts` | Rewrite with real credentials + TOTP + all role fixtures |
| `web/test/e2e/page-objects/login.page.ts` | Minor enhancement (already complete) |
| `web/pages/index.vue` | Add `data-testid` attributes |
| `web/pages/catalog/[classId].vue` | Add `data-testid` attributes |
| `web/pages/activate/[token].vue` | Add `data-testid` attributes |
| `web/layouts/default.vue` | Add `data-testid` attributes |
| `web/components/catalog/GradeGrid.vue` | Add `data-testid` attributes |
| `web/components/catalog/GradeInput.vue` | Add `data-testid` attributes |
| `web/components/SyncStatus.vue` | Add `data-testid` attributes |

### Files to Delete

| File | Reason |
|------|--------|
| `web/test/e2e/login.spec.ts` | Replaced by `web/test/e2e/auth/login.spec.ts` (moved into auth/ subdirectory) |

---

## Task 1: Update seed data for E2E testing

**Files:**
- Modify: `api/db/seed.sql` (append UPDATE/INSERT blocks after line 279)

- [ ] **Step 1: Add TOTP secrets for MFA-enabled roles**

Append to the end of `api/db/seed.sql` (after the source_mappings section):

```sql
-- ============================================================
-- E2E TEST SUPPORT
-- TOTP secrets, student passwords, activation tokens
-- ============================================================

-- TOTP secrets for MFA-enabled roles (raw base32, no encryption)
-- All staff users share the same test secret: JBSWY3DPEHPK3PXP
-- The otpauth library in E2E tests generates valid codes from this secret
UPDATE users SET totp_secret = 'JBSWY3DPEHPK3PXP'::bytea, totp_enabled = true
WHERE id IN (
    'b1000000-0000-0000-0000-000000000001',  -- admin Maria Popescu
    'b1000000-0000-0000-0000-000000000002',  -- secretary Elena Ionescu
    'b1000000-0000-0000-0000-000000000010',  -- teacher Ana Dumitrescu (primary)
    'b1000000-0000-0000-0000-000000000011',  -- teacher Ion Vasilescu (middle)
    'b1000000-0000-0000-0000-000000000012',  -- teacher Gabriela Marin (middle)
    'b2000000-0000-0000-0000-000000000001',  -- admin Adrian Neagu (School 2)
    'b2000000-0000-0000-0000-000000000010',  -- teacher Mihai Stanescu (School 2)
    'b2000000-0000-0000-0000-000000000011'   -- teacher Laura Georgescu (School 2)
);
```

- [ ] **Step 2: Add password hash for 2 students so they can log in**

Continue appending:

```sql
-- Activate 2 students with password for E2E login tests
-- password: "catalog2026" (same bcrypt hash as all other test users)
UPDATE users SET password_hash = '$2a$10$AgrFyrZVE6ZRRSXt46/eHepzjgYkWMTxQAB7b6QU83l2NnNDrvAXW'
WHERE id IN (
    'b1000000-0000-0000-0000-000000000101',  -- Andrei Moldovan (class 2A, primary)
    'b1000000-0000-0000-0000-000000000201'   -- Alexandru Pop (class 6B, middle)
);
```

- [ ] **Step 3: Set up unactivated student for activation flow tests**

```sql
-- Radu Campean: revert to unactivated state with activation token
-- Used by auth/activation.spec.ts (tests 14-17, currently deferred)
UPDATE users
SET activated_at = NULL,
    password_hash = NULL,
    activation_token = 'test-activation-token-radu',
    activation_sent_at = now()
WHERE id = 'b1000000-0000-0000-0000-000000000205';
```

- [ ] **Step 4: Add unassigned teacher for empty-state test**

```sql
-- Teacher with zero class assignments (for empty dashboard E2E test #71)
INSERT INTO users (id, school_id, role, email, first_name, last_name, password_hash, totp_secret, totp_enabled, activated_at, provisioned_by) VALUES
    ('b1000000-0000-0000-0000-000000000013',
     'a0000000-0000-0000-0000-000000000001',
     'teacher', 'dan.pavel@scoala-rebreanu.ro',
     'Dan', 'Pavel',
     '$2a$10$AgrFyrZVE6ZRRSXt46/eHepzjgYkWMTxQAB7b6QU83l2NnNDrvAXW',
     'JBSWY3DPEHPK3PXP'::bytea, true,
     now(), 'b1000000-0000-0000-0000-000000000002');
```

- [ ] **Step 5: Add a thesis grade for tests 49 and 57**

```sql
-- Thesis grade for Alexandru Pop in ROM (middle school, has_thesis=true)
-- Used by tests 49 (thesis display) and 57 (thesis creation verification)
INSERT INTO grades (school_id, student_id, class_id, subject_id, teacher_id, school_year_id, semester, numeric_grade, is_thesis, grade_date, description) VALUES
    ('a0000000-0000-0000-0000-000000000001',
     'b1000000-0000-0000-0000-000000000201',
     'f1000000-0000-0000-0000-000000000002',
     'f1000000-0000-0000-0000-000000000003',
     'b1000000-0000-0000-0000-000000000011',
     'e0000000-0000-0000-0000-000000000001',
     'I', 7, true, '2027-01-15', 'Teză semestrială');
```

- [ ] **Step 6: Verify seed applies cleanly**

Run:
```bash
cd /home/gabriel/openpublic/catalog-scolar/catalogro
# Drop and recreate DB, then apply migrations and seed
docker compose exec -T postgres psql -U catalogro -c "SELECT 1" && \
  PGPASSWORD=catalogro dropdb -U catalogro -h localhost catalogro --if-exists && \
  PGPASSWORD=catalogro createdb -U catalogro -h localhost catalogro && \
  make migrate && make seed
```

Expected: All commands succeed. No errors.

- [ ] **Step 7: Verify TOTP and student data**

Run:
```bash
docker compose exec -T postgres psql -U catalogro -d catalogro -c \
  "SELECT email, role, totp_enabled, password_hash IS NOT NULL as has_password, activation_token FROM users WHERE school_id = 'a0000000-0000-0000-0000-000000000001' ORDER BY role, email;"
```

Expected: admin/secretary/teachers show `totp_enabled=true`. Andrei Moldovan and Alexandru Pop show `has_password=true`. Radu Campean shows `activation_token='test-activation-token-radu'`. Dan Pavel (teacher) exists with TOTP enabled.

- [ ] **Step 8: Commit**

```bash
git add api/db/seed.sql
git commit -m "feat(seed): add E2E test support data (TOTP, student passwords, activation token)"
```

---

## Task 2: Install otpauth and create TOTP helper

**Files:**
- Modify: `web/package.json` (add devDependency)
- Create: `web/test/e2e/helpers/totp.ts`

- [ ] **Step 1: Install otpauth**

Run:
```bash
cd /home/gabriel/openpublic/catalog-scolar/catalogro/web
npm install --save-dev otpauth
```

Expected: Package added to devDependencies in package.json.

- [ ] **Step 2: Create TOTP helper**

Create `web/test/e2e/helpers/totp.ts`:

```typescript
/**
 * totp.ts
 *
 * Generates valid TOTP codes for E2E authentication tests.
 *
 * WHY THIS EXISTS
 * ───────────────
 * Admin, secretary, and teacher roles require 2FA (TOTP) to log in.
 * The seed data stores a known base32 secret (JBSWY3DPEHPK3PXP) for all
 * MFA-enabled users. This helper generates valid 6-digit codes from that
 * secret at runtime, so auth fixtures can complete the MFA step.
 *
 * TOTP WINDOW RACE MITIGATION
 * ────────────────────────────
 * TOTP codes are valid for 30 seconds. If we generate a code at second 28
 * of the window, it may expire before the form is submitted. The Go API
 * accepts +/- 1 time step (90-second effective window), but to be safe we
 * check the remaining time. If fewer than 5 seconds remain in the current
 * window, we wait for the next window before generating.
 */

import { TOTP, Secret } from 'otpauth';

/**
 * The base32-encoded TOTP secret shared by all MFA-enabled test users.
 * This MUST match the value stored in api/db/seed.sql.
 */
export const TEST_TOTP_SECRET = 'JBSWY3DPEHPK3PXP';

/** TOTP time step in seconds (standard RFC 6238 value). */
const TOTP_PERIOD = 30;

/** Minimum seconds remaining in the current window before we generate. */
const MIN_REMAINING_SECONDS = 5;

/**
 * generateTOTP
 *
 * Returns a valid 6-digit TOTP code for the given secret.
 * If the current time window has fewer than MIN_REMAINING_SECONDS left,
 * waits until the next window to avoid race conditions.
 *
 * @param secret - Base32-encoded TOTP secret. Defaults to TEST_TOTP_SECRET.
 * @returns A 6-character numeric string (e.g., "482913").
 */
export async function generateTOTP(secret: string = TEST_TOTP_SECRET): Promise<string> {
  // Check how many seconds remain in the current 30-second window.
  // TOTP windows align to Unix epoch, so: remaining = period - (now % period)
  const now = Math.floor(Date.now() / 1000);
  const remaining = TOTP_PERIOD - (now % TOTP_PERIOD);

  // If we are too close to the window boundary, wait for the next window.
  // This prevents generating a code that expires before the API validates it.
  if (remaining < MIN_REMAINING_SECONDS) {
    const waitMs = remaining * 1000 + 500; // +500ms safety margin
    await new Promise((resolve) => setTimeout(resolve, waitMs));
  }

  // Create a TOTP instance matching the API's configuration:
  // - SHA1 algorithm (RFC 6238 default, matches pquerna/otp Go library)
  // - 6 digits
  // - 30-second period
  // NOTE: otpauth v9+ requires a Secret object, not a raw string.
  const totp = new TOTP({
    secret: Secret.fromBase32(secret),
    digits: 6,
    period: TOTP_PERIOD,
    algorithm: 'SHA1',
  });

  return totp.generate();
}
```

- [ ] **Step 3: Commit**

```bash
git add web/package.json web/package-lock.json web/test/e2e/helpers/totp.ts
git commit -m "feat(e2e): add otpauth dependency and TOTP code generator helper"
```

---

## Task 3: Create global setup and update Playwright config

**Files:**
- Create: `web/test/e2e/global-setup.ts`
- Modify: `web/playwright.config.ts` (add globalSetup line)

- [ ] **Step 1: Create global setup**

Create `web/test/e2e/global-setup.ts`:

```typescript
/**
 * global-setup.ts
 *
 * Playwright global setup — runs once before the entire test suite.
 *
 * WHAT IT DOES
 * ────────────
 * 1. Resets the database to a known state (drop, create, migrate, seed)
 * 2. Waits for the API server to be healthy
 * 3. Waits for the Nuxt dev server to be ready
 *
 * WHY FRESH DB?
 * ─────────────
 * Tests create/modify data (grades, absences) freely. A fresh database
 * per run ensures no leftover state from previous runs causes failures.
 * The seed data provides known users, classes, and sample grades.
 *
 * PREREQUISITES
 * ─────────────
 * - Docker Compose must be running (database container)
 * - `make dev` should be running (API + Nuxt dev servers)
 * - The globalSetup only resets the DB — it does NOT start servers
 */

import { execFileSync } from 'node:child_process';
import { resolve } from 'node:path';

/** Absolute path to the monorepo root (two levels up from web/test/e2e/). */
const PROJECT_ROOT = resolve(__dirname, '..', '..', '..');

/** Maximum time to wait for a URL to become reachable. */
const HEALTH_CHECK_TIMEOUT_MS = 30_000;
const NUXT_CHECK_TIMEOUT_MS = 60_000;

/**
 * waitForURL
 *
 * Polls a URL until it returns a 2xx status or the timeout is reached.
 * Used to wait for API and Nuxt servers to be ready before running tests.
 *
 * @param url - The URL to poll.
 * @param timeoutMs - Maximum wait time in milliseconds.
 * @throws Error if the URL does not become reachable within the timeout.
 */
async function waitForURL(url: string, timeoutMs: number): Promise<void> {
  const start = Date.now();
  const pollInterval = 1000;

  while (Date.now() - start < timeoutMs) {
    try {
      const response = await fetch(url);
      if (response.ok) return;
    } catch {
      // Server not ready yet — keep polling
    }
    await new Promise((resolve) => setTimeout(resolve, pollInterval));
  }

  throw new Error(`Timed out waiting for ${url} after ${timeoutMs}ms`);
}

/**
 * runCommand
 *
 * Runs a shell command synchronously from the project root.
 * Uses execFileSync with 'make' binary to avoid shell injection.
 *
 * @param binary - The binary to execute (e.g., 'make').
 * @param args - Arguments to pass to the binary.
 */
function runCommand(binary: string, args: string[], env?: NodeJS.ProcessEnv): void {
  console.log(`[global-setup] Running: ${binary} ${args.join(' ')}`);
  execFileSync(binary, args, {
    cwd: PROJECT_ROOT,
    stdio: 'inherit',
    timeout: 120_000,
    env: env ?? process.env,
  });
}

/**
 * globalSetup
 *
 * Playwright calls this function once before any test file runs.
 * It resets the database and waits for servers to be ready.
 */
async function globalSetup(): Promise<void> {
  console.log('[global-setup] Resetting database...');

  // Terminate any active connections to the database before dropping it.
  // Without this, dropdb fails if the API server or other clients are connected.
  // Uses psql with PGPASSWORD to authenticate against the Docker-hosted PostgreSQL.
  const pgEnv = { ...process.env, PGPASSWORD: 'catalogro' };

  try {
    execFileSync('psql', [
      '-U', 'catalogro',
      '-h', 'localhost',
      '-d', 'postgres',
      '-c', "SELECT pg_terminate_backend(pid) FROM pg_stat_activity WHERE datname = 'catalogro' AND pid <> pg_backend_pid();",
    ], { cwd: PROJECT_ROOT, stdio: 'inherit', timeout: 10_000, env: pgEnv });
  } catch {
    // Ignore — database may not exist yet on first run
  }

  // Drop and recreate the database for a clean slate.
  // PGPASSWORD is passed via environment (not command-line) for security.
  execFileSync('dropdb', [
    '-U', 'catalogro',
    '-h', 'localhost',
    '--if-exists',
    'catalogro',
  ], { cwd: PROJECT_ROOT, stdio: 'inherit', timeout: 30_000, env: pgEnv });

  execFileSync('createdb', [
    '-U', 'catalogro',
    '-h', 'localhost',
    'catalogro',
  ], { cwd: PROJECT_ROOT, stdio: 'inherit', timeout: 30_000, env: pgEnv });

  // Run migrations to create the schema.
  runCommand('make', ['migrate']);

  // Load seed data (users, classes, grades, TOTP secrets, etc.).
  runCommand('make', ['seed']);

  console.log('[global-setup] Database reset complete. Waiting for servers...');

  // Wait for the Go API to respond to health checks.
  await waitForURL('http://localhost:8080/api/v1/health', HEALTH_CHECK_TIMEOUT_MS);
  console.log('[global-setup] API server is ready.');

  // Wait for the Nuxt dev server to be ready.
  await waitForURL('http://localhost:3000', NUXT_CHECK_TIMEOUT_MS);
  console.log('[global-setup] Nuxt dev server is ready.');

  console.log('[global-setup] Setup complete. Starting tests...');
}

export default globalSetup;
```

- [ ] **Step 2: Update Playwright config**

In `web/playwright.config.ts`, add the `globalSetup` property inside the `defineConfig({...})` call, right after the opening brace (after line 33):

Add this line after `testDir: 'test/e2e',`:

```typescript
  /**
   * globalSetup: runs once before any test.
   * Resets the database (drop + create + migrate + seed) and waits for
   * API + Nuxt servers to be healthy. See test/e2e/global-setup.ts.
   */
  globalSetup: './test/e2e/global-setup.ts',
```

- [ ] **Step 3: Commit**

```bash
git add web/test/e2e/global-setup.ts web/playwright.config.ts
git commit -m "feat(e2e): add global setup with DB reset and server health checks"
```

---

## Task 4: Rewrite auth fixtures with real credentials and TOTP

**Files:**
- Modify: `web/test/e2e/fixtures/auth.fixture.ts` (full rewrite)

- [ ] **Step 1: Rewrite auth fixture**

Replace the entire contents of `web/test/e2e/fixtures/auth.fixture.ts`:

```typescript
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
 * - adminPage       → logged in as school director (MFA required)
 * - secretaryPage   → logged in as secretary (MFA required)
 * - teacherPage     → logged in as Ana Dumitrescu, primary teacher (MFA required)
 * - teacherMiddlePage → logged in as Ion Vasilescu, middle school teacher (MFA required)
 * - parentPage      → logged in as parent Ion Moldovan (no MFA)
 * - studentPage     → logged in as student Andrei Moldovan (no MFA)
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
  // Navigate to the login page.
  await page.goto('/login');

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
    await page.getByTestId('mfa-input').fill(code);
    await page.getByTestId('submit-button').click();
  }

  // Wait for navigation to the dashboard (successful login redirect).
  await page.waitForURL('/', { timeout: 10_000 });
}

// ── Fixture type declarations ─────────────────────────────────────────────────

type AppFixtures = {
  adminPage: Page;
  secretaryPage: Page;
  teacherPage: Page;
  teacherMiddlePage: Page;
  parentPage: Page;
  studentPage: Page;
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

export { expect } from '@playwright/test';
```

- [ ] **Step 2: Commit**

```bash
git add web/test/e2e/fixtures/auth.fixture.ts
git commit -m "feat(e2e): rewrite auth fixtures with real seed credentials and TOTP"
```

---

## Task 5: Add data-testid attributes to Vue pages and components

**Files:**
- Modify: `web/pages/index.vue`
- Modify: `web/pages/catalog/[classId].vue`
- Modify: `web/pages/activate/[token].vue`
- Modify: `web/layouts/default.vue`
- Modify: `web/components/catalog/GradeGrid.vue`
- Modify: `web/components/catalog/GradeInput.vue`
- Modify: `web/components/SyncStatus.vue`

This task adds `data-testid` attributes to all elements that E2E tests need to interact with. The login page already has them — we are extending the same pattern to the rest of the app.

**Important:** Read each file before modifying. The exact elements to target are described below, but line numbers may shift. Use the element's existing CSS classes or surrounding context to locate the correct insertion point.

- [ ] **Step 1: Add testids to dashboard (index.vue)**

Read `web/pages/index.vue`. Add `data-testid` attributes to:

| Element | Testid | How to find it |
|---------|--------|---------------|
| Loading spinner container | `dashboard-loading` | The `v-if="isLoading"` div with `animate-spin` |
| Error banner | `dashboard-error` | The `v-else-if="error"` div with `bg-red-50` |
| Teacher class card (each) | `class-card` | The `v-for` loop NuxtLink with `group rounded-xl` |
| Class name heading | `class-card-name` | The `h3` inside each class card |
| Student count | `class-card-student-count` | The span showing student count |
| Admin card (each) | `admin-card` | Admin dashboard card elements |
| Welcome message | `welcome-message` | The fallback text for student/other roles |
| Dashboard content | `dashboard-content` | The main container after loading |

- [ ] **Step 2: Add testids to catalog page (catalog/[classId].vue)**

Read `web/pages/catalog/[classId].vue`. Add `data-testid` attributes to:

| Element | Testid | How to find it |
|---------|--------|---------------|
| Loading skeleton | `catalog-loading` | The `v-if="isLoading"` div with `animate-pulse` |
| Error banner | `catalog-error` | The error div with `bg-red-50` |
| Back link | `back-link` | The `NuxtLink` with "Inapoi" text |
| Class title | `class-title` | The `h1` with `text-2xl font-bold` |
| Education level badge | `education-level-badge` | The badge span next to class name |
| Student count | `catalog-student-count` | The span showing student count |
| Semester I button | `semester-I` | The "Semestrul I" button |
| Semester II button | `semester-II` | The "Semestrul II" button |
| Subject tab (each) | `subject-tab` | Each `role="tab"` button |
| Grade grid container | `grade-grid-container` | The container wrapping the GradeGrid component |

- [ ] **Step 3: Add testids to layout (default.vue)**

Read `web/layouts/default.vue`. Add `data-testid` attributes to:

| Element | Testid | How to find it |
|---------|--------|---------------|
| Mobile hamburger button | `mobile-menu-button` | The button with SVG hamburger icon |
| Sidebar | `sidebar` | The `aside` element |
| Sidebar overlay/backdrop | `sidebar-overlay` | The fixed overlay div |
| Nav items | `nav-item` | Each `NuxtLink` in the nav section |
| School name | `school-name` | The `h2` in the sidebar header |
| User name | `user-name` | The `p` showing first + last name |
| User role label | `user-role` | The `p` showing role text |
| Logout button | `logout-button` | The button with "Iesire" text |

- [ ] **Step 4: Add testids to GradeGrid component**

Read `web/components/catalog/GradeGrid.vue`. Add `data-testid` attributes to:

| Element | Testid | How to find it |
|---------|--------|---------------|
| Grid table | `grade-grid` | The `table` element |
| Student row (each) | `student-row` | Each `tr` in the `v-for` loop |
| Student name cell | `student-name` | The `td` with student name text |
| Grade badge (each) | `grade-badge` | Each grade span/button in the row |
| Average cell | `student-average` | The `td` showing the computed average |
| Add grade button | `add-grade-button` | The "+" button per student row |
| Delete grade button | `delete-grade-button` | The delete button shown on hover |
| Loading skeleton | `grade-grid-loading` | The skeleton placeholder |
| Empty state | `grade-grid-empty` | The "no students" message |
| Error banner | `grade-grid-error` | The error message div |

- [ ] **Step 5: Add testids to GradeInput modal**

Read `web/components/catalog/GradeInput.vue`. Add `data-testid` attributes to:

| Element | Testid | How to find it |
|---------|--------|---------------|
| Modal backdrop | `grade-modal-backdrop` | The fixed overlay button |
| Modal dialog | `grade-modal` | The dialog container div |
| Modal title | `grade-modal-title` | The `h2` with title text |
| Student name | `grade-modal-student` | The `p` showing student name |
| Qualifier button (each) | `qualifier-{FB\|B\|S\|I}` | Each qualifier selection button |
| Numeric input | `grade-numeric-input` | The `input#grade-numeric` element |
| Date input | `grade-date-input` | The `input#grade-date` element |
| Description input | `grade-description-input` | The `input#grade-description` element |
| Thesis checkbox | `grade-thesis-checkbox` | The thesis toggle (if present) |
| Validation error | `grade-validation-error` | The error message div |
| Cancel button | `grade-cancel-button` | The "Anuleaza" button |
| Save button | `grade-save-button` | The submit button |

- [ ] **Step 6: Add testids to SyncStatus component**

Read `web/components/SyncStatus.vue`. Add `data-testid` attributes to:

| Element | Testid | How to find it |
|---------|--------|---------------|
| Status container | `sync-status` | The root container |
| Status dot | `sync-status-dot` | The colored dot span |
| Status label | `sync-status-label` | The text label span |

- [ ] **Step 7: Add testids to activation page**

Read `web/pages/activate/[token].vue`. Add `data-testid` attributes to:

| Element | Testid | How to find it |
|---------|--------|---------------|
| Loading state | `activate-loading` | The loading div |
| Error state | `activate-error` | The error div |
| Identity confirmation | `activate-identity` | The blue info box with user data |
| Password input | `activate-password` | The password field |
| Password confirm | `activate-password-confirm` | The confirmation field |
| GDPR checkbox | `activate-gdpr` | The consent checkbox |
| Submit button | `activate-submit` | The activation submit button |
| Success state | `activate-success` | The success message div |

- [ ] **Step 8: Run Nuxt dev to verify no compilation errors**

Run:
```bash
cd /home/gabriel/openpublic/catalog-scolar/catalogro/web && npm run build 2>&1 | tail -20
```

Expected: Build succeeds. No template compilation errors.

- [ ] **Step 9: Commit**

```bash
git add web/pages/ web/layouts/ web/components/
git commit -m "feat(ui): add data-testid attributes for E2E test selectors"
```

---

## Task 6: Create page objects

**Files:**
- Create: `web/test/e2e/page-objects/dashboard.page.ts`
- Create: `web/test/e2e/page-objects/catalog.page.ts`
- Create: `web/test/e2e/page-objects/grade-input.page.ts`
- Create: `web/test/e2e/page-objects/activation.page.ts`
- Create: `web/test/e2e/page-objects/layout.page.ts`

Each page object follows the pattern established in `login.page.ts`: constructor takes a Playwright `Page`, locators defined as readonly properties via `page.getByTestId()`, methods return typed values.

- [ ] **Step 1: Create DashboardPage**

Create `web/test/e2e/page-objects/dashboard.page.ts`. Methods:
- `getClassCards()` → returns locator for all `[data-testid="class-card"]` elements
- `getClassCardByName(name: string)` → finds card containing the given class name text
- `clickClassCard(name: string)` → clicks the class card with the given name
- `getAdminCards()` → returns locator for admin dashboard cards
- `getWelcomeMessage()` → returns text of the welcome message
- `isLoading()` → checks if loading spinner is visible
- `getError()` → returns error text or null

- [ ] **Step 2: Create CatalogPage**

Create `web/test/e2e/page-objects/catalog.page.ts`. Methods:
- `goto(classId: string)` → navigates to `/catalog/{classId}`
- `getClassTitle()` → returns class name text from header
- `getEducationLevel()` → returns education level badge text
- `getStudentCount()` → returns student count text
- `selectSemester(semester: 'I' | 'II')` → clicks the semester button
- `getSubjectTabs()` → returns locator for all subject tabs
- `clickSubjectTab(name: string)` → clicks a subject tab by name
- `getStudentRows()` → returns locator for all student rows in the grid
- `getStudentName(rowIndex: number)` → returns student name from a row
- `getGradeBadges(studentName: string)` → returns grade badges for a student
- `clickAddGrade(studentName: string)` → clicks the add button for a student
- `clickGradeBadge(studentName: string, badgeIndex: number)` → clicks a specific grade
- `getAverage(studentName: string)` → returns average value text
- `goBack()` → clicks the back link
- `isLoading()` → checks loading state
- `getError()` → returns error text or null
- `getEmptyState()` → returns empty state text or null

- [ ] **Step 3: Create GradeInputModal**

Create `web/test/e2e/page-objects/grade-input.page.ts`. Methods:
- `isVisible()` → checks if modal is shown
- `getTitle()` → returns modal title text
- `getStudentName()` → returns student name shown in modal
- `selectQualifier(q: 'FB' | 'B' | 'S' | 'I')` → clicks a qualifier button
- `fillNumericGrade(n: number)` → fills the numeric grade input
- `setDate(date: string)` → fills the date input (YYYY-MM-DD format)
- `fillDescription(text: string)` → fills the description field
- `toggleThesis()` → toggles the thesis checkbox (if present)
- `save()` → clicks the save button
- `cancel()` → clicks the cancel button
- `close()` → clicks the backdrop to dismiss
- `getValidationError()` → returns validation error text or null

- [ ] **Step 4: Create ActivationPage**

Create `web/test/e2e/page-objects/activation.page.ts`. Methods:
- `goto(token: string)` → navigates to `/activate/{token}`
- `isLoading()` → checks loading state
- `getError()` → returns error text or null
- `getUserInfo()` → returns identity confirmation text
- `fillPassword(pw: string)` → fills password field
- `fillPasswordConfirm(pw: string)` → fills confirmation field
- `acceptGdpr()` → checks the GDPR checkbox
- `submit()` → clicks the submit button
- `getSuccessMessage()` → returns success text or null

- [ ] **Step 5: Create LayoutPage**

Create `web/test/e2e/page-objects/layout.page.ts`. Methods:
- `getSidebarItems()` → returns locator for all nav items
- `getActiveNavItem()` → returns the currently highlighted nav item text
- `clickNavItem(label: string)` → clicks a nav item by label text
- `getUserName()` → returns the user name text from sidebar footer
- `getUserRole()` → returns the user role label text
- `getSchoolName()` → returns the school name from sidebar header
- `clickLogout()` → clicks the logout button
- `isHamburgerVisible()` → checks if the mobile menu button is visible
- `openMobileMenu()` → clicks the hamburger button
- `closeMobileMenu()` → clicks the overlay/close button
- `isSidebarVisible()` → checks if the sidebar is visible
- `getSyncStatus()` → returns the sync status text

- [ ] **Step 6: Commit**

```bash
git add web/test/e2e/page-objects/
git commit -m "feat(e2e): add page objects for dashboard, catalog, grade-input, activation, layout"
```

---

## Task 7: Write auth login tests (tests 1-10)

**Files:**
- Delete: `web/test/e2e/login.spec.ts`
- Create: `web/test/e2e/auth/login.spec.ts`

- [ ] **Step 1: Delete old login spec**

```bash
rm web/test/e2e/login.spec.ts
```

- [ ] **Step 2: Create auth/login.spec.ts**

Create `web/test/e2e/auth/login.spec.ts` with 10 tests:

1. `login page renders` — verify email, password, submit visible
2. `empty form shows validation` — submit without filling, stays on /login
3. `invalid credentials show error` — wrong password, error message visible
4. `parent login succeeds without MFA` — parent credentials, redirect to /
5. `student login succeeds without MFA` — student credentials, redirect to /
6. `teacher login shows MFA then succeeds` — teacher login, MFA input appears, TOTP code, redirect
7. `teacher login with invalid TOTP shows error` — wrong code, MFA error visible
8. `admin login with MFA succeeds` — admin credentials + TOTP, redirect
9. `secretary login with MFA succeeds` — secretary credentials + TOTP, redirect
10. `logout clears session and redirects` — use parentPage fixture, click logout, verify redirect to /login

Import `LoginPage` from page objects. For tests 4-9, perform login manually (not via fixture) to test the flow itself. For test 10, use the `parentPage` fixture. Import `generateTOTP` from helpers for MFA tests.

- [ ] **Step 3: Run the tests**

Run:
```bash
cd /home/gabriel/openpublic/catalog-scolar/catalogro/web && npx playwright test auth/login.spec.ts --reporter=list
```

Expected: All 10 tests pass.

- [ ] **Step 4: Commit**

```bash
git add -A web/test/e2e/auth/login.spec.ts
git rm web/test/e2e/login.spec.ts 2>/dev/null || true
git commit -m "feat(e2e): auth login tests — all 5 roles + MFA + logout (tests 1-10)"
```

---

## Task 8: Write auth token, activation, and access control tests (tests 11-20)

**Files:**
- Create: `web/test/e2e/auth/token.spec.ts`
- Create: `web/test/e2e/auth/activation.spec.ts`
- Create: `web/test/e2e/auth/access-control.spec.ts`

- [ ] **Step 1: Create token.spec.ts (tests 11-13)**

Tests:
- Test 11: Log in as parent, manually tamper `catalogro_access_token` in localStorage to an expired JWT, navigate to `/`, verify page still loads (silent refresh happened)
- Test 12: Log in as parent, clear both tokens from localStorage, navigate to `/`, verify redirect to `/login`
- Test 13: Fresh page (no fixture), navigate to `/`, verify redirect to `/login`

Use `page.evaluate()` to manipulate localStorage.

- [ ] **Step 2: Create activation.spec.ts (tests 14-17, all test.skip)**

Create the file with 4 `test.skip` blocks. Include TODO comments explaining these are deferred until activation API endpoints are implemented. Structure the test bodies as comments showing the intended implementation.

- [ ] **Step 3: Create access-control.spec.ts (tests 18-20)**

Tests:
- Test 18: Use `parentPage` fixture, navigate to `/catalog/f1000000-0000-0000-0000-000000000001`, verify error or redirect (parent should not access teacher catalog)
- Test 19: Use `studentPage` fixture, navigate to an admin-only route, verify access denied
- Test 20: Use `teacherPage` fixture (Ana, primary only), navigate to `/catalog/f1000000-0000-0000-0000-000000000002` (class 6B, not her class), verify empty or denied

- [ ] **Step 4: Run the tests**

```bash
cd /home/gabriel/openpublic/catalog-scolar/catalogro/web && npx playwright test auth/ --reporter=list
```

Expected: 16 active tests pass, 4 skipped (activation).

- [ ] **Step 5: Commit**

```bash
git add web/test/e2e/auth/
git commit -m "feat(e2e): token lifecycle, activation (deferred), and access control tests (tests 11-20)"
```

---

## Task 9: Write dashboard tests (tests 21-29)

**Files:**
- Create: `web/test/e2e/dashboard/teacher.spec.ts`
- Create: `web/test/e2e/dashboard/admin.spec.ts`
- Create: `web/test/e2e/dashboard/parent.spec.ts`
- Create: `web/test/e2e/dashboard/student.spec.ts`

- [ ] **Step 1: Create teacher.spec.ts (tests 21-23)**

Use `teacherPage` fixture. Tests:
- Test 21: Verify class cards are visible, count matches teacher's assignments (Ana teaches 2A)
- Test 22: Verify class card content: name "2A", education level "primary", student count "5", subjects list
- Test 23: Click class card, verify navigation to `/catalog/f1000000-...0001`

- [ ] **Step 2: Create admin.spec.ts (tests 24-25)**

Use `adminPage` fixture. Tests:
- Test 24: Verify admin quick-access cards are visible (user management, classes, reports)
- Test 25: Click an admin card, verify navigation to the correct route

- [ ] **Step 3: Create parent.spec.ts (tests 26-27)**

Use `parentPage` fixture. Tests:
- Test 26: Verify "My children" section or linked student info is shown
- Test 27: Verify teacher class grid and admin cards are NOT visible

- [ ] **Step 4: Create student.spec.ts (tests 28-29)**

Use `studentPage` fixture. Tests:
- Test 28: Verify welcome message contains student's name ("Andrei")
- Test 29: Verify class management / admin features are NOT visible

- [ ] **Step 5: Run dashboard tests**

```bash
cd /home/gabriel/openpublic/catalog-scolar/catalogro/web && npx playwright test dashboard/ --reporter=list
```

Expected: All 9 tests pass.

- [ ] **Step 6: Commit**

```bash
git add web/test/e2e/dashboard/
git commit -m "feat(e2e): dashboard tests for all 4 roles (tests 21-29)"
```

---

## Task 10: Write navigation and responsive tests (tests 30-37)

**Files:**
- Create: `web/test/e2e/navigation/sidebar.spec.ts`
- Create: `web/test/e2e/navigation/responsive.spec.ts`

- [ ] **Step 1: Create sidebar.spec.ts (tests 30-33)**

Use `teacherPage` fixture and `LayoutPage` page object. Tests:
- Test 30: Verify sidebar contains 3 nav items: "Tablou de bord", "Catalog", "Absente"
- Test 31: Verify the active (highlighted) nav item matches the current route
- Test 32: Verify user info in sidebar footer shows "Ana Dumitrescu" and role label
- Test 33: Click logout button, verify redirect to `/login` and session cleared

- [ ] **Step 2: Create responsive.spec.ts (tests 34-37)**

Use `teacherPage` fixture. Set viewport to mobile size (375x667). Tests:
- Test 34: Verify sidebar is hidden, hamburger button is visible
- Test 35: Click hamburger, verify sidebar overlay opens
- Test 36: Click backdrop/close button, verify sidebar dismisses
- Test 37: Open mobile menu, click a nav item, verify page navigates correctly

Use `page.setViewportSize({ width: 375, height: 667 })` in `test.beforeEach`.

- [ ] **Step 3: Run navigation tests**

```bash
cd /home/gabriel/openpublic/catalog-scolar/catalogro/web && npx playwright test navigation/ --reporter=list
```

Expected: All 8 tests pass.

- [ ] **Step 4: Commit**

```bash
git add web/test/e2e/navigation/
git commit -m "feat(e2e): sidebar navigation and mobile responsive tests (tests 30-37)"
```

---

## Task 11: Write catalog navigation and grade display tests (tests 38-50)

**Files:**
- Create: `web/test/e2e/catalog/navigation.spec.ts`
- Create: `web/test/e2e/catalog/grade-grid.spec.ts`

- [ ] **Step 1: Create navigation.spec.ts (tests 38-43)**

Use `teacherPage` fixture and `CatalogPage` page object. Navigate to class 2A in beforeEach. Tests:
- Test 38: Verify class header shows "2A", education level, student count "5", back link
- Test 39: Verify semester toggle defaults to "Semestrul I" (active state)
- Test 40: Click "Semestrul II", verify grade grid reloads (may show empty)
- Test 41: Verify subject tabs show CLR and MEM (Ana's assigned subjects for 2A)
- Test 42: Click a subject tab, verify grade grid content changes
- Test 43: Click back link, verify redirect to dashboard `/`

- [ ] **Step 2: Create grade-grid.spec.ts (tests 44-50)**

Tests 44-46, 48-50 use `teacherPage` fixture (primary, class 2A).
Test 47 uses `teacherMiddlePage` fixture (middle, class 6B).

In each test, navigate to the appropriate catalog page and select a subject.

- Test 44: Verify students sorted alphabetically by last name (Crisan, Luca, Moldovan, Muresan, Toma)
- Test 45: Verify each row has: row number, student name, grade badges area, average column, add button
- Test 46: Verify qualifier badges show correct colors (FB=green, B=blue — from seed data)
- Test 47: (teacherMiddlePage) Navigate to 6B/ROM, verify numeric badges with correct colors (9=green, 8=blue, 7=blue — from seed data)
- Test 48: Hover over a grade badge, verify tooltip shows date and description
- Test 49: If seed data contains thesis grades, verify "T" prefix and purple ring. Otherwise, test.skip with note.
- Test 50: Use `page.route()` to delay API response, verify skeleton loading animation appears

- [ ] **Step 3: Run catalog tests**

```bash
cd /home/gabriel/openpublic/catalog-scolar/catalogro/web && npx playwright test catalog/ --reporter=list
```

Expected: All 13 tests pass (or some skipped if thesis data not seeded).

- [ ] **Step 4: Commit**

```bash
git add web/test/e2e/catalog/
git commit -m "feat(e2e): catalog navigation and grade display tests (tests 38-50)"
```

---

## Task 12: Write grade CRUD and edge case tests (tests 51-59)

**Files:**
- Create: `web/test/e2e/catalog/grade-crud.spec.ts`
- Create: `web/test/e2e/catalog/grade-edge-cases.spec.ts`

- [ ] **Step 1: Create grade-crud.spec.ts (tests 51-56)**

Tests 51, 53-55 use `teacherPage` (primary, class 2A/CLR).
Tests 52, 56 use `teacherMiddlePage` (middle, class 6B/ROM).

Use `GradeInputModal` page object for modal interactions.

- Test 51: Navigate to 2A/CLR. Click add grade for a student. Select "FB" qualifier. Set date. Save. Verify grade badge appears in the grid.
- Test 52: (teacherMiddlePage) Navigate to 6B/ROM. Click add grade. Enter numeric 8. Set date. Save. Verify numeric badge appears.
- Test 53: Click an existing grade badge. Verify modal opens pre-filled. Change qualifier. Save. Verify grid updates.
- Test 54: Hover a grade badge. Click delete button. Confirm deletion. Verify badge removed from grid.
- Test 55: Open add grade modal. Try saving without required fields. Verify validation error appears. Try numeric value outside 1-10 range. Verify validation.
- Test 56: (teacherMiddlePage) Navigate to 6B/ROM. Note current average for Alexandru Pop. Add a grade. Verify average recalculates.

- [ ] **Step 2: Create grade-edge-cases.spec.ts (tests 57-59)**

Use `teacherPage` fixture (or `teacherMiddlePage` where thesis subjects exist).

- Test 57: Add a grade with thesis flag enabled (if subject has_thesis). Verify "T" prefix on the badge. If no thesis-capable subject available for primary teacher, use `teacherMiddlePage` with ROM (has_thesis=true).
- Test 58: Verify a student with multiple existing grades (from seed) shows all badges in sequence.
- Test 59: Add a grade without description. Verify it saves successfully and appears in the grid.

- [ ] **Step 3: Run CRUD tests**

```bash
cd /home/gabriel/openpublic/catalog-scolar/catalogro/web && npx playwright test catalog/ --reporter=list
```

Expected: All catalog tests pass (navigation + grid + crud + edge cases).

- [ ] **Step 4: Commit**

```bash
git add web/test/e2e/catalog/
git commit -m "feat(e2e): grade CRUD and edge case tests (tests 51-59)"
```

---

## Task 13: Write offline sync tests (tests 60-65)

**Files:**
- Create: `web/test/e2e/sync/offline-mode.spec.ts`
- Create: `web/test/e2e/sync/conflict.spec.ts`

- [ ] **Step 1: Create offline-mode.spec.ts (tests 60-64)**

Use `teacherPage` fixture. Navigate to a catalog page (class 2A/CLR) in beforeEach.

Simulate offline/online using Playwright's `page.context().setOffline(true/false)`.

- Test 60: When online with no pending mutations, verify SyncStatus shows green dot and "Sincronizat" text.
- Test 61: Set offline via `page.context().setOffline(true)`. Verify SyncStatus changes to yellow "Offline".
- Test 62: While offline, add a grade (click add, fill, save). Verify: grade appears optimistically in grid AND SyncStatus shows "Sincronizare (1)".
- Test 63: Go back online via `page.context().setOffline(false)`. Wait for sync to complete. Verify SyncStatus returns to green "Sincronizat".
- Test 64: While offline, add 3 grades. Verify SyncStatus count increments to 3.

- [ ] **Step 2: Create conflict.spec.ts (test 65)**

Use `teacherPage` fixture.

- Test 65: Go offline. Add a grade. Go back online. Wait for sync. Reload the page (`page.reload()`). Verify the grade persists after reload (it was successfully synced to the server).

- [ ] **Step 3: Run sync tests**

```bash
cd /home/gabriel/openpublic/catalog-scolar/catalogro/web && npx playwright test sync/ --reporter=list
```

Expected: All 6 tests pass.

- [ ] **Step 4: Commit**

```bash
git add web/test/e2e/sync/
git commit -m "feat(e2e): offline sync and conflict resolution tests (tests 60-65)"
```

---

## Task 14: Write error state and session tests (tests 66-70)

**Files:**
- Create: `web/test/e2e/error/api-errors.spec.ts`
- Create: `web/test/e2e/error/session.spec.ts`

- [ ] **Step 1: Create api-errors.spec.ts (tests 66-68)**

Use `teacherPage` fixture for tests 66-67. Use `page.route()` to intercept API calls.

- Test 66: Use `page.route('**/catalog/classes/*/subjects/*/grades*', route => route.fulfill({ status: 500, body: 'Internal Server Error' }))`. Navigate to catalog page. Verify error banner appears in the grade grid area.
- Test 67: Use `page.route('**/catalog/grades', route => route.fulfill({ status: 403, body: JSON.stringify({ error: 'forbidden' }) }))`. Try to add a grade. Verify error message appears.
- Test 68: Navigate to `/login`. Use `page.route('**/auth/login', route => route.abort('timedout'))`. Fill credentials, submit. Verify error message appears (not infinite spinner).

- [ ] **Step 2: Create session.spec.ts (tests 69-70)**

- Test 69: Log in as parent (via fixture). Use `page.evaluate()` to clear both tokens from localStorage. Navigate to a page (e.g., click a nav item). Verify redirect to `/login`.
- Test 70: Log in as parent (via fixture). Use `page.evaluate()` to replace `catalogro_access_token` with an expired JWT string (any invalid string). Navigate to `/`. The API wrapper should attempt a silent refresh using the valid refresh token. Verify the page loads successfully (refresh worked).

- [ ] **Step 3: Run error tests**

```bash
cd /home/gabriel/openpublic/catalog-scolar/catalogro/web && npx playwright test error/ --reporter=list
```

Expected: All 5 tests pass.

- [ ] **Step 4: Commit**

```bash
git add web/test/e2e/error/
git commit -m "feat(e2e): API error handling and session management tests (tests 66-70)"
```

---

## Task 15: Write empty/edge state tests (tests 71-73)

**Files:**
- Create: `web/test/e2e/edge/empty-states.spec.ts`

- [ ] **Step 1: Create empty-states.spec.ts (tests 71-73)**

- Test 71: Use `unassignedTeacherPage` fixture (Dan Pavel, no class assignments). Verify dashboard shows an empty state (no class cards, possibly an informational message).
- Test 72: Use `teacherPage` fixture. Navigate to class 2A. Select a subject. Switch to Semester II (which has no seed data). Verify the grade grid shows student names but no grade badges (empty grid, not an error).
- Test 73: Same as test 72 but verify specifically that no error banner appears — just an empty table with student rows.

- [ ] **Step 2: Run edge tests**

```bash
cd /home/gabriel/openpublic/catalog-scolar/catalogro/web && npx playwright test edge/ --reporter=list
```

Expected: All 3 tests pass.

- [ ] **Step 3: Commit**

```bash
git add web/test/e2e/edge/
git commit -m "feat(e2e): empty state and edge case tests (tests 71-73)"
```

---

## Task 16: Run full test suite and verify

- [ ] **Step 1: Run the complete E2E suite**

```bash
cd /home/gabriel/openpublic/catalog-scolar/catalogro/web && npx playwright test --reporter=list 2>&1
```

Expected: 69 tests passed, 4 skipped (activation). Total time under 10 minutes.

- [ ] **Step 2: Fix any failures**

If tests fail, investigate and fix. Common issues:
- Timing: increase `waitFor` timeouts or add `page.waitForLoadState('networkidle')`
- Selectors: verify `data-testid` attributes were added correctly in Vue files
- Data: verify seed data was applied (check globalSetup output)
- TOTP: if MFA tests fail intermittently, check the TOTP window race mitigation

- [ ] **Step 3: Final commit**

If any fixes were needed, stage only the specific files that changed:

```bash
git add web/test/e2e/ web/pages/ web/components/ web/layouts/
git commit -m "fix(e2e): address test suite issues from full run"
```

- [ ] **Step 4: Run once more to confirm stability**

```bash
cd /home/gabriel/openpublic/catalog-scolar/catalogro/web && npx playwright test --reporter=list 2>&1
```

Expected: Same results — 69 passed, 4 skipped. No flaky tests.

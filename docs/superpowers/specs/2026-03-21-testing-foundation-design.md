# Testing Foundation — Design Spec

**Date:** 2026-03-21
**Status:** Draft
**Scope:** Test infrastructure, helpers, and example tests for Go API, Nuxt frontend, and E2E. No handler implementation.

## Overview

Build the testing foundation for the CatalogRO monorepo across three layers: Go integration tests (testcontainers-go + PostgreSQL), Nuxt unit tests (Vitest + happy-dom), and E2E tests (Playwright). Establishes reusable helpers, fixture patterns, and example tests that serve as templates for all future TDD development.

## Decisions

| Decision | Choice | Rationale |
|----------|--------|-----------|
| DB strategy for Go tests | Shared container, truncate between tests | RLS policies require committed data — transaction rollback makes RLS untestable |
| E2E stack (local) | Real API + DB, frontend dev server | Full-stack E2E catches integration bugs that mocked APIs miss |
| E2E stack (CI) | All services via docker-compose | Reproducible, no external dependencies |
| Example test scope | Schema + RLS + SIIIR parser | Tests code that exists today. Handler tests come when handlers are built via TDD |
| Code comments | Verbose, junior-friendly, PM-readable | Explicit user requirement — every function, type, and non-trivial block explained in plain language |

## Design

### 1. Go Integration Test Foundation

#### 1.1 Dependencies

Add to `api/go.mod`:
- `github.com/testcontainers/testcontainers-go` — programmatic Docker containers for tests
- `github.com/testcontainers/testcontainers-go/modules/postgres` — PostgreSQL module with health checks
- `github.com/google/uuid` — UUID generation for test data (not currently in go.mod, must be explicitly added)

#### 1.2 Test Helper Package: `api/internal/testutil/`

**`container.go`** — PostgreSQL container lifecycle:
- `StartPostgres(t *testing.T) *pgxpool.Pool` — starts a PostgreSQL 17 container via testcontainers-go, runs all goose migrations from `db/migrations/`, then creates a non-superuser role (`CREATE ROLE catalogro_app LOGIN PASSWORD 'test'; GRANT ALL ON ALL TABLES IN SCHEMA public TO catalogro_app;`) for RLS testing. Returns a connection pool (connected as superuser for setup/teardown). Container is shared per test package using `sync.Once`. Container is destroyed via `t.Cleanup()` when the package finishes.
- `TruncateAll(t *testing.T, pool *pgxpool.Pool)` — truncates all application tables in reverse foreign-key order using `TRUNCATE ... CASCADE`. Called in `t.Cleanup()` or explicitly between tests to ensure isolation. Does NOT truncate `districts` or `schools` tables by default (these are shared reference data). Note: `refresh_tokens` will be cascade-deleted when `users` is truncated (FK dependency).

**Database role strategy for RLS testing:** The test container connects as the default `postgres` superuser for all setup/teardown operations (seeding, truncating). Superusers bypass RLS, which is correct for setup. For **RLS verification tests** (`TestRLSIsolation`), the test must create a non-superuser role (e.g., `catalogro_app`) matching the application role, and use `SET ROLE catalogro_app` on the test connection before querying. Without this, RLS tests would pass trivially even if policies are broken. The `container.go` helper should create this role during migration setup.

**`seed.go`** — test data factories:
- `SeedSchools(t *testing.T, pool *pgxpool.Pool) (school1ID, school2ID uuid.UUID)` — inserts 2 test schools with districts and school years. Returns their UUIDs for use in other seed functions.
- `SeedUsers(t *testing.T, pool *pgxpool.Pool, schoolID uuid.UUID) map[string]uuid.UUID` — inserts one user per role (admin, secretary, teacher, parent, student) for a given school. Returns `map[role]userID`.
- `SeedClass(t *testing.T, pool *pgxpool.Pool, schoolID, teacherID uuid.UUID) uuid.UUID` — inserts a class with the teacher as diriginte, one subject, and one enrollment. Returns classID.
- All seed functions use deterministic UUIDs derived from the school ID for predictability.

**`tenant.go`** — RLS context helpers:
- `SetTenantOnConn(t *testing.T, conn *pgxpool.Conn, schoolID uuid.UUID)` — executes `SELECT set_config('app.current_school_id', $1, true)` on the connection (matching the SQL used by `platform.DB.SetTenant()`). Accepts `uuid.UUID` but converts to string internally via `.String()`. Fails the test on error.
- `AcquireWithTenant(t *testing.T, pool *pgxpool.Pool, schoolID uuid.UUID) *pgxpool.Conn` — acquires a `*pgxpool.Conn` from the pool (not `*pgx.Conn`) and calls `SetTenantOnConn`. Returns the pooled connection. Registers `t.Cleanup` that runs `RESET ROLE` (to restore superuser) before calling `conn.Release()` — this prevents a `SET ROLE catalogro_app` from leaking to subsequent tests that reuse the same pooled connection.
- `AcquireAsAppRole(t *testing.T, pool *pgxpool.Pool, schoolID uuid.UUID) *pgxpool.Conn` — like `AcquireWithTenant` but also runs `SET ROLE catalogro_app` to simulate the application-level connection. Used specifically by RLS isolation tests. Cleanup resets the role before release.
- These helpers replicate the SQL from `platform.DB.SetTenant()` directly, rather than wrapping it, because the test container connects as superuser while `SetTenant` is a method on `*DB`.

#### 1.3 Example Test Files

**`api/internal/platform/database_test.go`** — Integration test pattern:
- `TestMigrationsRun` — verifies container starts, migrations complete, key tables exist.
- `TestSetTenant` — verifies `SetTenant` sets the session variable correctly.

**`api/internal/platform/rls_test.go`** — Table-driven RLS test pattern:
- `TestRLSIsolation` — table-driven test iterating over all RLS-protected tables (users, classes, grades, absences, etc.). For each table:
  1. Seed data as school A
  2. Query as school A → rows returned
  3. Query as school B → 0 rows returned
- Demonstrates: table-driven tests, subtests (`t.Run`), RLS verification pattern.

**`api/internal/interop/siiir/parser_test.go`** — Unit test pattern:
- `TestParseCSV_2024Format` — parses a Windows-1250, semicolon-delimited CSV (2024-v1 format).
- `TestParseCSV_2025Format` — parses a UTF-8, comma-delimited CSV (2025-v1 format).
- `TestDetectFormat` — tests format auto-detection heuristics. Note: `DetectFormat` takes `io.ReadSeeker` (it calls `Seek` internally). Use `strings.NewReader` which satisfies both `io.Reader` and `io.ReadSeeker`.
- `TestParseCSV_MalformedInput` — verifies graceful handling of corrupt/incomplete CSV.
- Test data: embedded as string constants (small, 3-5 row CSVs), wrapped in `strings.NewReader`.

**`api/internal/interop/siiir/mapper_test.go`** — Unit test pattern:
- `TestMapStudent_NameNormalization` — "ion POPESCU" → "Ion Popescu", "ana-maria" → "Ana-Maria".
- `TestMapStudent_ClassNormalization` — "5 A" → "5A", "  10B " → "10B". Note: the current `normalizeClassName` only trims whitespace and removes spaces — it does NOT convert Roman numerals. Do not test Roman numeral conversion.
- `TestMapStudent_Deduplication` — second call to `MapStudent` with the same CNP in the batch returns an error (the mapper rejects duplicates, it does not silently deduplicate).
- `TestMapStudent_MissingCNP` — fallback to synthetic source ID from name+class.

### 2. Vitest Unit Test Foundation

#### 2.1 Configuration

**`web/vitest.config.ts`**:
- Test environment: `happy-dom`
- Globals: `true` (describe/it/expect available without import)
- Root: `.` (relative to web/)
- Include pattern: `test/**/*.test.ts`
- Setup file: `test/setup.ts`
- Use `@nuxt/test-utils/config` to define the config via `defineVitestConfig()`, which auto-resolves all Nuxt aliases (`#app`, `~`, `#imports`, etc.) from the generated `.nuxt/tsconfig.json`. This requires `npx nuxt prepare` to have run (triggered by `npm install` via the `postinstall` script).

**`web/test/setup.ts`** — Global test setup:
- Mock Nuxt auto-imports: `navigateTo`, `useRuntimeConfig`, `useRoute`, `useRouter`, `useState`
- Mock `localStorage` with a fresh Map-backed implementation per test
- Reset all mocks in `beforeEach`

#### 2.2 Test Helpers: `web/test/helpers/`

**`mock-api.ts`** — API mock factory:
- `createMockFetch(routes: Record<string, MockResponse>)` — returns a function matching the native `fetch()` signature (the actual `api()` function in `lib/api.ts` uses native `fetch`, not Nuxt's `$fetch`). Mock is injected via `vi.stubGlobal('fetch', createMockFetch(...))`. Routes are matched by URL pattern. Unmatched routes throw an error (fail-fast, no silent swallowing).
- `mockApiError(status: number, code: string, message?: string)` — creates a structured API error response matching the `ApiError` class shape from `lib/api.ts`.
- `mockSuccessResponse<T>(data: T)` — wraps data in the standard API response envelope.

**`mock-storage.ts`** — localStorage replacement:
- `createMockStorage()` — returns a `Storage`-compatible object backed by a `Map`. Supports `getItem`, `setItem`, `removeItem`, `clear`, `length`, `key()`.
- Auto-injected in `test/setup.ts` via `vi.stubGlobal('localStorage', createMockStorage())`.

**`mock-dexie.ts`** — IndexedDB/Dexie mock:
- `createMockDb()` — returns an object that mimics the Dexie `db` interface from `lib/db.ts`. Tables are backed by in-memory arrays. Supports: `add()`, `get()`, `update()`, `delete()`, `clear()`, `count()`, `where().equals().toArray()`, `where().equals().delete()`, `where().equals().sortBy()`, `where().anyOf().count()`.
- Does NOT mock full Dexie API — only the subset actually called by `sync-queue.ts` and `sync-engine.ts`.

#### 2.3 Example Test Files

**`web/test/composables/useAuth.test.ts`**:
- `login() stores tokens and fetches profile`
- `login() with MFA returns mfa_required status`
- `verifyMfa() completes authentication`
- `logout() clears tokens and state`
- `requireRole() returns false when user has wrong role`
- Pattern: mock fetch, call composable methods, assert reactive state changes.

**`web/test/lib/sync-queue.test.ts`**:
- `enqueue() adds mutation to pending queue`
- `getPending() returns only pending mutations`
- `markSyncing() updates status`
- `markSynced() deletes the mutation from the queue`
- `markFailed() resets status to pending, stores error message, and increments retry count`
- `pendingCount() returns count of pending and syncing mutations`
- `isExhausted() returns true at MAX_RETRIES`
- `clearCompleted() is effectively a no-op (markSynced deletes records, so no 'synced' status exists) — test verifies it runs without error on empty result set`
- Pattern: mock Dexie, test pure functions against mock DB.

**`web/test/lib/api.test.ts`**:
- `successful GET request`
- `successful POST with body`
- `401 triggers token refresh and retry`
- `refresh failure redirects to login`
- `ApiError has correct structure`
- `skipAuth option omits Authorization header`
- Pattern: mock fetch with route matching, test token lifecycle.

#### 2.4 Dependencies

No new dependencies — `vitest`, `happy-dom`, `@nuxt/test-utils` already in devDependencies.

Update `web/package.json` scripts:
- `"test:unit": "vitest run"` — explicit unit test script
- `"test:unit:watch": "vitest"` — watch mode for development
- Keep `"test": "vitest run"` as-is (unit tests are the default)

### 3. Playwright E2E Foundation

#### 3.1 Installation

Add to `web/` devDependencies:
- `@playwright/test`

Run `npx playwright install chromium` to download browser binary.

#### 3.2 Configuration

**`web/playwright.config.ts`** — Dual-mode config:

Local dev mode (default):
- `baseURL`: `http://localhost:3000`
- `use.trace`: `'on-first-retry'`
- `use.screenshot`: `'only-on-failure'`
- `retries`: 0
- Projects: Chromium only
- No `webServer` — developer runs `make dev` manually

CI mode (detected via `process.env.CI`):
- `retries`: 2
- `webServer`: starts `npm run dev` on port 3000, waits for server ready
- Expects API + DB already running (docker-compose in CI workflow)

Shared:
- `testDir`: `test/e2e`
- `outputDir`: `test/e2e/results`
- `timeout`: 30000ms
- `expect.timeout`: 5000ms

#### 3.3 E2E Fixtures & Page Objects

**`web/test/e2e/fixtures/auth.fixture.ts`** — Playwright test fixture extension:
- Extends base `test` with `authenticatedPage` fixture
- `authenticatedPage` logs in via the login page before each test, stores session via `storageState`
- `teacherPage`, `parentPage`, `adminPage` — role-specific variants using seed data credentials
- Pattern: `test.extend<{ authenticatedPage: Page }>({ ... })`

**`web/test/e2e/page-objects/login.page.ts`** — Page Object Model:
- `goto()` — navigates to `/login`
- `fillEmail(email: string)` — fills email input
- `fillPassword(password: string)` — fills password input
- `submit()` — clicks submit button
- `fillMfaCode(code: string)` — fills TOTP code input (shown after password submit)
- `getErrorMessage()` — returns visible error text
- `isOnDashboard()` — checks if redirected to dashboard after login
- Selectors use `data-testid` attributes (to be added to login.vue)

#### 3.4 Example Test Files

**`web/test/e2e/login.spec.ts`** — E2E skeleton:
- `login page renders` — navigates to /login, verifies form elements visible
- `empty form shows validation errors` — submits empty form, checks error messages
- `invalid credentials show error` — submits wrong email/password, checks error shown
- `test.skip('successful login redirects to dashboard')` — requires auth handler. TODO comment explains what to implement.
- `test.skip('MFA flow completes successfully')` — requires auth + TOTP handler.
- Pattern: Page Object usage, fixture usage (for skipped auth tests), clear TODO markers.

**Data-testid attributes to add to `web/pages/login.vue`:**
- `data-testid="email-input"`
- `data-testid="password-input"`
- `data-testid="submit-button"`
- `data-testid="mfa-input"`
- `data-testid="login-error"` — on the error div in the login form
- `data-testid="mfa-error"` — on the error div in the MFA form (the page has two separate error divs, one per form step)

#### 3.5 Playwright .gitignore additions

Add to `web/.gitignore` (create if needed) or root `.gitignore`:
- `test/e2e/results/`
- `playwright-report/`
- `blob-report/`

### 4. Makefile & Script Updates

| Target | Command | Description |
|--------|---------|-------------|
| `test` | `test-api test-web` | Unchanged — runs Go + Vitest (no E2E) |
| `test-api` | `cd api && go test ./... -v -race -count=1` | Unchanged |
| `test-web` | `cd web && npm run test` | Unchanged |
| `test-e2e` (new) | `cd web && npx playwright test` | Run Playwright E2E tests |
| `test-all` (new) | `test-api test-web test-e2e` | Everything including E2E |

E2E is deliberately NOT part of `make test` — it requires the full stack running. `make test-all` is the opt-in command.

## Files to Create

| File | Purpose |
|------|---------|
| `api/internal/testutil/container.go` | testcontainers-go PostgreSQL lifecycle |
| `api/internal/testutil/seed.go` | Test data factory functions |
| `api/internal/testutil/tenant.go` | RLS context helpers for tests |
| `api/internal/platform/database_test.go` | Migration + SetTenant integration tests |
| `api/internal/platform/rls_test.go` | Table-driven RLS isolation tests |
| `api/internal/interop/siiir/parser_test.go` | SIIIR CSV parser unit tests |
| `api/internal/interop/siiir/mapper_test.go` | SIIIR mapper unit tests |
| `web/vitest.config.ts` | Vitest configuration |
| `web/test/setup.ts` | Global test setup (Nuxt mock auto-imports) |
| `web/test/helpers/mock-api.ts` | API mock factory |
| `web/test/helpers/mock-storage.ts` | localStorage mock |
| `web/test/helpers/mock-dexie.ts` | Dexie/IndexedDB mock |
| `web/test/composables/useAuth.test.ts` | useAuth composable tests |
| `web/test/lib/sync-queue.test.ts` | Sync queue tests |
| `web/test/lib/api.test.ts` | API client tests |
| `web/playwright.config.ts` | Playwright configuration |
| `web/test/e2e/fixtures/auth.fixture.ts` | Auth test fixtures |
| `web/test/e2e/page-objects/login.page.ts` | Login page object |
| `web/test/e2e/login.spec.ts` | Login E2E test skeleton |

## Files to Modify

| File | Changes |
|------|---------|
| `api/go.mod` | Add testcontainers-go, postgres module, google/uuid |
| `web/package.json` | Add @playwright/test devDependency; add test:unit script |
| `web/pages/login.vue` | Add data-testid attributes to form elements |
| `Makefile` | Add test-e2e, test-all targets; update .PHONY |
| `.gitignore` or `web/.gitignore` | Exclude Playwright results/reports |

## Dependencies to Install

**Go (api/):**
- `go get -t github.com/testcontainers/testcontainers-go`
- `go get -t github.com/testcontainers/testcontainers-go/modules/postgres`
- `go get github.com/google/uuid`
- Requires Docker running (testcontainers-go uses the Docker API)

**npm (web/):**
- `npm install --save-dev @playwright/test`
- `npx playwright install chromium`

## Prerequisites

- Docker must be running (testcontainers-go creates PostgreSQL containers)
- `make migrate` must have been run at least once (to verify migration files are valid)
- `npm install` in web/ must be complete (Vitest and happy-dom already installed)

## Not in Scope

- Coverage reporting and thresholds (planned for C phase)
- Mutation testing (C phase)
- Visual regression testing (C phase)
- Firefox/WebKit Playwright projects (C phase)
- axe-core accessibility auditing in E2E (C phase)
- Handler implementation or handler tests (separate TDD cycles)
- CI workflow updates (existing `go test` and `npm test` will automatically pick up the new tests)

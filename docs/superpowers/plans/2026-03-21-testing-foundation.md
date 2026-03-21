# Testing Foundation Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build reusable test infrastructure and example tests for Go integration tests, Nuxt unit tests, and Playwright E2E tests — establishing patterns for all future TDD development.

**Architecture:** Three independent test layers: (1) Go tests use testcontainers-go for real PostgreSQL, shared container per package with truncate-between-tests isolation, and a non-superuser role for RLS verification. (2) Vitest tests use happy-dom with mock helpers for fetch/localStorage/Dexie. (3) Playwright E2E uses page objects and auth fixtures with dual-mode config (local/CI).

**Tech Stack:** testcontainers-go, google/uuid, Vitest, happy-dom, @nuxt/test-utils, @playwright/test

**Spec:** `docs/superpowers/specs/2026-03-21-testing-foundation-design.md`

**Code style:** All Go and TypeScript code must be fully commented — verbose, junior-developer-friendly, PM-readable. Every function, type, constant, and non-trivial block gets a plain-language comment.

---

## File Map

| File | Action | Responsibility |
|------|--------|---------------|
| `api/internal/testutil/container.go` | Create | testcontainers-go PG lifecycle, migration runner, role creation |
| `api/internal/testutil/seed.go` | Create | Test data factories (schools, users, classes) |
| `api/internal/testutil/tenant.go` | Create | RLS context helpers (SetTenantOnConn, AcquireWithTenant, AcquireAsAppRole) |
| `api/internal/platform/database_test.go` | Create | Migration + SetTenant integration tests |
| `api/internal/platform/rls_test.go` | Create | Table-driven RLS isolation tests for all 17 tables |
| `api/internal/interop/siiir/parser_test.go` | Create | SIIIR CSV parser unit tests (4 tests) |
| `api/internal/interop/siiir/mapper_test.go` | Create | SIIIR mapper unit tests (4 tests) |
| `web/vitest.config.ts` | Create | Vitest configuration via @nuxt/test-utils |
| `web/test/setup.ts` | Create | Global mocks for Nuxt auto-imports |
| `web/test/helpers/mock-api.ts` | Create | Native fetch mock factory |
| `web/test/helpers/mock-storage.ts` | Create | localStorage mock |
| `web/test/helpers/mock-dexie.ts` | Create | Dexie/IndexedDB mock |
| `web/test/composables/useAuth.test.ts` | Create | useAuth composable tests (5 tests) |
| `web/test/lib/sync-queue.test.ts` | Create | Sync queue tests (8 tests) |
| `web/test/lib/api.test.ts` | Create | API client tests (6 tests) |
| `web/playwright.config.ts` | Create | Playwright dual-mode config |
| `web/test/e2e/fixtures/auth.fixture.ts` | Create | Auth test fixtures (role-specific pages) |
| `web/test/e2e/page-objects/login.page.ts` | Create | Login page object model |
| `web/test/e2e/login.spec.ts` | Create | Login E2E skeleton (3 active + 2 skipped tests) |
| `web/pages/login.vue` | Modify | Add data-testid attributes |
| `api/go.mod` | Modify | Add testcontainers-go, uuid |
| `web/package.json` | Modify | Add @playwright/test, test:unit scripts |
| `Makefile` | Modify | Add test-e2e, test-all targets |
| `.gitignore` | Modify | Exclude Playwright reports |

---

### Task 1: Install Go Test Dependencies

**Files:**
- Modify: `api/go.mod`

- [ ] **Step 1: Add testcontainers-go and uuid**

```bash
cd /home/gabriel/openpublic/catalog-scolar/catalogro/api && go get -t github.com/testcontainers/testcontainers-go && go get -t github.com/testcontainers/testcontainers-go/modules/postgres && go get github.com/google/uuid && go mod tidy
```

Expected: `go.mod` and `go.sum` updated, no errors.

- [ ] **Step 2: Verify imports compile**

```bash
cd /home/gabriel/openpublic/catalog-scolar/catalogro/api && go build ./...
```

Expected: Exit 0.

- [ ] **Step 3: Commit**

```bash
git add api/go.mod api/go.sum
git commit -m "chore: add testcontainers-go and google/uuid for test infrastructure"
```

---

### Task 2: Create testutil/container.go

**Files:**
- Create: `api/internal/testutil/container.go`

This is the most critical file — it manages the PostgreSQL container lifecycle for all Go integration tests.

- [ ] **Step 1: Create the file**

Write `api/internal/testutil/container.go` with:

```go
// Package testutil provides shared test helpers for CatalogRO integration tests.
// It manages a PostgreSQL container via testcontainers-go, runs migrations,
// seeds data, and provides RLS-aware connection helpers.
//
// USAGE IN TESTS:
//
//	func TestSomething(t *testing.T) {
//	    pool := testutil.StartPostgres(t)    // shared PG container
//	    testutil.TruncateAll(t, pool)         // clean state
//	    // ... your test logic ...
//	}
package testutil

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"
)

// sharedPool holds the single PostgreSQL connection pool shared across
// all tests in a package. Initialized once via sync.Once.
var (
	sharedPool *pgxpool.Pool
	poolOnce   sync.Once
	poolErr    error
)

// StartPostgres starts a PostgreSQL 17 container (or returns the shared one
// if already running), runs all goose migrations, and creates the app role
// for RLS testing. The container is destroyed when all tests in the package finish.
//
// This function is safe to call from multiple tests — only the first call
// starts the container; subsequent calls return the same pool.
func StartPostgres(t *testing.T) *pgxpool.Pool {
	t.Helper()

	poolOnce.Do(func() {
		ctx := context.Background()

		// Start a PostgreSQL 17 container with a test database.
		// testcontainers-go pulls the Docker image automatically.
		container, err := postgres.Run(ctx,
			"postgres:17-alpine",
			postgres.WithDatabase("catalogro_test"),
			postgres.WithUsername("postgres"),
			postgres.WithPassword("postgres"),
			testcontainers.WithWaitStrategy(
				wait.ForLog("database system is ready to accept connections").
					WithOccurrence(2),
			),
		)
		if err != nil {
			poolErr = fmt.Errorf("start postgres container: %w", err)
			return
		}

		// Get the connection string for the running container.
		connStr, err := container.ConnectionString(ctx, "sslmode=disable")
		if err != nil {
			poolErr = fmt.Errorf("get connection string: %w", err)
			return
		}

		// Create a connection pool to the test database.
		pool, err := pgxpool.New(ctx, connStr)
		if err != nil {
			poolErr = fmt.Errorf("create pool: %w", err)
			return
		}

		// Run all goose migrations to set up the schema.
		if err := runMigrations(ctx, pool); err != nil {
			poolErr = fmt.Errorf("run migrations: %w", err)
			return
		}

		// Create the non-superuser application role used for RLS testing.
		// RLS policies only apply to non-superuser roles, so we need this
		// role to verify that RLS actually works correctly.
		if err := createAppRole(ctx, pool); err != nil {
			poolErr = fmt.Errorf("create app role: %w", err)
			return
		}

		sharedPool = pool

		// Note: We do NOT register container cleanup here because sync.Once
		// runs only once per package. The container stays alive for all tests
		// in the package. Go's test runner handles process cleanup.
	})

	if poolErr != nil {
		t.Fatalf("StartPostgres failed: %v", poolErr)
	}
	return sharedPool
}

// runMigrations reads all .sql files from api/db/migrations/ and executes
// them in order. This replicates what `goose up` does, but without requiring
// the goose CLI binary.
func runMigrations(ctx context.Context, pool *pgxpool.Pool) error {
	// Find the migrations directory relative to this source file.
	_, thisFile, _, _ := runtime.Caller(0)
	migrationsDir := filepath.Join(filepath.Dir(thisFile), "..", "..", "db", "migrations")

	entries, err := os.ReadDir(migrationsDir)
	if err != nil {
		return fmt.Errorf("read migrations dir %s: %w", migrationsDir, err)
	}

	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".sql" {
			continue
		}

		sql, err := os.ReadFile(filepath.Join(migrationsDir, entry.Name()))
		if err != nil {
			return fmt.Errorf("read migration %s: %w", entry.Name(), err)
		}

		// Execute only the "up" portion (everything before -- +goose Down)
		upSQL := extractGooseUp(string(sql))

		if _, err := pool.Exec(ctx, upSQL); err != nil {
			return fmt.Errorf("execute migration %s: %w", entry.Name(), err)
		}
	}

	return nil
}

// extractGooseUp returns only the "up" portion of a goose migration file.
// Goose files have sections like:
//
//	-- +goose Up
//	CREATE TABLE ...
//	-- +goose Down
//	DROP TABLE ...
//
// We only want the Up section for test setup.
func extractGooseUp(sql string) string {
	// Simple approach: find "-- +goose Down" and return everything before it
	const downMarker = "-- +goose Down"
	if idx := strings.Index(sql, downMarker); idx >= 0 {
		return sql[:idx]
	}
	return sql
}

// createAppRole creates a non-superuser PostgreSQL role that mimics
// the application's database connection. RLS policies only apply to
// non-superuser roles, so tests that verify RLS must use this role.
func createAppRole(ctx context.Context, pool *pgxpool.Pool) error {
	// Use DO block to avoid "role already exists" errors on re-runs
	_, err := pool.Exec(ctx, `
		DO $$
		BEGIN
			IF NOT EXISTS (SELECT FROM pg_roles WHERE rolname = 'catalogro_app') THEN
				CREATE ROLE catalogro_app LOGIN PASSWORD 'test';
			END IF;
		END
		$$;
		GRANT ALL ON ALL TABLES IN SCHEMA public TO catalogro_app;
		GRANT ALL ON ALL SEQUENCES IN SCHEMA public TO catalogro_app;
	`)
	return err
}

// TruncateAll removes all data from application tables to ensure test isolation.
// Called between tests to prevent data leakage. Uses TRUNCATE CASCADE for speed.
//
// Does NOT truncate districts or schools — these are shared reference data
// that most tests need. Call TruncateSchools() explicitly if you need to
// clear those too.
//
// The truncation order doesn't matter because CASCADE handles FK dependencies,
// but we list tables explicitly so it's clear what's being cleaned.
func TruncateAll(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()

	// These are all the application tables that hold per-test data.
	// refresh_tokens are cascade-deleted when users are truncated (FK).
	tables := []string{
		"message_recipients", "messages",
		"audit_log", "sync_conflicts",
		"descriptive_evaluations", "averages",
		"absences", "grades",
		"evaluation_configs",
		"class_subject_teachers", "subjects",
		"class_enrollments", "classes",
		"parent_student_links",
		"source_mappings",
		"refresh_tokens", "users",
		"school_years",
		// Note: schools and districts are NOT truncated here.
		// They are shared reference data that most tests need.
		// Call TruncateAll then re-seed schools if you need full isolation.
	}

	query := "TRUNCATE " + strings.Join(tables, ", ") + " CASCADE"
	if _, err := pool.Exec(context.Background(), query); err != nil {
		t.Fatalf("TruncateAll failed: %v", err)
	}
}
```

- [ ] **Step 2: Verify it compiles**

```bash
cd /home/gabriel/openpublic/catalog-scolar/catalogro/api && go build ./internal/testutil/...
```

Expected: Exit 0.

- [ ] **Step 3: Commit**

```bash
git add api/internal/testutil/container.go
git commit -m "feat: add testcontainers-go PostgreSQL lifecycle helper"
```

---

### Task 3: Create testutil/seed.go

**Files:**
- Create: `api/internal/testutil/seed.go`

- [ ] **Step 1: Create seed.go**

Write `api/internal/testutil/seed.go` with seed functions that insert test schools, users, and classes. Use deterministic UUIDs via `uuid.NewSHA1` (UUID v5) derived from a namespace + school name so IDs are predictable across tests.

Key functions:
- `SeedSchools(t, pool) (school1ID, school2ID uuid.UUID)` — inserts 2 districts + 2 schools + 2 school years
- `SeedUsers(t, pool, schoolID) map[string]uuid.UUID` — inserts admin, secretary, teacher, parent, student for a school
- `SeedClass(t, pool, schoolID, teacherID) uuid.UUID` — inserts class + subject + enrollment

Each function must:
- Set tenant context before inserting (since tables have RLS policies) — use `SetTenantOnConn` from `tenant.go`
- Use raw SQL INSERT (not sqlc generated code, to avoid circular dependencies)
- Return the created IDs for use in test assertions
- Have verbose comments explaining what each SQL does and why

**UUID generation strategy:** Use `uuid.NewSHA1(uuid.NameSpaceURL, []byte("catalogro-test-"+name))` to generate deterministic UUIDs from names. This makes IDs predictable and stable across test runs.

**SeedSchools SQL pattern:**
```sql
-- 1. District (no school_id, no RLS)
INSERT INTO districts (id, name, county) VALUES ($1, 'ISJ Cluj', 'Cluj');
-- 2. School (no school_id column on schools table itself)
INSERT INTO schools (id, district_id, name, siiir_code, education_level, address, city)
  VALUES ($1, $2, 'Scoala Test Rebreanu', 'CJ001', 'middle', 'Str. Test 1', 'Cluj-Napoca');
-- 3. School year (has school_id — needs tenant context set first)
INSERT INTO school_years (id, school_id, label, start_date, end_date, is_current)
  VALUES ($1, $2, '2025-2026', '2025-09-15', '2026-06-15', true);
```

**SeedUsers SQL pattern:**
```sql
-- Must set tenant context first: SetTenantOnConn(t, conn, schoolID)
-- Users table columns: id, school_id, role, email, first_name, last_name, password_hash, is_active, activated_at
INSERT INTO users (id, school_id, role, email, first_name, last_name, password_hash, is_active, activated_at)
  VALUES ($1, $2, 'admin', 'admin@test.ro', 'Admin', 'Test', '$2a$10$fake', true, now());
-- Repeat for: secretary, teacher, parent, student (each with unique UUID and email)
```

**SeedClass SQL pattern:**
```sql
-- Set tenant context first
-- 1. Class: INSERT INTO classes (id, school_id, school_year_id, name, grade_level, education_level, homeroom_teacher_id)
-- 2. Subject: INSERT INTO subjects (id, school_id, name, code)
-- 3. Enrollment: INSERT INTO class_enrollments (id, school_id, class_id, student_id, enrolled_at)
-- 4. Teacher assignment: INSERT INTO class_subject_teachers (id, school_id, class_id, subject_id, teacher_id)
```

Reference the exact column names from `api/db/migrations/001_baseline.sql`. The enum values are: `user_role` = admin|secretary|teacher|parent|student, `education_level` = primary|middle|high.

- [ ] **Step 2: Verify it compiles**

```bash
cd /home/gabriel/openpublic/catalog-scolar/catalogro/api && go build ./internal/testutil/...
```

- [ ] **Step 3: Commit**

```bash
git add api/internal/testutil/seed.go
git commit -m "feat: add test data seed factories (schools, users, classes)"
```

---

### Task 4: Create testutil/tenant.go

**Files:**
- Create: `api/internal/testutil/tenant.go`

- [ ] **Step 1: Create tenant.go**

Write `api/internal/testutil/tenant.go` with three helper functions:

```go
// SetTenantOnConn sets the RLS tenant context on a specific database connection.
// This tells PostgreSQL which school's data this connection is allowed to see.
// Equivalent to what the API middleware does on every request.
func SetTenantOnConn(t *testing.T, conn *pgxpool.Conn, schoolID uuid.UUID) {
	t.Helper()
	// Note: we use 'false' (session-wide) instead of 'true' (transaction-local)
	// because test helpers often operate outside explicit transactions.
	// The actual platform.DB.SetTenant() uses 'true' for transaction-local scope.
	_, err := conn.Exec(context.Background(),
		"SELECT set_config('app.current_school_id', $1, false)", schoolID.String())
	if err != nil {
		t.Fatalf("SetTenantOnConn failed: %v", err)
	}
}

// AcquireWithTenant gets a connection from the pool and sets the tenant context.
// The connection is automatically released when the test finishes.
// Uses the superuser role (for setup/teardown operations).
func AcquireWithTenant(t *testing.T, pool *pgxpool.Pool, schoolID uuid.UUID) *pgxpool.Conn {
	t.Helper()
	conn, err := pool.Acquire(context.Background())
	if err != nil {
		t.Fatalf("Acquire connection: %v", err)
	}
	SetTenantOnConn(t, conn, schoolID)
	t.Cleanup(func() {
		// Reset role and tenant before returning connection to pool
		conn.Exec(context.Background(), "RESET ROLE")
		conn.Exec(context.Background(), "RESET ALL")
		conn.Release()
	})
	return conn
}

// AcquireAsAppRole gets a connection that simulates the real application.
// It sets both the tenant context AND switches to the non-superuser role.
// This is essential for testing RLS — superusers bypass RLS entirely.
func AcquireAsAppRole(t *testing.T, pool *pgxpool.Pool, schoolID uuid.UUID) *pgxpool.Conn {
	t.Helper()
	conn := AcquireWithTenant(t, pool, schoolID)
	_, err := conn.Exec(context.Background(), "SET ROLE catalogro_app")
	if err != nil {
		t.Fatalf("SET ROLE catalogro_app: %v", err)
	}
	return conn
}
```

- [ ] **Step 2: Verify it compiles**

```bash
cd /home/gabriel/openpublic/catalog-scolar/catalogro/api && go build ./internal/testutil/...
```

- [ ] **Step 3: Commit**

```bash
git add api/internal/testutil/tenant.go
git commit -m "feat: add RLS tenant context helpers for tests"
```

---

### Task 5: Create database_test.go

**Files:**
- Create: `api/internal/platform/database_test.go`

- [ ] **Step 1: Create the test file**

Write `api/internal/platform/database_test.go` with two integration tests:

- `TestMigrationsRun` — calls `testutil.StartPostgres(t)`, then queries `pg_tables` to verify key tables exist (users, grades, absences, schools, classes). This proves the container + migration flow works.

- `TestSetTenant` — creates a `platform.DB` by wrapping the test pool, calls `SetTenant` with a UUID, then verifies the session variable is set correctly by querying `current_setting('app.current_school_id')`.

Both tests must call `testutil.TruncateAll(t, pool)` at the start for isolation.

- [ ] **Step 2: Run tests (Docker must be running)**

```bash
cd /home/gabriel/openpublic/catalog-scolar/catalogro/api && go test ./internal/platform/ -v -run TestMigrations -count=1
```

Expected: PASS (container starts, migrations run, tables exist).

```bash
cd /home/gabriel/openpublic/catalog-scolar/catalogro/api && go test ./internal/platform/ -v -run TestSetTenant -count=1
```

Expected: PASS.

- [ ] **Step 3: Commit**

```bash
git add api/internal/platform/database_test.go
git commit -m "test: add migration and SetTenant integration tests"
```

---

### Task 6: Create rls_test.go

**Files:**
- Create: `api/internal/platform/rls_test.go`

- [ ] **Step 1: Create the test file**

Write `api/internal/platform/rls_test.go` with `TestRLSIsolation` — a table-driven test that verifies RLS policies block cross-tenant access. For each of the 17 RLS-enabled tables:

1. Seed data as school A (using superuser connection with tenant A)
2. Query as school A via `AcquireAsAppRole` → expect rows > 0
3. Query as school B via `AcquireAsAppRole` → expect rows == 0

Use `t.Run(tableName, ...)` for subtests so each table's test is independent.

Tables to test (from schema): `users`, `school_years`, `classes`, `class_enrollments`, `subjects`, `class_subject_teachers`, `evaluation_configs`, `grades`, `absences`, `averages`, `descriptive_evaluations`, `sync_conflicts`, `audit_log`, `messages`, `message_recipients`, `parent_student_links`, `source_mappings`.

Some tables require parent records (e.g., grades need an enrollment). The seed helpers from Task 3 should provide enough base data. For tables that need additional setup (like `grades` needing a student enrollment), add the minimum INSERT in the test itself.

- [ ] **Step 2: Run the RLS tests**

```bash
cd /home/gabriel/openpublic/catalog-scolar/catalogro/api && go test ./internal/platform/ -v -run TestRLSIsolation -count=1 -timeout 120s
```

Expected: All subtests PASS. If any fail, the RLS policy for that table may be misconfigured — investigate and fix the test (not the schema, since the schema is known-good).

- [ ] **Step 3: Commit**

```bash
git add api/internal/platform/rls_test.go
git commit -m "test: add table-driven RLS isolation tests for all 17 tables"
```

---

### Task 7: Create parser_test.go

**Files:**
- Create: `api/internal/interop/siiir/parser_test.go`

- [ ] **Step 1: Create the test file**

Write `api/internal/interop/siiir/parser_test.go` with 4 table-driven unit tests:

- `TestParseCSV_2024Format` — embed a small CSV (3 rows) matching the 2024-v1 format: semicolon-delimited, 8 columns (CNP;LastName;FirstName;Class;Form;Status;BirthDate;Gender). Use `strings.NewReader`. Verify: correct number of students, field values match.

- `TestParseCSV_2025Format` — embed a small CSV matching 2025-v1: comma-delimited, different column order (CNP,LastName,FirstName,BirthDate,Gender,Class,Form,Status). Verify: same fields parsed correctly despite different column order.

- `TestDetectFormat` — test table with multiple inputs: a 2024-format sample, a 2025-format sample, and an unknown format. Verify: correct `ColumnMapping.Version` returned, error on unknown. Use `strings.NewReader` (satisfies `io.ReadSeeker`).

- `TestParseCSV_MalformedInput` — test with incomplete rows (missing columns), empty file, and rows with missing name fields. Verify: no panic, partial results returned (malformed rows skipped).

All test data embedded as string constants — no external files.

- [ ] **Step 2: Run parser tests**

```bash
cd /home/gabriel/openpublic/catalog-scolar/catalogro/api && go test ./internal/interop/siiir/ -v -run TestParse -count=1
```

Expected: All PASS.

- [ ] **Step 3: Commit**

```bash
git add api/internal/interop/siiir/parser_test.go
git commit -m "test: add SIIIR CSV parser unit tests (2024 + 2025 formats)"
```

---

### Task 8: Create mapper_test.go

**Files:**
- Create: `api/internal/interop/siiir/mapper_test.go`

- [ ] **Step 1: Create the test file**

Write `api/internal/interop/siiir/mapper_test.go` with 4 table-driven unit tests:

- `TestMapStudent_NameNormalization` — table of inputs → expected outputs:
  - `"ion POPESCU"` → `"Ion Popescu"` (last), `"MARIA"` → `"Maria"` (first)
  - `"ana-maria"` → `"Ana-Maria"` (compound hyphenated)
  - `"  ION  "` → `"Ion"` (whitespace trimming)

- `TestMapStudent_ClassNormalization` — table of inputs → expected:
  - `"5 A"` → `"5A"` (spaces removed)
  - `"  10B "` → `"10B"` (trim + collapse)
  - Do NOT test Roman numeral conversion (code doesn't implement it).

- `TestMapStudent_Deduplication` — create a `NewMapper()`, call `MapStudent` twice with same CNP. First call succeeds, second call returns error containing "duplicate CNP/ID in batch".

- `TestMapStudent_MissingCNP` — call `MapStudent` with empty CNP field. Verify the `SourceMapping.SourceID` starts with `"nokey:"` (synthetic fallback).

- [ ] **Step 2: Run mapper tests**

```bash
cd /home/gabriel/openpublic/catalog-scolar/catalogro/api && go test ./internal/interop/siiir/ -v -run TestMapStudent -count=1
```

Expected: All PASS.

- [ ] **Step 3: Commit**

```bash
git add api/internal/interop/siiir/mapper_test.go
git commit -m "test: add SIIIR mapper unit tests (names, classes, dedup)"
```

---

### Task 9: Create Vitest Config + Setup

**Files:**
- Create: `web/vitest.config.ts`
- Create: `web/test/setup.ts`
- Modify: `web/package.json`

- [ ] **Step 1: Create vitest.config.ts**

```typescript
// vitest.config.ts — Configuration for CatalogRO unit tests.
//
// Uses @nuxt/test-utils to auto-resolve Nuxt aliases (#app, ~, #imports).
// Tests run in happy-dom (fast DOM simulation, no real browser needed).
//
// USAGE: npm run test:unit   (single run)
//        npm run test:watch  (re-run on file changes)

import { defineVitestConfig } from '@nuxt/test-utils/config';

export default defineVitestConfig({
  test: {
    // Use happy-dom for fast DOM simulation (no real browser needed).
    // This is much faster than jsdom and sufficient for component/composable tests.
    environment: 'happy-dom',

    // Make describe/it/expect available without importing.
    // This matches the pattern used by Jest and reduces boilerplate.
    globals: true,

    // Only look for test files in the test/ directory.
    include: ['test/**/*.test.ts'],

    // Run this file before every test suite to set up global mocks.
    setupFiles: ['test/setup.ts'],
  },

  // Define compile-time constants for the test environment.
  // import.meta.client is a Nuxt-specific flag that is true on the client side.
  // Without this, api.ts guards (getAccessToken, setTokens, clearTokens) return
  // early, making auth-related tests impossible.
  define: {
    'import.meta.client': true,
  },
});
```

- [ ] **Step 2: Create test/setup.ts**

```typescript
// test/setup.ts — Global test setup that runs before every test file.
//
// This file mocks Nuxt's auto-imported functions (navigateTo, useRuntimeConfig, etc.)
// so they work outside of a real Nuxt application context.
// Without these mocks, any composable that uses Nuxt auto-imports would crash.

import { vi, beforeEach } from 'vitest';

// ─── Mock Nuxt auto-imports ─────────────────────────────────
// These functions are auto-imported by Nuxt at build time, but don't exist
// in a plain Vitest environment. We mock them so tests can run.

// navigateTo() is Nuxt's client-side navigation function.
// In tests, we just track that it was called with the right path.
vi.stubGlobal('navigateTo', vi.fn());

// useRuntimeConfig() returns Nuxt's runtime configuration.
// We provide the API base URL that lib/api.ts needs.
vi.stubGlobal('useRuntimeConfig', () => ({
  public: {
    apiBase: 'http://localhost:8080/api/v1',
  },
}));

// ref() and computed() from Vue — Nuxt auto-imports these.
// In test files that import composables, Vue's reactivity is needed.
// @nuxt/test-utils should handle these, but we ensure they're available.
vi.stubGlobal(
  'ref',
  await import('vue').then((m) => m.ref),
);
vi.stubGlobal(
  'computed',
  await import('vue').then((m) => m.computed),
);
vi.stubGlobal(
  'readonly',
  await import('vue').then((m) => m.readonly),
);

// import.meta.client — Nuxt uses this to detect client vs server.
// In tests, we always simulate the client side.
// Note: This is tricky to mock. We set it via define.
// If it doesn't work, individual tests can mock it differently.

// ─── Reset mocks between tests ──────────────────────────────
// This prevents state from one test leaking into another.
beforeEach(() => {
  vi.clearAllMocks();
  localStorage.clear();
});
```

- [ ] **Step 3: Update package.json scripts**

Add to `web/package.json` scripts section:
- `"test:unit": "vitest run"`

Keep the existing `"test": "vitest run"` and `"test:watch": "vitest"` unchanged (test:watch already provides watch mode).

- [ ] **Step 4: Verify Vitest runs (no tests yet, but config loads)**

```bash
cd /home/gabriel/openpublic/catalog-scolar/catalogro/web && npx vitest run --reporter=verbose 2>&1 | head -5
```

Expected: Vitest starts, finds 0 test files, exits clean.

- [ ] **Step 5: Commit**

```bash
git add web/vitest.config.ts web/test/setup.ts web/package.json
git commit -m "feat: add Vitest configuration with happy-dom and Nuxt test-utils"
```

---

### Task 10: Create Mock Helpers

**Files:**
- Create: `web/test/helpers/mock-api.ts`
- Create: `web/test/helpers/mock-storage.ts`
- Create: `web/test/helpers/mock-dexie.ts`

- [ ] **Step 1: Create mock-api.ts**

Write the native `fetch()` mock factory. Key design:
- `createMockFetch(routes)` accepts a `Record<string, MockRoute>` where keys are URL path patterns and values define the response
- Each `MockRoute` has `status`, `body`, optional `headers`
- Returns a function matching `typeof fetch` — constructs proper `Response` objects
- Unmatched URLs throw an error (fail-fast, no silent 404s)
- `mockApiError(status, code, message)` creates an error body matching the `ApiError` structure from `lib/api.ts`
- `mockSuccessResponse<T>(data)` wraps data in standard envelope

The mock matches URLs by checking if the fetch URL ends with the route key (e.g., route key `/auth/login` matches `http://localhost:8080/api/v1/auth/login`).

- [ ] **Step 2: Create mock-storage.ts**

Write a `Storage`-compatible mock backed by a `Map<string, string>`. Must implement: `getItem`, `setItem`, `removeItem`, `clear`, `length` (getter), `key(index)`.

- [ ] **Step 3: Create mock-dexie.ts**

Write a Dexie mock matching the interface used by `sync-queue.ts`. Must support:
- `db.syncQueue.add(item)` → assigns auto-increment `id`, pushes to array
- `db.syncQueue.get(id)` → find by id
- `db.syncQueue.update(id, changes)` → merge changes into item
- `db.syncQueue.delete(id)` → remove by id
- `db.syncQueue.clear()` → empty array
- `db.syncQueue.count()` → array length
- `db.syncQueue.where('field').equals(value).toArray()` → filter
- `db.syncQueue.where('field').equals(value).delete()` → filter + remove
- `db.syncQueue.where('field').equals(value).sortBy(key)` → filter + sort
- `db.syncQueue.where('field').anyOf(values).count()` → filter by array + count

All operations return Promises (matching Dexie's async API).

- [ ] **Step 4: Verify files compile with TypeScript**

```bash
cd /home/gabriel/openpublic/catalog-scolar/catalogro/web && npx tsc --noEmit test/helpers/mock-api.ts test/helpers/mock-storage.ts test/helpers/mock-dexie.ts 2>&1 | head -20
```

Expected: No errors (or only expected Nuxt alias warnings).

- [ ] **Step 5: Commit**

```bash
git add web/test/helpers/
git commit -m "feat: add test mock helpers (fetch, localStorage, Dexie)"
```

---

### Task 11: Create useAuth.test.ts

**Files:**
- Create: `web/test/composables/useAuth.test.ts`

- [ ] **Step 1: Create the test file**

Write 5 tests for the `useAuth` composable from `composables/useAuth.ts`:

1. **`login() stores tokens and fetches profile`** — mock fetch to return `{ access_token, refresh_token }` for `/auth/login` and `{ data: { id, role, ... } }` for `/users/me`. Call `login()`, verify `isAuthenticated` becomes true, `user.value` has correct fields.

2. **`login() with MFA returns mfa_required status`** — mock `/auth/login` to return `{ mfa_required: true, mfa_token: 'tok' }`. Call `login()`, verify return value `{ mfaRequired: true, mfaToken: 'tok' }`, verify `isAuthenticated` stays false.

3. **`verifyMfa() completes authentication`** — mock `/auth/2fa/login` and `/users/me`. Call `verifyMfa()`, verify tokens stored and profile fetched.

4. **`logout() clears tokens and state`** — set up authenticated state first, then call `logout()`. Verify `user.value` is null, `isAuthenticated` is false, localStorage tokens cleared.

5. **`requireRole() returns false when user has wrong role`** — set up user with role `'student'`. Call `requireRole('admin', 'teacher')`, verify returns `false`. Call `requireRole('student')`, verify returns `true`.

Each test must:
- Use `createMockFetch()` from `test/helpers/mock-api.ts`
- Mock fetch via `vi.stubGlobal('fetch', ...)`
- Import and call `useAuth()` to get the composable instance
- Clean up mocks via the global `beforeEach` in setup.ts

**IMPORTANT — Module state isolation:** The `user`, `isAuthenticated`, and `isLoading` refs in `useAuth.ts` are declared at module scope (not inside the composable function). This means state persists between tests if the module is imported once. To prevent test pollution, each test file must call `vi.resetModules()` in `beforeEach` and dynamically import the composable:

```typescript
let useAuth: typeof import('~/composables/useAuth')['useAuth'];

beforeEach(async () => {
  vi.resetModules();
  const mod = await import('~/composables/useAuth');
  useAuth = mod.useAuth;
});
```

This ensures each test gets fresh module-level state.

- [ ] **Step 2: Run the tests**

```bash
cd /home/gabriel/openpublic/catalog-scolar/catalogro/web && npx vitest run test/composables/useAuth.test.ts --reporter=verbose
```

Expected: All 5 tests PASS.

- [ ] **Step 3: Commit**

```bash
git add web/test/composables/useAuth.test.ts
git commit -m "test: add useAuth composable tests (login, MFA, logout, roles)"
```

---

### Task 12: Create sync-queue.test.ts

**Files:**
- Create: `web/test/lib/sync-queue.test.ts`

- [ ] **Step 1: Create the test file**

Write 8 tests for `lib/sync-queue.ts`. Each test must:
- Import the mock Dexie from `test/helpers/mock-dexie.ts`
- Mock the `db` import using `vi.mock('~/lib/db', ...)`
- Test one function per test case

Tests:
1. `enqueue() adds mutation to pending queue` — call enqueue, verify db has 1 item with status 'pending'
2. `getPending() returns only pending mutations` — directly insert 3 items into mock DB: one with status `'pending'`, one with `'syncing'`, one with `'failed'` (note: `markFailed()` resets to `'pending'`, so `'failed'` status can only exist via direct insertion). Verify only the `'pending'` item is returned.
3. `markSyncing() updates status` — enqueue + markSyncing, verify status changed
4. `markSynced() deletes the mutation from the queue` — enqueue + markSynced, verify item removed
5. `markFailed() resets status to pending, stores error, increments retry count` — enqueue + markFailed('network error'), verify all three changes
6. `pendingCount() returns count of pending and syncing mutations` — directly insert into mock DB: 2 items with status `'pending'`, 1 with `'syncing'`, 1 with `'failed'`. Verify count is 3 (pending + syncing only; `anyOf(['pending', 'syncing'])` excludes `'failed'`).
7. `isExhausted() returns true at MAX_RETRIES` — create mutation with attempts=5, verify isExhausted returns true; with attempts=4, verify false
8. `clearCompleted() runs without error on empty result set` — call clearCompleted on empty queue, verify no error

- [ ] **Step 2: Run the tests**

```bash
cd /home/gabriel/openpublic/catalog-scolar/catalogro/web && npx vitest run test/lib/sync-queue.test.ts --reporter=verbose
```

Expected: All 8 tests PASS.

- [ ] **Step 3: Commit**

```bash
git add web/test/lib/sync-queue.test.ts
git commit -m "test: add sync queue unit tests (enqueue, status, retries)"
```

---

### Task 13: Create api.test.ts

**Files:**
- Create: `web/test/lib/api.test.ts`

- [ ] **Step 1: Create the test file**

Write 6 tests for `lib/api.ts`:

1. **`successful GET request`** — mock fetch for `/users/me`, call `api('/users/me')`, verify response data returned.
2. **`successful POST with body`** — mock `/auth/login`, call with `{ method: 'POST', body: { email, password } }`, verify fetch called with correct body.
3. **`401 triggers token refresh and retry`** — mock `/users/me` to return 401 first, then 200 on retry. Mock `/auth/refresh` to succeed. Verify: refresh called, original request retried, final response correct.
4. **`refresh failure redirects to login`** — mock `/users/me` 401, mock `/auth/refresh` to fail. To test the `window.location.href = '/login'` assignment in happy-dom, spy on the property: `const locationSpy = vi.spyOn(window, 'location', 'get').mockReturnValue({ ...window.location, href: '' } as Location);` — or alternatively, verify the side effects that definitely happen: `clearTokens()` is called (localStorage tokens removed). The redirect is a best-effort check.
5. **`ApiError has correct structure`** — mock a 422 response with error body `{ error: { code: 'VALIDATION', message: 'Invalid email' } }`. Catch the error, verify it's an `ApiError` with correct `status`, `code`, `message`.
6. **`skipAuth option omits Authorization header`** — set a token in localStorage, call `api('/auth/login', { skipAuth: true })`. Verify fetch was called WITHOUT Authorization header.

Each test needs `import.meta.client` to be truthy for localStorage access. Handle this by setting it in setup or mocking.

- [ ] **Step 2: Run the tests**

```bash
cd /home/gabriel/openpublic/catalog-scolar/catalogro/web && npx vitest run test/lib/api.test.ts --reporter=verbose
```

Expected: All 6 tests PASS.

- [ ] **Step 3: Commit**

```bash
git add web/test/lib/api.test.ts
git commit -m "test: add API client tests (requests, auth, token refresh, errors)"
```

---

### Task 14: Install Playwright + Config

**Files:**
- Modify: `web/package.json`
- Create: `web/playwright.config.ts`
- Modify: `.gitignore`

- [ ] **Step 1: Install Playwright**

```bash
cd /home/gabriel/openpublic/catalog-scolar/catalogro/web && npm install --save-dev @playwright/test && npx playwright install chromium
```

- [ ] **Step 2: Create playwright.config.ts**

Write `web/playwright.config.ts` with the dual-mode config:

```typescript
// playwright.config.ts — End-to-end test configuration for CatalogRO.
//
// Two modes:
// - LOCAL (default): developer runs `make dev` manually, tests connect to localhost
// - CI: detected via process.env.CI, auto-starts the Nuxt dev server
//
// USAGE: npx playwright test              (run all E2E tests)
//        npx playwright test --ui         (interactive test runner)
//        npx playwright test --debug      (step-by-step debugging)

import { defineConfig, devices } from '@playwright/test';

// Detect if we're running in a CI environment (GitHub Actions, etc.)
const isCI = Boolean(process.env['CI']);

export default defineConfig({
  // Where to find test files
  testDir: 'test/e2e',

  // Where to save test artifacts (screenshots, traces, videos)
  outputDir: 'test/e2e/results',

  // Maximum time a single test can take before timing out
  timeout: 30_000,

  // Maximum time expect() assertions can wait for a condition
  expect: { timeout: 5_000 },

  // In CI, retry failed tests twice to handle flakiness.
  // Locally, no retries — failures should be investigated immediately.
  retries: isCI ? 2 : 0,

  // Only run one test at a time (E2E tests share server state)
  workers: 1,

  // Use the Chromium browser for tests.
  // Firefox and WebKit will be added in the C phase.
  projects: [
    {
      name: 'chromium',
      use: { ...devices['Desktop Chrome'] },
    },
  ],

  // Shared settings for all tests
  use: {
    // The base URL for all page.goto('/path') calls.
    baseURL: 'http://localhost:3000',

    // Capture a trace on the first retry of a failed test.
    // Traces include a timeline of actions, DOM snapshots, and network requests.
    trace: 'on-first-retry',

    // Take a screenshot when a test fails.
    screenshot: 'only-on-failure',
  },

  // In CI: automatically start the Nuxt dev server before running tests.
  // Locally: the developer runs `make dev` manually.
  ...(isCI
    ? {
        webServer: {
          command: 'npm run dev',
          port: 3000,
          reuseExistingServer: false,
        },
      }
    : {}),
});
```

- [ ] **Step 3: Add Playwright outputs to .gitignore**

Append to `/home/gabriel/openpublic/catalog-scolar/catalogro/.gitignore` (create if it doesn't exist):

```
# Playwright
web/test/e2e/results/
web/playwright-report/
web/blob-report/
```

- [ ] **Step 4: Commit**

```bash
git add web/playwright.config.ts web/package.json web/package-lock.json .gitignore
git commit -m "feat: add Playwright E2E test config with dual-mode (local/CI)"
```

---

### Task 15: Create E2E Fixtures + Page Objects

**Files:**
- Create: `web/test/e2e/fixtures/auth.fixture.ts`
- Create: `web/test/e2e/page-objects/login.page.ts`

- [ ] **Step 1: Create auth.fixture.ts**

Write the Playwright test fixture extension that provides role-based authenticated pages:

```typescript
// auth.fixture.ts — Playwright test fixtures for authenticated users.
//
// Extends the base Playwright `test` object with pre-authenticated pages.
// Each fixture logs in as a specific role before the test starts,
// so tests don't need to repeat the login flow.
//
// USAGE:
//   import { test } from './fixtures/auth.fixture';
//   test('teacher can see class list', async ({ teacherPage }) => {
//     await teacherPage.goto('/classes');
//     // ...
//   });
//
// NOTE: These fixtures require the auth handler to be implemented.
// Until then, they are available but will fail on login.

import { test as base, type Page } from '@playwright/test';

// Test user credentials from the seed data (api/db/seed.sql).
// These are the default passwords set during seeding.
const TEST_USERS = {
  admin: { email: 'admin@rebreanu.test', password: 'TestPass123!' },
  teacher: { email: 'teacher@rebreanu.test', password: 'TestPass123!' },
  parent: { email: 'parent@rebreanu.test', password: 'TestPass123!' },
  student: { email: 'student@rebreanu.test', password: 'TestPass123!' },
} as const;

// ... export extended test with authenticatedPage, teacherPage, etc.
// Each fixture navigates to /login, fills credentials, submits, waits for dashboard.
```

The fixture should be complete but will not work until auth handlers are implemented — that's expected.

- [ ] **Step 2: Create login.page.ts**

Write the Page Object Model for the login page:

```typescript
// login.page.ts — Page Object Model for the CatalogRO login page.
//
// Encapsulates all interactions with the login page so tests read
// like user stories, not CSS selectors. If the login page HTML changes,
// only this file needs updating.
//
// USAGE:
//   const loginPage = new LoginPage(page);
//   await loginPage.goto();
//   await loginPage.fillEmail('admin@school.test');
//   await loginPage.fillPassword('secret');
//   await loginPage.submit();

import type { Page, Locator } from '@playwright/test';

export class LoginPage {
  // Selectors use data-testid attributes for stability.
  // These are added to web/pages/login.vue in Task 16.
  readonly emailInput: Locator;
  readonly passwordInput: Locator;
  readonly submitButton: Locator;
  readonly mfaInput: Locator;
  readonly loginError: Locator;
  readonly mfaError: Locator;

  constructor(private readonly page: Page) {
    this.emailInput = page.getByTestId('email-input');
    this.passwordInput = page.getByTestId('password-input');
    this.submitButton = page.getByTestId('submit-button');
    this.mfaInput = page.getByTestId('mfa-input');
    this.loginError = page.getByTestId('login-error');
    this.mfaError = page.getByTestId('mfa-error');
  }

  // ... goto(), fillEmail(), fillPassword(), submit(), fillMfaCode(),
  //     getErrorMessage(), isOnDashboard() methods
}
```

- [ ] **Step 3: Commit**

```bash
git add web/test/e2e/
git commit -m "feat: add E2E auth fixtures and login page object"
```

---

### Task 16: Create login.spec.ts + Add data-testid

**Files:**
- Create: `web/test/e2e/login.spec.ts`
- Modify: `web/pages/login.vue`

- [ ] **Step 1: Add data-testid attributes to login.vue**

Add these attributes to the existing elements in `web/pages/login.vue`:
- Email input (line 61): add `data-testid="email-input"`
- Password input (line 73): add `data-testid="password-input"`
- Login submit button (line 84): add `data-testid="submit-button"`
- Login error div (line 80): add `data-testid="login-error"`
- TOTP input (line 100): add `data-testid="mfa-input"`
- MFA error div (line 111): add `data-testid="mfa-error"`

Do NOT change any other attributes or styling.

- [ ] **Step 2: Create login.spec.ts**

Write the E2E test skeleton with 3 active tests and 2 skipped:

1. `login page renders` — goto /login, expect email input, password input, submit button visible
2. `empty form shows validation errors` — click submit without filling, verify browser validation (required fields)
3. `invalid credentials show error` — fill email + password, submit, expect error message visible (will get network error since API isn't running, but the test validates the form submission flow)
4. `test.skip('successful login redirects to dashboard')` — TODO comment: implement when auth handler exists
5. `test.skip('MFA flow completes successfully')` — TODO comment: implement when auth + TOTP handler exists

Use the `LoginPage` page object for all interactions.

- [ ] **Step 3: Verify ESLint passes on modified login.vue**

```bash
cd /home/gabriel/openpublic/catalog-scolar/catalogro/web && npx eslint pages/login.vue
```

Expected: No errors.

- [ ] **Step 4: Commit**

```bash
git add web/pages/login.vue web/test/e2e/login.spec.ts
git commit -m "test: add login E2E skeleton with page object and data-testid attributes"
```

---

### Task 17: Update Makefile

**Files:**
- Modify: `Makefile`

- [ ] **Step 1: Add test-e2e and test-all targets**

Add to the `.PHONY` line: `test-e2e test-all`

Add after the `test-web` target:

```makefile
test-e2e: ## Run Playwright E2E tests (requires make dev running)
	cd web && npx playwright test

test-all: test-api test-web test-e2e ## Run ALL tests including E2E
```

- [ ] **Step 2: Verify**

```bash
make help | grep -E '(test-e2e|test-all)'
```

Expected: Both targets shown.

- [ ] **Step 3: Commit**

```bash
git add Makefile
git commit -m "chore: add test-e2e and test-all Makefile targets"
```

---

### Task 18: Verify End-to-End

**Files:** None (verification only)

- [ ] **Step 1: Run Go tests**

```bash
cd /home/gabriel/openpublic/catalog-scolar/catalogro && make test-api
```

Expected: All Go tests PASS (database, RLS, parser, mapper). First run may be slow (~30s) due to Docker image pull.

- [ ] **Step 2: Run Vitest tests**

```bash
cd /home/gabriel/openpublic/catalog-scolar/catalogro && make test-web
```

Expected: All Vitest tests PASS (useAuth, sync-queue, api).

- [ ] **Step 3: Run lint to verify no regressions**

```bash
cd /home/gabriel/openpublic/catalog-scolar/catalogro && make lint
```

Expected: Both golangci-lint and ESLint pass clean.

- [ ] **Step 4: Run pre-commit hooks**

```bash
pre-commit run --all-files
```

Expected: All hooks pass.

// Package testutil provides shared test infrastructure for integration tests across the
// catalogro API. The primary goal is to spin up a real PostgreSQL 17 instance inside a
// Docker container, run all schema migrations against it, and then hand back a connection
// pool that tests can use exactly as they would use the production database.
//
// Using a real database (rather than mocks) means that RLS policies, CHECK constraints,
// indexes, and SQL semantics are all exercised, giving high confidence that the code works
// in production conditions.
package testutil

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	testcontainers "github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"
)

// ----------------------------------------------------------------------------
// Package-level shared state
// ----------------------------------------------------------------------------

// sharedPool is the single *pgxpool.Pool that all tests in a package share.
// We create it once and reuse it to avoid the overhead of starting a new
// container for every test function.
var sharedPool *pgxpool.Pool

// initOnce ensures that startContainer() is only called once per test binary,
// no matter how many test functions call StartPostgres concurrently.
var initOnce sync.Once

// initErr captures any error that occurred during container startup so that
// subsequent callers can fail fast with a meaningful message.
var initErr error

// ----------------------------------------------------------------------------
// Public API
// ----------------------------------------------------------------------------

// StartPostgres starts a PostgreSQL 17 Docker container (or reuses an already-
// running one) and returns a *pgxpool.Pool connected as the superuser.
//
// On first call it:
//  1. Pulls and starts postgres:17-alpine via testcontainers-go.
//  2. Waits until the database is ready to accept connections.
//  3. Runs all goose .sql migrations found in db/migrations/.
//  4. Creates (or confirms the existence of) the catalogro_app application role
//     that the real server uses when executing queries under RLS.
//
// On subsequent calls within the same test binary it returns the cached pool
// immediately — no container is started twice.
//
// The caller should NOT close the pool; it lives for the duration of the test
// binary and is cleaned up automatically when the process exits.
//
// Usage:
//
//	func TestSomething(t *testing.T) {
//	    pool := testutil.StartPostgres(t)
//	    defer testutil.TruncateAll(t, pool)
//	    // ... test code ...
//	}
func StartPostgres(t *testing.T) *pgxpool.Pool {
	t.Helper()

	// sync.Once guarantees that the container is started exactly once even if
	// multiple test functions are running in parallel.
	initOnce.Do(func() {
		sharedPool, initErr = startContainer()
	})

	if initErr != nil {
		// If container startup failed, every test that needs it should fail
		// immediately with a clear diagnostic rather than panicking later.
		t.Fatalf("testutil.StartPostgres: container startup failed: %v", initErr)
	}

	return sharedPool
}

// TruncateAll removes all rows from every application table (in the correct
// dependency order so that foreign-key constraints are not violated).
//
// It intentionally skips the `schools` and `districts` tables because those
// act as shared reference/seed data that many tests rely on being present.
//
// Pass the pool returned by StartPostgres. The function calls t.Fatal on any
// database error, so the caller does not need to check an error return.
//
// Typical usage — call it in a defer right after getting the pool:
//
//	pool := testutil.StartPostgres(t)
//	defer testutil.TruncateAll(t, pool)
func TruncateAll(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()

	// Tables are listed from the most-dependent (leaf) tables to the least-
	// dependent (root-adjacent) tables so that TRUNCATE CASCADE can do its work
	// without hitting unsatisfied FK constraints.
	//
	// The order mirrors the dependency graph in db/migrations/001_baseline.sql:
	//   message_recipients → messages → ...
	//   audit_log, sync_conflicts are standalone leaf tables
	//   ...
	//   users and school_years reference schools (which we keep)
	tables := []string{
		"message_recipients",
		"messages",
		"audit_log",
		"sync_conflicts",
		"descriptive_evaluations",
		"averages",
		"absences",
		"grades",
		"evaluation_configs",
		"class_subject_teachers",
		"subjects",
		"class_enrollments",
		"classes",
		"parent_student_links",
		"source_mappings",
		"refresh_tokens",
		"users",
		"school_years",
	}

	// Build a single TRUNCATE statement for all tables. Using CASCADE means
	// PostgreSQL will automatically handle any remaining FK dependencies even
	// if the list order is slightly off.
	query := fmt.Sprintf(
		"TRUNCATE %s CASCADE",
		strings.Join(tables, ", "),
	)

	ctx := context.Background()
	if _, err := pool.Exec(ctx, query); err != nil { // nosemgrep: rls-missing-tenant-context
		t.Fatalf("testutil.TruncateAll: failed to truncate tables: %v", err)
	}
}

// ----------------------------------------------------------------------------
// Internal helpers
// ----------------------------------------------------------------------------

// startContainer does the actual work of launching the Docker container,
// running migrations, and opening the connection pool.
// It is called at most once per test binary thanks to sync.Once in StartPostgres.
func startContainer() (*pgxpool.Pool, error) {
	ctx := context.Background()

	// ----------------------------------------------------------------
	// 1. Start the container
	// ----------------------------------------------------------------

	// postgres.Run is the testcontainers-go v0.41.0 API for starting a
	// Postgres container. We pass functional options to configure the
	// database name, credentials, and the readiness wait strategy.
	pgContainer, err := postgres.Run(
		ctx,
		// Use the official Postgres 17 Alpine image — small, fast to pull,
		// and matches the production version declared in CLAUDE.md.
		"postgres:17-alpine",

		// Set the superuser credentials. Using simple well-known values is
		// fine for ephemeral test containers that are never exposed publicly.
		postgres.WithUsername("postgres"),
		postgres.WithPassword("postgres"),
		postgres.WithDatabase("catalogro_test"),

		// Wait until the database is fully ready before returning.
		// WithOccurrence(2) waits for the message to appear twice because
		// postgres prints it once during initialisation and again after it
		// has finished setting up the data directory — the second occurrence
		// is the safe signal that the server is ready for connections.
		// testcontainers.WithWaitStrategy wraps the LogStrategy into the
		// ContainerCustomizer interface that postgres.Run expects.
		testcontainers.WithWaitStrategy(
			wait.ForLog("database system is ready to accept connections").
				WithOccurrence(2),
		),
	)
	if err != nil {
		return nil, fmt.Errorf("startContainer: run postgres container: %w", err)
	}

	// ----------------------------------------------------------------
	// 2. Get the connection string
	// ----------------------------------------------------------------

	// ConnectionString returns a DSN such as:
	//   postgres://postgres:postgres@localhost:<port>/catalogro_test
	// We append sslmode=disable because the container does not have TLS
	// configured and pgx will refuse to connect without this flag.
	connStr, err := pgContainer.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		return nil, fmt.Errorf("startContainer: get connection string: %w", err)
	}

	// ----------------------------------------------------------------
	// 3. Open the connection pool
	// ----------------------------------------------------------------

	// pgxpool.New parses the DSN and opens a pool of connections.
	// The pool is intentionally left with default settings (min 0, max 4);
	// integration tests are not performance benchmarks.
	pool, err := pgxpool.New(ctx, connStr)
	if err != nil {
		return nil, fmt.Errorf("startContainer: open pgxpool: %w", err)
	}

	// ----------------------------------------------------------------
	// 4. Run schema migrations
	// ----------------------------------------------------------------

	// runMigrations reads all .sql files from the migrations directory and
	// executes the "up" portion of each one in alphabetical order.
	if err := runMigrations(ctx, pool); err != nil {
		return nil, fmt.Errorf("startContainer: run migrations: %w", err)
	}

	// ----------------------------------------------------------------
	// 5. Create the application role
	// ----------------------------------------------------------------

	// createAppRole ensures catalogro_app exists with the required grants.
	// The baseline migration already creates it, but calling this function
	// here makes the helper self-contained even if migrations change.
	if err := createAppRole(ctx, pool); err != nil {
		return nil, fmt.Errorf("startContainer: create app role: %w", err)
	}

	return pool, nil
}

// runMigrations finds all *.sql files in the db/migrations/ directory relative
// to this source file's location, extracts the "up" portion of each goose
// migration (everything before the "-- +goose Down" marker), and executes
// them in alphabetical order against the given pool.
//
// We implement a minimal goose-compatible runner here so that the test helper
// has no runtime dependency on the goose binary.  Only the "Up" section of
// each migration file is executed; the "Down" section is ignored.
func runMigrations(ctx context.Context, pool *pgxpool.Pool) error {
	// runtime.Caller(0) returns the absolute path of THIS source file at
	// compile time. Using it as an anchor lets us find the migrations
	// directory with a relative path that works regardless of where the
	// tests are run from.
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		return fmt.Errorf("runMigrations: could not determine source file path via runtime.Caller")
	}

	// Walk up from internal/testutil/ to the api/ root, then descend into
	// db/migrations/.
	//   thisFile = .../api/internal/testutil/container.go
	//   Dir      = .../api/internal/testutil
	//   ../..    = .../api
	//   joined   = .../api/db/migrations
	migrationsDir := filepath.Join(filepath.Dir(thisFile), "..", "..", "db", "migrations")

	// Read all entries in the migrations directory.
	entries, err := os.ReadDir(migrationsDir)
	if err != nil {
		return fmt.Errorf("runMigrations: read dir %q: %w", migrationsDir, err)
	}

	// Collect only .sql files.
	var sqlFiles []string
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".sql") {
			sqlFiles = append(sqlFiles, filepath.Join(migrationsDir, e.Name()))
		}
	}

	// Sort alphabetically so that 001_baseline.sql runs before 002_*, etc.
	sort.Strings(sqlFiles)

	// Execute each migration's "up" section.
	for _, path := range sqlFiles {
		contents, err := os.ReadFile(path)
		if err != nil {
			return fmt.Errorf("runMigrations: read %q: %w", path, err)
		}

		upSQL := extractUpSection(string(contents))
		if strings.TrimSpace(upSQL) == "" {
			// Nothing to execute in this file — skip gracefully.
			continue
		}

		if _, err := pool.Exec(ctx, upSQL); err != nil { // nosemgrep: rls-missing-tenant-context
			return fmt.Errorf("runMigrations: execute %q: %w", path, err)
		}
	}

	return nil
}

// extractUpSection returns the portion of a goose SQL migration that should
// be executed on "migrate up". It strips everything from the "-- +goose Down"
// marker to the end of the file.
//
// Example migration structure:
//
//	-- +goose Up
//	CREATE TABLE foo (...);
//
//	-- +goose Down
//	DROP TABLE foo;
//
// extractUpSection returns:
//
//	-- +goose Up
//	CREATE TABLE foo (...);
func extractUpSection(content string) string {
	// strings.Index returns -1 if the marker is not present (e.g. the file
	// has no Down section). In that case we return the whole file.
	downIdx := strings.Index(content, "-- +goose Down")
	if downIdx == -1 {
		return content
	}
	// Return only the text before the Down marker.
	return content[:downIdx]
}

// createAppRole creates the catalogro_app PostgreSQL role if it does not
// already exist, and grants it SELECT/INSERT/UPDATE/DELETE on all current
// tables and USAGE/SELECT on all sequences.
//
// The baseline migration (001_baseline.sql) already performs these steps, but
// having them here makes the helper robust against future refactors where the
// role creation might move to a different migration file.
//
// The DO $$ … $$ anonymous block pattern allows us to use IF NOT EXISTS
// semantics without failing if the role already exists — which would happen
// if the migration already created it.
func createAppRole(ctx context.Context, pool *pgxpool.Pool) error {
	// This DO block is idempotent: it checks pg_roles before attempting to
	// create the role, so running it multiple times is safe.
	createRoleSQL := `
DO $$
BEGIN
	IF NOT EXISTS (SELECT FROM pg_roles WHERE rolname = 'catalogro_app') THEN
		CREATE ROLE catalogro_app LOGIN PASSWORD 'catalogro_app' NOSUPERUSER;
	END IF;
END
$$;`

	if _, err := pool.Exec(ctx, createRoleSQL); err != nil { // nosemgrep: rls-missing-tenant-context
		return fmt.Errorf("createAppRole: create role: %w", err)
	}

	// Grant DML privileges on all tables that currently exist in the public
	// schema. This must run AFTER migrations so that all tables are present.
	grantTablesSQL := `GRANT SELECT, INSERT, UPDATE, DELETE ON ALL TABLES IN SCHEMA public TO catalogro_app;`
	if _, err := pool.Exec(ctx, grantTablesSQL); err != nil { // nosemgrep: rls-missing-tenant-context
		return fmt.Errorf("createAppRole: grant tables: %w", err)
	}

	// Grant sequence privileges so that the app role can generate new IDs
	// via nextval() (e.g., the audit_log identity column).
	grantSeqSQL := `GRANT USAGE, SELECT ON ALL SEQUENCES IN SCHEMA public TO catalogro_app;`
	if _, err := pool.Exec(ctx, grantSeqSQL); err != nil { // nosemgrep: rls-missing-tenant-context
		return fmt.Errorf("createAppRole: grant sequences: %w", err)
	}

	return nil
}

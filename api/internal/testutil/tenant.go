// Package testutil provides shared helpers for integration tests.
// These helpers reduce boilerplate when setting up database connections
// that need Row-Level Security (RLS) context, role simulation, and
// automatic cleanup after each test.
package testutil

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// SetTenantOnConn sets the PostgreSQL session-level configuration variable
// "app.current_school_id" on the given connection.
//
// The RLS policies on all multi-tenant tables read this variable to decide
// which rows the current database session is allowed to see or modify:
//
//	CREATE POLICY tenant_isolation ON grades
//	    USING (school_id = current_setting('app.current_school_id')::uuid);
//
// WHY false (session-wide) instead of true (transaction-local)?
// The third argument to set_config controls scope:
//   - true  → the setting resets automatically when the current transaction ends.
//   - false → the setting persists for the lifetime of the session/connection.
//
// Test helpers often call helper functions outside of an explicit BEGIN/COMMIT
// block. Using true would cause the setting to evaporate as soon as any
// implicit transaction commits, leaving subsequent statements with no tenant
// context and triggering RLS errors. Using false keeps the setting alive for
// the entire connection lifetime, which is what we want in tests where a
// single acquired connection is reused across multiple helper calls.
// AcquireWithTenant and AcquireAsAppRole both run RESET ALL in their cleanup
// hooks, so there is no risk of tenant context leaking between test cases.
func SetTenantOnConn(t *testing.T, conn *pgxpool.Conn, schoolID uuid.UUID) {
	// t.Helper() marks this function as a test helper. When a test fails
	// inside a helper, Go's testing framework reports the line in the calling
	// test rather than the line inside this helper, making failures easier
	// to locate.
	t.Helper()

	// Execute the PostgreSQL built-in set_config function.
	// $1 is the school UUID as a plain string (e.g. "550e8400-e29b-41d4-a716-446655440000").
	// PostgreSQL will cast it to uuid automatically when the RLS policy runs.
	_, err := conn.Exec( // nosemgrep: rls-missing-tenant-context
		context.Background(),
		"SELECT set_config('app.current_school_id', $1, false)",
		schoolID.String(),
	)
	if err != nil {
		// t.Fatalf stops the test immediately and marks it as failed.
		// We use Fatalf (not Errorf) because there is no point continuing
		// a test if the RLS context could not be established — every
		// subsequent database call would either see all rows or no rows,
		// making the test result meaningless.
		t.Fatalf("testutil.SetTenantOnConn: set app.current_school_id: %v", err)
	}
}

// AcquireWithTenant acquires a *pgxpool.Conn from the pool and immediately
// sets the RLS tenant context to schoolID by calling SetTenantOnConn.
//
// It registers a t.Cleanup function that:
//  1. Resets the active PostgreSQL role to the login role (RESET ROLE).
//  2. Clears all session-level GUC overrides, including app.current_school_id (RESET ALL).
//  3. Returns the connection to the pool (conn.Release).
//
// The cleanup runs automatically when the test (or sub-test) that called
// AcquireWithTenant finishes, regardless of pass or fail. This prevents
// tenant context from leaking into unrelated tests that later acquire the
// same connection from the pool.
//
// Usage in a test:
//
//	conn := testutil.AcquireWithTenant(t, pool, schoolID)
//	// use conn for RLS-aware queries …
//	// no manual cleanup needed
func AcquireWithTenant(t *testing.T, pool *pgxpool.Pool, schoolID uuid.UUID) *pgxpool.Conn {
	t.Helper()

	// Acquire a dedicated connection from the pool. pgxpool.Conn gives us a
	// stable connection that will not be handed to another goroutine while we
	// hold it, which is essential because SET CONFIG is connection-scoped.
	conn, err := pool.Acquire(context.Background())
	if err != nil {
		t.Fatalf("testutil.AcquireWithTenant: acquire connection: %v", err)
	}

	// Set the RLS tenant context on the acquired connection.
	SetTenantOnConn(t, conn, schoolID)

	// Register cleanup. t.Cleanup callbacks run in LIFO order after the test
	// completes. We deliberately reset role and all settings before releasing
	// the connection so that a subsequent test that acquires the same
	// underlying physical connection from the pool starts with a clean slate.
	t.Cleanup(func() {
		// RESET ROLE reverts to the role that was active when the connection
		// was originally established (the login role). This is a no-op when
		// no SET ROLE has been issued, but it is safe to run unconditionally.
		_, _ = conn.Exec(context.Background(), "RESET ROLE") // nosemgrep: rls-missing-tenant-context

		// RESET ALL clears all session-level parameter overrides, including
		// app.current_school_id. Errors here are intentionally ignored because
		// cleanup must not cause a test to fail after it has already passed.
		_, _ = conn.Exec(context.Background(), "RESET ALL") // nosemgrep: rls-missing-tenant-context

		// Return the connection to the pool so other tests can use it.
		conn.Release()
	})

	return conn
}

// AcquireAsAppRole acquires a *pgxpool.Conn, sets the RLS tenant context to
// schoolID, and then issues SET ROLE catalogro_app to switch the active
// PostgreSQL role to the application's non-superuser role.
//
// WHY is this necessary?
// PostgreSQL superusers bypass all RLS policies unconditionally. If the
// connection is made with a superuser account (common in local dev and CI),
// the policies are silently skipped and tests that are supposed to verify RLS
// isolation will always pass — even when the policies are broken. By switching
// to the catalogro_app role, which is a normal (non-superuser) role, we force
// the database to actually evaluate the RLS policies, making the tests
// meaningful.
//
// Cleanup behaviour is identical to AcquireWithTenant: RESET ROLE and
// RESET ALL are executed before releasing the connection.
//
// The database role catalogro_app must exist in the test database. Create it
// once in a migration or test-setup fixture:
//
//	CREATE ROLE catalogro_app NOLOGIN NOINHERIT;
//	GRANT SELECT, INSERT, UPDATE, DELETE ON ALL TABLES IN SCHEMA public TO catalogro_app;
func AcquireAsAppRole(t *testing.T, pool *pgxpool.Pool, schoolID uuid.UUID) *pgxpool.Conn {
	t.Helper()

	// Acquire the connection and configure the tenant context. This also
	// registers the base cleanup (RESET ROLE + RESET ALL + Release) via
	// AcquireWithTenant's own t.Cleanup registration.
	conn := AcquireWithTenant(t, pool, schoolID)

	// Switch to the application role to ensure RLS policies are enforced.
	// SET ROLE must be issued after the connection is acquired; it cannot be
	// specified at pool creation time because the pool is shared across tests
	// that may need different roles.
	_, err := conn.Exec(context.Background(), "SET ROLE catalogro_app") // nosemgrep: rls-missing-tenant-context
	if err != nil {
		// If we cannot set the role the test is meaningless (RLS would not be
		// tested), so we fail immediately. The cleanup registered by
		// AcquireWithTenant will still run because t.Cleanup callbacks execute
		// even when t.Fatalf is called.
		t.Fatalf("testutil.AcquireAsAppRole: SET ROLE catalogro_app: %v", err)
	}

	return conn
}

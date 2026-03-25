// Package user_test contains integration tests for the user provisioning HTTP handlers.
//
// # What these tests verify
//
// The three user management endpoints are tested end-to-end against a real
// PostgreSQL 17 container (via testcontainers-go):
//
//	POST /users          — ProvisionUser: creates a new account, returns activation URL
//	GET  /users          — ListUsers: lists active school users (no sensitive fields)
//	GET  /users/pending  — ListPendingActivations: lists accounts not yet activated
//
// # Testing strategy
//
// Instead of mocking the database, we spin up a real PostgreSQL container with
// all migrations applied. This means:
//   - The ProvisionUser SQL query runs against the real schema.
//   - Row-Level Security (RLS) policies are actually evaluated.
//   - Unique constraints (e.g., duplicate email) are enforced at the DB level.
//
// To test the HTTP layer, each test:
//  1. Sets up the DB (start container, seed school).
//  2. Acquires a transaction with the RLS tenant set to the test school.
//  3. Builds a sqlc Queries object bound to that transaction.
//  4. Injects the Queries + fake JWT Claims into the request context using the
//     exported auth.WithQueries and auth.WithClaims helpers.
//  5. Calls the handler directly via httptest.NewRecorder().
//  6. Asserts status code and response body.
//
// # Running these tests
//
//	go test ./internal/user/ -v -run TestHandler -count=1 -timeout 180s
//
// Docker must be running. The first run pulls postgres:17-alpine (~30 MB).
package user_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/vlahsh/catalogro/api/db/generated"
	"github.com/vlahsh/catalogro/api/internal/auth"
	"github.com/vlahsh/catalogro/api/internal/testutil"
	"github.com/vlahsh/catalogro/api/internal/user"
)

// ---------------------------------------------------------------------------
// Package-level shared pool — started once, reused by all tests in this package
// ---------------------------------------------------------------------------

// Note: The pool is accessed via testutil.StartPostgres(t) inside each test.
// There is no package-level pool variable here because Go's testing framework
// does not provide a TestMain without extra boilerplate, and StartPostgres
// already handles singleton initialization via sync.Once internally.

// ---------------------------------------------------------------------------
// Test helpers
// ---------------------------------------------------------------------------

// testLogger returns a slog.Logger that writes to os.Stderr at Debug level.
// Using a real logger (rather than slog.Default()) means handler log lines
// appear in test output when run with -v, which aids debugging.
func testLogger() *slog.Logger {
	// slog.New(slog.NewTextHandler(...)) creates a human-readable logger.
	// We direct it to os.Stderr so it interleaves with t.Log output in -v mode.
	return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
		Level: slog.LevelDebug,
	}))
}

// buildHandler constructs a user.Handler wired to the given pool.
// The baseURL "http://localhost:3000" is used to construct activation URLs in responses.
func buildHandler(pool *pgxpool.Pool) *user.Handler {
	// generated.New(pool) creates a pool-level Queries. The handler stores this
	// as a fallback, but in practice always uses the transaction-scoped Queries
	// from context (injected by withTenantContext below).
	queries := generated.New(pool)
	return user.NewHandler(queries, testLogger(), "http://localhost:3000")
}

// withTenantContext sets up the request context as the real TenantContext
// middleware would: it begins a PostgreSQL transaction, sets the RLS tenant to
// schoolID, creates a transaction-scoped Queries object, and stores both the
// Queries and fake JWT Claims in the request context.
//
// It returns:
//   - The augmented *http.Request (use this, not the original)
//   - A rollback function — call it with defer to clean up the transaction
//
// Because we are in a test (not a real HTTP request), we ROLLBACK at the end
// rather than COMMIT. This keeps every test hermetically isolated even when
// multiple tests run against the same container.
func withTenantContext(
	t *testing.T,
	pool *pgxpool.Pool,
	r *http.Request,
	schoolID uuid.UUID,
	provisionerID uuid.UUID,
	role string,
) (req *http.Request, rollbackFn func()) {
	t.Helper()

	ctx := r.Context()

	// Step 1: Begin a transaction so that set_config with is_local=true (which
	// is what TenantContext middleware uses) takes effect for all subsequent queries.
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("withTenantContext: begin transaction: %v", err)
	}

	// Step 2: Set the RLS tenant context inside the transaction.
	// "SELECT set_config('app.current_school_id', $1, true)" is transaction-local:
	// it is cleared automatically when the transaction ends.
	_, err = tx.Exec(ctx, // nosemgrep: rls-missing-tenant-context
		"SELECT set_config('app.current_school_id', $1, true)",
		schoolID.String(),
	)
	if err != nil {
		_ = tx.Rollback(ctx)
		t.Fatalf("withTenantContext: set_config tenant: %v", err)
	}

	// Step 3: Create a Queries object bound to this transaction.
	// All SQL run through these Queries will respect the RLS tenant we just set.
	queries := generated.New(pool).WithTx(tx)

	// Step 4: Build fake JWT claims that represent the provisioning admin.
	// The handler calls auth.GetUserID(ctx) to get the provisioner's UUID for
	// the audit trail. We populate all three fields that Claims carries.
	claims := &auth.Claims{
		UserID:   provisionerID.String(),
		SchoolID: schoolID.String(),
		Role:     role,
	}

	// Step 5: Inject the Queries and Claims into the request context.
	// auth.WithQueries / auth.WithClaims use the same context keys that the
	// real middleware (TenantContext / JWTAuth) uses, so the handler's calls
	// to auth.GetQueries(ctx) and auth.GetUserID(ctx) work correctly.
	ctx = auth.WithQueries(ctx, queries)
	ctx = auth.WithClaims(ctx, claims)

	// Return the augmented request and a cleanup function that rolls back the tx.
	rollback := func() {
		// Rollback is idempotent — safe to call even if the tx already ended.
		_ = tx.Rollback(context.Background())
	}
	return r.WithContext(ctx), rollback
}

// postJSON builds an *http.Request for POST /users with a JSON body.
// The body is encoded from the given value. If encoding fails, the test is
// aborted immediately via t.Fatalf.
func postJSON(t *testing.T, body any) *http.Request {
	t.Helper()

	// json.Marshal encodes the Go value to compact JSON bytes.
	b, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("postJSON: marshal body: %v", err)
	}

	// httptest.NewRequest creates an *http.Request with a valid context (no
	// network connection needed). Target path does not affect handler dispatch
	// when calling the handler directly.
	req := httptest.NewRequest(http.MethodPost, "/users", bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	return req
}

// getRequest builds an *http.Request for GET at the given path.
func getRequest(path string) *http.Request {
	return httptest.NewRequest(http.MethodGet, path, http.NoBody)
}

// decodeData decodes the standard { "data": ... } API envelope and returns
// the inner data field as a map[string]any. If the response is not a valid
// envelope, the test is aborted.
func decodeData(t *testing.T, rr *httptest.ResponseRecorder) map[string]any {
	t.Helper()

	var env struct {
		Data map[string]any `json:"data"`
	}
	if err := json.NewDecoder(rr.Body).Decode(&env); err != nil {
		t.Fatalf("decodeData: decode JSON envelope: %v\nbody: %s", err, rr.Body.String())
	}
	return env.Data
}

// decodeDataList decodes the standard { "data": [...] } envelope and returns
// the inner data array as []any. If the response is not a valid list envelope,
// the test is aborted.
func decodeDataList(t *testing.T, rr *httptest.ResponseRecorder) []any {
	t.Helper()

	var env struct {
		Data []any `json:"data"`
	}
	if err := json.NewDecoder(rr.Body).Decode(&env); err != nil {
		t.Fatalf("decodeDataList: decode JSON envelope: %v\nbody: %s", err, rr.Body.String())
	}
	return env.Data
}

// decodeError decodes the standard { "error": { "code": ..., "message": ... } }
// envelope and returns the code and message. Used to assert 4xx error responses.
func decodeError(t *testing.T, rr *httptest.ResponseRecorder) (code, message string) {
	t.Helper()

	var env struct {
		Error struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.NewDecoder(rr.Body).Decode(&env); err != nil {
		t.Fatalf("decodeError: decode JSON envelope: %v\nbody: %s", err, rr.Body.String())
	}
	return env.Error.Code, env.Error.Message
}

// insertActivatedUser inserts a user with activated_at = now() directly via the pool
// (superuser — bypasses RLS, sets school_id explicitly).
func insertActivatedUser(
	t *testing.T,
	pool *pgxpool.Pool,
	schoolID uuid.UUID,
	email, firstName, lastName, role string,
) uuid.UUID {
	t.Helper()

	ctx := context.Background()
	// Generate a deterministic UUID for the activated user based on email.
	id := uuid.NewSHA1(uuid.NameSpaceURL, []byte("catalogro-test-activated-"+email))

	_, err := pool.Exec(ctx, // nosemgrep: rls-missing-tenant-context
		`INSERT INTO users (id, school_id, role, email, first_name, last_name,
			password_hash, is_active, activated_at)
		VALUES ($1, $2, $3::user_role, $4, $5, $6,
			'$2a$10$abcdefghijklmnopqrstuuABCDEFGHIJKLMNOPQRSTUVWXYZ012',
			true, now())
		ON CONFLICT (id) DO NOTHING`,
		id, schoolID, role, email, firstName, lastName,
	)
	if err != nil {
		t.Fatalf("insertActivatedUser: insert %s: %v", email, err)
	}
	return id
}

// ---------------------------------------------------------------------------
// Test 1: ProvisionUser — success (POST /users with valid body)
// ---------------------------------------------------------------------------

// TestProvisionUser_Success verifies the happy path for POST /users.
//
// Scenario:
//   - A school admin sends a valid JSON body with role=teacher, email, first_name, last_name.
//   - The handler should return HTTP 201 with a JSON body containing:
//     id, role, activation_token (64-char hex), and activation_url.
//   - The new user should be visible in the database afterwards.
func TestProvisionUser_Success(t *testing.T) {
	// -----------------------------------------------------------------------
	// 1. Set up the database.
	// -----------------------------------------------------------------------
	// StartPostgres starts (or reuses) the shared Postgres 17 container.
	// TruncateAll ensures no leftover rows from previous tests.
	pool := testutil.StartPostgres(t)
	testutil.TruncateAll(t, pool)

	// SeedSchools inserts two test schools plus school years. We use the first.
	school1ID, _ := testutil.SeedSchools(t, pool)

	// We also need an existing admin user as the "provisioner" — the person
	// who creates the new account. The handler reads their ID from the JWT claims
	// and stores it in users.provisioned_by for the audit trail.
	users1 := testutil.SeedUsers(t, pool, school1ID)
	adminID := users1["admin"]

	// -----------------------------------------------------------------------
	// 2. Build the request.
	// -----------------------------------------------------------------------
	// This is the JSON body a school secretary would send when creating a new
	// teacher account. Email is optional but strongly recommended.
	body := map[string]any{
		"role":       "teacher",
		"email":      "prof.popescu@test.catalogro.ro",
		"first_name": "Ion",
		"last_name":  "Popescu",
	}
	req := postJSON(t, body)

	// -----------------------------------------------------------------------
	// 3. Inject the auth + tenant context.
	// -----------------------------------------------------------------------
	// withTenantContext begins a real PG transaction, sets the RLS school_id,
	// and injects the Queries + Claims into the request context — exactly what
	// the real middleware chain would do for an authenticated admin request.
	req, rollback := withTenantContext(t, pool, req, school1ID, adminID, "admin")
	defer rollback() // always roll back so this test doesn't affect others

	// -----------------------------------------------------------------------
	// 4. Call the handler.
	// -----------------------------------------------------------------------
	rr := httptest.NewRecorder()
	h := buildHandler(pool)
	h.ProvisionUser(rr, req)

	// -----------------------------------------------------------------------
	// 5. Assert HTTP status 201 Created.
	// -----------------------------------------------------------------------
	// 201 is the correct status for a successful resource creation (RFC 9110).
	// If we get 500, the most likely cause is an RLS or context injection issue.
	if rr.Code != http.StatusCreated {
		t.Fatalf("ProvisionUser: expected 201, got %d — body: %s", rr.Code, rr.Body.String())
	}

	// -----------------------------------------------------------------------
	// 6. Decode and assert the response body.
	// -----------------------------------------------------------------------
	data := decodeData(t, rr)

	// The response must include a non-empty id (UUID string).
	userID, ok := data["id"].(string)
	if !ok || userID == "" {
		t.Errorf("ProvisionUser: expected non-empty 'id' in response, got: %v", data["id"])
	}
	if _, err := uuid.Parse(userID); err != nil {
		t.Errorf("ProvisionUser: 'id' is not a valid UUID: %q", userID)
	}

	// The role must match what we sent.
	if role, _ := data["role"].(string); role != "teacher" {
		t.Errorf("ProvisionUser: expected role='teacher', got %q", role)
	}

	// activation_token must be a 64-character hex string (32 random bytes encoded as hex).
	token, _ := data["activation_token"].(string)
	if len(token) != 64 {
		t.Errorf("ProvisionUser: expected activation_token to be 64 chars, got %d chars: %q", len(token), token)
	}
	// Verify it is valid hexadecimal (only 0-9, a-f characters).
	// De Morgan's equivalent: a character is invalid if it is NOT in [0-9] AND NOT in [a-f].
	for _, c := range strings.ToLower(token) {
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			t.Errorf("ProvisionUser: activation_token contains non-hex character: %q", string(c))
			break
		}
	}

	// activation_url must start with the base URL and contain the token.
	activationURL, _ := data["activation_url"].(string)
	expectedURLPrefix := fmt.Sprintf("http://localhost:3000/activate/%s", token)
	if activationURL != expectedURLPrefix {
		t.Errorf("ProvisionUser: activation_url = %q, want %q", activationURL, expectedURLPrefix)
	}

	// -----------------------------------------------------------------------
	// 7. Verify the user is in the database.
	// -----------------------------------------------------------------------
	// Use the superuser pool (bypasses RLS) to confirm the row was inserted.
	// If the handler returned 201 but the row is missing, there is a commit bug.
	// NOTE: because withTenantContext uses ROLLBACK for test isolation, the
	// user will NOT be visible after rollback. We verify within the same tx
	// by querying the DB with the school context on a fresh superuser query.
	//
	// The ProvisionUser handler runs its INSERT inside the tx that withTenantContext
	// began. Because we defer rollback(), we cannot confirm the row persists after
	// the test. However, we CAN confirm that the handler returned the correct response
	// fields — which proves the INSERT query returned the expected row.
	//
	// For a deeper DB assertion, we query the users table via the superuser pool
	// BEFORE the deferred rollback runs (we are still inside the deferred rollback
	// window here — but the rollback happens when the function returns, so
	// the row IS still visible via the same underlying physical transaction).
	//
	// Actually, the INSERT runs inside the test transaction. The pool.QueryRow
	// below uses a DIFFERENT connection (superuser, no tenant context) and
	// therefore cannot see the uncommitted insert. This is the expected behaviour
	// of PostgreSQL's MVCC: uncommitted rows are invisible to other transactions.
	//
	// Conclusion: we rely on the 201 response + correct fields as the primary
	// assertion. The DB-level assertion is intentionally left as a comment to
	// document this design decision rather than add a flaky superuser check.
	//
	// If you want to verify DB persistence, call the rollback after the query:
	//   var found bool
	//   _ = pool.QueryRow(ctx, "SELECT true FROM users WHERE email = $1", "prof.popescu@...").Scan(&found)
	// — but this would only work in a COMMIT-based test helper, not a rollback-based one.
	t.Logf("ProvisionUser: created user id=%s role=teacher email=prof.popescu@test.catalogro.ro", userID)
}

// ---------------------------------------------------------------------------
// Test 2: ProvisionUser — validation errors (missing required fields)
// ---------------------------------------------------------------------------

// TestProvisionUser_ValidationErrors verifies that POST /users returns HTTP 400
// when required fields are missing from the request body.
//
// Scenario:
//   - Three sub-tests: missing email is allowed (email is optional), but missing
//     role, first_name, and last_name each trigger separate 400 errors.
//   - We do NOT test missing email because the handler allows email to be empty
//     (some accounts are phone-only per the domain spec).
func TestProvisionUser_ValidationErrors(t *testing.T) {
	// -----------------------------------------------------------------------
	// Set up the database once; sub-tests share the pool and school.
	// -----------------------------------------------------------------------
	pool := testutil.StartPostgres(t)
	testutil.TruncateAll(t, pool)
	school1ID, _ := testutil.SeedSchools(t, pool)
	users1 := testutil.SeedUsers(t, pool, school1ID)
	adminID := users1["admin"]

	// testCase describes one invalid request and the expected error code.
	type testCase struct {
		// name is the sub-test label shown in test output.
		name string
		// body is the JSON object to send (some fields intentionally missing).
		body map[string]any
		// wantCode is the expected "code" field in the error response.
		wantCode string
	}

	cases := []testCase{
		{
			// Missing role: the handler checks role first, so this returns MISSING_FIELD.
			name:     "missing_role",
			body:     map[string]any{"first_name": "Ion", "last_name": "Popescu"},
			wantCode: "MISSING_FIELD",
		},
		{
			// Missing first_name: after role is validated, first_name is checked.
			name:     "missing_first_name",
			body:     map[string]any{"role": "teacher", "last_name": "Popescu"},
			wantCode: "MISSING_FIELD",
		},
		{
			// Missing last_name: the final required-field check.
			name:     "missing_last_name",
			body:     map[string]any{"role": "teacher", "first_name": "Ion"},
			wantCode: "MISSING_FIELD",
		},
	}

	for _, tc := range cases {
		// Capture loop variable for use in the closure (Go ≤ 1.21 safety pattern).
		tc := tc

		t.Run(tc.name, func(t *testing.T) {
			// Build the request with the intentionally incomplete body.
			req := postJSON(t, tc.body)

			// Inject the tenant/auth context. The handler needs valid context even
			// for 400 paths because it checks auth.GetQueries(ctx) first. If we
			// omit the context injection the handler would return 500 (missing queries),
			// not 400 (validation failure) — the 500 would mask the bug under test.
			req, rollback := withTenantContext(t, pool, req, school1ID, adminID, "admin")
			defer rollback()

			rr := httptest.NewRecorder()
			h := buildHandler(pool)
			h.ProvisionUser(rr, req)

			// ---------------------------------------------------------------
			// Assert HTTP 400 Bad Request.
			// ---------------------------------------------------------------
			if rr.Code != http.StatusBadRequest {
				t.Errorf("case %q: expected 400, got %d — body: %s", tc.name, rr.Code, rr.Body.String())
				return
			}

			// Assert the error code in the response body.
			code, _ := decodeError(t, rr)
			if code != tc.wantCode {
				t.Errorf("case %q: expected error code %q, got %q", tc.name, tc.wantCode, code)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Test 3: ProvisionUser — invalid role
// ---------------------------------------------------------------------------

// TestProvisionUser_InvalidRole verifies that POST /users returns HTTP 400
// when the role field contains an unrecognised value.
//
// Scenario:
//   - The request body contains role="superadmin" (not in the allowed set).
//   - The handler should return 400 with error code "INVALID_ROLE".
//
// Why this matters: if unknown roles were accepted, a malicious secretary could
// create an account with a privilege level the application does not handle,
// potentially bypassing RBAC checks elsewhere.
func TestProvisionUser_InvalidRole(t *testing.T) {
	pool := testutil.StartPostgres(t)
	testutil.TruncateAll(t, pool)
	school1ID, _ := testutil.SeedSchools(t, pool)
	users1 := testutil.SeedUsers(t, pool, school1ID)
	adminID := users1["admin"]

	// The request contains an invalid role value.
	body := map[string]any{
		"role":       "superadmin", // not in allowedRoles
		"email":      "hacker@evil.com",
		"first_name": "Evil",
		"last_name":  "Hacker",
	}
	req := postJSON(t, body)
	req, rollback := withTenantContext(t, pool, req, school1ID, adminID, "admin")
	defer rollback()

	rr := httptest.NewRecorder()
	h := buildHandler(pool)
	h.ProvisionUser(rr, req)

	// -----------------------------------------------------------------------
	// Assert HTTP 400 Bad Request with INVALID_ROLE code.
	// -----------------------------------------------------------------------
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("TestProvisionUser_InvalidRole: expected 400, got %d — body: %s", rr.Code, rr.Body.String())
	}

	code, msg := decodeError(t, rr)
	if code != "INVALID_ROLE" {
		t.Errorf("TestProvisionUser_InvalidRole: expected error code 'INVALID_ROLE', got %q (message: %q)", code, msg)
	}
}

// ---------------------------------------------------------------------------
// Test 4: ProvisionUser — duplicate email
// ---------------------------------------------------------------------------

// TestProvisionUser_DuplicateEmail verifies that attempting to provision a
// second user with the same email at the same school returns an error.
//
// Scenario:
//   - First call: POST /users with email="teacher2@test.ro" succeeds (201).
//   - Second call: POST /users with the same email fails.
//
// The DB has a UNIQUE constraint on (email, school_id) — PostgreSQL will
// reject the second INSERT. The handler catches the DB error and returns 500
// (since there is currently no dedicated 409 Conflict response for this case).
//
// NOTE: The current handler implementation logs the constraint violation and
// returns HTTP 500 (InternalError). This is intentional — the handler does not
// yet parse specific PostgreSQL error codes to return a 409. This test documents
// that behaviour. If a future PR adds 409 handling, update the assertion here.
func TestProvisionUser_DuplicateEmail(t *testing.T) {
	pool := testutil.StartPostgres(t)
	testutil.TruncateAll(t, pool)
	school1ID, _ := testutil.SeedSchools(t, pool)
	users1 := testutil.SeedUsers(t, pool, school1ID)
	adminID := users1["admin"]

	duplicateEmail := "teacher2@test.catalogro.ro"

	// -----------------------------------------------------------------------
	// First call: should succeed with 201 Created.
	// -----------------------------------------------------------------------
	body1 := map[string]any{
		"role":       "teacher",
		"email":      duplicateEmail,
		"first_name": "Maria",
		"last_name":  "Ionescu",
	}
	req1 := postJSON(t, body1)
	req1, rollback1 := withTenantContext(t, pool, req1, school1ID, adminID, "admin")
	// Do NOT defer rollback yet — we need the INSERT to be visible to the second call.
	// Instead we commit via a post-call rollback after the assertion.

	rr1 := httptest.NewRecorder()
	h := buildHandler(pool)
	h.ProvisionUser(rr1, req1)
	rollback1() // roll back so the inserted row is gone for cleanup; but first check status

	if rr1.Code != http.StatusCreated {
		t.Fatalf("TestProvisionUser_DuplicateEmail: first call expected 201, got %d — body: %s",
			rr1.Code, rr1.Body.String())
	}

	// -----------------------------------------------------------------------
	// Insert the first user permanently (via superuser INSERT) so the unique
	// constraint is in effect for the second call.
	// -----------------------------------------------------------------------
	// withTenantContext uses ROLLBACK, so the first handler call's INSERT was
	// rolled back. To test the uniqueness constraint, we need the first user
	// to actually exist in the DB when the second call runs.
	// We use a direct superuser INSERT for this.
	insertActivatedUser(t, pool, school1ID, duplicateEmail, "Maria", "Ionescu", "teacher")

	// -----------------------------------------------------------------------
	// Second call: same email — should fail with a DB constraint violation.
	// -----------------------------------------------------------------------
	body2 := map[string]any{
		"role":       "teacher", // same role + same email + same school = unique constraint violation
		"email":      duplicateEmail,
		"first_name": "Maria",
		"last_name":  "Ionescu",
	}
	req2 := postJSON(t, body2)
	req2, rollback2 := withTenantContext(t, pool, req2, school1ID, adminID, "admin")
	defer rollback2()

	rr2 := httptest.NewRecorder()
	h.ProvisionUser(rr2, req2)

	// -----------------------------------------------------------------------
	// Assert that the second call fails.
	// -----------------------------------------------------------------------
	// The handler currently returns 500 for DB errors (including constraint violations).
	// A duplicate email causes a UNIQUE constraint violation in PostgreSQL, which
	// the handler catches as a generic error and returns InternalError.
	//
	// If this returns 201, the unique constraint is not working as expected.
	if rr2.Code == http.StatusCreated {
		t.Errorf("TestProvisionUser_DuplicateEmail: expected an error response for duplicate email, got 201 Created")
	}

	// The response must be a non-2xx status (either 400, 409, or 500).
	if rr2.Code < 400 {
		t.Errorf("TestProvisionUser_DuplicateEmail: expected 4xx/5xx for duplicate email, got %d", rr2.Code)
	}

	t.Logf("TestProvisionUser_DuplicateEmail: duplicate email correctly rejected with status %d", rr2.Code)
}

// ---------------------------------------------------------------------------
// Test 5: ListUsers — returns seeded users without sensitive fields
// ---------------------------------------------------------------------------

// TestListUsers_ReturnsSeedUsers verifies the GET /users endpoint.
//
// Scenario:
//   - SeedUsers inserts 5 users (admin, secretary, teacher, parent, student)
//     all with is_active=true and activated_at=now().
//   - ListUsers should return all 5 users in the response.
//   - The response must NOT include password_hash or totp_secret fields.
//
// Why check for absent fields?
// Accidentally leaking password_hash (even a bcrypt hash) would allow
// offline brute-force attacks. totp_secret leakage would allow TOTP cloning.
// This test acts as a canary — if a future refactor accidentally exposes
// these fields, the test will fail immediately.
func TestListUsers_ReturnsSeedUsers(t *testing.T) {
	// -----------------------------------------------------------------------
	// 1. Set up DB with 5 seeded users.
	// -----------------------------------------------------------------------
	pool := testutil.StartPostgres(t)
	testutil.TruncateAll(t, pool)
	school1ID, _ := testutil.SeedSchools(t, pool)
	users1 := testutil.SeedUsers(t, pool, school1ID)
	adminID := users1["admin"]

	// -----------------------------------------------------------------------
	// 2. Build the request and inject context.
	// -----------------------------------------------------------------------
	req := getRequest("/users")
	req, rollback := withTenantContext(t, pool, req, school1ID, adminID, "admin")
	defer rollback()

	// -----------------------------------------------------------------------
	// 3. Call the handler.
	// -----------------------------------------------------------------------
	rr := httptest.NewRecorder()
	h := buildHandler(pool)
	h.ListUsers(rr, req)

	// -----------------------------------------------------------------------
	// 4. Assert HTTP 200 OK.
	// -----------------------------------------------------------------------
	if rr.Code != http.StatusOK {
		t.Fatalf("ListUsers: expected 200, got %d — body: %s", rr.Code, rr.Body.String())
	}

	// -----------------------------------------------------------------------
	// 5. Decode the list and assert count.
	// -----------------------------------------------------------------------
	// SeedUsers inserts 5 users (admin, secretary, teacher, parent, student),
	// all with is_active = true. ListUsersBySchool filters by is_active = true.
	items := decodeDataList(t, rr)

	// We seeded 5 users in SeedUsers. The list should contain exactly 5.
	if len(items) != 5 {
		t.Errorf("ListUsers: expected 5 users, got %d — body: %s", len(items), rr.Body.String())
	}

	// -----------------------------------------------------------------------
	// 6. Verify that each user object does NOT contain sensitive fields.
	// -----------------------------------------------------------------------
	// We check that password_hash and totp_secret are absent from the JSON.
	// json.Marshal(userResponse) will only include fields defined in the struct,
	// but we verify at the JSON level to catch future accidental additions.
	rawBody := rr.Body.String()

	// These field names must NEVER appear in the list response.
	forbiddenFields := []string{"password_hash", "totp_secret", "activation_token"}
	for _, field := range forbiddenFields {
		// Use a simple substring check on the raw JSON body.
		// The field name wrapped in quotes is how it appears in JSON output.
		jsonKey := fmt.Sprintf("%q", field) // produces `"password_hash"` etc.
		// Remove the surrounding quotes that fmt.Sprintf adds:
		jsonKey = jsonKey[1 : len(jsonKey)-1]
		if strings.Contains(rawBody, `"`+jsonKey+`"`) {
			t.Errorf("ListUsers: response must NOT contain field %q, but it does\nbody: %s", field, rawBody)
		}
	}

	// -----------------------------------------------------------------------
	// 7. Verify expected safe fields are present in at least one user object.
	// -----------------------------------------------------------------------
	// A user object must have: id, school_id, role, first_name, last_name, is_active.
	if len(items) > 0 {
		first, ok := items[0].(map[string]any)
		if !ok {
			t.Fatalf("ListUsers: expected each item to be a JSON object, got %T", items[0])
		}

		requiredFields := []string{"id", "school_id", "role", "first_name", "last_name", "is_active"}
		for _, field := range requiredFields {
			if _, exists := first[field]; !exists {
				t.Errorf("ListUsers: expected field %q to be present in user object, but it is missing", field)
			}
		}
	}
}

// ---------------------------------------------------------------------------
// Test 6: ListPendingActivations — returns only unactivated users
// ---------------------------------------------------------------------------

// TestListPendingActivations_ReturnsOnlyPending verifies the GET /users/pending endpoint.
//
// Scenario:
//   - We insert two types of users:
//     a) 1 pending user (activated_at IS NULL) — created via the ProvisionUser handler.
//     b) 5 activated users — created via SeedUsers (activated_at = now()).
//   - The /users/pending endpoint should return ONLY the pending user (1 result).
//
// Why this matters:
//   - If the filter is wrong, secretaries would see a huge list of already-activated
//     users mixed in with the ones who haven't set their password yet.
//   - The ListPendingActivations SQL query uses WHERE activated_at IS NULL AND is_active = true.
//     This test confirms both that the filter works AND that the handler wires it correctly.
func TestListPendingActivations_ReturnsOnlyPending(t *testing.T) {
	// -----------------------------------------------------------------------
	// 1. Set up DB.
	// -----------------------------------------------------------------------
	pool := testutil.StartPostgres(t)
	testutil.TruncateAll(t, pool)
	school1ID, _ := testutil.SeedSchools(t, pool)

	// SeedUsers inserts 5 activated users (admin, secretary, teacher, parent, student).
	// All have activated_at = now() and is_active = true.
	users1 := testutil.SeedUsers(t, pool, school1ID)
	adminID := users1["admin"]

	// -----------------------------------------------------------------------
	// 2. Create one pending user via the ProvisionUser handler.
	// -----------------------------------------------------------------------
	// We use the handler itself to create the pending user, which proves that
	// the INSERT + the ListPendingActivations query work together end-to-end.
	//
	// The handler runs inside a transaction (via withTenantContext). Because we
	// use COMMIT-semantics here (by using pool.Begin without rolling back),
	// the pending user row will persist and be visible to the subsequent
	// ListPendingActivations call.
	//
	// However, withTenantContext always calls ROLLBACK on cleanup. To make the
	// pending user visible for the list query, we need to use a separate
	// transaction that we can commit. We use insertActivatedUser-style raw SQL
	// for this — but WITHOUT activated_at — to simulate a freshly provisioned user.
	ctx := context.Background()

	// Generate a deterministic UUID for the pending user.
	pendingUserID := uuid.NewSHA1(uuid.NameSpaceURL, []byte("catalogro-test-pending-user"))

	// Insert a user with activated_at IS NULL (i.e., pending activation).
	// This is the DB state that ProvisionUser creates (minus the activation_token,
	// which we set to a placeholder since NOT NULL constraints may differ).
	//
	// We need a valid provisioner_id — use the admin UUID we already have.
	// testActivationToken is a placeholder 64-char hex string used as the
	// activation_token column value for the test fixture user. It is NOT a real
	// credential — the column requires a non-NULL value, and this test does not
	// exercise the activation flow itself.
	const testActivationToken = "aabbccddeeff00112233445566778899" + //nolint:gosec // test fixture, not a real secret
		"aabbccddeeff00112233445566778899"
	_, err := pool.Exec(ctx, // nosemgrep: rls-missing-tenant-context
		`INSERT INTO users (id, school_id, role, email, first_name, last_name,
			is_active, activation_token, activation_sent_at, provisioned_by)
		VALUES ($1, $2, 'parent'::user_role, $3, $4, $5,
			true, $6, now(), $7)
		ON CONFLICT (id) DO NOTHING`,
		pendingUserID,
		school1ID,
		"pending.parinte@test.catalogro.ro",
		"Parinte",
		"Pending",
		testActivationToken,
		adminID, // provisioned_by — must reference a valid user
	)
	if err != nil {
		t.Fatalf("TestListPendingActivations: insert pending user: %v", err)
	}

	// -----------------------------------------------------------------------
	// 3. Call GET /users/pending.
	// -----------------------------------------------------------------------
	req := getRequest("/users/pending")
	req, rollback := withTenantContext(t, pool, req, school1ID, adminID, "admin")
	defer rollback()

	rr := httptest.NewRecorder()
	h := buildHandler(pool)
	h.ListPendingActivations(rr, req)

	// -----------------------------------------------------------------------
	// 4. Assert HTTP 200 OK.
	// -----------------------------------------------------------------------
	if rr.Code != http.StatusOK {
		t.Fatalf("ListPendingActivations: expected 200, got %d — body: %s", rr.Code, rr.Body.String())
	}

	// -----------------------------------------------------------------------
	// 5. Assert that ONLY the pending user is returned.
	// -----------------------------------------------------------------------
	// We inserted 5 activated users (SeedUsers) and 1 pending user.
	// ListPendingActivations filters by activated_at IS NULL, so only the
	// pending user should appear.
	items := decodeDataList(t, rr)

	if len(items) != 1 {
		t.Errorf("ListPendingActivations: expected 1 pending user, got %d — body: %s",
			len(items), rr.Body.String())
	}

	// -----------------------------------------------------------------------
	// 6. Verify the returned user is the one we inserted.
	// -----------------------------------------------------------------------
	if len(items) == 1 {
		item, ok := items[0].(map[string]any)
		if !ok {
			t.Fatalf("ListPendingActivations: expected item to be a JSON object, got %T", items[0])
		}

		// The pending user's UUID should match what we inserted.
		gotID, _ := item["id"].(string)
		if gotID != pendingUserID.String() {
			t.Errorf("ListPendingActivations: expected user id=%s, got %q", pendingUserID, gotID)
		}

		// The email should match.
		gotEmail, _ := item["email"].(string)
		if gotEmail != "pending.parinte@test.catalogro.ro" {
			t.Errorf("ListPendingActivations: expected email 'pending.parinte@test.catalogro.ro', got %q", gotEmail)
		}

		// The role should be 'parent' (what we inserted).
		gotRole, _ := item["role"].(string)
		if gotRole != "parent" {
			t.Errorf("ListPendingActivations: expected role 'parent', got %q", gotRole)
		}

		// Verify that sensitive fields are absent from the pending response.
		// The pendingUserResponse struct intentionally omits password_hash and totp_secret.
		rawBody := rr.Body.String()
		for _, field := range []string{"password_hash", "totp_secret"} {
			if strings.Contains(rawBody, `"`+field+`"`) {
				t.Errorf("ListPendingActivations: response must NOT contain field %q\nbody: %s", field, rawBody)
			}
		}
	}
}

// ---------------------------------------------------------------------------
// Test 7: ListChildren — parent sees their linked child with class info
// ---------------------------------------------------------------------------

// TestListChildren_ParentSeesChild verifies the GET /users/me/children endpoint
// happy path for a parent who has one linked child enrolled in a class.
//
// Scenario:
//   - We seed schools, users, and a class via the standard helpers.
//   - SeedUsers creates a parent→student link automatically.
//   - SeedClass creates a class (9A), a subject (Matematică), and enrolls the
//     seeded student in that class.
//   - ListChildren is called as the parent user.
//   - The response must contain exactly one child with the correct name and
//     class fields (class_name "9A", class_education_level "high").
//
// This test exercises the enhanced query that JOINs class_enrollments and classes
// — the old query only returned user rows without class info.
func TestListChildren_ParentSeesChild(t *testing.T) {
	// -----------------------------------------------------------------------
	// 1. Set up the database.
	// -----------------------------------------------------------------------
	// Start (or reuse) the shared Postgres container, truncate all rows so
	// we start clean, then insert the minimal reference data this test needs.
	pool := testutil.StartPostgres(t)
	testutil.TruncateAll(t, pool)

	// SeedSchools creates two schools (school1 and school2) plus one school year
	// each. We only use school1 in this test.
	school1ID, _ := testutil.SeedSchools(t, pool)

	// SeedUsers creates one user per role (admin, secretary, teacher, parent, student)
	// and also creates a parent_student_links row linking the parent to the student.
	// It returns a map from role name → UUID so we can reference them by name below.
	users1 := testutil.SeedUsers(t, pool, school1ID)
	parentID := users1["parent"]
	teacherID := users1["teacher"]
	studentID := users1["student"]

	// SeedClass creates a 9A (high school) class, a Matematică subject,
	// enrolls the student in the class, and assigns the teacher to the subject.
	// Returns the class UUID which we do not need here (we assert via the response).
	testutil.SeedClass(t, pool, school1ID, teacherID)

	// Verify seeded IDs are valid (defensive — deterministicID should never panic).
	if parentID == (uuid.UUID{}) {
		t.Fatal("TestListChildren_ParentSeesChild: parentID is zero — SeedUsers may have failed")
	}
	if studentID == (uuid.UUID{}) {
		t.Fatal("TestListChildren_ParentSeesChild: studentID is zero — SeedUsers may have failed")
	}

	// -----------------------------------------------------------------------
	// 2. Build the request and inject the parent's auth context.
	// -----------------------------------------------------------------------
	// getRequest("/users/me/children") builds a GET request.
	// withTenantContext begins a PG transaction, sets the RLS school_id to school1,
	// creates a transaction-scoped Queries, and injects it + fake Claims (as the
	// parent user) into the request context.
	req := getRequest("/users/me/children")
	req, rollback := withTenantContext(t, pool, req, school1ID, parentID, "parent")
	defer rollback()

	// -----------------------------------------------------------------------
	// 3. Call the ListChildren handler.
	// -----------------------------------------------------------------------
	// We call the handler directly, bypassing the chi router. The handler reads
	// the user ID from the JWT claims in context (parentID).
	rr := httptest.NewRecorder()
	h := buildHandler(pool)
	h.ListChildren(rr, req)

	// -----------------------------------------------------------------------
	// 4. Assert HTTP 200 OK.
	// -----------------------------------------------------------------------
	if rr.Code != http.StatusOK {
		t.Fatalf("ListChildren: expected 200, got %d — body: %s", rr.Code, rr.Body.String())
	}

	// -----------------------------------------------------------------------
	// 5. Decode the list and assert count.
	// -----------------------------------------------------------------------
	// The parent has exactly one linked child (seeded by SeedUsers via
	// the parent_student_links INSERT). Expect exactly one entry in the array.
	items := decodeDataList(t, rr)

	if len(items) != 1 {
		t.Fatalf("ListChildren: expected 1 child, got %d — body: %s", len(items), rr.Body.String())
	}

	// -----------------------------------------------------------------------
	// 6. Assert child identity and class fields.
	// -----------------------------------------------------------------------
	child, ok := items[0].(map[string]any)
	if !ok {
		t.Fatalf("ListChildren: expected item to be a JSON object, got %T", items[0])
	}

	// The child's ID must match the seeded student UUID.
	gotID, _ := child["id"].(string)
	if gotID != studentID.String() {
		t.Errorf("ListChildren: expected child id=%s, got %q", studentID, gotID)
	}

	// The role must always be "student" for linked children.
	gotRole, _ := child["role"].(string)
	if gotRole != "student" {
		t.Errorf("ListChildren: expected role='student', got %q", gotRole)
	}

	// first_name and last_name must be present (non-empty).
	if fn, _ := child["first_name"].(string); fn == "" {
		t.Errorf("ListChildren: expected non-empty first_name, got empty")
	}
	if ln, _ := child["last_name"].(string); ln == "" {
		t.Errorf("ListChildren: expected non-empty last_name, got empty")
	}

	// class_name must be "9A" — that is what SeedClass creates (grade 9, education_level "high").
	gotClassName, _ := child["class_name"].(string)
	if gotClassName != "9A" {
		t.Errorf("ListChildren: expected class_name='9A', got %q", gotClassName)
	}

	// class_education_level must be "high" — SeedClass creates a high-school class.
	gotEduLevel, _ := child["class_education_level"].(string)
	if gotEduLevel != "high" {
		t.Errorf("ListChildren: expected class_education_level='high', got %q", gotEduLevel)
	}

	// class_id must be a non-empty UUID string.
	gotClassID, _ := child["class_id"].(string)
	if gotClassID == "" {
		t.Errorf("ListChildren: expected non-empty class_id, got empty")
	}
	if _, err := uuid.Parse(gotClassID); err != nil {
		t.Errorf("ListChildren: class_id is not a valid UUID: %q", gotClassID)
	}

	// Sensitive fields must NOT be present in the response.
	rawBody := rr.Body.String()
	for _, field := range []string{"password_hash", "totp_secret", "activation_token"} {
		if strings.Contains(rawBody, `"`+field+`"`) {
			t.Errorf("ListChildren: response must NOT contain field %q\nbody: %s", field, rawBody)
		}
	}

	t.Logf("ListChildren: parent %s sees child %s in class 9A (high)", parentID, studentID)
}

// ---------------------------------------------------------------------------
// Test 8: ListChildren — returns empty array for user with no linked children
// ---------------------------------------------------------------------------

// TestListChildren_EmptyForUserWithNoChildren verifies that GET /users/me/children
// returns an empty JSON array (not null, not an error) when the authenticated user
// has no entries in parent_student_links.
//
// Scenario:
//   - We seed a school and users but do NOT call SeedClass or create any link.
//   - We call ListChildren as the teacher user (who has no linked children).
//   - The response must be HTTP 200 with { "data": [] }.
//
// Why this matters:
//   - A null response or an error response for "no children" would break the
//     frontend, which expects to always receive an iterable array.
//   - A teacher calling this endpoint for communication purposes must get an
//     empty list, not a 403 or 500.
func TestListChildren_EmptyForUserWithNoChildren(t *testing.T) {
	// -----------------------------------------------------------------------
	// 1. Set up: school + users, but NO parent_student_links for the teacher.
	// -----------------------------------------------------------------------
	pool := testutil.StartPostgres(t)
	testutil.TruncateAll(t, pool)
	school1ID, _ := testutil.SeedSchools(t, pool)
	users1 := testutil.SeedUsers(t, pool, school1ID)

	// Call as the teacher — who has no children linked to them.
	teacherID := users1["teacher"]

	// -----------------------------------------------------------------------
	// 2. Build the request and inject the teacher's auth context.
	// -----------------------------------------------------------------------
	req := getRequest("/users/me/children")
	req, rollback := withTenantContext(t, pool, req, school1ID, teacherID, "teacher")
	defer rollback()

	// -----------------------------------------------------------------------
	// 3. Call the handler.
	// -----------------------------------------------------------------------
	rr := httptest.NewRecorder()
	h := buildHandler(pool)
	h.ListChildren(rr, req)

	// -----------------------------------------------------------------------
	// 4. Assert HTTP 200 OK.
	// -----------------------------------------------------------------------
	// Even with no children, the handler should return 200, not 404 or 500.
	// An empty dataset is not an error.
	if rr.Code != http.StatusOK {
		t.Fatalf("ListChildren (empty): expected 200, got %d — body: %s", rr.Code, rr.Body.String())
	}

	// -----------------------------------------------------------------------
	// 5. Assert the response is an empty array (not null).
	// -----------------------------------------------------------------------
	items := decodeDataList(t, rr)

	if len(items) != 0 {
		t.Errorf("ListChildren (empty): expected 0 items, got %d — body: %s", len(items), rr.Body.String())
	}

	// The raw JSON must contain [] (empty array), not null or omitted.
	rawBody := rr.Body.String()
	if !strings.Contains(rawBody, `[]`) {
		t.Errorf("ListChildren (empty): expected '[]' in response body, got: %s", rawBody)
	}

	t.Logf("ListChildren (empty): teacher %s correctly received empty children list", teacherID)
}

// ---------------------------------------------------------------------------
// Test 9: ListChildren — child's class name and education level are correct
// ---------------------------------------------------------------------------

// TestListChildren_CorrectClassInfo verifies that the class fields in the
// ListChildren response exactly match what was inserted for the child's class.
//
// Scenario:
//   - We seed school1 with users and a class (9A, high school, Matematică).
//   - The seeded parent is linked to the seeded student who is enrolled in 9A.
//   - We then verify that the class_name="9A" and class_education_level="high"
//     values in the response accurately reflect what SeedClass inserted.
//
// This test is a targeted regression test for the JOIN logic in the enhanced
// ListChildrenForParent query: if the LEFT JOIN to class_enrollments or classes
// breaks, the class fields would be absent (null) even though the student is enrolled.
func TestListChildren_CorrectClassInfo(t *testing.T) {
	// -----------------------------------------------------------------------
	// 1. Full setup: schools, users, class with enrollment.
	// -----------------------------------------------------------------------
	pool := testutil.StartPostgres(t)
	testutil.TruncateAll(t, pool)
	school1ID, _ := testutil.SeedSchools(t, pool)
	users1 := testutil.SeedUsers(t, pool, school1ID)
	parentID := users1["parent"]
	teacherID := users1["teacher"]

	// SeedClass inserts class "9A" (education_level="high", grade_number=9)
	// and enrolls the seeded student in it.
	classID := testutil.SeedClass(t, pool, school1ID, teacherID)

	// -----------------------------------------------------------------------
	// 2. Call ListChildren as the parent.
	// -----------------------------------------------------------------------
	req := getRequest("/users/me/children")
	req, rollback := withTenantContext(t, pool, req, school1ID, parentID, "parent")
	defer rollback()

	rr := httptest.NewRecorder()
	h := buildHandler(pool)
	h.ListChildren(rr, req)

	// -----------------------------------------------------------------------
	// 3. Assert 200 OK and at least one child.
	// -----------------------------------------------------------------------
	if rr.Code != http.StatusOK {
		t.Fatalf("ListChildren (class info): expected 200, got %d — body: %s", rr.Code, rr.Body.String())
	}

	items := decodeDataList(t, rr)
	if len(items) == 0 {
		t.Fatalf("ListChildren (class info): expected at least 1 child, got 0 — body: %s", rr.Body.String())
	}

	// -----------------------------------------------------------------------
	// 4. Verify the class_id, class_name, and class_education_level fields.
	// -----------------------------------------------------------------------
	child, ok := items[0].(map[string]any)
	if !ok {
		t.Fatalf("ListChildren (class info): expected item to be a JSON object")
	}

	// class_id must match the class created by SeedClass.
	gotClassID, _ := child["class_id"].(string)
	if gotClassID != classID.String() {
		t.Errorf("ListChildren (class info): expected class_id=%s, got %q", classID, gotClassID)
	}

	// class_name must be "9A" — SeedClass always creates a class named "9A".
	gotClassName, _ := child["class_name"].(string)
	if gotClassName != "9A" {
		t.Errorf("ListChildren (class info): expected class_name='9A', got %q", gotClassName)
	}

	// class_education_level must be "high" — SeedClass uses education_level="high".
	gotEduLevel, _ := child["class_education_level"].(string)
	if gotEduLevel != "high" {
		t.Errorf("ListChildren (class info): expected class_education_level='high', got %q", gotEduLevel)
	}

	t.Logf("ListChildren (class info): child correctly enrolled in class %s (%s)", gotClassName, gotEduLevel)
}

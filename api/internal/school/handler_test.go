// Package school_test contains integration tests for the school HTTP handlers.
//
// # What these tests verify
//
// The subject management endpoint is tested end-to-end against a real
// PostgreSQL 17 container (via testcontainers-go):
//
//	POST /subjects — CreateSubject: creates a new subject, returns the created row
//
// # Testing strategy
//
// Instead of mocking the database, we spin up a real PostgreSQL container with
// all migrations applied. This means:
//   - The CreateSubject SQL INSERT runs against the real schema.
//   - Row-Level Security (RLS) policies are actually evaluated.
//   - Unique constraints (e.g., duplicate name+level) are enforced at the DB level.
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
//	go test ./internal/school/ -v -run TestHandler -count=1 -timeout 180s
//
// Docker must be running. The first run pulls postgres:17-alpine (~30 MB).
package school_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/vlahsh/catalogro/api/db/generated"
	"github.com/vlahsh/catalogro/api/internal/auth"
	"github.com/vlahsh/catalogro/api/internal/school"
	"github.com/vlahsh/catalogro/api/internal/testutil"
)

// ---------------------------------------------------------------------------
// Test helpers
// ---------------------------------------------------------------------------

// testLogger returns a slog.Logger that writes to os.Stderr at Debug level.
// Using a real logger (rather than slog.Default()) means handler log lines
// appear in test output when run with -v, which aids debugging.
func testLogger() *slog.Logger {
	// slog.NewTextHandler produces human-readable output.
	// os.Stderr is used so it interleaves with t.Log output in -v mode.
	return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
		Level: slog.LevelDebug,
	}))
}

// buildSchoolHandler constructs a school.Handler wired to the given pool.
// The handler is safe for concurrent use and can be reused across test calls.
func buildSchoolHandler(pool *pgxpool.Pool) *school.Handler {
	// generated.New(pool) creates a pool-level Queries. The handler stores this
	// as a fallback, but in practice always uses the transaction-scoped Queries
	// from context (injected by withTenantCtx below).
	queries := generated.New(pool)
	return school.NewHandler(queries, testLogger())
}

// withTenantCtx sets up the request context as the real TenantContext middleware
// would: it begins a PostgreSQL transaction, sets the RLS tenant to schoolID,
// creates a transaction-scoped Queries object, and stores both the Queries and
// fake JWT Claims in the request context.
//
// It returns:
//   - The augmented *http.Request (use this, not the original)
//   - A rollback function — call it with defer to clean up the transaction
//
// Because we are in a test (not a real HTTP request), we ROLLBACK at the end
// rather than COMMIT. This keeps every test hermetically isolated even when
// multiple tests run against the same container.
func withTenantCtx(
	t *testing.T,
	pool *pgxpool.Pool,
	r *http.Request,
	schoolID uuid.UUID,
	userID uuid.UUID,
	role string,
) (req *http.Request, rollbackFn func()) {
	t.Helper()

	ctx := r.Context()

	// Step 1: Begin a real PostgreSQL transaction.
	// set_config with is_local=true (used by TenantContext middleware) only
	// takes effect inside a transaction, so we must BEGIN one here.
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("withTenantCtx: begin transaction: %v", err)
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
		t.Fatalf("withTenantCtx: set_config tenant: %v", err)
	}

	// Step 3: Create a Queries object bound to this transaction.
	// All SQL run through these Queries will respect the RLS tenant we just set.
	queries := generated.New(pool).WithTx(tx)

	// Step 4: Build fake JWT claims representing the requesting user.
	// auth.GetUserID(ctx), auth.GetSchoolID(ctx), and auth.GetUserRole(ctx) all
	// read from this Claims struct, so the handler can call them normally.
	claims := &auth.Claims{
		UserID:   userID.String(),
		SchoolID: schoolID.String(),
		Role:     role,
	}

	// Step 5: Inject the Queries and Claims into the request context.
	// auth.WithQueries / auth.WithClaims use the same context keys that the
	// real middleware (TenantContext / JWTAuth) uses.
	ctx = auth.WithQueries(ctx, queries)
	ctx = auth.WithClaims(ctx, claims)

	// Return the augmented request and a cleanup function that rolls back the tx.
	rollback := func() {
		// Rollback is idempotent — safe to call even if the tx already ended.
		_ = tx.Rollback(context.Background())
	}
	return r.WithContext(ctx), rollback
}

// postSubjectJSON builds an *http.Request for POST /subjects with a JSON body.
// The body is encoded from the given value. Encoding failures abort the test.
func postSubjectJSON(t *testing.T, body any) *http.Request {
	t.Helper()

	b, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("postSubjectJSON: marshal body: %v", err)
	}

	// httptest.NewRequest creates a valid *http.Request with a background context.
	// The target path "/subjects" does not affect handler dispatch when calling
	// the handler directly (bypassing chi routing).
	req := httptest.NewRequest(http.MethodPost, "/subjects", bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	return req
}

// decodeSubjectData decodes the standard { "data": {...} } API envelope returned
// by POST /subjects and returns the inner data map. Decoding failures abort the test.
func decodeSubjectData(t *testing.T, rr *httptest.ResponseRecorder) map[string]any {
	t.Helper()

	var env struct {
		Data map[string]any `json:"data"`
	}
	if err := json.NewDecoder(rr.Body).Decode(&env); err != nil {
		t.Fatalf("decodeSubjectData: decode JSON envelope: %v\nbody: %s", err, rr.Body.String())
	}
	return env.Data
}

// decodeSubjectError decodes the standard { "error": { "code": ..., "message": ... } }
// envelope and returns the code and message strings. Used to assert 4xx responses.
func decodeSubjectError(t *testing.T, rr *httptest.ResponseRecorder) (code, message string) {
	t.Helper()

	var env struct {
		Error struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.NewDecoder(rr.Body).Decode(&env); err != nil {
		t.Fatalf("decodeSubjectError: decode JSON envelope: %v\nbody: %s", err, rr.Body.String())
	}
	return env.Error.Code, env.Error.Message
}

// insertSubjectDirect inserts a subject directly via the superuser connection
// (bypassing RLS, with an explicit school_id). Used to pre-populate a subject
// so that a subsequent handler call can trigger a duplicate constraint violation.
func insertSubjectDirect(
	t *testing.T,
	pool *pgxpool.Pool,
	schoolID uuid.UUID,
	name, level string,
) {
	t.Helper()

	ctx := context.Background()
	id := uuid.NewSHA1(uuid.NameSpaceURL, []byte(fmt.Sprintf("catalogro-test-subject-%s-%s-%s", schoolID, name, level)))

	_, err := pool.Exec(ctx, // nosemgrep: rls-missing-tenant-context
		`INSERT INTO subjects (id, school_id, name, education_level, has_thesis)
		VALUES ($1, $2, $3, $4::education_level, false)
		ON CONFLICT (id) DO NOTHING`,
		id, schoolID, name, level,
	)
	if err != nil {
		t.Fatalf("insertSubjectDirect: insert subject %q (%s): %v", name, level, err)
	}
}

// ---------------------------------------------------------------------------
// Test 1: CreateSubject — success (POST /subjects with valid body)
// ---------------------------------------------------------------------------

// TestCreateSubject_Success verifies the happy path for POST /subjects.
//
// Scenario:
//   - A school admin sends a valid JSON body with all required fields.
//   - The handler should return HTTP 201 Created with a JSON body containing:
//     id (UUID), name, education_level, has_thesis, and short_name.
//   - The returned fields must match what was sent in the request.
func TestCreateSubject_Success(t *testing.T) {
	// -----------------------------------------------------------------------
	// 1. Set up the database.
	// -----------------------------------------------------------------------
	pool := testutil.StartPostgres(t)
	testutil.TruncateAll(t, pool)

	// SeedSchools inserts two test schools plus school years. We use the first.
	school1ID, _ := testutil.SeedSchools(t, pool)

	// SeedUsers inserts one user per role. We use the admin to call the endpoint.
	users := testutil.SeedUsers(t, pool, school1ID)
	adminID := users["admin"]

	// -----------------------------------------------------------------------
	// 2. Build the request.
	// -----------------------------------------------------------------------
	// This payload represents a school admin creating "Matematică" for middle school.
	shortName := "MAT"
	body := map[string]any{
		"name":            "Matematică",
		"short_name":      shortName,
		"education_level": "middle",
		"has_thesis":      true,
	}
	req := postSubjectJSON(t, body)

	// -----------------------------------------------------------------------
	// 3. Inject the auth + tenant context as an admin user.
	// -----------------------------------------------------------------------
	req, rollback := withTenantCtx(t, pool, req, school1ID, adminID, "admin")
	defer rollback() // rolls back the transaction — keeps test DB clean

	// -----------------------------------------------------------------------
	// 4. Call the handler directly.
	// -----------------------------------------------------------------------
	rr := httptest.NewRecorder()
	h := buildSchoolHandler(pool)
	h.CreateSubject(rr, req)

	// -----------------------------------------------------------------------
	// 5. Assert HTTP 201 Created.
	// -----------------------------------------------------------------------
	if rr.Code != http.StatusCreated {
		t.Fatalf("CreateSubject: expected 201, got %d — body: %s", rr.Code, rr.Body.String())
	}

	// -----------------------------------------------------------------------
	// 6. Decode and assert the response body.
	// -----------------------------------------------------------------------
	data := decodeSubjectData(t, rr)

	// id must be a valid UUID string.
	subjectID, ok := data["id"].(string)
	if !ok || subjectID == "" {
		t.Errorf("CreateSubject: expected non-empty 'id' in response, got: %v", data["id"])
	}
	if _, err := uuid.Parse(subjectID); err != nil {
		t.Errorf("CreateSubject: 'id' is not a valid UUID: %q", subjectID)
	}

	// name must match what we sent.
	if name, _ := data["name"].(string); name != "Matematică" {
		t.Errorf("CreateSubject: expected name='Matematică', got %q", name)
	}

	// education_level must match what we sent.
	if level, _ := data["education_level"].(string); level != "middle" {
		t.Errorf("CreateSubject: expected education_level='middle', got %q", level)
	}

	// has_thesis must be true (we sent true).
	if hasThesis, _ := data["has_thesis"].(bool); !hasThesis {
		t.Errorf("CreateSubject: expected has_thesis=true, got %v", data["has_thesis"])
	}

	// short_name must be returned. JSON unmarshals to string.
	if sn, _ := data["short_name"].(string); sn != "MAT" {
		t.Errorf("CreateSubject: expected short_name='MAT', got %v", data["short_name"])
	}
}

// ---------------------------------------------------------------------------
// Test 2: CreateSubject — missing name → 400 Bad Request
// ---------------------------------------------------------------------------

// TestCreateSubject_MissingName verifies that omitting the required "name" field
// causes the handler to return HTTP 400 with an MISSING_FIELD error code.
//
// This is a validation test. The handler must reject the request before reaching
// the database, so no database side-effects should occur.
func TestCreateSubject_MissingName(t *testing.T) {
	// -----------------------------------------------------------------------
	// 1. Set up the database — we still need a valid auth context.
	// -----------------------------------------------------------------------
	pool := testutil.StartPostgres(t)
	testutil.TruncateAll(t, pool)

	school1ID, _ := testutil.SeedSchools(t, pool)
	users := testutil.SeedUsers(t, pool, school1ID)
	adminID := users["admin"]

	// -----------------------------------------------------------------------
	// 2. Build a request body WITHOUT the required "name" field.
	// -----------------------------------------------------------------------
	body := map[string]any{
		// "name" intentionally omitted
		"education_level": "middle",
		"has_thesis":      false,
	}
	req := postSubjectJSON(t, body)
	req, rollback := withTenantCtx(t, pool, req, school1ID, adminID, "admin")
	defer rollback()

	// -----------------------------------------------------------------------
	// 3. Call the handler.
	// -----------------------------------------------------------------------
	rr := httptest.NewRecorder()
	h := buildSchoolHandler(pool)
	h.CreateSubject(rr, req)

	// -----------------------------------------------------------------------
	// 4. Assert HTTP 400 Bad Request.
	// -----------------------------------------------------------------------
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("CreateSubject (missing name): expected 400, got %d — body: %s",
			rr.Code, rr.Body.String())
	}

	// The error code in the response body should be "MISSING_FIELD".
	code, _ := decodeSubjectError(t, rr)
	if code != "MISSING_FIELD" {
		t.Errorf("CreateSubject (missing name): expected error code 'MISSING_FIELD', got %q", code)
	}
}

// ---------------------------------------------------------------------------
// Test 3: CreateSubject — invalid education_level → 400 Bad Request
// ---------------------------------------------------------------------------

// TestCreateSubject_InvalidEducationLevel verifies that sending an unrecognised
// education_level value causes the handler to return HTTP 400 with the error
// code INVALID_EDUCATION_LEVEL.
//
// The valid values are: "primary", "middle", "high". Anything else (including
// empty string, upper-case variants, or typos) must be rejected.
func TestCreateSubject_InvalidEducationLevel(t *testing.T) {
	// -----------------------------------------------------------------------
	// 1. Set up the database.
	// -----------------------------------------------------------------------
	pool := testutil.StartPostgres(t)
	testutil.TruncateAll(t, pool)

	school1ID, _ := testutil.SeedSchools(t, pool)
	users := testutil.SeedUsers(t, pool, school1ID)
	adminID := users["admin"]

	// -----------------------------------------------------------------------
	// 2. Build a request with an invalid education_level value.
	// -----------------------------------------------------------------------
	// "university" is not a valid level in the Romanian primary-education system.
	// Neither are "MIDDLE" (wrong case) or "" (empty). We test with "university"
	// as a clear example of an unsupported value.
	body := map[string]any{
		"name":            "Calcul infinitezimal",
		"education_level": "university", // not valid
		"has_thesis":      false,
	}
	req := postSubjectJSON(t, body)
	req, rollback := withTenantCtx(t, pool, req, school1ID, adminID, "admin")
	defer rollback()

	// -----------------------------------------------------------------------
	// 3. Call the handler.
	// -----------------------------------------------------------------------
	rr := httptest.NewRecorder()
	h := buildSchoolHandler(pool)
	h.CreateSubject(rr, req)

	// -----------------------------------------------------------------------
	// 4. Assert HTTP 400 Bad Request.
	// -----------------------------------------------------------------------
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("CreateSubject (invalid level): expected 400, got %d — body: %s",
			rr.Code, rr.Body.String())
	}

	// The error code must be INVALID_EDUCATION_LEVEL.
	code, _ := decodeSubjectError(t, rr)
	if code != "INVALID_EDUCATION_LEVEL" {
		t.Errorf("CreateSubject (invalid level): expected error code 'INVALID_EDUCATION_LEVEL', got %q", code)
	}
}

// ---------------------------------------------------------------------------
// Test 4: CreateSubject — duplicate name+level → constraint error (non-201)
// ---------------------------------------------------------------------------

// TestCreateSubject_DuplicateNameLevel verifies that creating two subjects with
// the same name and education_level for the same school triggers the database
// unique constraint and results in a non-201 response.
//
// The subjects table has a unique index on (school_id, name, education_level).
// The handler should return 409 Conflict (or 500 if it cannot detect the
// specific constraint) — the important thing is it must NOT return 201.
//
// Why use insertSubjectDirect instead of two handler calls?
// withTenantCtx uses a ROLLBACK, so the first handler call's INSERT is rolled
// back before the second call runs. To test the uniqueness constraint, we need
// the first subject to actually exist in the DB when the second call runs.
// insertSubjectDirect uses a superuser connection that commits immediately.
func TestCreateSubject_DuplicateNameLevel(t *testing.T) {
	// -----------------------------------------------------------------------
	// 1. Set up the database.
	// -----------------------------------------------------------------------
	pool := testutil.StartPostgres(t)
	testutil.TruncateAll(t, pool)

	school1ID, _ := testutil.SeedSchools(t, pool)
	users := testutil.SeedUsers(t, pool, school1ID)
	adminID := users["admin"]

	// -----------------------------------------------------------------------
	// 2. Pre-insert a subject directly so the unique constraint is active
	//    when the handler call runs.
	// -----------------------------------------------------------------------
	// insertSubjectDirect commits immediately (bypasses the test transaction),
	// so the unique constraint on (school_id, name, education_level) is
	// enforced when the handler tries to insert the same combination.
	insertSubjectDirect(t, pool, school1ID, "Fizică", "high")

	// -----------------------------------------------------------------------
	// 3. Build a request with the same name + education_level as the pre-inserted subject.
	// -----------------------------------------------------------------------
	body := map[string]any{
		"name":            "Fizică", // same as inserted above
		"education_level": "high",   // same as inserted above
		"has_thesis":      true,
	}
	req := postSubjectJSON(t, body)
	req, rollback := withTenantCtx(t, pool, req, school1ID, adminID, "admin")
	defer rollback()

	// -----------------------------------------------------------------------
	// 4. Call the handler.
	// -----------------------------------------------------------------------
	rr := httptest.NewRecorder()
	h := buildSchoolHandler(pool)
	h.CreateSubject(rr, req)

	// -----------------------------------------------------------------------
	// 5. Assert that the response is NOT 201 Created.
	// -----------------------------------------------------------------------
	// A 201 here would mean the unique constraint is not being enforced —
	// the school would end up with two subjects named "Fizică" at "high" level,
	// which would cause confusion in grade entry and reporting.
	// The handler should detect pgconn error code 23505 and return 409 Conflict.
	if rr.Code != http.StatusConflict {
		t.Errorf("CreateSubject (duplicate): expected status 409 Conflict, got %d — body: %s",
			rr.Code, rr.Body.String())
	}
}

// ===========================================================================
// Class management tests — POST /classes and PUT /classes/{classId}
// ===========================================================================

// ---------------------------------------------------------------------------
// Class test helpers
// ---------------------------------------------------------------------------

// postClassJSON builds an *http.Request for POST /classes with a JSON body.
func postClassJSON(t *testing.T, body any) *http.Request {
	t.Helper()

	b, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("postClassJSON: marshal body: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/classes", bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	return req
}

// putClassJSON builds an *http.Request for PUT /classes/{classId} with a JSON body.
// The classId is embedded in the chi route context so the handler can read it
// via chi.URLParam(r, "classId").
func putClassJSON(t *testing.T, classID string, body any) *http.Request {
	t.Helper()

	b, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("putClassJSON: marshal body: %v", err)
	}

	// The path does not matter to chi when calling the handler directly, but
	// chi.URLParam reads from the route context, so we inject it manually below.
	req := httptest.NewRequest(http.MethodPut, "/classes/"+classID, bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")

	// chi stores URL parameters in the request context using a routeContext key.
	// We must inject {classId} so that chi.URLParam(r, "classId") works when
	// the handler is called directly (bypassing chi's router dispatch).
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("classId", classID)
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	return req
}

// decodeClassData decodes the { "data": {...} } envelope from a class response.
func decodeClassData(t *testing.T, rr *httptest.ResponseRecorder) map[string]any {
	t.Helper()

	var env struct {
		Data map[string]any `json:"data"`
	}
	if err := json.NewDecoder(rr.Body).Decode(&env); err != nil {
		t.Fatalf("decodeClassData: decode JSON envelope: %v\nbody: %s", err, rr.Body.String())
	}
	return env.Data
}

// decodeClassError decodes the { "error": { "code": ..., "message": ... } } envelope.
func decodeClassError(t *testing.T, rr *httptest.ResponseRecorder) (code, message string) {
	t.Helper()

	var env struct {
		Error struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.NewDecoder(rr.Body).Decode(&env); err != nil {
		t.Fatalf("decodeClassError: decode JSON envelope: %v\nbody: %s", err, rr.Body.String())
	}
	return env.Error.Code, env.Error.Message
}

// insertClassDirect inserts a class row directly via the pool (bypassing RLS)
// so that a subsequent handler call can trigger a duplicate constraint violation.
// This mirrors insertSubjectDirect but for the classes table.
func insertClassDirect(
	t *testing.T,
	pool *pgxpool.Pool,
	schoolID, schoolYearID uuid.UUID,
	name string,
) {
	t.Helper()

	ctx := context.Background()
	id := uuid.NewSHA1(uuid.NameSpaceURL, []byte(
		fmt.Sprintf("catalogro-test-class-%s-%s-%s", schoolID, schoolYearID, name),
	))

	_, err := pool.Exec(ctx, // nosemgrep: rls-missing-tenant-context
		`INSERT INTO classes (id, school_id, school_year_id, name, education_level, grade_number)
		VALUES ($1, $2, $3, $4, 'middle'::education_level, 5)
		ON CONFLICT (id) DO NOTHING`,
		id, schoolID, schoolYearID, name,
	)
	if err != nil {
		t.Fatalf("insertClassDirect: insert class %q: %v", name, err)
	}
}

// ---------------------------------------------------------------------------
// Test 5: CreateClass — success (POST /classes with valid body)
// ---------------------------------------------------------------------------

// TestCreateClass_Success verifies the happy path for POST /classes.
//
// Scenario:
//   - A school admin sends a valid JSON body with all required fields.
//   - The handler should return HTTP 201 Created with a JSON body containing:
//     id (UUID), school_year_id, name, education_level, grade_number,
//     homeroom_teacher_id (null), max_students.
func TestCreateClass_Success(t *testing.T) {
	// -----------------------------------------------------------------------
	// 1. Set up the database.
	// -----------------------------------------------------------------------
	pool := testutil.StartPostgres(t)
	testutil.TruncateAll(t, pool)

	school1ID, _ := testutil.SeedSchools(t, pool)
	users := testutil.SeedUsers(t, pool, school1ID)
	adminID := users["admin"]

	// Reconstruct the school year ID the same way SeedSchools creates it.
	// This is deterministic — see testutil.deterministicID.
	schoolYearID := uuid.NewSHA1(uuid.NameSpaceURL, []byte("catalogro-test-school-year-1"))

	// -----------------------------------------------------------------------
	// 2. Build the request.
	// -----------------------------------------------------------------------
	body := map[string]any{
		"school_year_id":  schoolYearID.String(),
		"name":            "5A",
		"education_level": "middle",
		"grade_number":    5,
	}
	req := postClassJSON(t, body)

	// -----------------------------------------------------------------------
	// 3. Inject auth + tenant context as admin.
	// -----------------------------------------------------------------------
	req, rollback := withTenantCtx(t, pool, req, school1ID, adminID, "admin")
	defer rollback()

	// -----------------------------------------------------------------------
	// 4. Call the handler.
	// -----------------------------------------------------------------------
	rr := httptest.NewRecorder()
	h := buildSchoolHandler(pool)
	h.CreateClass(rr, req)

	// -----------------------------------------------------------------------
	// 5. Assert HTTP 201 Created.
	// -----------------------------------------------------------------------
	if rr.Code != http.StatusCreated {
		t.Fatalf("CreateClass: expected 201, got %d — body: %s", rr.Code, rr.Body.String())
	}

	// -----------------------------------------------------------------------
	// 6. Decode and assert the response body.
	// -----------------------------------------------------------------------
	data := decodeClassData(t, rr)

	// id must be a valid UUID.
	classID, ok := data["id"].(string)
	if !ok || classID == "" {
		t.Errorf("CreateClass: expected non-empty 'id' in response, got: %v", data["id"])
	}
	if _, err := uuid.Parse(classID); err != nil {
		t.Errorf("CreateClass: 'id' is not a valid UUID: %q", classID)
	}

	// name must match what we sent.
	if name, _ := data["name"].(string); name != "5A" {
		t.Errorf("CreateClass: expected name='5A', got %q", name)
	}

	// education_level must match.
	if level, _ := data["education_level"].(string); level != "middle" {
		t.Errorf("CreateClass: expected education_level='middle', got %q", level)
	}

	// grade_number must match (JSON numbers decode as float64 in map[string]any).
	if gradeNum, _ := data["grade_number"].(float64); gradeNum != 5 {
		t.Errorf("CreateClass: expected grade_number=5, got %v", data["grade_number"])
	}
}

// ---------------------------------------------------------------------------
// Test 6: CreateClass — missing name → 400 Bad Request
// ---------------------------------------------------------------------------

// TestCreateClass_MissingName verifies that omitting the required "name" field
// causes the handler to return HTTP 400 with a MISSING_FIELD error code.
func TestCreateClass_MissingName(t *testing.T) {
	// -----------------------------------------------------------------------
	// 1. Set up the database.
	// -----------------------------------------------------------------------
	pool := testutil.StartPostgres(t)
	testutil.TruncateAll(t, pool)

	school1ID, _ := testutil.SeedSchools(t, pool)
	users := testutil.SeedUsers(t, pool, school1ID)
	adminID := users["admin"]

	schoolYearID := uuid.NewSHA1(uuid.NameSpaceURL, []byte("catalogro-test-school-year-1"))

	// -----------------------------------------------------------------------
	// 2. Build a request WITHOUT the required "name" field.
	// -----------------------------------------------------------------------
	body := map[string]any{
		"school_year_id":  schoolYearID.String(),
		"education_level": "middle",
		"grade_number":    5,
		// "name" intentionally omitted
	}
	req := postClassJSON(t, body)
	req, rollback := withTenantCtx(t, pool, req, school1ID, adminID, "admin")
	defer rollback()

	// -----------------------------------------------------------------------
	// 3. Call the handler and assert 400.
	// -----------------------------------------------------------------------
	rr := httptest.NewRecorder()
	h := buildSchoolHandler(pool)
	h.CreateClass(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("CreateClass (missing name): expected 400, got %d — body: %s",
			rr.Code, rr.Body.String())
	}

	code, _ := decodeClassError(t, rr)
	if code != "MISSING_FIELD" {
		t.Errorf("CreateClass (missing name): expected error code 'MISSING_FIELD', got %q", code)
	}
}

// ---------------------------------------------------------------------------
// Test 7: CreateClass — invalid education_level → 400 Bad Request
// ---------------------------------------------------------------------------

// TestCreateClass_InvalidEducationLevel verifies that an unrecognised
// education_level value causes the handler to return HTTP 400 with the error
// code INVALID_EDUCATION_LEVEL.
func TestCreateClass_InvalidEducationLevel(t *testing.T) {
	// -----------------------------------------------------------------------
	// 1. Set up the database.
	// -----------------------------------------------------------------------
	pool := testutil.StartPostgres(t)
	testutil.TruncateAll(t, pool)

	school1ID, _ := testutil.SeedSchools(t, pool)
	users := testutil.SeedUsers(t, pool, school1ID)
	adminID := users["admin"]

	schoolYearID := uuid.NewSHA1(uuid.NameSpaceURL, []byte("catalogro-test-school-year-1"))

	// -----------------------------------------------------------------------
	// 2. Build a request with an invalid education_level value.
	// -----------------------------------------------------------------------
	body := map[string]any{
		"school_year_id":  schoolYearID.String(),
		"name":            "5A",
		"education_level": "lyceum", // not a valid value
		"grade_number":    5,
	}
	req := postClassJSON(t, body)
	req, rollback := withTenantCtx(t, pool, req, school1ID, adminID, "admin")
	defer rollback()

	// -----------------------------------------------------------------------
	// 3. Call the handler and assert 400.
	// -----------------------------------------------------------------------
	rr := httptest.NewRecorder()
	h := buildSchoolHandler(pool)
	h.CreateClass(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("CreateClass (invalid level): expected 400, got %d — body: %s",
			rr.Code, rr.Body.String())
	}

	code, _ := decodeClassError(t, rr)
	if code != "INVALID_EDUCATION_LEVEL" {
		t.Errorf("CreateClass (invalid level): expected error code 'INVALID_EDUCATION_LEVEL', got %q", code)
	}
}

// ---------------------------------------------------------------------------
// Test 8: CreateClass — duplicate name in same school year → 409 Conflict
// ---------------------------------------------------------------------------

// TestCreateClass_DuplicateName verifies that creating two classes with the
// same name in the same school year triggers the UNIQUE constraint and results
// in HTTP 409 Conflict.
//
// The classes table has UNIQUE(school_id, school_year_id, name).
// We pre-insert a class via insertClassDirect (committed, bypassing the test
// transaction rollback) so the constraint is active when the handler runs.
func TestCreateClass_DuplicateName(t *testing.T) {
	// -----------------------------------------------------------------------
	// 1. Set up the database.
	// -----------------------------------------------------------------------
	pool := testutil.StartPostgres(t)
	testutil.TruncateAll(t, pool)

	school1ID, _ := testutil.SeedSchools(t, pool)
	users := testutil.SeedUsers(t, pool, school1ID)
	adminID := users["admin"]

	schoolYearID := uuid.NewSHA1(uuid.NameSpaceURL, []byte("catalogro-test-school-year-1"))

	// -----------------------------------------------------------------------
	// 2. Pre-insert a class so the UNIQUE constraint fires on the handler call.
	// -----------------------------------------------------------------------
	// insertClassDirect commits immediately (unlike withTenantCtx which rolls back),
	// so the unique constraint on (school_id, school_year_id, name) is active.
	insertClassDirect(t, pool, school1ID, schoolYearID, "6A")

	// -----------------------------------------------------------------------
	// 3. Build a request with the same name.
	// -----------------------------------------------------------------------
	body := map[string]any{
		"school_year_id":  schoolYearID.String(),
		"name":            "6A", // same as pre-inserted row
		"education_level": "middle",
		"grade_number":    6,
	}
	req := postClassJSON(t, body)
	req, rollback := withTenantCtx(t, pool, req, school1ID, adminID, "admin")
	defer rollback()

	// -----------------------------------------------------------------------
	// 4. Call the handler and assert 409 Conflict.
	// -----------------------------------------------------------------------
	rr := httptest.NewRecorder()
	h := buildSchoolHandler(pool)
	h.CreateClass(rr, req)

	if rr.Code != http.StatusConflict {
		t.Errorf("CreateClass (duplicate): expected 409 Conflict, got %d — body: %s",
			rr.Code, rr.Body.String())
	}
}

// ---------------------------------------------------------------------------
// Test 9: UpdateClass — success (PUT /classes/{classId})
// ---------------------------------------------------------------------------

// TestUpdateClass_Success verifies the happy path for PUT /classes/{classId}.
//
// Scenario:
//   - An existing class "9A" is created via SeedClass.
//   - The admin sends a request to rename it to "9B" and set a max_students of 28.
//   - The handler returns HTTP 200 OK with the updated class in the response body.
func TestUpdateClass_Success(t *testing.T) {
	// -----------------------------------------------------------------------
	// 1. Set up the database with a seeded class.
	// -----------------------------------------------------------------------
	pool := testutil.StartPostgres(t)
	testutil.TruncateAll(t, pool)

	school1ID, _ := testutil.SeedSchools(t, pool)
	users := testutil.SeedUsers(t, pool, school1ID)
	adminID := users["admin"]
	teacherID := users["teacher"]

	// SeedClass creates class "9A" with the teacher as homeroom teacher and
	// inserts the related subject and enrollment. Returns the class UUID.
	classID := testutil.SeedClass(t, pool, school1ID, teacherID)

	// -----------------------------------------------------------------------
	// 2. Build the update request.
	// -----------------------------------------------------------------------
	newMaxStudents := int16(28)
	body := map[string]any{
		"name":         "9B",        // rename from "9A" to "9B"
		"max_students": newMaxStudents, // reduce capacity
	}
	req := putClassJSON(t, classID.String(), body)

	// -----------------------------------------------------------------------
	// 3. Inject auth + tenant context as admin.
	// -----------------------------------------------------------------------
	req, rollback := withTenantCtx(t, pool, req, school1ID, adminID, "admin")
	defer rollback()

	// -----------------------------------------------------------------------
	// 4. Call the handler.
	// -----------------------------------------------------------------------
	rr := httptest.NewRecorder()
	h := buildSchoolHandler(pool)
	h.UpdateClass(rr, req)

	// -----------------------------------------------------------------------
	// 5. Assert HTTP 200 OK.
	// -----------------------------------------------------------------------
	if rr.Code != http.StatusOK {
		t.Fatalf("UpdateClass: expected 200, got %d — body: %s", rr.Code, rr.Body.String())
	}

	// -----------------------------------------------------------------------
	// 6. Decode and assert the response body.
	// -----------------------------------------------------------------------
	data := decodeClassData(t, rr)

	// id must match the class we updated.
	if id, _ := data["id"].(string); id != classID.String() {
		t.Errorf("UpdateClass: expected id=%s, got %q", classID, id)
	}

	// name must reflect the new value.
	if name, _ := data["name"].(string); name != "9B" {
		t.Errorf("UpdateClass: expected name='9B', got %q", name)
	}

	// max_students must reflect the new value.
	// JSON numbers decode as float64 when the target is map[string]any.
	if ms, _ := data["max_students"].(float64); ms != float64(newMaxStudents) {
		t.Errorf("UpdateClass: expected max_students=%d, got %v", newMaxStudents, data["max_students"])
	}
}

// ---------------------------------------------------------------------------
// Test 10: UpdateClass — non-existent class ID → 404 Not Found
// ---------------------------------------------------------------------------

// TestUpdateClass_NotFound verifies that updating a class that does not exist
// (or belongs to a different tenant) returns HTTP 404 Not Found.
//
// The handler calls GetClassByID first. If that returns pgx.ErrNoRows, the
// handler must return 404 immediately without attempting a database UPDATE.
func TestUpdateClass_NotFound(t *testing.T) {
	// -----------------------------------------------------------------------
	// 1. Set up the database (no class inserted for this ID).
	// -----------------------------------------------------------------------
	pool := testutil.StartPostgres(t)
	testutil.TruncateAll(t, pool)

	school1ID, _ := testutil.SeedSchools(t, pool)
	users := testutil.SeedUsers(t, pool, school1ID)
	adminID := users["admin"]

	// Use a random UUID that definitely does not exist in the database.
	// uuid.New() generates a V4 (random) UUID, which has negligible collision probability.
	nonExistentID := uuid.New()

	// -----------------------------------------------------------------------
	// 2. Build the update request with the non-existent ID.
	// -----------------------------------------------------------------------
	body := map[string]any{
		"name": "Phantom Class",
	}
	req := putClassJSON(t, nonExistentID.String(), body)

	req, rollback := withTenantCtx(t, pool, req, school1ID, adminID, "admin")
	defer rollback()

	// -----------------------------------------------------------------------
	// 3. Call the handler and assert 404.
	// -----------------------------------------------------------------------
	rr := httptest.NewRecorder()
	h := buildSchoolHandler(pool)
	h.UpdateClass(rr, req)

	if rr.Code != http.StatusNotFound {
		t.Fatalf("UpdateClass (not found): expected 404, got %d — body: %s",
			rr.Code, rr.Body.String())
	}
}

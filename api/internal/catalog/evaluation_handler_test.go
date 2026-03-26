// Package catalog_test contains integration tests for the descriptive evaluation
// HTTP handlers (evaluation_handler.go).
//
// # What these tests verify
//
// The descriptive evaluation endpoints are tested end-to-end against a real
// PostgreSQL 17 container (via testcontainers-go):
//
//	GET    /catalog/classes/{classId}/subjects/{subjectId}/evaluations — list evaluations
//	POST   /catalog/evaluations                                       — create evaluation
//	PUT    /catalog/evaluations/{evalId}                              — update evaluation
//	DELETE /catalog/evaluations/{evalId}                              — delete evaluation
//
// # Testing strategy
//
// Same approach as the school handler tests: real PostgreSQL container, real RLS
// policies, fake JWT claims injected via auth.WithClaims/WithQueries.
// Each test uses a transaction that is rolled back at the end (hermetic isolation).
//
// # Running these tests
//
//	go test ./internal/catalog/ -v -run TestEvaluation -count=1 -timeout 180s
//
// Docker must be running. The first run pulls postgres:17-alpine (~30 MB).
package catalog_test

import (
	"bytes"
	"context"
	"encoding/json"
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
	"github.com/vlahsh/catalogro/api/internal/catalog"
	"github.com/vlahsh/catalogro/api/internal/testutil"
)

// ---------------------------------------------------------------------------
// Test helpers
// ---------------------------------------------------------------------------

// testLogger returns a slog.Logger that writes to os.Stderr at Debug level.
// Using a real logger means handler log lines appear in test output with -v.
func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
		Level: slog.LevelDebug,
	}))
}

// buildCatalogHandler constructs a catalog.Handler wired to the given pool.
func buildCatalogHandler(pool *pgxpool.Pool) *catalog.Handler {
	queries := generated.New(pool)
	return catalog.NewHandler(queries, testLogger())
}

// withTenantCtx sets up the request context as the real TenantContext middleware
// would: begins a PostgreSQL transaction, sets the RLS tenant, creates a
// transaction-scoped Queries object, and stores both Queries and fake JWT Claims
// in the request context.
//
// Returns the augmented *http.Request and a rollback function (call with defer).
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

	// Begin a real PostgreSQL transaction.
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("withTenantCtx: begin transaction: %v", err)
	}

	// Set the RLS tenant context inside the transaction.
	_, err = tx.Exec(ctx, // nosemgrep: rls-missing-tenant-context
		"SELECT set_config('app.current_school_id', $1, true)",
		schoolID.String(),
	)
	if err != nil {
		_ = tx.Rollback(ctx)
		t.Fatalf("withTenantCtx: set_config tenant: %v", err)
	}

	// Create a Queries object bound to this transaction.
	queries := generated.New(pool).WithTx(tx)

	// Build fake JWT claims representing the requesting user.
	claims := &auth.Claims{
		UserID:   userID.String(),
		SchoolID: schoolID.String(),
		Role:     role,
	}

	// Inject the Queries and Claims into the request context.
	ctx = auth.WithQueries(ctx, queries)
	ctx = auth.WithClaims(ctx, claims)

	rollback := func() {
		_ = tx.Rollback(context.Background())
	}
	return r.WithContext(ctx), rollback
}

// postJSON builds an *http.Request for a POST to the given path with a JSON body.
func postJSON(t *testing.T, path string, body any) *http.Request {
	t.Helper()

	b, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("postJSON: marshal body: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, path, bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	return req
}

// putJSON builds an *http.Request for a PUT to the given path with a JSON body.
func putJSON(t *testing.T, path string, body any) *http.Request {
	t.Helper()

	b, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("putJSON: marshal body: %v", err)
	}

	req := httptest.NewRequest(http.MethodPut, path, bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	return req
}

// withChiURLParam injects a chi URL parameter into the request context.
// This simulates what the chi router does when matching a route like
// /evaluations/{evalId} — it stores the captured parameter in the context.
func withChiURLParam(r *http.Request, key, value string) *http.Request {
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add(key, value)
	return r.WithContext(context.WithValue(r.Context(), chi.RouteCtxKey, rctx))
}

// withChiURLParams injects multiple chi URL parameters into the request context.
// Used for routes like /classes/{classId}/subjects/{subjectId}/evaluations.
func withChiURLParams(r *http.Request, params map[string]string) *http.Request {
	rctx := chi.NewRouteContext()
	for k, v := range params {
		rctx.URLParams.Add(k, v)
	}
	return r.WithContext(context.WithValue(r.Context(), chi.RouteCtxKey, rctx))
}

// decodeDataMap decodes the standard { "data": {...} } API envelope and returns
// the inner data map. Decoding failures abort the test.
func decodeDataMap(t *testing.T, rr *httptest.ResponseRecorder) map[string]any {
	t.Helper()

	var env struct {
		Data map[string]any `json:"data"`
	}
	if err := json.NewDecoder(rr.Body).Decode(&env); err != nil {
		t.Fatalf("decodeDataMap: decode JSON: %v\nbody: %s", err, rr.Body.String())
	}
	return env.Data
}

// decodeError decodes the standard error envelope and returns code + message.
func decodeError(t *testing.T, rr *httptest.ResponseRecorder) (code, message string) {
	t.Helper()

	var env struct {
		Error struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.NewDecoder(rr.Body).Decode(&env); err != nil {
		t.Fatalf("decodeError: decode JSON: %v\nbody: %s", err, rr.Body.String())
	}
	return env.Error.Code, env.Error.Message
}

// insertEvaluationDirect inserts a descriptive evaluation directly via the
// superuser pool (bypassing handlers and RLS). This is used by multi-step tests
// (update, delete, list) that need pre-existing data in the database.
//
// Returns the evaluation's UUID.
func insertEvaluationDirect(
	t *testing.T,
	pool *pgxpool.Pool,
	schoolID, studentID, classID, subjectID, teacherID uuid.UUID,
	semester, content string,
) uuid.UUID {
	t.Helper()

	ctx := context.Background()
	evalID := uuid.New()

	// Look up the school year for this school.
	schoolYearID := uuid.NewSHA1(uuid.NameSpaceURL, []byte("catalogro-test-school-year-1"))
	school1ID := uuid.NewSHA1(uuid.NameSpaceURL, []byte("catalogro-test-school-1"))
	if schoolID != school1ID {
		schoolYearID = uuid.NewSHA1(uuid.NameSpaceURL, []byte("catalogro-test-school-year-2"))
	}

	_, err := pool.Exec(ctx, // nosemgrep: rls-missing-tenant-context
		`INSERT INTO descriptive_evaluations
			(id, school_id, student_id, class_id, subject_id, teacher_id,
			school_year_id, semester, content)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8::semester, $9)`,
		evalID, schoolID, studentID, classID, subjectID, teacherID,
		schoolYearID, semester, content,
	)
	if err != nil {
		t.Fatalf("insertEvaluationDirect: %v", err)
	}
	return evalID
}

// ---------------------------------------------------------------------------
// Test 1: CreateEvaluation — success
// ---------------------------------------------------------------------------

// TestCreateEvaluation_Success verifies that a teacher assigned to a primary
// class+subject can create a descriptive evaluation for a student.
func TestCreateEvaluation_Success(t *testing.T) {
	// 1. Set up the database.
	pool := testutil.StartPostgres(t)
	testutil.TruncateAll(t, pool)

	school1ID, _ := testutil.SeedSchools(t, pool)
	users := testutil.SeedUsers(t, pool, school1ID)
	teacherID := users["teacher"]
	primary := testutil.SeedPrimaryClass(t, pool, school1ID, teacherID)

	// 2. Build the request — teacher creates an evaluation for the enrolled student.
	body := map[string]any{
		"student_id": primary.StudentID.String(),
		"class_id":   primary.ClassID.String(),
		"subject_id": primary.SubjectID.String(),
		"semester":   "I",
		"content":    "Elevul demonstrează progres în citire și scriere. Participă activ la ore.",
	}
	req := postJSON(t, "/catalog/evaluations", body)

	// 3. Inject auth context as the assigned teacher.
	req, rollback := withTenantCtx(t, pool, req, school1ID, teacherID, "teacher")
	defer rollback()

	// 4. Call the handler.
	rr := httptest.NewRecorder()
	h := buildCatalogHandler(pool)
	h.CreateEvaluation(rr, req)

	// 5. Assert HTTP 201 Created.
	if rr.Code != http.StatusCreated {
		t.Fatalf("CreateEvaluation: expected 201, got %d — body: %s", rr.Code, rr.Body.String())
	}

	// 6. Decode and assert the response body.
	data := decodeDataMap(t, rr)

	// id must be a valid UUID.
	evalID, ok := data["id"].(string)
	if !ok || evalID == "" {
		t.Errorf("CreateEvaluation: expected non-empty 'id' in response, got: %v", data["id"])
	}
	if _, err := uuid.Parse(evalID); err != nil {
		t.Errorf("CreateEvaluation: 'id' is not a valid UUID: %q", evalID)
	}

	// content must match what was sent.
	if content, _ := data["content"].(string); content != body["content"] {
		t.Errorf("CreateEvaluation: expected content to match, got %q", content)
	}

	// semester must be "I".
	if semester, _ := data["semester"].(string); semester != "I" {
		t.Errorf("CreateEvaluation: expected semester='I', got %q", semester)
	}
}

// ---------------------------------------------------------------------------
// Test 2: CreateEvaluation — empty content → 400
// ---------------------------------------------------------------------------

// TestCreateEvaluation_EmptyContent verifies that submitting an empty content
// string returns 400 Bad Request with the appropriate error code.
func TestCreateEvaluation_EmptyContent(t *testing.T) {
	pool := testutil.StartPostgres(t)
	testutil.TruncateAll(t, pool)

	school1ID, _ := testutil.SeedSchools(t, pool)
	users := testutil.SeedUsers(t, pool, school1ID)
	teacherID := users["teacher"]
	primary := testutil.SeedPrimaryClass(t, pool, school1ID, teacherID)

	body := map[string]any{
		"student_id": primary.StudentID.String(),
		"class_id":   primary.ClassID.String(),
		"subject_id": primary.SubjectID.String(),
		"semester":   "I",
		"content":    "", // intentionally empty
	}
	req := postJSON(t, "/catalog/evaluations", body)
	req, rollback := withTenantCtx(t, pool, req, school1ID, teacherID, "teacher")
	defer rollback()

	rr := httptest.NewRecorder()
	h := buildCatalogHandler(pool)
	h.CreateEvaluation(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("CreateEvaluation (empty content): expected 400, got %d — body: %s",
			rr.Code, rr.Body.String())
	}

	code, _ := decodeError(t, rr)
	if code != "MISSING_FIELD" {
		t.Errorf("CreateEvaluation (empty content): expected error code 'MISSING_FIELD', got %q", code)
	}
}

// ---------------------------------------------------------------------------
// Test 3: CreateEvaluation — wrong teacher → 403
// ---------------------------------------------------------------------------

// TestCreateEvaluation_WrongTeacher verifies that a teacher NOT assigned to the
// class+subject receives 403 Forbidden when trying to create an evaluation.
func TestCreateEvaluation_WrongTeacher(t *testing.T) {
	pool := testutil.StartPostgres(t)
	testutil.TruncateAll(t, pool)

	school1ID, _ := testutil.SeedSchools(t, pool)
	users := testutil.SeedUsers(t, pool, school1ID)
	teacherID := users["teacher"]
	primary := testutil.SeedPrimaryClass(t, pool, school1ID, teacherID)

	// Use the secretary as "wrong teacher" — they are a valid user but not
	// assigned to teach in the primary class.
	wrongUserID := users["secretary"]

	body := map[string]any{
		"student_id": primary.StudentID.String(),
		"class_id":   primary.ClassID.String(),
		"subject_id": primary.SubjectID.String(),
		"semester":   "I",
		"content":    "This should be forbidden.",
	}
	req := postJSON(t, "/catalog/evaluations", body)

	// Use the wrong user with "teacher" role — they aren't assigned to this class.
	req, rollback := withTenantCtx(t, pool, req, school1ID, wrongUserID, "teacher")
	defer rollback()

	rr := httptest.NewRecorder()
	h := buildCatalogHandler(pool)
	h.CreateEvaluation(rr, req)

	if rr.Code != http.StatusForbidden {
		t.Fatalf("CreateEvaluation (wrong teacher): expected 403, got %d — body: %s",
			rr.Code, rr.Body.String())
	}
}

// ---------------------------------------------------------------------------
// Test 4: UpdateEvaluation — success
// ---------------------------------------------------------------------------

// TestUpdateEvaluation_Success verifies that the teacher who created an evaluation
// can update its content.
func TestUpdateEvaluation_Success(t *testing.T) {
	pool := testutil.StartPostgres(t)
	testutil.TruncateAll(t, pool)

	school1ID, _ := testutil.SeedSchools(t, pool)
	users := testutil.SeedUsers(t, pool, school1ID)
	teacherID := users["teacher"]
	primary := testutil.SeedPrimaryClass(t, pool, school1ID, teacherID)

	// --- Step 1: Insert an evaluation directly via the pool (bypassing handler)
	// so it's committed and visible to the handler's transaction.
	evalID := insertEvaluationDirect(t, pool, school1ID,
		primary.StudentID, primary.ClassID, primary.SubjectID, teacherID,
		"I", "Evaluare inițială.")

	// --- Step 2: Update the evaluation content via the handler.
	updateBody := map[string]any{
		"content": "Evaluare actualizată cu observații detaliate despre progresul elevului.",
	}
	updateReq := putJSON(t, "/catalog/evaluations/"+evalID.String(), updateBody)
	updateReq = withChiURLParam(updateReq, "evalId", evalID.String())
	updateReq, rollback := withTenantCtx(t, pool, updateReq, school1ID, teacherID, "teacher")
	defer rollback()

	updateRR := httptest.NewRecorder()
	h := buildCatalogHandler(pool)
	h.UpdateEvaluation(updateRR, updateReq)

	// Assert HTTP 200 OK.
	if updateRR.Code != http.StatusOK {
		t.Fatalf("UpdateEvaluation: expected 200, got %d — body: %s", updateRR.Code, updateRR.Body.String())
	}

	// Assert the content was updated.
	updateData := decodeDataMap(t, updateRR)
	if content, _ := updateData["content"].(string); content != updateBody["content"] {
		t.Errorf("UpdateEvaluation: expected updated content, got %q", content)
	}
}

// ---------------------------------------------------------------------------
// Test 5: UpdateEvaluation — not found → 404
// ---------------------------------------------------------------------------

// TestUpdateEvaluation_NotFound verifies that updating a non-existent evaluation
// returns 404 Not Found.
func TestUpdateEvaluation_NotFound(t *testing.T) {
	pool := testutil.StartPostgres(t)
	testutil.TruncateAll(t, pool)

	school1ID, _ := testutil.SeedSchools(t, pool)
	users := testutil.SeedUsers(t, pool, school1ID)
	teacherID := users["teacher"]

	// Use a random UUID that doesn't exist in the database.
	fakeEvalID := uuid.New().String()

	updateBody := map[string]any{
		"content": "This evaluation does not exist.",
	}
	req := putJSON(t, "/catalog/evaluations/"+fakeEvalID, updateBody)
	req = withChiURLParam(req, "evalId", fakeEvalID)
	req, rollback := withTenantCtx(t, pool, req, school1ID, teacherID, "teacher")
	defer rollback()

	rr := httptest.NewRecorder()
	h := buildCatalogHandler(pool)
	h.UpdateEvaluation(rr, req)

	if rr.Code != http.StatusNotFound {
		t.Fatalf("UpdateEvaluation (not found): expected 404, got %d — body: %s",
			rr.Code, rr.Body.String())
	}
}

// ---------------------------------------------------------------------------
// Test 6: DeleteEvaluation — success
// ---------------------------------------------------------------------------

// TestDeleteEvaluation_Success verifies that the teacher who created an evaluation
// can delete it, and the response confirms the deletion.
func TestDeleteEvaluation_Success(t *testing.T) {
	pool := testutil.StartPostgres(t)
	testutil.TruncateAll(t, pool)

	school1ID, _ := testutil.SeedSchools(t, pool)
	users := testutil.SeedUsers(t, pool, school1ID)
	teacherID := users["teacher"]
	primary := testutil.SeedPrimaryClass(t, pool, school1ID, teacherID)

	// Insert an evaluation directly via the pool so it's committed and visible.
	evalID := insertEvaluationDirect(t, pool, school1ID,
		primary.StudentID, primary.ClassID, primary.SubjectID, teacherID,
		"I", "Evaluare care va fi ștearsă.")

	// Delete the evaluation via the handler.
	deleteReq := httptest.NewRequest(http.MethodDelete, "/catalog/evaluations/"+evalID.String(), http.NoBody)
	deleteReq = withChiURLParam(deleteReq, "evalId", evalID.String())
	deleteReq, rollback := withTenantCtx(t, pool, deleteReq, school1ID, teacherID, "teacher")
	defer rollback()

	deleteRR := httptest.NewRecorder()
	h := buildCatalogHandler(pool)
	h.DeleteEvaluation(deleteRR, deleteReq)

	// Assert HTTP 200 OK.
	if deleteRR.Code != http.StatusOK {
		t.Fatalf("DeleteEvaluation: expected 200, got %d — body: %s", deleteRR.Code, deleteRR.Body.String())
	}

	// Assert the response confirms deletion.
	deleteData := decodeDataMap(t, deleteRR)
	if deleted, _ := deleteData["deleted"].(bool); !deleted {
		t.Errorf("DeleteEvaluation: expected deleted=true, got %v", deleteData["deleted"])
	}
}

// ---------------------------------------------------------------------------
// Test 7: ListEvaluations — returns created evaluations
// ---------------------------------------------------------------------------

// TestListEvaluations_ReturnsCreated verifies that after inserting a descriptive
// evaluation, the list endpoint returns it in the response.
func TestListEvaluations_ReturnsCreated(t *testing.T) {
	pool := testutil.StartPostgres(t)
	testutil.TruncateAll(t, pool)

	school1ID, _ := testutil.SeedSchools(t, pool)
	users := testutil.SeedUsers(t, pool, school1ID)
	teacherID := users["teacher"]
	primary := testutil.SeedPrimaryClass(t, pool, school1ID, teacherID)

	// Insert an evaluation directly via the pool so it's committed and visible.
	evalContent := "Elevul face progrese excelente în lectură."
	insertEvaluationDirect(t, pool, school1ID,
		primary.StudentID, primary.ClassID, primary.SubjectID, teacherID,
		"I", evalContent)

	// List evaluations for the same class/subject/semester.
	schoolYearID := uuid.NewSHA1(uuid.NameSpaceURL, []byte("catalogro-test-school-year-1"))

	listURL := "/catalog/classes/" + primary.ClassID.String() +
		"/subjects/" + primary.SubjectID.String() +
		"/evaluations?semester=I&school_year_id=" + schoolYearID.String()

	listReq := httptest.NewRequest(http.MethodGet, listURL, http.NoBody)
	listReq = withChiURLParams(listReq, map[string]string{
		"classId":   primary.ClassID.String(),
		"subjectId": primary.SubjectID.String(),
	})
	listReq, rollback := withTenantCtx(t, pool, listReq, school1ID, teacherID, "teacher")
	defer rollback()

	listRR := httptest.NewRecorder()
	h := buildCatalogHandler(pool)
	h.ListEvaluations(listRR, listReq)

	// Assert HTTP 200 OK.
	if listRR.Code != http.StatusOK {
		t.Fatalf("ListEvaluations: expected 200, got %d — body: %s", listRR.Code, listRR.Body.String())
	}

	// Decode the response and check the students array.
	var listEnv struct {
		Data struct {
			Students []struct {
				Student struct {
					ID string `json:"id"`
				} `json:"student"`
				Evaluation *struct {
					ID      string `json:"id"`
					Content string `json:"content"`
				} `json:"evaluation"`
			} `json:"students"`
		} `json:"data"`
	}
	if err := json.NewDecoder(listRR.Body).Decode(&listEnv); err != nil {
		t.Fatalf("ListEvaluations: decode response: %v", err)
	}

	// There should be at least one student with an evaluation.
	if len(listEnv.Data.Students) == 0 {
		t.Fatal("ListEvaluations: expected at least one student, got 0")
	}

	// Find the student we created the evaluation for.
	found := false
	for _, s := range listEnv.Data.Students {
		if s.Student.ID == primary.StudentID.String() && s.Evaluation != nil {
			found = true
			if s.Evaluation.Content != evalContent {
				t.Errorf("ListEvaluations: expected content to match, got %q", s.Evaluation.Content)
			}
		}
	}
	if !found {
		t.Error("ListEvaluations: student with evaluation not found in response")
	}
}

// ---------------------------------------------------------------------------
// Test 8: CreateEvaluation — admin bypasses teacher assignment check
// ---------------------------------------------------------------------------

// TestCreateEvaluation_AdminBypass verifies that an admin can create a
// descriptive evaluation even without a teacher assignment for the class+subject.
func TestCreateEvaluation_AdminBypass(t *testing.T) {
	pool := testutil.StartPostgres(t)
	testutil.TruncateAll(t, pool)

	school1ID, _ := testutil.SeedSchools(t, pool)
	users := testutil.SeedUsers(t, pool, school1ID)
	adminID := users["admin"]
	teacherID := users["teacher"]
	primary := testutil.SeedPrimaryClass(t, pool, school1ID, teacherID)

	// The admin is NOT the assigned teacher, but should still be allowed.
	body := map[string]any{
		"student_id": primary.StudentID.String(),
		"class_id":   primary.ClassID.String(),
		"subject_id": primary.SubjectID.String(),
		"semester":   "I",
		"content":    "Admin override — evaluare adăugată de director.",
	}
	req := postJSON(t, "/catalog/evaluations", body)
	req, rollback := withTenantCtx(t, pool, req, school1ID, adminID, "admin")
	defer rollback()

	rr := httptest.NewRecorder()
	h := buildCatalogHandler(pool)
	h.CreateEvaluation(rr, req)

	if rr.Code != http.StatusCreated {
		t.Fatalf("CreateEvaluation (admin): expected 201, got %d — body: %s", rr.Code, rr.Body.String())
	}
}

// ---------------------------------------------------------------------------
// Test 9: CreateEvaluation — invalid semester → 400
// ---------------------------------------------------------------------------

// TestCreateEvaluation_InvalidSemester verifies that an invalid semester value
// returns 400 Bad Request.
func TestCreateEvaluation_InvalidSemester(t *testing.T) {
	pool := testutil.StartPostgres(t)
	testutil.TruncateAll(t, pool)

	school1ID, _ := testutil.SeedSchools(t, pool)
	users := testutil.SeedUsers(t, pool, school1ID)
	teacherID := users["teacher"]
	primary := testutil.SeedPrimaryClass(t, pool, school1ID, teacherID)

	body := map[string]any{
		"student_id": primary.StudentID.String(),
		"class_id":   primary.ClassID.String(),
		"subject_id": primary.SubjectID.String(),
		"semester":   "III", // invalid — only "I" and "II" are valid
		"content":    "This should fail.",
	}
	req := postJSON(t, "/catalog/evaluations", body)
	req, rollback := withTenantCtx(t, pool, req, school1ID, teacherID, "teacher")
	defer rollback()

	rr := httptest.NewRecorder()
	h := buildCatalogHandler(pool)
	h.CreateEvaluation(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("CreateEvaluation (invalid semester): expected 400, got %d — body: %s",
			rr.Code, rr.Body.String())
	}

	code, _ := decodeError(t, rr)
	if code != "INVALID_SEMESTER" {
		t.Errorf("CreateEvaluation (invalid semester): expected 'INVALID_SEMESTER', got %q", code)
	}
}

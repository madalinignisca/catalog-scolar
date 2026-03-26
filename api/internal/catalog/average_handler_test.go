// Package catalog_test contains integration tests for the semester close
// and average approval HTTP handlers (average_handler.go).
//
// # What these tests verify
//
// The average computation and approval endpoints are tested end-to-end
// against a real PostgreSQL 17 container (via testcontainers-go).
//
// # Running these tests
//
//	go test ./internal/catalog/ -v -run TestAverage -count=1 -timeout 180s
package catalog_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/vlahsh/catalogro/api/internal/testutil"
)

// ---------------------------------------------------------------------------
// Seed helpers for average tests
// ---------------------------------------------------------------------------

// seedGrades inserts numeric grades for a student/subject/semester.
// Used by close-average tests to have data for average computation.
func seedGrades(
	t *testing.T,
	pool *pgxpool.Pool,
	schoolID, studentID, classID, subjectID, teacherID uuid.UUID,
	semester string,
	grades []int16,
	thesisGrade *int16,
) {
	t.Helper()
	ctx := context.Background()

	schoolYearID := testutil.DeterministicID("school-year-1")
	school1ID := testutil.DeterministicID("school-1")
	if schoolID != school1ID {
		schoolYearID = testutil.DeterministicID("school-year-2")
	}

	for i, g := range grades {
		grade := g
		clientID := uuid.New() // Unique client_id to satisfy UNIQUE NULLS NOT DISTINCT constraint.
		_, err := pool.Exec(ctx, // nosemgrep: rls-missing-tenant-context
			`INSERT INTO grades (school_id, student_id, class_id, subject_id,
				teacher_id, school_year_id, semester, numeric_grade, is_thesis, grade_date, client_id)
			VALUES ($1, $2, $3, $4, $5, $6, $7::semester, $8, false, CURRENT_DATE + ($9::int), $10)`,
			schoolID, studentID, classID, subjectID,
			teacherID, schoolYearID, semester, grade, i, clientID,
		)
		if err != nil {
			t.Fatalf("seedGrades: insert grade %d: %v", i, err)
		}
	}

	if thesisGrade != nil {
		clientID := uuid.New()
		_, err := pool.Exec(ctx, // nosemgrep: rls-missing-tenant-context
			`INSERT INTO grades (school_id, student_id, class_id, subject_id,
				teacher_id, school_year_id, semester, numeric_grade, is_thesis, grade_date, client_id)
			VALUES ($1, $2, $3, $4, $5, $6, $7::semester, $8, true, CURRENT_DATE + 100::int, $9)`,
			schoolID, studentID, classID, subjectID,
			teacherID, schoolYearID, semester, *thesisGrade, clientID,
		)
		if err != nil {
			t.Fatalf("seedGrades: insert thesis grade: %v", err)
		}
	}
}

// seedEvalConfig inserts an evaluation config for a school/level/year.
func seedEvalConfig(
	t *testing.T,
	pool *pgxpool.Pool,
	schoolID uuid.UUID,
	level string,
	useQualifiers bool,
	thesisWeight *float64,
	minGrades int,
) {
	t.Helper()
	ctx := context.Background()

	schoolYearID := testutil.DeterministicID("school-year-1")
	school1ID := testutil.DeterministicID("school-1")
	if schoolID != school1ID {
		schoolYearID = testutil.DeterministicID("school-year-2")
	}

	_, err := pool.Exec(ctx, // nosemgrep: rls-missing-tenant-context
		`INSERT INTO evaluation_configs
			(school_id, education_level, school_year_id, use_qualifiers,
			min_grade, max_grade, thesis_weight, min_grades_sem)
		VALUES ($1, $2::education_level, $3, $4, 1, 10, $5, $6)
		ON CONFLICT (school_id, education_level, school_year_id) DO UPDATE SET
			use_qualifiers = EXCLUDED.use_qualifiers,
			thesis_weight = EXCLUDED.thesis_weight,
			min_grades_sem = EXCLUDED.min_grades_sem`,
		schoolID, level, schoolYearID, useQualifiers, thesisWeight, minGrades,
	)
	if err != nil {
		t.Fatalf("seedEvalConfig: %v", err)
	}
}

// ---------------------------------------------------------------------------
// Test 1: CloseAverage — success (middle school, no thesis)
// ---------------------------------------------------------------------------

// TestCloseAverage_NumericSuccess verifies that closing averages for a middle
// school class computes correct arithmetic averages.
func TestCloseAverage_NumericSuccess(t *testing.T) {
	pool := testutil.StartPostgres(t)
	testutil.TruncateAll(t, pool)

	school1ID, _ := testutil.SeedSchools(t, pool)
	users := testutil.SeedUsers(t, pool, school1ID)
	teacherID := users["teacher"]

	// SeedClass creates a high-school class by default. We need it for the
	// teacher assignment. But we need to adjust the eval config for its level.
	classID := testutil.SeedClass(t, pool, school1ID, teacherID)

	// The SeedClass creates a "high" level subject. Get the subject ID.
	suffix := school1ID.String()[:8]
	subjectID := testutil.DeterministicID("subject-" + suffix)
	studentID := testutil.DeterministicID("student-" + suffix)

	// Seed eval config for "high" level (no qualifiers, min 3 grades).
	tw := 0.0
	seedEvalConfig(t, pool, school1ID, "high", false, &tw, 3)

	// Seed grades: 8, 9, 7 → mean = 8.00
	seedGrades(t, pool, school1ID, studentID, classID, subjectID, teacherID,
		"I", []int16{8, 9, 7}, nil)

	// Build the close request.
	body := map[string]any{
		"class_id": classID.String(),
		"semester": "I",
	}
	req := postJSON(t, "/catalog/averages/"+subjectID.String()+"/close", body)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("subjectId", subjectID.String())
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	req, rollback := withTenantCtx(t, pool, req, school1ID, teacherID, "teacher")
	defer rollback()

	rr := httptest.NewRecorder()
	h := buildCatalogHandler(pool)
	h.CloseAverage(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("CloseAverage: expected 200, got %d — body: %s", rr.Code, rr.Body.String())
	}

	// Decode the response.
	var env struct {
		Data struct {
			Averages []struct {
				Average struct {
					ComputedValue *float64 `json:"computed_value"`
					FinalValue    *float64 `json:"final_value"`
					IsClosed      bool     `json:"is_closed"`
				} `json:"average"`
			} `json:"averages"`
			Closed int `json:"closed"`
		} `json:"data"`
	}
	if err := json.NewDecoder(rr.Body).Decode(&env); err != nil {
		t.Fatalf("CloseAverage: decode: %v", err)
	}

	if env.Data.Closed != 1 {
		t.Fatalf("CloseAverage: expected 1 closed, got %d", env.Data.Closed)
	}

	avg := env.Data.Averages[0].Average
	if avg.ComputedValue == nil || *avg.ComputedValue != 8.0 {
		t.Errorf("CloseAverage: expected computed_value=8.0, got %v", avg.ComputedValue)
	}
	if !avg.IsClosed {
		t.Error("CloseAverage: expected is_closed=true")
	}
}

// ---------------------------------------------------------------------------
// Test 2: CloseAverage — insufficient grades → skipped
// ---------------------------------------------------------------------------

// TestCloseAverage_InsufficientGrades verifies that students with fewer grades
// than min_grades_sem are skipped (not included in the results).
func TestCloseAverage_InsufficientGrades(t *testing.T) {
	pool := testutil.StartPostgres(t)
	testutil.TruncateAll(t, pool)

	school1ID, _ := testutil.SeedSchools(t, pool)
	users := testutil.SeedUsers(t, pool, school1ID)
	teacherID := users["teacher"]
	classID := testutil.SeedClass(t, pool, school1ID, teacherID)

	suffix := school1ID.String()[:8]
	subjectID := testutil.DeterministicID("subject-" + suffix)
	studentID := testutil.DeterministicID("student-" + suffix)

	// Require 3 minimum grades.
	tw := 0.0
	seedEvalConfig(t, pool, school1ID, "high", false, &tw, 3)

	// Only seed 2 grades (below minimum).
	seedGrades(t, pool, school1ID, studentID, classID, subjectID, teacherID,
		"I", []int16{8, 9}, nil)

	body := map[string]any{
		"class_id": classID.String(),
		"semester": "I",
	}
	req := postJSON(t, "/catalog/averages/"+subjectID.String()+"/close", body)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("subjectId", subjectID.String())
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	req, rollback := withTenantCtx(t, pool, req, school1ID, teacherID, "teacher")
	defer rollback()

	rr := httptest.NewRecorder()
	h := buildCatalogHandler(pool)
	h.CloseAverage(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("CloseAverage (insufficient): expected 200, got %d — body: %s", rr.Code, rr.Body.String())
	}

	// Decode — closed should be 0 (student skipped).
	var env struct {
		Data struct {
			Closed int `json:"closed"`
			Total  int `json:"total"`
		} `json:"data"`
	}
	if err := json.NewDecoder(rr.Body).Decode(&env); err != nil {
		t.Fatalf("CloseAverage: decode: %v", err)
	}

	if env.Data.Closed != 0 {
		t.Errorf("CloseAverage: expected 0 closed (insufficient grades), got %d", env.Data.Closed)
	}
	if env.Data.Total != 1 {
		t.Errorf("CloseAverage: expected 1 total student, got %d", env.Data.Total)
	}
}

// ---------------------------------------------------------------------------
// Test 3: CloseAverage — wrong teacher → 403
// ---------------------------------------------------------------------------

func TestCloseAverage_WrongTeacher(t *testing.T) {
	pool := testutil.StartPostgres(t)
	testutil.TruncateAll(t, pool)

	school1ID, _ := testutil.SeedSchools(t, pool)
	users := testutil.SeedUsers(t, pool, school1ID)
	teacherID := users["teacher"]
	testutil.SeedClass(t, pool, school1ID, teacherID)

	suffix := school1ID.String()[:8]
	subjectID := testutil.DeterministicID("subject-" + suffix)

	// Use a different user as "teacher" — they're not assigned.
	wrongUserID := users["secretary"]

	body := map[string]any{
		"class_id": testutil.DeterministicID("class-" + suffix).String(),
		"semester": "I",
	}
	req := postJSON(t, "/catalog/averages/"+subjectID.String()+"/close", body)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("subjectId", subjectID.String())
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	req, rollback := withTenantCtx(t, pool, req, school1ID, wrongUserID, "teacher")
	defer rollback()

	rr := httptest.NewRecorder()
	h := buildCatalogHandler(pool)
	h.CloseAverage(rr, req)

	if rr.Code != http.StatusForbidden {
		t.Fatalf("CloseAverage (wrong teacher): expected 403, got %d — body: %s",
			rr.Code, rr.Body.String())
	}
}

// ---------------------------------------------------------------------------
// Test 4: ApproveAverage — success
// ---------------------------------------------------------------------------

func TestApproveAverage_Success(t *testing.T) {
	pool := testutil.StartPostgres(t)
	testutil.TruncateAll(t, pool)

	school1ID, _ := testutil.SeedSchools(t, pool)
	users := testutil.SeedUsers(t, pool, school1ID)
	teacherID := users["teacher"]
	adminID := users["admin"]
	classID := testutil.SeedClass(t, pool, school1ID, teacherID)

	suffix := school1ID.String()[:8]
	subjectID := testutil.DeterministicID("subject-" + suffix)
	studentID := testutil.DeterministicID("student-" + suffix)
	schoolYearID := testutil.DeterministicID("school-year-1")

	// Insert a closed average directly via the pool (bypassing the handler's
	// rolled-back transaction) so it persists for the approve step.
	averageID := uuid.New()
	ctx := context.Background()
	_, err := pool.Exec(ctx, // nosemgrep: rls-missing-tenant-context
		`INSERT INTO averages (id, school_id, student_id, class_id, subject_id,
			school_year_id, semester, computed_value, final_value,
			is_closed, closed_by, closed_at)
		VALUES ($1, $2, $3, $4, $5, $6, 'I'::semester, 8.0, 8.0, true, $7, now())`,
		averageID, school1ID, studentID, classID, subjectID, schoolYearID, teacherID,
	)
	if err != nil {
		t.Fatalf("insert average: %v", err)
	}

	// Approve the average as admin.
	approveReq := httptest.NewRequest(http.MethodPost, "/catalog/averages/"+averageID.String()+"/approve", http.NoBody)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("averageId", averageID.String())
	approveReq = approveReq.WithContext(context.WithValue(approveReq.Context(), chi.RouteCtxKey, rctx))
	approveReq, rollback := withTenantCtx(t, pool, approveReq, school1ID, adminID, "admin")
	defer rollback()

	approveRR := httptest.NewRecorder()
	h := buildCatalogHandler(pool)
	h.ApproveAverage(approveRR, approveReq)

	if approveRR.Code != http.StatusOK {
		t.Fatalf("ApproveAverage: expected 200, got %d — body: %s",
			approveRR.Code, approveRR.Body.String())
	}

	// Verify the response.
	data := decodeDataMap(t, approveRR)
	if data["approved_by"] == nil {
		t.Error("ApproveAverage: expected approved_by to be set")
	}
}

// ---------------------------------------------------------------------------
// Test 5: ApproveAverage — non-admin → 403
// ---------------------------------------------------------------------------

func TestApproveAverage_NonAdmin(t *testing.T) {
	pool := testutil.StartPostgres(t)
	testutil.TruncateAll(t, pool)

	school1ID, _ := testutil.SeedSchools(t, pool)
	users := testutil.SeedUsers(t, pool, school1ID)
	teacherID := users["teacher"]

	fakeID := uuid.New().String()
	req := httptest.NewRequest(http.MethodPost, "/catalog/averages/"+fakeID+"/approve", http.NoBody)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("averageId", fakeID)
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	req, rollback := withTenantCtx(t, pool, req, school1ID, teacherID, "teacher")
	defer rollback()

	rr := httptest.NewRecorder()
	h := buildCatalogHandler(pool)
	h.ApproveAverage(rr, req)

	if rr.Code != http.StatusForbidden {
		t.Fatalf("ApproveAverage (non-admin): expected 403, got %d — body: %s",
			rr.Code, rr.Body.String())
	}
}

// ---------------------------------------------------------------------------
// Test 6: CloseAverage — with thesis weighted average
// ---------------------------------------------------------------------------

func TestCloseAverage_WithThesis(t *testing.T) {
	pool := testutil.StartPostgres(t)
	testutil.TruncateAll(t, pool)

	school1ID, _ := testutil.SeedSchools(t, pool)
	users := testutil.SeedUsers(t, pool, school1ID)
	teacherID := users["teacher"]
	classID := testutil.SeedClass(t, pool, school1ID, teacherID)

	suffix := school1ID.String()[:8]
	subjectID := testutil.DeterministicID("subject-" + suffix)
	studentID := testutil.DeterministicID("student-" + suffix)

	// Enable thesis with 25% weight.
	tw := 0.25
	seedEvalConfig(t, pool, school1ID, "high", false, &tw, 3)

	// Mark the subject as has_thesis=true.
	_, err := pool.Exec(context.Background(), // nosemgrep: rls-missing-tenant-context
		`UPDATE subjects SET has_thesis = true WHERE id = $1`, subjectID)
	if err != nil {
		t.Fatalf("update subject: %v", err)
	}

	// Grades: 8, 9, 7 → regular mean = 8.0
	// Thesis: 10
	// Weighted: 0.75 * 8.0 + 0.25 * 10 = 6.0 + 2.5 = 8.5
	thesis := int16(10)
	seedGrades(t, pool, school1ID, studentID, classID, subjectID, teacherID,
		"I", []int16{8, 9, 7}, &thesis)

	body := map[string]any{
		"class_id": classID.String(),
		"semester": "I",
	}
	req := postJSON(t, "/catalog/averages/"+subjectID.String()+"/close", body)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("subjectId", subjectID.String())
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	req, rollback := withTenantCtx(t, pool, req, school1ID, teacherID, "teacher")
	defer rollback()

	rr := httptest.NewRecorder()
	h := buildCatalogHandler(pool)
	h.CloseAverage(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("CloseAverage (thesis): expected 200, got %d — body: %s", rr.Code, rr.Body.String())
	}

	var env struct {
		Data struct {
			Averages []struct {
				Average struct {
					ComputedValue *float64 `json:"computed_value"`
				} `json:"average"`
			} `json:"averages"`
		} `json:"data"`
	}
	if err := json.NewDecoder(rr.Body).Decode(&env); err != nil {
		t.Fatalf("decode: %v", err)
	}

	if len(env.Data.Averages) == 0 {
		t.Fatal("no averages returned")
	}

	cv := env.Data.Averages[0].Average.ComputedValue
	if cv == nil || *cv != 8.5 {
		t.Errorf("CloseAverage (thesis): expected 8.5, got %v", cv)
	}
}

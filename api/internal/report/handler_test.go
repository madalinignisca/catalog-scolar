// Package report_test contains integration tests for the report handlers.
//
// # Running these tests
//
//	go test ./internal/report/ -v -run Test -count=1 -timeout 180s
package report_test

import (
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
	"github.com/vlahsh/catalogro/api/internal/report"
	"github.com/vlahsh/catalogro/api/internal/testutil"
)

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelDebug}))
}

func buildReportHandler(pool *pgxpool.Pool) *report.Handler {
	return report.NewHandler(generated.New(pool), testLogger())
}

func withTenantCtx(
	t *testing.T,
	pool *pgxpool.Pool,
	r *http.Request,
	schoolID, userID uuid.UUID,
	role string,
) (req *http.Request, rollbackFn func()) {
	t.Helper()
	ctx := r.Context()

	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("withTenantCtx: begin tx: %v", err)
	}

	_, err = tx.Exec(ctx, // nosemgrep: rls-missing-tenant-context
		"SELECT set_config('app.current_school_id', $1, true)", schoolID.String())
	if err != nil {
		_ = tx.Rollback(ctx)
		t.Fatalf("withTenantCtx: set_config: %v", err)
	}

	queries := generated.New(pool).WithTx(tx)
	claims := &auth.Claims{UserID: userID.String(), SchoolID: schoolID.String(), Role: role}
	ctx = auth.WithQueries(ctx, queries)
	ctx = auth.WithClaims(ctx, claims)

	return r.WithContext(ctx), func() { _ = tx.Rollback(context.Background()) }
}

// ---------------------------------------------------------------------------
// Test 1: Dashboard — returns counts and class summaries
// ---------------------------------------------------------------------------

func TestDashboard_Success(t *testing.T) {
	pool := testutil.StartPostgres(t)
	testutil.TruncateAll(t, pool)

	school1ID, _ := testutil.SeedSchools(t, pool)
	users := testutil.SeedUsers(t, pool, school1ID)
	adminID := users["admin"]

	// SeedClass creates a class with 1 student enrolled.
	testutil.SeedClass(t, pool, school1ID, users["teacher"])

	req := httptest.NewRequest(http.MethodGet, "/reports/dashboard", http.NoBody)
	req, rollback := withTenantCtx(t, pool, req, school1ID, adminID, "admin")
	defer rollback()

	rr := httptest.NewRecorder()
	h := buildReportHandler(pool)
	h.Dashboard(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("Dashboard: expected 200, got %d — body: %s", rr.Code, rr.Body.String())
	}

	var env struct {
		Data struct {
			TotalStudents int64            `json:"total_students"`
			TotalTeachers int64            `json:"total_teachers"`
			TotalClasses  int64            `json:"total_classes"`
			Classes       []map[string]any `json:"classes"`
		} `json:"data"`
	}
	if err := json.NewDecoder(rr.Body).Decode(&env); err != nil {
		t.Fatalf("Dashboard: decode: %v", err)
	}

	// SeedUsers creates 1 student, 1 teacher, plus admin/secretary/parent.
	if env.Data.TotalStudents < 1 {
		t.Errorf("Dashboard: expected at least 1 student, got %d", env.Data.TotalStudents)
	}
	if env.Data.TotalTeachers < 1 {
		t.Errorf("Dashboard: expected at least 1 teacher, got %d", env.Data.TotalTeachers)
	}
	if env.Data.TotalClasses < 1 {
		t.Errorf("Dashboard: expected at least 1 class, got %d", env.Data.TotalClasses)
	}
}

// ---------------------------------------------------------------------------
// Test 2: StudentReport — returns grades, absences, averages, evaluations
// ---------------------------------------------------------------------------

func TestStudentReport_Success(t *testing.T) {
	pool := testutil.StartPostgres(t)
	testutil.TruncateAll(t, pool)

	school1ID, _ := testutil.SeedSchools(t, pool)
	users := testutil.SeedUsers(t, pool, school1ID)
	adminID := users["admin"]
	studentID := testutil.DeterministicID("student-" + school1ID.String()[:8])

	req := httptest.NewRequest(http.MethodGet, "/reports/student/"+studentID.String(), http.NoBody)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("studentId", studentID.String())
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	req, rollback := withTenantCtx(t, pool, req, school1ID, adminID, "admin")
	defer rollback()

	rr := httptest.NewRecorder()
	h := buildReportHandler(pool)
	h.StudentReport(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("StudentReport: expected 200, got %d — body: %s", rr.Code, rr.Body.String())
	}

	var env struct {
		Data struct {
			StudentID string `json:"student_id"`
			Grades    []any  `json:"grades"`
			Absences  []any  `json:"absences"`
			Averages  []any  `json:"averages"`
		} `json:"data"`
	}
	if err := json.NewDecoder(rr.Body).Decode(&env); err != nil {
		t.Fatalf("StudentReport: decode: %v", err)
	}

	if env.Data.StudentID != studentID.String() {
		t.Errorf("StudentReport: expected student_id=%s, got %s", studentID, env.Data.StudentID)
	}
}

// ---------------------------------------------------------------------------
// Test 3: ClassStats — returns subject aggregates and absence summary
// ---------------------------------------------------------------------------

func TestClassStats_Success(t *testing.T) {
	pool := testutil.StartPostgres(t)
	testutil.TruncateAll(t, pool)

	school1ID, _ := testutil.SeedSchools(t, pool)
	users := testutil.SeedUsers(t, pool, school1ID)
	adminID := users["admin"]
	classID := testutil.SeedClass(t, pool, school1ID, users["teacher"])

	req := httptest.NewRequest(http.MethodGet, "/reports/class/"+classID.String()+"/stats", http.NoBody)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("classId", classID.String())
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	req, rollback := withTenantCtx(t, pool, req, school1ID, adminID, "admin")
	defer rollback()

	rr := httptest.NewRecorder()
	h := buildReportHandler(pool)
	h.ClassStats(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("ClassStats: expected 200, got %d — body: %s", rr.Code, rr.Body.String())
	}

	var env struct {
		Data struct {
			ClassID  string           `json:"class_id"`
			Subjects []map[string]any `json:"subjects"`
			Absences map[string]any   `json:"absences"`
		} `json:"data"`
	}
	if err := json.NewDecoder(rr.Body).Decode(&env); err != nil {
		t.Fatalf("ClassStats: decode: %v", err)
	}

	if env.Data.ClassID != classID.String() {
		t.Errorf("ClassStats: expected class_id=%s, got %s", classID, env.Data.ClassID)
	}

	// Absences should have the expected keys even if all zeroes.
	if env.Data.Absences["total"] == nil {
		t.Error("ClassStats: expected absences.total to be present")
	}
}

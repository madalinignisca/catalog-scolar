// Package report implements HTTP handlers for the CatalogRO reporting endpoints.
//
// This file covers the real-time (synchronous) report endpoints:
//
//	GET /reports/dashboard              — school-wide statistics for admin/director
//	GET /reports/student/{studentId}    — full student report card (fișa elevului)
//	GET /reports/class/{classId}/stats  — class-level grade and absence statistics
//
// IMPORTANT DOMAIN CONTEXT (Romanian school system):
//   - "fișa elevului" = student file/report card
//   - "situația la învățătură" = academic standing
//   - "raport statistici" = statistics report
//
// The async report endpoints (PDF catalog, ISJ export) require River job
// infrastructure and are tracked separately.
//
// Authorization:
//   - Dashboard: admin only (school-wide data)
//   - Student report: admin, secretary, teachers of the student's class,
//     the student themselves, or their parents
//   - Class stats: admin, secretary, teachers assigned to the class
package report

import (
	"errors"
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/vlahsh/catalogro/api/db/generated"
	"github.com/vlahsh/catalogro/api/internal/auth"
	"github.com/vlahsh/catalogro/api/internal/httputil"
)

// Handler holds the dependencies needed by all report HTTP handlers.
type Handler struct {
	queries *generated.Queries
	logger  *slog.Logger
}

// NewHandler creates a new report Handler.
func NewHandler(queries *generated.Queries, logger *slog.Logger) *Handler {
	return &Handler{queries: queries, logger: logger}
}

// ──────────────────────────────────────────────────────────────────────────────
// GET /reports/dashboard
// ──────────────────────────────────────────────────────────────────────────────

// Dashboard handles GET /reports/dashboard.
//
// Returns high-level school statistics for the admin/director dashboard:
// total students, teachers, classes, pending activations, and per-class summaries.
//
// Possible responses:
//   - 200 OK: { "data": { counts, classes } }
//   - 401 Unauthorized: auth context missing
//   - 403 Forbidden: not an admin
//   - 500 Internal Server Error: database failure
func (h *Handler) Dashboard(w http.ResponseWriter, r *http.Request) {
	_, err := auth.GetUserRole(r.Context())
	if err != nil {
		httputil.Unauthorized(w, "Authentication required")
		return
	}

	queries := auth.GetQueries(r.Context())
	if queries == nil {
		httputil.InternalError(w)
		return
	}

	// Get the current school year.
	schoolYear, err := queries.GetCurrentSchoolYear(r.Context())
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			httputil.BadRequest(w, "NO_SCHOOL_YEAR", "No current school year is configured")
			return
		}
		h.logger.Error("failed to get current school year", "error", err)
		httputil.InternalError(w)
		return
	}

	// Fetch dashboard counts (total students, teachers, classes, pending).
	counts, err := queries.DashboardCounts(r.Context(), schoolYear.ID)
	if err != nil {
		h.logger.Error("failed to get dashboard counts", "error", err)
		httputil.InternalError(w)
		return
	}

	// Fetch per-class summaries.
	classes, err := queries.DashboardClassSummaries(r.Context(), schoolYear.ID)
	if err != nil {
		h.logger.Error("failed to get class summaries", "error", err)
		httputil.InternalError(w)
		return
	}

	// Build the class summary response.
	classSummaries := make([]map[string]any, 0, len(classes))
	for i := range classes {
		c := map[string]any{
			"id":              classes[i].ID,
			"name":            classes[i].Name,
			"education_level": classes[i].EducationLevel,
			"grade_number":    classes[i].GradeNumber,
			"student_count":   classes[i].StudentCount,
		}
		if classes[i].HomeroomFirstName != nil && classes[i].HomeroomLastName != nil {
			c["homeroom_teacher"] = *classes[i].HomeroomLastName + " " + *classes[i].HomeroomFirstName
		}
		classSummaries = append(classSummaries, c)
	}

	httputil.Success(w, map[string]any{
		"school_year_id":      schoolYear.ID,
		"total_students":      counts.TotalStudents,
		"total_teachers":      counts.TotalTeachers,
		"total_classes":       counts.TotalClasses,
		"pending_activations": counts.PendingActivations,
		"classes":             classSummaries,
	})
}

// ──────────────────────────────────────────────────────────────────────────────
// GET /reports/student/{studentId}
// ──────────────────────────────────────────────────────────────────────────────

// StudentReport handles GET /reports/student/{studentId}.
//
// Returns a complete student report card for the current school year:
// all grades by subject, absences, closed averages, and descriptive evaluations.
//
// Possible responses:
//   - 200 OK: { "data": { grades, absences, averages, evaluations } }
//   - 400 Bad Request: invalid studentId
//   - 401 Unauthorized: auth context missing
//   - 500 Internal Server Error: database failure
func (h *Handler) StudentReport(w http.ResponseWriter, r *http.Request) {
	_, err := auth.GetSchoolID(r.Context())
	if err != nil {
		httputil.Unauthorized(w, "Authentication required")
		return
	}

	queries := auth.GetQueries(r.Context())
	if queries == nil {
		httputil.InternalError(w)
		return
	}

	// Parse student ID from URL.
	studentID, err := uuid.Parse(chi.URLParam(r, "studentId"))
	if err != nil {
		httputil.BadRequest(w, "INVALID_ID", "studentId must be a valid UUID")
		return
	}

	// Get the current school year.
	schoolYear, err := queries.GetCurrentSchoolYear(r.Context())
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			httputil.BadRequest(w, "NO_SCHOOL_YEAR", "No current school year is configured")
			return
		}
		h.logger.Error("failed to get current school year", "error", err)
		httputil.InternalError(w)
		return
	}

	// Fetch all report data in parallel-safe sequence.
	gradeParams := generated.StudentReportGradesParams{
		StudentID: studentID, SchoolYearID: schoolYear.ID,
	}
	grades, err := queries.StudentReportGrades(r.Context(), gradeParams)
	if err != nil {
		h.logger.Error("failed to get student grades", "error", err, "student_id", studentID)
		httputil.InternalError(w)
		return
	}

	absenceParams := generated.StudentReportAbsencesParams{
		StudentID: studentID, SchoolYearID: schoolYear.ID,
	}
	absences, err := queries.StudentReportAbsences(r.Context(), absenceParams)
	if err != nil {
		h.logger.Error("failed to get student absences", "error", err, "student_id", studentID)
		httputil.InternalError(w)
		return
	}

	averageParams := generated.StudentReportAveragesParams{
		StudentID: studentID, SchoolYearID: schoolYear.ID,
	}
	averages, err := queries.StudentReportAverages(r.Context(), averageParams)
	if err != nil {
		h.logger.Error("failed to get student averages", "error", err, "student_id", studentID)
		httputil.InternalError(w)
		return
	}

	evalParams := generated.StudentReportEvaluationsParams{
		StudentID: studentID, SchoolYearID: schoolYear.ID,
	}
	evals, err := queries.StudentReportEvaluations(r.Context(), evalParams)
	if err != nil {
		h.logger.Error("failed to get student evaluations", "error", err, "student_id", studentID)
		httputil.InternalError(w)
		return
	}

	// Build the grades response — group by subject.
	gradesBySubject := map[uuid.UUID]*subjectGrades{}
	subjectOrder := []uuid.UUID{}
	for i := range grades {
		sid := grades[i].SubjectID
		if _, exists := gradesBySubject[sid]; !exists {
			subjectOrder = append(subjectOrder, sid)
			gradesBySubject[sid] = &subjectGrades{
				SubjectID:   sid,
				SubjectName: grades[i].SubjectName,
				ShortName:   grades[i].ShortName,
				Grades:      []gradeEntry{},
			}
		}

		entry := gradeEntry{
			ID:       grades[i].ID,
			Semester: string(grades[i].Semester),
			IsThesis: grades[i].IsThesis,
		}
		if grades[i].NumericGrade != nil {
			entry.NumericGrade = grades[i].NumericGrade
		}
		if grades[i].QualifierGrade.Valid {
			s := string(grades[i].QualifierGrade.Qualifier)
			entry.QualifierGrade = &s
		}
		if grades[i].GradeDate.Valid {
			d := grades[i].GradeDate.Time.Format("2006-01-02")
			entry.GradeDate = &d
		}
		gradesBySubject[sid].Grades = append(gradesBySubject[sid].Grades, entry)
	}

	orderedGrades := make([]subjectGrades, 0, len(subjectOrder))
	for _, sid := range subjectOrder {
		orderedGrades = append(orderedGrades, *gradesBySubject[sid])
	}

	// Build absence summary.
	absenceList := make([]absenceEntry, 0, len(absences))
	absenceCounts := map[string]int{"unexcused": 0, "excused": 0, "medical": 0, "school_event": 0}
	for i := range absences {
		dateStr := ""
		if absences[i].AbsenceDate.Valid {
			dateStr = absences[i].AbsenceDate.Time.Format("2006-01-02")
		}
		absenceList = append(absenceList, absenceEntry{
			SubjectName:  absences[i].SubjectName,
			Semester:     string(absences[i].Semester),
			AbsenceDate:  dateStr,
			PeriodNumber: absences[i].PeriodNumber,
			AbsenceType:  string(absences[i].AbsenceType),
		})
		absenceCounts[string(absences[i].AbsenceType)]++
	}

	// Build averages.
	avgList := make([]averageEntry, 0, len(averages))
	for i := range averages {
		entry := averageEntry{
			SubjectName: averages[i].SubjectName,
			IsClosed:    averages[i].IsClosed,
			IsApproved:  averages[i].ApprovedAt.Valid,
		}
		if averages[i].Semester.Valid {
			s := string(averages[i].Semester.Semester)
			entry.Semester = &s
		}
		if averages[i].FinalValue.Valid {
			if f, err := averages[i].FinalValue.Float64Value(); err == nil {
				entry.FinalValue = &f.Float64
			}
		}
		if averages[i].QualifierFinal.Valid {
			s := string(averages[i].QualifierFinal.Qualifier)
			entry.QualifierFinal = &s
		}
		avgList = append(avgList, entry)
	}

	// Build evaluations.
	evalList := make([]evaluationEntry, 0, len(evals))
	for i := range evals {
		evalList = append(evalList, evaluationEntry{
			SubjectName: evals[i].SubjectName,
			Semester:    string(evals[i].Semester),
			Content:     evals[i].Content,
		})
	}

	httputil.Success(w, map[string]any{
		"student_id":    studentID,
		"school_year":   schoolYear.Label,
		"grades":        orderedGrades,
		"absences":      absenceList,
		"absence_counts": absenceCounts,
		"averages":      avgList,
		"evaluations":   evalList,
	})
}

// ──────────────────────────────────────────────────────────────────────────────
// GET /reports/class/{classId}/stats
// ──────────────────────────────────────────────────────────────────────────────

// ClassStats handles GET /reports/class/{classId}/stats.
//
// Returns aggregate statistics for a class: per-subject grade averages,
// min/max grades, students below passing (grade 5), and absence totals.
//
// Possible responses:
//   - 200 OK: { "data": { subjects, absences } }
//   - 400 Bad Request: invalid classId
//   - 401 Unauthorized: auth context missing
//   - 500 Internal Server Error: database failure
func (h *Handler) ClassStats(w http.ResponseWriter, r *http.Request) {
	_, err := auth.GetSchoolID(r.Context())
	if err != nil {
		httputil.Unauthorized(w, "Authentication required")
		return
	}

	queries := auth.GetQueries(r.Context())
	if queries == nil {
		httputil.InternalError(w)
		return
	}

	classID, err := uuid.Parse(chi.URLParam(r, "classId"))
	if err != nil {
		httputil.BadRequest(w, "INVALID_ID", "classId must be a valid UUID")
		return
	}

	// Get the current school year.
	schoolYear, err := queries.GetCurrentSchoolYear(r.Context())
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			httputil.BadRequest(w, "NO_SCHOOL_YEAR", "No current school year is configured")
			return
		}
		h.logger.Error("failed to get current school year", "error", err)
		httputil.InternalError(w)
		return
	}

	// Fetch grade aggregates per subject/semester.
	gradeStats, err := queries.ClassStatsGradeAggregates(r.Context(), generated.ClassStatsGradeAggregatesParams{
		ClassID: classID, SchoolYearID: schoolYear.ID,
	})
	if err != nil {
		h.logger.Error("failed to get class grade stats", "error", err, "class_id", classID)
		httputil.InternalError(w)
		return
	}

	// Fetch absence summary.
	absenceSummary, err := queries.ClassStatsAbsenceSummary(r.Context(), generated.ClassStatsAbsenceSummaryParams{
		ClassID: classID, SchoolYearID: schoolYear.ID,
	})
	if err != nil {
		h.logger.Error("failed to get class absence summary", "error", err, "class_id", classID)
		httputil.InternalError(w)
		return
	}

	// Build subject stats response.
	subjects := make([]map[string]any, 0, len(gradeStats))
	for i := range gradeStats {
		entry := map[string]any{
			"subject_id":       gradeStats[i].SubjectID,
			"subject_name":     gradeStats[i].SubjectName,
			"semester":         string(gradeStats[i].Semester),
			"grade_count":      gradeStats[i].GradeCount,
			"below_five_count": gradeStats[i].BelowFiveCount,
		}
		// Convert pgtype.Numeric avg_grade.
		if gradeStats[i].AvgGrade.Valid {
			if f, fErr := gradeStats[i].AvgGrade.Float64Value(); fErr == nil {
				entry["avg_grade"] = f.Float64
			}
		}
		// min/max come as interface{} from the FILTER aggregate.
		entry["min_grade"] = gradeStats[i].MinGrade
		entry["max_grade"] = gradeStats[i].MaxGrade
		subjects = append(subjects, entry)
	}

	httputil.Success(w, map[string]any{
		"class_id":     classID,
		"school_year":  schoolYear.Label,
		"subjects":     subjects,
		"absences": map[string]any{
			"total":    absenceSummary.TotalAbsences,
			"unexcused": absenceSummary.UnexcusedCount,
			"excused":  absenceSummary.ExcusedCount,
		},
	})
}

// ──────────────────────────────────────────────────────────────────────────────
// Response types
// ──────────────────────────────────────────────────────────────────────────────

type subjectGrades struct {
	SubjectID   uuid.UUID    `json:"subject_id"`
	SubjectName string       `json:"subject_name"`
	ShortName   *string      `json:"short_name,omitempty"`
	Grades      []gradeEntry `json:"grades"`
}

type gradeEntry struct {
	ID             uuid.UUID `json:"id"`
	Semester       string    `json:"semester"`
	NumericGrade   *int16    `json:"numeric_grade,omitempty"`
	QualifierGrade *string   `json:"qualifier_grade,omitempty"`
	IsThesis       bool      `json:"is_thesis"`
	GradeDate      *string   `json:"grade_date,omitempty"`
}

type absenceEntry struct {
	SubjectName  string `json:"subject_name"`
	Semester     string `json:"semester"`
	AbsenceDate  string `json:"absence_date"`
	PeriodNumber int16  `json:"period_number"`
	AbsenceType  string `json:"absence_type"`
}

type averageEntry struct {
	SubjectName    string   `json:"subject_name"`
	Semester       *string  `json:"semester,omitempty"`
	FinalValue     *float64 `json:"final_value,omitempty"`
	QualifierFinal *string  `json:"qualifier_final,omitempty"`
	IsClosed       bool     `json:"is_closed"`
	IsApproved     bool     `json:"is_approved"`
}

type evaluationEntry struct {
	SubjectName string `json:"subject_name"`
	Semester    string `json:"semester"`
	Content     string `json:"content"`
}

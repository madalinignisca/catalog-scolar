// Package catalog implements HTTP handlers for the core catalog functionality
// in the CatalogRO API: grades (note) and absences (absente).
//
// This file covers the grade endpoints:
//
//	GET    /catalog/classes/{classId}/subjects/{subjectId}/grades — list grades
//	POST   /catalog/grades                                       — create a grade
//	PUT    /catalog/grades/{gradeId}                             — update a grade
//	DELETE /catalog/grades/{gradeId}                             — soft-delete a grade
//
// IMPORTANT DOMAIN CONTEXT (Romanian school system):
//   - "nota" (plural "note") = grade
//   - Primary school (classes P-IV) uses qualifiers: FB (Foarte Bine), B (Bine),
//     S (Suficient), I (Insuficient) — NOT numeric grades.
//   - Middle school (V-VIII) and high school (IX-XII) use numeric grades 1-10.
//   - A "teza" (thesis) is a semester exam that counts for 25% of the average.
//   - The evaluation_configs table determines which type of grading each
//     education level uses, so the rules are NOT hardcoded.
//
// Authorization model:
//   - Only teachers assigned to a class+subject can create/update/delete grades.
//   - The assignment is checked via the class_subject_teachers table.
//   - Admins can also manage grades (they bypass the teacher assignment check).
package catalog

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/vlahsh/catalogro/api/db/generated"
	"github.com/vlahsh/catalogro/api/internal/auth"
	"github.com/vlahsh/catalogro/api/internal/httputil"
)

// Handler holds the dependencies needed by all catalog-related HTTP handlers.
// It is created once at application startup and reused for every request.
type Handler struct {
	// queries is the sqlc-generated query interface for type-safe DB access.
	queries *generated.Queries

	// logger is the structured logger for recording errors and debug info.
	logger *slog.Logger
}

// NewHandler creates a new catalog Handler with the given dependencies.
func NewHandler(queries *generated.Queries, logger *slog.Logger) *Handler {
	return &Handler{
		queries: queries,
		logger:  logger,
	}
}

// ──────────────────────────────────────────────────────────────────────────────
// GET /catalog/classes/{classId}/subjects/{subjectId}/grades
// ──────────────────────────────────────────────────────────────────────────────

// gradeResponse is the JSON shape for a single grade in API responses.
// We define this separately from the DB model to control the API contract.
type gradeResponse struct {
	ID             uuid.UUID `json:"id"`
	StudentID      uuid.UUID `json:"student_id"`
	TeacherID      uuid.UUID `json:"teacher_id"`
	Semester       string    `json:"semester"`
	NumericGrade   *int16    `json:"numeric_grade,omitempty"`
	QualifierGrade *string   `json:"qualifier_grade,omitempty"`
	IsThesis       bool      `json:"is_thesis"`
	GradeDate      string    `json:"grade_date"`
	Description    *string   `json:"description,omitempty"`
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at"`
}

// studentGrades groups a student with their list of grades for the response.
// This is the shape returned by the list-grades endpoint:
//
//	{ "students": [{ "student": {...}, "grades": [...] }] }
type studentGrades struct {
	Student studentInfo     `json:"student"`
	Grades  []gradeResponse `json:"grades"`
}

// studentInfo is a minimal student representation used inside grade listings.
type studentInfo struct {
	ID        uuid.UUID `json:"id"`
	FirstName string    `json:"first_name"`
	LastName  string    `json:"last_name"`
}

// ListGrades handles GET /catalog/classes/{classId}/subjects/{subjectId}/grades.
//
// Query parameters:
//   - semester (required): "I" or "II"
//   - school_year_id (optional): UUID of the school year. If omitted, uses the current year.
//
// Returns a list of students in the class, each with their grades for the
// specified subject and semester. Students are sorted alphabetically.
//
// Possible responses:
//   - 200 OK: { "data": { "students": [...] } }
//   - 400 Bad Request: invalid parameters
//   - 401 Unauthorized: auth context missing
//   - 500 Internal Server Error: database failure
func (h *Handler) ListGrades(w http.ResponseWriter, r *http.Request) {
	// Step 1: Verify authentication.
	_, err := auth.GetSchoolID(r.Context())
	if err != nil {
		httputil.Unauthorized(w, "Authentication required")
		return
	}

	// Step 1b: Retrieve the transaction-scoped Queries from context so that
	// all database calls in this handler use the RLS-enabled transaction.
	queries := auth.GetQueries(r.Context())
	if queries == nil {
		httputil.InternalError(w)
		return
	}

	// Step 2: Parse and validate URL path parameters.
	classID, err := uuid.Parse(chi.URLParam(r, "classId"))
	if err != nil {
		httputil.BadRequest(w, "INVALID_ID", "classId must be a valid UUID")
		return
	}

	subjectID, err := uuid.Parse(chi.URLParam(r, "subjectId"))
	if err != nil {
		httputil.BadRequest(w, "INVALID_ID", "subjectId must be a valid UUID")
		return
	}

	// Step 3: Parse and validate query parameters.
	semesterStr := r.URL.Query().Get("semester")
	if semesterStr != "I" && semesterStr != "II" {
		httputil.BadRequest(w, "INVALID_SEMESTER", "semester must be 'I' or 'II'")
		return
	}
	semester := generated.Semester(semesterStr)

	// Step 4: Resolve the school year ID.
	// If the client provides school_year_id as a query param, use that.
	// Otherwise, look up the current school year from the database.
	var schoolYearID uuid.UUID
	if syID := r.URL.Query().Get("school_year_id"); syID != "" {
		schoolYearID, err = uuid.Parse(syID)
		if err != nil {
			httputil.BadRequest(w, "INVALID_ID", "school_year_id must be a valid UUID")
			return
		}
	} else {
		// No school_year_id provided — use the current school year.
		sy, err := queries.GetCurrentSchoolYear(r.Context())
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				httputil.BadRequest(w, "NO_SCHOOL_YEAR", "No current school year is configured")
				return
			}
			h.logger.Error("failed to get current school year", "error", err)
			httputil.InternalError(w)
			return
		}
		schoolYearID = sy.ID
	}

	// Step 5: Fetch grades from the database.
	// The ListGradesByClassSubject query joins grades with users to get student names.
	// Results are sorted by student last name, then first name, then grade date.
	rows, err := queries.ListGradesByClassSubject(r.Context(), generated.ListGradesByClassSubjectParams{
		ClassID:      classID,
		SubjectID:    subjectID,
		Semester:     semester,
		SchoolYearID: schoolYearID,
	})
	if err != nil {
		h.logger.Error("failed to list grades", "error", err,
			"class_id", classID, "subject_id", subjectID)
		httputil.InternalError(w)
		return
	}

	// Step 6: Group grades by student.
	// The SQL results are flat rows (one row per grade). We need to group them
	// into a structure where each student has an array of their grades.
	// We use a map to collect grades per student, and a slice to preserve order.
	// Indexing (rows[i]) avoids copying the large row struct on each iteration.
	studentOrder := []uuid.UUID{}                // Preserves the alphabetical order from SQL.
	studentMap := map[uuid.UUID]*studentGrades{} // Groups grades by student ID.

	for i := range rows {
		// If this is the first grade we've seen for this student, create the entry.
		if _, exists := studentMap[rows[i].StudentID]; !exists {
			studentOrder = append(studentOrder, rows[i].StudentID)
			studentMap[rows[i].StudentID] = &studentGrades{
				Student: studentInfo{
					ID:        rows[i].StudentID,
					FirstName: rows[i].StudentFirstName,
					LastName:  rows[i].StudentLastName,
				},
				Grades: []gradeResponse{},
			}
		}

		// Convert the qualifier grade from the nullable enum to a plain *string.
		var qualGrade *string
		if rows[i].QualifierGrade.Valid {
			s := string(rows[i].QualifierGrade.Qualifier)
			qualGrade = &s
		}

		// Append this grade to the student's list.
		studentMap[rows[i].StudentID].Grades = append(studentMap[rows[i].StudentID].Grades, gradeResponse{
			ID:             rows[i].ID,
			StudentID:      rows[i].StudentID,
			TeacherID:      rows[i].TeacherID,
			Semester:       string(rows[i].Semester),
			NumericGrade:   rows[i].NumericGrade,
			QualifierGrade: qualGrade,
			IsThesis:       rows[i].IsThesis,
			GradeDate:      rows[i].GradeDate.Time.Format("2006-01-02"),
			Description:    rows[i].Description,
			CreatedAt:      rows[i].CreatedAt,
			UpdatedAt:      rows[i].UpdatedAt,
		})
	}

	// Step 7: Build the final ordered list from the map.
	result := make([]studentGrades, 0, len(studentOrder))
	for _, sid := range studentOrder {
		result = append(result, *studentMap[sid])
	}

	// Step 8: Return the response.
	httputil.Success(w, map[string]any{
		"students": result,
	})
}

// ──────────────────────────────────────────────────────────────────────────────
// POST /catalog/grades
// ──────────────────────────────────────────────────────────────────────────────

// createGradeRequest is the expected JSON body for POST /catalog/grades.
// The client sends either numeric_grade (for middle/high school) or
// qualifier_grade (for primary school), but never both.
type createGradeRequest struct {
	StudentID       uuid.UUID  `json:"student_id"`
	ClassID         uuid.UUID  `json:"class_id"`
	SubjectID       uuid.UUID  `json:"subject_id"`
	Semester        string     `json:"semester"`
	NumericGrade    *int16     `json:"numeric_grade"`
	QualifierGrade  *string    `json:"qualifier_grade"`
	IsThesis        bool       `json:"is_thesis"`
	GradeDate       string     `json:"grade_date"`
	Description     *string    `json:"description"`
	ClientID        *uuid.UUID `json:"client_id"`
	ClientTimestamp *time.Time `json:"client_timestamp"`
}

// CreateGrade handles POST /catalog/grades.
//
// Creates a new grade entry in the catalog. The handler performs several
// validation steps before inserting the grade:
//
//  1. The teacher must be assigned to the class+subject (authorization check).
//  2. Either numeric_grade OR qualifier_grade must be provided, but not both.
//  3. Numeric grades must be between 1 and 10.
//  4. Qualifier grades must be one of: FB, B, S, I.
//  5. The semester must be "I" or "II".
//  6. The grade_date must be a valid date string (YYYY-MM-DD).
//
// Possible responses:
//   - 201 Created: { "data": { created grade } }
//   - 400 Bad Request: validation failure
//   - 401 Unauthorized: auth context missing
//   - 403 Forbidden: teacher not assigned to class+subject
//   - 500 Internal Server Error: database failure
func (h *Handler) CreateGrade(w http.ResponseWriter, r *http.Request) {
	// Step 1: Extract the authenticated user's identity.
	userID, err := auth.GetUserID(r.Context())
	if err != nil {
		httputil.Unauthorized(w, "Authentication required")
		return
	}

	role, err := auth.GetUserRole(r.Context())
	if err != nil {
		httputil.Unauthorized(w, "Authentication required")
		return
	}

	// Step 1b: Retrieve the transaction-scoped Queries from context so that
	// all database calls in this handler use the RLS-enabled transaction.
	queries := auth.GetQueries(r.Context())
	if queries == nil {
		httputil.InternalError(w)
		return
	}

	// Step 2: Parse the JSON request body.
	var req createGradeRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httputil.BadRequest(w, "INVALID_JSON", "Request body must be valid JSON")
		return
	}

	// Step 3: Validate required fields.
	if req.StudentID == uuid.Nil {
		httputil.BadRequest(w, "MISSING_FIELD", "student_id is required")
		return
	}
	if req.ClassID == uuid.Nil {
		httputil.BadRequest(w, "MISSING_FIELD", "class_id is required")
		return
	}
	if req.SubjectID == uuid.Nil {
		httputil.BadRequest(w, "MISSING_FIELD", "subject_id is required")
		return
	}

	// Step 4: Validate the semester value.
	if req.Semester != "I" && req.Semester != "II" {
		httputil.BadRequest(w, "INVALID_SEMESTER", "semester must be 'I' or 'II'")
		return
	}

	// Step 5: Validate the grade value.
	// Romanian schools use two grading systems depending on the education level:
	//   - Primary (P-IV): qualifiers FB/B/S/I
	//   - Middle/High (V-XII): numeric grades 1-10
	// The client must provide exactly one of numeric_grade or qualifier_grade.
	if req.NumericGrade == nil && req.QualifierGrade == nil {
		httputil.BadRequest(w, "MISSING_GRADE", "Either numeric_grade or qualifier_grade must be provided")
		return
	}
	if req.NumericGrade != nil && req.QualifierGrade != nil {
		httputil.BadRequest(w, "GRADE_CONFLICT", "Provide either numeric_grade or qualifier_grade, not both")
		return
	}

	// Validate numeric grade range: must be between 1 and 10 (inclusive).
	if req.NumericGrade != nil {
		if *req.NumericGrade < 1 || *req.NumericGrade > 10 {
			httputil.BadRequest(w, "GRADE_INVALID", "numeric_grade must be between 1 and 10")
			return
		}
	}

	// Validate qualifier grade: must be one of the four allowed values.
	var qualifierGrade generated.NullQualifier
	if req.QualifierGrade != nil {
		switch *req.QualifierGrade {
		case "FB", "B", "S", "I":
			qualifierGrade = generated.NullQualifier{
				Qualifier: generated.Qualifier(*req.QualifierGrade),
				Valid:     true,
			}
		default:
			httputil.BadRequest(w, "QUALIFIER_INVALID",
				"qualifier_grade must be one of: FB (Foarte Bine), B (Bine), S (Suficient), I (Insuficient)")
			return
		}
	}

	// Step 6: Parse the grade date.
	gradeDate, err := time.Parse("2006-01-02", req.GradeDate)
	if err != nil {
		httputil.BadRequest(w, "INVALID_DATE", "grade_date must be in YYYY-MM-DD format")
		return
	}

	// Step 7: Authorization check — verify the teacher is assigned to this class+subject.
	// Admins bypass this check because they may need to correct grades on behalf of teachers.
	if role == "teacher" {
		_, err := queries.CheckTeacherClassSubject(r.Context(), generated.CheckTeacherClassSubjectParams{
			TeacherID: userID,
			ClassID:   req.ClassID,
			SubjectID: req.SubjectID,
		})
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				httputil.Forbidden(w, "You are not assigned to teach this subject in this class")
				return
			}
			h.logger.Error("failed to check teacher assignment", "error", err)
			httputil.InternalError(w)
			return
		}
	}

	// Step 8: Get the current school year for the grade record.
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

	// Step 9: Build the optional fields for offline sync support.
	// client_id and client_timestamp are used by the offline sync system.
	// When a grade is created offline on the client device, the client assigns
	// a UUID (client_id) and records the local timestamp. The server stores
	// these so it can detect and resolve conflicts during sync.
	var clientID pgtype.UUID
	if req.ClientID != nil {
		clientID = pgtype.UUID{Bytes: *req.ClientID, Valid: true}
	}

	var clientTimestamp pgtype.Timestamptz
	if req.ClientTimestamp != nil {
		clientTimestamp = pgtype.Timestamptz{Time: *req.ClientTimestamp, Valid: true}
	}

	// Step 10: Insert the grade into the database.
	grade, err := queries.CreateGrade(r.Context(), generated.CreateGradeParams{
		StudentID:       req.StudentID,
		ClassID:         req.ClassID,
		SubjectID:       req.SubjectID,
		TeacherID:       userID,
		SchoolYearID:    schoolYear.ID,
		Semester:        generated.Semester(req.Semester),
		NumericGrade:    req.NumericGrade,
		QualifierGrade:  qualifierGrade,
		IsThesis:        req.IsThesis,
		GradeDate:       pgtype.Date{Time: gradeDate, Valid: true},
		Description:     req.Description,
		ClientID:        clientID,
		ClientTimestamp: clientTimestamp,
		SyncStatus:      generated.SyncStatusSynced,
	})
	if err != nil {
		h.logger.Error("failed to create grade", "error", err,
			"student_id", req.StudentID, "class_id", req.ClassID)
		httputil.InternalError(w)
		return
	}

	// Step 11: Return the created grade.
	httputil.Created(w, mapGradeToResponse(&grade))
}

// ──────────────────────────────────────────────────────────────────────────────
// PUT /catalog/grades/{gradeId}
// ──────────────────────────────────────────────────────────────────────────────

// updateGradeRequest is the expected JSON body for PUT /catalog/grades/{gradeId}.
type updateGradeRequest struct {
	NumericGrade   *int16  `json:"numeric_grade"`
	QualifierGrade *string `json:"qualifier_grade"`
	GradeDate      string  `json:"grade_date"`
	Description    *string `json:"description"`
}

// UpdateGrade handles PUT /catalog/grades/{gradeId}.
//
// Updates an existing grade. The grade must not be soft-deleted.
// Only the teacher who created the grade (or an admin) can update it.
//
// Possible responses:
//   - 200 OK: { "data": { updated grade } }
//   - 400 Bad Request: validation failure
//   - 401 Unauthorized: auth context missing
//   - 403 Forbidden: not the original teacher
//   - 404 Not Found: grade does not exist or is deleted
//   - 500 Internal Server Error: database failure
func (h *Handler) UpdateGrade(w http.ResponseWriter, r *http.Request) {
	// Step 1: Extract the authenticated user's identity.
	userID, err := auth.GetUserID(r.Context())
	if err != nil {
		httputil.Unauthorized(w, "Authentication required")
		return
	}

	role, err := auth.GetUserRole(r.Context())
	if err != nil {
		httputil.Unauthorized(w, "Authentication required")
		return
	}

	// Step 1b: Retrieve the transaction-scoped Queries from context so that
	// all database calls in this handler use the RLS-enabled transaction.
	queries := auth.GetQueries(r.Context())
	if queries == nil {
		httputil.InternalError(w)
		return
	}

	// Step 2: Parse the grade ID from the URL.
	gradeID, err := uuid.Parse(chi.URLParam(r, "gradeId"))
	if err != nil {
		httputil.BadRequest(w, "INVALID_ID", "gradeId must be a valid UUID")
		return
	}

	// Step 3: Parse the JSON request body.
	var req updateGradeRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httputil.BadRequest(w, "INVALID_JSON", "Request body must be valid JSON")
		return
	}

	// Step 4: Fetch the existing grade to verify it exists and check ownership.
	existing, err := queries.GetGradeByID(r.Context(), gradeID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			httputil.NotFound(w, "Grade not found")
			return
		}
		h.logger.Error("failed to get grade", "error", err, "grade_id", gradeID)
		httputil.InternalError(w)
		return
	}

	// Step 5: Authorization — only the original teacher or an admin can update.
	if role == "teacher" && existing.TeacherID != userID {
		httputil.Forbidden(w, "Only the teacher who created this grade can update it")
		return
	}

	// Step 6: Validate the grade value (same rules as create).
	if req.NumericGrade == nil && req.QualifierGrade == nil {
		httputil.BadRequest(w, "MISSING_GRADE", "Either numeric_grade or qualifier_grade must be provided")
		return
	}
	if req.NumericGrade != nil && req.QualifierGrade != nil {
		httputil.BadRequest(w, "GRADE_CONFLICT", "Provide either numeric_grade or qualifier_grade, not both")
		return
	}

	if req.NumericGrade != nil {
		if *req.NumericGrade < 1 || *req.NumericGrade > 10 {
			httputil.BadRequest(w, "GRADE_INVALID", "numeric_grade must be between 1 and 10")
			return
		}
	}

	var qualifierGrade generated.NullQualifier
	if req.QualifierGrade != nil {
		switch *req.QualifierGrade {
		case "FB", "B", "S", "I":
			qualifierGrade = generated.NullQualifier{
				Qualifier: generated.Qualifier(*req.QualifierGrade),
				Valid:     true,
			}
		default:
			httputil.BadRequest(w, "QUALIFIER_INVALID",
				"qualifier_grade must be one of: FB, B, S, I")
			return
		}
	}

	// Step 7: Parse the grade date.
	gradeDate, err := time.Parse("2006-01-02", req.GradeDate)
	if err != nil {
		httputil.BadRequest(w, "INVALID_DATE", "grade_date must be in YYYY-MM-DD format")
		return
	}

	// Step 8: Update the grade in the database.
	updated, err := queries.UpdateGrade(r.Context(), generated.UpdateGradeParams{
		ID:             gradeID,
		NumericGrade:   req.NumericGrade,
		QualifierGrade: qualifierGrade,
		GradeDate:      pgtype.Date{Time: gradeDate, Valid: true},
		Description:    req.Description,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			httputil.NotFound(w, "Grade not found or already deleted")
			return
		}
		h.logger.Error("failed to update grade", "error", err, "grade_id", gradeID)
		httputil.InternalError(w)
		return
	}

	// Step 9: Return the updated grade.
	httputil.Success(w, mapGradeToResponse(&updated))
}

// ──────────────────────────────────────────────────────────────────────────────
// DELETE /catalog/grades/{gradeId}
// ──────────────────────────────────────────────────────────────────────────────

// DeleteGrade handles DELETE /catalog/grades/{gradeId}.
//
// Performs a soft delete by setting deleted_at on the grade record.
// The grade data is preserved for audit purposes — student data is never
// hard-deleted per Romanian education law requirements.
//
// Only the teacher who created the grade (or an admin) can delete it.
//
// Possible responses:
//   - 200 OK: { "data": { "deleted": true } }
//   - 400 Bad Request: invalid gradeId format
//   - 401 Unauthorized: auth context missing
//   - 403 Forbidden: not the original teacher
//   - 404 Not Found: grade does not exist
//   - 500 Internal Server Error: database failure
func (h *Handler) DeleteGrade(w http.ResponseWriter, r *http.Request) {
	// Step 1: Extract the authenticated user's identity.
	userID, err := auth.GetUserID(r.Context())
	if err != nil {
		httputil.Unauthorized(w, "Authentication required")
		return
	}

	role, err := auth.GetUserRole(r.Context())
	if err != nil {
		httputil.Unauthorized(w, "Authentication required")
		return
	}

	// Step 1b: Retrieve the transaction-scoped Queries from context so that
	// all database calls in this handler use the RLS-enabled transaction.
	queries := auth.GetQueries(r.Context())
	if queries == nil {
		httputil.InternalError(w)
		return
	}

	// Step 2: Parse the grade ID from the URL.
	gradeID, err := uuid.Parse(chi.URLParam(r, "gradeId"))
	if err != nil {
		httputil.BadRequest(w, "INVALID_ID", "gradeId must be a valid UUID")
		return
	}

	// Step 3: Fetch the existing grade to verify it exists and check ownership.
	existing, err := queries.GetGradeByID(r.Context(), gradeID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			httputil.NotFound(w, "Grade not found")
			return
		}
		h.logger.Error("failed to get grade for delete", "error", err, "grade_id", gradeID)
		httputil.InternalError(w)
		return
	}

	// Step 4: Authorization — only the original teacher or an admin can delete.
	if role == "teacher" && existing.TeacherID != userID {
		httputil.Forbidden(w, "Only the teacher who created this grade can delete it")
		return
	}

	// Step 5: Soft delete the grade (sets deleted_at = now()).
	if err := queries.SoftDeleteGrade(r.Context(), gradeID); err != nil {
		h.logger.Error("failed to soft delete grade", "error", err, "grade_id", gradeID)
		httputil.InternalError(w)
		return
	}

	// Step 6: Return a confirmation response.
	httputil.Success(w, map[string]any{
		"deleted":  true,
		"grade_id": gradeID,
	})
}

// ──────────────────────────────────────────────────────────────────────────────
// Helpers
// ──────────────────────────────────────────────────────────────────────────────

// mapGradeToResponse converts a database Grade model to the API response struct.
// This is used by both CreateGrade and UpdateGrade to format the response.
//
// It handles the conversion of:
//   - Nullable qualifier grade enum to a plain *string
//   - pgtype.Date to a YYYY-MM-DD string
//   - Semester enum to a plain string
func mapGradeToResponse(g *generated.Grade) gradeResponse {
	// Convert the qualifier grade from the nullable DB enum to a plain *string.
	// The qualifier is nil for numeric grades (middle/high school).
	var qualGrade *string
	if g.QualifierGrade.Valid {
		s := string(g.QualifierGrade.Qualifier)
		qualGrade = &s
	}

	// Format the grade date as YYYY-MM-DD string for the JSON response.
	gradeDateStr := ""
	if g.GradeDate.Valid {
		gradeDateStr = g.GradeDate.Time.Format("2006-01-02")
	}

	return gradeResponse{
		ID:             g.ID,
		StudentID:      g.StudentID,
		TeacherID:      g.TeacherID,
		Semester:       string(g.Semester),
		NumericGrade:   g.NumericGrade,
		QualifierGrade: qualGrade,
		IsThesis:       g.IsThesis,
		GradeDate:      gradeDateStr,
		Description:    g.Description,
		CreatedAt:      g.CreatedAt,
		UpdatedAt:      g.UpdatedAt,
	}
}

// formatDate converts a pgtype.Date to a YYYY-MM-DD string.
// Returns an empty string if the date is null.
func formatDate(d pgtype.Date) string {
	if !d.Valid {
		return ""
	}
	return d.Time.Format("2006-01-02")
}

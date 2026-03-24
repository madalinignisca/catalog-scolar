// This file implements the absence (absenta) HTTP handlers for the CatalogRO API.
//
// Endpoints covered:
//
//	GET  /catalog/classes/{classId}/absences       — list absences for a class
//	POST /catalog/absences                         — record a new absence
//	PUT  /catalog/absences/{absenceId}/excuse      — excuse (motivate) an absence
//
// IMPORTANT DOMAIN CONTEXT (Romanian school system):
//   - "absenta" (plural "absente") = absence
//   - Absences are recorded per student, per class period (ora), per day.
//   - A "period_number" refers to the class period (1st hour, 2nd hour, etc.).
//   - Absences start as "unexcused" (nemotivata) and can later be excused by
//     the homeroom teacher (diriginte) with a medical certificate or other reason.
//   - Absence types: unexcused, medical, excused, school_event.
//   - Too many unexcused absences can lead to a student being declared "corigent"
//     (must retake exams) or "repetent" (must repeat the year).
//
// Authorization model:
//   - Teachers can record absences for classes+subjects they are assigned to.
//   - Homeroom teachers (diriginti) and admins can excuse absences.
//   - The assignment check uses the class_subject_teachers table.
package catalog

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/vlahsh/catalogro/api/db/generated"
	"github.com/vlahsh/catalogro/api/internal/auth"
	"github.com/vlahsh/catalogro/api/internal/httputil"
)

// ──────────────────────────────────────────────────────────────────────────────
// GET /catalog/classes/{classId}/absences
// ──────────────────────────────────────────────────────────────────────────────

// absenceResponse is the JSON shape for a single absence in API responses.
type absenceResponse struct {
	ID             uuid.UUID `json:"id"`
	StudentID      uuid.UUID `json:"student_id"`
	StudentName    string    `json:"student_name"`
	SubjectID      uuid.UUID `json:"subject_id"`
	TeacherID      uuid.UUID `json:"teacher_id"`
	Semester       string    `json:"semester"`
	AbsenceDate    string    `json:"absence_date"`
	PeriodNumber   int16     `json:"period_number"`
	AbsenceType    string    `json:"absence_type"`
	ExcuseReason   *string   `json:"excuse_reason,omitempty"`
	ExcuseDocument *string   `json:"excuse_document,omitempty"`
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at"`
}

// ListAbsences handles GET /catalog/classes/{classId}/absences.
//
// Supports two query modes:
//  1. By specific date: ?date=2026-10-15
//  2. By semester and month: ?semester=I&month=10&school_year_id=xxx
//
// Returns a flat list of absences for the class, including student names.
//
// Possible responses:
//   - 200 OK: { "data": [ ...absences ] }
//   - 400 Bad Request: invalid parameters or missing required query params
//   - 401 Unauthorized: auth context missing
//   - 500 Internal Server Error: database failure
func (h *Handler) ListAbsences(w http.ResponseWriter, r *http.Request) {
	// Step 1: Verify authentication.
	_, err := auth.GetSchoolID(r.Context())
	if err != nil {
		httputil.Unauthorized(w, "Authentication required")
		return
	}

	// Step 2: Parse the class ID from the URL path.
	classID, err := uuid.Parse(chi.URLParam(r, "classId"))
	if err != nil {
		httputil.BadRequest(w, "INVALID_ID", "classId must be a valid UUID")
		return
	}

	// Step 3: Determine which query mode to use based on query parameters.
	// The client can filter by either a specific date OR by semester+month.
	dateStr := r.URL.Query().Get("date")
	semesterStr := r.URL.Query().Get("semester")
	monthStr := r.URL.Query().Get("month")

	// Mode 1: Filter by specific date (e.g. ?date=2026-10-15).
	if dateStr != "" {
		h.listAbsencesByDate(w, r, classID, dateStr)
		return
	}

	// Mode 2: Filter by semester and month (e.g. ?semester=I&month=10).
	if semesterStr != "" && monthStr != "" {
		h.listAbsencesBySemesterMonth(w, r, classID, semesterStr, monthStr)
		return
	}

	// If neither mode is specified, return an error explaining the expected params.
	httputil.BadRequest(w, "MISSING_FILTER",
		"Provide either ?date=YYYY-MM-DD or ?semester=I|II&month=1-12&school_year_id=uuid")
}

// listAbsencesByDate handles the ?date=YYYY-MM-DD query mode for ListAbsences.
// It returns all absences for the class on the specified date.
func (h *Handler) listAbsencesByDate(w http.ResponseWriter, r *http.Request, classID uuid.UUID, dateStr string) {
	// Retrieve the transaction-scoped Queries from context so that all database
	// calls in this helper use the RLS-enabled transaction.
	queries := auth.GetQueries(r.Context())
	if queries == nil {
		httputil.InternalError(w)
		return
	}

	// Parse the date string into a time.Time value.
	absDate, err := time.Parse("2006-01-02", dateStr)
	if err != nil {
		httputil.BadRequest(w, "INVALID_DATE", "date must be in YYYY-MM-DD format")
		return
	}

	// Query the database for absences on this specific date.
	rows, err := queries.ListAbsencesByClassDate(r.Context(), generated.ListAbsencesByClassDateParams{
		ClassID:     classID,
		AbsenceDate: pgtype.Date{Time: absDate, Valid: true},
	})
	if err != nil {
		h.logger.Error("failed to list absences by date", "error", err,
			"class_id", classID, "date", dateStr)
		httputil.InternalError(w)
		return
	}

	// Map the database rows to the API response format.
	// We use indexing (rows[i]) instead of a range copy to avoid copying the
	// large ListAbsencesByClassDateRow struct (408 bytes) on each iteration.
	items := make([]absenceResponse, len(rows))
	for i := range rows {
		items[i] = absenceResponse{
			ID:             rows[i].ID,
			StudentID:      rows[i].StudentID,
			StudentName:    rows[i].StudentLastName + " " + rows[i].StudentFirstName,
			SubjectID:      rows[i].SubjectID,
			TeacherID:      rows[i].TeacherID,
			Semester:       string(rows[i].Semester),
			AbsenceDate:    formatDate(rows[i].AbsenceDate),
			PeriodNumber:   rows[i].PeriodNumber,
			AbsenceType:    string(rows[i].AbsenceType),
			ExcuseReason:   rows[i].ExcuseReason,
			ExcuseDocument: rows[i].ExcuseDocument,
			CreatedAt:      rows[i].CreatedAt,
			UpdatedAt:      rows[i].UpdatedAt,
		}
	}

	httputil.List(w, items, nil)
}

// listAbsencesBySemesterMonth handles the ?semester=I&month=10 query mode.
// It returns all absences for the class in the specified semester and calendar month.
func (h *Handler) listAbsencesBySemesterMonth(w http.ResponseWriter, r *http.Request, classID uuid.UUID, semesterStr, monthStr string) {
	// Retrieve the transaction-scoped Queries from context so that all database
	// calls in this helper use the RLS-enabled transaction.
	queries := auth.GetQueries(r.Context())
	if queries == nil {
		httputil.InternalError(w)
		return
	}

	// Validate the semester value.
	if semesterStr != "I" && semesterStr != "II" {
		httputil.BadRequest(w, "INVALID_SEMESTER", "semester must be 'I' or 'II'")
		return
	}

	// Parse the month as an integer (1-12).
	month, err := strconv.Atoi(monthStr)
	if err != nil || month < 1 || month > 12 {
		httputil.BadRequest(w, "INVALID_MONTH", "month must be an integer between 1 and 12")
		return
	}

	// Resolve the school year ID. If not provided, use the current school year.
	var schoolYearID uuid.UUID
	if syID := r.URL.Query().Get("school_year_id"); syID != "" {
		schoolYearID, err = uuid.Parse(syID)
		if err != nil {
			httputil.BadRequest(w, "INVALID_ID", "school_year_id must be a valid UUID")
			return
		}
	} else {
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

	// Query the database for absences matching the semester and month.
	rows, err := queries.ListAbsencesByClassSemesterMonth(r.Context(), generated.ListAbsencesByClassSemesterMonthParams{
		ClassID:      classID,
		Semester:     generated.Semester(semesterStr),
		Column3:      int32(month), //nolint:gosec // month is validated 1-12, no overflow risk
		SchoolYearID: schoolYearID,
	})
	if err != nil {
		h.logger.Error("failed to list absences by semester/month", "error", err,
			"class_id", classID, "semester", semesterStr, "month", month)
		httputil.InternalError(w)
		return
	}

	// Map the database rows to the API response format.
	// Use indexing to avoid copying the large row struct on each iteration.
	items := make([]absenceResponse, len(rows))
	for i := range rows {
		items[i] = absenceResponse{
			ID:             rows[i].ID,
			StudentID:      rows[i].StudentID,
			StudentName:    rows[i].StudentLastName + " " + rows[i].StudentFirstName,
			SubjectID:      rows[i].SubjectID,
			TeacherID:      rows[i].TeacherID,
			Semester:       string(rows[i].Semester),
			AbsenceDate:    formatDate(rows[i].AbsenceDate),
			PeriodNumber:   rows[i].PeriodNumber,
			AbsenceType:    string(rows[i].AbsenceType),
			ExcuseReason:   rows[i].ExcuseReason,
			ExcuseDocument: rows[i].ExcuseDocument,
			CreatedAt:      rows[i].CreatedAt,
			UpdatedAt:      rows[i].UpdatedAt,
		}
	}

	httputil.List(w, items, nil)
}

// ──────────────────────────────────────────────────────────────────────────────
// POST /catalog/absences
// ──────────────────────────────────────────────────────────────────────────────

// createAbsenceRequest is the expected JSON body for POST /catalog/absences.
type createAbsenceRequest struct {
	StudentID       uuid.UUID  `json:"student_id"`
	ClassID         uuid.UUID  `json:"class_id"`
	SubjectID       uuid.UUID  `json:"subject_id"`
	AbsenceDate     string     `json:"absence_date"`
	PeriodNumber    int16      `json:"period_number"`
	ClientID        *uuid.UUID `json:"client_id"`
	ClientTimestamp *time.Time `json:"client_timestamp"`
}

// CreateAbsence handles POST /catalog/absences.
//
// Records a new absence for a student. New absences are always created with
// type "unexcused" — they can be excused later via the PUT /excuse endpoint.
//
// Validation rules:
//  1. The teacher must be assigned to the class+subject.
//  2. absence_date must be a valid date.
//  3. period_number must be between 1 and 14 (Romanian schools have up to 7-8
//     periods per day, but we allow up to 14 for edge cases like exam days).
//
// Possible responses:
//   - 201 Created: { "data": { created absence } }
//   - 400 Bad Request: validation failure
//   - 401 Unauthorized: auth context missing
//   - 403 Forbidden: teacher not assigned to class+subject
//   - 500 Internal Server Error: database failure
func (h *Handler) CreateAbsence(w http.ResponseWriter, r *http.Request) {
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
	var req createAbsenceRequest
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

	// Step 4: Parse and validate the absence date.
	absDate, err := time.Parse("2006-01-02", req.AbsenceDate)
	if err != nil {
		httputil.BadRequest(w, "INVALID_DATE", "absence_date must be in YYYY-MM-DD format")
		return
	}

	// Step 5: Validate the period number (class period / ora).
	// Romanian schools typically have 6-8 class periods per day. We allow up to 14
	// to cover exceptional cases like extended exam schedules.
	if req.PeriodNumber < 1 || req.PeriodNumber > 14 {
		httputil.BadRequest(w, "INVALID_PERIOD", "period_number must be between 1 and 14")
		return
	}

	// Step 6: Authorization check — verify teacher assignment.
	// Admins bypass this check.
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
			h.logger.Error("failed to check teacher assignment for absence", "error", err)
			httputil.InternalError(w)
			return
		}
	}

	// Step 7: Get the current school year to determine the semester.
	schoolYear, err := queries.GetCurrentSchoolYear(r.Context())
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			httputil.BadRequest(w, "NO_SCHOOL_YEAR", "No current school year is configured")
			return
		}
		h.logger.Error("failed to get current school year for absence", "error", err)
		httputil.InternalError(w)
		return
	}

	// Step 8: Determine which semester the absence falls in based on the date.
	// The school_years table has sem1_start, sem1_end, sem2_start, sem2_end dates.
	// If the absence date falls within semester 1, it is semester I; otherwise semester II.
	semester := determineSemester(absDate, &schoolYear)

	// Step 9: Build optional offline sync fields.
	var clientID pgtype.UUID
	if req.ClientID != nil {
		clientID = pgtype.UUID{Bytes: *req.ClientID, Valid: true}
	}

	var clientTimestamp pgtype.Timestamptz
	if req.ClientTimestamp != nil {
		clientTimestamp = pgtype.Timestamptz{Time: *req.ClientTimestamp, Valid: true}
	}

	// Step 10: Insert the absence into the database.
	// New absences always start as "unexcused" — they are excused later by
	// the homeroom teacher (diriginte) via the PUT /excuse endpoint.
	absence, err := queries.CreateAbsence(r.Context(), generated.CreateAbsenceParams{
		StudentID:       req.StudentID,
		ClassID:         req.ClassID,
		SubjectID:       req.SubjectID,
		TeacherID:       userID,
		SchoolYearID:    schoolYear.ID,
		Semester:        semester,
		AbsenceDate:     pgtype.Date{Time: absDate, Valid: true},
		PeriodNumber:    req.PeriodNumber,
		AbsenceType:     generated.AbsenceTypeUnexcused,
		ClientID:        clientID,
		ClientTimestamp: clientTimestamp,
		SyncStatus:      generated.SyncStatusSynced,
	})
	if err != nil {
		// Check for duplicate absence (same student, date, period).
		// The database has a UNIQUE constraint on (student_id, absence_date, period_number).
		h.logger.Error("failed to create absence", "error", err,
			"student_id", req.StudentID, "date", req.AbsenceDate, "period", req.PeriodNumber)
		httputil.InternalError(w)
		return
	}

	// Step 11: Return the created absence.
	httputil.Created(w, mapAbsenceToResponse(&absence))
}

// ──────────────────────────────────────────────────────────────────────────────
// PUT /catalog/absences/{absenceId}/excuse
// ──────────────────────────────────────────────────────────────────────────────

// excuseAbsenceRequest is the expected JSON body for PUT /catalog/absences/{absenceId}/excuse.
type excuseAbsenceRequest struct {
	// AbsenceType is the new type after excusing. Valid values:
	//   - "medical" — excused with a medical certificate (adeverinta medicala)
	//   - "excused" — excused for other valid reasons (e.g. family emergency)
	//   - "school_event" — absent due to a school-organized event
	AbsenceType  string  `json:"absence_type"`
	ExcuseReason *string `json:"excuse_reason"`
}

// ExcuseAbsence handles PUT /catalog/absences/{absenceId}/excuse.
//
// Excuses (motivates) an existing absence. In the Romanian school system,
// the homeroom teacher (diriginte) is responsible for excusing absences
// based on medical certificates or other documentation.
//
// The absence_type changes from "unexcused" to one of: medical, excused, school_event.
//
// Possible responses:
//   - 200 OK: { "data": { updated absence } }
//   - 400 Bad Request: invalid absence type
//   - 401 Unauthorized: auth context missing
//   - 404 Not Found: absence does not exist
//   - 500 Internal Server Error: database failure
func (h *Handler) ExcuseAbsence(w http.ResponseWriter, r *http.Request) {
	// Step 1: Extract the authenticated user's identity.
	userID, err := auth.GetUserID(r.Context())
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

	// Step 2: Parse the absence ID from the URL.
	absenceID, err := uuid.Parse(chi.URLParam(r, "absenceId"))
	if err != nil {
		httputil.BadRequest(w, "INVALID_ID", "absenceId must be a valid UUID")
		return
	}

	// Step 3: Parse the JSON request body.
	var req excuseAbsenceRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httputil.BadRequest(w, "INVALID_JSON", "Request body must be valid JSON")
		return
	}

	// Step 4: Validate the new absence type.
	// When excusing, the type must be one of the "excused" variants.
	// Setting it back to "unexcused" is not done via this endpoint.
	validTypes := map[string]generated.AbsenceType{
		"medical":      generated.AbsenceTypeMedical,
		"excused":      generated.AbsenceTypeExcused,
		"school_event": generated.AbsenceTypeSchoolEvent,
	}
	absType, ok := validTypes[req.AbsenceType]
	if !ok {
		httputil.BadRequest(w, "INVALID_ABSENCE_TYPE",
			"absence_type must be one of: medical, excused, school_event")
		return
	}

	// Step 5: Verify the absence exists.
	_, err = queries.GetAbsenceByID(r.Context(), absenceID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			httputil.NotFound(w, "Absence not found")
			return
		}
		h.logger.Error("failed to get absence for excuse", "error", err, "absence_id", absenceID)
		httputil.InternalError(w)
		return
	}

	// Step 6: Update the absence with the excuse information.
	// The excused_by field records who excused it, and excused_at records when.
	updated, err := queries.ExcuseAbsence(r.Context(), generated.ExcuseAbsenceParams{
		ID:           absenceID,
		AbsenceType:  absType,
		ExcusedBy:    pgtype.UUID{Bytes: userID, Valid: true},
		ExcuseReason: req.ExcuseReason,
		// ExcuseDocument is not set here — it could be added via a file upload endpoint.
		ExcuseDocument: nil,
	})
	if err != nil {
		h.logger.Error("failed to excuse absence", "error", err, "absence_id", absenceID)
		httputil.InternalError(w)
		return
	}

	// Step 7: Return the updated absence.
	httputil.Success(w, mapAbsenceToResponse(&updated))
}

// ──────────────────────────────────────────────────────────────────────────────
// Helpers
// ──────────────────────────────────────────────────────────────────────────────

// mapAbsenceToResponse converts a database Absence model to the API response struct.
// Accepts a pointer to avoid copying the large Absence struct (376 bytes).
func mapAbsenceToResponse(a *generated.Absence) absenceResponse {
	return absenceResponse{
		ID:             a.ID,
		StudentID:      a.StudentID,
		StudentName:    "", // Not available from the Absence model alone; set by list handlers.
		SubjectID:      a.SubjectID,
		TeacherID:      a.TeacherID,
		Semester:       string(a.Semester),
		AbsenceDate:    formatDate(a.AbsenceDate),
		PeriodNumber:   a.PeriodNumber,
		AbsenceType:    string(a.AbsenceType),
		ExcuseReason:   a.ExcuseReason,
		ExcuseDocument: a.ExcuseDocument,
		CreatedAt:      a.CreatedAt,
		UpdatedAt:      a.UpdatedAt,
	}
}

// determineSemester figures out which semester a given date falls into based
// on the school year's semester date ranges.
//
// The school_years table stores:
//   - sem1_start / sem1_end: dates for semester I
//   - sem2_start / sem2_end: dates for semester II
//
// If the date falls within the semester 1 range, we return "I".
// Otherwise we default to "II". This simple logic works for the Romanian
// school calendar where semesters do not overlap.
func determineSemester(date time.Time, sy *generated.SchoolYear) generated.Semester {
	// Extract the semester boundary dates from the school year record.
	// The pgtype.Date stores the date as a time.Time value.
	sem1End := sy.Sem1End.Time

	// If the absence date is on or before the last day of semester 1,
	// it belongs to semester I. Otherwise, it belongs to semester II.
	if !date.After(sem1End) {
		return generated.SemesterI
	}
	return generated.SemesterII
}

// This file implements the descriptive evaluation (evaluare descriptivă) HTTP
// handlers for the CatalogRO API.
//
// Endpoints covered:
//
//	GET    /catalog/classes/{classId}/subjects/{subjectId}/evaluations — list evaluations
//	POST   /catalog/evaluations                                       — create an evaluation
//	PUT    /catalog/evaluations/{evalId}                              — update an evaluation
//	DELETE /catalog/evaluations/{evalId}                              — delete an evaluation
//
// IMPORTANT DOMAIN CONTEXT (Romanian school system):
//   - "evaluare descriptivă" (plural "evaluări descriptive") = descriptive evaluation
//   - Primary school (classes P-IV) uses descriptive evaluations instead of, or in
//     addition to, qualifier grades (FB/B/S/I). A descriptive evaluation is a free-text
//     narrative written by the teacher for each student, per subject, per semester.
//   - Unlike numeric grades (multiple entries per student per subject per semester),
//     a descriptive evaluation is ONE text block per student per subject per semester.
//   - Middle school and high school do NOT use descriptive evaluations.
//
// Authorization model:
//   - Only teachers assigned to a class+subject can create/update/delete evaluations.
//   - Admins can also manage evaluations (they bypass the teacher assignment check).
//   - The assignment is checked via the class_subject_teachers table.
package catalog

import (
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/vlahsh/catalogro/api/db/generated"
	"github.com/vlahsh/catalogro/api/internal/auth"
	"github.com/vlahsh/catalogro/api/internal/httputil"
)

// ──────────────────────────────────────────────────────────────────────────────
// GET /catalog/classes/{classId}/subjects/{subjectId}/evaluations
// ──────────────────────────────────────────────────────────────────────────────

// evaluationResponse is the JSON shape for a single descriptive evaluation in API responses.
// We define this separately from the DB model to control the API contract and
// ensure snake_case field names for the frontend's snakeToCamel converter.
type evaluationResponse struct {
	ID          uuid.UUID `json:"id"`
	StudentID   uuid.UUID `json:"student_id"`
	TeacherID   uuid.UUID `json:"teacher_id"`
	Semester    string    `json:"semester"`
	Content     string    `json:"content"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// studentEvaluation groups a student with their descriptive evaluation for the response.
// This is the shape returned by the list endpoint:
//
//	{ "students": [{ "student": {...}, "evaluation": {...} or null }] }
//
// A student may not have an evaluation yet (evaluation is null in that case).
type studentEvaluation struct {
	Student    studentInfo          `json:"student"`
	Evaluation *evaluationResponse  `json:"evaluation"`
}

// ListEvaluations handles GET /catalog/classes/{classId}/subjects/{subjectId}/evaluations.
//
// Query parameters:
//   - semester (required): "I" or "II"
//   - school_year_id (optional): UUID of the school year. If omitted, uses the current year.
//
// Returns a list of students in the class, each with their descriptive evaluation
// for the specified subject and semester. Students who don't have an evaluation yet
// are still included with evaluation: null. Students are sorted alphabetically.
//
// Possible responses:
//   - 200 OK: { "data": { "students": [...] } }
//   - 400 Bad Request: invalid parameters
//   - 401 Unauthorized: auth context missing
//   - 500 Internal Server Error: database failure
func (h *Handler) ListEvaluations(w http.ResponseWriter, r *http.Request) {
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

	// Step 5: Fetch ALL enrolled students for this class.
	// We need the full student list so the response includes students who don't
	// have an evaluation yet (evaluation: null). This is critical for the UI —
	// teachers need to see which students still need an evaluation written.
	students, err := queries.ListStudentsByClass(r.Context(), classID)
	if err != nil {
		h.logger.Error("failed to list students by class", "error", err, "class_id", classID)
		httputil.InternalError(w)
		return
	}

	// Step 6: Fetch existing descriptive evaluations for this class/subject/semester.
	evals, err := queries.ListDescriptiveEvaluations(r.Context(), generated.ListDescriptiveEvaluationsParams{
		ClassID:      classID,
		SubjectID:    subjectID,
		Semester:     semester,
		SchoolYearID: schoolYearID,
	})
	if err != nil {
		h.logger.Error("failed to list descriptive evaluations", "error", err,
			"class_id", classID, "subject_id", subjectID)
		httputil.InternalError(w)
		return
	}

	// Step 7: Build a lookup map of evaluations by student ID.
	// This allows O(1) lookup when merging with the student list.
	evalMap := make(map[uuid.UUID]*evaluationResponse, len(evals))
	for i := range evals {
		evalMap[evals[i].StudentID] = &evaluationResponse{
			ID:        evals[i].ID,
			StudentID: evals[i].StudentID,
			TeacherID: evals[i].TeacherID,
			Semester:  string(evals[i].Semester),
			Content:   evals[i].Content,
			CreatedAt: evals[i].CreatedAt,
			UpdatedAt: evals[i].UpdatedAt,
		}
	}

	// Step 8: Merge students with their evaluations.
	// Every enrolled student appears in the result. Students without an
	// evaluation have evaluation: null — the teacher sees who still needs one.
	result := make([]studentEvaluation, 0, len(students))
	for i := range students {
		result = append(result, studentEvaluation{
			Student: studentInfo{
				ID:        students[i].ID,
				FirstName: students[i].FirstName,
				LastName:  students[i].LastName,
			},
			Evaluation: evalMap[students[i].ID], // nil if no evaluation exists
		})
	}

	// Step 9: Return the response.
	httputil.Success(w, map[string]any{
		"students": result,
	})
}

// ──────────────────────────────────────────────────────────────────────────────
// POST /catalog/evaluations
// ──────────────────────────────────────────────────────────────────────────────

// createEvaluationRequest is the expected JSON body for POST /catalog/evaluations.
// The teacher provides a free-text evaluation (content) for one student in one subject.
type createEvaluationRequest struct {
	StudentID uuid.UUID `json:"student_id"`
	ClassID   uuid.UUID `json:"class_id"`
	SubjectID uuid.UUID `json:"subject_id"`
	Semester  string    `json:"semester"`
	Content   string    `json:"content"`
}

// CreateEvaluation handles POST /catalog/evaluations.
//
// Creates a new descriptive evaluation for a primary school student.
// The handler validates the request and checks that the teacher is assigned
// to the class+subject before creating the evaluation.
//
// Possible responses:
//   - 201 Created: { "data": { created evaluation } }
//   - 400 Bad Request: validation failure
//   - 401 Unauthorized: auth context missing
//   - 403 Forbidden: teacher not assigned to class+subject
//   - 500 Internal Server Error: database failure
func (h *Handler) CreateEvaluation(w http.ResponseWriter, r *http.Request) {
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
	var req createEvaluationRequest
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

	// Step 5: Validate the content — it must not be empty.
	// Descriptive evaluations are free-text but cannot be blank.
	if req.Content == "" {
		httputil.BadRequest(w, "MISSING_FIELD", "content is required and cannot be empty")
		return
	}

	// Step 6: Authorization — only admins and teachers can create evaluations.
	// Parents, students, and secretaries must not be able to write evaluations.
	if role != "admin" && role != "teacher" {
		httputil.Forbidden(w, "Only teachers and admins can create descriptive evaluations")
		return
	}

	// Step 6b: If the user is a teacher (not admin), verify they are assigned
	// to the class+subject. Admins bypass this check because they may need to
	// write evaluations on behalf of teachers.
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

	// Step 7: Get the current school year for the evaluation record.
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

	// Step 8: Insert the descriptive evaluation into the database.
	eval, err := queries.CreateDescriptiveEvaluation(r.Context(), generated.CreateDescriptiveEvaluationParams{
		StudentID:    req.StudentID,
		ClassID:      req.ClassID,
		SubjectID:    req.SubjectID,
		TeacherID:    userID,
		SchoolYearID: schoolYear.ID,
		Semester:     generated.Semester(req.Semester),
		Content:      req.Content,
	})
	if err != nil {
		h.logger.Error("failed to create descriptive evaluation", "error", err,
			"student_id", req.StudentID, "class_id", req.ClassID)
		httputil.InternalError(w)
		return
	}

	// Step 9: Return the created evaluation.
	httputil.Created(w, mapEvaluationToResponse(&eval))
}

// ──────────────────────────────────────────────────────────────────────────────
// PUT /catalog/evaluations/{evalId}
// ──────────────────────────────────────────────────────────────────────────────

// updateEvaluationRequest is the expected JSON body for PUT /catalog/evaluations/{evalId}.
type updateEvaluationRequest struct {
	Content string `json:"content"`
}

// UpdateEvaluation handles PUT /catalog/evaluations/{evalId}.
//
// Updates the content of an existing descriptive evaluation.
// Only the teacher who created the evaluation (or an admin) can update it.
//
// Possible responses:
//   - 200 OK: { "data": { updated evaluation } }
//   - 400 Bad Request: validation failure
//   - 401 Unauthorized: auth context missing
//   - 403 Forbidden: not the original teacher
//   - 404 Not Found: evaluation does not exist
//   - 500 Internal Server Error: database failure
func (h *Handler) UpdateEvaluation(w http.ResponseWriter, r *http.Request) {
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

	// Step 2: Parse the evaluation ID from the URL.
	evalID, err := uuid.Parse(chi.URLParam(r, "evalId"))
	if err != nil {
		httputil.BadRequest(w, "INVALID_ID", "evalId must be a valid UUID")
		return
	}

	// Step 3: Parse the JSON request body.
	var req updateEvaluationRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httputil.BadRequest(w, "INVALID_JSON", "Request body must be valid JSON")
		return
	}

	// Step 4: Validate the content — it must not be empty.
	if req.Content == "" {
		httputil.BadRequest(w, "MISSING_FIELD", "content is required and cannot be empty")
		return
	}

	// Step 5: Authorization — only admins and teachers can update evaluations.
	if role != "admin" && role != "teacher" {
		httputil.Forbidden(w, "Only teachers and admins can update descriptive evaluations")
		return
	}

	// Step 5b: Fetch the existing evaluation to verify it exists and check ownership.
	existing, err := queries.GetDescriptiveEvaluation(r.Context(), evalID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			httputil.NotFound(w, "Descriptive evaluation not found")
			return
		}
		h.logger.Error("failed to get evaluation", "error", err, "eval_id", evalID)
		httputil.InternalError(w)
		return
	}

	// Step 5c: If the user is a teacher (not admin), verify they own this evaluation.
	if role == "teacher" && existing.TeacherID != userID {
		httputil.Forbidden(w, "Only the teacher who created this evaluation can update it")
		return
	}

	// Step 7: Update the evaluation in the database.
	updated, err := queries.UpdateDescriptiveEvaluation(r.Context(), generated.UpdateDescriptiveEvaluationParams{
		ID:      evalID,
		Content: req.Content,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			httputil.NotFound(w, "Descriptive evaluation not found")
			return
		}
		h.logger.Error("failed to update evaluation", "error", err, "eval_id", evalID)
		httputil.InternalError(w)
		return
	}

	// Step 8: Return the updated evaluation.
	httputil.Success(w, mapEvaluationToResponse(&updated))
}

// ──────────────────────────────────────────────────────────────────────────────
// DELETE /catalog/evaluations/{evalId}
// ──────────────────────────────────────────────────────────────────────────────

// DeleteEvaluation handles DELETE /catalog/evaluations/{evalId}.
//
// Deletes a descriptive evaluation. Unlike grades (which are soft-deleted for
// audit purposes), descriptive evaluations are hard-deleted because they are
// free-text and can be rewritten at any time — there is no regulatory requirement
// to preserve old versions of descriptive evaluation text.
//
// Only the teacher who created the evaluation (or an admin) can delete it.
//
// Possible responses:
//   - 200 OK: { "data": { "deleted": true } }
//   - 400 Bad Request: invalid evalId format
//   - 401 Unauthorized: auth context missing
//   - 403 Forbidden: not the original teacher
//   - 404 Not Found: evaluation does not exist
//   - 500 Internal Server Error: database failure
func (h *Handler) DeleteEvaluation(w http.ResponseWriter, r *http.Request) {
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

	// Step 2: Parse the evaluation ID from the URL.
	evalID, err := uuid.Parse(chi.URLParam(r, "evalId"))
	if err != nil {
		httputil.BadRequest(w, "INVALID_ID", "evalId must be a valid UUID")
		return
	}

	// Step 3: Authorization — only admins and teachers can delete evaluations.
	if role != "admin" && role != "teacher" {
		httputil.Forbidden(w, "Only teachers and admins can delete descriptive evaluations")
		return
	}

	// Step 3b: Fetch the existing evaluation to verify it exists and check ownership.
	existing, err := queries.GetDescriptiveEvaluation(r.Context(), evalID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			httputil.NotFound(w, "Descriptive evaluation not found")
			return
		}
		h.logger.Error("failed to get evaluation for delete", "error", err, "eval_id", evalID)
		httputil.InternalError(w)
		return
	}

	// Step 3c: If the user is a teacher (not admin), verify they own this evaluation.
	if role == "teacher" && existing.TeacherID != userID {
		httputil.Forbidden(w, "Only the teacher who created this evaluation can delete it")
		return
	}

	// Step 5: Delete the evaluation.
	if err := queries.DeleteDescriptiveEvaluation(r.Context(), evalID); err != nil {
		h.logger.Error("failed to delete evaluation", "error", err, "eval_id", evalID)
		httputil.InternalError(w)
		return
	}

	// Step 6: Return a confirmation response.
	httputil.Success(w, map[string]any{
		"deleted": true,
		"eval_id": evalID,
	})
}

// ──────────────────────────────────────────────────────────────────────────────
// Helpers
// ──────────────────────────────────────────────────────────────────────────────

// mapEvaluationToResponse converts a database DescriptiveEvaluation model to
// the API response struct. This is used by both CreateEvaluation and
// UpdateEvaluation to format the response.
func mapEvaluationToResponse(e *generated.DescriptiveEvaluation) evaluationResponse {
	return evaluationResponse{
		ID:        e.ID,
		StudentID: e.StudentID,
		TeacherID: e.TeacherID,
		Semester:  string(e.Semester),
		Content:   e.Content,
		CreatedAt: e.CreatedAt,
		UpdatedAt: e.UpdatedAt,
	}
}

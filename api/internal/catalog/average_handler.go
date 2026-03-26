// This file implements the semester close and average approval HTTP handlers
// for the CatalogRO API.
//
// Endpoints covered:
//
//	POST /catalog/averages/{subjectId}/close   — compute and close semester averages
//	POST /catalog/averages/{averageId}/approve — admin approves a closed average
//
// IMPORTANT DOMAIN CONTEXT (Romanian school system):
//   - "medie" = average, "închidere" = close, "aprobare" = approval
//   - At the end of each semester, the teacher "closes" the averages for their
//     subject in a class. This computes the final grade average for each student.
//   - The director (admin) then "approves" each average, locking it permanently.
//
// Average computation rules (per evaluation_configs):
//   - Primary (use_qualifiers=true): the "average" is a qualifier (FB/B/S/I)
//     determined by majority of qualifier grades. No numeric computation.
//   - Middle/High (use_qualifiers=false): arithmetic mean of non-thesis grades,
//     then thesis weighted in if the subject has a thesis (teză).
//     Formula: final = (1 - thesis_weight) * mean + thesis_weight * thesis_grade
//     The result is rounded per the school's rounding_rule setting.
//
// Authorization model:
//   - Only teachers assigned to the class+subject (or admins) can close.
//   - Only admins can approve.
package catalog

import (
	"encoding/json"
	"errors"
	"math"
	"math/big"
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

// ──────────────────────────────────────────────────────────────────────────────
// POST /catalog/averages/{subjectId}/close
// ──────────────────────────────────────────────────────────────────────────────

// closeAverageRequest is the expected JSON body for the close endpoint.
type closeAverageRequest struct {
	ClassID  uuid.UUID `json:"class_id"`
	Semester string    `json:"semester"`
}

// averageResponse is the JSON shape for a single average in API responses.
type averageResponse struct {
	ID             uuid.UUID  `json:"id"`
	StudentID      uuid.UUID  `json:"student_id"`
	ComputedValue  *float64   `json:"computed_value,omitempty"`
	FinalValue     *float64   `json:"final_value,omitempty"`
	QualifierFinal *string    `json:"qualifier_final,omitempty"`
	IsClosed       bool       `json:"is_closed"`
	ClosedBy       *uuid.UUID `json:"closed_by,omitempty"`
	ClosedAt       *time.Time `json:"closed_at,omitempty"`
	ApprovedBy     *uuid.UUID `json:"approved_by,omitempty"`
	ApprovedAt     *time.Time `json:"approved_at,omitempty"`
}

// studentAverageResult groups a student with the computed average for the response.
type studentAverageResult struct {
	StudentID string `json:"student_id"`
	FirstName string `json:"first_name"`
	LastName  string `json:"last_name"`
	Average   averageResponse `json:"average"`
}

// CloseAverage handles POST /catalog/averages/{subjectId}/close.
//
// Computes and closes semester averages for ALL students enrolled in a class
// for a given subject. The handler:
//  1. Validates the request and authorization.
//  2. Loads the evaluation config for the education level.
//  3. For each enrolled student, fetches their grades and computes the average.
//  4. Upserts the average row (creates or updates).
//  5. Returns the list of computed averages.
//
// Possible responses:
//   - 200 OK: { "data": { "averages": [...] } }
//   - 400 Bad Request: validation failure or insufficient grades
//   - 401 Unauthorized: auth context missing
//   - 403 Forbidden: not assigned to class+subject
//   - 500 Internal Server Error: database failure
func (h *Handler) CloseAverage(w http.ResponseWriter, r *http.Request) {
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

	queries := auth.GetQueries(r.Context())
	if queries == nil {
		httputil.InternalError(w)
		return
	}

	// Step 2: Only admins and teachers can close averages.
	if role != "admin" && role != "teacher" {
		httputil.Forbidden(w, "Only teachers and admins can close semester averages")
		return
	}

	// Step 3: Parse the subject ID from the URL.
	subjectID, err := uuid.Parse(chi.URLParam(r, "subjectId"))
	if err != nil {
		httputil.BadRequest(w, "INVALID_ID", "subjectId must be a valid UUID")
		return
	}

	// Step 4: Parse the request body.
	var req closeAverageRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httputil.BadRequest(w, "INVALID_JSON", "Request body must be valid JSON")
		return
	}

	if req.ClassID == uuid.Nil {
		httputil.BadRequest(w, "MISSING_FIELD", "class_id is required")
		return
	}
	if req.Semester != "I" && req.Semester != "II" {
		httputil.BadRequest(w, "INVALID_SEMESTER", "semester must be 'I' or 'II'")
		return
	}

	// Step 5: Authorization — teachers must be assigned to the class+subject.
	if role == "teacher" {
		_, err := queries.CheckTeacherClassSubject(r.Context(), generated.CheckTeacherClassSubjectParams{
			TeacherID: userID,
			ClassID:   req.ClassID,
			SubjectID: subjectID,
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

	// Step 6: Look up the subject to know its education level and thesis flag.
	subject, err := queries.GetSubjectByID(r.Context(), subjectID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			httputil.NotFound(w, "Subject not found")
			return
		}
		h.logger.Error("failed to get subject", "error", err)
		httputil.InternalError(w)
		return
	}

	// Step 7: Get the current school year.
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

	// Step 8: Load the evaluation config for this education level.
	evalConfig, err := queries.GetEvaluationConfig(r.Context(), generated.GetEvaluationConfigParams{
		EducationLevel: subject.EducationLevel,
		SchoolYearID:   schoolYear.ID,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			httputil.BadRequest(w, "NO_EVAL_CONFIG",
				"No evaluation configuration found for this education level and school year")
			return
		}
		h.logger.Error("failed to get evaluation config", "error", err)
		httputil.InternalError(w)
		return
	}

	// Step 9: Fetch all enrolled students in the class.
	students, err := queries.ListStudentsByClass(r.Context(), req.ClassID)
	if err != nil {
		h.logger.Error("failed to list students", "error", err, "class_id", req.ClassID)
		httputil.InternalError(w)
		return
	}

	if len(students) == 0 {
		httputil.BadRequest(w, "NO_STUDENTS", "No students enrolled in this class")
		return
	}

	// Step 10: For each student, fetch grades, compute average, and upsert.
	semester := generated.Semester(req.Semester)
	results := make([]studentAverageResult, 0, len(students))

	for i := range students {
		// Fetch all grades for this student/subject/semester.
		grades, err := queries.ListGradesForAverage(r.Context(), generated.ListGradesForAverageParams{
			StudentID:    students[i].ID,
			SubjectID:    subjectID,
			SchoolYearID: schoolYear.ID,
			Semester:     semester,
		})
		if err != nil {
			h.logger.Error("failed to list grades for average",
				"student_id", students[i].ID, "error", err)
			httputil.InternalError(w)
			return
		}

		// Compute the average based on the evaluation config.
		avg, err := computeAverage(grades, &evalConfig, subject.HasThesis)
		if err != nil {
			// Not enough grades — include error info in response but continue
			// to next student. The teacher can see who needs more grades.
			h.logger.Warn("insufficient grades for average",
				"student_id", students[i].ID,
				"subject_id", subjectID,
				"error", err)
			continue
		}

		// Upsert the average row.
		dbAvg, err := queries.CreateOrUpdateAverage(r.Context(), generated.CreateOrUpdateAverageParams{
			StudentID:    students[i].ID,
			ClassID:      req.ClassID,
			SubjectID:    subjectID,
			SchoolYearID: schoolYear.ID,
			Semester:     generated.NullSemester{Semester: semester, Valid: true},
			ComputedValue: avg.numericValue,
			FinalValue:    avg.numericValue,
			QualifierFinal: avg.qualifierValue,
			ClosedBy:     pgtype.UUID{Bytes: userID, Valid: true},
		})
		if err != nil {
			h.logger.Error("failed to upsert average",
				"student_id", students[i].ID, "error", err)
			httputil.InternalError(w)
			return
		}

		results = append(results, studentAverageResult{
			StudentID: students[i].ID.String(),
			FirstName: students[i].FirstName,
			LastName:  students[i].LastName,
			Average:   mapAverageToResponse(&dbAvg),
		})
	}

	// Step 11: Return the results.
	httputil.Success(w, map[string]any{
		"averages": results,
		"closed":   len(results),
		"total":    len(students),
	})
}

// ──────────────────────────────────────────────────────────────────────────────
// POST /catalog/averages/{averageId}/approve
// ──────────────────────────────────────────────────────────────────────────────

// ApproveAverage handles POST /catalog/averages/{averageId}/approve.
//
// Marks a closed average as approved by the school director (admin).
// Only admins can approve averages. The average must already be closed.
//
// Possible responses:
//   - 200 OK: { "data": { approved average } }
//   - 400 Bad Request: average is not closed yet
//   - 401 Unauthorized: auth context missing
//   - 403 Forbidden: not an admin
//   - 404 Not Found: average does not exist
//   - 500 Internal Server Error: database failure
func (h *Handler) ApproveAverage(w http.ResponseWriter, r *http.Request) {
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

	queries := auth.GetQueries(r.Context())
	if queries == nil {
		httputil.InternalError(w)
		return
	}

	// Step 2: Only admins can approve averages.
	if role != "admin" {
		httputil.Forbidden(w, "Only admins can approve semester averages")
		return
	}

	// Step 3: Parse the average ID from the URL.
	averageID, err := uuid.Parse(chi.URLParam(r, "averageId"))
	if err != nil {
		httputil.BadRequest(w, "INVALID_ID", "averageId must be a valid UUID")
		return
	}

	// Step 4: Fetch the existing average to verify it exists and is closed.
	existing, err := queries.GetAverageByID(r.Context(), averageID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			httputil.NotFound(w, "Average not found")
			return
		}
		h.logger.Error("failed to get average", "error", err, "average_id", averageID)
		httputil.InternalError(w)
		return
	}

	// The average must be closed before it can be approved.
	if !existing.IsClosed {
		httputil.BadRequest(w, "NOT_CLOSED", "Average must be closed before it can be approved")
		return
	}

	// Don't approve twice.
	if existing.ApprovedBy.Valid {
		httputil.BadRequest(w, "ALREADY_APPROVED", "This average has already been approved")
		return
	}

	// Step 5: Approve the average.
	approved, err := queries.ApproveAverage(r.Context(), generated.ApproveAverageParams{
		ID:         averageID,
		ApprovedBy: pgtype.UUID{Bytes: userID, Valid: true},
	})
	if err != nil {
		h.logger.Error("failed to approve average", "error", err, "average_id", averageID)
		httputil.InternalError(w)
		return
	}

	// Step 6: Return the approved average.
	httputil.Success(w, mapAverageToResponse(&approved))
}

// ──────────────────────────────────────────────────────────────────────────────
// Average computation
// ──────────────────────────────────────────────────────────────────────────────

// computedAverage holds the result of average computation.
// For primary school, only qualifierValue is set.
// For middle/high, only numericValue is set.
type computedAverage struct {
	numericValue   pgtype.Numeric
	qualifierValue generated.NullQualifier
}

// computeAverage calculates the semester average from a list of grades.
//
// For primary school (use_qualifiers=true):
//   - The "average" is the most frequent qualifier (majority rule).
//   - If there's a tie, the higher qualifier wins (FB > B > S > I).
//
// For middle/high school (use_qualifiers=false):
//   - Non-thesis grades: arithmetic mean.
//   - If the subject has a thesis: weighted average.
//     final = (1 - thesis_weight) * mean + thesis_weight * thesis_grade
//   - Result is rounded per the school's rounding_rule.
func computeAverage(
	grades []generated.ListGradesForAverageRow,
	config *generated.EvaluationConfig,
	hasThesis bool,
) (computedAverage, error) {
	if config.UseQualifiers {
		return computeQualifierAverage(grades, config.MinGradesSem)
	}
	return computeNumericAverage(grades, config, hasThesis)
}

// computeQualifierAverage determines the "average" qualifier for primary school.
// Uses majority rule: the most frequent qualifier wins.
// On tie, the higher qualifier wins (FB > B > S > I).
func computeQualifierAverage(
	grades []generated.ListGradesForAverageRow,
	minGrades int16,
) (computedAverage, error) {
	// Count qualifier occurrences.
	counts := map[string]int{}
	for _, g := range grades {
		if g.QualifierGrade.Valid {
			counts[string(g.QualifierGrade.Qualifier)]++
		}
	}

	total := 0
	for _, c := range counts {
		total += c
	}

	if total < int(minGrades) {
		return computedAverage{}, errors.New("insufficient qualifier grades")
	}

	// Find the qualifier with the highest count.
	// On tie, the order FB > B > S > I determines the winner.
	qualOrder := []string{"FB", "B", "S", "I"}
	bestQual := ""
	bestCount := 0
	for _, q := range qualOrder {
		if counts[q] > bestCount {
			bestCount = counts[q]
			bestQual = q
		}
	}

	return computedAverage{
		qualifierValue: generated.NullQualifier{
			Qualifier: generated.Qualifier(bestQual),
			Valid:     true,
		},
	}, nil
}

// computeNumericAverage calculates the arithmetic average for middle/high school.
// If the subject has a thesis, applies the weighted formula.
func computeNumericAverage(
	grades []generated.ListGradesForAverageRow,
	config *generated.EvaluationConfig,
	hasThesis bool,
) (computedAverage, error) {
	var regularGrades []float64
	var thesisGrade float64
	hasThesisGrade := false

	for _, g := range grades {
		if g.NumericGrade == nil {
			continue
		}
		if g.IsThesis {
			thesisGrade = float64(*g.NumericGrade)
			hasThesisGrade = true
		} else {
			regularGrades = append(regularGrades, float64(*g.NumericGrade))
		}
	}

	if len(regularGrades) < int(config.MinGradesSem) {
		return computedAverage{}, errors.New("insufficient numeric grades")
	}

	// Calculate arithmetic mean of regular (non-thesis) grades.
	sum := 0.0
	for _, g := range regularGrades {
		sum += g
	}
	mean := sum / float64(len(regularGrades))

	// Apply thesis weight if applicable.
	finalValue := mean
	if hasThesis && hasThesisGrade && config.ThesisWeight.Valid {
		// Parse thesis weight from pgtype.Numeric.
		tw, err := numericToFloat(config.ThesisWeight)
		if err == nil && tw > 0 {
			finalValue = (1-tw)*mean + tw*thesisGrade
		}
	}

	// Apply rounding rule.
	rounded := applyRounding(finalValue, config.RoundingRule)

	return computedAverage{
		numericValue: floatToNumeric(rounded),
	}, nil
}

// applyRounding applies the school's configured rounding rule to an average.
//
// Rounding rules:
//   - "standard": standard math rounding (round half away from zero)
//   - "round_up": always round up (ceiling)
//   - "round_half_up": round 0.5 up (common in Romanian schools)
//
// All rounding is to 2 decimal places.
func applyRounding(value float64, rule string) float64 {
	switch rule {
	case "round_up":
		// Ceiling to 2 decimal places.
		return math.Ceil(value*100) / 100
	case "round_half_up":
		// Round half up to 2 decimal places.
		return math.Floor(value*100+0.5) / 100
	default:
		// Standard rounding to 2 decimal places.
		return math.Round(value*100) / 100
	}
}

// ──────────────────────────────────────────────────────────────────────────────
// Helpers
// ──────────────────────────────────────────────────────────────────────────────

// numericToFloat converts a pgtype.Numeric to float64.
func numericToFloat(n pgtype.Numeric) (float64, error) {
	if !n.Valid {
		return 0, errors.New("null numeric")
	}
	f, err := n.Float64Value()
	if err != nil {
		return 0, err
	}
	return f.Float64, nil
}

// floatToNumeric converts a float64 to pgtype.Numeric.
// We multiply by 100 to get an integer (2 decimal places precision),
// then set Exp = -2 to restore the decimal point.
func floatToNumeric(f float64) pgtype.Numeric {
	// Round to 2 decimal places to avoid floating-point artifacts.
	cents := int64(math.Round(f * 100))
	n := pgtype.Numeric{
		Int:              new(big.Int).SetInt64(cents),
		Exp:              -2,
		Valid:            true,
		NaN:              false,
		InfinityModifier: pgtype.Finite,
	}
	return n
}

// mapAverageToResponse converts a database Average model to the API response struct.
func mapAverageToResponse(a *generated.Average) averageResponse {
	resp := averageResponse{
		ID:        a.ID,
		StudentID: a.StudentID,
		IsClosed:  a.IsClosed,
	}

	// Convert numeric values.
	if a.ComputedValue.Valid {
		if f, err := numericToFloat(a.ComputedValue); err == nil {
			resp.ComputedValue = &f
		}
	}
	if a.FinalValue.Valid {
		if f, err := numericToFloat(a.FinalValue); err == nil {
			resp.FinalValue = &f
		}
	}

	// Convert qualifier.
	if a.QualifierFinal.Valid {
		s := string(a.QualifierFinal.Qualifier)
		resp.QualifierFinal = &s
	}

	// Convert optional UUID fields.
	if a.ClosedBy.Valid {
		id := uuid.UUID(a.ClosedBy.Bytes)
		resp.ClosedBy = &id
	}
	if a.ApprovedBy.Valid {
		id := uuid.UUID(a.ApprovedBy.Bytes)
		resp.ApprovedBy = &id
	}

	// Convert optional timestamps.
	if a.ClosedAt.Valid {
		resp.ClosedAt = &a.ClosedAt.Time
	}
	if a.ApprovedAt.Valid {
		resp.ApprovedAt = &a.ApprovedAt.Time
	}

	return resp
}

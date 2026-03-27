// This file implements the student portability (EHEIF-aligned) HTTP handlers.
//
// Endpoints:
//
//	POST /interop/portability/export/{studentId} — export student record package
//	POST /interop/portability/import             — import a student record package
//
// EHEIF = European Higher Education Interoperability Framework. While designed
// for higher education, CatalogRO adopts the principle "data follows the
// student" for all education levels. When a student transfers between schools,
// the sending school exports a StudentRecordPackage (JSON) that the receiving
// school can import.
//
// Authorization:
//   - Export: admin or secretary (they handle student transfers)
//   - Import: admin or secretary at the receiving school
package portability

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/vlahsh/catalogro/api/db/generated"
	"github.com/vlahsh/catalogro/api/internal/auth"
	"github.com/vlahsh/catalogro/api/internal/httputil"
)

// Handler holds the dependencies for portability HTTP handlers.
type Handler struct {
	queries *generated.Queries
	logger  *slog.Logger
}

// NewHandler creates a new portability Handler.
func NewHandler(queries *generated.Queries, logger *slog.Logger) *Handler {
	return &Handler{queries: queries, logger: logger}
}

// ──────────────────────────────────────────────────────────────────────────────
// POST /interop/portability/export/{studentId}
// ──────────────────────────────────────────────────────────────────────────────

// ExportStudent handles POST /interop/portability/export/{studentId}.
//
// Generates a StudentRecordPackage containing the student's complete academic
// history at this school: grades, averages, absences, and descriptive evaluations
// for all school years.
func (h *Handler) ExportStudent(w http.ResponseWriter, r *http.Request) {
	role, err := auth.GetUserRole(r.Context())
	if err != nil {
		httputil.Unauthorized(w, "Authentication required")
		return
	}

	// Only admin and secretary can export student records (they handle transfers).
	if role != "admin" && role != "secretary" {
		httputil.Forbidden(w, "Only admins and secretaries can export student records")
		return
	}

	schoolID, err := auth.GetSchoolID(r.Context())
	if err != nil {
		httputil.Unauthorized(w, "Authentication required")
		return
	}

	queries := auth.GetQueries(r.Context())
	if queries == nil {
		httputil.InternalError(w)
		return
	}

	studentID, err := uuid.Parse(chi.URLParam(r, "studentId"))
	if err != nil {
		httputil.BadRequest(w, "INVALID_ID", "studentId must be a valid UUID")
		return
	}

	// Fetch the student.
	student, err := queries.GetUserByID(r.Context(), studentID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			httputil.NotFound(w, "Student not found")
			return
		}
		h.logger.Error("failed to get student", "error", err)
		httputil.InternalError(w)
		return
	}

	if string(student.Role) != "student" {
		httputil.BadRequest(w, "NOT_A_STUDENT", "The specified user is not a student")
		return
	}

	// Fetch the school info.
	school, err := queries.GetSchoolByID(r.Context(), schoolID)
	if err != nil {
		h.logger.Error("failed to get school", "error", err)
		httputil.InternalError(w)
		return
	}

	// Fetch ALL school years (not just current) for complete academic history.
	schoolYears, err := queries.ListSchoolYears(r.Context())
	if err != nil {
		h.logger.Error("failed to list school years", "error", err)
		httputil.InternalError(w)
		return
	}

	if len(schoolYears) == 0 {
		httputil.BadRequest(w, "NO_SCHOOL_YEAR", "No school years configured")
		return
	}

	// Fetch external IDs from source mappings.
	var identifiers []ExternalID
	mappings, err := queries.ListSourceMappingsByEntity(r.Context(), generated.ListSourceMappingsByEntityParams{
		EntityType: "user",
		EntityID:   studentID,
	})
	if err == nil {
		for i := range mappings {
			identifiers = append(identifiers, ExternalID{
				System: mappings[i].SourceSystem,
				Value:  mappings[i].SourceID,
			})
		}
	}

	// Build academic records for each school year.
	var academicRecords []AcademicRecord

	for yi := range schoolYears {
		sy := &schoolYears[yi]

		// Fetch averages (semester results).
		averages, err := queries.StudentReportAverages(r.Context(), generated.StudentReportAveragesParams{
			StudentID: studentID, SchoolYearID: sy.ID,
		})
		if err != nil {
			h.logger.Error("failed to get averages", "error", err, "year_id", sy.ID)
			continue
		}

		// Fetch absences.
		absences, err := queries.StudentReportAbsences(r.Context(), generated.StudentReportAbsencesParams{
			StudentID: studentID, SchoolYearID: sy.ID,
		})
		if err != nil {
			h.logger.Error("failed to get absences", "error", err, "year_id", sy.ID)
			continue
		}

		// Fetch grades (individual marks, not just averages).
		grades, err := queries.StudentReportGrades(r.Context(), generated.StudentReportGradesParams{
			StudentID: studentID, SchoolYearID: sy.ID,
		})
		if err != nil {
			h.logger.Error("failed to get grades", "error", err, "year_id", sy.ID)
			continue
		}

		// Fetch descriptive evaluations (primary school).
		evals, err := queries.StudentReportEvaluations(r.Context(), generated.StudentReportEvaluationsParams{
			StudentID: studentID, SchoolYearID: sy.ID,
		})
		if err != nil {
			h.logger.Error("failed to get evaluations", "error", err, "year_id", sy.ID)
			continue
		}

		// Skip school years with no data for this student.
		if len(averages) == 0 && len(grades) == 0 && len(absences) == 0 && len(evals) == 0 {
			continue
		}

		// Build subject results from averages.
		var results []SubjectResult
		for i := range averages {
			result := SubjectResult{
				Course: averages[i].SubjectName,
				Type:   "semester_average",
			}
			if averages[i].FinalValue.Valid {
				if f, fErr := averages[i].FinalValue.Float64Value(); fErr == nil {
					result.Grade = &f.Float64
				}
			}
			if averages[i].QualifierFinal.Valid {
				s := string(averages[i].QualifierFinal.Qualifier)
				result.Qualifier = &s
			}
			results = append(results, result)
		}

		// Add individual grade results (not just averages).
		for i := range grades {
			result := SubjectResult{
				Course: grades[i].SubjectName,
				Type:   "grade",
			}
			if grades[i].IsThesis {
				result.Type = "thesis"
			}
			if grades[i].NumericGrade != nil {
				f := float64(*grades[i].NumericGrade)
				result.Grade = &f
			}
			if grades[i].QualifierGrade.Valid {
				s := string(grades[i].QualifierGrade.Qualifier)
				result.Qualifier = &s
			}
			results = append(results, result)
		}

		// Add descriptive evaluations.
		for i := range evals {
			q := evals[i].Content
			results = append(results, SubjectResult{
				Course:    evals[i].SubjectName,
				Type:      "descriptive_evaluation",
				Qualifier: &q,
			})
		}

		// Build absence summary.
		absenceSummary := AbsenceSummary{}
		for i := range absences {
			absenceSummary.Total++
			if string(absences[i].AbsenceType) == "unexcused" {
				absenceSummary.Unexcused++
			} else {
				absenceSummary.Excused++
			}
		}

		academicRecords = append(academicRecords, AcademicRecord{
			SchoolYear: sy.Label,
			Results:    results,
			Absences:   absenceSummary,
		})
	}

	// Build the period from the earliest and latest school years.
	startDate := ""
	endDate := ""
	if len(schoolYears) > 0 {
		// schoolYears is sorted DESC by start_date, so last is earliest.
		oldest := schoolYears[len(schoolYears)-1]
		newest := schoolYears[0]
		if oldest.StartDate.Valid {
			startDate = oldest.StartDate.Time.Format("2006-01-02")
		}
		if newest.EndDate.Valid {
			endDate = newest.EndDate.Time.Format("2006-01-02")
		}
	}

	// Assemble the package.
	pkg := StudentRecordPackage{
		Version:         "1.0",
		Standard:        "catalogro-student-record",
		OneRosterCompat: true,
		ExportedAt:      time.Now().UTC().Format(time.RFC3339),
		ExportedBy: ExportedBy{
			SchoolName: school.Name,
			System:     "CatalogRO",
		},
		Student: StudentIdentity{
			SourcedID:   studentID.String(),
			GivenName:   student.FirstName,
			FamilyName:  student.LastName,
			Identifiers: identifiers,
		},
		SchoolHistory: []SchoolRecord{
			{
				School: SchoolInfo{
					Name:   school.Name,
					County: ptrToString(school.County),
					City:   ptrToString(school.City),
				},
				Period: Period{
					From: startDate,
					To:   endDate,
				},
				Records: academicRecords,
			},
		},
	}

	httputil.Success(w, pkg)
}

// ──────────────────────────────────────────────────────────────────────────────
// POST /interop/portability/import — import a student record package
// ──────────────────────────────────────────────────────────────────────────────

// ImportStudent handles POST /interop/portability/import.
//
// Accepts a StudentRecordPackage JSON and creates the student user in the
// receiving school. The academic history is stored as metadata (not as
// individual grade rows, since grades belong to the sending school's context).
func (h *Handler) ImportStudent(w http.ResponseWriter, r *http.Request) {
	role, err := auth.GetUserRole(r.Context())
	if err != nil {
		httputil.Unauthorized(w, "Authentication required")
		return
	}

	if role != "admin" && role != "secretary" {
		httputil.Forbidden(w, "Only admins and secretaries can import student records")
		return
	}

	queries := auth.GetQueries(r.Context())
	if queries == nil {
		httputil.InternalError(w)
		return
	}

	// Parse the incoming student record package.
	var pkg StudentRecordPackage
	if err := json.NewDecoder(r.Body).Decode(&pkg); err != nil {
		httputil.BadRequest(w, "INVALID_JSON", "Request body must be a valid StudentRecordPackage JSON")
		return
	}

	// Validate required fields.
	if pkg.Student.GivenName == "" || pkg.Student.FamilyName == "" {
		httputil.BadRequest(w, "MISSING_FIELD", "Student givenName and familyName are required")
		return
	}

	if pkg.Version == "" {
		httputil.BadRequest(w, "MISSING_FIELD", "Package version is required")
		return
	}

	// Check for duplicate: if a student with this sourcedId was already imported,
	// return the existing user instead of creating a duplicate.
	if pkg.Student.SourcedID != "" {
		existing, err := queries.GetSourceMappingByExternalID(r.Context(), generated.GetSourceMappingByExternalIDParams{
			SourceSystem: "portability",
			SourceID:     pkg.Student.SourcedID,
			EntityType:   "user",
		})
		if err == nil {
			// Already imported — return the existing user info.
			httputil.Success(w, map[string]any{
				"user_id":     existing.EntityID,
				"status":      "already_imported",
				"source_id":   existing.SourceID,
				"imported_at": existing.CreatedAt,
			})
			return
		}
		// pgx.ErrNoRows means not found — proceed with import.
	}

	// Provision the student in the receiving school.
	// The academic history from the sending school is stored as source metadata.
	historyJSON, err := json.Marshal(pkg.SchoolHistory)
	if err != nil {
		h.logger.Error("failed to marshal school history", "error", err)
		httputil.InternalError(w)
		return
	}

	activationToken := uuid.New().String()
	syntheticEmail := "transfer-" + uuid.New().String()[:8] + "@portability.import"

	newUser, err := queries.ProvisionUser(r.Context(), generated.ProvisionUserParams{
		FirstName:       pkg.Student.GivenName,
		LastName:        pkg.Student.FamilyName,
		Role:            generated.UserRoleStudent,
		Email:           &syntheticEmail,
		ActivationToken: &activationToken,
	})
	if err != nil {
		h.logger.Error("failed to provision transferred student", "error", err)
		httputil.InternalError(w)
		return
	}

	// Create a source mapping for traceability.
	sourceID := pkg.Student.SourcedID
	if sourceID == "" {
		sourceID = "transfer:" + newUser.ID.String()
	}

	_, err = queries.UpsertSourceMapping(r.Context(), generated.UpsertSourceMappingParams{
		EntityType:     "user",
		EntityID:       newUser.ID,
		SourceSystem:   "portability",
		SourceID:       sourceID,
		SourceMetadata: historyJSON,
	})
	if err != nil {
		h.logger.Warn("failed to create source mapping for transfer", "error", err)
		// Non-fatal — the user was still created.
	}

	httputil.Created(w, map[string]any{
		"user_id":          newUser.ID,
		"first_name":       newUser.FirstName,
		"last_name":        newUser.LastName,
		"activation_token": activationToken,
		"source_school":    pkg.ExportedBy.SchoolName,
		"academic_records": len(pkg.SchoolHistory),
	})
}

// ptrToString dereferences a *string, returning "" if nil.
func ptrToString(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

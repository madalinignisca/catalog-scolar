// This file implements the OneRoster 1.2 read-only API handlers for CatalogRO.
//
// These endpoints are designed for external system integration (LMS, SIS, ISJ tools).
// They return data in OneRoster 1.2 JSON Binding format, mapping CatalogRO entities
// to standard OneRoster types.
//
// All endpoints are read-only. Authentication uses the same JWT middleware as the
// main API. The OneRoster endpoints live under /oneroster/* and use the same
// RLS tenant context.
//
// Reference: IMS OneRoster 1.2 Final Specification
// https://www.imsglobal.org/oneroster-v12-final-specification
package oneroster

import (
	"encoding/json"
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/vlahsh/catalogro/api/db/generated"
	"github.com/vlahsh/catalogro/api/internal/auth"
)

// Handler holds the dependencies needed by OneRoster HTTP handlers.
type Handler struct {
	queries *generated.Queries
	logger  *slog.Logger
}

// NewHandler creates a new OneRoster Handler.
func NewHandler(queries *generated.Queries, logger *slog.Logger) *Handler {
	return &Handler{queries: queries, logger: logger}
}

// writeJSON writes a JSON response with the given status code.
func writeJSON(w http.ResponseWriter, status int, data any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(data)
}

// writeError writes a OneRoster-compliant error response.
func writeError(w http.ResponseWriter, status int, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"errors": []map[string]string{
			{"description": message},
		},
	})
}

// ──────────────────────────────────────────────────────────────────────────────
// GET /oneroster/orgs — list all organizations (schools)
// ──────────────────────────────────────────────────────────────────────────────

// ListOrgs returns all schools visible to the current tenant as OneRoster Orgs.
func (h *Handler) ListOrgs(w http.ResponseWriter, r *http.Request) {
	schoolID, err := auth.GetSchoolID(r.Context())
	if err != nil {
		writeError(w, http.StatusUnauthorized, "Authentication required")
		return
	}

	queries := auth.GetQueries(r.Context())
	if queries == nil {
		writeError(w, http.StatusInternalServerError, "Internal error")
		return
	}

	school, err := queries.GetSchoolByID(r.Context(), schoolID)
	if err != nil {
		h.logger.Error("failed to get school for OneRoster", "error", err)
		writeError(w, http.StatusInternalServerError, "Failed to retrieve organization")
		return
	}

	org := Org{
		SourcedID: school.ID.String(),
		Status:    StatusActive,
		Name:      school.Name,
		Type:      OrgSchool,
	}

	writeJSON(w, http.StatusOK, CollectionResponse[Org]{Data: []Org{org}})
}

// GetOrg returns a single organization by sourcedId.
func (h *Handler) GetOrg(w http.ResponseWriter, r *http.Request) {
	schoolID, err := auth.GetSchoolID(r.Context())
	if err != nil {
		writeError(w, http.StatusUnauthorized, "Authentication required")
		return
	}

	sourcedID := chi.URLParam(r, "sourcedId")
	if sourcedID != schoolID.String() {
		writeError(w, http.StatusNotFound, "Organization not found")
		return
	}

	queries := auth.GetQueries(r.Context())
	if queries == nil {
		writeError(w, http.StatusInternalServerError, "Internal error")
		return
	}

	school, err := queries.GetSchoolByID(r.Context(), schoolID)
	if err != nil {
		writeError(w, http.StatusNotFound, "Organization not found")
		return
	}

	org := Org{
		SourcedID: school.ID.String(),
		Status:    StatusActive,
		Name:      school.Name,
		Type:      OrgSchool,
	}

	writeJSON(w, http.StatusOK, SingleResponse[Org]{Data: org})
}

// ──────────────────────────────────────────────────────────────────────────────
// GET /oneroster/users — list all users
// ──────────────────────────────────────────────────────────────────────────────

// ListUsers returns all active users in the tenant as OneRoster Users.
func (h *Handler) ListUsers(w http.ResponseWriter, r *http.Request) {
	schoolID, err := auth.GetSchoolID(r.Context())
	if err != nil {
		writeError(w, http.StatusUnauthorized, "Authentication required")
		return
	}

	queries := auth.GetQueries(r.Context())
	if queries == nil {
		writeError(w, http.StatusInternalServerError, "Internal error")
		return
	}

	dbUsers, err := queries.ListUsersBySchool(r.Context())
	if err != nil {
		h.logger.Error("failed to list users for OneRoster", "error", err)
		writeError(w, http.StatusInternalServerError, "Failed to retrieve users")
		return
	}

	orgRef := GUIDRef{SourcedID: schoolID.String(), Type: "org"}
	users := make([]User, 0, len(dbUsers))
	for i := range dbUsers {
		users = append(users, User{
			SourcedID:  dbUsers[i].ID.String(),
			Status:     StatusActive,
			GivenName:  dbUsers[i].FirstName,
			FamilyName: dbUsers[i].LastName,
			Email:      stringOrEmpty(dbUsers[i].Email),
			Role:       mapRole(string(dbUsers[i].Role)),
			Orgs:       []GUIDRef{orgRef},
		})
	}

	writeJSON(w, http.StatusOK, CollectionResponse[User]{Data: users})
}

// GetUser returns a single user by sourcedId.
func (h *Handler) GetUser(w http.ResponseWriter, r *http.Request) {
	schoolID, err := auth.GetSchoolID(r.Context())
	if err != nil {
		writeError(w, http.StatusUnauthorized, "Authentication required")
		return
	}

	queries := auth.GetQueries(r.Context())
	if queries == nil {
		writeError(w, http.StatusInternalServerError, "Internal error")
		return
	}

	userID, err := uuid.Parse(chi.URLParam(r, "sourcedId"))
	if err != nil {
		writeError(w, http.StatusNotFound, "User not found")
		return
	}

	dbUser, err := queries.GetUserByID(r.Context(), userID)
	if err != nil {
		writeError(w, http.StatusNotFound, "User not found")
		return
	}

	orgRef := GUIDRef{SourcedID: schoolID.String(), Type: "org"}
	user := User{
		SourcedID:  dbUser.ID.String(),
		Status:     StatusActive,
		GivenName:  dbUser.FirstName,
		FamilyName: dbUser.LastName,
		Email:      stringOrEmpty(dbUser.Email),
		Role:       mapRole(string(dbUser.Role)),
		Orgs:       []GUIDRef{orgRef},
	}

	writeJSON(w, http.StatusOK, SingleResponse[User]{Data: user})
}

// ──────────────────────────────────────────────────────────────────────────────
// GET /oneroster/classes — list all classes
// ──────────────────────────────────────────────────────────────────────────────

// ListClasses returns all classes in the current school year as OneRoster Classes.
func (h *Handler) ListClasses(w http.ResponseWriter, r *http.Request) {
	schoolID, err := auth.GetSchoolID(r.Context())
	if err != nil {
		writeError(w, http.StatusUnauthorized, "Authentication required")
		return
	}

	queries := auth.GetQueries(r.Context())
	if queries == nil {
		writeError(w, http.StatusInternalServerError, "Internal error")
		return
	}

	schoolYear, err := queries.GetCurrentSchoolYear(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "No current school year")
		return
	}

	dbClasses, err := queries.ListClassesBySchoolYear(r.Context(), schoolYear.ID)
	if err != nil {
		h.logger.Error("failed to list classes for OneRoster", "error", err)
		writeError(w, http.StatusInternalServerError, "Failed to retrieve classes")
		return
	}

	schoolRef := GUIDRef{SourcedID: schoolID.String(), Type: "org"}
	termRef := GUIDRef{SourcedID: schoolYear.ID.String(), Type: "academicSession"}

	classes := make([]Class, 0, len(dbClasses))
	for i := range dbClasses {
		classes = append(classes, Class{
			SourcedID: dbClasses[i].ID.String(),
			Status:    StatusActive,
			Title:     dbClasses[i].Name,
			ClassType: "homeroom",
			School:    schoolRef,
			Terms:     []GUIDRef{termRef},
		})
	}

	writeJSON(w, http.StatusOK, CollectionResponse[Class]{Data: classes})
}

// ListClassStudents returns students enrolled in a class.
func (h *Handler) ListClassStudents(w http.ResponseWriter, r *http.Request) {
	schoolID, err := auth.GetSchoolID(r.Context())
	if err != nil {
		writeError(w, http.StatusUnauthorized, "Authentication required")
		return
	}

	queries := auth.GetQueries(r.Context())
	if queries == nil {
		writeError(w, http.StatusInternalServerError, "Internal error")
		return
	}

	classID, err := uuid.Parse(chi.URLParam(r, "sourcedId"))
	if err != nil {
		writeError(w, http.StatusNotFound, "Class not found")
		return
	}

	students, err := queries.ListStudentsByClass(r.Context(), classID)
	if err != nil {
		h.logger.Error("failed to list class students for OneRoster", "error", err)
		writeError(w, http.StatusInternalServerError, "Failed to retrieve students")
		return
	}

	orgRef := GUIDRef{SourcedID: schoolID.String(), Type: "org"}
	users := make([]User, 0, len(students))
	for i := range students {
		users = append(users, User{
			SourcedID:  students[i].ID.String(),
			Status:     StatusActive,
			GivenName:  students[i].FirstName,
			FamilyName: students[i].LastName,
			Email:      stringOrEmpty(students[i].Email),
			Role:       RoleStudent,
			Orgs:       []GUIDRef{orgRef},
		})
	}

	writeJSON(w, http.StatusOK, CollectionResponse[User]{Data: users})
}

// ──────────────────────────────────────────────────────────────────────────────
// GET /oneroster/courses — list all subjects as courses
// ──────────────────────────────────────────────────────────────────────────────

// ListCourses returns all active subjects as OneRoster Courses.
func (h *Handler) ListCourses(w http.ResponseWriter, r *http.Request) {
	schoolID, err := auth.GetSchoolID(r.Context())
	if err != nil {
		writeError(w, http.StatusUnauthorized, "Authentication required")
		return
	}

	queries := auth.GetQueries(r.Context())
	if queries == nil {
		writeError(w, http.StatusInternalServerError, "Internal error")
		return
	}

	dbSubjects, err := queries.ListSubjectsBySchool(r.Context())
	if err != nil {
		h.logger.Error("failed to list subjects for OneRoster", "error", err)
		writeError(w, http.StatusInternalServerError, "Failed to retrieve courses")
		return
	}

	orgRef := GUIDRef{SourcedID: schoolID.String(), Type: "org"}
	courses := make([]Course, 0, len(dbSubjects))
	for i := range dbSubjects {
		code := ""
		if dbSubjects[i].ShortName != nil {
			code = *dbSubjects[i].ShortName
		}
		courses = append(courses, Course{
			SourcedID:  dbSubjects[i].ID.String(),
			Status:     StatusActive,
			Title:      dbSubjects[i].Name,
			CourseCode: code,
			Org:        orgRef,
		})
	}

	writeJSON(w, http.StatusOK, CollectionResponse[Course]{Data: courses})
}

// ──────────────────────────────────────────────────────────────────────────────
// GET /oneroster/enrollments — list all enrollments
// ──────────────────────────────────────────────────────────────────────────────

// ListEnrollments returns all class enrollments as OneRoster Enrollments.
func (h *Handler) ListEnrollments(w http.ResponseWriter, r *http.Request) {
	schoolID, err := auth.GetSchoolID(r.Context())
	if err != nil {
		writeError(w, http.StatusUnauthorized, "Authentication required")
		return
	}

	queries := auth.GetQueries(r.Context())
	if queries == nil {
		writeError(w, http.StatusInternalServerError, "Internal error")
		return
	}

	schoolYear, err := queries.GetCurrentSchoolYear(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "No current school year")
		return
	}

	dbClasses, err := queries.ListClassesBySchoolYear(r.Context(), schoolYear.ID)
	if err != nil {
		h.logger.Error("failed to list classes for enrollments", "error", err)
		writeError(w, http.StatusInternalServerError, "Failed to retrieve enrollments")
		return
	}

	schoolRef := GUIDRef{SourcedID: schoolID.String(), Type: "org"}
	enrollments := make([]Enrollment, 0)

	for i := range dbClasses {
		students, err := queries.ListStudentsByClass(r.Context(), dbClasses[i].ID)
		if err != nil {
			h.logger.Error("failed to list students for enrollment", "error", err,
				"class_id", dbClasses[i].ID)
			continue
		}
		classRef := GUIDRef{SourcedID: dbClasses[i].ID.String(), Type: "class"}
		for j := range students {
			enrollments = append(enrollments, Enrollment{
				SourcedID: students[j].ID.String() + ":" + dbClasses[i].ID.String(),
				Status:    StatusActive,
				User:      GUIDRef{SourcedID: students[j].ID.String(), Type: "user"},
				Class:     classRef,
				School:    schoolRef,
				Role:      RoleStudent,
			})
		}
	}

	writeJSON(w, http.StatusOK, CollectionResponse[Enrollment]{Data: enrollments})
}

// ──────────────────────────────────────────────────────────────────────────────
// GET /oneroster/academicSessions — list school years and terms
// ──────────────────────────────────────────────────────────────────────────────

// ListAcademicSessions returns school years as OneRoster AcademicSessions.
func (h *Handler) ListAcademicSessions(w http.ResponseWriter, r *http.Request) {
	queries := auth.GetQueries(r.Context())
	if queries == nil {
		writeError(w, http.StatusInternalServerError, "Internal error")
		return
	}

	schoolYear, err := queries.GetCurrentSchoolYear(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "No current school year")
		return
	}

	startDate := ""
	endDate := ""
	if schoolYear.StartDate.Valid {
		startDate = schoolYear.StartDate.Time.Format("2006-01-02")
	}
	if schoolYear.EndDate.Valid {
		endDate = schoolYear.EndDate.Time.Format("2006-01-02")
	}

	session := AcademicSession{
		SourcedID:  schoolYear.ID.String(),
		Status:     StatusActive,
		Title:      schoolYear.Label,
		Type:       SessionSchoolYear,
		StartDate:  startDate,
		EndDate:    endDate,
		SchoolYear: schoolYear.Label,
	}

	writeJSON(w, http.StatusOK, CollectionResponse[AcademicSession]{Data: []AcademicSession{session}})
}

// ──────────────────────────────────────────────────────────────────────────────
// GET /oneroster/lineItems — list gradable items
// GET /oneroster/results — list grades/scores
// ──────────────────────────────────────────────────────────────────────────────

// ListLineItems returns a stub list. Full implementation requires mapping
// subject+semester+class combinations to line items.
func (h *Handler) ListLineItems(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, CollectionResponse[LineItem]{Data: []LineItem{}})
}

// ListResults returns a stub list. Full implementation requires mapping
// all grades across all classes.
func (h *Handler) ListResults(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, CollectionResponse[Result]{Data: []Result{}})
}

// ──────────────────────────────────────────────────────────────────────────────
// Helpers
// ──────────────────────────────────────────────────────────────────────────────

// mapRole converts a CatalogRO role to a OneRoster RoleType.
func mapRole(role string) RoleType {
	switch role {
	case "student":
		return RoleStudent
	case "teacher":
		return RoleTeacher
	case "parent":
		return RoleGuardian
	case "admin", "secretary":
		return RoleAdmin
	default:
		return RoleStudent
	}
}

// stringOrEmpty returns the string value or empty string for nil pointers.
func stringOrEmpty(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

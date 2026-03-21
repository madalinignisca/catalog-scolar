// Package school implements HTTP handlers for school, class, and subject
// management endpoints in the CatalogRO API.
//
// These handlers cover the "School config" and "Classes" sections of the API:
//
//	GET  /schools/current               — current school info
//	GET  /classes                       — list classes for current school year
//	GET  /classes/{classId}             — class details with enrolled students
//	GET  /classes/{classId}/teachers    — teacher assignments for a class
//	GET  /subjects                      — list subjects
//
// All endpoints require authentication. The user's school_id (tenant) is read
// from the request context, which was set by the auth/tenant middleware. The
// database queries use PostgreSQL Row-Level Security (RLS) so that each school
// can only see its own data.
package school

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

// Handler holds the dependencies needed by all school-related HTTP handlers.
//
// In Go, we use a struct to group handlers that share the same dependencies
// (database queries, logger, etc.). Each HTTP handler method is defined on
// this struct so it can access the shared dependencies without global variables.
type Handler struct {
	// queries is the sqlc-generated query interface. It provides type-safe
	// database methods like GetSchoolByCurrentTenant, ListClassesBySchoolYear, etc.
	queries *generated.Queries

	// logger is used to record errors and debug information. We use the
	// structured logger (slog) so that log entries include context fields
	// like request_id, user_id, etc.
	logger *slog.Logger
}

// NewHandler creates a new Handler with the given dependencies.
// This is called once at application startup (in main.go) and the returned
// handler is reused for every request — it is safe for concurrent use.
func NewHandler(queries *generated.Queries, logger *slog.Logger) *Handler {
	return &Handler{
		queries: queries,
		logger:  logger,
	}
}

// ──────────────────────────────────────────────────────────────────────────────
// GET /schools/current
// ──────────────────────────────────────────────────────────────────────────────

// schoolResponse is the JSON shape returned by GET /schools/current.
// We define a dedicated response struct (rather than returning the raw DB model)
// so that we control exactly which fields are exposed to the API consumer and
// can rename or omit fields without changing the database schema.
type schoolResponse struct {
	ID              uuid.UUID `json:"id"`
	Name            string    `json:"name"`
	SiiirCode       *string   `json:"siiir_code"`
	EducationLevels []string  `json:"education_levels"`
	Address         *string   `json:"address"`
	City            *string   `json:"city"`
	County          *string   `json:"county"`
	Phone           *string   `json:"phone"`
	Email           *string   `json:"email"`
	IsActive        bool      `json:"is_active"`
}

// GetCurrentSchool handles GET /schools/current.
//
// It returns the school details for the currently-authenticated tenant.
// The tenant is determined by the school_id stored in the JWT token and
// set in the database session by the tenant middleware.
//
// Possible responses:
//   - 200 OK: { "data": { school details } }
//   - 401 Unauthorized: auth context missing
//   - 404 Not Found: school not found (should never happen if tenant middleware works)
//   - 500 Internal Server Error: database failure
func (h *Handler) GetCurrentSchool(w http.ResponseWriter, r *http.Request) {
	// Step 1: Verify that the user is authenticated.
	// GetSchoolID reads the school UUID from the request context. If the auth
	// middleware has not run (e.g. misconfigured routes), this returns an error.
	_, err := auth.GetSchoolID(r.Context())
	if err != nil {
		httputil.Unauthorized(w, "Authentication required")
		return
	}

	// Step 2: Query the database for the current tenant's school.
	// GetSchoolByCurrentTenant uses current_school_id() in the SQL, which reads
	// the PostgreSQL session variable set by the tenant middleware.
	school, err := h.queries.GetSchoolByCurrentTenant(r.Context())
	if err != nil {
		// If no rows are found, the tenant ID in the JWT does not match any school.
		// This could indicate a deleted school or a corrupted token.
		if errors.Is(err, pgx.ErrNoRows) {
			httputil.NotFound(w, "School not found for current tenant")
			return
		}
		// For any other database error, log it and return a generic 500.
		// We never expose raw database errors to the client for security reasons.
		h.logger.Error("failed to get current school", "error", err)
		httputil.InternalError(w)
		return
	}

	// Step 3: Map the database model to the API response struct.
	// We convert the education_levels from the generated enum slice to plain strings
	// so the JSON output is clean (e.g. ["primary","middle"] instead of typed enums).
	levels := make([]string, len(school.EducationLevels))
	for i, l := range school.EducationLevels {
		levels[i] = string(l)
	}

	// Step 4: Send the response wrapped in the standard { "data": { ... } } envelope.
	httputil.Success(w, schoolResponse{
		ID:              school.ID,
		Name:            school.Name,
		SiiirCode:       school.SiiirCode,
		EducationLevels: levels,
		Address:         school.Address,
		City:            school.City,
		County:          school.County,
		Phone:           school.Phone,
		Email:           school.Email,
		IsActive:        school.IsActive,
	})
}

// ──────────────────────────────────────────────────────────────────────────────
// GET /classes
// ──────────────────────────────────────────────────────────────────────────────

// classListItem is the JSON shape for each class in the GET /classes response.
type classListItem struct {
	ID              uuid.UUID  `json:"id"`
	Name            string     `json:"name"`
	EducationLevel  string     `json:"education_level"`
	GradeNumber     int16      `json:"grade_number"`
	MaxStudents     *int16     `json:"max_students"`
	HomeroomTeacher *teacherBrief `json:"homeroom_teacher"`
}

// teacherBrief is a compact teacher representation used inside class responses.
// It only includes the fields needed for display (name), not sensitive data.
type teacherBrief struct {
	ID        uuid.UUID `json:"id"`
	FirstName string    `json:"first_name"`
	LastName  string    `json:"last_name"`
}

// ListClasses handles GET /classes.
//
// Returns all classes for the current school year. If the authenticated user
// is a teacher, only their assigned classes are returned. Admins and secretaries
// see all classes.
//
// Possible responses:
//   - 200 OK: { "data": [ ...classes ] }
//   - 401 Unauthorized: auth context missing
//   - 500 Internal Server Error: database failure
func (h *Handler) ListClasses(w http.ResponseWriter, r *http.Request) {
	// Step 1: Extract the authenticated user's identity from the request context.
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

	// Step 2: Get the current school year. Classes are always scoped to a school year.
	// If the school has not configured a current school year, we return an empty list
	// rather than an error, because this is a valid (albeit incomplete) configuration.
	schoolYear, err := h.queries.GetCurrentSchoolYear(r.Context())
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			// No current school year configured — return empty class list.
			httputil.List(w, []any{}, nil)
			return
		}
		h.logger.Error("failed to get current school year", "error", err)
		httputil.InternalError(w)
		return
	}

	// Step 3: Fetch classes based on the user's role.
	// Teachers only see the classes they are assigned to teach.
	// Admins, secretaries, and other roles see all classes in the school.
	var items []classListItem

	if role == "teacher" {
		// For teachers: query only their assigned classes.
		rows, err := h.queries.ListClassesByTeacher(r.Context(), generated.ListClassesByTeacherParams{
			TeacherID:    userID,
			SchoolYearID: schoolYear.ID,
		})
		if err != nil {
			h.logger.Error("failed to list classes for teacher", "error", err, "teacher_id", userID)
			httputil.InternalError(w)
			return
		}
		items = make([]classListItem, len(rows))
		for i, row := range rows {
			items[i] = classListItem{
				ID:             row.ID,
				Name:           row.Name,
				EducationLevel: string(row.EducationLevel),
				GradeNumber:    row.GradeNumber,
				MaxStudents:    row.MaxStudents,
			}
			// Include homeroom teacher info if the class has one assigned.
			if row.HomeroomTeacherID.Valid {
				items[i].HomeroomTeacher = &teacherBrief{
					ID:        row.HomeroomTeacherID.Bytes,
					FirstName: ptrOrEmpty(row.HomeroomFirstName),
					LastName:  ptrOrEmpty(row.HomeroomLastName),
				}
			}
		}
	} else {
		// For admins, secretaries, parents, students: query all classes in the school year.
		rows, err := h.queries.ListClassesBySchoolYear(r.Context(), schoolYear.ID)
		if err != nil {
			h.logger.Error("failed to list classes", "error", err)
			httputil.InternalError(w)
			return
		}
		items = make([]classListItem, len(rows))
		for i, row := range rows {
			items[i] = classListItem{
				ID:             row.ID,
				Name:           row.Name,
				EducationLevel: string(row.EducationLevel),
				GradeNumber:    row.GradeNumber,
				MaxStudents:    row.MaxStudents,
			}
			if row.HomeroomTeacherID.Valid {
				items[i].HomeroomTeacher = &teacherBrief{
					ID:        row.HomeroomTeacherID.Bytes,
					FirstName: ptrOrEmpty(row.HomeroomFirstName),
					LastName:  ptrOrEmpty(row.HomeroomLastName),
				}
			}
		}
	}

	// Step 4: Return the list wrapped in the standard response envelope.
	httputil.List(w, items, nil)
}

// ──────────────────────────────────────────────────────────────────────────────
// GET /classes/{classId}
// ──────────────────────────────────────────────────────────────────────────────

// classDetailResponse is the JSON shape for GET /classes/{classId}.
// It includes the class info plus the list of enrolled students.
type classDetailResponse struct {
	ID              uuid.UUID      `json:"id"`
	Name            string         `json:"name"`
	EducationLevel  string         `json:"education_level"`
	GradeNumber     int16          `json:"grade_number"`
	MaxStudents     *int16         `json:"max_students"`
	HomeroomTeacher *teacherBrief  `json:"homeroom_teacher"`
	Students        []studentBrief `json:"students"`
}

// studentBrief is a compact student representation for class enrollment lists.
type studentBrief struct {
	ID        uuid.UUID `json:"id"`
	FirstName string    `json:"first_name"`
	LastName  string    `json:"last_name"`
	Email     *string   `json:"email,omitempty"`
}

// GetClass handles GET /classes/{classId}.
//
// Returns the class details along with the list of currently-enrolled students.
// Students are ordered alphabetically by last name (standard Romanian catalog order).
//
// Possible responses:
//   - 200 OK: { "data": { class with students } }
//   - 400 Bad Request: invalid classId format
//   - 401 Unauthorized: auth context missing
//   - 404 Not Found: class does not exist
//   - 500 Internal Server Error: database failure
func (h *Handler) GetClass(w http.ResponseWriter, r *http.Request) {
	// Step 1: Verify authentication.
	_, err := auth.GetSchoolID(r.Context())
	if err != nil {
		httputil.Unauthorized(w, "Authentication required")
		return
	}

	// Step 2: Parse the class ID from the URL path parameter.
	// chi.URLParam extracts named parameters from the route (e.g. {classId}).
	classIDStr := chi.URLParam(r, "classId")
	classID, err := uuid.Parse(classIDStr)
	if err != nil {
		httputil.BadRequest(w, "INVALID_ID", "classId must be a valid UUID")
		return
	}

	// Step 3: Fetch the class from the database.
	cls, err := h.queries.GetClassByID(r.Context(), classID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			httputil.NotFound(w, "Class not found")
			return
		}
		h.logger.Error("failed to get class", "error", err, "class_id", classID)
		httputil.InternalError(w)
		return
	}

	// Step 4: Fetch the enrolled students for this class.
	studentRows, err := h.queries.ListStudentsByClass(r.Context(), classID)
	if err != nil {
		h.logger.Error("failed to list students for class", "error", err, "class_id", classID)
		httputil.InternalError(w)
		return
	}

	// Step 5: Build the response.
	students := make([]studentBrief, len(studentRows))
	for i, s := range studentRows {
		students[i] = studentBrief{
			ID:        s.ID,
			FirstName: s.FirstName,
			LastName:  s.LastName,
			Email:     s.Email,
		}
	}

	resp := classDetailResponse{
		ID:             cls.ID,
		Name:           cls.Name,
		EducationLevel: string(cls.EducationLevel),
		GradeNumber:    cls.GradeNumber,
		MaxStudents:    cls.MaxStudents,
		Students:       students,
	}

	// Include homeroom teacher if assigned.
	if cls.HomeroomTeacherID.Valid {
		resp.HomeroomTeacher = &teacherBrief{
			ID:        cls.HomeroomTeacherID.Bytes,
			FirstName: ptrOrEmpty(cls.HomeroomFirstName),
			LastName:  ptrOrEmpty(cls.HomeroomLastName),
		}
	}

	httputil.Success(w, resp)
}

// ──────────────────────────────────────────────────────────────────────────────
// GET /classes/{classId}/teachers
// ──────────────────────────────────────────────────────────────────────────────

// teacherAssignment is the JSON shape for each entry in the teacher list.
// It shows which teacher teaches which subject in the class.
type teacherAssignment struct {
	ID           uuid.UUID `json:"id"`
	TeacherID    uuid.UUID `json:"teacher_id"`
	FirstName    string    `json:"first_name"`
	LastName     string    `json:"last_name"`
	SubjectID    uuid.UUID `json:"subject_id"`
	SubjectName  string    `json:"subject_name"`
	HoursPerWeek int16     `json:"hours_per_week"`
}

// ListTeachers handles GET /classes/{classId}/teachers.
//
// Returns the list of teacher-subject assignments for a class. This tells the
// frontend which teachers are assigned to teach which subjects in the class.
//
// Possible responses:
//   - 200 OK: { "data": [ ...assignments ] }
//   - 400 Bad Request: invalid classId format
//   - 401 Unauthorized: auth context missing
//   - 500 Internal Server Error: database failure
func (h *Handler) ListTeachers(w http.ResponseWriter, r *http.Request) {
	// Step 1: Verify authentication.
	_, err := auth.GetSchoolID(r.Context())
	if err != nil {
		httputil.Unauthorized(w, "Authentication required")
		return
	}

	// Step 2: Parse the class ID from the URL.
	classIDStr := chi.URLParam(r, "classId")
	classID, err := uuid.Parse(classIDStr)
	if err != nil {
		httputil.BadRequest(w, "INVALID_ID", "classId must be a valid UUID")
		return
	}

	// Step 3: Fetch teacher assignments from the database.
	rows, err := h.queries.ListTeachersByClass(r.Context(), classID)
	if err != nil {
		h.logger.Error("failed to list teachers for class", "error", err, "class_id", classID)
		httputil.InternalError(w)
		return
	}

	// Step 4: Map database rows to the API response format.
	items := make([]teacherAssignment, len(rows))
	for i, row := range rows {
		items[i] = teacherAssignment{
			ID:           row.ID,
			TeacherID:    row.TeacherID,
			FirstName:    row.TeacherFirstName,
			LastName:     row.TeacherLastName,
			SubjectID:    row.SubjectID,
			SubjectName:  row.SubjectName,
			HoursPerWeek: row.HoursPerWeek,
		}
	}

	httputil.List(w, items, nil)
}

// ──────────────────────────────────────────────────────────────────────────────
// GET /subjects
// ──────────────────────────────────────────────────────────────────────────────

// subjectResponse is the JSON shape for each subject in the list.
type subjectResponse struct {
	ID             uuid.UUID `json:"id"`
	Name           string    `json:"name"`
	ShortName      *string   `json:"short_name"`
	EducationLevel string    `json:"education_level"`
	HasThesis      bool      `json:"has_thesis"`
}

// ListSubjects handles GET /subjects.
//
// Returns all active subjects for the current school. Subjects are scoped by
// RLS to the current tenant automatically.
//
// Possible responses:
//   - 200 OK: { "data": [ ...subjects ] }
//   - 401 Unauthorized: auth context missing
//   - 500 Internal Server Error: database failure
func (h *Handler) ListSubjects(w http.ResponseWriter, r *http.Request) {
	// Step 1: Verify authentication.
	_, err := auth.GetSchoolID(r.Context())
	if err != nil {
		httputil.Unauthorized(w, "Authentication required")
		return
	}

	// Step 2: Fetch all active subjects from the database.
	rows, err := h.queries.ListSubjectsBySchool(r.Context())
	if err != nil {
		h.logger.Error("failed to list subjects", "error", err)
		httputil.InternalError(w)
		return
	}

	// Step 3: Map to API response format.
	items := make([]subjectResponse, len(rows))
	for i, row := range rows {
		items[i] = subjectResponse{
			ID:             row.ID,
			Name:           row.Name,
			ShortName:      row.ShortName,
			EducationLevel: string(row.EducationLevel),
			HasThesis:      row.HasThesis,
		}
	}

	httputil.List(w, items, nil)
}

// ──────────────────────────────────────────────────────────────────────────────
// Helpers
// ──────────────────────────────────────────────────────────────────────────────

// ptrOrEmpty safely dereferences a *string pointer.
// Returns the string value if the pointer is non-nil, or an empty string if nil.
// This is used when mapping nullable database columns to non-nullable JSON fields.
func ptrOrEmpty(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

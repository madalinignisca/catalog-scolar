// Package school implements HTTP handlers for school, class, and subject
// management endpoints in the CatalogRO API.
//
// These handlers cover the "School config" and "Classes" sections of the API:
//
//	GET  /schools/current               — current school info
//	GET  /schools/current/year          — current school year with semester dates
//	GET  /classes                       — list classes for current school year
//	GET  /classes/{classId}             — class details with enrolled students
//	GET  /classes/{classId}/teachers    — teacher assignments for a class
//	GET  /subjects                      — list subjects
//	POST /subjects                      — create a new subject (admin only)
//
// All endpoints require authentication. The user's school_id (tenant) is read
// from the request context, which was set by the auth/tenant middleware. The
// database queries use PostgreSQL Row-Level Security (RLS) so that each school
// can only see its own data.
package school

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"

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

	// Step 1b: Retrieve the transaction-scoped Queries from context.
	// This Queries object is bound to a PostgreSQL transaction that has the
	// RLS tenant variable (app.current_school_id) already set, so all queries
	// through it are automatically filtered to the current school.
	queries := auth.GetQueries(r.Context())
	if queries == nil {
		httputil.InternalError(w)
		return
	}

	// Step 2: Query the database for the current tenant's school.
	// GetSchoolByCurrentTenant uses current_school_id() in the SQL, which reads
	// the PostgreSQL session variable set by the tenant middleware.
	school, err := queries.GetSchoolByCurrentTenant(r.Context())
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

	// Step 3: Send the response wrapped in the standard { "data": { ... } } envelope.
	// Note: education_levels is omitted from the query (array type needs custom pgx
	// registration). It can be added later when the school settings page needs it.
	httputil.Success(w, schoolResponse{
		ID:              school.ID,
		Name:            school.Name,
		SiiirCode:       school.SiiirCode,
		EducationLevels: []string{}, // TODO: add when pgx array type is registered
		Address:         school.Address,
		City:            school.City,
		County:          school.County,
		Phone:           school.Phone,
		Email:           school.Email,
		IsActive:        school.IsActive,
	})
}

// ──────────────────────────────────────────────────────────────────────────────
// GET /schools/current/year
// ──────────────────────────────────────────────────────────────────────────────

// schoolYearResponse is the JSON shape returned by GET /schools/current/year.
//
// We define a dedicated response struct (instead of returning the raw DB model)
// for two reasons:
//  1. We can rename or omit fields without touching the database schema.
//  2. pgtype.Date must be converted to a plain string so that JSON clients
//     receive "2026-09-14" instead of a pgtype-specific struct.
//
// All date fields use the ISO 8601 / "YYYY-MM-DD" format expected by the
// Romanian education calendar (starts mid-September, ends mid-June).
type schoolYearResponse struct {
	// ID is the school year's primary key UUID.
	ID string `json:"id"`

	// Label is the human-readable school year name, e.g. "2026-2027".
	Label string `json:"label"`

	// StartDate is the first day of the school year (ISO 8601).
	StartDate string `json:"start_date"`

	// EndDate is the last day of the school year (ISO 8601).
	EndDate string `json:"end_date"`

	// Sem1Start is the first day of the first semester (ISO 8601).
	Sem1Start string `json:"sem1_start"`

	// Sem1End is the last day of the first semester (ISO 8601).
	Sem1End string `json:"sem1_end"`

	// Sem2Start is the first day of the second semester (ISO 8601).
	Sem2Start string `json:"sem2_start"`

	// Sem2End is the last day of the second semester (ISO 8601).
	Sem2End string `json:"sem2_end"`

	// IsCurrent flags this as the active school year for the school.
	// There can only be one current year per school (enforced by a partial
	// unique index in PostgreSQL).
	IsCurrent bool `json:"is_current"`
}

// GetCurrentYear handles GET /schools/current/year.
//
// Returns the school year that is currently marked as active (is_current = true)
// for the authenticated user's school. The query is tenant-scoped via RLS —
// no school_id parameter is needed in the URL.
//
// Possible responses:
//   - 200 OK:                    { "data": { school year fields } }
//   - 401 Unauthorized:          auth context missing (JWT not provided)
//   - 404 Not Found:             no school year is currently marked as current
//   - 500 Internal Server Error: database failure
func (h *Handler) GetCurrentYear(w http.ResponseWriter, r *http.Request) {
	// Step 1: Verify that the user is authenticated.
	// GetSchoolID reads the school UUID from the JWT claims in the request
	// context. If the auth middleware has not run (e.g. misconfigured routes)
	// this returns an error and we must reject the request immediately.
	_, err := auth.GetSchoolID(r.Context())
	if err != nil {
		httputil.Unauthorized(w, "Authentication required")
		return
	}

	// Step 1b: Retrieve the transaction-scoped Queries from context.
	// This Queries object is bound to a PostgreSQL transaction that already has
	// the RLS tenant variable (app.current_school_id) set, so every query
	// through it is automatically filtered to the current school — the handler
	// never needs to pass school_id explicitly.
	queries := auth.GetQueries(r.Context())
	if queries == nil {
		// This should never happen when the middleware chain is correctly wired,
		// but we guard against it to avoid a nil-pointer panic in the handler.
		httputil.InternalError(w)
		return
	}

	// Step 2: Query the database for the current school year.
	// GetCurrentSchoolYear uses current_school_id() in the SQL, which reads
	// the PostgreSQL session variable set by the TenantContext middleware.
	// The query returns exactly one row (the current year) or pgx.ErrNoRows.
	year, err := queries.GetCurrentSchoolYear(r.Context())
	if err != nil {
		// If no rows are found, the school has not yet configured a current
		// school year. This is a valid business state (e.g. the school is newly
		// created). We return 404 with a descriptive message rather than 500
		// so that the frontend can show a friendly "no year configured" screen.
		if errors.Is(err, pgx.ErrNoRows) {
			httputil.NotFound(w, "No current school year configured")
			return
		}
		// For any other database error (connection loss, RLS misconfiguration,
		// unexpected schema change), log the full error internally and return
		// a generic 500 response. We never expose raw DB errors to the client.
		h.logger.Error("get_current_year: database query failed", "error", err)
		httputil.InternalError(w)
		return
	}

	// Step 3: Build the response struct by mapping DB fields to JSON-friendly types.
	// pgtype.Date stores the date as a time.Time value and a Valid boolean flag.
	// We convert it to a plain "YYYY-MM-DD" string using the formatPgDate helper
	// defined below, which is safe to call even when Valid = false (returns "").
	httputil.Success(w, schoolYearResponse{
		ID:        year.ID.String(),
		Label:     year.Label,
		StartDate: formatPgDate(year.StartDate),
		EndDate:   formatPgDate(year.EndDate),
		Sem1Start: formatPgDate(year.Sem1Start),
		Sem1End:   formatPgDate(year.Sem1End),
		Sem2Start: formatPgDate(year.Sem2Start),
		Sem2End:   formatPgDate(year.Sem2End),
		IsCurrent: year.IsCurrent,
	})
}

// formatPgDate converts a pgtype.Date value to an ISO 8601 "YYYY-MM-DD" string.
//
// pgtype.Date wraps the Go time.Time type with a Valid flag (similar to
// database/sql.NullTime). When Valid = false the column contained SQL NULL;
// we return an empty string in that case rather than a zero-time string such
// as "0001-01-01", which would be misleading to API consumers.
//
// This function is defined in the school package rather than in httputil
// because it is specific to the school year domain and not used elsewhere yet.
//
// Example output: "2026-09-14"
func formatPgDate(d pgtype.Date) string {
	// pgtype.Date.Valid is false when the database column contained SQL NULL.
	// Returning an empty string is the least-surprising behaviour for API
	// consumers: they can check `if date == ""` rather than parsing a sentinel.
	if !d.Valid {
		return ""
	}
	// time.Time.Format uses Go's reference time layout ("Mon Jan 2 15:04:05 MST 2006").
	// "2006-01-02" is the magic layout string that produces ISO 8601 date output.
	return d.Time.Format("2006-01-02")
}

// ──────────────────────────────────────────────────────────────────────────────
// GET /classes
// ──────────────────────────────────────────────────────────────────────────────

// classListItem is the JSON shape for each class in the GET /classes response.
type classListItem struct {
	ID              uuid.UUID     `json:"id"`
	Name            string        `json:"name"`
	EducationLevel  string        `json:"education_level"`
	GradeNumber     int16         `json:"grade_number"`
	MaxStudents     *int16        `json:"max_students"`
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

	// Step 1b: Retrieve the transaction-scoped Queries from context so that
	// all database calls in this handler use the RLS-enabled transaction.
	queries := auth.GetQueries(r.Context())
	if queries == nil {
		httputil.InternalError(w)
		return
	}

	// Step 2: Get the current school year. Classes are always scoped to a school year.
	// If the school has not configured a current school year, we return an empty list
	// rather than an error, because this is a valid (albeit incomplete) configuration.
	schoolYear, err := queries.GetCurrentSchoolYear(r.Context())
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
		rows, err := queries.ListClassesByTeacher(r.Context(), generated.ListClassesByTeacherParams{
			TeacherID:    userID,
			SchoolYearID: schoolYear.ID,
		})
		if err != nil {
			h.logger.Error("failed to list classes for teacher", "error", err, "teacher_id", userID)
			httputil.InternalError(w)
			return
		}
		items = make([]classListItem, len(rows))
		for i := range rows {
			items[i] = classListItem{
				ID:             rows[i].ID,
				Name:           rows[i].Name,
				EducationLevel: string(rows[i].EducationLevel),
				GradeNumber:    rows[i].GradeNumber,
				MaxStudents:    rows[i].MaxStudents,
			}
			// Include homeroom teacher info if the class has one assigned.
			if rows[i].HomeroomTeacherID.Valid {
				items[i].HomeroomTeacher = &teacherBrief{
					ID:        rows[i].HomeroomTeacherID.Bytes,
					FirstName: ptrOrEmpty(rows[i].HomeroomFirstName),
					LastName:  ptrOrEmpty(rows[i].HomeroomLastName),
				}
			}
		}
	} else {
		// For admins, secretaries, parents, students: query all classes in the school year.
		rows, err := queries.ListClassesBySchoolYear(r.Context(), schoolYear.ID)
		if err != nil {
			h.logger.Error("failed to list classes", "error", err)
			httputil.InternalError(w)
			return
		}
		items = make([]classListItem, len(rows))
		for i := range rows {
			items[i] = classListItem{
				ID:             rows[i].ID,
				Name:           rows[i].Name,
				EducationLevel: string(rows[i].EducationLevel),
				GradeNumber:    rows[i].GradeNumber,
				MaxStudents:    rows[i].MaxStudents,
			}
			if rows[i].HomeroomTeacherID.Valid {
				items[i].HomeroomTeacher = &teacherBrief{
					ID:        rows[i].HomeroomTeacherID.Bytes,
					FirstName: ptrOrEmpty(rows[i].HomeroomFirstName),
					LastName:  ptrOrEmpty(rows[i].HomeroomLastName),
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

	// Step 1b: Retrieve the transaction-scoped Queries from context so that
	// all database calls in this handler use the RLS-enabled transaction.
	queries := auth.GetQueries(r.Context())
	if queries == nil {
		httputil.InternalError(w)
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
	cls, err := queries.GetClassByID(r.Context(), classID)
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
	studentRows, err := queries.ListStudentsByClass(r.Context(), classID)
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

	// Step 1b: Retrieve the transaction-scoped Queries from context so that
	// all database calls in this handler use the RLS-enabled transaction.
	queries := auth.GetQueries(r.Context())
	if queries == nil {
		httputil.InternalError(w)
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
	rows, err := queries.ListTeachersByClass(r.Context(), classID)
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

	// Step 1b: Retrieve the transaction-scoped Queries from context so that
	// all database calls in this handler use the RLS-enabled transaction.
	queries := auth.GetQueries(r.Context())
	if queries == nil {
		httputil.InternalError(w)
		return
	}

	// Step 2: Fetch all active subjects from the database.
	rows, err := queries.ListSubjectsBySchool(r.Context())
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
// POST /classes
// ──────────────────────────────────────────────────────────────────────────────

// createClassRequest is the JSON body expected by POST /classes.
// Only admin users may call this endpoint (enforced via RequireRole middleware
// in main.go — the handler itself does not re-check the role).
type createClassRequest struct {
	// SchoolYearID is the UUID of the school year this class belongs to.
	// Required. The front-end should send the current school year's ID, which
	// can be obtained via GET /schools/current/year (or the handler can auto-
	// resolve from GetCurrentSchoolYear if omitted — see Step 2 below).
	SchoolYearID string `json:"school_year_id"`

	// Name is the class label, e.g. "5A" or "IX B".
	// Required and must be non-blank (trimmed).
	Name string `json:"name"`

	// EducationLevel scopes the class to a school level.
	// Must be one of "primary", "middle", or "high".
	EducationLevel string `json:"education_level"`

	// GradeNumber is the Romanian clasă/an: 1–4 for primary, 5–8 for middle,
	// 9–12 for high school.
	// Required and must be between 1 and 12.
	GradeNumber int16 `json:"grade_number"`

	// HomeroomTeacherID (diriginte) is optional. Pass null or omit to leave
	// the homeroom teacher unassigned at creation time.
	HomeroomTeacherID *string `json:"homeroom_teacher_id"`

	// MaxStudents is the class capacity. Optional — if omitted the database
	// default of 30 is used.
	MaxStudents *int16 `json:"max_students"`
}

// classResponse is the JSON shape returned after creating or updating a class.
// It mirrors the columns of the classes table but omits school_id (implicit
// from the JWT/RLS context) and internal audit columns the client does not need.
type classResponse struct {
	ID                uuid.UUID  `json:"id"`
	SchoolYearID      uuid.UUID  `json:"school_year_id"`
	Name              string     `json:"name"`
	EducationLevel    string     `json:"education_level"`
	GradeNumber       int16      `json:"grade_number"`
	HomeroomTeacherID *uuid.UUID `json:"homeroom_teacher_id"`
	MaxStudents       *int16     `json:"max_students"`
}

// CreateClass handles POST /classes.
//
// Creates a new class scoped to the current tenant school. The school_id is
// set automatically by the PostgreSQL function current_school_id() via RLS —
// the caller does not provide it.
//
// This endpoint is restricted to the "admin" role. The RequireRole("admin")
// middleware applied in main.go enforces this before the handler runs.
//
// Request body (JSON):
//
//	{
//	  "school_year_id":      "uuid",     // required
//	  "name":                "5A",       // required, non-empty
//	  "education_level":     "middle",   // required: primary | middle | high
//	  "grade_number":        5,          // required: 1-12
//	  "homeroom_teacher_id": "uuid",     // optional, null to leave unassigned
//	  "max_students":        30          // optional, defaults to 30
//	}
//
// Possible responses:
//   - 201 Created:              { "data": { class fields } }
//   - 400 Bad Request:         validation failure
//   - 401 Unauthorized:        auth context missing
//   - 409 Conflict:            duplicate class name in the same school year
//   - 500 Internal Server Error: database failure
func (h *Handler) CreateClass(w http.ResponseWriter, r *http.Request) {
	// Step 1: Verify authentication — ensure the auth middleware ran.
	_, err := auth.GetSchoolID(r.Context())
	if err != nil {
		httputil.Unauthorized(w, "Authentication required")
		return
	}

	// Step 1b: Retrieve the transaction-scoped Queries bound to the RLS context.
	// All SQL executed through this object is automatically filtered to the
	// current tenant's school_id via PostgreSQL Row-Level Security.
	queries := auth.GetQueries(r.Context())
	if queries == nil {
		httputil.InternalError(w)
		return
	}

	// Step 2: Decode the JSON request body.
	var req createClassRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		// Malformed JSON — return a descriptive 400 so the caller can fix their payload.
		httputil.BadRequest(w, "INVALID_JSON", "Request body must be valid JSON")
		return
	}

	// Step 3: Validate required fields.

	// name must be present and non-whitespace.
	if strings.TrimSpace(req.Name) == "" {
		httputil.BadRequest(w, "MISSING_FIELD", "name is required and must not be blank")
		return
	}

	// education_level must be one of the three allowed enum values.
	// Validating early gives a friendlier error than a raw DB enum rejection.
	if !allowedEducationLevels[req.EducationLevel] {
		httputil.BadRequest(w, "INVALID_EDUCATION_LEVEL",
			"education_level must be one of: primary, middle, high")
		return
	}

	// grade_number must be between 1 and 12 (Romanian school system).
	// Primary: 1-4 (clasa pregătitoare is grade 0 in some systems, but here 1-4),
	// Middle: 5-8, High: 9-12.
	if req.GradeNumber < 1 || req.GradeNumber > 12 {
		httputil.BadRequest(w, "INVALID_GRADE_NUMBER",
			"grade_number must be between 1 and 12")
		return
	}

	// school_year_id must be a valid UUID.
	schoolYearID, err := uuid.Parse(req.SchoolYearID)
	if err != nil {
		httputil.BadRequest(w, "INVALID_SCHOOL_YEAR_ID",
			"school_year_id must be a valid UUID")
		return
	}

	// Step 4: Build the homeroom_teacher_id parameter.
	// pgtype.UUID is pgx's nullable UUID type. We set Valid=false when the
	// caller omits the field (null teacher assignment at creation time).
	var homeroomTeacherID pgtype.UUID
	if req.HomeroomTeacherID != nil {
		parsed, err := uuid.Parse(*req.HomeroomTeacherID)
		if err != nil {
			httputil.BadRequest(w, "INVALID_TEACHER_ID",
				"homeroom_teacher_id must be a valid UUID")
			return
		}
		homeroomTeacherID = pgtype.UUID{Bytes: parsed, Valid: true}
	}

	// Step 5: Insert the class into the database.
	cls, err := queries.CreateClass(r.Context(), generated.CreateClassParams{
		SchoolYearID:      schoolYearID,
		Name:              req.Name,
		EducationLevel:    generated.EducationLevel(req.EducationLevel),
		GradeNumber:       req.GradeNumber,
		HomeroomTeacherID: homeroomTeacherID,
		MaxStudents:       req.MaxStudents,
	})
	if err != nil {
		// Check for a PostgreSQL unique constraint violation (error code 23505).
		// The classes table has UNIQUE(school_id, school_year_id, name), so inserting
		// a class with the same name in the same school year triggers this error.
		// We return 409 Conflict so the caller knows to use a different name.
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			httputil.Error(w, http.StatusConflict, "DUPLICATE_CLASS",
				"A class with this name already exists for the selected school year")
			return
		}

		// Any other DB error is unexpected — log and return generic 500.
		// Never expose raw database error messages to the caller.
		h.logger.Error("create_class: database insert failed",
			"error", err,
			"name", req.Name,
			"education_level", req.EducationLevel,
			"school_year_id", schoolYearID,
		)
		httputil.InternalError(w)
		return
	}

	// Step 6: Return 201 Created with the newly created class in the data envelope.
	httputil.Created(w, classResponseFromRow(&cls))
}

// ──────────────────────────────────────────────────────────────────────────────
// PUT /classes/{classId}
// ──────────────────────────────────────────────────────────────────────────────

// updateClassRequest is the JSON body expected by PUT /classes/{classId}.
// All fields are optional — omitting a field keeps the current value on the row.
// Setting homeroom_teacher_id to null explicitly clears the assignment.
type updateClassRequest struct {
	// Name is the new class label, e.g. "5B". Optional — omit to keep current name.
	Name *string `json:"name"`

	// HomeroomTeacherID is the new diriginte UUID. Pass null to unassign the
	// current teacher. Omitting the field also clears it (JSON null and absent
	// field both decode to nil *string — callers must always send the value
	// explicitly if they want to keep the current teacher).
	HomeroomTeacherID *string `json:"homeroom_teacher_id"`

	// MaxStudents is the new class capacity. Optional — omit to keep current value.
	MaxStudents *int16 `json:"max_students"`
}

// UpdateClass handles PUT /classes/{classId}.
//
// Updates mutable fields of an existing class. The class must belong to the
// current tenant (enforced by RLS). The handler reads the current class row
// first so that fields not included in the request body retain their values.
//
// This endpoint is restricted to the "admin" role (enforced in main.go via
// RequireRole middleware).
//
// Request body (JSON) — all fields optional:
//
//	{
//	  "name":                "5B",     // optional — omit to keep current
//	  "homeroom_teacher_id": "uuid",   // optional — null clears the assignment
//	  "max_students":        32        // optional — omit to keep current
//	}
//
// Possible responses:
//   - 200 OK:                  { "data": { updated class fields } }
//   - 400 Bad Request:         invalid classId format or validation failure
//   - 401 Unauthorized:        auth context missing
//   - 404 Not Found:           class does not exist (or belongs to another tenant)
//   - 500 Internal Server Error: database failure
func (h *Handler) UpdateClass(w http.ResponseWriter, r *http.Request) {
	// Step 1: Verify authentication.
	_, err := auth.GetSchoolID(r.Context())
	if err != nil {
		httputil.Unauthorized(w, "Authentication required")
		return
	}

	// Step 1b: Retrieve the transaction-scoped Queries from context.
	queries := auth.GetQueries(r.Context())
	if queries == nil {
		httputil.InternalError(w)
		return
	}

	// Step 2: Parse the class ID from the URL path.
	// chi.URLParam extracts the {classId} segment registered in main.go.
	classIDStr := chi.URLParam(r, "classId")
	classID, err := uuid.Parse(classIDStr)
	if err != nil {
		httputil.BadRequest(w, "INVALID_ID", "classId must be a valid UUID")
		return
	}

	// Step 3: Decode the JSON request body.
	var req updateClassRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httputil.BadRequest(w, "INVALID_JSON", "Request body must be valid JSON")
		return
	}

	// Step 4: Fetch the current class row so we can apply partial updates.
	// We need the current name and max_students values for COALESCE-style logic:
	// if the caller omits a field, we keep the existing value.
	current, err := queries.GetClassByID(r.Context(), classID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			httputil.NotFound(w, "Class not found")
			return
		}
		h.logger.Error("update_class: get current class failed", "error", err, "class_id", classID)
		httputil.InternalError(w)
		return
	}

	// Step 5: Determine the final values for each mutable field.

	// name: use the requested value if provided, otherwise keep the current name.
	// The SQL uses COALESCE($2, name), but since the generated param type is
	// non-nullable string, we resolve the final name here in Go.
	newName := current.Name
	if req.Name != nil {
		if strings.TrimSpace(*req.Name) == "" {
			httputil.BadRequest(w, "INVALID_FIELD", "name must not be blank if provided")
			return
		}
		newName = *req.Name
	}

	// homeroom_teacher_id: consistent with name and max_students — omitting the
	// field preserves the current value. To CLEAR the teacher, send explicit null.
	// This is the behavior Gemini Code Assist recommended for predictable partial updates.
	newTeacherID := current.HomeroomTeacherID // preserve current by default
	if req.HomeroomTeacherID != nil {
		if *req.HomeroomTeacherID == "" {
			// Explicit empty string means "clear the teacher assignment"
			newTeacherID = pgtype.UUID{Valid: false}
		} else {
			parsed, err := uuid.Parse(*req.HomeroomTeacherID)
			if err != nil {
				httputil.BadRequest(w, "INVALID_TEACHER_ID",
					"homeroom_teacher_id must be a valid UUID or empty string to clear")
				return
			}
			newTeacherID = pgtype.UUID{Bytes: parsed, Valid: true}
		}
	}

	// max_students: preserve current if omitted, update if provided.
	newMaxStudents := current.MaxStudents
	if req.MaxStudents != nil {
		newMaxStudents = req.MaxStudents
	}

	// Step 6: Execute the update. All fields are resolved — SQL does direct assignment.
	cls, err := queries.UpdateClass(r.Context(), generated.UpdateClassParams{
		ID:                classID,
		Name:              newName,
		HomeroomTeacherID: newTeacherID,
		MaxStudents:       newMaxStudents,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			// Should not happen because we fetched the row in Step 4, but guard
			// against a race condition where the class was deleted between reads.
			httputil.NotFound(w, "Class not found")
			return
		}
		h.logger.Error("update_class: database update failed",
			"error", err,
			"class_id", classID,
		)
		httputil.InternalError(w)
		return
	}

	// Step 7: Return 200 OK with the updated class in the data envelope.
	httputil.Success(w, classResponseFromRow(&cls))
}

// ──────────────────────────────────────────────────────────────────────────────
// POST /subjects
// ──────────────────────────────────────────────────────────────────────────────

// createSubjectRequest is the JSON body expected by POST /subjects.
// Only admin users may call this endpoint (enforced via RequireRole middleware
// in main.go — the handler itself does not re-check the role).
type createSubjectRequest struct {
	// Name is the full subject name in Romanian, e.g. "Matematică".
	// Required — the request is rejected with 400 if this is missing or blank.
	Name string `json:"name"`

	// ShortName is the abbreviated subject code, e.g. "MAT".
	// Optional — if omitted the database stores NULL.
	ShortName *string `json:"short_name"`

	// EducationLevel scopes the subject to a school level.
	// Must be one of "primary", "middle", or "high".
	EducationLevel string `json:"education_level"`

	// HasThesis indicates whether a semester thesis (teză) is recorded for
	// this subject. Defaults to false when not supplied in the request body.
	HasThesis bool `json:"has_thesis"`
}

// allowedEducationLevels is the set of valid values for the education_level field.
// It mirrors the PostgreSQL education_level enum defined in the schema.
var allowedEducationLevels = map[string]bool{
	"primary": true,
	"middle":  true,
	"high":    true,
}

// CreateSubject handles POST /subjects.
//
// Creates a new subject scoped to the current tenant school. The school_id is
// set automatically by the PostgreSQL function current_school_id() via RLS —
// the caller does not provide it.
//
// This endpoint is restricted to the "admin" role. The RequireRole("admin")
// middleware applied in main.go enforces this before the handler runs.
//
// Request body (JSON):
//
//	{
//	  "name":            "Matematică",   // required, non-empty
//	  "short_name":      "MAT",          // optional
//	  "education_level": "middle",       // required: primary | middle | high
//	  "has_thesis":      true            // optional, defaults to false
//	}
//
// Possible responses:
//   - 201 Created:              { "data": { subject fields } }
//   - 400 Bad Request:         validation failure (missing name, invalid level)
//   - 401 Unauthorized:        auth context missing
//   - 409 Conflict:            duplicate name+education_level for this school
//   - 500 Internal Server Error: database failure
func (h *Handler) CreateSubject(w http.ResponseWriter, r *http.Request) {
	// Step 1: Verify authentication — ensure the auth middleware ran.
	// GetSchoolID reads the school UUID from JWT claims in the request context.
	// If it is absent, the middleware chain was misconfigured.
	_, err := auth.GetSchoolID(r.Context())
	if err != nil {
		httputil.Unauthorized(w, "Authentication required")
		return
	}

	// Step 1b: Retrieve the transaction-scoped Queries bound to the RLS context.
	// All SQL executed through this object is automatically filtered to the
	// current tenant's school_id via PostgreSQL Row-Level Security.
	queries := auth.GetQueries(r.Context())
	if queries == nil {
		httputil.InternalError(w)
		return
	}

	// Step 2: Decode the JSON request body into our request struct.
	// json.NewDecoder streams the body — more efficient than ioutil.ReadAll for
	// potentially large payloads (though subject payloads are always tiny).
	var req createSubjectRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		// Malformed JSON (e.g. missing closing brace, wrong value type).
		// Return a descriptive 400 so the caller knows to fix their payload.
		httputil.BadRequest(w, "INVALID_JSON", "Request body must be valid JSON")
		return
	}

	// Step 3: Validate required fields.

	// name must be present and non-whitespace.
	// strings.TrimSpace catches payloads like {"name": "   "} which would
	// create an empty-looking subject row — a common junior mistake.
	if strings.TrimSpace(req.Name) == "" {
		httputil.BadRequest(w, "MISSING_FIELD", "name is required and must not be blank")
		return
	}

	// education_level must be one of the three allowed enum values.
	// The PostgreSQL enum would also enforce this, but validating early gives
	// a friendlier error message instead of a raw DB error string.
	if !allowedEducationLevels[req.EducationLevel] {
		httputil.BadRequest(w, "INVALID_EDUCATION_LEVEL",
			"education_level must be one of: primary, middle, high")
		return
	}

	// Step 4: Insert the subject into the database.
	// generated.EducationLevel is the Go type that maps to the PostgreSQL enum.
	// Casting req.EducationLevel (a plain string) to this type is safe here
	// because we already validated it against allowedEducationLevels above.
	subject, err := queries.CreateSubject(r.Context(), generated.CreateSubjectParams{
		Name:           req.Name,
		ShortName:      req.ShortName,
		EducationLevel: generated.EducationLevel(req.EducationLevel),
		HasThesis:      req.HasThesis,
	})
	if err != nil {
		// Check for a PostgreSQL unique constraint violation (error code 23505).
		// This happens when the school already has a subject with the same name
		// at the same education level. We return 409 Conflict rather than 500
		// so the caller can display a meaningful message ("subject already exists").
		//
		// pgconn.PgError is the pgx type that wraps PostgreSQL error codes.
		// We use errors.As (not a type switch) because pgx may wrap the error.
		// The Code field (not SQLState method) holds the 5-char SQLSTATE string.
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			httputil.Error(w, http.StatusConflict, "DUPLICATE_SUBJECT",
				"A subject with this name already exists for the selected education level")
			return
		}

		// Any other DB error is unexpected — log it and return a generic 500.
		// Never expose raw database error messages to the caller (security/info-leak).
		h.logger.Error("create_subject: database insert failed",
			"error", err,
			"name", req.Name,
			"education_level", req.EducationLevel,
		)
		httputil.InternalError(w)
		return
	}

	// Step 5: Return 201 Created with the newly created subject in the data envelope.
	// We reuse subjectResponse (the same struct used by ListSubjects) so the
	// shape is consistent between GET /subjects and POST /subjects.
	httputil.Created(w, subjectResponse{
		ID:             subject.ID,
		Name:           subject.Name,
		ShortName:      subject.ShortName,
		EducationLevel: string(subject.EducationLevel),
		HasThesis:      subject.HasThesis,
	})
}

// ──────────────────────────────────────────────────────────────────────────────
// POST /classes/{classId}/enroll
// ──────────────────────────────────────────────────────────────────────────────

// enrollRequest is the JSON body expected by POST /classes/{classId}/enroll.
// Only admin and secretary users may call this endpoint (enforced via
// RequireRole middleware in main.go — the handler itself does not re-check).
type enrollRequest struct {
	// StudentID is the UUID of the student to enrol in the class.
	// Required — the request is rejected with 400 if this is missing or not
	// a valid UUID. The student must already exist as a user in the school.
	StudentID string `json:"student_id"`
}

// enrollmentResponse is the JSON shape returned after a successful enrollment.
// It mirrors the relevant columns of the class_enrollments table, omitting
// school_id (implicit from the JWT/RLS context).
type enrollmentResponse struct {
	ID        uuid.UUID `json:"id"`
	ClassID   uuid.UUID `json:"class_id"`
	StudentID uuid.UUID `json:"student_id"`
}

// EnrollStudent handles POST /classes/{classId}/enroll.
//
// Enrols a student in a class. The school_id is set automatically by
// current_school_id() via RLS — the caller does not provide it. The
// (class_id, student_id) pair must be unique; a second enrolment of the
// same student returns 409 Conflict.
//
// This endpoint is restricted to "admin" and "secretary" roles. The
// RequireRole middleware applied in main.go enforces this before the handler.
//
// Request body (JSON):
//
//	{ "student_id": "uuid" }
//
// Possible responses:
//   - 201 Created:              { "data": { enrollment fields } }
//   - 400 Bad Request:         student_id missing or not a valid UUID
//   - 401 Unauthorized:        auth context missing
//   - 409 Conflict:            student already enrolled in this class
//   - 500 Internal Server Error: database failure
func (h *Handler) EnrollStudent(w http.ResponseWriter, r *http.Request) {
	// Step 1: Verify authentication — ensure the auth middleware ran.
	_, err := auth.GetSchoolID(r.Context())
	if err != nil {
		httputil.Unauthorized(w, "Authentication required")
		return
	}

	// Step 1b: Retrieve the transaction-scoped Queries bound to the RLS context.
	// All SQL executed through this object is automatically filtered to the
	// current tenant's school_id via PostgreSQL Row-Level Security.
	queries := auth.GetQueries(r.Context())
	if queries == nil {
		httputil.InternalError(w)
		return
	}

	// Step 2: Parse the class ID from the URL path parameter.
	// chi.URLParam extracts the {classId} segment registered in main.go.
	classIDStr := chi.URLParam(r, "classId")
	classID, err := uuid.Parse(classIDStr)
	if err != nil {
		httputil.BadRequest(w, "INVALID_ID", "classId must be a valid UUID")
		return
	}

	// Step 3: Decode the JSON request body.
	// We expect exactly one field: student_id (a UUID string).
	var req enrollRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httputil.BadRequest(w, "INVALID_JSON", "Request body must be valid JSON")
		return
	}

	// Step 4: Validate the student_id field.
	// The field is required and must be a well-formed UUID. An empty or
	// missing student_id would produce pgx.ErrNoRows or a FK violation —
	// both worse errors than the early validation we do here.
	if strings.TrimSpace(req.StudentID) == "" {
		httputil.BadRequest(w, "MISSING_FIELD", "student_id is required")
		return
	}
	studentID, err := uuid.Parse(req.StudentID)
	if err != nil {
		httputil.BadRequest(w, "INVALID_STUDENT_ID", "student_id must be a valid UUID")
		return
	}

	// Step 5: Insert the enrollment record into the database.
	// EnrollStudent uses current_school_id() in the INSERT, so school_id is
	// set automatically from the RLS tenant context — we only pass class and student.
	enrollment, err := queries.EnrollStudent(r.Context(), generated.EnrollStudentParams{
		ClassID:   classID,
		StudentID: studentID,
	})
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) {
			switch pgErr.Code {
			case "23505": // unique_violation
				// The (class_id, student_id) pair already exists.
				httputil.Error(w, http.StatusConflict, "DUPLICATE_ENROLLMENT",
					"Student is already enrolled in this class")
				return
			case "23503": // foreign_key_violation
				// The class_id or student_id references a non-existent row.
				// Inspect the constraint name to give a specific error.
				if strings.Contains(pgErr.ConstraintName, "student_id") {
					httputil.BadRequest(w, "STUDENT_NOT_FOUND",
						"The specified student does not exist")
				} else {
					httputil.Error(w, http.StatusNotFound, "CLASS_NOT_FOUND",
						"The specified class does not exist")
				}
				return
			}
		}

		// Any other database error is unexpected — log and return generic 500.
		// We never expose raw DB errors to the caller (security / info-leak risk).
		h.logger.Error("enroll_student: database insert failed",
			"error", err,
			"class_id", classID,
			"student_id", studentID,
		)
		httputil.InternalError(w)
		return
	}

	// Step 6: Return 201 Created with the new enrollment data in the standard envelope.
	httputil.Created(w, enrollmentResponse{
		ID:        enrollment.ID,
		ClassID:   enrollment.ClassID,
		StudentID: enrollment.StudentID,
	})
}

// ──────────────────────────────────────────────────────────────────────────────
// POST /classes/{classId}/teachers
// ──────────────────────────────────────────────────────────────────────────────

// assignTeacherRequest is the JSON body expected by
// POST /classes/{classId}/teachers.
//
// Only admin users may call this endpoint (enforced via RequireRole middleware
// in main.go — the handler itself does not re-check the role).
type assignTeacherRequest struct {
	// SubjectID is the UUID of the subject the teacher will teach in this class.
	// Required — the request is rejected with 400 if this is missing or not a
	// valid UUID.
	SubjectID string `json:"subject_id"`

	// TeacherID is the UUID of the user with role=teacher to assign.
	// Required — the request is rejected with 400 if this is missing or not a
	// valid UUID.
	TeacherID string `json:"teacher_id"`

	// HoursPerWeek is the number of lessons this teacher teaches per week for
	// this class+subject pair. Optional — defaults to 1 if not provided.
	// Must be a positive smallint when provided.
	HoursPerWeek *int16 `json:"hours_per_week"`
}

// assignTeacherResponse is the JSON shape returned after a successful
// teacher-subject assignment. It mirrors the relevant columns of the
// class_subject_teachers table, omitting school_id (implicit from the
// JWT/RLS context).
type assignTeacherResponse struct {
	ID           uuid.UUID `json:"id"`
	ClassID      uuid.UUID `json:"class_id"`
	SubjectID    uuid.UUID `json:"subject_id"`
	TeacherID    uuid.UUID `json:"teacher_id"`
	HoursPerWeek int16     `json:"hours_per_week"`
}

// AssignTeacher handles POST /classes/{classId}/teachers.
//
// Assigns a teacher to a subject in a class. The school_id is set automatically
// by current_school_id() via RLS — the caller does not provide it. The
// (class_id, subject_id, teacher_id) triple must be unique; a second identical
// assignment returns 409 Conflict.
//
// This endpoint is restricted to the "admin" role. The RequireRole middleware
// applied in main.go enforces this before the handler runs.
//
// Request body (JSON):
//
//	{
//	  "subject_id":    "uuid",   // required
//	  "teacher_id":   "uuid",   // required
//	  "hours_per_week": 4       // optional, defaults to 1
//	}
//
// Possible responses:
//   - 201 Created:              { "data": { assignment fields } }
//   - 400 Bad Request:         subject_id or teacher_id missing/invalid UUID,
//                               or teacher/subject FK does not exist
//   - 401 Unauthorized:        auth context missing
//   - 409 Conflict:            same (class, subject, teacher) already assigned
//   - 500 Internal Server Error: database failure
func (h *Handler) AssignTeacher(w http.ResponseWriter, r *http.Request) {
	// Step 1: Verify authentication — ensure the auth middleware ran.
	_, err := auth.GetSchoolID(r.Context())
	if err != nil {
		httputil.Unauthorized(w, "Authentication required")
		return
	}

	// Step 1b: Retrieve the transaction-scoped Queries bound to the RLS context.
	// All SQL executed through this object is automatically filtered to the
	// current tenant's school_id via PostgreSQL Row-Level Security.
	queries := auth.GetQueries(r.Context())
	if queries == nil {
		httputil.InternalError(w)
		return
	}

	// Step 2: Parse the class ID from the URL path parameter.
	// chi.URLParam extracts the {classId} segment registered in main.go.
	classIDStr := chi.URLParam(r, "classId")
	classID, err := uuid.Parse(classIDStr)
	if err != nil {
		httputil.BadRequest(w, "INVALID_ID", "classId must be a valid UUID")
		return
	}

	// Step 3: Decode the JSON request body.
	// We expect subject_id and teacher_id (required), and hours_per_week (optional).
	var req assignTeacherRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httputil.BadRequest(w, "INVALID_JSON", "Request body must be valid JSON")
		return
	}

	// Step 4: Validate required fields.

	// subject_id must be present and a well-formed UUID.
	if strings.TrimSpace(req.SubjectID) == "" {
		httputil.BadRequest(w, "MISSING_FIELD", "subject_id is required")
		return
	}
	subjectID, err := uuid.Parse(req.SubjectID)
	if err != nil {
		httputil.BadRequest(w, "INVALID_SUBJECT_ID", "subject_id must be a valid UUID")
		return
	}

	// teacher_id must be present and a well-formed UUID.
	if strings.TrimSpace(req.TeacherID) == "" {
		httputil.BadRequest(w, "MISSING_FIELD", "teacher_id is required")
		return
	}
	teacherID, err := uuid.Parse(req.TeacherID)
	if err != nil {
		httputil.BadRequest(w, "INVALID_TEACHER_ID", "teacher_id must be a valid UUID")
		return
	}

	// Step 5: Resolve the hours_per_week value.
	// If the caller omits the field (nil pointer), default to 1.
	// Romanian schools typically have 1–7 hours per week per subject.
	hoursPerWeek := int16(1)
	if req.HoursPerWeek != nil {
		if *req.HoursPerWeek <= 0 {
			httputil.BadRequest(w, "INVALID_HOURS", "hours_per_week must be a positive number")
			return
		}
		hoursPerWeek = *req.HoursPerWeek
	}

	// Step 6: Insert the assignment record into the database.
	// AssignTeacherToSubject uses current_school_id() in the INSERT, so
	// school_id is set automatically from the RLS tenant context — we only
	// pass class, subject, teacher, and hours.
	assignment, err := queries.AssignTeacherToSubject(r.Context(), generated.AssignTeacherToSubjectParams{
		ClassID:      classID,
		SubjectID:    subjectID,
		TeacherID:    teacherID,
		HoursPerWeek: hoursPerWeek,
	})
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) {
			switch pgErr.Code {
			case "23505": // unique_violation
				// The (class_id, subject_id, teacher_id) triple already exists.
				// Return 409 Conflict so the caller knows the assignment is a duplicate.
				httputil.Error(w, http.StatusConflict, "DUPLICATE_ASSIGNMENT",
					"This teacher is already assigned to the subject in this class")
				return
			case "23503": // foreign_key_violation
				// One of the referenced rows (class, subject, or teacher) does not exist
				// in the current tenant. Inspect the constraint name to give a specific
				// error message that helps the caller identify which ID is invalid.
				switch {
				case strings.Contains(pgErr.ConstraintName, "teacher_id"):
					httputil.BadRequest(w, "TEACHER_NOT_FOUND",
						"The specified teacher does not exist")
				case strings.Contains(pgErr.ConstraintName, "subject_id"):
					httputil.BadRequest(w, "SUBJECT_NOT_FOUND",
						"The specified subject does not exist")
				default:
					// Covers class_id FK violation (the class does not exist).
					httputil.Error(w, http.StatusNotFound, "CLASS_NOT_FOUND",
						"The specified class does not exist")
				}
				return
			}
		}

		// Any other database error is unexpected — log and return generic 500.
		// We never expose raw DB errors to the caller (security / info-leak risk).
		h.logger.Error("assign_teacher: database insert failed",
			"error", err,
			"class_id", classID,
			"subject_id", subjectID,
			"teacher_id", teacherID,
		)
		httputil.InternalError(w)
		return
	}

	// Step 7: Return 201 Created with the new assignment in the standard envelope.
	httputil.Created(w, assignTeacherResponse{
		ID:           assignment.ID,
		ClassID:      assignment.ClassID,
		SubjectID:    assignment.SubjectID,
		TeacherID:    assignment.TeacherID,
		HoursPerWeek: assignment.HoursPerWeek,
	})
}

// ──────────────────────────────────────────────────────────────────────────────
// DELETE /classes/{classId}/enroll/{studentId}
// ──────────────────────────────────────────────────────────────────────────────

// UnenrollStudent handles DELETE /classes/{classId}/enroll/{studentId}.
//
// Removes a student from a class by deleting the class_enrollments row that
// links them. The operation is idempotent: if the student is not enrolled, the
// handler still returns 204 (no error) because the desired state (not enrolled)
// is already satisfied.
//
// This endpoint is restricted to "admin" and "secretary" roles. The
// RequireRole middleware applied in main.go enforces this before the handler.
//
// Possible responses:
//   - 204 No Content:          enrollment removed (or student was not enrolled)
//   - 400 Bad Request:         classId or studentId is not a valid UUID
//   - 401 Unauthorized:        auth context missing
//   - 500 Internal Server Error: database failure
func (h *Handler) UnenrollStudent(w http.ResponseWriter, r *http.Request) {
	// Step 1: Verify authentication — ensure the auth middleware ran.
	_, err := auth.GetSchoolID(r.Context())
	if err != nil {
		httputil.Unauthorized(w, "Authentication required")
		return
	}

	// Step 1b: Retrieve the transaction-scoped Queries bound to the RLS context.
	queries := auth.GetQueries(r.Context())
	if queries == nil {
		httputil.InternalError(w)
		return
	}

	// Step 2: Parse the class ID from the URL path.
	classIDStr := chi.URLParam(r, "classId")
	classID, err := uuid.Parse(classIDStr)
	if err != nil {
		httputil.BadRequest(w, "INVALID_ID", "classId must be a valid UUID")
		return
	}

	// Step 3: Parse the student ID from the URL path.
	// chi captures {studentId} from the registered DELETE route.
	studentIDStr := chi.URLParam(r, "studentId")
	studentID, err := uuid.Parse(studentIDStr)
	if err != nil {
		httputil.BadRequest(w, "INVALID_STUDENT_ID", "studentId must be a valid UUID")
		return
	}

	// Step 4: Delete the enrollment record.
	// UnenrollStudent is an :exec query — it returns only an error, not a row.
	// We do NOT check how many rows were affected: if the enrollment did not
	// exist, the DELETE is a no-op and we still return 204. This idempotent
	// behavior is standard for DELETE endpoints (RFC 9110 §9.3.5).
	if err := queries.UnenrollStudent(r.Context(), generated.UnenrollStudentParams{
		ClassID:   classID,
		StudentID: studentID,
	}); err != nil {
		h.logger.Error("unenroll_student: database delete failed",
			"error", err,
			"class_id", classID,
			"student_id", studentID,
		)
		httputil.InternalError(w)
		return
	}

	// Step 5: Return 204 No Content — no response body for DELETE.
	w.WriteHeader(http.StatusNoContent)
}

// ──────────────────────────────────────────────────────────────────────────────
// Helpers
// ──────────────────────────────────────────────────────────────────────────────

// classResponseFromRow converts a generated.Class database row into the
// classResponse JSON shape used by both POST /classes and PUT /classes/{classId}.
//
// The parameter is passed as a pointer to avoid copying the 160-byte struct on
// every call (gocritic hugeParam). The caller always has the value on the stack
// so taking its address is safe and zero-allocation.
//
// homeroom_teacher_id is stored in the database as a nullable UUID (pgtype.UUID).
// We convert it to a *uuid.UUID pointer: non-nil when assigned, nil when unset.
// This keeps the JSON output clean ("homeroom_teacher_id": null vs. absent key).
func classResponseFromRow(cls *generated.Class) classResponse {
	resp := classResponse{
		ID:             cls.ID,
		SchoolYearID:   cls.SchoolYearID,
		Name:           cls.Name,
		EducationLevel: string(cls.EducationLevel),
		GradeNumber:    cls.GradeNumber,
		MaxStudents:    cls.MaxStudents,
	}
	// Only set the pointer when the teacher is actually assigned.
	// pgtype.UUID.Valid is false when the column value is SQL NULL.
	if cls.HomeroomTeacherID.Valid {
		id := uuid.UUID(cls.HomeroomTeacherID.Bytes)
		resp.HomeroomTeacherID = &id
	}
	return resp
}

// ptrOrEmpty safely dereferences a *string pointer.
// Returns the string value if the pointer is non-nil, or an empty string if nil.
// This is used when mapping nullable database columns to non-nullable JSON fields.
func ptrOrEmpty(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

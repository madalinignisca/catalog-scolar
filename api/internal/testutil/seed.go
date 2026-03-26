// Package testutil — seed.go provides factory functions that insert realistic
// test data into the database. Each function is self-contained: it acquires a
// connection, sets the RLS tenant context when needed, inserts rows via raw SQL,
// and returns the generated UUIDs so that the calling test can reference them.
//
// WHY raw SQL instead of sqlc-generated code?
// The sqlc package lives under db/sqlc/, which imports pgx types that would
// create a circular dependency (testutil → sqlc → testutil). By using plain
// INSERT statements we keep testutil dependency-free and easy to maintain.
//
// UUID STRATEGY — deterministic test IDs:
// All UUIDs are generated with uuid.NewSHA1(uuid.NameSpaceURL, []byte(seed)).
// This produces the same UUID every time for the same seed string, which makes
// debugging easier: if a test fails you can grep the logs for a predictable ID
// instead of chasing a random one. The seed string always starts with
// "catalogro-test-" followed by a human-readable suffix (e.g. "school-1").
package testutil

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// ---------------------------------------------------------------------------
// Helper: deterministic UUID generator
// ---------------------------------------------------------------------------

// DeterministicID produces a deterministic V5 UUID by hashing the given name
// under the URL namespace. The same name always yields the same UUID, making
// test data reproducible across runs.
//
// Exported so that test files in other packages can generate IDs that match
// the ones created by seed helpers (e.g., "school-year-1" for the first school year).
//
// Example:
//
//	DeterministicID("school-1") → always the same UUID
//	DeterministicID("school-2") → a different UUID, but also always the same
func DeterministicID(name string) uuid.UUID {
	return uuid.NewSHA1(uuid.NameSpaceURL, []byte("catalogro-test-"+name))
}

// deterministicID is the package-internal alias for DeterministicID.
// Kept for backwards compatibility with existing seed code.
func deterministicID(name string) uuid.UUID {
	return DeterministicID(name)
}

// ---------------------------------------------------------------------------
// SeedSchools
// ---------------------------------------------------------------------------

// SeedSchools inserts the minimal reference data that almost every integration
// test needs: two districts and two schools (one per district), plus one
// school year for each school.
//
// Table-level RLS notes:
//   - districts: NO RLS — public reference data, no school_id column.
//   - schools:   NO RLS — the schools table itself is the tenant root.
//   - school_years: HAS RLS — requires tenant context (school_id).
//
// The function acquires its own connection from the pool, sets the tenant
// context where necessary, and releases the connection before returning.
//
// Returns the two school UUIDs so that callers can pass them to SeedUsers,
// SeedClass, and other helpers.
func SeedSchools(t *testing.T, pool *pgxpool.Pool) (school1ID, school2ID uuid.UUID) {
	t.Helper()

	ctx := context.Background()

	// --- Generate deterministic UUIDs for all entities we are about to create.
	district1ID := deterministicID("district-1")
	district2ID := deterministicID("district-2")
	school1ID = deterministicID("school-1")
	school2ID = deterministicID("school-2")
	schoolYear1ID := deterministicID("school-year-1")
	schoolYear2ID := deterministicID("school-year-2")

	// --- Acquire a dedicated connection from the pool.
	// We need a single connection because SET CONFIG (tenant context) is
	// connection-scoped; using pool.Exec would give us a random connection
	// each time, and the tenant setting would be lost between calls.
	conn, err := pool.Acquire(ctx)
	if err != nil {
		t.Fatalf("SeedSchools: acquire connection: %v", err)
	}
	defer conn.Release()

	// ---------------------------------------------------------------
	// 1. Insert districts (no RLS, no school_id)
	// ---------------------------------------------------------------
	// districts columns: id, name, county_code, created_at (has default)
	// We specify id, name, county_code. created_at gets its DEFAULT now().

	_, err = conn.Exec(ctx, // nosemgrep: rls-missing-tenant-context
		`INSERT INTO districts (id, name, county_code)
		VALUES ($1, $2, $3)
		ON CONFLICT (id) DO NOTHING`,
		district1ID, "ISJ București", "B1")
	if err != nil {
		t.Fatalf("SeedSchools: insert district 1: %v", err)
	}

	_, err = conn.Exec(ctx, // nosemgrep: rls-missing-tenant-context
		`INSERT INTO districts (id, name, county_code)
		VALUES ($1, $2, $3)
		ON CONFLICT (id) DO NOTHING`,
		district2ID, "ISJ Cluj", "CJ")
	if err != nil {
		t.Fatalf("SeedSchools: insert district 2: %v", err)
	}

	// ---------------------------------------------------------------
	// 2. Insert schools (no RLS on schools table)
	// ---------------------------------------------------------------
	// schools columns: id, district_id, name, siiir_code, education_levels,
	//   address, city, county, phone, email, config, is_active, created_at, updated_at
	// We provide the required columns plus a few optional ones for realism.
	// education_levels is an array of the education_level enum.

	_, err = conn.Exec(ctx, // nosemgrep: rls-missing-tenant-context
		`INSERT INTO schools (id, district_id, name, siiir_code, education_levels, city, county)
		VALUES ($1, $2, $3, $4, $5::education_level[], $6, $7)
		ON CONFLICT (id) DO NOTHING`,
		school1ID, district1ID, "Liceul Teoretic Test 1", "1100001", "{middle,high}", "București", "București")
	if err != nil {
		t.Fatalf("SeedSchools: insert school 1: %v", err)
	}

	_, err = conn.Exec(ctx, // nosemgrep: rls-missing-tenant-context
		`INSERT INTO schools (id, district_id, name, siiir_code, education_levels, city, county)
		VALUES ($1, $2, $3, $4, $5::education_level[], $6, $7)
		ON CONFLICT (id) DO NOTHING`,
		school2ID, district2ID, "Școala Gimnazială Test 2", "1200002", "{primary,middle}", "Cluj-Napoca", "Cluj")
	if err != nil {
		t.Fatalf("SeedSchools: insert school 2: %v", err)
	}

	// ---------------------------------------------------------------
	// 3. Insert school years (HAS RLS — needs tenant context)
	// ---------------------------------------------------------------
	// school_years columns: id, school_id, label, start_date, end_date,
	//   sem1_start, sem1_end, sem2_start, sem2_end, is_current, created_at
	//
	// We use realistic dates for the 2025-2026 school year in Romania.
	// The semester boundaries follow the typical Romanian school calendar.

	// Set tenant context for school 1 so RLS allows the INSERT.
	SetTenantOnConn(t, conn, school1ID)

	_, err = conn.Exec(ctx, // nosemgrep: rls-missing-tenant-context
		`INSERT INTO school_years (id, school_id, label, start_date, end_date,
			sem1_start, sem1_end, sem2_start, sem2_end, is_current)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
		ON CONFLICT (id) DO NOTHING`,
		schoolYear1ID,
		school1ID,
		"2025-2026",
		time.Date(2025, 9, 8, 0, 0, 0, 0, time.UTC),  // start_date
		time.Date(2026, 6, 19, 0, 0, 0, 0, time.UTC), // end_date
		time.Date(2025, 9, 8, 0, 0, 0, 0, time.UTC),  // sem1_start
		time.Date(2026, 1, 30, 0, 0, 0, 0, time.UTC), // sem1_end
		time.Date(2026, 2, 9, 0, 0, 0, 0, time.UTC),  // sem2_start
		time.Date(2026, 6, 19, 0, 0, 0, 0, time.UTC), // sem2_end
		true, // is_current
	)
	if err != nil {
		t.Fatalf("SeedSchools: insert school year 1: %v", err)
	}

	// Switch tenant context to school 2 for the second school year.
	SetTenantOnConn(t, conn, school2ID)

	_, err = conn.Exec(ctx, // nosemgrep: rls-missing-tenant-context
		`INSERT INTO school_years (id, school_id, label, start_date, end_date,
			sem1_start, sem1_end, sem2_start, sem2_end, is_current)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
		ON CONFLICT (id) DO NOTHING`,
		schoolYear2ID,
		school2ID,
		"2025-2026",
		time.Date(2025, 9, 8, 0, 0, 0, 0, time.UTC),
		time.Date(2026, 6, 19, 0, 0, 0, 0, time.UTC),
		time.Date(2025, 9, 8, 0, 0, 0, 0, time.UTC),
		time.Date(2026, 1, 30, 0, 0, 0, 0, time.UTC),
		time.Date(2026, 2, 9, 0, 0, 0, 0, time.UTC),
		time.Date(2026, 6, 19, 0, 0, 0, 0, time.UTC),
		true,
	)
	if err != nil {
		t.Fatalf("SeedSchools: insert school year 2: %v", err)
	}

	return school1ID, school2ID
}

// ---------------------------------------------------------------------------
// SeedUsers
// ---------------------------------------------------------------------------

// SeedUsers inserts one user for each of the five roles defined in the
// user_role enum: admin, secretary, teacher, parent, and student.
// It also creates a parent_student_links row to connect the parent to the
// student, which is required for realistic parent-access tests.
//
// Prerequisites:
//   - SeedSchools must have been called first so that the given schoolID
//     references a valid row in the schools table (FK constraint).
//
// The users table has RLS enabled, so the function sets the tenant context
// to schoolID before inserting.
//
// Returns a map from role name (string) to user UUID, e.g.:
//
//	map["admin"]     → UUID of the admin user
//	map["secretary"] → UUID of the secretary user
//	map["teacher"]   → UUID of the teacher user
//	map["parent"]    → UUID of the parent user
//	map["student"]   → UUID of the student user
func SeedUsers(t *testing.T, pool *pgxpool.Pool, schoolID uuid.UUID) map[string]uuid.UUID {
	t.Helper()

	ctx := context.Background()

	// --- Generate deterministic UUIDs for each user.
	// The suffix includes the school ID so that calling SeedUsers with
	// different schools produces different user UUIDs (no collisions).
	suffix := schoolID.String()[:8] // first 8 hex chars, enough to differentiate
	adminID := deterministicID("admin-" + suffix)
	secretaryID := deterministicID("secretary-" + suffix)
	teacherID := deterministicID("teacher-" + suffix)
	parentID := deterministicID("parent-" + suffix)
	studentID := deterministicID("student-" + suffix)
	linkID := deterministicID("parent-student-link-" + suffix)

	// --- Acquire a dedicated connection and set tenant context.
	conn, err := pool.Acquire(ctx)
	if err != nil {
		t.Fatalf("SeedUsers: acquire connection: %v", err)
	}
	defer conn.Release()

	// Set the RLS tenant context so that all INSERTs below are visible
	// through the users_tenant and psl_tenant policies.
	SetTenantOnConn(t, conn, schoolID)

	// ---------------------------------------------------------------
	// Insert users — one per role
	// ---------------------------------------------------------------
	// users columns used: id, school_id, role, email, first_name, last_name,
	//   password_hash, is_active, activated_at
	//
	// password_hash is a bcrypt hash of "Test1234!" — not a real secret,
	// just enough to satisfy NOT NULL if tests exercise auth flows.
	// In practice password_hash is nullable, so we use a placeholder.
	//
	// activated_at is set to now() so that users appear as activated by
	// default. Tests that need to verify the activation flow can create
	// their own users without this field.

	// Struct to reduce repetition in the insert loop below.
	type seedUser struct {
		id        uuid.UUID
		role      string
		email     string
		firstName string
		lastName  string
	}

	users := []seedUser{
		{adminID, "admin", "admin@test.catalogro.ro", "Admin", "Testescu"},
		{secretaryID, "secretary", "secretary@test.catalogro.ro", "Secretara", "Testescu"},
		{teacherID, "teacher", "teacher@test.catalogro.ro", "Profesor", "Testescu"},
		{parentID, "parent", "parent@test.catalogro.ro", "Părinte", "Testescu"},
		{studentID, "student", "student@test.catalogro.ro", "Elev", "Testescu"},
	}

	for _, u := range users {
		_, err := conn.Exec(ctx, // nosemgrep: rls-missing-tenant-context
			`INSERT INTO users (id, school_id, role, email, first_name, last_name,
				password_hash, is_active, activated_at)
			VALUES ($1, $2, $3::user_role, $4, $5, $6,
				'$2a$10$abcdefghijklmnopqrstuuABCDEFGHIJKLMNOPQRSTUVWXYZ012',
				true, now())
			ON CONFLICT (id) DO NOTHING`,
			u.id, schoolID, u.role, u.email, u.firstName, u.lastName)
		if err != nil {
			t.Fatalf("SeedUsers: insert user %s (%s): %v", u.role, u.email, err)
		}
	}

	// ---------------------------------------------------------------
	// Link the parent to the student
	// ---------------------------------------------------------------
	// parent_student_links columns: id, school_id, parent_id, student_id,
	//   relationship, is_primary, created_at
	// This link is needed for tests that verify parent-scoped data access
	// (e.g., "a parent can only see grades for their linked children").

	_, err = conn.Exec(ctx, // nosemgrep: rls-missing-tenant-context
		`INSERT INTO parent_student_links (id, school_id, parent_id, student_id)
		VALUES ($1, $2, $3, $4)
		ON CONFLICT (parent_id, student_id) DO NOTHING`,
		linkID, schoolID, parentID, studentID)
	if err != nil {
		t.Fatalf("SeedUsers: insert parent-student link: %v", err)
	}

	// Build and return the role→UUID map.
	return map[string]uuid.UUID{
		"admin":     adminID,
		"secretary": secretaryID,
		"teacher":   teacherID,
		"parent":    parentID,
		"student":   studentID,
	}
}

// ---------------------------------------------------------------------------
// SeedClass
// ---------------------------------------------------------------------------

// SeedClass creates a class with all the associated objects needed for a
// realistic catalog test scenario:
//   - A class (e.g. "9A") assigned to the given teacher as homeroom teacher.
//   - A subject (e.g. "Matematică") for middle/high school.
//   - A class enrollment linking the student from SeedUsers to the class.
//   - A teacher assignment (class_subject_teachers) linking the teacher to
//     the subject in this class.
//
// Prerequisites:
//   - SeedSchools must have been called (the school and school_year must exist).
//   - SeedUsers must have been called for the same schoolID (teacher and student
//     must exist).
//
// Parameters:
//   - schoolID:  the school under which to create the class.
//   - teacherID: the teacher who will be homeroom teacher AND subject teacher.
//
// All tables involved (classes, subjects, class_enrollments,
// class_subject_teachers) have RLS enabled, so the function sets the tenant
// context before inserting.
//
// Returns the UUID of the newly created class.
func SeedClass(t *testing.T, pool *pgxpool.Pool, schoolID, teacherID uuid.UUID) uuid.UUID {
	t.Helper()

	ctx := context.Background()

	// --- Generate deterministic UUIDs.
	suffix := schoolID.String()[:8]
	classID := deterministicID("class-" + suffix)
	subjectID := deterministicID("subject-" + suffix)
	enrollmentID := deterministicID("enrollment-" + suffix)
	assignmentID := deterministicID("assignment-" + suffix)

	// The school year ID must match the one created by SeedSchools for this
	// school. We use the same deterministic seed to reconstruct it.
	// SeedSchools creates "school-year-1" for the first school and
	// "school-year-2" for the second. We look up the school year by school_id
	// to stay robust regardless of which school is passed.
	schoolYearID := deterministicID("school-year-1")
	// If this is school-2, recalculate. We check by comparing to the known
	// school-1 deterministic ID.
	school1ID := deterministicID("school-1")
	if schoolID != school1ID {
		schoolYearID = deterministicID("school-year-2")
	}

	// We also need a student ID for the enrollment. Reconstruct it the same
	// way SeedUsers does (using the first 8 hex chars of the school UUID).
	studentID := deterministicID("student-" + suffix)

	// --- Acquire a dedicated connection and set tenant context.
	conn, err := pool.Acquire(ctx)
	if err != nil {
		t.Fatalf("SeedClass: acquire connection: %v", err)
	}
	defer conn.Release()

	// Set tenant context — all tables below have RLS.
	SetTenantOnConn(t, conn, schoolID)

	// ---------------------------------------------------------------
	// 1. Insert the class
	// ---------------------------------------------------------------
	// classes columns: id, school_id, school_year_id, name, education_level,
	//   grade_number, homeroom_teacher_id, max_students, created_at, updated_at
	// We create a 9th-grade high school class ("9A") as a realistic default.

	_, err = conn.Exec(ctx, // nosemgrep: rls-missing-tenant-context
		`INSERT INTO classes (id, school_id, school_year_id, name, education_level,
			grade_number, homeroom_teacher_id)
		VALUES ($1, $2, $3, $4, $5::education_level, $6, $7)
		ON CONFLICT (id) DO NOTHING`,
		classID, schoolID, schoolYearID, "9A", "high", 9, teacherID)
	if err != nil {
		t.Fatalf("SeedClass: insert class: %v", err)
	}

	// ---------------------------------------------------------------
	// 2. Insert a subject
	// ---------------------------------------------------------------
	// subjects columns: id, school_id, name, short_name, education_level,
	//   has_thesis, is_active, created_at
	// "Matematică" (Mathematics) is a universal subject across all levels.

	_, err = conn.Exec(ctx, // nosemgrep: rls-missing-tenant-context
		`INSERT INTO subjects (id, school_id, name, short_name, education_level, has_thesis)
		VALUES ($1, $2, $3, $4, $5::education_level, $6)
		ON CONFLICT (id) DO NOTHING`,
		subjectID, schoolID, "Matematică", "MAT", "high", true)
	if err != nil {
		t.Fatalf("SeedClass: insert subject: %v", err)
	}

	// ---------------------------------------------------------------
	// 3. Enroll the student in the class
	// ---------------------------------------------------------------
	// class_enrollments columns: id, school_id, class_id, student_id,
	//   enrolled_at (DATE, defaults to CURRENT_DATE), withdrawn_at, created_at

	_, err = conn.Exec(ctx, // nosemgrep: rls-missing-tenant-context
		`INSERT INTO class_enrollments (id, school_id, class_id, student_id)
		VALUES ($1, $2, $3, $4)
		ON CONFLICT (id) DO NOTHING`,
		enrollmentID, schoolID, classID, studentID)
	if err != nil {
		t.Fatalf("SeedClass: insert class enrollment: %v", err)
	}

	// ---------------------------------------------------------------
	// 4. Assign the teacher to the subject in this class
	// ---------------------------------------------------------------
	// class_subject_teachers columns: id, school_id, class_id, subject_id,
	//   teacher_id, hours_per_week (defaults to 1), created_at

	_, err = conn.Exec(ctx, // nosemgrep: rls-missing-tenant-context
		`INSERT INTO class_subject_teachers (id, school_id, class_id, subject_id, teacher_id)
		VALUES ($1, $2, $3, $4, $5)
		ON CONFLICT (id) DO NOTHING`,
		assignmentID, schoolID, classID, subjectID, teacherID)
	if err != nil {
		t.Fatalf("SeedClass: insert teacher assignment: %v", err)
	}

	return classID
}

// ---------------------------------------------------------------------------
// SeedPrimaryClass
// ---------------------------------------------------------------------------

// PrimaryClassResult holds the IDs created by SeedPrimaryClass so that tests
// can reference the exact entities (class, subject, students, teacher assignment).
type PrimaryClassResult struct {
	ClassID    uuid.UUID
	SubjectID  uuid.UUID
	StudentID  uuid.UUID // First student (from SeedUsers).
	Student2ID uuid.UUID // Second student (created by SeedPrimaryClass).
}

// SeedPrimaryClass creates a primary-education class (grade 2, "2A") with a
// subject ("Comunicare în limba română", CLR) and enrolls the test student.
// The teacher is assigned to teach CLR in this class.
//
// This is specifically designed for testing descriptive evaluations, which
// are only used in primary school (classes P-IV) in the Romanian system.
//
// Prerequisites: SeedSchools and SeedUsers must have been called first for the
// same schoolID. The teacherID must be a valid user created by SeedUsers.
//
// Returns a PrimaryClassResult with the IDs of all created entities.
func SeedPrimaryClass(t *testing.T, pool *pgxpool.Pool, schoolID, teacherID uuid.UUID) PrimaryClassResult {
	t.Helper()

	ctx := context.Background()

	// --- Generate deterministic UUIDs for primary class entities.
	// We use a "primary-" prefix to avoid collision with SeedClass which uses
	// a generic "class-" prefix.
	suffix := schoolID.String()[:8]
	classID := deterministicID("primary-class-" + suffix)
	subjectID := deterministicID("primary-subject-" + suffix)
	enrollmentID := deterministicID("primary-enrollment-" + suffix)
	assignmentID := deterministicID("primary-assignment-" + suffix)
	student2ID := deterministicID("primary-student2-" + suffix)
	enrollment2ID := deterministicID("primary-enrollment2-" + suffix)

	// The school year ID must match the one created by SeedSchools.
	schoolYearID := deterministicID("school-year-1")
	school1ID := deterministicID("school-1")
	if schoolID != school1ID {
		schoolYearID = deterministicID("school-year-2")
	}

	// Reuse the first student ID from SeedUsers.
	studentID := deterministicID("student-" + suffix)

	// --- Acquire a dedicated connection and set tenant context.
	conn, err := pool.Acquire(ctx)
	if err != nil {
		t.Fatalf("SeedPrimaryClass: acquire connection: %v", err)
	}
	defer conn.Release()

	SetTenantOnConn(t, conn, schoolID)

	// ---------------------------------------------------------------
	// 1. Insert the primary class (grade 2, "2A").
	// ---------------------------------------------------------------
	_, err = conn.Exec(ctx, // nosemgrep: rls-missing-tenant-context
		`INSERT INTO classes (id, school_id, school_year_id, name, education_level,
			grade_number, homeroom_teacher_id)
		VALUES ($1, $2, $3, $4, $5::education_level, $6, $7)
		ON CONFLICT (id) DO NOTHING`,
		classID, schoolID, schoolYearID, "2A", "primary", 2, teacherID)
	if err != nil {
		t.Fatalf("SeedPrimaryClass: insert class: %v", err)
	}

	// ---------------------------------------------------------------
	// 2. Insert a primary-level subject.
	// ---------------------------------------------------------------
	// "Comunicare în limba română" (CLR) is the primary-level Romanian language subject.
	_, err = conn.Exec(ctx, // nosemgrep: rls-missing-tenant-context
		`INSERT INTO subjects (id, school_id, name, short_name, education_level, has_thesis)
		VALUES ($1, $2, $3, $4, $5::education_level, $6)
		ON CONFLICT (id) DO NOTHING`,
		subjectID, schoolID, "Comunicare în limba română", "CLR", "primary", false)
	if err != nil {
		t.Fatalf("SeedPrimaryClass: insert subject: %v", err)
	}

	// ---------------------------------------------------------------
	// 3. Enroll the student in the class.
	// ---------------------------------------------------------------
	_, err = conn.Exec(ctx, // nosemgrep: rls-missing-tenant-context
		`INSERT INTO class_enrollments (id, school_id, class_id, student_id)
		VALUES ($1, $2, $3, $4)
		ON CONFLICT (id) DO NOTHING`,
		enrollmentID, schoolID, classID, studentID)
	if err != nil {
		t.Fatalf("SeedPrimaryClass: insert enrollment: %v", err)
	}

	// ---------------------------------------------------------------
	// 3b. Create a second student and enroll them in the class.
	// ---------------------------------------------------------------
	// This enables tests to verify that ListEvaluations returns ALL students,
	// including those without an evaluation (evaluation: null).
	_, err = conn.Exec(ctx, // nosemgrep: rls-missing-tenant-context
		`INSERT INTO users (id, school_id, role, first_name, last_name, is_active)
		VALUES ($1, $2, 'student'::user_role, $3, $4, true)
		ON CONFLICT (id) DO NOTHING`,
		student2ID, schoolID, "Maria", "Ionescu")
	if err != nil {
		t.Fatalf("SeedPrimaryClass: insert student2: %v", err)
	}

	_, err = conn.Exec(ctx, // nosemgrep: rls-missing-tenant-context
		`INSERT INTO class_enrollments (id, school_id, class_id, student_id)
		VALUES ($1, $2, $3, $4)
		ON CONFLICT (id) DO NOTHING`,
		enrollment2ID, schoolID, classID, student2ID)
	if err != nil {
		t.Fatalf("SeedPrimaryClass: insert enrollment2: %v", err)
	}

	// ---------------------------------------------------------------
	// 4. Assign the teacher to teach CLR in class 2A.
	// ---------------------------------------------------------------
	_, err = conn.Exec(ctx, // nosemgrep: rls-missing-tenant-context
		`INSERT INTO class_subject_teachers (id, school_id, class_id, subject_id, teacher_id)
		VALUES ($1, $2, $3, $4, $5)
		ON CONFLICT (id) DO NOTHING`,
		assignmentID, schoolID, classID, subjectID, teacherID)
	if err != nil {
		t.Fatalf("SeedPrimaryClass: insert teacher assignment: %v", err)
	}

	return PrimaryClassResult{
		ClassID:    classID,
		SubjectID:  subjectID,
		StudentID:  studentID,
		Student2ID: student2ID,
	}
}

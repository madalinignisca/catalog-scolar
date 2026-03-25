-- name: ListClassesBySchoolYear :many
-- Returns all classes for a given school year within the current tenant.
-- Used by the /classes endpoint. Teachers see all classes here; the handler
-- filters by teacher assignment at the application level when needed.
SELECT c.*,
    u.first_name AS homeroom_first_name,
    u.last_name AS homeroom_last_name
FROM classes c
LEFT JOIN users u ON u.id = c.homeroom_teacher_id
WHERE c.school_year_id = $1
ORDER BY c.grade_number, c.name;

-- name: ListClassesByTeacher :many
-- Returns only the classes assigned to a specific teacher for the given school year.
-- A teacher is "assigned" if they have at least one entry in class_subject_teachers.
-- This powers the teacher-filtered view in /classes.
SELECT DISTINCT c.*,
    u.first_name AS homeroom_first_name,
    u.last_name AS homeroom_last_name
FROM classes c
JOIN class_subject_teachers cst ON cst.class_id = c.id
LEFT JOIN users u ON u.id = c.homeroom_teacher_id
WHERE cst.teacher_id = $1
    AND c.school_year_id = $2
ORDER BY c.grade_number, c.name;

-- name: GetClassByID :one
-- Retrieves a single class by its ID. Used by /classes/{classId}.
SELECT c.*,
    u.first_name AS homeroom_first_name,
    u.last_name AS homeroom_last_name
FROM classes c
LEFT JOIN users u ON u.id = c.homeroom_teacher_id
WHERE c.id = $1;

-- name: ListStudentsByClass :many
-- Returns all currently-enrolled students in a class (those without a withdrawal date).
-- Ordered alphabetically by last name, then first name — standard Romanian catalog order.
SELECT u.id, u.first_name, u.last_name, u.email, u.phone,
    ce.enrolled_at
FROM users u
JOIN class_enrollments ce ON ce.student_id = u.id
WHERE ce.class_id = $1
    AND ce.withdrawn_at IS NULL
ORDER BY u.last_name, u.first_name;

-- name: EnrollStudent :one
-- Enrols a student in a class for the current tenant school.
-- The school_id is set automatically by current_school_id() via RLS context.
-- Returns the full enrollment row so the caller can include it in the 201 response.
-- $1 = class_id (UUID of the target class)
-- $2 = student_id (UUID of the student to enrol)
-- A unique constraint on (class_id, student_id) prevents duplicate enrollments;
-- the caller should map the 23505 pgconn error to HTTP 409 Conflict.
INSERT INTO class_enrollments (school_id, class_id, student_id)
VALUES (current_school_id(), $1, $2)
RETURNING *;

-- name: UnenrollStudent :exec
-- Removes a student's enrollment from a class (hard delete — enrollment record is removed).
-- Only affects the exact (class_id, student_id) pair; no other data is changed.
-- The handler should return 204 No Content regardless of whether a row was found,
-- because a DELETE of a non-existent record is idempotent from the caller's perspective.
-- $1 = class_id (UUID of the class)
-- $2 = student_id (UUID of the student to remove)
DELETE FROM class_enrollments
WHERE class_id = $1 AND student_id = $2;

-- name: AssignTeacherToSubject :one
-- Assigns a teacher to a subject in a class for the current tenant school.
-- The school_id is set automatically by current_school_id() via RLS context.
-- Returns the full assignment row so the caller can include it in the 201 response.
-- $1 = class_id   (UUID of the target class)
-- $2 = subject_id (UUID of the subject to teach)
-- $3 = teacher_id (UUID of the teacher to assign)
-- $4 = hours_per_week (smallint, defaults to 1 if the caller does not provide a value)
-- A unique constraint on (class_id, subject_id, teacher_id) prevents duplicate
-- assignments; the caller should map pgconn error 23505 to HTTP 409 Conflict.
INSERT INTO class_subject_teachers (school_id, class_id, subject_id, teacher_id, hours_per_week)
VALUES (current_school_id(), $1, $2, $3, $4)
RETURNING *;

-- name: ListTeachersByClass :many
-- Returns the teacher-subject assignments for a given class.
-- Used by /classes/{classId}/teachers.
SELECT cst.id, cst.teacher_id, cst.subject_id, cst.hours_per_week,
    u.first_name AS teacher_first_name,
    u.last_name AS teacher_last_name,
    s.name AS subject_name,
    s.short_name AS subject_short_name
FROM class_subject_teachers cst
JOIN users u ON u.id = cst.teacher_id
JOIN subjects s ON s.id = cst.subject_id
WHERE cst.class_id = $1
ORDER BY s.name, u.last_name;

-- name: CheckTeacherClassSubject :one
-- Verifies that a teacher is assigned to a specific class+subject combination.
-- Returns the assignment row if it exists, or pgx.ErrNoRows otherwise.
-- This is the authorization check used before creating grades or absences.
SELECT id FROM class_subject_teachers
WHERE teacher_id = $1
    AND class_id = $2
    AND subject_id = $3;

-- name: CreateClass :one
-- Creates a new class for the current school and the specified school year.
-- The school_id is set automatically by current_school_id() via RLS context.
-- $1 = school_year_id (UUID, required)
-- $2 = name (text, e.g. "5A", required)
-- $3 = education_level (education_level enum: primary, middle, high)
-- $4 = grade_number (smallint 1–12, required)
-- $5 = homeroom_teacher_id (UUID, optional — pass NULL if not yet assigned)
-- $6 = max_students (smallint, optional — pass NULL to use DB default of 30)
INSERT INTO classes (school_id, school_year_id, name, education_level, grade_number, homeroom_teacher_id, max_students)
VALUES (current_school_id(), $1, $2, $3, $4, $5, $6)
RETURNING *;

-- name: UpdateClass :one
-- Updates mutable fields on a class row.
-- All fields use the same partial-update pattern: pass the new value to change,
-- or pass the CURRENT value to preserve it. The Go handler resolves which to send.
-- $1 = id, $2 = name, $3 = homeroom_teacher_id, $4 = max_students
UPDATE classes SET
    name = $2,
    homeroom_teacher_id = $3,
    max_students = $4,
    updated_at = now()
WHERE id = $1
RETURNING *;

-- name: CreateSubject :one
-- Creates a new subject for the current school. The school_id is set
-- automatically by current_school_id() via RLS context.
-- $1 = name (text, required)
-- $2 = short_name (text, optional — pass NULL if not provided)
-- $3 = education_level (education_level enum: primary, middle, high)
-- $4 = has_thesis (boolean, defaults to false if not provided by the caller)
INSERT INTO subjects (school_id, name, short_name, education_level, has_thesis)
VALUES (current_school_id(), $1, $2, $3, $4)
RETURNING *;

-- name: ListSubjectsBySchool :many
-- Returns all active subjects for the current tenant school.
-- Used by the /subjects endpoint.
SELECT * FROM subjects
WHERE is_active = true
ORDER BY education_level, name;

-- name: GetSubjectByID :one
-- Returns a single subject by ID. Used for validation when creating grades.
SELECT * FROM subjects WHERE id = $1;

-- name: GetSchoolByCurrentTenant :one
-- Returns the school for the current RLS tenant (current_school_id()).
-- Used by /schools/current. The RLS policy on schools is NOT enabled
-- (schools is a non-tenant table), so we filter explicitly.
-- Note: we select specific columns instead of SELECT * to avoid the
-- education_levels array column which requires custom pgx type registration.
SELECT id, district_id, name, siiir_code, address, city, county, phone, email, is_active, created_at, updated_at
FROM schools WHERE id = current_school_id();

-- name: GetGradeByID :one
-- Returns a single grade by its ID (only non-deleted grades).
-- Used when updating or deleting a specific grade.
SELECT * FROM grades WHERE id = $1 AND deleted_at IS NULL;

-- name: GetAbsenceByID :one
-- Returns a single absence by its ID.
-- Used when excusing a specific absence.
SELECT * FROM absences WHERE id = $1;

-- name: ListAbsencesByClassSemesterMonth :many
-- Returns absences for a class filtered by semester and calendar month.
-- Used when the client requests ?semester=I&month=10.
-- The month parameter ($3) is cast to integer so sqlc generates the correct Go type.
SELECT a.*, u.first_name as student_first_name, u.last_name as student_last_name
FROM absences a
JOIN users u ON u.id = a.student_id
WHERE a.class_id = $1
    AND a.semester = $2
    AND EXTRACT(MONTH FROM a.absence_date) = $3::int
    AND a.school_year_id = $4
ORDER BY a.absence_date, a.period_number, u.last_name;

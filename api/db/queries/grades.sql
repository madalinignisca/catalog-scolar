-- name: ListGradesByClassSubject :many
SELECT g.*, u.first_name as student_first_name, u.last_name as student_last_name
FROM grades g
JOIN users u ON u.id = g.student_id
WHERE g.class_id = $1
    AND g.subject_id = $2
    AND g.semester = $3
    AND g.school_year_id = $4
    AND g.deleted_at IS NULL
ORDER BY u.last_name, u.first_name, g.grade_date;

-- name: CreateGrade :one
INSERT INTO grades (
    school_id, student_id, class_id, subject_id, teacher_id,
    school_year_id, semester, numeric_grade, qualifier_grade,
    is_thesis, grade_date, description, client_id, client_timestamp, sync_status
) VALUES (
    current_school_id(), $1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14
) RETURNING *;

-- name: UpdateGrade :one
UPDATE grades SET
    numeric_grade = $2,
    qualifier_grade = $3,
    grade_date = $4,
    description = $5,
    updated_at = now()
WHERE id = $1 AND deleted_at IS NULL
RETURNING *;

-- name: SoftDeleteGrade :exec
UPDATE grades SET deleted_at = now(), updated_at = now() WHERE id = $1;

-- name: GetGradeByClientID :one
SELECT * FROM grades WHERE client_id = $1 AND school_id = current_school_id();

-- name: ListGradesModifiedSince :many
SELECT * FROM grades
WHERE updated_at > $1
    AND class_id = ANY($2::uuid[])
ORDER BY updated_at;

-- name: CountGradesForAverage :one
SELECT COUNT(*) FROM grades
WHERE student_id = $1
    AND subject_id = $2
    AND school_year_id = $3
    AND semester = $4
    AND deleted_at IS NULL
    AND is_thesis = false;

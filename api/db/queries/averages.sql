-- ============================================================
-- Averages (semester/annual denormalized cache)
-- ============================================================
-- Averages are computed when a teacher "closes" a semester for a subject.
-- The workflow is: teacher closes → director approves.
--
-- Romanian terms:
--   medie = average, închidere = close, aprobare = approval
--   medie anuală = annual average (semester IS NULL)
--   medie semestrială = semester average (semester = 'I' or 'II')

-- name: CreateOrUpdateAverage :one
-- Upserts an average row for a student/subject/semester.
-- Uses ON CONFLICT to handle the UNIQUE(student_id, subject_id, school_year_id, semester)
-- constraint. On conflict, updates the computed/final values and close metadata.
INSERT INTO averages (
    school_id, student_id, class_id, subject_id, school_year_id,
    semester, computed_value, final_value, qualifier_final,
    is_closed, closed_by, closed_at
) VALUES (
    current_school_id(), $1, $2, $3, $4,
    $5, $6, $7, $8,
    true, $9, now()
)
ON CONFLICT (student_id, subject_id, school_year_id, semester)
DO UPDATE SET
    computed_value = EXCLUDED.computed_value,
    final_value = EXCLUDED.final_value,
    qualifier_final = EXCLUDED.qualifier_final,
    is_closed = true,
    closed_by = EXCLUDED.closed_by,
    closed_at = now(),
    updated_at = now()
RETURNING *;

-- name: GetAverageByID :one
-- Returns a single average by its ID.
SELECT * FROM averages WHERE id = $1;

-- name: ListAveragesByClassSubject :many
-- Lists all averages for a class/subject/semester, joined with student names.
-- Used by the close endpoint to show all computed averages for a class.
SELECT a.*,
    u.first_name AS student_first_name,
    u.last_name AS student_last_name
FROM averages a
JOIN users u ON u.id = a.student_id
WHERE a.class_id = $1
    AND a.subject_id = $2
    AND a.semester = $3
    AND a.school_year_id = $4
ORDER BY u.last_name, u.first_name;

-- name: ApproveAverage :one
-- Marks an average as approved by a director/admin.
-- Only closed averages can be approved (enforced in handler).
UPDATE averages SET
    approved_by = $2,
    approved_at = now(),
    updated_at = now()
WHERE id = $1
RETURNING *;

-- name: HasApprovedAverages :one
-- Checks if any averages for a class/subject/semester have been approved.
-- Returns true if at least one approved average exists, preventing re-close.
SELECT EXISTS (
    SELECT 1 FROM averages
    WHERE class_id = $1
        AND subject_id = $2
        AND semester = $3
        AND school_year_id = $4
        AND approved_at IS NOT NULL
) AS has_approved;

-- name: ListGradesForAverage :many
-- Returns all non-deleted grades for a student/subject/semester,
-- including the is_thesis flag needed for weighted average calculation.
-- Ordered by grade_date for deterministic results.
SELECT id, numeric_grade, qualifier_grade, is_thesis
FROM grades
WHERE student_id = $1
    AND subject_id = $2
    AND school_year_id = $3
    AND semester = $4
    AND deleted_at IS NULL
ORDER BY grade_date;

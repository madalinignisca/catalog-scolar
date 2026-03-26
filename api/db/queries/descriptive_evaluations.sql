-- ============================================================
-- Descriptive Evaluations (primary school)
-- ============================================================
-- Primary school students (classes P-IV) receive descriptive evaluations
-- instead of numeric grades. These are free-text assessments written by
-- the teacher for each student, per subject, per semester.
--
-- Romanian term: "evaluare descriptivă" (plural "evaluări descriptive").
--
-- Unlike numeric grades (which are individual entries per assessment),
-- a descriptive evaluation is ONE text per student per subject per semester.
-- If updated, the content is overwritten (not versioned).

-- name: ListDescriptiveEvaluations :many
-- Lists all descriptive evaluations for a class, subject, and semester.
-- Joins with users to include student names for display in the catalog UI.
-- Results are sorted alphabetically by student last name, then first name.
SELECT de.*,
    u.first_name AS student_first_name,
    u.last_name AS student_last_name
FROM descriptive_evaluations de
JOIN users u ON u.id = de.student_id
WHERE de.class_id = $1
    AND de.subject_id = $2
    AND de.semester = $3
    AND de.school_year_id = $4
ORDER BY u.last_name, u.first_name;

-- name: GetDescriptiveEvaluation :one
-- Returns a single descriptive evaluation by its ID.
-- Used when updating an existing evaluation.
SELECT * FROM descriptive_evaluations WHERE id = $1;

-- name: CreateDescriptiveEvaluation :one
-- Creates or updates a descriptive evaluation for a primary school student.
-- Uses ON CONFLICT to enforce the one-evaluation-per-student-per-subject-per-semester
-- rule: if an evaluation already exists, the content and teacher are updated (upsert).
-- The school_id is set automatically by current_school_id() via RLS context.
INSERT INTO descriptive_evaluations (
    school_id, student_id, class_id, subject_id, teacher_id,
    school_year_id, semester, content
) VALUES (
    current_school_id(), $1, $2, $3, $4, $5, $6, $7
)
ON CONFLICT (school_id, student_id, subject_id, school_year_id, semester)
DO UPDATE SET content = EXCLUDED.content, teacher_id = EXCLUDED.teacher_id, updated_at = now()
RETURNING *;

-- name: UpdateDescriptiveEvaluation :one
-- Updates the content of an existing descriptive evaluation.
-- Only the content and updated_at timestamp change.
UPDATE descriptive_evaluations SET
    content = $2,
    updated_at = now()
WHERE id = $1
RETURNING *;

-- name: DeleteDescriptiveEvaluation :exec
-- Hard-deletes a descriptive evaluation.
-- Unlike grades, descriptive evaluations don't have soft delete
-- because they are free-text and can be rewritten at any time.
DELETE FROM descriptive_evaluations WHERE id = $1;

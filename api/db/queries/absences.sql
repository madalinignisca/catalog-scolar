-- name: ListAbsencesByClassDate :many
SELECT a.*, u.first_name as student_first_name, u.last_name as student_last_name
FROM absences a
JOIN users u ON u.id = a.student_id
WHERE a.class_id = $1
    AND a.absence_date = $2
ORDER BY a.period_number, u.last_name;

-- name: ListAbsencesByStudentSemester :many
SELECT a.*, s.name as subject_name
FROM absences a
JOIN subjects s ON s.id = a.subject_id
WHERE a.student_id = $1
    AND a.school_year_id = $2
    AND a.semester = $3
ORDER BY a.absence_date, a.period_number;

-- name: CreateAbsence :one
INSERT INTO absences (
    school_id, student_id, class_id, subject_id, teacher_id,
    school_year_id, semester, absence_date, period_number,
    absence_type, client_id, client_timestamp, sync_status
) VALUES (
    current_school_id(), $1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12
) RETURNING *;

-- name: ExcuseAbsence :one
UPDATE absences SET
    absence_type = $2,
    excused_by = $3,
    excused_at = now(),
    excuse_reason = $4,
    excuse_document = $5,
    updated_at = now()
WHERE id = $1
RETURNING *;

-- name: CountUnexcusedAbsences :one
SELECT COUNT(*) FROM absences
WHERE student_id = $1
    AND school_year_id = $2
    AND semester = $3
    AND absence_type = 'unexcused';

-- name: GetAbsenceByClientID :one
SELECT * FROM absences WHERE client_id = $1 AND school_id = current_school_id();

-- name: ListAbsencesModifiedSince :many
SELECT * FROM absences
WHERE updated_at > $1
    AND class_id = ANY($2::uuid[])
ORDER BY updated_at;

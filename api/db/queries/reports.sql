-- ============================================================
-- Report queries (dashboard, student report, class statistics)
-- ============================================================
-- These queries aggregate data for the real-time report endpoints.
-- They run within the RLS tenant context (current_school_id).

-- name: DashboardCounts :one
-- Returns high-level counts for the school dashboard.
-- Used by GET /reports/dashboard.
SELECT
    (SELECT COUNT(*) FROM users WHERE role = 'student' AND is_active = true) AS total_students,
    (SELECT COUNT(*) FROM users WHERE role = 'teacher' AND is_active = true) AS total_teachers,
    (SELECT COUNT(*) FROM classes WHERE school_year_id = $1) AS total_classes,
    (SELECT COUNT(*) FROM users WHERE role IN ('student', 'parent') AND activation_token IS NOT NULL AND is_active = false) AS pending_activations;

-- name: DashboardClassSummaries :many
-- Returns per-class summary stats for the dashboard.
-- Includes student count and class metadata.
SELECT c.id, c.name, c.education_level, c.grade_number,
    u.first_name AS homeroom_first_name,
    u.last_name AS homeroom_last_name,
    (SELECT COUNT(*) FROM class_enrollments ce
    WHERE ce.class_id = c.id AND ce.withdrawn_at IS NULL) AS student_count
FROM classes c
LEFT JOIN users u ON u.id = c.homeroom_teacher_id
WHERE c.school_year_id = $1
ORDER BY c.grade_number, c.name;

-- name: StudentReportGrades :many
-- Returns all grades for a student in a school year, grouped by subject.
-- Includes subject name, semester, and grade details.
SELECT g.id, g.subject_id, s.name AS subject_name, s.short_name,
    g.semester, g.numeric_grade, g.qualifier_grade, g.is_thesis,
    g.grade_date, g.description
FROM grades g
JOIN subjects s ON s.id = g.subject_id
WHERE g.student_id = $1
    AND g.school_year_id = $2
    AND g.deleted_at IS NULL
ORDER BY s.name, g.semester, g.grade_date;

-- name: StudentReportAbsences :many
-- Returns all absences for a student in a school year.
SELECT a.id, a.subject_id, s.name AS subject_name,
    a.semester, a.absence_date, a.period_number, a.absence_type
FROM absences a
JOIN subjects s ON s.id = a.subject_id
WHERE a.student_id = $1
    AND a.school_year_id = $2
ORDER BY a.absence_date, a.period_number;

-- name: StudentReportAverages :many
-- Returns all closed averages for a student in a school year.
SELECT a.id, a.subject_id, s.name AS subject_name,
    a.semester, a.computed_value, a.final_value, a.qualifier_final,
    a.is_closed, a.approved_at
FROM averages a
JOIN subjects s ON s.id = a.subject_id
WHERE a.student_id = $1
    AND a.school_year_id = $2
ORDER BY s.name, a.semester;

-- name: StudentReportEvaluations :many
-- Returns descriptive evaluations for a student in a school year (primary only).
SELECT de.id, de.subject_id, s.name AS subject_name,
    de.semester, de.content
FROM descriptive_evaluations de
JOIN subjects s ON s.id = de.subject_id
WHERE de.student_id = $1
    AND de.school_year_id = $2
ORDER BY s.name, de.semester;

-- name: ClassStatsGradeAggregates :many
-- Returns grade aggregates per subject for a class in a school year.
-- Provides count, average, min, max for numeric grades per semester.
SELECT g.subject_id, s.name AS subject_name,
    g.semester,
    COUNT(*) FILTER (WHERE g.numeric_grade IS NOT NULL AND g.is_thesis = false) AS grade_count,
    ROUND(AVG(g.numeric_grade) FILTER (WHERE g.numeric_grade IS NOT NULL AND g.is_thesis = false), 2) AS avg_grade,
    MIN(g.numeric_grade) FILTER (WHERE g.numeric_grade IS NOT NULL AND g.is_thesis = false) AS min_grade,
    MAX(g.numeric_grade) FILTER (WHERE g.numeric_grade IS NOT NULL AND g.is_thesis = false) AS max_grade,
    COUNT(*) FILTER (WHERE g.numeric_grade IS NOT NULL AND g.numeric_grade < 5 AND g.is_thesis = false) AS below_five_count
FROM grades g
JOIN subjects s ON s.id = g.subject_id
WHERE g.class_id = $1
    AND g.school_year_id = $2
    AND g.deleted_at IS NULL
GROUP BY g.subject_id, s.name, g.semester
ORDER BY s.name, g.semester;

-- name: ClassStatsAbsenceSummary :one
-- Returns total absence counts for a class in a school year.
SELECT
    COUNT(*) AS total_absences,
    COUNT(*) FILTER (WHERE absence_type = 'unexcused') AS unexcused_count,
    COUNT(*) FILTER (WHERE absence_type IN ('excused', 'medical', 'school_event')) AS excused_count
FROM absences
WHERE class_id = $1
    AND school_year_id = $2;

-- name: GetSchoolByID :one
SELECT * FROM schools WHERE id = $1;

-- name: GetSchoolBySIIIRCode :one
SELECT * FROM schools WHERE siiir_code = $1;

-- name: ListSchoolsByDistrict :many
SELECT * FROM schools WHERE district_id = $1 AND is_active = true ORDER BY name;

-- name: GetCurrentSchoolYear :one
SELECT * FROM school_years WHERE school_id = current_school_id() AND is_current = true;

-- name: ListSchoolYears :many
-- Returns all school years for the current tenant, ordered by start date.
-- Used by the student portability export to gather records across all years.
SELECT * FROM school_years
WHERE school_id = current_school_id()
ORDER BY start_date DESC;

-- name: GetEvaluationConfig :one
SELECT * FROM evaluation_configs
WHERE school_id = current_school_id()
    AND education_level = $1
    AND school_year_id = $2;

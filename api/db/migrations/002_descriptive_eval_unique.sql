-- +goose Up
-- Enforce one descriptive evaluation per student per subject per semester.
-- The schema comment says "a descriptive evaluation is ONE text per student per
-- subject per semester" but the original table definition lacks a constraint.
-- This migration adds the missing UNIQUE constraint.
ALTER TABLE descriptive_evaluations
    ADD CONSTRAINT uq_desc_eval_student_subject_semester
    UNIQUE (school_id, student_id, subject_id, school_year_id, semester);

-- +goose Down
ALTER TABLE descriptive_evaluations
    DROP CONSTRAINT IF EXISTS uq_desc_eval_student_subject_semester;

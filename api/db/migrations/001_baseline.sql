-- +goose Up
-- CatalogRO: baseline schema with RLS multi-tenancy

-- ============================================================
-- Extensions
-- ============================================================
CREATE EXTENSION IF NOT EXISTS "pgcrypto";
CREATE EXTENSION IF NOT EXISTS "citext";

-- ============================================================
-- ENUM types
-- ============================================================
CREATE TYPE user_role AS ENUM (
    'admin',
    'secretary',
    'teacher',
    'parent',
    'student'
);

CREATE TYPE education_level AS ENUM (
    'primary',
    'middle',
    'high'
);

CREATE TYPE qualifier AS ENUM ('FB', 'B', 'S', 'I');

CREATE TYPE absence_type AS ENUM (
    'unexcused',
    'medical',
    'excused',
    'school_event'
);

CREATE TYPE sync_status AS ENUM ('pending', 'synced', 'conflict', 'resolved');

CREATE TYPE semester AS ENUM ('I', 'II');

-- ============================================================
-- Districts (ISJ) — no RLS, public data
-- ============================================================
CREATE TABLE districts (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name            TEXT NOT NULL,
    county_code     CHAR(2) NOT NULL UNIQUE,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- ============================================================
-- Schools (tenants)
-- ============================================================
CREATE TABLE schools (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    district_id     UUID NOT NULL REFERENCES districts(id),
    name            TEXT NOT NULL,
    siiir_code      TEXT UNIQUE,
    education_levels education_level[] NOT NULL,
    address         TEXT,
    city            TEXT,
    county          TEXT,
    phone           TEXT,
    email           CITEXT,
    config          JSONB NOT NULL DEFAULT '{}',
    is_active       BOOLEAN NOT NULL DEFAULT true,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_schools_district ON schools(district_id);
CREATE INDEX idx_schools_siiir ON schools(siiir_code) WHERE siiir_code IS NOT NULL;

-- ============================================================
-- School years
-- ============================================================
CREATE TABLE school_years (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    school_id       UUID NOT NULL REFERENCES schools(id),
    label           TEXT NOT NULL,
    start_date      DATE NOT NULL,
    end_date        DATE NOT NULL,
    sem1_start      DATE NOT NULL,
    sem1_end        DATE NOT NULL,
    sem2_start      DATE NOT NULL,
    sem2_end        DATE NOT NULL,
    is_current      BOOLEAN NOT NULL DEFAULT false,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE(school_id, label)
);

-- ============================================================
-- Users (provisioned by secretary, activated by user)
-- ============================================================
CREATE TABLE users (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    school_id       UUID NOT NULL REFERENCES schools(id),
    role            user_role NOT NULL,
    email           CITEXT,
    phone           TEXT,
    first_name      TEXT NOT NULL,
    last_name       TEXT NOT NULL,
    password_hash   TEXT,
    totp_secret     BYTEA,
    totp_enabled    BOOLEAN NOT NULL DEFAULT false,
    provisioned_by  UUID REFERENCES users(id),
    siiir_student_id TEXT,
    activation_token TEXT UNIQUE,
    activation_sent_at TIMESTAMPTZ,
    activated_at    TIMESTAMPTZ,
    gdpr_consent_at TIMESTAMPTZ,
    is_active       BOOLEAN NOT NULL DEFAULT true,
    last_login_at   TIMESTAMPTZ,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE NULLS NOT DISTINCT (school_id, email, role)
);

CREATE INDEX idx_users_school ON users(school_id);
CREATE INDEX idx_users_email ON users(email) WHERE email IS NOT NULL;
CREATE INDEX idx_users_activation ON users(activation_token) WHERE activation_token IS NOT NULL;
CREATE INDEX idx_users_siiir ON users(siiir_student_id) WHERE siiir_student_id IS NOT NULL;

-- Parent-student links
CREATE TABLE parent_student_links (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    school_id       UUID NOT NULL REFERENCES schools(id),
    parent_id       UUID NOT NULL REFERENCES users(id),
    student_id      UUID NOT NULL REFERENCES users(id),
    relationship    TEXT DEFAULT 'parent',
    is_primary      BOOLEAN NOT NULL DEFAULT true,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE(parent_id, student_id)
);

-- ============================================================
-- Classes
-- ============================================================
CREATE TABLE classes (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    school_id       UUID NOT NULL REFERENCES schools(id),
    school_year_id  UUID NOT NULL REFERENCES school_years(id),
    name            TEXT NOT NULL,
    education_level education_level NOT NULL,
    grade_number    SMALLINT NOT NULL,
    homeroom_teacher_id UUID REFERENCES users(id),
    max_students    SMALLINT DEFAULT 30,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE(school_id, school_year_id, name)
);

CREATE INDEX idx_classes_school_year ON classes(school_id, school_year_id);

-- Class enrollments
CREATE TABLE class_enrollments (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    school_id       UUID NOT NULL REFERENCES schools(id),
    class_id        UUID NOT NULL REFERENCES classes(id),
    student_id      UUID NOT NULL REFERENCES users(id),
    enrolled_at     DATE NOT NULL DEFAULT CURRENT_DATE,
    withdrawn_at    DATE,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE(class_id, student_id)
);

-- ============================================================
-- Subjects & teacher assignments
-- ============================================================
CREATE TABLE subjects (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    school_id       UUID NOT NULL REFERENCES schools(id),
    name            TEXT NOT NULL,
    short_name      TEXT,
    education_level education_level NOT NULL,
    has_thesis      BOOLEAN NOT NULL DEFAULT false,
    is_active       BOOLEAN NOT NULL DEFAULT true,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE(school_id, name, education_level)
);

CREATE TABLE class_subject_teachers (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    school_id       UUID NOT NULL REFERENCES schools(id),
    class_id        UUID NOT NULL REFERENCES classes(id),
    subject_id      UUID NOT NULL REFERENCES subjects(id),
    teacher_id      UUID NOT NULL REFERENCES users(id),
    hours_per_week  SMALLINT NOT NULL DEFAULT 1,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE(class_id, subject_id, teacher_id)
);

CREATE INDEX idx_cst_teacher ON class_subject_teachers(teacher_id);
CREATE INDEX idx_cst_class ON class_subject_teachers(class_id);

-- ============================================================
-- Evaluation configs (per school/level/year)
-- ============================================================
CREATE TABLE evaluation_configs (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    school_id       UUID NOT NULL REFERENCES schools(id),
    education_level education_level NOT NULL,
    school_year_id  UUID NOT NULL REFERENCES school_years(id),
    use_qualifiers  BOOLEAN NOT NULL DEFAULT false,
    min_grade       SMALLINT NOT NULL DEFAULT 1,
    max_grade       SMALLINT NOT NULL DEFAULT 10,
    thesis_weight   NUMERIC(3,2) DEFAULT 0.25,
    min_grades_sem  SMALLINT NOT NULL DEFAULT 3,
    rounding_rule   TEXT NOT NULL DEFAULT 'standard',
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE(school_id, education_level, school_year_id)
);

-- ============================================================
-- SOURCE MAPPINGS (interoperability abstraction layer)
-- Links internal entities to external IDs (SIIIR, OneRoster, etc.)
-- ============================================================
CREATE TABLE source_mappings (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    school_id       UUID NOT NULL REFERENCES schools(id),
    entity_type     TEXT NOT NULL,
    entity_id       UUID NOT NULL,
    source_system   TEXT NOT NULL,
    source_id       TEXT NOT NULL,
    source_metadata JSONB DEFAULT '{}',
    last_synced_at  TIMESTAMPTZ,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE(school_id, entity_type, entity_id, source_system),
    UNIQUE(school_id, source_system, source_id, entity_type)
);

CREATE INDEX idx_source_mappings_entity ON source_mappings(entity_type, entity_id);
CREATE INDEX idx_source_mappings_source ON source_mappings(source_system, source_id);

-- ============================================================
-- GRADES (catalog core)
-- ============================================================
CREATE TABLE grades (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    school_id       UUID NOT NULL REFERENCES schools(id),
    student_id      UUID NOT NULL REFERENCES users(id),
    class_id        UUID NOT NULL REFERENCES classes(id),
    subject_id      UUID NOT NULL REFERENCES subjects(id),
    teacher_id      UUID NOT NULL REFERENCES users(id),
    school_year_id  UUID NOT NULL REFERENCES school_years(id),
    semester        semester NOT NULL,
    numeric_grade   SMALLINT CHECK (numeric_grade BETWEEN 1 AND 10),
    qualifier_grade qualifier,
    is_thesis       BOOLEAN NOT NULL DEFAULT false,
    grade_date      DATE NOT NULL DEFAULT CURRENT_DATE,
    description     TEXT,
    client_id       UUID,
    client_timestamp TIMESTAMPTZ,
    sync_status     sync_status NOT NULL DEFAULT 'synced',
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    deleted_at      TIMESTAMPTZ,
    CHECK (
        (numeric_grade IS NOT NULL AND qualifier_grade IS NULL) OR
        (numeric_grade IS NULL AND qualifier_grade IS NOT NULL)
    ),
    UNIQUE NULLS NOT DISTINCT (school_id, client_id)
);

CREATE INDEX idx_grades_student ON grades(student_id, subject_id, school_year_id);
CREATE INDEX idx_grades_class ON grades(class_id, subject_id, semester);
CREATE INDEX idx_grades_sync ON grades(sync_status) WHERE sync_status != 'synced';
CREATE INDEX idx_grades_deleted ON grades(deleted_at) WHERE deleted_at IS NOT NULL;

-- ============================================================
-- ABSENCES
-- ============================================================
CREATE TABLE absences (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    school_id       UUID NOT NULL REFERENCES schools(id),
    student_id      UUID NOT NULL REFERENCES users(id),
    class_id        UUID NOT NULL REFERENCES classes(id),
    subject_id      UUID NOT NULL REFERENCES subjects(id),
    teacher_id      UUID NOT NULL REFERENCES users(id),
    school_year_id  UUID NOT NULL REFERENCES school_years(id),
    semester        semester NOT NULL,
    absence_date    DATE NOT NULL,
    period_number   SMALLINT NOT NULL,
    absence_type    absence_type NOT NULL DEFAULT 'unexcused',
    excused_by      UUID REFERENCES users(id),
    excused_at      TIMESTAMPTZ,
    excuse_reason   TEXT,
    excuse_document TEXT,
    client_id       UUID,
    client_timestamp TIMESTAMPTZ,
    sync_status     sync_status NOT NULL DEFAULT 'synced',
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE(student_id, absence_date, period_number),
    UNIQUE NULLS NOT DISTINCT (school_id, client_id)
);

CREATE INDEX idx_absences_student ON absences(student_id, school_year_id);
CREATE INDEX idx_absences_class ON absences(class_id, absence_date);
CREATE INDEX idx_absences_sync ON absences(sync_status) WHERE sync_status != 'synced';

-- ============================================================
-- AVERAGES (denormalized cache)
-- ============================================================
CREATE TABLE averages (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    school_id       UUID NOT NULL REFERENCES schools(id),
    student_id      UUID NOT NULL REFERENCES users(id),
    class_id        UUID NOT NULL REFERENCES classes(id),
    subject_id      UUID NOT NULL REFERENCES subjects(id),
    school_year_id  UUID NOT NULL REFERENCES school_years(id),
    semester        semester,
    computed_value  NUMERIC(4,2),
    final_value     NUMERIC(4,2),
    qualifier_final qualifier,
    is_closed       BOOLEAN NOT NULL DEFAULT false,
    closed_by       UUID REFERENCES users(id),
    closed_at       TIMESTAMPTZ,
    approved_by     UUID REFERENCES users(id),
    approved_at     TIMESTAMPTZ,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE(student_id, subject_id, school_year_id, semester)
);

-- ============================================================
-- DESCRIPTIVE EVALUATIONS (primary)
-- ============================================================
CREATE TABLE descriptive_evaluations (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    school_id       UUID NOT NULL REFERENCES schools(id),
    student_id      UUID NOT NULL REFERENCES users(id),
    class_id        UUID NOT NULL REFERENCES classes(id),
    subject_id      UUID NOT NULL REFERENCES subjects(id),
    teacher_id      UUID NOT NULL REFERENCES users(id),
    school_year_id  UUID NOT NULL REFERENCES school_years(id),
    semester        semester NOT NULL,
    content         TEXT NOT NULL,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- ============================================================
-- SYNC CONFLICTS (audit)
-- ============================================================
CREATE TABLE sync_conflicts (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    school_id       UUID NOT NULL REFERENCES schools(id),
    entity_type     TEXT NOT NULL,
    entity_id       UUID NOT NULL,
    client_version  JSONB NOT NULL,
    server_version  JSONB NOT NULL,
    resolution      TEXT NOT NULL DEFAULT 'server_wins',
    resolved_by     UUID REFERENCES users(id),
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- ============================================================
-- AUDIT LOG (immutable, append-only)
-- ============================================================
CREATE TABLE audit_log (
    id              BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    school_id       UUID NOT NULL,
    user_id         UUID NOT NULL,
    action          TEXT NOT NULL,
    entity_type     TEXT NOT NULL,
    entity_id       UUID NOT NULL,
    old_values      JSONB,
    new_values      JSONB,
    ip_address      INET,
    user_agent      TEXT,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_audit_school ON audit_log(school_id, created_at DESC);
CREATE INDEX idx_audit_entity ON audit_log(entity_type, entity_id);

-- ============================================================
-- MESSAGES
-- ============================================================
CREATE TABLE messages (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    school_id       UUID NOT NULL REFERENCES schools(id),
    sender_id       UUID NOT NULL REFERENCES users(id),
    subject         TEXT,
    body            TEXT NOT NULL,
    is_announcement BOOLEAN NOT NULL DEFAULT false,
    target_class_id UUID REFERENCES classes(id),
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE message_recipients (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    school_id       UUID NOT NULL REFERENCES schools(id),
    message_id      UUID NOT NULL REFERENCES messages(id) ON DELETE CASCADE,
    recipient_id    UUID NOT NULL REFERENCES users(id),
    read_at         TIMESTAMPTZ,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE(message_id, recipient_id)
);

-- ============================================================
-- REFRESH TOKENS
-- ============================================================
CREATE TABLE refresh_tokens (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id         UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    token_hash      TEXT NOT NULL UNIQUE,
    expires_at      TIMESTAMPTZ NOT NULL,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    revoked_at      TIMESTAMPTZ
);

CREATE INDEX idx_refresh_user ON refresh_tokens(user_id);

-- ============================================================
-- RLS helper
-- ============================================================
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION current_school_id() RETURNS UUID AS $$
    SELECT current_setting('app.current_school_id', true)::uuid;
$$ LANGUAGE sql STABLE;
-- +goose StatementEnd

-- ============================================================
-- ROW-LEVEL SECURITY — enable on all tenant tables
-- ============================================================
ALTER TABLE users ENABLE ROW LEVEL SECURITY;
CREATE POLICY users_tenant ON users USING (school_id = current_school_id());

ALTER TABLE school_years ENABLE ROW LEVEL SECURITY;
CREATE POLICY school_years_tenant ON school_years USING (school_id = current_school_id());

ALTER TABLE classes ENABLE ROW LEVEL SECURITY;
CREATE POLICY classes_tenant ON classes USING (school_id = current_school_id());

ALTER TABLE class_enrollments ENABLE ROW LEVEL SECURITY;
CREATE POLICY enrollments_tenant ON class_enrollments USING (school_id = current_school_id());

ALTER TABLE subjects ENABLE ROW LEVEL SECURITY;
CREATE POLICY subjects_tenant ON subjects USING (school_id = current_school_id());

ALTER TABLE class_subject_teachers ENABLE ROW LEVEL SECURITY;
CREATE POLICY cst_tenant ON class_subject_teachers USING (school_id = current_school_id());

ALTER TABLE evaluation_configs ENABLE ROW LEVEL SECURITY;
CREATE POLICY eval_config_tenant ON evaluation_configs USING (school_id = current_school_id());

ALTER TABLE grades ENABLE ROW LEVEL SECURITY;
CREATE POLICY grades_tenant ON grades USING (school_id = current_school_id());

ALTER TABLE absences ENABLE ROW LEVEL SECURITY;
CREATE POLICY absences_tenant ON absences USING (school_id = current_school_id());

ALTER TABLE averages ENABLE ROW LEVEL SECURITY;
CREATE POLICY averages_tenant ON averages USING (school_id = current_school_id());

ALTER TABLE descriptive_evaluations ENABLE ROW LEVEL SECURITY;
CREATE POLICY desc_eval_tenant ON descriptive_evaluations USING (school_id = current_school_id());

ALTER TABLE sync_conflicts ENABLE ROW LEVEL SECURITY;
CREATE POLICY sync_conflicts_tenant ON sync_conflicts USING (school_id = current_school_id());

ALTER TABLE audit_log ENABLE ROW LEVEL SECURITY;
CREATE POLICY audit_tenant ON audit_log USING (school_id = current_school_id());

ALTER TABLE messages ENABLE ROW LEVEL SECURITY;
CREATE POLICY messages_tenant ON messages USING (school_id = current_school_id());

ALTER TABLE message_recipients ENABLE ROW LEVEL SECURITY;
CREATE POLICY msg_recip_tenant ON message_recipients USING (school_id = current_school_id());

ALTER TABLE parent_student_links ENABLE ROW LEVEL SECURITY;
CREATE POLICY psl_tenant ON parent_student_links USING (school_id = current_school_id());

ALTER TABLE source_mappings ENABLE ROW LEVEL SECURITY;
CREATE POLICY source_mappings_tenant ON source_mappings USING (school_id = current_school_id());

-- ============================================================
-- App role (non-superuser, respects RLS)
-- ============================================================
-- +goose StatementBegin
DO $$
BEGIN
    IF NOT EXISTS (SELECT FROM pg_roles WHERE rolname = 'catalogro_app') THEN
        CREATE ROLE catalogro_app LOGIN PASSWORD 'catalogro_app' NOSUPERUSER;
    END IF;
END
$$;
-- +goose StatementEnd

GRANT SELECT, INSERT, UPDATE, DELETE ON ALL TABLES IN SCHEMA public TO catalogro_app;
GRANT USAGE, SELECT ON ALL SEQUENCES IN SCHEMA public TO catalogro_app;

-- +goose Down
DROP OWNED BY catalogro_app;
DROP ROLE IF EXISTS catalogro_app;

DROP TABLE IF EXISTS message_recipients CASCADE;
DROP TABLE IF EXISTS messages CASCADE;
DROP TABLE IF EXISTS refresh_tokens CASCADE;
DROP TABLE IF EXISTS audit_log CASCADE;
DROP TABLE IF EXISTS sync_conflicts CASCADE;
DROP TABLE IF EXISTS descriptive_evaluations CASCADE;
DROP TABLE IF EXISTS averages CASCADE;
DROP TABLE IF EXISTS absences CASCADE;
DROP TABLE IF EXISTS grades CASCADE;
DROP TABLE IF EXISTS source_mappings CASCADE;
DROP TABLE IF EXISTS evaluation_configs CASCADE;
DROP TABLE IF EXISTS class_subject_teachers CASCADE;
DROP TABLE IF EXISTS subjects CASCADE;
DROP TABLE IF EXISTS class_enrollments CASCADE;
DROP TABLE IF EXISTS classes CASCADE;
DROP TABLE IF EXISTS parent_student_links CASCADE;
DROP TABLE IF EXISTS users CASCADE;
DROP TABLE IF EXISTS school_years CASCADE;
DROP TABLE IF EXISTS schools CASCADE;
DROP TABLE IF EXISTS districts CASCADE;

DROP FUNCTION IF EXISTS current_school_id();

DROP TYPE IF EXISTS semester;
DROP TYPE IF EXISTS sync_status;
DROP TYPE IF EXISTS absence_type;
DROP TYPE IF EXISTS qualifier;
DROP TYPE IF EXISTS education_level;
DROP TYPE IF EXISTS user_role;

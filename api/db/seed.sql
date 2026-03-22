-- CatalogRO seed data: 2 realistic Romanian schools
-- Run with: make seed
-- Requires: migration 001_baseline already applied

-- Stable UUIDs for predictable references in dev/testing
-- District: ISJ Cluj
INSERT INTO districts (id, name, county_code) VALUES
    ('d0000000-0000-0000-0000-000000000001', 'Inspectoratul Școlar Județean Cluj', 'CJ'),
    ('d0000000-0000-0000-0000-000000000002', 'Inspectoratul Școlar Municipal București', 'B ');

-- ============================================================
-- School 1: Școala Gimnazială "Liviu Rebreanu" Cluj-Napoca
-- Levels: primary + middle (clasele P-VIII)
-- ============================================================
INSERT INTO schools (id, district_id, name, siiir_code, education_levels, address, city, county, phone, email) VALUES
    ('a0000000-0000-0000-0000-000000000001',
     'd0000000-0000-0000-0000-000000000001',
     'Școala Gimnazială "Liviu Rebreanu"',
     'CJ-GIM-0042',
     '{primary,middle}',
     'Str. Memorandumului nr. 12',
     'Cluj-Napoca',
     'Cluj',
     '0264-555-100',
     'secretariat@scoala-rebreanu.ro');

-- School year 2026-2027
INSERT INTO school_years (id, school_id, label, start_date, end_date, sem1_start, sem1_end, sem2_start, sem2_end, is_current) VALUES
    ('e0000000-0000-0000-0000-000000000001',
     'a0000000-0000-0000-0000-000000000001',
     '2026-2027', '2026-09-14', '2027-06-20',
     '2026-09-14', '2027-01-31', '2027-02-10', '2027-06-20',
     true);

-- Evaluation configs
INSERT INTO evaluation_configs (school_id, education_level, school_year_id, use_qualifiers, min_grade, max_grade, thesis_weight, min_grades_sem) VALUES
    ('a0000000-0000-0000-0000-000000000001', 'primary', 'e0000000-0000-0000-0000-000000000001', true, 1, 10, null, 3),
    ('a0000000-0000-0000-0000-000000000001', 'middle', 'e0000000-0000-0000-0000-000000000001', false, 1, 10, 0.25, 3);

-- Admin (director) — pre-activated for dev
INSERT INTO users (id, school_id, role, email, first_name, last_name, password_hash, activated_at, gdpr_consent_at) VALUES
    ('b1000000-0000-0000-0000-000000000001',
     'a0000000-0000-0000-0000-000000000001',
     'admin',
     'director@scoala-rebreanu.ro',
     'Maria', 'Popescu',
     -- password: "catalog2026" (bcrypt)
     '$2a$10$AgrFyrZVE6ZRRSXt46/eHepzjgYkWMTxQAB7b6QU83l2NnNDrvAXW',
     now(), now());

-- Secretary
INSERT INTO users (id, school_id, role, email, first_name, last_name, password_hash, activated_at, provisioned_by) VALUES
    ('b1000000-0000-0000-0000-000000000002',
     'a0000000-0000-0000-0000-000000000001',
     'secretary',
     'secretar@scoala-rebreanu.ro',
     'Elena', 'Ionescu',
     '$2a$10$AgrFyrZVE6ZRRSXt46/eHepzjgYkWMTxQAB7b6QU83l2NnNDrvAXW',
     now(),
     'b1000000-0000-0000-0000-000000000001');

-- Teachers
INSERT INTO users (id, school_id, role, email, first_name, last_name, password_hash, activated_at, provisioned_by) VALUES
    ('b1000000-0000-0000-0000-000000000010',
     'a0000000-0000-0000-0000-000000000001',
     'teacher', 'ana.dumitrescu@scoala-rebreanu.ro',
     'Ana', 'Dumitrescu',
     '$2a$10$AgrFyrZVE6ZRRSXt46/eHepzjgYkWMTxQAB7b6QU83l2NnNDrvAXW',
     now(), 'b1000000-0000-0000-0000-000000000002'),
    ('b1000000-0000-0000-0000-000000000011',
     'a0000000-0000-0000-0000-000000000001',
     'teacher', 'ion.vasilescu@scoala-rebreanu.ro',
     'Ion', 'Vasilescu',
     '$2a$10$AgrFyrZVE6ZRRSXt46/eHepzjgYkWMTxQAB7b6QU83l2NnNDrvAXW',
     now(), 'b1000000-0000-0000-0000-000000000002'),
    ('b1000000-0000-0000-0000-000000000012',
     'a0000000-0000-0000-0000-000000000001',
     'teacher', 'gabriela.marin@scoala-rebreanu.ro',
     'Gabriela', 'Marin',
     '$2a$10$AgrFyrZVE6ZRRSXt46/eHepzjgYkWMTxQAB7b6QU83l2NnNDrvAXW',
     now(), 'b1000000-0000-0000-0000-000000000002');

-- Classes: 2A (primar), 6B (gimnaziu)
INSERT INTO classes (id, school_id, school_year_id, name, education_level, grade_number, homeroom_teacher_id) VALUES
    ('f1000000-0000-0000-0000-000000000001',
     'a0000000-0000-0000-0000-000000000001',
     'e0000000-0000-0000-0000-000000000001',
     '2A', 'primary', 2,
     'b1000000-0000-0000-0000-000000000010'),
    ('f1000000-0000-0000-0000-000000000002',
     'a0000000-0000-0000-0000-000000000001',
     'e0000000-0000-0000-0000-000000000001',
     '6B', 'middle', 6,
     'b1000000-0000-0000-0000-000000000011');

-- Subjects
INSERT INTO subjects (id, school_id, name, short_name, education_level, has_thesis) VALUES
    ('f1000000-0000-0000-0000-000000000001', 'a0000000-0000-0000-0000-000000000001', 'Comunicare în limba română', 'CLR', 'primary', false),
    ('f1000000-0000-0000-0000-000000000002', 'a0000000-0000-0000-0000-000000000001', 'Matematică și explorarea mediului', 'MEM', 'primary', false),
    ('f1000000-0000-0000-0000-000000000003', 'a0000000-0000-0000-0000-000000000001', 'Limba și literatura română', 'ROM', 'middle', true),
    ('f1000000-0000-0000-0000-000000000004', 'a0000000-0000-0000-0000-000000000001', 'Matematică', 'MAT', 'middle', true),
    ('f1000000-0000-0000-0000-000000000005', 'a0000000-0000-0000-0000-000000000001', 'Istorie', 'IST', 'middle', false),
    ('f1000000-0000-0000-0000-000000000006', 'a0000000-0000-0000-0000-000000000001', 'Educație fizică', 'EFS', 'middle', false);

-- Teacher assignments
INSERT INTO class_subject_teachers (school_id, class_id, subject_id, teacher_id, hours_per_week) VALUES
    ('a0000000-0000-0000-0000-000000000001', 'f1000000-0000-0000-0000-000000000001', 'f1000000-0000-0000-0000-000000000001', 'b1000000-0000-0000-0000-000000000010', 7),
    ('a0000000-0000-0000-0000-000000000001', 'f1000000-0000-0000-0000-000000000001', 'f1000000-0000-0000-0000-000000000002', 'b1000000-0000-0000-0000-000000000010', 5),
    ('a0000000-0000-0000-0000-000000000001', 'f1000000-0000-0000-0000-000000000002', 'f1000000-0000-0000-0000-000000000003', 'b1000000-0000-0000-0000-000000000011', 5),
    ('a0000000-0000-0000-0000-000000000001', 'f1000000-0000-0000-0000-000000000002', 'f1000000-0000-0000-0000-000000000004', 'b1000000-0000-0000-0000-000000000012', 4),
    ('a0000000-0000-0000-0000-000000000001', 'f1000000-0000-0000-0000-000000000002', 'f1000000-0000-0000-0000-000000000005', 'b1000000-0000-0000-0000-000000000011', 2);

-- Students: 5 in 2A (primar), 5 in 6B (gimnaziu)
-- Each student gets a unique email to satisfy UNIQUE NULLS NOT DISTINCT (school_id, email, role)
INSERT INTO users (id, school_id, role, email, first_name, last_name, activated_at, provisioned_by) VALUES
    ('b1000000-0000-0000-0000-000000000101', 'a0000000-0000-0000-0000-000000000001', 'student', 'andrei.moldovan@elev.rebreanu.ro', 'Andrei', 'Moldovan', now(), 'b1000000-0000-0000-0000-000000000002'),
    ('b1000000-0000-0000-0000-000000000102', 'a0000000-0000-0000-0000-000000000001', 'student', 'ioana.crisan@elev.rebreanu.ro', 'Ioana', 'Crișan', now(), 'b1000000-0000-0000-0000-000000000002'),
    ('b1000000-0000-0000-0000-000000000103', 'a0000000-0000-0000-0000-000000000001', 'student', 'mircea.toma@elev.rebreanu.ro', 'Mircea', 'Toma', now(), 'b1000000-0000-0000-0000-000000000002'),
    ('b1000000-0000-0000-0000-000000000104', 'a0000000-0000-0000-0000-000000000001', 'student', 'daria.luca@elev.rebreanu.ro', 'Daria', 'Luca', now(), 'b1000000-0000-0000-0000-000000000002'),
    ('b1000000-0000-0000-0000-000000000105', 'a0000000-0000-0000-0000-000000000001', 'student', 'matei.muresan@elev.rebreanu.ro', 'Matei', 'Mureșan', now(), 'b1000000-0000-0000-0000-000000000002'),
    ('b1000000-0000-0000-0000-000000000201', 'a0000000-0000-0000-0000-000000000001', 'student', 'alexandru.pop@elev.rebreanu.ro', 'Alexandru', 'Pop', now(), 'b1000000-0000-0000-0000-000000000002'),
    ('b1000000-0000-0000-0000-000000000202', 'a0000000-0000-0000-0000-000000000001', 'student', 'sofia.rus@elev.rebreanu.ro', 'Sofia', 'Rus', now(), 'b1000000-0000-0000-0000-000000000002'),
    ('b1000000-0000-0000-0000-000000000203', 'a0000000-0000-0000-0000-000000000001', 'student', 'david.bogdan@elev.rebreanu.ro', 'David', 'Bogdan', now(), 'b1000000-0000-0000-0000-000000000002'),
    ('b1000000-0000-0000-0000-000000000204', 'a0000000-0000-0000-0000-000000000001', 'student', 'maria.suciu@elev.rebreanu.ro', 'Maria', 'Suciu', now(), 'b1000000-0000-0000-0000-000000000002'),
    ('b1000000-0000-0000-0000-000000000205', 'a0000000-0000-0000-0000-000000000001', 'student', 'radu.campean@elev.rebreanu.ro', 'Radu', 'Câmpean', now(), 'b1000000-0000-0000-0000-000000000002');

-- Enrollments
INSERT INTO class_enrollments (school_id, class_id, student_id) VALUES
    ('a0000000-0000-0000-0000-000000000001', 'f1000000-0000-0000-0000-000000000001', 'b1000000-0000-0000-0000-000000000101'),
    ('a0000000-0000-0000-0000-000000000001', 'f1000000-0000-0000-0000-000000000001', 'b1000000-0000-0000-0000-000000000102'),
    ('a0000000-0000-0000-0000-000000000001', 'f1000000-0000-0000-0000-000000000001', 'b1000000-0000-0000-0000-000000000103'),
    ('a0000000-0000-0000-0000-000000000001', 'f1000000-0000-0000-0000-000000000001', 'b1000000-0000-0000-0000-000000000104'),
    ('a0000000-0000-0000-0000-000000000001', 'f1000000-0000-0000-0000-000000000001', 'b1000000-0000-0000-0000-000000000105'),
    ('a0000000-0000-0000-0000-000000000001', 'f1000000-0000-0000-0000-000000000002', 'b1000000-0000-0000-0000-000000000201'),
    ('a0000000-0000-0000-0000-000000000001', 'f1000000-0000-0000-0000-000000000002', 'b1000000-0000-0000-0000-000000000202'),
    ('a0000000-0000-0000-0000-000000000001', 'f1000000-0000-0000-0000-000000000002', 'b1000000-0000-0000-0000-000000000203'),
    ('a0000000-0000-0000-0000-000000000001', 'f1000000-0000-0000-0000-000000000002', 'b1000000-0000-0000-0000-000000000204'),
    ('a0000000-0000-0000-0000-000000000001', 'f1000000-0000-0000-0000-000000000002', 'b1000000-0000-0000-0000-000000000205');

-- Parents with children linked (activated, with GDPR consent)
INSERT INTO users (id, school_id, role, email, phone, first_name, last_name, password_hash, activated_at, gdpr_consent_at, provisioned_by) VALUES
    ('b1000000-0000-0000-0000-000000000301', 'a0000000-0000-0000-0000-000000000001', 'parent', 'ion.moldovan@gmail.com', '0741-100-001', 'Ion', 'Moldovan', '$2a$10$AgrFyrZVE6ZRRSXt46/eHepzjgYkWMTxQAB7b6QU83l2NnNDrvAXW', now(), now(), 'b1000000-0000-0000-0000-000000000002'),
    ('b1000000-0000-0000-0000-000000000302', 'a0000000-0000-0000-0000-000000000001', 'parent', 'cristina.pop@yahoo.com', '0741-200-001', 'Cristina', 'Pop', '$2a$10$AgrFyrZVE6ZRRSXt46/eHepzjgYkWMTxQAB7b6QU83l2NnNDrvAXW', now(), now(), 'b1000000-0000-0000-0000-000000000002');

INSERT INTO parent_student_links (school_id, parent_id, student_id) VALUES
    ('a0000000-0000-0000-0000-000000000001', 'b1000000-0000-0000-0000-000000000301', 'b1000000-0000-0000-0000-000000000101'),
    ('a0000000-0000-0000-0000-000000000001', 'b1000000-0000-0000-0000-000000000302', 'b1000000-0000-0000-0000-000000000201');

-- Sample grades for 6B (numeric)
INSERT INTO grades (school_id, student_id, class_id, subject_id, teacher_id, school_year_id, semester, numeric_grade, grade_date) VALUES
    ('a0000000-0000-0000-0000-000000000001', 'b1000000-0000-0000-0000-000000000201', 'f1000000-0000-0000-0000-000000000002', 'f1000000-0000-0000-0000-000000000003', 'b1000000-0000-0000-0000-000000000011', 'e0000000-0000-0000-0000-000000000001', 'I', 9, '2026-10-05'),
    ('a0000000-0000-0000-0000-000000000001', 'b1000000-0000-0000-0000-000000000201', 'f1000000-0000-0000-0000-000000000002', 'f1000000-0000-0000-0000-000000000003', 'b1000000-0000-0000-0000-000000000011', 'e0000000-0000-0000-0000-000000000001', 'I', 8, '2026-11-12'),
    ('a0000000-0000-0000-0000-000000000001', 'b1000000-0000-0000-0000-000000000201', 'f1000000-0000-0000-0000-000000000002', 'f1000000-0000-0000-0000-000000000004', 'b1000000-0000-0000-0000-000000000012', 'e0000000-0000-0000-0000-000000000001', 'I', 10, '2026-10-10'),
    ('a0000000-0000-0000-0000-000000000001', 'b1000000-0000-0000-0000-000000000202', 'f1000000-0000-0000-0000-000000000002', 'f1000000-0000-0000-0000-000000000003', 'b1000000-0000-0000-0000-000000000011', 'e0000000-0000-0000-0000-000000000001', 'I', 7, '2026-10-05'),
    ('a0000000-0000-0000-0000-000000000001', 'b1000000-0000-0000-0000-000000000203', 'f1000000-0000-0000-0000-000000000002', 'f1000000-0000-0000-0000-000000000004', 'b1000000-0000-0000-0000-000000000012', 'e0000000-0000-0000-0000-000000000001', 'I', 6, '2026-10-10');

-- Sample qualifiers for 2A (primary)
INSERT INTO grades (school_id, student_id, class_id, subject_id, teacher_id, school_year_id, semester, qualifier_grade, grade_date) VALUES
    ('a0000000-0000-0000-0000-000000000001', 'b1000000-0000-0000-0000-000000000101', 'f1000000-0000-0000-0000-000000000001', 'f1000000-0000-0000-0000-000000000001', 'b1000000-0000-0000-0000-000000000010', 'e0000000-0000-0000-0000-000000000001', 'I', 'FB', '2026-10-08'),
    ('a0000000-0000-0000-0000-000000000001', 'b1000000-0000-0000-0000-000000000102', 'f1000000-0000-0000-0000-000000000001', 'f1000000-0000-0000-0000-000000000001', 'b1000000-0000-0000-0000-000000000010', 'e0000000-0000-0000-0000-000000000001', 'I', 'B', '2026-10-08'),
    ('a0000000-0000-0000-0000-000000000001', 'b1000000-0000-0000-0000-000000000103', 'f1000000-0000-0000-0000-000000000001', 'f1000000-0000-0000-0000-000000000002', 'b1000000-0000-0000-0000-000000000010', 'e0000000-0000-0000-0000-000000000001', 'I', 'FB', '2026-10-15');

-- Sample absences
INSERT INTO absences (school_id, student_id, class_id, subject_id, teacher_id, school_year_id, semester, absence_date, period_number) VALUES
    ('a0000000-0000-0000-0000-000000000001', 'b1000000-0000-0000-0000-000000000203', 'f1000000-0000-0000-0000-000000000002', 'f1000000-0000-0000-0000-000000000003', 'b1000000-0000-0000-0000-000000000011', 'e0000000-0000-0000-0000-000000000001', 'I', '2026-10-20', 3),
    ('a0000000-0000-0000-0000-000000000001', 'b1000000-0000-0000-0000-000000000204', 'f1000000-0000-0000-0000-000000000002', 'f1000000-0000-0000-0000-000000000004', 'b1000000-0000-0000-0000-000000000012', 'e0000000-0000-0000-0000-000000000001', 'I', '2026-11-03', 1);


-- ============================================================
-- School 2: Liceul Teoretic "Tudor Vianu" București
-- Levels: high (clasele IX-XII)
-- ============================================================
INSERT INTO schools (id, district_id, name, siiir_code, education_levels, address, city, county, phone, email) VALUES
    ('a0000000-0000-0000-0000-000000000002',
     'd0000000-0000-0000-0000-000000000002',
     'Liceul Teoretic "Tudor Vianu"',
     'B-LIC-0118',
     '{high}',
     'Str. Arhitect Ion Mincu nr. 10',
     'București',
     'Sector 1',
     '021-314-5500',
     'secretariat@vianu.ro');

INSERT INTO school_years (id, school_id, label, start_date, end_date, sem1_start, sem1_end, sem2_start, sem2_end, is_current) VALUES
    ('e0000000-0000-0000-0000-000000000002',
     'a0000000-0000-0000-0000-000000000002',
     '2026-2027', '2026-09-14', '2027-06-20',
     '2026-09-14', '2027-01-31', '2027-02-10', '2027-06-20',
     true);

INSERT INTO evaluation_configs (school_id, education_level, school_year_id, use_qualifiers, min_grade, max_grade, thesis_weight, min_grades_sem) VALUES
    ('a0000000-0000-0000-0000-000000000002', 'high', 'e0000000-0000-0000-0000-000000000002', false, 1, 10, 0.25, 3);

-- Admin
INSERT INTO users (id, school_id, role, email, first_name, last_name, password_hash, activated_at, gdpr_consent_at) VALUES
    ('b2000000-0000-0000-0000-000000000001',
     'a0000000-0000-0000-0000-000000000002',
     'admin',
     'director@vianu.ro',
     'Adrian', 'Neagu',
     '$2a$10$AgrFyrZVE6ZRRSXt46/eHepzjgYkWMTxQAB7b6QU83l2NnNDrvAXW',
     now(), now());

-- Teachers
INSERT INTO users (id, school_id, role, email, first_name, last_name, password_hash, activated_at, provisioned_by) VALUES
    ('b2000000-0000-0000-0000-000000000010',
     'a0000000-0000-0000-0000-000000000002',
     'teacher', 'mihai.stanescu@vianu.ro',
     'Mihai', 'Stănescu',
     '$2a$10$AgrFyrZVE6ZRRSXt46/eHepzjgYkWMTxQAB7b6QU83l2NnNDrvAXW',
     now(), 'b2000000-0000-0000-0000-000000000001'),
    ('b2000000-0000-0000-0000-000000000011',
     'a0000000-0000-0000-0000-000000000002',
     'teacher', 'laura.georgescu@vianu.ro',
     'Laura', 'Georgescu',
     '$2a$10$AgrFyrZVE6ZRRSXt46/eHepzjgYkWMTxQAB7b6QU83l2NnNDrvAXW',
     now(), 'b2000000-0000-0000-0000-000000000001');

-- Class 10A
INSERT INTO classes (id, school_id, school_year_id, name, education_level, grade_number, homeroom_teacher_id) VALUES
    ('f2000000-0000-0000-0000-000000000001',
     'a0000000-0000-0000-0000-000000000002',
     'e0000000-0000-0000-0000-000000000002',
     '10A', 'high', 10,
     'b2000000-0000-0000-0000-000000000010');

-- Subjects
INSERT INTO subjects (id, school_id, name, short_name, education_level, has_thesis) VALUES
    ('f2000000-0000-0000-0000-000000000001', 'a0000000-0000-0000-0000-000000000002', 'Limba și literatura română', 'ROM', 'high', true),
    ('f2000000-0000-0000-0000-000000000002', 'a0000000-0000-0000-0000-000000000002', 'Matematică', 'MAT', 'high', true),
    ('f2000000-0000-0000-0000-000000000003', 'a0000000-0000-0000-0000-000000000002', 'Informatică', 'INF', 'high', false),
    ('f2000000-0000-0000-0000-000000000004', 'a0000000-0000-0000-0000-000000000002', 'Fizică', 'FIZ', 'high', true);

INSERT INTO class_subject_teachers (school_id, class_id, subject_id, teacher_id, hours_per_week) VALUES
    ('a0000000-0000-0000-0000-000000000002', 'f2000000-0000-0000-0000-000000000001', 'f2000000-0000-0000-0000-000000000001', 'b2000000-0000-0000-0000-000000000011', 4),
    ('a0000000-0000-0000-0000-000000000002', 'f2000000-0000-0000-0000-000000000001', 'f2000000-0000-0000-0000-000000000002', 'b2000000-0000-0000-0000-000000000010', 4),
    ('a0000000-0000-0000-0000-000000000002', 'f2000000-0000-0000-0000-000000000001', 'f2000000-0000-0000-0000-000000000003', 'b2000000-0000-0000-0000-000000000010', 3),
    ('a0000000-0000-0000-0000-000000000002', 'f2000000-0000-0000-0000-000000000001', 'f2000000-0000-0000-0000-000000000004', 'b2000000-0000-0000-0000-000000000011', 3);

-- Students 10A
INSERT INTO users (id, school_id, role, email, first_name, last_name, activated_at, provisioned_by) VALUES
    ('b2000000-0000-0000-0000-000000000101', 'a0000000-0000-0000-0000-000000000002', 'student', 'vlad.petre@elev.vianu.ro', 'Vlad', 'Petre', now(), 'b2000000-0000-0000-0000-000000000001'),
    ('b2000000-0000-0000-0000-000000000102', 'a0000000-0000-0000-0000-000000000002', 'student', 'diana.radu@elev.vianu.ro', 'Diana', 'Radu', now(), 'b2000000-0000-0000-0000-000000000001'),
    ('b2000000-0000-0000-0000-000000000103', 'a0000000-0000-0000-0000-000000000002', 'student', 'cosmin.dragomir@elev.vianu.ro', 'Cosmin', 'Dragomir', now(), 'b2000000-0000-0000-0000-000000000001'),
    ('b2000000-0000-0000-0000-000000000104', 'a0000000-0000-0000-0000-000000000002', 'student', 'amalia.constantinescu@elev.vianu.ro', 'Amalia', 'Constantinescu', now(), 'b2000000-0000-0000-0000-000000000001');

INSERT INTO class_enrollments (school_id, class_id, student_id) VALUES
    ('a0000000-0000-0000-0000-000000000002', 'f2000000-0000-0000-0000-000000000001', 'b2000000-0000-0000-0000-000000000101'),
    ('a0000000-0000-0000-0000-000000000002', 'f2000000-0000-0000-0000-000000000001', 'b2000000-0000-0000-0000-000000000102'),
    ('a0000000-0000-0000-0000-000000000002', 'f2000000-0000-0000-0000-000000000001', 'b2000000-0000-0000-0000-000000000103'),
    ('a0000000-0000-0000-0000-000000000002', 'f2000000-0000-0000-0000-000000000001', 'b2000000-0000-0000-0000-000000000104');

-- Grades for 10A
INSERT INTO grades (school_id, student_id, class_id, subject_id, teacher_id, school_year_id, semester, numeric_grade, grade_date) VALUES
    ('a0000000-0000-0000-0000-000000000002', 'b2000000-0000-0000-0000-000000000101', 'f2000000-0000-0000-0000-000000000001', 'f2000000-0000-0000-0000-000000000002', 'b2000000-0000-0000-0000-000000000010', 'e0000000-0000-0000-0000-000000000002', 'I', 10, '2026-09-25'),
    ('a0000000-0000-0000-0000-000000000002', 'b2000000-0000-0000-0000-000000000101', 'f2000000-0000-0000-0000-000000000001', 'f2000000-0000-0000-0000-000000000002', 'b2000000-0000-0000-0000-000000000010', 'e0000000-0000-0000-0000-000000000002', 'I', 9, '2026-10-15'),
    ('a0000000-0000-0000-0000-000000000002', 'b2000000-0000-0000-0000-000000000102', 'f2000000-0000-0000-0000-000000000001', 'f2000000-0000-0000-0000-000000000001', 'b2000000-0000-0000-0000-000000000011', 'e0000000-0000-0000-0000-000000000002', 'I', 8, '2026-10-02');

-- ============================================================
-- SOURCE MAPPINGS (simulate SIIIR import traceability)
-- ============================================================

-- Schools mapped to SIIIR codes
INSERT INTO source_mappings (school_id, entity_type, entity_id, source_system, source_id, source_metadata) VALUES
    ('a0000000-0000-0000-0000-000000000001', 'school', 'a0000000-0000-0000-0000-000000000001', 'siiir', 'CJ-GIM-0042', '{"county": "CJ", "type": "gimnazial"}'),
    ('a0000000-0000-0000-0000-000000000002', 'school', 'a0000000-0000-0000-0000-000000000002', 'siiir', 'B-LIC-0118', '{"county": "B", "type": "liceal"}');

-- Students from School 1 mapped to SIIIR (simulated CNPs)
INSERT INTO source_mappings (school_id, entity_type, entity_id, source_system, source_id, source_metadata) VALUES
    ('a0000000-0000-0000-0000-000000000001', 'user', 'b1000000-0000-0000-0000-000000000101', 'siiir', '5180415123456', '{"form": "zi", "status": "inscris"}'),
    ('a0000000-0000-0000-0000-000000000001', 'user', 'b1000000-0000-0000-0000-000000000102', 'siiir', '6190522234567', '{"form": "zi", "status": "inscris"}'),
    ('a0000000-0000-0000-0000-000000000001', 'user', 'b1000000-0000-0000-0000-000000000201', 'siiir', '5140811345678', '{"form": "zi", "status": "inscris"}'),
    ('a0000000-0000-0000-0000-000000000001', 'user', 'b1000000-0000-0000-0000-000000000202', 'siiir', '6150203456789', '{"form": "zi", "status": "inscris"}');

-- Classes mapped to SIIIR
INSERT INTO source_mappings (school_id, entity_type, entity_id, source_system, source_id, source_metadata) VALUES
    ('a0000000-0000-0000-0000-000000000001', 'class', 'f1000000-0000-0000-0000-000000000001', 'siiir', 'CJ-GIM-0042:2A:2026', '{"level": "primar"}'),
    ('a0000000-0000-0000-0000-000000000001', 'class', 'f1000000-0000-0000-0000-000000000002', 'siiir', 'CJ-GIM-0042:6B:2026', '{"level": "gimnazial"}');

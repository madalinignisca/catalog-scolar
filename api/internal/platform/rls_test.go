// Package platform_test — rls_test.go contains table-driven integration tests
// that verify Row-Level Security (RLS) tenant isolation across all 17 RLS-enabled
// tables in the CatalogRO schema.
//
// # Why do we need these tests?
//
// CatalogRO is a multi-tenant application where every school (tenant) must only
// ever see its own data. The isolation is enforced at the database level via
// PostgreSQL Row-Level Security policies such as:
//
//	CREATE POLICY grades_tenant ON grades
//	    USING (school_id = current_setting('app.current_school_id')::uuid);
//
// If such a policy is accidentally dropped, broken, or missing a table entirely,
// a teacher at School A could read the catalog data of School B — a serious
// privacy and data-integrity breach. These tests catch that class of bug before
// it ever reaches production.
//
// # How each subtest works
//
//  1. Seed data for school 1 (the "visible" tenant).
//  2. Acquire a connection as the catalogro_app role (non-superuser, so RLS
//     policies are actually evaluated) with the tenant context set to school 1.
//     Query the table → expect > 0 rows.
//  3. Acquire a second connection with the tenant context set to school 2.
//     Run the SAME query → expect exactly 0 rows (cross-tenant data is hidden).
//
// # Requirements
//
//   - Docker must be running (testcontainers-go pulls postgres:17-alpine).
//   - The catalogro_app PostgreSQL role must exist (created by migrations).
//
// Run these tests:
//
//	go test ./internal/platform/ -v -run TestRLSIsolation -count=1 -timeout 180s
package platform_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/vlahsh/catalogro/api/internal/testutil"
)

// ---------------------------------------------------------------------------
// TestRLSIsolation
// ---------------------------------------------------------------------------

// TestRLSIsolation verifies that RLS policies correctly block cross-tenant
// data access for all 17 RLS-enabled tables in the CatalogRO schema.
//
// Tables under test (in schema dependency order):
//  1. users
//  2. school_years
//  3. classes
//  4. class_enrollments
//  5. subjects
//  6. class_subject_teachers
//  7. evaluation_configs
//  8. grades
//  9. absences
//
// 10. averages
// 11. descriptive_evaluations
// 12. sync_conflicts
// 13. audit_log
// 14. messages
// 15. message_recipients
// 16. parent_student_links
// 17. source_mappings
func TestRLSIsolation(t *testing.T) {
	// -----------------------------------------------------------------------
	// 1. Start (or reuse) the shared PostgreSQL 17 container.
	// -----------------------------------------------------------------------
	// StartPostgres launches a Docker container with all goose migrations
	// applied and returns a superuser *pgxpool.Pool. Subsequent calls within
	// the same test binary return the cached pool (no second container).
	pool := testutil.StartPostgres(t)

	// -----------------------------------------------------------------------
	// 2. Truncate all tables to start from a clean, empty state.
	// -----------------------------------------------------------------------
	// This guarantees that data seeded by a previous test run (e.g. from
	// TestMigrationsRun) does not pollute our assertions.
	testutil.TruncateAll(t, pool)

	// -----------------------------------------------------------------------
	// 3. Seed the two tenant schools plus their school years.
	// -----------------------------------------------------------------------
	// SeedSchools creates:
	//   - 2 districts (no RLS)
	//   - 2 schools (no RLS — they ARE the tenants)
	//   - 1 school year per school (school_years table, has RLS)
	//
	// We use deterministic UUIDs so test output is reproducible and easy to
	// grep in logs.
	school1ID, school2ID := testutil.SeedSchools(t, pool)

	// -----------------------------------------------------------------------
	// 4. Seed users (admin, secretary, teacher, parent, student) for school 1.
	// -----------------------------------------------------------------------
	// SeedUsers also creates a parent_student_links row for the parent+student
	// pair, so both "users" and "parent_student_links" are populated.
	users1 := testutil.SeedUsers(t, pool, school1ID)
	teacherID := users1["teacher"]
	studentID := users1["student"]
	adminID := users1["admin"]
	parentID := users1["parent"]

	// NOTE: We intentionally do NOT seed users for school 2. The whole point
	// of this test is to verify that school 2's app-role session sees zero
	// rows from every table. If we seeded school 2 data (users, school year,
	// etc.), those tables would return > 0 rows for school 2 — not because
	// RLS is broken, but simply because school 2 genuinely has data. By
	// keeping school 2 completely empty we get a clean, unambiguous signal:
	// any non-zero count from school 2's perspective means RLS is broken.

	// SeedSchools seeds a school_year for BOTH schools. Delete school 2's
	// school year now so that the school_years table is empty for school 2.
	// We use the superuser pool so that no RLS tenant context is required
	// for this administrative cleanup step.
	//
	// deterministicTestID mirrors the seed.go deterministicID logic, so
	// "school-year-2" produces the same UUID that SeedSchools used.
	if _, err := pool.Exec(context.Background(), // nosemgrep: rls-missing-tenant-context
		`DELETE FROM school_years WHERE id = $1`,
		deterministicTestID("school-year-2"),
	); err != nil {
		t.Fatalf("TestRLSIsolation: delete school 2 school year: %v", err)
	}

	// -----------------------------------------------------------------------
	// 5. Seed a class (with subject, enrollment, and teacher assignment) for school 1.
	// -----------------------------------------------------------------------
	// SeedClass creates:
	//   - classes row
	//   - subjects row
	//   - class_enrollments row
	//   - class_subject_teachers row
	classID := testutil.SeedClass(t, pool, school1ID, teacherID)

	// -----------------------------------------------------------------------
	// 6. Derive the school year and subject IDs.
	// -----------------------------------------------------------------------
	// SeedClass and SeedSchools use deterministic IDs seeded from well-known
	// strings. We reconstruct them here using the same naming convention so
	// that INSERT statements below can reference them without extra DB queries.
	//
	// See seed.go deterministicID function for the naming scheme.
	// school1's suffix is the first 8 hex chars of school1ID.
	suffix1 := school1ID.String()[:8]
	subjectID := deterministicTestID("subject-" + suffix1)

	// The school year for school 1 is always "school-year-1" per seed.go.
	schoolYear1ID := deterministicTestID("school-year-1")

	// -----------------------------------------------------------------------
	// 7. Insert additional rows into tables that SeedClass does not populate.
	// -----------------------------------------------------------------------
	// Several RLS-enabled tables (evaluation_configs, grades, absences, etc.)
	// need at least one row so that the school-1 query returns > 0.
	//
	// IMPORTANT — why we use pool.Exec (superuser) rather than AcquireWithTenant:
	// The superuser role in PostgreSQL bypasses RLS policies entirely, so we
	// do not need to set app.current_school_id for the INSERT to succeed.
	// The school_id column value in each row is what gives the row its tenant
	// ownership — when the app role later queries with school_id = school1ID as
	// tenant context, RLS will filter and return exactly those rows.
	//
	// Using pool.Exec also avoids the double-release bug: AcquireWithTenant
	// registers a t.Cleanup that calls conn.Release(); calling conn.Release()
	// manually a second time causes a panic in pgxpool's resource tracker.
	ctx := context.Background()

	// ------------------------------------------------------------------
	// 7a. evaluation_configs: one row for school 1 / high / 2025-2026.
	// ------------------------------------------------------------------
	// evaluation_configs tracks how grades are calculated (numeric vs
	// qualifier, thesis weight, etc.) per school, education level, and year.
	if _, err := pool.Exec(ctx, // nosemgrep: rls-missing-tenant-context
		`INSERT INTO evaluation_configs
			(id, school_id, education_level, school_year_id,
				use_qualifiers, min_grade, max_grade, thesis_weight, min_grades_sem, rounding_rule)
		VALUES ($1, $2, 'high'::education_level, $3,
			false, 1, 10, 0.25, 3, 'standard')
		ON CONFLICT DO NOTHING`,
		deterministicTestID("eval-config-1"),
		school1ID,
		schoolYear1ID,
	); err != nil {
		t.Fatalf("TestRLSIsolation: insert evaluation_configs: %v", err)
	}

	// ------------------------------------------------------------------
	// 7b. grades: one numeric grade for the student in school 1.
	// ------------------------------------------------------------------
	// grades stores individual marks (note) given by a teacher to a student
	// for a subject. It references classes, subjects, users, and school_years.
	if _, err := pool.Exec(ctx, // nosemgrep: rls-missing-tenant-context
		`INSERT INTO grades
			(id, school_id, student_id, class_id, subject_id, teacher_id,
				school_year_id, semester, numeric_grade, is_thesis, grade_date)
		VALUES ($1, $2, $3, $4, $5, $6,
			$7, 'I'::semester, 9, false, $8)
		ON CONFLICT DO NOTHING`,
		deterministicTestID("grade-1"),
		school1ID,
		studentID,
		classID,
		subjectID,
		teacherID,
		schoolYear1ID,
		time.Date(2025, 10, 15, 0, 0, 0, 0, time.UTC),
	); err != nil {
		t.Fatalf("TestRLSIsolation: insert grades: %v", err)
	}

	// ------------------------------------------------------------------
	// 7c. absences: one absence record for the student in school 1.
	// ------------------------------------------------------------------
	// absences tracks each individual missed lesson (absență). It references
	// the same set of FKs as grades.
	if _, err := pool.Exec(ctx, // nosemgrep: rls-missing-tenant-context
		`INSERT INTO absences
			(id, school_id, student_id, class_id, subject_id, teacher_id,
				school_year_id, semester, absence_date, period_number, absence_type)
		VALUES ($1, $2, $3, $4, $5, $6,
			$7, 'I'::semester, $8, 1, 'unexcused'::absence_type)
		ON CONFLICT DO NOTHING`,
		deterministicTestID("absence-1"),
		school1ID,
		studentID,
		classID,
		subjectID,
		teacherID,
		schoolYear1ID,
		time.Date(2025, 10, 20, 0, 0, 0, 0, time.UTC),
	); err != nil {
		t.Fatalf("TestRLSIsolation: insert absences: %v", err)
	}

	// ------------------------------------------------------------------
	// 7d. averages: computed semester average for the student.
	// ------------------------------------------------------------------
	// averages is a denormalised cache that stores the computed and/or
	// manually-closed semester average (medie semestrială) per student/subject.
	if _, err := pool.Exec(ctx, // nosemgrep: rls-missing-tenant-context
		`INSERT INTO averages
			(id, school_id, student_id, class_id, subject_id,
				school_year_id, semester, computed_value, is_closed)
		VALUES ($1, $2, $3, $4, $5,
			$6, 'I'::semester, 9.00, false)
		ON CONFLICT DO NOTHING`,
		deterministicTestID("average-1"),
		school1ID,
		studentID,
		classID,
		subjectID,
		schoolYear1ID,
	); err != nil {
		t.Fatalf("TestRLSIsolation: insert averages: %v", err)
	}

	// ------------------------------------------------------------------
	// 7e. descriptive_evaluations: a text evaluation (primary school style).
	// ------------------------------------------------------------------
	// descriptive_evaluations hold free-text progress descriptions used
	// instead of numeric grades for primary-school students (calificative-free
	// narrative evaluations). They reference the same FK chain as grades.
	if _, err := pool.Exec(ctx, // nosemgrep: rls-missing-tenant-context
		`INSERT INTO descriptive_evaluations
			(id, school_id, student_id, class_id, subject_id, teacher_id,
				school_year_id, semester, content)
		VALUES ($1, $2, $3, $4, $5, $6,
			$7, 'I'::semester, 'Elevul demonstrează o bună înțelegere a materiei.')
		ON CONFLICT DO NOTHING`,
		deterministicTestID("desc-eval-1"),
		school1ID,
		studentID,
		classID,
		subjectID,
		teacherID,
		schoolYear1ID,
	); err != nil {
		t.Fatalf("TestRLSIsolation: insert descriptive_evaluations: %v", err)
	}

	// ------------------------------------------------------------------
	// 7f. sync_conflicts: a conflict record for an offline sync operation.
	// ------------------------------------------------------------------
	// sync_conflicts records situations where a client submitted a mutation
	// that conflicted with the server state. We insert a minimal row.
	if _, err := pool.Exec(ctx, // nosemgrep: rls-missing-tenant-context
		`INSERT INTO sync_conflicts
			(id, school_id, entity_type, entity_id, client_version, server_version, resolution)
		VALUES ($1, $2, 'grade', $3, '{"grade": 8}'::jsonb, '{"grade": 9}'::jsonb, 'server_wins')
		ON CONFLICT DO NOTHING`,
		deterministicTestID("sync-conflict-1"),
		school1ID,
		deterministicTestID("grade-1"), // references the grade we inserted above
	); err != nil {
		t.Fatalf("TestRLSIsolation: insert sync_conflicts: %v", err)
	}

	// ------------------------------------------------------------------
	// 7g. audit_log: an immutable audit entry for school 1.
	// ------------------------------------------------------------------
	// audit_log records all data-changing actions for compliance. It uses a
	// BIGINT GENERATED ALWAYS AS IDENTITY primary key (not UUID), so we only
	// specify school_id, user_id, action, entity_type, entity_id.
	//
	// NOTE: audit_log does NOT declare a foreign key on school_id → schools,
	// so the superuser INSERT succeeds without a tenant context. RLS is still
	// enforced for the app-role SELECT in the subtests below.
	if _, err := pool.Exec(ctx, // nosemgrep: rls-missing-tenant-context
		`INSERT INTO audit_log
			(school_id, user_id, action, entity_type, entity_id, new_values)
		VALUES ($1, $2, 'grade.create', 'grade', $3, '{"grade": 9}'::jsonb)`,
		school1ID,
		adminID,
		deterministicTestID("grade-1"),
	); err != nil {
		t.Fatalf("TestRLSIsolation: insert audit_log: %v", err)
	}

	// ------------------------------------------------------------------
	// 7h. messages + message_recipients: internal messaging.
	// ------------------------------------------------------------------
	// messages stores school announcements and direct messages. A message is
	// sent by a user (sender_id) and can be addressed to one or more recipients
	// via the message_recipients join table. Both tables have RLS.
	msgID := deterministicTestID("message-1")

	if _, err := pool.Exec(ctx, // nosemgrep: rls-missing-tenant-context
		`INSERT INTO messages
			(id, school_id, sender_id, subject, body, is_announcement)
		VALUES ($1, $2, $3, 'Test subiect', 'Test mesaj', false)
		ON CONFLICT DO NOTHING`,
		msgID,
		school1ID,
		teacherID,
	); err != nil {
		t.Fatalf("TestRLSIsolation: insert messages: %v", err)
	}

	if _, err := pool.Exec(ctx, // nosemgrep: rls-missing-tenant-context
		`INSERT INTO message_recipients
			(id, school_id, message_id, recipient_id)
		VALUES ($1, $2, $3, $4)
		ON CONFLICT DO NOTHING`,
		deterministicTestID("message-recipient-1"),
		school1ID,
		msgID,
		parentID,
	); err != nil {
		t.Fatalf("TestRLSIsolation: insert message_recipients: %v", err)
	}

	// ------------------------------------------------------------------
	// 7i. source_mappings: a SIIIR external ID mapping for the student.
	// ------------------------------------------------------------------
	// source_mappings links internal entity UUIDs to external system IDs
	// (e.g. SIIIR student codes, OneRoster sourcedId). The entity_id is
	// the student UUID we already have.
	if _, err := pool.Exec(ctx, // nosemgrep: rls-missing-tenant-context
		`INSERT INTO source_mappings
			(id, school_id, entity_type, entity_id, source_system, source_id)
		VALUES ($1, $2, 'user', $3, 'siiir', 'SIIIR-12345')
		ON CONFLICT DO NOTHING`,
		deterministicTestID("source-mapping-1"),
		school1ID,
		studentID,
	); err != nil {
		t.Fatalf("TestRLSIsolation: insert source_mappings: %v", err)
	}

	// -----------------------------------------------------------------------
	// 8. Define the table subtests.
	// -----------------------------------------------------------------------
	// Each entry in this slice describes one RLS-enabled table.
	//   - name:  the PostgreSQL table name (used as the subtest label).
	//   - query: a SELECT that returns rows for school 1's seeded data.
	//
	// The verification pattern is:
	//   - As school 1 (app role) → rows returned > 0
	//   - As school 2 (app role) → rows returned == 0  (RLS blocks cross-tenant)
	//
	// All queries use COUNT(*) to avoid type-scanning individual columns and
	// to make the queries robust against future schema changes.
	type tableCase struct {
		name  string // PostgreSQL table name
		query string // SELECT COUNT(*) query — must return > 0 for school 1
	}

	cases := []tableCase{
		{
			// users: all provisioned accounts for a school (admin, teacher, etc.).
			// SeedUsers inserted 5 users for school 1, so COUNT(*) should be 5.
			name:  "users",
			query: "SELECT COUNT(*) FROM users",
		},
		{
			// school_years: academic year configuration (dates, semester boundaries).
			// SeedSchools inserted 1 school year per school.
			name:  "school_years",
			query: "SELECT COUNT(*) FROM school_years",
		},
		{
			// classes: class groups such as "9A" or "5B".
			// SeedClass inserted 1 class for school 1.
			name:  "classes",
			query: "SELECT COUNT(*) FROM classes",
		},
		{
			// class_enrollments: links between students and their classes.
			// SeedClass inserted 1 enrollment for school 1.
			name:  "class_enrollments",
			query: "SELECT COUNT(*) FROM class_enrollments",
		},
		{
			// subjects: teaching subjects like "Matematică" or "Română".
			// SeedClass inserted 1 subject for school 1.
			name:  "subjects",
			query: "SELECT COUNT(*) FROM subjects",
		},
		{
			// class_subject_teachers: assigns a teacher to a subject in a class.
			// SeedClass inserted 1 assignment for school 1.
			name:  "class_subject_teachers",
			query: "SELECT COUNT(*) FROM class_subject_teachers",
		},
		{
			// evaluation_configs: grade calculation rules per level / year.
			// Inserted manually in step 7a above.
			name:  "evaluation_configs",
			query: "SELECT COUNT(*) FROM evaluation_configs",
		},
		{
			// grades: individual numeric marks (note) given by teachers.
			// Inserted manually in step 7b above.
			name:  "grades",
			query: "SELECT COUNT(*) FROM grades",
		},
		{
			// absences: individual missed lessons (absențe).
			// Inserted manually in step 7c above.
			name:  "absences",
			query: "SELECT COUNT(*) FROM absences",
		},
		{
			// averages: cached semester / annual averages (medii).
			// Inserted manually in step 7d above.
			name:  "averages",
			query: "SELECT COUNT(*) FROM averages",
		},
		{
			// descriptive_evaluations: free-text evaluations for primary school.
			// Inserted manually in step 7e above.
			name:  "descriptive_evaluations",
			query: "SELECT COUNT(*) FROM descriptive_evaluations",
		},
		{
			// sync_conflicts: records of offline-sync data conflicts.
			// Inserted manually in step 7f above.
			name:  "sync_conflicts",
			query: "SELECT COUNT(*) FROM sync_conflicts",
		},
		{
			// audit_log: immutable record of all data-changing actions.
			// Inserted manually in step 7g above.
			name:  "audit_log",
			query: "SELECT COUNT(*) FROM audit_log",
		},
		{
			// messages: internal announcements and direct messages.
			// Inserted manually in step 7h above.
			name:  "messages",
			query: "SELECT COUNT(*) FROM messages",
		},
		{
			// message_recipients: join table linking messages to their readers.
			// Inserted manually in step 7h above.
			name:  "message_recipients",
			query: "SELECT COUNT(*) FROM message_recipients",
		},
		{
			// parent_student_links: connects a parent account to a student account.
			// SeedUsers inserted 1 link for school 1.
			name:  "parent_student_links",
			query: "SELECT COUNT(*) FROM parent_student_links",
		},
		{
			// source_mappings: maps internal UUIDs to external system IDs (SIIIR, OneRoster).
			// Inserted manually in step 7i above.
			name:  "source_mappings",
			query: "SELECT COUNT(*) FROM source_mappings",
		},
	}

	// -----------------------------------------------------------------------
	// 9. Run one subtest per table.
	// -----------------------------------------------------------------------
	for _, tc := range cases {
		// Capture loop variable for use inside the closure (Go ≤ 1.21
		// closures capture by reference; explicit copy is the safe pattern).
		tc := tc

		t.Run(tc.name, func(t *testing.T) {
			// ---------------------------------------------------------------
			// 9a. Query as school 1 → expect at least 1 row.
			// ---------------------------------------------------------------
			// AcquireAsAppRole acquires a connection, sets the tenant context
			// to school1ID, and switches the active PostgreSQL role to
			// catalogro_app (a non-superuser). This forces RLS policies to be
			// evaluated — superusers bypass RLS silently.
			//
			// The connection is automatically cleaned up (RESET ROLE + RESET ALL
			// + Release) when the subtest ends, via the t.Cleanup callback
			// registered by AcquireAsAppRole.
			conn1 := testutil.AcquireAsAppRole(t, pool, school1ID)

			var count1 int64
			if err := conn1.QueryRow(ctx, tc.query).Scan(&count1); err != nil {
				t.Fatalf("table %q: query as school1: %v", tc.name, err)
			}

			// The query must return at least 1 row. If it returns 0, it means
			// either the seed data was not inserted correctly, or the RLS
			// policy is accidentally blocking inserts (which would be a
			// different bug to the one we are testing for here).
			if count1 == 0 {
				t.Errorf(
					"table %q: expected > 0 rows when querying as school1, got 0 — "+
						"check that seed/insert code above correctly set the tenant context "+
						"and that school1ID rows are actually present",
					tc.name,
				)
			}

			// ---------------------------------------------------------------
			// 9b. Query as school 2 → expect exactly 0 rows (RLS isolation).
			// ---------------------------------------------------------------
			// school 2 has no data in this table (we seeded only school 1).
			// With correct RLS policies the query must return 0. If it returns
			// any row, the RLS policy for this table is broken or missing.
			conn2 := testutil.AcquireAsAppRole(t, pool, school2ID)

			var count2 int64
			if err := conn2.QueryRow(ctx, tc.query).Scan(&count2); err != nil {
				t.Fatalf("table %q: query as school2: %v", tc.name, err)
			}

			// This is the core RLS assertion: school 2's app-role session must
			// see zero rows from school 1's data, because the RLS policy
			// filters on school_id = current_school_id() and school 2's
			// tenant context points to school2ID, not school1ID.
			if count2 != 0 {
				t.Errorf(
					"table %q: RLS ISOLATION FAILED — expected 0 rows when querying as school2, "+
						"but got %d. "+
						"This means either the RLS policy is missing, disabled, or incorrectly "+
						"allows cross-tenant data access for this table.",
					tc.name,
					count2,
				)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// deterministicTestID
// ---------------------------------------------------------------------------

// deterministicTestID produces a deterministic V5 UUID from the given label.
// It mirrors the deterministicID function in testutil/seed.go so that this
// test file can reconstruct the same IDs that the seed helpers created,
// without requiring the seed helpers to export that function.
//
// The label is prefixed with "catalogro-test-" for namespace consistency —
// exactly as the seed helpers do. Using the same prefix means the same label
// produces the same UUID in both places.
//
// Example:
//
//	deterministicTestID("grade-1") → always the same UUID
func deterministicTestID(label string) uuid.UUID {
	// uuid.NameSpaceURL is the namespace UUID for URL-based hashing.
	// uuid.NewSHA1 computes a V5 UUID as SHA-1("catalogro-test-<label>").
	return uuid.NewSHA1(uuid.NameSpaceURL, []byte("catalogro-test-"+label))
}

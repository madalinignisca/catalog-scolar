// Package siiir contains tests for the SIIIR mapper.
//
// These tests verify that raw SIIIR CSV data is correctly converted into
// internal CatalogRO entities. Each test function is table-driven, meaning
// we define a list of input/expected pairs and loop over them — this makes
// it easy to add new cases in the future without rewriting test logic.
package siiir

import (
	"strings"
	"testing"
)

// TestMapStudent_NameNormalization checks that student first and last names are
// cleaned up into proper title case, including compound hyphenated names and
// names with extra whitespace.
//
// Why this matters: SIIIR CSV exports often contain names in ALL-CAPS or
// mixed case (e.g. "ion POPESCU"). We must normalise them before storing in
// the database, so that the UI always shows "Ion Popescu" to teachers.
func TestMapStudent_NameNormalization(t *testing.T) {
	// Each test case describes one student row from a SIIIR CSV file.
	// We test last name and first name separately to keep assertions clear.
	cases := []struct {
		name          string // human-readable description of this test case
		inputLast     string // raw last name as it arrives from the CSV
		inputFirst    string // raw first name as it arrives from the CSV
		expectedLast  string // what we expect after normalization
		expectedFirst string // what we expect after normalization
	}{
		{
			// Standard ALL-CAPS last name + mixed-case first name.
			// SIIIR frequently exports last names in upper case.
			name:          "all-caps last name and mixed-case first name",
			inputLast:     "ion POPESCU",
			inputFirst:    "MARIA",
			expectedLast:  "Ion Popescu",
			expectedFirst: "Maria",
		},
		{
			// Compound hyphenated first name — common in Romanian (e.g. Ana-Maria).
			// The hyphen must be preserved and each part capitalised individually.
			name:          "compound hyphenated first name",
			inputLast:     "IONESCU",
			inputFirst:    "ana-maria",
			expectedLast:  "Ionescu",
			expectedFirst: "Ana-Maria",
		},
		{
			// Leading and trailing whitespace — happens when CSV cells have
			// accidental spaces around the value.
			name:          "whitespace trimming on last name",
			inputLast:     "  ION  ",
			inputFirst:    "ELENA",
			expectedLast:  "Ion",
			expectedFirst: "Elena",
		},
	}

	for _, tc := range cases {
		// Run each case as a named sub-test so failures show the case name.
		t.Run(tc.name, func(t *testing.T) {
			// Create a FRESH mapper for every test case.
			// This is important: the mapper tracks seen CNPs to detect duplicates.
			// If we reused a single mapper, the second call with the same CNP
			// would return a "duplicate" error instead of testing name normalization.
			m := NewMapper()

			// Build a minimal Student record — only the fields our test cares about.
			// We assign a unique CNP per case so dedup logic never fires.
			s := &Student{
				CNP:       "1234567890" + tc.name[:1], // unique enough for this test
				LastName:  tc.inputLast,
				FirstName: tc.inputFirst,
				ClassName: "5A", // arbitrary, not under test here
			}

			// Call the mapper — we expect no error for valid data.
			got, err := m.MapStudent(s)
			if err != nil {
				t.Fatalf("MapStudent() unexpected error: %v", err)
			}

			// Verify last name normalization.
			if got.LastName != tc.expectedLast {
				t.Errorf("LastName: got %q, want %q", got.LastName, tc.expectedLast)
			}

			// Verify first name normalization.
			if got.FirstName != tc.expectedFirst {
				t.Errorf("FirstName: got %q, want %q", got.FirstName, tc.expectedFirst)
			}
		})
	}
}

// TestMapStudent_ClassNormalization verifies that class name strings are
// trimmed and have internal spaces collapsed.
//
// Why this matters: teachers sometimes type class names as "5 A" or " 10B "
// (with extra spaces). We normalise these so that database lookups and
// display are consistent.
//
// Note: Roman-numeral conversion (e.g. "IX-B" → "9B") is NOT tested here
// because the current implementation does not implement that conversion.
func TestMapStudent_ClassNormalization(t *testing.T) {
	cases := []struct {
		name           string // description of this case
		inputClass     string // raw class string from CSV
		expectedClass  string // expected normalised class name
	}{
		{
			// Single space between grade number and section letter.
			// Common in older SIIIR exports.
			name:          "space between number and letter",
			inputClass:    "5 A",
			expectedClass: "5A",
		},
		{
			// Leading/trailing whitespace combined with correct interior format.
			name:          "trim outer whitespace",
			inputClass:    "  10B ",
			expectedClass: "10B",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// Fresh mapper per case — avoids any cross-case interference.
			m := NewMapper()

			s := &Student{
				CNP:       "9999999999", // unique CNP so dedup never triggers
				LastName:  "Ionescu",
				FirstName: "Maria",
				ClassName: tc.inputClass, // the value under test
			}

			got, err := m.MapStudent(s)
			if err != nil {
				t.Fatalf("MapStudent() unexpected error: %v", err)
			}

			// Verify that the ClassName in the returned MappedUser is normalised.
			if got.ClassName != tc.expectedClass {
				t.Errorf("ClassName: got %q, want %q", got.ClassName, tc.expectedClass)
			}
		})
	}
}

// TestMapStudent_Deduplication verifies that importing the same student twice
// within a single batch is detected and rejected with a clear error.
//
// Why this matters: SIIIR CSV files sometimes contain duplicate rows due to
// data-entry errors at the school level. We must catch these before inserting
// into the database to avoid constraint violations and corrupted class lists.
// The dedup check is scoped to a SINGLE mapper instance (= one import batch).
func TestMapStudent_Deduplication(t *testing.T) {
	// Use ONE mapper for both calls — this simulates processing two rows from
	// the same CSV file. The mapper remembers which CNPs it has already seen.
	m := NewMapper()

	// Build a student record that will be used for both import attempts.
	// The CNP is the unique national identifier (like a social-security number).
	s := &Student{
		CNP:       "1234567890123",
		LastName:  "Popescu",
		FirstName: "Ion",
		ClassName: "7B",
	}

	// --- First call: should succeed ---
	_, err := m.MapStudent(s)
	if err != nil {
		t.Fatalf("first MapStudent() call: unexpected error: %v", err)
	}

	// --- Second call with the same CNP: must return an error ---
	_, err = m.MapStudent(s)
	if err == nil {
		t.Fatal("second MapStudent() call: expected a duplicate error but got nil")
	}

	// The error message must mention "duplicate CNP/ID in batch" so that the
	// import pipeline can present a meaningful message to the secretary/admin.
	const wantSubstr = "duplicate CNP/ID in batch"
	if !strings.Contains(err.Error(), wantSubstr) {
		t.Errorf("error message %q does not contain %q", err.Error(), wantSubstr)
	}
}

// TestMapStudent_MissingCNP verifies that students without a CNP (national
// identification number) still get imported, but receive a synthetic source ID
// prefixed with "nokey:" so that operators can identify and fix them later.
//
// Why this matters: early SIIIR exports for primary-school students sometimes
// omitted the CNP column. We must still import these students (they exist!) but
// flag them clearly so the secretary knows they need manual review.
func TestMapStudent_MissingCNP(t *testing.T) {
	m := NewMapper()

	// Student with an intentionally empty CNP.
	// The mapper should fall back to a synthetic ID built from name + class.
	s := &Student{
		CNP:       "", // deliberately missing
		LastName:  "Dumitrescu",
		FirstName: "Andrei",
		ClassName: "3C",
	}

	got, err := m.MapStudent(s)
	if err != nil {
		t.Fatalf("MapStudent() unexpected error for missing CNP: %v", err)
	}

	// The SourceMapping.SourceID must start with "nokey:" to indicate that
	// this record has no reliable external key and needs manual verification.
	const wantPrefix = "nokey:"
	if !strings.HasPrefix(got.SourceMapping.SourceID, wantPrefix) {
		t.Errorf(
			"SourceMapping.SourceID = %q; expected it to start with %q",
			got.SourceMapping.SourceID,
			wantPrefix,
		)
	}
}

// Package siiir contains the parser and format definitions for SIIIR CSV exports.
// This file contains unit tests for the parser functions.
//
// SIIIR (Sistemul Informatic Integrat al Învățământului din România) is the
// Romanian national school information system. Schools export student data as
// CSV files, but the format has changed across school years. These tests verify
// that our parser handles both the 2024 and 2025 formats correctly.
package siiir

import (
	"strings"
	"testing"
)

// ---------------------------------------------------------------------------
// Test 1: TestParseCSV_2024Format
// ---------------------------------------------------------------------------
//
// The 2024-v1 format uses semicolons as delimiters and the following column order:
//   Col 0: CNP (national ID number, all digits)
//   Col 1: LastName
//   Col 2: FirstName
//   Col 3: ClassName  (e.g. "5A", "9B")
//   Col 4: Form       (forma de învățământ: zi, seral, etc.)
//   Col 5: Status     (înscris, retras, transferat)
//   Col 6: BirthDate
//   Col 7: Gender
//
// We pass a ready-made ColumnMapping directly to ParseStudents so we don't
// need to worry about the Windows-1250 encoding check in DetectFormat.
// The test checks that 3 data rows produce 3 Student records with correct values.

func TestParseCSV_2024Format(t *testing.T) {
	// mapping2024 mirrors KnownVersions[0] (2024-v1). We build it manually so
	// the test is self-contained and doesn't depend on KnownVersions ordering.
	mapping2024 := &ColumnMapping{
		Version:   "2024-v1",
		Encoding:  "Windows-1250",
		Delimiter: ';',
		Columns: map[string]int{
			FieldStudentCNP:       0,
			FieldStudentLastName:  1,
			FieldStudentFirstName: 2,
			FieldStudentClass:     3,
			FieldStudentForm:      4,
			FieldStudentStatus:    5,
			FieldStudentBirthDate: 6,
			FieldStudentGender:    7,
		},
	}

	// csvData is a semicolon-delimited CSV that looks like a real 2024 SIIIR export.
	// Row 1: a header row — CNP cell contains non-digit text so isHeaderRow() skips it.
	// Rows 2-4: three student data rows.
	// Using plain UTF-8 here is intentional: ParseStudents does not re-check
	// encoding, it only uses the Delimiter from the mapping.
	const csvData = `CNP;NumeFamilie;Prenume;Clasa;Forma;Status;DataNasterii;Sex
1234567890123;Popescu;Ion;5A;zi;inscris;2012-03-15;M
9876543210987;Ionescu;Maria;5A;zi;inscris;2012-07-22;F
5550000111222;Georgescu;Andrei;9B;zi;inscris;2009-11-01;M`

	// Table of expected results, one entry per data row.
	wantStudents := []struct {
		cnp       string
		lastName  string
		firstName string
		class     string
		form      string
		status    string
		birthDate string
		gender    string
	}{
		{"1234567890123", "Popescu", "Ion", "5A", "zi", "inscris", "2012-03-15", "M"},
		{"9876543210987", "Ionescu", "Maria", "5A", "zi", "inscris", "2012-07-22", "F"},
		{"5550000111222", "Georgescu", "Andrei", "9B", "zi", "inscris", "2009-11-01", "M"},
	}

	// Parse the CSV using the 2024 column mapping.
	students, err := ParseStudents(strings.NewReader(csvData), mapping2024)
	if err != nil {
		// ParseStudents only errors on a completely unreadable header row.
		t.Fatalf("ParseStudents returned unexpected error: %v", err)
	}

	// Verify the right number of students was parsed.
	if len(students) != len(wantStudents) {
		t.Fatalf("got %d students, want %d", len(students), len(wantStudents))
	}

	// Verify each field of each student.
	for i, want := range wantStudents {
		got := students[i]

		// We use a small helper closure to reduce repetition.
		check := func(field, gotVal, wantVal string) {
			t.Helper()
			if gotVal != wantVal {
				t.Errorf("student[%d].%s = %q, want %q", i, field, gotVal, wantVal)
			}
		}

		check("CNP", got.CNP, want.cnp)
		check("LastName", got.LastName, want.lastName)
		check("FirstName", got.FirstName, want.firstName)
		check("ClassName", got.ClassName, want.class)
		check("Form", got.Form, want.form)
		check("Status", got.Status, want.status)
		check("BirthDate", got.BirthDate, want.birthDate)
		check("Gender", got.Gender, want.gender)

		// The Raw map should be populated for every student (used for source_metadata).
		if len(got.Raw) == 0 {
			t.Errorf("student[%d].Raw is empty, expected raw column data", i)
		}
	}
}

// ---------------------------------------------------------------------------
// Test 2: TestParseCSV_2025Format
// ---------------------------------------------------------------------------
//
// The 2025-v1 format changes two things compared to 2024:
//   - Comma delimiter instead of semicolon
//   - Different column order: CNP,LastName,FirstName,BirthDate,Gender,Class,Form,Status
//
// The key thing to verify here is that our parser correctly uses the column
// mapping — even though BirthDate/Gender moved earlier in the row, the values
// end up in the right struct fields because we look up column indices by name.

func TestParseCSV_2025Format(t *testing.T) {
	// mapping2025 mirrors KnownVersions[1] (2025-v1).
	mapping2025 := &ColumnMapping{
		Version:   "2025-v1",
		Encoding:  "UTF-8",
		Delimiter: ',',
		Columns: map[string]int{
			FieldStudentCNP:       0,
			FieldStudentLastName:  1,
			FieldStudentFirstName: 2,
			FieldStudentBirthDate: 3,
			FieldStudentGender:    4,
			FieldStudentClass:     5,
			FieldStudentForm:      6,
			FieldStudentStatus:    7,
		},
	}

	// csvData uses commas and the 2025 column order. Note that BirthDate and
	// Gender appear BEFORE Class/Form/Status compared to the 2024 format.
	const csvData = `CNP,LastName,FirstName,BirthDate,Gender,Class,Form,Status
1234567890123,Popescu,Ion,2012-03-15,M,5A,zi,inscris
9876543210987,Ionescu,Maria,2012-07-22,F,5A,zi,inscris
5550000111222,Georgescu,Andrei,2009-11-01,M,9B,zi,inscris`

	// Expected values: same logical data as the 2024 test, just parsed from
	// a differently-ordered CSV. This proves the mapping abstraction works.
	wantStudents := []struct {
		cnp       string
		lastName  string
		firstName string
		birthDate string
		gender    string
		class     string
		form      string
		status    string
	}{
		{"1234567890123", "Popescu", "Ion", "2012-03-15", "M", "5A", "zi", "inscris"},
		{"9876543210987", "Ionescu", "Maria", "2012-07-22", "F", "5A", "zi", "inscris"},
		{"5550000111222", "Georgescu", "Andrei", "2009-11-01", "M", "9B", "zi", "inscris"},
	}

	students, err := ParseStudents(strings.NewReader(csvData), mapping2025)
	if err != nil {
		t.Fatalf("ParseStudents returned unexpected error: %v", err)
	}

	if len(students) != len(wantStudents) {
		t.Fatalf("got %d students, want %d", len(students), len(wantStudents))
	}

	for i, want := range wantStudents {
		got := students[i]

		check := func(field, gotVal, wantVal string) {
			t.Helper()
			if gotVal != wantVal {
				t.Errorf("student[%d].%s = %q, want %q", i, field, gotVal, wantVal)
			}
		}

		check("CNP", got.CNP, want.cnp)
		check("LastName", got.LastName, want.lastName)
		check("FirstName", got.FirstName, want.firstName)
		check("BirthDate", got.BirthDate, want.birthDate)
		check("Gender", got.Gender, want.gender)
		check("ClassName", got.ClassName, want.class)
		check("Form", got.Form, want.form)
		check("Status", got.Status, want.status)
	}
}

// ---------------------------------------------------------------------------
// Test 3: TestDetectFormat
// ---------------------------------------------------------------------------
//
// DetectFormat reads the beginning of a CSV, detects encoding and delimiter,
// then matches against KnownVersions. The function is called before parsing
// in a typical workflow: first detect, then parse with the returned mapping.
//
// Important nuance: DetectFormat skips Windows-1250 versions when the input is
// valid UTF-8 (and vice-versa). Therefore:
//   - 2024-v1 can only be detected with non-UTF-8 bytes. We simulate this by
//     embedding a byte slice that contains an invalid UTF-8 sequence followed
//     by semicolon-delimited columns.
//   - 2025-v1 is UTF-8 + comma-delimited, so plain string works fine.
//   - An unknown format (e.g., tab-delimited with wrong column count) returns an error.

func TestDetectFormat(t *testing.T) {
	// build2024Sample creates a byte slice that looks like a Windows-1250
	// semicolon-delimited header row. We inject one byte that is invalid UTF-8
	// (0x80 is a continuation byte that can't start a UTF-8 sequence) to make
	// utf8.Valid() return false, which is required for the 2024-v1 path.
	//
	// The row must have at least 8 fields (indices 0-7) for maxColumnIndex to pass.
	build2024Sample := func() string {
		// 0x80 is an invalid UTF-8 start byte — this causes utf8.Valid to return
		// false, making DetectFormat treat the sample as Windows-1250.
		invalidByte := string([]byte{0x80})
		// Build a header row: 8 semicolon-separated columns. The first column
		// contains the invalid byte to trigger the encoding detection.
		return invalidByte + "CNP;NumeFamilie;Prenume;Clasa;Forma;Status;DataNasterii;Sex\n" +
			"1234567890123;Popescu;Ion;5A;zi;inscris;2012-03-15;M\n"
	}

	// sample2025 is a valid UTF-8 comma-delimited header that matches 2025-v1.
	const sample2025 = `CNP,LastName,FirstName,BirthDate,Gender,Class,Form,Status
1234567890123,Popescu,Ion,2012-03-15,M,5A,zi,inscris
`

	// sampleUnknown uses tab delimiter — no KnownVersion matches that, so an
	// error is expected.
	const sampleUnknown = "CNP\tLastName\tFirstName\n1234567890123\tPopescu\tIon\n"

	// Each test case specifies what to feed DetectFormat and what to expect back.
	tests := []struct {
		name        string // human-readable test case name
		input       string // CSV content to pass as io.ReadSeeker
		wantVersion string // expected ColumnMapping.Version (empty means error expected)
		wantErr     bool   // true if DetectFormat should return an error
	}{
		{
			name:        "2024-v1 format (semicolon, non-UTF-8)",
			input:       build2024Sample(),
			wantVersion: "2024-v1",
			wantErr:     false,
		},
		{
			name:        "2025-v1 format (comma, UTF-8)",
			input:       sample2025,
			wantVersion: "2025-v1",
			wantErr:     false,
		},
		{
			name:        "unknown format (tab-delimited)",
			input:       sampleUnknown,
			wantVersion: "",
			wantErr:     true,
		},
	}

	for _, tc := range tests {
		// tc is captured per iteration so subtests are independent.
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			// strings.Reader implements io.ReadSeeker (it has a Seek method),
			// which satisfies the DetectFormat signature.
			reader := strings.NewReader(tc.input)

			mapping, err := DetectFormat(reader)

			// Case 1: we expected an error.
			if tc.wantErr {
				if err == nil {
					t.Errorf("DetectFormat() returned nil error, wanted an error for input %q", tc.input[:minInt(40, len(tc.input))])
				}
				// Even if we got an error, mapping should be nil.
				if mapping != nil {
					t.Errorf("DetectFormat() returned non-nil mapping alongside error: %+v", mapping)
				}
				return // no further checks needed for error cases
			}

			// Case 2: we expected success.
			if err != nil {
				t.Fatalf("DetectFormat() unexpected error: %v", err)
			}
			if mapping == nil {
				t.Fatal("DetectFormat() returned nil mapping without error")
			}
			if mapping.Version != tc.wantVersion {
				t.Errorf("mapping.Version = %q, want %q", mapping.Version, tc.wantVersion)
			}
		})
	}
}

// minInt is a small helper used for safe string slicing in error messages.
// It returns the smaller of two integers.
// We use the name minInt to avoid shadowing Go 1.21+'s built-in min().
func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// ---------------------------------------------------------------------------
// Test 4: TestParseCSV_MalformedInput
// ---------------------------------------------------------------------------
//
// Real-world SIIIR exports sometimes contain:
//   - Rows with fewer columns than expected (truncated lines)
//   - Completely empty files
//   - Rows where the name fields are blank
//
// The parser is expected to:
//   - Never panic regardless of input quality
//   - Silently skip rows it cannot parse (partial results are okay)
//   - Return an empty slice (not an error) for an empty file (after the header)
//   - Return a non-nil slice even if some rows are skipped

func TestParseCSV_MalformedInput(t *testing.T) {
	// We reuse the 2025-v1 mapping for all sub-cases (comma-delimited, UTF-8).
	// The exact version doesn't matter — we're testing robustness, not format detection.
	mapping := &ColumnMapping{
		Version:   "2025-v1",
		Encoding:  "UTF-8",
		Delimiter: ',',
		Columns: map[string]int{
			FieldStudentCNP:       0,
			FieldStudentLastName:  1,
			FieldStudentFirstName: 2,
			FieldStudentBirthDate: 3,
			FieldStudentGender:    4,
			FieldStudentClass:     5,
			FieldStudentForm:      6,
			FieldStudentStatus:    7,
		},
	}

	tests := []struct {
		name         string // test case description
		input        string // CSV content (may be malformed)
		wantMinCount int    // minimum number of valid students we expect back
		wantMaxCount int    // maximum number we expect (upper bound)
		wantErr      bool   // true if ParseStudents itself should return a non-nil error
	}{
		{
			// A file with only a header row and no data rows at all.
			// ParseStudents should return an empty (but non-nil) slice without error.
			name:         "header only, no data rows",
			input:        "CNP,LastName,FirstName,BirthDate,Gender,Class,Form,Status\n",
			wantMinCount: 0,
			wantMaxCount: 0,
			wantErr:      false,
		},
		{
			// Mix of valid rows and rows that have too few columns. The parser
			// should skip the incomplete rows and return only the valid students.
			name: "incomplete rows mixed with valid rows",
			input: `CNP,LastName,FirstName,BirthDate,Gender,Class,Form,Status
1234567890123,Popescu,Ion,2012-03-15,M,5A,zi,inscris
9999999
5550000111222,Georgescu,Andrei,2009-11-01,M,9B,zi,inscris`,
			// Row "9999999" has only 1 column, so rowToStudent will return "" for
			// LastName/FirstName and the row will be skipped. We expect 2 students.
			wantMinCount: 2,
			wantMaxCount: 2,
			wantErr:      false,
		},
		{
			// Rows where LastName and FirstName are empty strings. The parser
			// returns an error from rowToStudent ("missing name fields") and skips them.
			name: "rows with missing name fields",
			input: `CNP,LastName,FirstName,BirthDate,Gender,Class,Form,Status
1234567890123,,,2012-03-15,M,5A,zi,inscris
9876543210987,,Maria,2012-07-22,F,5A,zi,inscris
5550000111222,Georgescu,Andrei,2009-11-01,M,9B,zi,inscris`,
			// Row 1: both names empty → skipped.
			// Row 2: LastName empty → skipped (rowToStudent checks LastName != "").
			// Row 3: valid → included.
			wantMinCount: 1,
			wantMaxCount: 1,
			wantErr:      false,
		},
		{
			// An entirely empty file (not even a header row).
			// csv.Reader.Read() will return io.EOF on the first call (header read),
			// so ParseStudents returns an error rather than an empty slice.
			name:         "empty file",
			input:        "",
			wantMinCount: 0,
			wantMaxCount: 0,
			wantErr:      true, // "read header: EOF"
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			// Wrap the raw string in a strings.Reader — satisfies io.Reader.
			students, err := ParseStudents(strings.NewReader(tc.input), mapping)

			// Check error expectation.
			if tc.wantErr && err == nil {
				t.Errorf("ParseStudents() returned nil error, expected an error")
			}
			if !tc.wantErr && err != nil {
				t.Errorf("ParseStudents() unexpected error: %v", err)
			}

			// If we expected an error, skip further count checks.
			if tc.wantErr {
				return
			}

			// Verify the student count is within expected bounds.
			count := len(students)
			if count < tc.wantMinCount || count > tc.wantMaxCount {
				t.Errorf("got %d students, want between %d and %d",
					count, tc.wantMinCount, tc.wantMaxCount)
			}

			// Sanity-check: none of the returned students should have empty names
			// (those rows should have been skipped by rowToStudent).
			for i, s := range students {
				if s.LastName == "" || s.FirstName == "" {
					t.Errorf("student[%d] has empty name: LastName=%q FirstName=%q",
						i, s.LastName, s.FirstName)
				}
			}
		})
	}
}

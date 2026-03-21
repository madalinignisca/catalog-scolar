package siiir

import (
	"bufio"
	"encoding/csv"
	"fmt"
	"io"
	"strings"
	"unicode/utf8"
)

// Parser reads SIIIR CSV exports and produces structured data.
// The mapping field is set after format detection via DetectFormat.
type Parser struct {
	Mapping *ColumnMapping
}

// DetectFormat reads the first few lines of a CSV to determine which
// SIIIR version it matches. Checks encoding, delimiter, and column count.
func DetectFormat(reader io.ReadSeeker) (*ColumnMapping, error) {
	// Read first 4KB to detect encoding and delimiter
	buf := make([]byte, 4096)
	n, err := reader.Read(buf)
	if err != nil && err != io.EOF {
		return nil, fmt.Errorf("read sample: %w", err)
	}
	sample := buf[:n]

	// Reset reader for subsequent parsing
	if _, err := reader.Seek(0, io.SeekStart); err != nil {
		return nil, fmt.Errorf("seek: %w", err)
	}

	// Detect encoding
	isUTF8 := utf8.Valid(sample)

	// Detect delimiter by counting occurrences in first line
	firstLine := string(sample)
	if idx := strings.IndexByte(firstLine, '\n'); idx > 0 {
		firstLine = firstLine[:idx]
	}

	delimiter := detectDelimiter(firstLine)

	// Try each known version
	for _, v := range KnownVersions {
		if isUTF8 && v.Encoding == "Windows-1250" {
			continue // encoding mismatch
		}
		if !isUTF8 && v.Encoding == "UTF-8" {
			continue
		}
		if delimiter != v.Delimiter {
			continue
		}

		// Count columns in first line
		fields := strings.Split(firstLine, string(delimiter))
		expectedCols := maxColumnIndex(v.Columns) + 1
		if len(fields) >= expectedCols {
			match := v // copy
			return &match, nil
		}
	}

	// Fallback: try to build a best-effort mapping
	return nil, fmt.Errorf("unrecognized SIIIR format (encoding=%v, delimiter=%q, cols=%d)",
		isUTF8, string(delimiter), strings.Count(firstLine, string(delimiter))+1)
}

// ParseStudents reads the CSV and returns structured student records.
func ParseStudents(reader io.Reader, mapping *ColumnMapping) ([]Student, error) {
	csvReader := csv.NewReader(reader)
	csvReader.Comma = mapping.Delimiter
	csvReader.LazyQuotes = true // SIIIR exports are inconsistent with quoting
	csvReader.TrimLeadingSpace = true

	var students []Student

	// Skip header row if present
	header, err := csvReader.Read()
	if err != nil {
		return nil, fmt.Errorf("read header: %w", err)
	}
	isHeader := isHeaderRow(header, mapping)

	if !isHeader {
		// First row was data, parse it
		if s, err := rowToStudent(header, mapping); err == nil {
			students = append(students, s)
		}
	}

	for {
		record, err := csvReader.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			continue // skip malformed rows
		}

		s, err := rowToStudent(record, mapping)
		if err != nil {
			continue // skip unparseable rows
		}
		students = append(students, s)
	}

	return students, nil
}

func rowToStudent(record []string, mapping *ColumnMapping) (Student, error) {
	s := Student{Raw: make(map[string]string)}

	get := func(field string) string {
		if idx, ok := mapping.Columns[field]; ok && idx < len(record) {
			return strings.TrimSpace(record[idx])
		}
		return ""
	}

	s.CNP = get(FieldStudentCNP)
	s.LastName = get(FieldStudentLastName)
	s.FirstName = get(FieldStudentFirstName)
	s.ClassName = get(FieldStudentClass)
	s.Form = get(FieldStudentForm)
	s.Status = get(FieldStudentStatus)
	s.BirthDate = get(FieldStudentBirthDate)
	s.Gender = get(FieldStudentGender)

	if s.LastName == "" || s.FirstName == "" {
		return s, fmt.Errorf("missing name fields")
	}

	// Store all raw columns for source_metadata
	for i, v := range record {
		s.Raw[fmt.Sprintf("col_%d", i)] = v
	}

	return s, nil
}

func detectDelimiter(line string) rune {
	counts := map[rune]int{
		';':  strings.Count(line, ";"),
		',':  strings.Count(line, ","),
		'\t': strings.Count(line, "\t"),
	}

	best := ','
	bestCount := 0
	for d, c := range counts {
		if c > bestCount {
			bestCount = c
			best = d
		}
	}
	return best
}

func maxColumnIndex(cols map[string]int) int {
	result := 0
	for _, idx := range cols {
		if idx > result {
			result = idx
		}
	}
	return result
}

func isHeaderRow(record []string, mapping *ColumnMapping) bool {
	// Heuristic: if the CNP column contains non-digit text, it's probably a header
	if idx, ok := mapping.Columns[FieldStudentCNP]; ok && idx < len(record) {
		val := strings.TrimSpace(record[idx])
		if val == "" || !isDigits(val) {
			return true
		}
	}
	return false
}

func isDigits(s string) bool {
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return s != ""
}

// Ensure bufio is importable (used by callers for encoding conversion)
var _ = bufio.NewReader

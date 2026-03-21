package siiir

import (
	"encoding/json"
	"fmt"
	"strings"
)

// MappedUser represents an internal user ready to be persisted,
// along with its source mapping for traceability.
type MappedUser struct {
	FirstName     string
	LastName      string
	Role          string // "student" or "teacher"
	ClassName     string // for students: which class to enroll in
	SourceMapping SourceMapping
}

// SourceMapping is the data for the source_mappings table.
type SourceMapping struct {
	EntityType     string          // "user", "class", etc.
	SourceSystem   string          // always "siiir"
	SourceID       string          // CNP or SIIIR-specific ID
	SourceMetadata json.RawMessage // raw columns for audit/debugging
}

// Mapper converts SIIIR parsed data to internal CatalogRO entities.
type Mapper struct {
	// cnpToUserID tracks already-imported CNPs to prevent duplicates within a batch.
	cnpToUserID map[string]bool
}

// NewMapper creates a new SIIIR → CatalogRO mapper.
func NewMapper() *Mapper {
	return &Mapper{
		cnpToUserID: make(map[string]bool),
	}
}

// MapStudent converts a parsed SIIIR student to an internal user + source mapping.
// Returns an error if the student has invalid data.
func (m *Mapper) MapStudent(s *Student) (*MappedUser, error) {
	if s.LastName == "" || s.FirstName == "" {
		return nil, fmt.Errorf("student missing name: %q %q", s.LastName, s.FirstName)
	}

	// Normalize name: title case
	firstName := normalizeName(s.FirstName)
	lastName := normalizeName(s.LastName)

	// Build source metadata from raw columns
	metadata, _ := json.Marshal(s.Raw)

	// CNP as source_id (primary identifier in SIIIR)
	sourceID := s.CNP
	if sourceID == "" {
		// Fallback: generate synthetic ID from name + class (less reliable)
		sourceID = fmt.Sprintf("nokey:%s:%s:%s", lastName, firstName, s.ClassName)
	}

	// Check for duplicates within this batch
	if m.cnpToUserID[sourceID] {
		return nil, fmt.Errorf("duplicate CNP/ID in batch: %s", sourceID)
	}
	m.cnpToUserID[sourceID] = true

	return &MappedUser{
		FirstName: firstName,
		LastName:  lastName,
		Role:      "student",
		ClassName: normalizeClassName(s.ClassName),
		SourceMapping: SourceMapping{
			EntityType:     "user",
			SourceSystem:   "siiir",
			SourceID:       sourceID,
			SourceMetadata: metadata,
		},
	}, nil
}

// MapTeacher converts a parsed SIIIR teacher to an internal user + source mapping.
func (m *Mapper) MapTeacher(t *Teacher) (*MappedUser, error) {
	if t.LastName == "" || t.FirstName == "" {
		return nil, fmt.Errorf("teacher missing name")
	}

	metadata, _ := json.Marshal(t.Raw)

	sourceID := t.CNP
	if sourceID == "" {
		sourceID = fmt.Sprintf("nokey:%s:%s", normalizeName(t.LastName), normalizeName(t.FirstName))
	}

	if m.cnpToUserID[sourceID] {
		return nil, fmt.Errorf("duplicate CNP/ID in batch: %s", sourceID)
	}
	m.cnpToUserID[sourceID] = true

	return &MappedUser{
		FirstName: normalizeName(t.FirstName),
		LastName:  normalizeName(t.LastName),
		Role:      "teacher",
		SourceMapping: SourceMapping{
			EntityType:     "user",
			SourceSystem:   "siiir",
			SourceID:       sourceID,
			SourceMetadata: metadata,
		},
	}, nil
}

// normalizeName converts "POPESCU" or "popescu" to "Popescu".
func normalizeName(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return s
	}
	// Handle compound names: "Ana-Maria", "Ana Maria"
	parts := strings.FieldsFunc(s, func(r rune) bool { return r == ' ' || r == '-' })
	for i, p := range parts {
		if p != "" {
			parts[i] = strings.ToUpper(p[:1]) + strings.ToLower(p[1:])
		}
	}
	// Rejoin with original separator
	result := strings.Join(parts, " ")
	// Restore hyphens
	if strings.Contains(s, "-") {
		result = strings.ReplaceAll(result, " ", "-")
	}
	return result
}

// normalizeClassName converts class names: "5 A" → "5A", "IX-B" → "9B" (for matching).
// Keeps original if no normalization is needed.
func normalizeClassName(s string) string {
	s = strings.TrimSpace(s)
	s = strings.ReplaceAll(s, " ", "")
	return s
}

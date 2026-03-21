package portability

// StudentRecordPackage is a portable, self-contained export of a student's
// entire academic history. Designed to be imported by any OneRoster-compatible
// system, aligned with EHEIF (European Higher Education Interoperability Framework)
// principle: "data follows the student".
type StudentRecordPackage struct {
	Version         string          `json:"version"`              // "1.0"
	Standard        string          `json:"standard"`             // "catalogro-student-record"
	OneRosterCompat bool            `json:"oneroster_compatible"` // always true
	ExportedAt      string          `json:"exported_at"`          // ISO 8601
	ExportedBy      ExportedBy      `json:"exported_by"`
	Student         StudentIdentity `json:"student"`
	SchoolHistory   []SchoolRecord  `json:"school_history"`
}

// ExportedBy identifies the exporting school and system.
type ExportedBy struct {
	SchoolName string `json:"school_name"`
	SIIIRCode  string `json:"siiir_code,omitempty"`
	System     string `json:"system"` // "CatalogRO"
	SystemURL  string `json:"system_url,omitempty"`
}

// StudentIdentity holds the student's core identity data.
type StudentIdentity struct {
	SourcedID  string `json:"sourcedId"` // OneRoster sourcedId (our UUID)
	GivenName  string `json:"givenName"`
	FamilyName string `json:"familyName"`
	BirthDate  string `json:"birthDate,omitempty"` // ISO 8601 date
	Gender     string `json:"gender,omitempty"`
	// External identifiers (SIIIR, CNP hash, etc.)
	Identifiers []ExternalID `json:"identifiers,omitempty"`
}

// ExternalID is a reference to the student in an external system.
type ExternalID struct {
	System string `json:"system"` // "siiir", "cnp_hash"
	Value  string `json:"value"`
}

// SchoolRecord represents the student's time at one school.
type SchoolRecord struct {
	School  SchoolInfo       `json:"school"`
	Period  Period           `json:"period"`
	Classes []string         `json:"classes"` // class names in order: ["5B", "6B"]
	Records []AcademicRecord `json:"academic_records"`
}

// SchoolInfo identifies a school.
type SchoolInfo struct {
	Name      string `json:"name"`
	SIIIRCode string `json:"siiir_code,omitempty"`
	County    string `json:"county,omitempty"`
	City      string `json:"city,omitempty"`
}

// Period is a date range.
type Period struct {
	From string `json:"from"` // ISO 8601 date
	To   string `json:"to"`
}

// AcademicRecord holds one semester or year of results.
type AcademicRecord struct {
	SchoolYear string          `json:"year"`     // "2026-2027"
	ClassName  string          `json:"class"`    // "6B"
	Semester   string          `json:"semester"` // "I", "II", or "annual"
	Results    []SubjectResult `json:"results"`
	Absences   AbsenceSummary  `json:"absences"`
	Behavior   *string         `json:"behavior_note,omitempty"` // nota la purtare
}

// SubjectResult is a grade or qualifier for one subject.
type SubjectResult struct {
	Course    string   `json:"course"`              // "Matematică"
	Grade     *float64 `json:"grade,omitempty"`     // numeric average (middle/high)
	Qualifier *string  `json:"qualifier,omitempty"` // "FB"/"B"/"S"/"I" (primary)
	Type      string   `json:"type"`                // "semester_average", "annual_average", "thesis", "evaluation_nationale"
	Thesis    *float64 `json:"thesis,omitempty"`    // thesis grade if applicable
}

// AbsenceSummary aggregates absences for a period.
type AbsenceSummary struct {
	Total     int `json:"total"`
	Excused   int `json:"excused"`
	Unexcused int `json:"unexcused"`
}

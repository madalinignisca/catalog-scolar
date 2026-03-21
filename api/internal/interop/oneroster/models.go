package oneroster

// OneRoster 1.2 JSON Binding data models.
// Reference: https://www.imsglobal.org/oneroster-v12-final-specification

// Status represents the OneRoster status enum.
type Status string

const (
	StatusActive   Status = "active"
	StatusInactive Status = "tobedeleted"
)

// RoleType represents user roles in OneRoster.
type RoleType string

const (
	RoleStudent  RoleType = "student"
	RoleTeacher  RoleType = "teacher"
	RoleGuardian RoleType = "guardian"
	RoleAdmin    RoleType = "administrator"
)

// OrgType represents organization types.
type OrgType string

const (
	OrgSchool   OrgType = "school"
	OrgDistrict OrgType = "district"
)

// SessionType represents academic session types.
type SessionType string

const (
	SessionSchoolYear SessionType = "schoolYear"
	SessionTerm       SessionType = "term"
)

// GUIDRef is a reference to another entity by sourcedId.
type GUIDRef struct {
	SourcedID string `json:"sourcedId"`
	Type      string `json:"type"`
}

// Org represents a school or district.
type Org struct {
	SourcedID  string    `json:"sourcedId"`
	Status     Status    `json:"status"`
	Name       string    `json:"name"`
	Type       OrgType   `json:"type"`
	Identifier string    `json:"identifier,omitempty"` // SIIIR code
	Parent     *GUIDRef  `json:"parent,omitempty"`     // district for schools
}

// User represents a student, teacher, or guardian.
type User struct {
	SourcedID   string    `json:"sourcedId"`
	Status      Status    `json:"status"`
	GivenName   string    `json:"givenName"`
	FamilyName  string    `json:"familyName"`
	Email       string    `json:"email,omitempty"`
	Phone       string    `json:"phone,omitempty"`
	Role        RoleType  `json:"role"`
	Orgs        []GUIDRef `json:"orgs"`
	Identifiers []string  `json:"userIds,omitempty"` // external IDs
}

// AcademicSession represents a school year or semester.
type AcademicSession struct {
	SourcedID string      `json:"sourcedId"`
	Status    Status      `json:"status"`
	Title     string      `json:"title"`
	Type      SessionType `json:"type"`
	StartDate string      `json:"startDate"` // ISO 8601 date
	EndDate   string      `json:"endDate"`
	Parent    *GUIDRef    `json:"parent,omitempty"` // schoolYear for terms
	SchoolYear string    `json:"schoolYear"`
}

// Course represents a subject/discipline.
type Course struct {
	SourcedID string   `json:"sourcedId"`
	Status    Status   `json:"status"`
	Title     string   `json:"title"`
	CourseCode string  `json:"courseCode,omitempty"`
	Org       GUIDRef  `json:"org"` // school
}

// Class represents a class/group.
type Class struct {
	SourcedID string    `json:"sourcedId"`
	Status    Status    `json:"status"`
	Title     string    `json:"title"`
	ClassCode string    `json:"classCode,omitempty"`
	ClassType string    `json:"classType"` // "homeroom" or "scheduled"
	Course    GUIDRef   `json:"course"`
	School    GUIDRef   `json:"school"`
	Terms     []GUIDRef `json:"terms"`
}

// Enrollment links a user to a class.
type Enrollment struct {
	SourcedID string   `json:"sourcedId"`
	Status    Status   `json:"status"`
	User      GUIDRef  `json:"user"`
	Class     GUIDRef  `json:"class"`
	School    GUIDRef  `json:"school"`
	Role      RoleType `json:"role"`
}

// LineItem represents a gradable item (e.g., a subject in a semester).
type LineItem struct {
	SourcedID    string   `json:"sourcedId"`
	Status       Status   `json:"status"`
	Title        string   `json:"title"`
	Category     string   `json:"category"` // "term", "thesis", "final"
	AssignDate   string   `json:"assignDate"`
	DueDate      string   `json:"dueDate"`
	Class        GUIDRef  `json:"class"`
	GradingPeriod GUIDRef `json:"gradingPeriod"`
	ResultValueMin float64 `json:"resultValueMin"`
	ResultValueMax float64 `json:"resultValueMax"`
}

// Result represents a grade/score for a student on a line item.
type Result struct {
	SourcedID  string  `json:"sourcedId"`
	Status     Status  `json:"status"`
	Student    GUIDRef `json:"student"`
	LineItem   GUIDRef `json:"lineItem"`
	Score      float64 `json:"score"`
	ScoreDate  string  `json:"scoreDate"`
	Comment    string  `json:"comment,omitempty"`
	ScoreStatus string `json:"scoreStatus"` // "fully graded", "partially graded", "exempt"
}

// CollectionResponse wraps a list of entities in OneRoster JSON format.
type CollectionResponse[T any] struct {
	Data []T `json:"data"`
}

// SingleResponse wraps a single entity in OneRoster JSON format.
type SingleResponse[T any] struct {
	Data T `json:"data"`
}

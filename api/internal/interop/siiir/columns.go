package siiir

// ColumnMapping defines how SIIIR CSV columns map to internal fields.
// Versioned because SIIIR export format changes between school years.
type ColumnMapping struct {
	Version   string         `json:"version"`
	Columns   map[string]int `json:"columns"`   // field name → column index (0-based)
	Encoding  string         `json:"encoding"`  // "UTF-8", "Windows-1250"
	Delimiter rune           `json:"delimiter"` // ',', ';', '\t'
}

// Known SIIIR field names (stable across versions)
const (
	FieldStudentCNP       = "student_cnp"
	FieldStudentLastName  = "student_last_name"
	FieldStudentFirstName = "student_first_name"
	FieldStudentClass     = "student_class"
	FieldStudentForm      = "student_form"   // forma de învățământ (zi, seral, frecvență redusă)
	FieldStudentStatus    = "student_status" // înscris, retras, transferat
	FieldStudentBirthDate = "student_birth_date"
	FieldStudentGender    = "student_gender"
	FieldTeacherCNP       = "teacher_cnp"
	FieldTeacherLastName  = "teacher_last_name"
	FieldTeacherFirstName = "teacher_first_name"
	FieldTeacherSubject   = "teacher_subject"
	FieldTeacherNorm      = "teacher_norm" // norma didactică
	FieldClassName        = "class_name"
	FieldClassLevel       = "class_level"   // nivel: primar, gimnazial, liceal
	FieldClassProfile     = "class_profile" // profil liceu: real, uman, tehnic
	FieldSchoolCode       = "school_siiir_code"
)

// KnownVersions holds all recognized SIIIR export formats.
// New versions are added here when SIIIR changes their export format.
var KnownVersions = []ColumnMapping{
	{
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
	},
	{
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
	},
}

// Student represents a parsed student row from SIIIR CSV.
type Student struct {
	CNP       string `json:"cnp"`
	LastName  string `json:"last_name"`
	FirstName string `json:"first_name"`
	ClassName string `json:"class_name"`
	Form      string `json:"form"`
	Status    string `json:"status"`
	BirthDate string `json:"birth_date"`
	Gender    string `json:"gender"`
	// Raw stores all original columns for source_metadata
	Raw map[string]string `json:"raw"`
}

// Teacher represents a parsed teacher row from SIIIR CSV.
type Teacher struct {
	CNP       string            `json:"cnp"`
	LastName  string            `json:"last_name"`
	FirstName string            `json:"first_name"`
	Subject   string            `json:"subject"`
	Norm      string            `json:"norm"`
	Raw       map[string]string `json:"raw"`
}

package interop

import (
	"fmt"
	"io"
)

// ImportFormat identifies the source system format.
type ImportFormat string

const (
	FormatSIIIR     ImportFormat = "siiir"
	FormatOneRoster ImportFormat = "oneroster"
	FormatCSV       ImportFormat = "csv"
)

// ImportPreview is the result of parsing a file before confirmation.
type ImportPreview struct {
	Format      ImportFormat        `json:"format"`
	Students    int                 `json:"students"`
	Teachers    int                 `json:"teachers"`
	Classes     int                 `json:"classes"`
	Warnings    []string            `json:"warnings,omitempty"`
	RawEntities []map[string]string `json:"-"` // held in memory until confirmed
}

// Adapter defines the interface every import/export format must implement.
type Adapter interface {
	// Detect checks if the reader matches this format. Returns confidence 0-100.
	Detect(reader io.ReadSeeker) (int, error)

	// Preview parses the file and returns a summary without persisting anything.
	Preview(reader io.ReadSeeker) (*ImportPreview, error)

	// Format returns the adapter's format identifier.
	Format() ImportFormat
}

// Registry holds all registered adapters and selects the right one.
type Registry struct {
	adapters []Adapter
}

// NewRegistry creates a registry with all built-in adapters.
func NewRegistry() *Registry {
	return &Registry{
		adapters: []Adapter{
			// Registered in priority order (most specific first)
			// NewSIIIRAdapter(),
			// NewOneRosterAdapter(),
			// NewGenericCSVAdapter(),
		},
	}
}

// Register adds a new adapter to the registry.
func (r *Registry) Register(a Adapter) {
	r.adapters = append(r.adapters, a)
}

// Detect tries all adapters and returns the best match.
func (r *Registry) Detect(reader io.ReadSeeker) (Adapter, error) {
	var bestAdapter Adapter
	var bestScore int

	for _, a := range r.adapters {
		if _, err := reader.Seek(0, io.SeekStart); err != nil {
			return nil, fmt.Errorf("seek: %w", err)
		}

		score, err := a.Detect(reader)
		if err != nil {
			continue
		}
		if score > bestScore {
			bestScore = score
			bestAdapter = a
		}
	}

	if bestAdapter == nil {
		return nil, fmt.Errorf("no adapter matched the input file")
	}

	return bestAdapter, nil
}

// Get returns a specific adapter by format.
func (r *Registry) Get(format ImportFormat) (Adapter, error) {
	for _, a := range r.adapters {
		if a.Format() == format {
			return a, nil
		}
	}
	return nil, fmt.Errorf("no adapter registered for format %q", format)
}

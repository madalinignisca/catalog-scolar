package jobs

import (
	"context"
	"log/slog"

	"github.com/google/uuid"
	"github.com/riverqueue/river"
)

// ReportGenerationArgs is the payload for an async report generation job.
// Supports PDF catalog generation and ISJ/SIIIR export.
type ReportGenerationArgs struct {
	// ReportType is the kind of report: "catalog_pdf" or "isj_export".
	ReportType string `json:"report_type"`

	// SchoolID is the school this report is for.
	SchoolID uuid.UUID `json:"school_id"`

	// SchoolYearID is the school year to generate the report for.
	SchoolYearID uuid.UUID `json:"school_year_id"`

	// RequestedBy is the user who requested the report.
	RequestedBy uuid.UUID `json:"requested_by"`

	// Parameters holds report-specific options (e.g., class_id, semester).
	Parameters map[string]string `json:"parameters,omitempty"`
}

// Kind returns the unique job type identifier for River.
func (ReportGenerationArgs) Kind() string { return "report_generation" }

// InsertOpts returns default insert options for report jobs.
func (ReportGenerationArgs) InsertOpts() river.InsertOpts {
	return river.InsertOpts{Queue: river.QueueDefault}
}

// ReportWorker processes async report generation jobs.
type ReportWorker struct {
	river.WorkerDefaults[ReportGenerationArgs]
	Logger *slog.Logger
}

// Work processes a single report generation job.
// Currently logs the request — real PDF/CSV generation will be implemented
// when the report generation logic is added.
func (w *ReportWorker) Work(ctx context.Context, job *river.Job[ReportGenerationArgs]) error {
	w.Logger.Info("processing report generation job",
		"report_type", job.Args.ReportType,
		"school_id", job.Args.SchoolID,
		"requested_by", job.Args.RequestedBy,
		"job_id", job.ID,
	)

	// TODO: Generate the actual report based on ReportType.
	// - "catalog_pdf": query grades/averages, generate PDF, upload to MinIO
	// - "isj_export": query students/grades, generate SIIIR CSV, upload to MinIO
	// After generation, update the job metadata with the download URL.

	return nil
}

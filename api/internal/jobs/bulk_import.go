package jobs

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/riverqueue/river"

	"github.com/vlahsh/catalogro/api/db/generated"
	"github.com/vlahsh/catalogro/api/internal/interop/siiir"
)

// BulkImportArgs is the payload for an async SIIIR bulk import job.
// Enqueued when the ConfirmImport handler receives a batch larger than
// the synchronous threshold (200 users).
type BulkImportArgs struct {
	// ImportID is the import session identifier.
	ImportID uuid.UUID `json:"import_id"`

	// Users is the list of mapped users to provision.
	Users []siiir.MappedUser `json:"users"`

	// ProvisionedBy is the user who initiated the import.
	ProvisionedBy uuid.UUID `json:"provisioned_by"`
}

// Kind returns the unique job type identifier for River.
func (BulkImportArgs) Kind() string { return "bulk_import" }

// InsertOpts returns default insert options.
func (BulkImportArgs) InsertOpts() river.InsertOpts {
	return river.InsertOpts{Queue: river.QueueDefault}
}

// BulkImportWorker processes async bulk import jobs.
type BulkImportWorker struct {
	river.WorkerDefaults[BulkImportArgs]
	Queries *generated.Queries
	Logger  *slog.Logger
}

// Work provisions each user in the batch, creating source mappings for
// traceability. Individual failures are logged but don't abort the batch.
func (w *BulkImportWorker) Work(ctx context.Context, job *river.Job[BulkImportArgs]) error {
	w.Logger.Info("processing bulk import job",
		"import_id", job.Args.ImportID,
		"users", len(job.Args.Users),
		"job_id", job.ID,
	)

	imported := 0
	skipped := 0

	for i := range job.Args.Users {
		u := &job.Args.Users[i]

		// Generate synthetic email and activation token (same as sync path).
		syntheticEmail := fmt.Sprintf("%s@siiir.import", u.SourceMapping.SourceID)
		activationToken := uuid.New().String()
		siiirID := u.SourceMapping.SourceID

		provisionedUser, err := w.Queries.ProvisionUser(ctx, generated.ProvisionUserParams{
			FirstName:       u.FirstName,
			LastName:        u.LastName,
			Role:            generated.UserRole(u.Role),
			Email:           &syntheticEmail,
			SiiirStudentID:  &siiirID,
			ActivationToken: &activationToken,
			ProvisionedBy:   pgtype.UUID{Bytes: job.Args.ProvisionedBy, Valid: true},
		})
		if err != nil {
			w.Logger.Warn("bulk import: failed to provision user",
				"name", u.FirstName+" "+u.LastName, "error", err)
			skipped++
			continue
		}

		// Create source mapping for traceability.
		_, err = w.Queries.UpsertSourceMapping(ctx, generated.UpsertSourceMappingParams{
			EntityType:     u.SourceMapping.EntityType,
			EntityID:       provisionedUser.ID,
			SourceSystem:   u.SourceMapping.SourceSystem,
			SourceID:       u.SourceMapping.SourceID,
			SourceMetadata: u.SourceMapping.SourceMetadata,
		})
		if err != nil {
			w.Logger.Warn("bulk import: failed to create source mapping",
				"user_id", provisionedUser.ID, "error", err)
		}

		imported++
	}

	w.Logger.Info("bulk import job completed",
		"import_id", job.Args.ImportID,
		"imported", imported,
		"skipped", skipped,
		"job_id", job.ID,
	)

	return nil
}

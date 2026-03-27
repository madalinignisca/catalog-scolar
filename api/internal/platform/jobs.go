// Package platform — jobs.go sets up the River job queue client for async
// task processing in CatalogRO.
//
// River is a PostgreSQL-backed job queue for Go. It uses the same PostgreSQL
// database as the application, eliminating the need for a separate message
// broker (Redis, RabbitMQ, etc.). Jobs are durable, transactional, and
// survive server restarts.
//
// Use cases in CatalogRO:
//   - PDF catalog generation (async, can take 30+ seconds)
//   - ISJ/SIIIR export generation (async, large data sets)
//   - Bulk import processing (async, 1000+ user provisioning)
//   - Email notifications (async, Mailgun/SMTP delivery)
//   - Push notifications (async, Web Push VAPID delivery)
//
// Architecture:
//
//	Handler → river.Client.Insert(ctx, job) → PostgreSQL (river_job table)
//	                                                ↓
//	River Worker pool ← polls for jobs ← PostgreSQL
//	       ↓
//	Worker.Work(ctx, job) → processes the job
package platform

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/riverdriver/riverpgxv5"
	"github.com/riverqueue/river/rivermigrate"
)

// SetupRiver initializes the River job queue client and runs the required
// database migrations. Returns a started River client ready to insert and
// process jobs.
//
// The client uses the same pgxpool.Pool as the rest of the application,
// meaning jobs participate in the same connection pool and can use the
// same RLS-scoped transactions if needed.
//
// Workers must be registered via river.AddWorker before calling this function.
func SetupRiver(ctx context.Context, pool *pgxpool.Pool, workers *river.Workers, logger *slog.Logger) (*river.Client[pgx.Tx], error) {
	// Step 1: Run River's schema migrations.
	// River stores jobs in its own tables (river_job, river_leader, etc.).
	// These migrations are idempotent — safe to run on every startup.
	migrator, err := rivermigrate.New(riverpgxv5.New(pool), nil)
	if err != nil {
		return nil, fmt.Errorf("create river migrator: %w", err)
	}

	_, err = migrator.Migrate(ctx, rivermigrate.DirectionUp, nil)
	if err != nil {
		return nil, fmt.Errorf("run river migrations: %w", err)
	}
	logger.Info("river migrations applied")

	// Step 2: Create the River client.
	// Default concurrency for the job worker pool. 5 workers is a reasonable
	// default for a school catalog (low-volume, mostly notifications and reports).
	const defaultMaxWorkers = 5

	riverClient, err := river.NewClient(riverpgxv5.New(pool), &river.Config{
		// Workers is the registry of job types this client can process.
		// Each worker handles one job type (e.g., "report_pdf", "email_send").
		Workers: workers,

		// Queues defines the named queues and their concurrency limits.
		// The "default" queue handles most jobs. Specialized queues can be
		// added later for priority management (e.g., "email" with higher
		// concurrency, "pdf" with lower to avoid memory pressure).
		Queues: map[string]river.QueueConfig{
			river.QueueDefault: {MaxWorkers: defaultMaxWorkers},
		},

		// Logger bridges River's internal logging to our structured logger.
		Logger: logger,
	})
	if err != nil {
		return nil, fmt.Errorf("create river client: %w", err)
	}

	// Step 3: Start the client (begins polling for jobs).
	if err := riverClient.Start(ctx); err != nil {
		return nil, fmt.Errorf("start river client: %w", err)
	}
	logger.Info("river job queue started", "workers", defaultMaxWorkers)

	return riverClient, nil
}


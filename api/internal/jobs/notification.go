// Package jobs defines River job workers for CatalogRO's async tasks.
//
// Each job type has:
//   - An Args struct (serialized to JSON and stored in river_job)
//   - A Worker struct with a Work method (executes the job)
//
// Jobs are enqueued via river.Client.Insert() and processed by the
// River worker pool in the background.
package jobs

import (
	"context"
	"log/slog"

	"github.com/google/uuid"
	"github.com/riverqueue/river"
)

// NotificationArgs is the payload for a notification dispatch job.
// When a grade is created, absence recorded, or message sent, the handler
// enqueues this job instead of sending notifications synchronously.
type NotificationArgs struct {
	// Event is the notification event type (e.g., "grade.created").
	Event string `json:"event"`

	// RecipientIDs is the list of user UUIDs to notify.
	RecipientIDs []uuid.UUID `json:"recipient_ids"`

	// Title is the notification title (shown in push notifications).
	Title string `json:"title"`

	// Body is the notification body text.
	Body string `json:"body"`

	// Data holds extra key-value pairs for the push notification payload.
	Data map[string]string `json:"data,omitempty"`
}

// Kind returns the unique job type identifier for River.
func (NotificationArgs) Kind() string { return "notification_dispatch" }

// InsertOpts returns default insert options for notification jobs.
func (NotificationArgs) InsertOpts() river.InsertOpts {
	return river.InsertOpts{Queue: river.QueueDefault}
}

// NotificationWorker processes notification dispatch jobs.
// It looks up push subscriptions and sends notifications via Web Push / email.
type NotificationWorker struct {
	river.WorkerDefaults[NotificationArgs]
	Logger *slog.Logger
}

// Work processes a single notification dispatch job.
// Currently logs the notification — real Web Push / SMTP sending will be
// implemented when the sending infrastructure is added (#86, #88).
func (w *NotificationWorker) Work(ctx context.Context, job *river.Job[NotificationArgs]) error {
	w.Logger.Info("processing notification job",
		"event", job.Args.Event,
		"recipients", len(job.Args.RecipientIDs),
		"title", job.Args.Title,
		"job_id", job.ID,
	)

	// TODO: Look up push subscriptions for recipients and send via Web Push.
	// TODO: For email events (user.provisioned), send via SMTP.
	// These will be implemented in #86 and #88.

	return nil
}

// Package notification implements the notification dispatch system for CatalogRO.
//
// The dispatcher is the central point for sending notifications across all
// channels: Web Push (VAPID), email (Mailgun), and in-app (via messages table).
//
// Architecture:
//
//	Handler (grade created) → Dispatcher.Send(event, recipients)
//	                                  │
//	                          ┌───────┼───────┐
//	                          ▼       ▼       ▼
//	                        Push    Email   In-App
//	                       (VAPID) (Mailgun) (DB)
//
// Currently, the actual Push and Email senders are logged stubs. They will be
// replaced with real implementations when River job infrastructure is added.
// The Dispatcher interface is designed to be pluggable — swap the sender
// implementations without changing the calling code.
//
// Notification events (from SPECS.md §3.5):
//   - grade.created     → push to parent
//   - absence.created   → push to parent
//   - absence.excused   → push to teacher
//   - average.closed    → push to homeroom teacher (diriginte)
//   - message.sent      → push to recipients
//   - user.provisioned  → email with activation link
package notification

import (
	"context"
	"log/slog"

	"github.com/google/uuid"

	"github.com/vlahsh/catalogro/api/db/generated"
)

// Event represents a notification event type.
type Event string

const (
	EventGradeCreated    Event = "grade.created"
	EventAbsenceCreated  Event = "absence.created"
	EventAbsenceExcused  Event = "absence.excused"
	EventAverageClosed   Event = "average.closed"
	EventMessageSent     Event = "message.sent"
	EventUserProvisioned Event = "user.provisioned"
)

// Notification is a single notification to be dispatched.
type Notification struct {
	Event      Event              `json:"event"`
	Recipients []uuid.UUID        `json:"recipients"`
	Title      string             `json:"title"`
	Body       string             `json:"body"`
	Data       map[string]string  `json:"data,omitempty"` // extra payload for push
}

// Dispatcher sends notifications across all configured channels.
type Dispatcher struct {
	queries *generated.Queries
	logger  *slog.Logger
}

// NewDispatcher creates a new notification Dispatcher.
func NewDispatcher(queries *generated.Queries, logger *slog.Logger) *Dispatcher {
	return &Dispatcher{
		queries: queries,
		logger:  logger,
	}
}

// Send dispatches a notification to all channels for the given recipients.
//
// Currently this is a synchronous stub that logs the event. When River
// job infrastructure is added, this will enqueue an async job instead.
func (d *Dispatcher) Send(ctx context.Context, n *Notification) {
	if len(n.Recipients) == 0 {
		return
	}

	d.logger.Info("notification dispatched",
		"event", string(n.Event),
		"recipients", len(n.Recipients),
		"title", n.Title,
	)

	// TODO: When River is added, enqueue a job here instead of inline dispatch.
	// For now, attempt to look up push subscriptions and log what would be sent.
	subs, err := d.queries.ListPushSubscriptionsForUsers(ctx, n.Recipients)
	if err != nil {
		d.logger.Warn("failed to look up push subscriptions", "error", err)
		return
	}

	for i := range subs {
		logEndpoint := subs[i].Endpoint
		if len(logEndpoint) > 50 {
			logEndpoint = logEndpoint[:50] + "..."
		}
		d.logger.Debug("would send push notification",
			"user_id", subs[i].UserID,
			"endpoint", logEndpoint,
			"title", n.Title,
		)
	}

	// TODO: Email notifications via Mailgun for EventUserProvisioned.
	// TODO: Web Push via VAPID for grade/absence/message events.
}

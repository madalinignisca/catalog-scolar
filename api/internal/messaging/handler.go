// Package messaging implements HTTP handlers for the teacher-parent messaging
// and school announcement system in CatalogRO.
//
// Endpoints:
//
//	GET  /messages                    — list messages for the current user
//	GET  /messages/{messageId}        — get a single message
//	POST /messages                    — send a message to specific recipients
//	POST /messages/announcements      — send an announcement to a class's parents
//	PUT  /messages/{messageId}/read   — mark a message as read
//
// DOMAIN CONTEXT (Romanian school system):
//   - Teachers send messages to parents about student progress or behavior.
//   - School admins send announcements to all parents in a class or school-wide.
//   - Parents can reply to teacher messages.
//   - Messages are stored per-recipient with individual read tracking.
//
// Authorization:
//   - All authenticated users can read their own messages.
//   - Teachers, admins, and secretaries can send messages.
//   - Parents can send messages (replies to teachers).
//   - Only admins and teachers can send announcements.
//   - Students cannot send messages (read-only access).
package messaging

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/vlahsh/catalogro/api/db/generated"
	"github.com/vlahsh/catalogro/api/internal/auth"
	"github.com/vlahsh/catalogro/api/internal/httputil"
)

// Handler holds the dependencies for messaging HTTP handlers.
type Handler struct {
	queries *generated.Queries
	logger  *slog.Logger
}

// NewHandler creates a new messaging Handler.
func NewHandler(queries *generated.Queries, logger *slog.Logger) *Handler {
	return &Handler{queries: queries, logger: logger}
}

// ──────────────────────────────────────────────────────────────────────────────
// GET /messages — list messages for the current user
// ──────────────────────────────────────────────────────────────────────────────

// messageResponse is the JSON shape for a message in API responses.
type messageResponse struct {
	ID              uuid.UUID  `json:"id"`
	SenderID        uuid.UUID  `json:"sender_id"`
	SenderFirstName string     `json:"sender_first_name"`
	SenderLastName  string     `json:"sender_last_name"`
	Subject         *string    `json:"subject,omitempty"`
	Body            string     `json:"body"`
	IsAnnouncement  bool       `json:"is_announcement"`
	ReadAt          *time.Time `json:"read_at,omitempty"`
	CreatedAt       time.Time  `json:"created_at"`
}

// ListMessages handles GET /messages.
// Returns all messages where the current user is a recipient.
func (h *Handler) ListMessages(w http.ResponseWriter, r *http.Request) {
	userID, err := auth.GetUserID(r.Context())
	if err != nil {
		httputil.Unauthorized(w, "Authentication required")
		return
	}

	queries := auth.GetQueries(r.Context())
	if queries == nil {
		httputil.InternalError(w)
		return
	}

	messages, err := queries.ListMessagesForUser(r.Context(), userID)
	if err != nil {
		h.logger.Error("failed to list messages", "error", err, "user_id", userID)
		httputil.InternalError(w)
		return
	}

	result := make([]messageResponse, 0, len(messages))
	for i := range messages {
		msg := messageResponse{
			ID:              messages[i].ID,
			SenderID:        messages[i].SenderID,
			SenderFirstName: messages[i].SenderFirstName,
			SenderLastName:  messages[i].SenderLastName,
			Subject:         messages[i].Subject,
			Body:            messages[i].Body,
			IsAnnouncement:  messages[i].IsAnnouncement,
			CreatedAt:       messages[i].CreatedAt,
		}
		if messages[i].ReadAt.Valid {
			msg.ReadAt = &messages[i].ReadAt.Time
		}
		result = append(result, msg)
	}

	httputil.Success(w, result)
}

// ──────────────────────────────────────────────────────────────────────────────
// GET /messages/{messageId} — get a single message
// ──────────────────────────────────────────────────────────────────────────────

// GetMessage handles GET /messages/{messageId}.
// Only the sender or a recipient can view a message.
func (h *Handler) GetMessage(w http.ResponseWriter, r *http.Request) {
	userID, err := auth.GetUserID(r.Context())
	if err != nil {
		httputil.Unauthorized(w, "Authentication required")
		return
	}

	queries := auth.GetQueries(r.Context())
	if queries == nil {
		httputil.InternalError(w)
		return
	}

	messageID, err := uuid.Parse(chi.URLParam(r, "messageId"))
	if err != nil {
		httputil.BadRequest(w, "INVALID_ID", "messageId must be a valid UUID")
		return
	}

	// The query enforces that the requesting user is either the sender or a recipient.
	msg, err := queries.GetMessageByID(r.Context(), generated.GetMessageByIDParams{
		ID:          messageID,
		RecipientID: userID,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			httputil.NotFound(w, "Message not found")
			return
		}
		h.logger.Error("failed to get message", "error", err)
		httputil.InternalError(w)
		return
	}

	httputil.Success(w, map[string]any{
		"id":                msg.ID,
		"sender_id":         msg.SenderID,
		"sender_first_name": msg.SenderFirstName,
		"sender_last_name":  msg.SenderLastName,
		"subject":           msg.Subject,
		"body":              msg.Body,
		"is_announcement":   msg.IsAnnouncement,
		"created_at":        msg.CreatedAt,
	})
}

// ──────────────────────────────────────────────────────────────────────────────
// POST /messages — send a message to specific recipients
// ──────────────────────────────────────────────────────────────────────────────

// sendMessageRequest is the expected JSON body for POST /messages.
type sendMessageRequest struct {
	RecipientIDs []uuid.UUID `json:"recipient_ids"`
	Subject      *string     `json:"subject"`
	Body         string      `json:"body"`
}

// SendMessage handles POST /messages.
// Sends a direct message to one or more specific recipients.
func (h *Handler) SendMessage(w http.ResponseWriter, r *http.Request) {
	userID, err := auth.GetUserID(r.Context())
	if err != nil {
		httputil.Unauthorized(w, "Authentication required")
		return
	}

	role, err := auth.GetUserRole(r.Context())
	if err != nil {
		httputil.Unauthorized(w, "Authentication required")
		return
	}

	// Students cannot send messages.
	if role == "student" {
		httputil.Forbidden(w, "Students cannot send messages")
		return
	}

	queries := auth.GetQueries(r.Context())
	if queries == nil {
		httputil.InternalError(w)
		return
	}

	var req sendMessageRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httputil.BadRequest(w, "INVALID_JSON", "Request body must be valid JSON")
		return
	}

	if req.Body == "" {
		httputil.BadRequest(w, "MISSING_FIELD", "body is required")
		return
	}
	if len(req.RecipientIDs) == 0 {
		httputil.BadRequest(w, "MISSING_FIELD", "recipient_ids must contain at least one recipient")
		return
	}

	// Create the message.
	msg, err := queries.CreateMessage(r.Context(), generated.CreateMessageParams{
		SenderID:      userID,
		Subject:       req.Subject,
		Body:          req.Body,
		IsAnnouncement: false,
		TargetClassID: pgtype.UUID{},
	})
	if err != nil {
		h.logger.Error("failed to create message", "error", err)
		httputil.InternalError(w)
		return
	}

	// Add recipients.
	for _, recipientID := range req.RecipientIDs {
		err := queries.CreateMessageRecipient(r.Context(), generated.CreateMessageRecipientParams{
			MessageID:   msg.ID,
			RecipientID: recipientID,
		})
		if err != nil {
			h.logger.Error("failed to add message recipient",
				"message_id", msg.ID, "recipient_id", recipientID, "error", err)
		}
	}

	httputil.Created(w, map[string]any{
		"id":         msg.ID,
		"recipients": len(req.RecipientIDs),
		"created_at": msg.CreatedAt,
	})
}

// ──────────────────────────────────────────────────────────────────────────────
// POST /messages/announcements — send announcement to a class's parents
// ──────────────────────────────────────────────────────────────────────────────

// announcementRequest is the expected JSON body for POST /messages/announcements.
type announcementRequest struct {
	ClassID uuid.UUID `json:"class_id"`
	Subject *string   `json:"subject"`
	Body    string    `json:"body"`
}

// SendAnnouncement handles POST /messages/announcements.
// Sends an announcement to all parents of students in the specified class.
func (h *Handler) SendAnnouncement(w http.ResponseWriter, r *http.Request) {
	userID, err := auth.GetUserID(r.Context())
	if err != nil {
		httputil.Unauthorized(w, "Authentication required")
		return
	}

	role, err := auth.GetUserRole(r.Context())
	if err != nil {
		httputil.Unauthorized(w, "Authentication required")
		return
	}

	// Only teachers and admins can send announcements.
	if role != "admin" && role != "teacher" && role != "secretary" {
		httputil.Forbidden(w, "Only staff can send announcements")
		return
	}

	queries := auth.GetQueries(r.Context())
	if queries == nil {
		httputil.InternalError(w)
		return
	}

	var req announcementRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httputil.BadRequest(w, "INVALID_JSON", "Request body must be valid JSON")
		return
	}

	if req.Body == "" {
		httputil.BadRequest(w, "MISSING_FIELD", "body is required")
		return
	}
	if req.ClassID == uuid.Nil {
		httputil.BadRequest(w, "MISSING_FIELD", "class_id is required")
		return
	}

	// Create the announcement message.
	msg, err := queries.CreateMessage(r.Context(), generated.CreateMessageParams{
		SenderID:       userID,
		Subject:        req.Subject,
		Body:           req.Body,
		IsAnnouncement: true,
		TargetClassID:  pgtype.UUID{Bytes: req.ClassID, Valid: true},
	})
	if err != nil {
		h.logger.Error("failed to create announcement", "error", err)
		httputil.InternalError(w)
		return
	}

	// Find all parents of students in the class and add them as recipients.
	parents, err := queries.ListParentsForClass(r.Context(), req.ClassID)
	if err != nil {
		h.logger.Error("failed to list parents for class", "error", err, "class_id", req.ClassID)
		httputil.InternalError(w)
		return
	}

	recipientCount := 0
	for i := range parents {
		err := queries.CreateMessageRecipient(r.Context(), generated.CreateMessageRecipientParams{
			MessageID:   msg.ID,
			RecipientID: parents[i],
		})
		if err != nil {
			h.logger.Error("failed to add announcement recipient",
				"message_id", msg.ID, "parent_id", parents[i], "error", err)
			continue
		}
		recipientCount++
	}

	httputil.Created(w, map[string]any{
		"id":         msg.ID,
		"recipients": recipientCount,
		"class_id":   req.ClassID,
		"created_at": msg.CreatedAt,
	})
}

// ──────────────────────────────────────────────────────────────────────────────
// PUT /messages/{messageId}/read — mark message as read
// ──────────────────────────────────────────────────────────────────────────────

// MarkRead handles PUT /messages/{messageId}/read.
// Marks a message as read by the current user. Idempotent — calling
// it again on an already-read message has no effect.
func (h *Handler) MarkRead(w http.ResponseWriter, r *http.Request) {
	userID, err := auth.GetUserID(r.Context())
	if err != nil {
		httputil.Unauthorized(w, "Authentication required")
		return
	}

	queries := auth.GetQueries(r.Context())
	if queries == nil {
		httputil.InternalError(w)
		return
	}

	messageID, err := uuid.Parse(chi.URLParam(r, "messageId"))
	if err != nil {
		httputil.BadRequest(w, "INVALID_ID", "messageId must be a valid UUID")
		return
	}

	err = queries.MarkMessageRead(r.Context(), generated.MarkMessageReadParams{
		MessageID:   messageID,
		RecipientID: userID,
	})
	if err != nil {
		h.logger.Error("failed to mark message read", "error", err, "message_id", messageID)
		httputil.InternalError(w)
		return
	}

	httputil.Success(w, map[string]any{
		"read": true,
	})
}

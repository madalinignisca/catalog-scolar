// This file implements HTTP handlers for push subscription management.
//
// Endpoints:
//
//	POST   /notifications/push/subscribe   — register a Web Push subscription
//	DELETE /notifications/push/unsubscribe — remove a push subscription
//	GET    /notifications/push/status      — check if user has active subscriptions
//
// These endpoints manage the client-side Web Push API subscriptions. The browser
// generates a subscription object (endpoint URL + keys) which is sent here for
// server-side storage. When the server needs to send a push notification, it
// reads these subscriptions and sends via the Web Push protocol (VAPID).
//
// Authorization: all authenticated users can manage their own subscriptions.
package notification

import (
	"encoding/json"
	"log/slog"
	"net/http"

	"github.com/vlahsh/catalogro/api/db/generated"
	"github.com/vlahsh/catalogro/api/internal/auth"
	"github.com/vlahsh/catalogro/api/internal/httputil"
)

// Handler holds the dependencies for notification HTTP handlers.
type Handler struct {
	queries *generated.Queries
	logger  *slog.Logger
}

// NewHandler creates a new notification Handler.
func NewHandler(queries *generated.Queries, logger *slog.Logger) *Handler {
	return &Handler{queries: queries, logger: logger}
}

// ──────────────────────────────────────────────────────────────────────────────
// POST /notifications/push/subscribe
// ──────────────────────────────────────────────────────────────────────────────

// subscribeRequest is the expected JSON body for push subscription registration.
// These fields come from the browser's PushSubscription object:
//
//	const sub = await registration.pushManager.subscribe({ ... });
//	// sub.endpoint, sub.getKey('p256dh'), sub.getKey('auth')
type subscribeRequest struct {
	Endpoint  string `json:"endpoint"`
	P256dhKey string `json:"p256dh_key"`
	AuthKey   string `json:"auth_key"`
}

// Subscribe handles POST /notifications/push/subscribe.
// Registers (or updates) a Web Push subscription for the current user.
func (h *Handler) Subscribe(w http.ResponseWriter, r *http.Request) {
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

	// Limit request body to 4 KB to prevent memory exhaustion from oversized payloads.
	r.Body = http.MaxBytesReader(w, r.Body, 4096)
	var req subscribeRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httputil.BadRequest(w, "INVALID_JSON", "Request body must be valid JSON")
		return
	}

	if req.Endpoint == "" {
		httputil.BadRequest(w, "MISSING_FIELD", "endpoint is required")
		return
	}
	if req.P256dhKey == "" || req.AuthKey == "" {
		httputil.BadRequest(w, "MISSING_FIELD", "p256dh_key and auth_key are required")
		return
	}

	// Get User-Agent for tracking which device this subscription belongs to.
	userAgent := r.Header.Get("User-Agent")
	var uaPtr *string
	if userAgent != "" {
		uaPtr = &userAgent
	}

	sub, err := queries.CreatePushSubscription(r.Context(), generated.CreatePushSubscriptionParams{
		UserID:    userID,
		Endpoint:  req.Endpoint,
		P256dhKey: req.P256dhKey,
		AuthKey:   req.AuthKey,
		UserAgent: uaPtr,
	})
	if err != nil {
		h.logger.Error("failed to create push subscription", "error", err, "user_id", userID)
		httputil.InternalError(w)
		return
	}

	httputil.Created(w, map[string]any{
		"id":       sub.ID,
		"endpoint": sub.Endpoint,
	})
}

// ──────────────────────────────────────────────────────────────────────────────
// DELETE /notifications/push/unsubscribe
// ──────────────────────────────────────────────────────────────────────────────

// unsubscribeRequest is the expected JSON body for removing a subscription.
type unsubscribeRequest struct {
	Endpoint string `json:"endpoint"`
}

// Unsubscribe handles DELETE /notifications/push/unsubscribe.
// Removes a push subscription by endpoint URL.
func (h *Handler) Unsubscribe(w http.ResponseWriter, r *http.Request) {
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

	// Limit request body to 1 KB — unsubscribe only needs the endpoint URL.
	r.Body = http.MaxBytesReader(w, r.Body, 1024)
	var req unsubscribeRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httputil.BadRequest(w, "INVALID_JSON", "Request body must be valid JSON")
		return
	}

	if req.Endpoint == "" {
		httputil.BadRequest(w, "MISSING_FIELD", "endpoint is required")
		return
	}

	err = queries.DeletePushSubscription(r.Context(), generated.DeletePushSubscriptionParams{
		UserID:   userID,
		Endpoint: req.Endpoint,
	})
	if err != nil {
		h.logger.Error("failed to delete push subscription", "error", err, "user_id", userID)
		httputil.InternalError(w)
		return
	}

	httputil.Success(w, map[string]any{"unsubscribed": true})
}

// ──────────────────────────────────────────────────────────────────────────────
// GET /notifications/push/status
// ──────────────────────────────────────────────────────────────────────────────

// PushStatus handles GET /notifications/push/status.
// Returns how many active push subscriptions the user has.
func (h *Handler) PushStatus(w http.ResponseWriter, r *http.Request) {
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

	subs, err := queries.ListPushSubscriptionsForUser(r.Context(), userID)
	if err != nil {
		h.logger.Error("failed to list push subscriptions", "error", err, "user_id", userID)
		httputil.InternalError(w)
		return
	}

	httputil.Success(w, map[string]any{
		"subscriptions": len(subs),
		"push_enabled":  len(subs) > 0,
	})
}

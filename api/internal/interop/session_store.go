// This file implements a Redis-backed import session store, replacing the
// in-memory map that was used previously. Redis allows the async
// BulkImportWorker to update session status because both the HTTP handler
// and the worker share the same Redis instance.
//
// Sessions expire after 1 hour (TTL) to prevent stale data accumulation.
package interop

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"

	"github.com/vlahsh/catalogro/api/internal/interop/siiir"
)

// sessionTTL is how long import sessions are kept in Redis before expiring.
const sessionTTL = 1 * time.Hour

// redisKeyPrefix is the namespace for import session keys in Redis.
const redisKeyPrefix = "import_session:"

// SessionStore manages import sessions in Redis.
type SessionStore struct {
	client *redis.Client
}

// NewSessionStore creates a new Redis-backed session store.
func NewSessionStore(client *redis.Client) *SessionStore {
	return &SessionStore{client: client}
}

// ImportSession holds the state of an in-progress SIIIR import.
// Stored as JSON in Redis.
type ImportSession struct {
	ID        uuid.UUID          `json:"id"`
	Status    string             `json:"status"` // "preview", "confirmed", "processing", "completed", "failed"
	CreatedAt time.Time          `json:"created_at"`
	Users     []siiir.MappedUser `json:"users"`
	Errors    []string           `json:"errors,omitempty"`
	Imported  int                `json:"imported"`
	Skipped   int                `json:"skipped"`
}

// Save stores an import session in Redis with TTL.
func (s *SessionStore) Save(ctx context.Context, session *ImportSession) error {
	data, err := json.Marshal(session)
	if err != nil {
		return fmt.Errorf("marshal session: %w", err)
	}
	key := redisKeyPrefix + session.ID.String()
	return s.client.Set(ctx, key, data, sessionTTL).Err()
}

// Get retrieves an import session from Redis by ID.
// Returns nil if the session doesn't exist or has expired.
func (s *SessionStore) Get(ctx context.Context, id uuid.UUID) (*ImportSession, error) {
	key := redisKeyPrefix + id.String()
	data, err := s.client.Get(ctx, key).Bytes()
	if err != nil {
		if err == redis.Nil {
			return nil, nil // not found / expired
		}
		return nil, fmt.Errorf("get session: %w", err)
	}

	var session ImportSession
	if err := json.Unmarshal(data, &session); err != nil {
		return nil, fmt.Errorf("unmarshal session: %w", err)
	}
	return &session, nil
}

// UpdateStatus atomically updates just the status of a session.
// Used by the BulkImportWorker to mark a session as completed.
func (s *SessionStore) UpdateStatus(ctx context.Context, id uuid.UUID, status string, imported, skipped int, errors []string) error {
	session, err := s.Get(ctx, id)
	if err != nil {
		return err
	}
	if session == nil {
		return fmt.Errorf("session %s not found", id)
	}

	session.Status = status
	session.Imported = imported
	session.Skipped = skipped
	if len(errors) > 0 {
		session.Errors = append(session.Errors, errors...)
	}

	return s.Save(ctx, session)
}

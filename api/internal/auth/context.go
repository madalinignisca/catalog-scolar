// Package auth provides authentication context helpers for the CatalogRO API.
//
// This file defines the context keys and getter functions that handlers use
// to retrieve the authenticated user's identity. The JWTAuth middleware stores
// the full Claims struct in context, and these functions extract specific fields.
//
// There are two styles of getters:
//
//  1. GetUserID(ctx) (uuid.UUID, error) — returns a parsed UUID + error.
//     Used by existing handlers (catalog, school) that expect this signature.
//
//  2. GetClaims(ctx) *Claims — returns the full claims struct (nil if missing).
//     Used by middleware and new handlers that need all claims at once.
package auth

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	"github.com/vlahsh/catalogro/api/db/generated"
)

// contextKey is a private type used for context keys in this package.
// Using a dedicated type (instead of a bare string) prevents collisions with
// context keys defined in other packages. Two different packages can both use
// the string "user_id" as a key, but they will never collide because Go
// compares the key type as well as the key value.
type contextKey string

const (
	// claimsKey is the context key where the full JWT Claims struct is stored
	// after successful authentication by the JWTAuth middleware.
	claimsKey contextKey = "auth_claims"

	// queriesKey is the context key where the transaction-scoped sqlc Queries
	// object is stored by the TenantContext middleware. This Queries instance
	// is bound to a PostgreSQL transaction that has the RLS tenant set.
	queriesKey contextKey = "auth_queries"
)

// =============================================================================
// Claims-based context helpers (used by middleware and new handlers)
// =============================================================================

// GetClaims extracts the JWT Claims from the request context.
//
// Returns nil if JWTAuth middleware hasn't run (or if the token was invalid).
// Handlers should always check for nil, although in practice all protected
// routes should have JWTAuth in their middleware chain.
func GetClaims(ctx context.Context) *Claims {
	claims, ok := ctx.Value(claimsKey).(*Claims)
	if !ok {
		return nil
	}
	return claims
}

// GetQueries extracts the transaction-scoped sqlc Queries object from the request
// context. This Queries instance is bound to a transaction that has the RLS tenant
// set, so all queries through it are automatically filtered by school_id.
//
// Returns nil if TenantContext middleware hasn't run.
//
// Usage in a handler:
//
//	q := auth.GetQueries(r.Context())
//	users, err := q.ListUsersBySchool(r.Context())
func GetQueries(ctx context.Context) *generated.Queries {
	q, ok := ctx.Value(queriesKey).(*generated.Queries)
	if !ok {
		return nil
	}
	return q
}

// =============================================================================
// Legacy-compatible context helpers (used by existing catalog/school handlers)
// =============================================================================
// These functions return (value, error) which is the signature used by the
// existing handlers built before this middleware was implemented. They read
// from the same Claims struct stored by JWTAuth middleware.

// GetUserID extracts the authenticated user's UUID from the request context.
//
// Returns an error if the value is missing (meaning the auth middleware did not
// run or did not set the value). Handlers should treat this as a 401 Unauthorized.
func GetUserID(ctx context.Context) (uuid.UUID, error) {
	claims := GetClaims(ctx)
	if claims == nil {
		return uuid.Nil, fmt.Errorf("auth: user_id not found in context (is auth middleware active?)")
	}
	parsed, err := uuid.Parse(claims.UserID)
	if err != nil {
		return uuid.Nil, fmt.Errorf("auth: invalid user_id in claims: %w", err)
	}
	return parsed, nil
}

// GetSchoolID extracts the school (tenant) UUID from the request context.
//
// Returns an error if the value is missing. Handlers should treat this as a
// 401 Unauthorized, because every authenticated request must have a tenant.
func GetSchoolID(ctx context.Context) (uuid.UUID, error) {
	claims := GetClaims(ctx)
	if claims == nil {
		return uuid.Nil, fmt.Errorf("auth: school_id not found in context (is tenant middleware active?)")
	}
	parsed, err := uuid.Parse(claims.SchoolID)
	if err != nil {
		return uuid.Nil, fmt.Errorf("auth: invalid school_id in claims: %w", err)
	}
	return parsed, nil
}

// GetUserRole extracts the user's role string from the request context.
//
// Returns an error if the value is missing. The role is one of:
// "admin", "secretary", "teacher", "parent", "student".
func GetUserRole(ctx context.Context) (string, error) {
	claims := GetClaims(ctx)
	if claims == nil {
		return "", fmt.Errorf("auth: user_role not found in context (is auth middleware active?)")
	}
	return claims.Role, nil
}

// =============================================================================
// String-returning convenience helpers (for use in new code)
// =============================================================================

// UserID is a convenience function that extracts just the user's UUID string
// from the request context. Returns an empty string if no claims are present.
// Prefer GetUserID if you need a uuid.UUID and error handling.
func UserID(ctx context.Context) string {
	claims := GetClaims(ctx)
	if claims == nil {
		return ""
	}
	return claims.UserID
}

// SchoolID is a convenience function that extracts just the school's UUID string
// from the request context. Returns an empty string if no claims are present.
// Prefer GetSchoolID if you need a uuid.UUID and error handling.
func SchoolID(ctx context.Context) string {
	claims := GetClaims(ctx)
	if claims == nil {
		return ""
	}
	return claims.SchoolID
}

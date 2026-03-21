package auth

// This file contains the HTTP middleware that protects API routes.
//
// Middleware in Go (and chi specifically) are functions that wrap an http.Handler,
// adding behavior before and/or after the wrapped handler runs. They form a chain:
//
//   Request -> CORS -> JWTAuth -> TenantContext -> RequireRole -> Your Handler -> Response
//
// Each middleware in this file serves a specific purpose:
//
//   JWTAuth:        Extracts and validates the JWT from the Authorization header.
//                   If the token is invalid or missing, the request is rejected with 401.
//                   On success, the parsed Claims are stored in the request context.
//
//   TenantContext:  Reads the school_id from the JWT claims (set by JWTAuth) and
//                   configures PostgreSQL Row-Level Security (RLS) for this request.
//                   It starts a transaction, sets "app.current_school_id", and ensures
//                   all downstream queries only see data for this school.
//
//   RequireRole:    Checks that the authenticated user's role is in the allowed list.
//                   For example, RequireRole("admin", "secretary") on a user-provisioning
//                   endpoint ensures that teachers and parents cannot create accounts.
//
// Context keys and getter functions are defined in context.go (same package).

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/vlahsh/catalogro/api/db/generated"
)

// =============================================================================
// JWTAuth middleware — validates the Bearer token on every protected request
// =============================================================================

// JWTAuth returns a chi middleware that validates JWT access tokens.
//
// How it works:
//  1. Extracts the token from the "Authorization: Bearer <token>" header
//  2. Validates the token's signature, expiry, and structure using ValidateToken
//  3. Stores the parsed Claims in the request context for downstream handlers
//  4. If anything fails, returns a 401 Unauthorized JSON error
//
// Usage in router setup:
//
//	r.Group(func(r chi.Router) {
//	    r.Use(auth.JWTAuth([]byte(cfg.JWTSecret)))
//	    r.Get("/users/me", profileHandler)
//	})
func JWTAuth(secret []byte) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Step 1: Get the Authorization header.
			// Expected format: "Bearer eyJhbGciOiJIUzI1NiIs..."
			authHeader := r.Header.Get("Authorization")
			if authHeader == "" {
				writeError(w, http.StatusUnauthorized, "UNAUTHORIZED", "Missing Authorization header")
				return
			}

			// Step 2: Split "Bearer" from the actual token.
			// We expect exactly two parts: ["Bearer", "<token>"].
			parts := strings.SplitN(authHeader, " ", 2)
			if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") {
				writeError(w, http.StatusUnauthorized, "UNAUTHORIZED", "Authorization header must be: Bearer <token>")
				return
			}

			tokenString := parts[1]

			// Step 3: Validate the token — check signature, expiry, and claims structure.
			claims, err := ValidateToken(tokenString, secret)
			if err != nil {
				// Log the actual error for debugging, but don't expose details to the client
				// (that would help attackers understand what's wrong with their forged token).
				slog.Debug("jwt validation failed", "error", err)
				writeError(w, http.StatusUnauthorized, "UNAUTHORIZED", "Invalid or expired token")
				return
			}

			// Step 4: Store claims in context so handlers can access user info.
			// This is how GetClaims(ctx), GetUserID(ctx), and GetSchoolID(ctx) work.
			// The claimsKey constant is defined in context.go (same package).
			ctx := context.WithValue(r.Context(), claimsKey, claims)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// =============================================================================
// TenantContext middleware — sets PostgreSQL RLS context per request
// =============================================================================

// TenantContext returns a chi middleware that activates Row-Level Security (RLS)
// for the current request by setting the PostgreSQL session variable
// "app.current_school_id" inside a transaction.
//
// Why a transaction?
// PostgreSQL's set_config with is_local=true (which is what SET LOCAL does) only
// works within a transaction. The setting is automatically cleared when the
// transaction ends, so there's no risk of one request's tenant leaking to another.
//
// How it works:
//  1. Reads the school_id from the JWT claims (set by JWTAuth middleware — must run first)
//  2. Begins a database transaction
//  3. Calls SET LOCAL to configure the tenant for RLS policies
//  4. Creates a Queries object bound to this transaction and stores it in context
//  5. Runs the downstream handler (which uses GetQueries(ctx) for all DB access)
//  6. Commits the transaction on success, rolls back on panic
//
// Usage in router setup:
//
//	r.Group(func(r chi.Router) {
//	    r.Use(auth.JWTAuth([]byte(cfg.JWTSecret)))
//	    r.Use(auth.TenantContext(db.Pool))
//	    r.Get("/classes", classListHandler)
//	})
func TenantContext(pool *pgxpool.Pool) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Step 1: Get the claims from context. JWTAuth must have run before this.
			claims := GetClaims(r.Context())
			if claims == nil {
				// This should never happen if middleware is wired correctly.
				// If it does, it means TenantContext was used without JWTAuth.
				writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "Authentication context missing")
				return
			}

			// Step 2: Start a database transaction.
			// All queries in this request will run inside this transaction, ensuring
			// the RLS setting is active for every query.
			tx, err := pool.Begin(r.Context())
			if err != nil {
				slog.Error("failed to begin transaction for tenant context", "error", err)
				writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "Database error")
				return
			}

			// Step 3: Ensure we clean up the transaction no matter what.
			// If the handler panics, we rollback. If everything succeeds, we commit below.
			defer func() {
				if p := recover(); p != nil {
					// Something panicked — rollback and re-panic so the Recoverer
					// middleware upstream can handle it properly.
					_ = tx.Rollback(r.Context())
					panic(p)
				}
			}()

			// Step 4: Set the tenant (school_id) for RLS.
			// This calls: SELECT set_config('app.current_school_id', '<uuid>', true)
			// The "true" means "local to this transaction" — it auto-clears on COMMIT/ROLLBACK.
			_, err = tx.Exec(r.Context(), "SELECT set_config('app.current_school_id', $1, true)", claims.SchoolID)
			if err != nil {
				_ = tx.Rollback(r.Context())
				slog.Error("failed to set tenant context", "school_id", claims.SchoolID, "error", err)
				writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "Failed to set tenant context")
				return
			}

			// Step 5: Create a Queries object bound to this transaction.
			// The generated sqlc Queries.WithTx(tx) method returns a new Queries that
			// executes all SQL through the transaction, so RLS is always active.
			queries := generated.New(pool).WithTx(tx)

			// Step 6: Store the tx-scoped Queries in context for handlers to use.
			// The queriesKey constant is defined in context.go (same package).
			ctx := context.WithValue(r.Context(), queriesKey, queries)

			// Step 7: Run the actual handler.
			next.ServeHTTP(w, r.WithContext(ctx))

			// Step 8: Commit the transaction if the handler completed normally.
			// If the handler returned an error response (4xx/5xx), we still commit
			// because the handler may not have written anything to the DB.
			// If we always rolled back on errors, read-only endpoints would needlessly
			// create and discard transactions.
			if err := tx.Commit(r.Context()); err != nil {
				slog.Error("failed to commit tenant transaction", "error", err)
				// At this point the response is already written, so we can only log the error.
			}
		})
	}
}

// =============================================================================
// RequireRole middleware — enforces role-based access control (RBAC)
// =============================================================================

// RequireRole returns a chi middleware that restricts access to users with
// one of the specified roles.
//
// Example: only admins and secretaries can provision new user accounts:
//
//	r.With(auth.RequireRole("admin", "secretary")).Post("/users", createUserHandler)
//
// If the user's role (from JWT claims) is not in the allowed list, the request
// is rejected with 403 Forbidden.
//
// IMPORTANT: JWTAuth middleware must run before RequireRole, because RequireRole
// reads the role from the JWT claims stored in context by JWTAuth.
func RequireRole(roles ...string) func(http.Handler) http.Handler {
	// Pre-build a set (map) of allowed roles for O(1) lookup.
	// This avoids looping through the slice on every request.
	allowed := make(map[string]bool, len(roles))
	for _, role := range roles {
		allowed[role] = true
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			claims := GetClaims(r.Context())
			if claims == nil {
				// Should not happen if middleware chain is correct.
				writeError(w, http.StatusUnauthorized, "UNAUTHORIZED", "Authentication required")
				return
			}

			// Check if the user's role is in the allowed set.
			if !allowed[claims.Role] {
				slog.Warn("access denied: insufficient role",
					"user_id", claims.UserID,
					"role", claims.Role,
					"required_roles", roles,
				)
				writeError(w, http.StatusForbidden, "FORBIDDEN",
					fmt.Sprintf("This endpoint requires one of these roles: %s", strings.Join(roles, ", ")))
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

// =============================================================================
// JSON error response helper
// =============================================================================

// writeError sends a JSON error response matching the CatalogRO API error format:
//
//	{ "error": { "code": "UNAUTHORIZED", "message": "Invalid or expired token" } }
//
// This helper is used by middleware to return consistent error responses.
// The status parameter is the HTTP status code (401, 403, 500, etc.).
func writeError(w http.ResponseWriter, status int, code, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)

	// Build the error response using the standard CatalogRO format.
	resp := map[string]interface{}{
		"error": map[string]string{
			"code":    code,
			"message": message,
		},
	}

	// Encode and write. If encoding fails (extremely unlikely), we've already
	// written the status code, so there's nothing more we can do.
	_ = json.NewEncoder(w).Encode(resp)
}

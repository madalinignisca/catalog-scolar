package auth

// This file contains the HTTP handlers for authentication endpoints:
//
//   POST /auth/login    — email + password login (returns tokens or mfa_required)
//   POST /auth/refresh  — exchange a refresh token for a new access + refresh token pair
//   POST /auth/logout   — invalidate the current session (client-side token discard)
//   GET  /users/me      — return the authenticated user's profile
//
// All handlers follow the CatalogRO API response format:
//   Success: { "data": { ... } }
//   Error:   { "error": { "code": "...", "message": "..." } }

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"golang.org/x/crypto/bcrypt"

	"github.com/vlahsh/catalogro/api/db/generated"
)

// =============================================================================
// Request / Response types — define the JSON shape of each endpoint
// =============================================================================

// loginRequest is the expected JSON body for POST /auth/login.
// Both fields are required — the handler validates this.
type loginRequest struct {
	// Email is the user's email address (e.g., "profesor.ion@scoala.ro").
	// In CatalogRO, email is the primary login identifier because accounts
	// are provisioned by the school secretary with a known email.
	Email string `json:"email"`

	// Password is the user's password, set during account activation.
	// It is compared against the bcrypt hash stored in users.password_hash.
	Password string `json:"password"`
}

// tokenResponse is the JSON structure returned when authentication succeeds.
// It contains the JWT pair that the client stores and uses for API access.
type tokenResponse struct {
	// AccessToken is a short-lived JWT (15 min) sent in the Authorization header.
	AccessToken string `json:"access_token"`

	// RefreshToken is a longer-lived JWT (7 days) used to obtain new access tokens
	// without re-entering credentials.
	RefreshToken string `json:"refresh_token"`

	// TokenType is always "Bearer" — tells the client how to send the access token.
	TokenType string `json:"token_type"`

	// ExpiresIn is the access token lifetime in seconds (900 = 15 minutes).
	// The client can use this to schedule a refresh before the token expires.
	ExpiresIn int `json:"expires_in"`
}

// mfaRequiredResponse is returned when the user has TOTP 2FA enabled.
// Instead of getting tokens immediately, they get an mfa_token that must be
// exchanged (with a valid TOTP code) at POST /auth/2fa/login.
type mfaRequiredResponse struct {
	// MFARequired is always true — signals to the client that 2FA is needed.
	MFARequired bool `json:"mfa_required"`

	// MFAToken is a short-lived JWT (5 min) that proves the user passed step 1
	// (email + password). The client sends this + the TOTP code to complete login.
	MFAToken string `json:"mfa_token"`
}

// refreshRequest is the expected JSON body for POST /auth/refresh.
type refreshRequest struct {
	// RefreshToken is the JWT refresh token received from a previous login or refresh.
	RefreshToken string `json:"refresh_token"`
}

// profileResponse is the JSON structure returned by GET /users/me.
// It contains the user's basic profile information — never sensitive fields
// like password_hash or totp_secret.
type profileResponse struct {
	ID        string `json:"id"`
	SchoolID  string `json:"school_id"`
	Role      string `json:"role"`
	Email     string `json:"email"`
	Phone     string `json:"phone,omitempty"`
	FirstName string `json:"first_name"`
	LastName  string `json:"last_name"`
}

// =============================================================================
// HandleLogin — POST /auth/login
// =============================================================================

// HandleLogin returns an http.HandlerFunc that authenticates a user with
// email and password.
//
// The login flow:
//  1. Parse the JSON request body (email + password)
//  2. Look up the user by email using GetUserByEmailForLogin (no RLS — pre-auth)
//  3. Verify the password against the bcrypt hash
//  4. Check that the account is activated (activated_at IS NOT NULL)
//  5. If TOTP is enabled: return mfa_required + mfa_token
//  6. If TOTP is not enabled: return access_token + refresh_token
//  7. Update the user's last_login_at timestamp
//
// Parameters:
//   - db: sqlc Queries instance (using the connection pool, not a transaction,
//     because login happens BEFORE we know the school_id for RLS)
//   - jwtSecret: the HMAC key for signing JWT tokens
func HandleLogin(db *generated.Queries, jwtSecret []byte) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// Step 1: Parse the request body.
		var req loginRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "BAD_REQUEST", "Invalid JSON body")
			return
		}

		// Validate required fields.
		if req.Email == "" || req.Password == "" {
			writeError(w, http.StatusBadRequest, "BAD_REQUEST", "Email and password are required")
			return
		}

		// Step 2: Look up the user by email.
		// We use GetUserByEmailForLogin which does NOT use RLS (no school_id filter),
		// because at login time we don't know which school the user belongs to yet.
		// The query also filters by is_active=true, so disabled accounts can't log in.
		user, err := db.GetUserByEmailForLogin(r.Context(), &req.Email)
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				// User not found — but we don't reveal whether the email exists
				// (prevents email enumeration attacks).
				writeError(w, http.StatusUnauthorized, "UNAUTHORIZED", "Invalid email or password")
				return
			}
			// Unexpected database error.
			slog.Error("login: failed to query user by email", "error", err)
			writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "An unexpected error occurred")
			return
		}

		// Step 3: Check that the account has been activated.
		// Users receive an activation link (email/SMS) after being provisioned.
		// Until they activate (set password + optional 2FA), they cannot log in.
		// ActivatedAt is a pgtype.Timestamptz — Valid=true means it's NOT NULL.
		if !user.ActivatedAt.Valid {
			writeError(w, http.StatusUnauthorized, "UNAUTHORIZED", "Account not activated. Please check your email for the activation link.")
			return
		}

		// Step 4: Verify the password.
		// user.PasswordHash is *string (nullable). If somehow nil, password can't match.
		if user.PasswordHash == nil {
			writeError(w, http.StatusUnauthorized, "UNAUTHORIZED", "Invalid email or password")
			return
		}

		// Compare the provided password with the stored bcrypt hash.
		// bcrypt.CompareHashAndPassword handles the salt extraction and comparison.
		if err := bcrypt.CompareHashAndPassword([]byte(*user.PasswordHash), []byte(req.Password)); err != nil {
			writeError(w, http.StatusUnauthorized, "UNAUTHORIZED", "Invalid email or password")
			return
		}

		// Step 5: Check if 2FA (TOTP) is enabled for this user.
		// Roles like teacher, admin, and secretary must have TOTP enabled per policy.
		// If enabled, we don't issue tokens yet — the user must provide a TOTP code.
		if user.TotpEnabled {
			// Generate a short-lived MFA token that proves password was correct.
			mfaToken, err := GenerateMFAToken(user.ID.String(), jwtSecret)
			if err != nil {
				slog.Error("login: failed to generate MFA token", "error", err)
				writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "Failed to generate MFA token")
				return
			}

			writeJSON(w, http.StatusOK, map[string]interface{}{
				"data": mfaRequiredResponse{
					MFARequired: true,
					MFAToken:    mfaToken,
				},
			})
			return
		}

		// Step 6: No 2FA — generate the full token pair.
		accessToken, err := GenerateAccessToken(
			user.ID.String(),
			user.SchoolID.String(),
			string(user.Role),
			jwtSecret,
		)
		if err != nil {
			slog.Error("login: failed to generate access token", "error", err)
			writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "Failed to generate token")
			return
		}

		refreshToken, err := GenerateRefreshToken(user.ID.String(), jwtSecret)
		if err != nil {
			slog.Error("login: failed to generate refresh token", "error", err)
			writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "Failed to generate token")
			return
		}

		// Step 7: Update the user's last login timestamp.
		// This is a fire-and-forget operation — we don't fail the login if it errors.
		if err := db.UpdateLastLogin(r.Context(), user.ID); err != nil {
			slog.Warn("login: failed to update last_login_at", "user_id", user.ID, "error", err)
		}

		// Return the token pair.
		writeJSON(w, http.StatusOK, map[string]interface{}{
			"data": tokenResponse{
				AccessToken:  accessToken,
				RefreshToken: refreshToken,
				TokenType:    "Bearer",
				ExpiresIn:    int(AccessTokenDuration.Seconds()),
			},
		})
	}
}

// =============================================================================
// HandleRefresh — POST /auth/refresh
// =============================================================================

// HandleRefresh returns an http.HandlerFunc that exchanges a valid refresh token
// for a new access token + refresh token pair (token rotation).
//
// Token rotation is a security best practice: every time the client uses a refresh
// token, it gets a new one. This limits the window of opportunity if a refresh token
// is stolen — the legitimate client will get a new token and the old one becomes
// invalid (since the next refresh attempt with the old token will fail).
//
// NOTE: In this initial implementation, we validate the refresh token's signature
// and expiry but don't track revocation in Redis yet. A production-hardened version
// would store refresh token hashes in Redis and revoke old ones on rotation.
//
// Parameters:
//   - db: sqlc Queries for looking up the user (to get current school_id and role)
//   - jwtSecret: the HMAC key for validating and signing JWT tokens
func HandleRefresh(db *generated.Queries, jwtSecret []byte) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// Step 1: Parse the request body.
		var req refreshRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "BAD_REQUEST", "Invalid JSON body")
			return
		}

		if req.RefreshToken == "" {
			writeError(w, http.StatusBadRequest, "BAD_REQUEST", "Refresh token is required")
			return
		}

		// Step 2: Validate the refresh token — check signature and expiry.
		userID, err := ValidateRefreshToken(req.RefreshToken, jwtSecret)
		if err != nil {
			slog.Debug("refresh: invalid refresh token", "error", err)
			writeError(w, http.StatusUnauthorized, "UNAUTHORIZED", "Invalid or expired refresh token")
			return
		}

		// Step 3: Look up the user to get their current school_id and role.
		// This ensures that if a user's role was changed (e.g., promoted to admin),
		// the new access token reflects the updated role.
		parsedID, err := uuid.Parse(userID)
		if err != nil {
			writeError(w, http.StatusUnauthorized, "UNAUTHORIZED", "Invalid user ID in token")
			return
		}

		user, err := db.GetUserByID(r.Context(), parsedID)
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				writeError(w, http.StatusUnauthorized, "UNAUTHORIZED", "User not found")
				return
			}
			slog.Error("refresh: failed to query user", "user_id", userID, "error", err)
			writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "An unexpected error occurred")
			return
		}

		// Step 4: Check that the account is still active and activated.
		if !user.IsActive {
			writeError(w, http.StatusUnauthorized, "UNAUTHORIZED", "Account is deactivated")
			return
		}

		// Step 5: Generate new token pair (rotation).
		accessToken, err := GenerateAccessToken(
			user.ID.String(),
			user.SchoolID.String(),
			string(user.Role),
			jwtSecret,
		)
		if err != nil {
			slog.Error("refresh: failed to generate access token", "error", err)
			writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "Failed to generate token")
			return
		}

		refreshToken, err := GenerateRefreshToken(user.ID.String(), jwtSecret)
		if err != nil {
			slog.Error("refresh: failed to generate refresh token", "error", err)
			writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "Failed to generate token")
			return
		}

		writeJSON(w, http.StatusOK, map[string]interface{}{
			"data": tokenResponse{
				AccessToken:  accessToken,
				RefreshToken: refreshToken,
				TokenType:    "Bearer",
				ExpiresIn:    int(AccessTokenDuration.Seconds()),
			},
		})
	}
}

// =============================================================================
// HandleLogout — POST /auth/logout
// =============================================================================

// HandleLogout returns an http.HandlerFunc that logs the user out.
//
// In a JWT-based system, "logging out" on the server side means invalidating the
// refresh token so it can't be used to obtain new access tokens. The access token
// itself remains valid until it expires (15 min), which is why we keep it short-lived.
//
// Current implementation: returns success and relies on the client discarding tokens.
// A production-hardened version would:
//   - Accept the refresh token in the request body
//   - Add it to a Redis blacklist (or delete from refresh_tokens table)
//   - Optionally add the access token's JTI to a short-lived Redis blacklist
func HandleLogout() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// For now, we return success. The client is responsible for deleting
		// its stored tokens (localStorage, cookies, etc.).
		//
		// TODO: Accept refresh_token in body and revoke it in Redis/DB.
		writeJSON(w, http.StatusOK, map[string]interface{}{
			"data": map[string]string{
				"message": "Logged out successfully",
			},
		})
	}
}

// =============================================================================
// HandleGetProfile — GET /users/me
// =============================================================================

// HandleGetProfile returns an http.HandlerFunc that returns the authenticated
// user's profile information.
//
// This endpoint requires authentication (JWTAuth middleware must be in the chain).
// It reads the user_id from the JWT claims and fetches the user from the database.
//
// The response intentionally omits sensitive fields:
//   - password_hash (never expose password hashes)
//   - totp_secret (never expose TOTP keys)
//   - activation_token (no longer needed after activation)
//
// Parameters:
//   - db: sqlc Queries for looking up the user
func HandleGetProfile(db *generated.Queries) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// Step 1: Get the user ID from the JWT claims in context.
		claims := GetClaims(r.Context())
		if claims == nil {
			writeError(w, http.StatusUnauthorized, "UNAUTHORIZED", "Authentication required")
			return
		}

		// Step 2: Parse the user ID string into a UUID.
		userID, err := uuid.Parse(claims.UserID)
		if err != nil {
			writeError(w, http.StatusBadRequest, "BAD_REQUEST", "Invalid user ID in token")
			return
		}

		// Step 3: Fetch the user from the database.
		// We use GetUserByID which does NOT filter by school_id (RLS handles that
		// in the tenant middleware, but /users/me might also be called with a
		// non-RLS-scoped Queries for simplicity — the user can only see their own data
		// because we look up by their own ID from the JWT).
		user, err := db.GetUserByID(r.Context(), userID)
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				writeError(w, http.StatusNotFound, "NOT_FOUND", "User not found")
				return
			}
			slog.Error("profile: failed to query user", "user_id", userID, "error", err)
			writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "An unexpected error occurred")
			return
		}

		// Step 4: Build the safe response (no sensitive fields).
		profile := profileResponse{
			ID:        user.ID.String(),
			SchoolID:  user.SchoolID.String(),
			Role:      string(user.Role),
			FirstName: user.FirstName,
			LastName:  user.LastName,
		}

		// Email and Phone are nullable (*string) in the DB.
		// Only include them in the response if they have a value.
		if user.Email != nil {
			profile.Email = *user.Email
		}
		if user.Phone != nil {
			profile.Phone = *user.Phone
		}

		writeJSON(w, http.StatusOK, map[string]interface{}{
			"data": profile,
		})
	}
}

// =============================================================================
// JSON response helper
// =============================================================================

// writeJSON sends a JSON response with the given HTTP status code and payload.
// This is the counterpart to writeError — used for success responses.
func writeJSON(w http.ResponseWriter, status int, payload interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}

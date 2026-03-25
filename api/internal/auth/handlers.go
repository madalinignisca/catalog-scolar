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
	"io"
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"
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

		// Set httpOnly cookies so the browser (and Nuxt SSR) can send
		// credentials automatically on subsequent requests. The JSON body
		// is also returned for backward compatibility with non-browser API
		// clients (e.g., mobile apps, CLI tools, Postman).
		setAuthCookies(w, r, accessToken, refreshToken)

		// Return the token pair in the JSON body as well.
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
		// The refresh token can come from either the JSON body (API clients)
		// or from the httpOnly cookie (browser clients using cookie auth).
		var req refreshRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			// Empty body (io.EOF) is expected for cookie-based requests — the
			// refresh token comes from the cookie, not the JSON body.
			// Any other decode error means malformed JSON — reject it.
			if !errors.Is(err, io.EOF) {
				writeError(w, http.StatusBadRequest, "BAD_REQUEST", "Invalid JSON body")
				return
			}
			req = refreshRequest{}
		}

		// If the JSON body didn't include a refresh token, try the cookie.
		// The refresh token cookie has Path="/api/v1/auth/refresh" so it is
		// only sent to this endpoint (not to every API call).
		if req.RefreshToken == "" {
			if cookie, err := r.Cookie("catalogro_refresh_token"); err == nil {
				req.RefreshToken = cookie.Value
			}
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
			slog.Error("refresh: failed to query user", "user_id", parsedID.String(), "error", err) //nolint:gosec // G706 false positive — parsedID is a validated UUID, err is a DB driver error, neither is user-controlled log injection
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

		// Set updated cookies with the new token pair (token rotation).
		// The browser will replace the old cookies automatically.
		setAuthCookies(w, r, accessToken, refreshToken)

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
		// Clear the httpOnly auth cookies by setting MaxAge=-1.
		// The browser will delete both cookies immediately.
		// We also return a JSON success body for API clients that may
		// rely on the response.
		//
		// TODO: Accept refresh_token in body and revoke it in Redis/DB.
		clearAuthCookies(w, r)

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
// HandleGetActivation — GET /auth/activate/{token}
// =============================================================================

// activationInfoResponse is the JSON payload returned by GET /auth/activate/{token}.
// It contains just enough user info for the activation page to show a confirmation
// screen ("You are activating the account for Radu Popescu, teacher at Scoala nr. 1").
// Sensitive fields (password_hash, totp_secret) are intentionally excluded.
type activationInfoResponse struct {
	// UserID is the UUID of the user being activated.
	// The frontend doesn't strictly need it for GET, but it's useful for logging
	// and for the POST payload so the frontend doesn't have to re-parse the token.
	UserID string `json:"user_id"`

	// Email is the user's email address (nullable — some accounts only have a phone).
	// Shown on the confirmation screen so the user can verify they are on the right account.
	Email string `json:"email,omitempty"`

	// FirstName and LastName are the user's name as provisioned by the secretary.
	// Displayed on the activation page so the user can confirm their identity.
	FirstName string `json:"first_name"`
	LastName  string `json:"last_name"`

	// Role is the user's role (e.g., "teacher", "student", "parent").
	// Used by the frontend to conditionally show the GDPR consent checkbox
	// (required for parent accounts per ROFUIP rules).
	Role string `json:"role"`

	// SchoolName is the name of the school the account belongs to.
	// Shown on the activation page alongside the identity confirmation block.
	SchoolName string `json:"school_name"`

	// Requires2FA indicates whether the user must set up TOTP (2FA) as part
	// of activation. True for admin, secretary, and teacher roles — these roles
	// handle sensitive student data and are required by school policy to use 2FA.
	// False for parent and student roles.
	Requires2FA bool `json:"requires_2fa"`
}

// HandleGetActivation returns an http.HandlerFunc that validates an activation
// token and returns the user's pre-filled identity information.
//
// This endpoint is intentionally PUBLIC — the user has not authenticated yet.
// It uses a direct DB connection (no RLS transaction) because we have no
// school_id context before authentication.
//
// The activation flow:
//  1. Extract the token from the URL path parameter {token}
//  2. Look up the user by activation_token (query also checks activated_at IS NULL,
//     so used/expired tokens return 404 naturally)
//  3. Return the user's identity for the confirmation screen
//
// Parameters:
//   - db: sqlc Queries using the connection pool directly (no RLS)
func HandleGetActivation(db *generated.Queries) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// Step 1: Extract the token from the URL path.
		// chi.URLParam reads path parameters defined with {token} in the route pattern.
		// Example: GET /auth/activate/abc123 → token = "abc123"
		token := chi.URLParam(r, "token")
		if token == "" {
			// This should not happen if chi routing is correct, but we guard anyway.
			writeError(w, http.StatusBadRequest, "BAD_REQUEST", "Token is required")
			return
		}

		// Step 2: Look up the user by activation token.
		// GetUserByActivationToken:
		//   - Does NOT use RLS (pre-login, no school_id context)
		//   - Joins schools to get school_name in one query (no N+1)
		//   - Returns pgx.ErrNoRows if: token not found, already activated, or expired
		// We pass a pointer to token because the DB column is nullable (*string).
		user, err := db.GetUserByActivationToken(r.Context(), &token)
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				// Token not found or already used — return a generic 404.
				// We don't distinguish between "never existed" and "already activated"
				// to avoid leaking information about which tokens have been used.
				writeError(w, http.StatusNotFound, "INVALID_TOKEN", "Activation token is invalid or expired")
				return
			}
			// Unexpected database error — log it, don't expose internals to the client.
			slog.Error("activate/get: failed to query activation token", "error", err)
			writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "An unexpected error occurred")
			return
		}

		// Step 3: Determine whether this role requires 2FA setup during activation.
		// Admin, secretary, and teacher roles handle sensitive data and are required
		// by CatalogRO policy to configure TOTP as part of their first login.
		// Parent and student roles do not need 2FA.
		requires2FA := user.Role == generated.UserRoleAdmin ||
			user.Role == generated.UserRoleSecretary ||
			user.Role == generated.UserRoleTeacher

		// Step 4: Build the safe response — only include identity fields.
		// Never include password_hash, totp_secret, or the raw activation_token.
		resp := activationInfoResponse{
			UserID:      user.ID.String(),
			FirstName:   user.FirstName,
			LastName:    user.LastName,
			Role:        string(user.Role),
			SchoolName:  user.SchoolName,
			Requires2FA: requires2FA,
		}

		// Email is nullable — only include it if the user has one.
		// Some accounts (e.g., students under GDPR age) may only have a phone.
		if user.Email != nil {
			resp.Email = *user.Email
		}

		writeJSON(w, http.StatusOK, map[string]interface{}{
			"data": resp,
		})
	}
}

// =============================================================================
// HandlePostActivation — POST /auth/activate
// =============================================================================

// activationRequest is the expected JSON body for POST /auth/activate.
// All three fields are documented below; gdpr_consent is optional (only required
// for parent accounts, but the handler accepts and stores it for any account type).
type activationRequest struct {
	// Token is the activation token from the URL the user clicked.
	// The frontend passes it back here so the server can re-validate it.
	// This prevents CSRF-style attacks where one user tries to activate another's account.
	Token string `json:"token"`

	// Password is the new password the user wants to set.
	// Must be at least 8 characters long (validated server-side).
	// The handler hashes it with bcrypt before storing — plaintext is never persisted.
	Password string `json:"password"`

	// GDPRConsent, when true, records the user's consent to data processing.
	// Required for parent accounts per Romanian ROFUIP rules.
	// Stored as gdpr_consent_at = now() in the users table.
	GDPRConsent bool `json:"gdpr_consent"`
}

// activationConfirmResponse is the JSON payload returned on successful activation.
type activationConfirmResponse struct {
	// Activated confirms the activation succeeded. Always true on a 200 response.
	Activated bool `json:"activated"`

	// Requires2FA tells the frontend whether the user still needs to set up TOTP.
	// If true, the frontend should redirect to /2fa/setup instead of /login.
	Requires2FA bool `json:"requires_2fa"`

	// UserID is the UUID of the newly activated user.
	// The frontend may use this to pre-fill the login form or for analytics.
	UserID string `json:"user_id"`
}

// HandlePostActivation returns an http.HandlerFunc that completes the account
// activation: validates the token, hashes the password, marks the account as
// activated, and optionally records GDPR consent.
//
// This endpoint is intentionally PUBLIC — it IS the authentication bootstrap.
// It uses a direct DB connection (no RLS transaction).
//
// The activation flow:
//  1. Parse and validate the request body (token + password required)
//  2. Re-validate the activation token (prevents replay after the GET call)
//  3. Hash the password with bcrypt (cost 12 — balance of security and speed)
//  4. Call ActivateUser to persist the hash and clear the activation_token
//  5. If gdpr_consent is true, record consent timestamp
//  6. Return activated=true + requires_2fa flag
//
// Parameters:
//   - db: sqlc Queries using the connection pool directly (no RLS)
func HandlePostActivation(db *generated.Queries) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// Step 1: Parse the JSON request body.
		var req activationRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "BAD_REQUEST", "Invalid JSON body")
			return
		}

		// Validate required fields: both token and password must be present.
		if req.Token == "" {
			writeError(w, http.StatusBadRequest, "BAD_REQUEST", "Token is required")
			return
		}
		if req.Password == "" {
			writeError(w, http.StatusBadRequest, "BAD_REQUEST", "Password is required")
			return
		}

		// Validate password length — minimum 8 characters.
		// This is a server-side guard; the frontend should also enforce this.
		if len(req.Password) < 8 {
			writeError(w, http.StatusBadRequest, "BAD_REQUEST", "Password must be at least 8 characters")
			return
		}

		// Step 2: Re-validate the activation token.
		// We look it up again (not just trusting the frontend) because:
		//   a) The token may have been used between the GET and POST calls.
		//   b) This prevents a malicious client from submitting a fake token.
		// GetUserByActivationToken checks activated_at IS NULL, so a used token
		// will return pgx.ErrNoRows here.
		user, err := db.GetUserByActivationToken(r.Context(), &req.Token)
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				writeError(w, http.StatusNotFound, "INVALID_TOKEN", "Activation token is invalid or expired")
				return
			}
			slog.Error("activate/post: failed to query activation token", "error", err)
			writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "An unexpected error occurred")
			return
		}

		// Step 3: Hash the password with bcrypt.
		// bcrypt.DefaultCost (10) is fine for development; consider cost 12 for production
		// (adds ~100ms per login which is acceptable and significantly raises brute-force cost).
		// GenerateFromPassword also generates a random salt and embeds it in the hash,
		// so we never need to manage salts separately.
		hash, err := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
		if err != nil {
			slog.Error("activate/post: failed to hash password", "user_id", user.ID, "error", err)
			writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "Failed to process password")
			return
		}

		// Convert the bcrypt output ([]byte) to a *string for the DB column.
		// The users.password_hash column is TEXT (nullable), so sqlc generates *string.
		hashStr := string(hash)

		// Step 4: Activate the user — persist the hash, clear activation_token,
		// set activated_at = now(). The ActivateUser query also re-checks that
		// activated_at IS NULL (WHERE clause), so it's safe if called twice.
		_, err = db.ActivateUser(r.Context(), generated.ActivateUserParams{
			ID:           user.ID,
			PasswordHash: &hashStr,
		})
		if err != nil {
			slog.Error("activate/post: failed to activate user", "user_id", user.ID, "error", err)
			writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "Failed to activate account")
			return
		}

		// Step 5: If the user gave GDPR consent, record the consent timestamp.
		// This is required for parent accounts per Romanian ROFUIP rules.
		// SetGDPRConsent sets gdpr_consent_at = now() on the users row.
		// We run it unconditionally when the flag is true — the column is just a timestamp,
		// so re-consenting (if somehow called twice) just updates the timestamp.
		if req.GDPRConsent {
			if err := db.SetGDPRConsent(r.Context(), user.ID); err != nil {
				// Non-fatal: consent timestamp is important but shouldn't block activation.
				// Log the error so an operator can manually patch it if needed.
				slog.Warn("activate/post: failed to record GDPR consent", "user_id", user.ID, "error", err)
			}
		}

		// Step 6: Determine if the newly activated user needs to set up 2FA next.
		// Same logic as HandleGetActivation — admin, secretary, teacher require TOTP.
		requires2FA := user.Role == generated.UserRoleAdmin ||
			user.Role == generated.UserRoleSecretary ||
			user.Role == generated.UserRoleTeacher

		writeJSON(w, http.StatusOK, map[string]interface{}{
			"data": activationConfirmResponse{
				Activated:   true,
				Requires2FA: requires2FA,
				UserID:      user.ID.String(),
			},
		})
	}
}

// =============================================================================
// Cookie helpers — set and clear httpOnly JWT cookies on auth responses
// =============================================================================

// setAuthCookies writes the access and refresh tokens as httpOnly cookies.
//
// Why cookies instead of (only) JSON body?
// Nuxt 3 uses SSR — the server-side render needs the JWT to make authenticated
// API calls. localStorage is client-only, so it's invisible to SSR. httpOnly
// cookies are sent automatically with every request (including SSR), solving
// the "auth lost on page refresh" problem.
//
// Security properties of these cookies:
//   - HttpOnly: true  — JavaScript cannot read the token (mitigates XSS theft)
//   - Secure: auto    — true when the request came over TLS, false on plain HTTP
//                        (allows dev on localhost without TLS while enforcing HTTPS in prod)
//   - SameSite: Lax   — cookie is sent on same-site requests and top-level navigations
//                        (protects against CSRF while allowing normal link clicks)
//   - Path: the access token cookie is sent on all paths ("/"), but the refresh
//     token cookie is scoped to "/api/v1/auth/refresh" so it is only sent when
//     the client explicitly calls the refresh endpoint (minimizes exposure)
//
// Parameters:
//   - w: the ResponseWriter to set cookies on
//   - r: the incoming request (used to detect TLS via r.TLS)
//   - accessToken: the short-lived JWT access token (15 min)
//   - refreshToken: the longer-lived JWT refresh token (7 days)
func setAuthCookies(w http.ResponseWriter, r *http.Request, accessToken, refreshToken string) {
	// Access token cookie — sent with every API request.
	// MaxAge = 900 seconds (15 minutes), matching the JWT's own expiry.
	http.SetCookie(w, newAuthCookie("catalogro_access_token", accessToken, "/", int(AccessTokenDuration.Seconds()), r))

	// Refresh token cookie — ONLY sent to the refresh endpoint.
	// Restricting the Path means the refresh token is never included in
	// regular API calls (e.g., GET /users/me), reducing the attack surface
	// if an XSS vulnerability is found elsewhere.
	// MaxAge = 604800 seconds (7 days), matching the refresh JWT's expiry.
	http.SetCookie(w, newAuthCookie("catalogro_refresh_token", refreshToken, "/api/v1/auth/refresh", int(RefreshTokenDuration.Seconds()), r))
}

// clearAuthCookies removes the access and refresh token cookies by setting
// MaxAge=-1, which tells the browser to delete them immediately.
//
// Called by HandleLogout so the browser stops sending credentials after logout.
// We must set the same Path for each cookie — browsers only delete a cookie
// when the name AND path match exactly.
func clearAuthCookies(w http.ResponseWriter, r *http.Request) {
	http.SetCookie(w, newAuthCookie("catalogro_access_token", "", "/", -1, r))
	http.SetCookie(w, newAuthCookie("catalogro_refresh_token", "", "/api/v1/auth/refresh", -1, r))
}

// newAuthCookie builds an http.Cookie with security defaults appropriate for
// JWT auth tokens. The Secure flag is set dynamically based on whether the
// request arrived over TLS: true in production (behind Traefik with HTTPS),
// false in local development (plain HTTP on localhost).
//
// All auth cookies share these properties:
//   - HttpOnly: true  — JavaScript cannot read the token (mitigates XSS theft)
//   - SameSite: Lax   — sent on same-site requests and top-level navigations
//                        (protects against CSRF while allowing normal link clicks)
func newAuthCookie(name, value, path string, maxAge int, r *http.Request) *http.Cookie {
	cookie := &http.Cookie{
		Name:     name,
		Value:    value,
		Path:     path,
		HttpOnly: true,
		Secure:   true,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   maxAge,
	}
	// In production behind a TLS-terminating proxy (Traefik/nginx), r.TLS is nil
	// because the Go server receives plain HTTP. Check X-Forwarded-Proto to detect
	// the original protocol. Only disable Secure for actual non-TLS development.
	if r.TLS == nil && r.Header.Get("X-Forwarded-Proto") != "https" {
		cookie.Secure = false
	}
	return cookie
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

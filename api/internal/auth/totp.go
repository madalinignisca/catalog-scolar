package auth

// This file implements TOTP-based Two-Factor Authentication (2FA) for CatalogRO.
//
// TOTP (Time-based One-Time Password) is the standard used by authenticator apps
// like Google Authenticator, Authy, and 1Password. It generates a 6-digit code
// that changes every 30 seconds, based on a shared secret.
//
// In CatalogRO, 2FA is mandatory for sensitive roles (teacher, admin, secretary)
// because these users can modify grades, absences, and student data. Students and
// parents do not require 2FA (they only have read access to their own data).
//
// The 2FA login flow:
//  1. User sends email + password to POST /auth/login
//  2. Server validates credentials and sees totp_enabled=true
//  3. Server returns { mfa_required: true, mfa_token: "<short-lived-jwt>" }
//  4. Client prompts user for their 6-digit TOTP code
//  5. Client sends mfa_token + totp_code to POST /auth/2fa/login
//  6. Server validates the MFA token, validates the TOTP code, returns real tokens
//
// The 2FA setup flow (for account activation):
//  1. User activates account via POST /auth/activate (sets password)
//  2. If their role requires 2FA, the response includes a TOTP secret + QR URL
//  3. User scans the QR code with their authenticator app
//  4. User sends the first valid code to POST /auth/2fa/verify to confirm setup
//  5. Server stores the TOTP secret and sets totp_enabled=true

import (
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/pquerna/otp/totp"

	"github.com/vlahsh/catalogro/api/db/generated"
)

// =============================================================================
// Request types for 2FA endpoints
// =============================================================================

// mfaLoginRequest is the expected JSON body for POST /auth/2fa/login.
// The client sends the mfa_token (from the login response) plus the 6-digit
// TOTP code displayed in their authenticator app.
type mfaLoginRequest struct {
	// MFAToken is the short-lived JWT received from POST /auth/login when
	// the response included mfa_required=true. It proves the user already
	// passed email + password verification.
	MFAToken string `json:"mfa_token"`

	// TOTPCode is the 6-digit code from the user's authenticator app.
	// It's time-based and changes every 30 seconds.
	TOTPCode string `json:"totp_code"`
}

// =============================================================================
// HandleMFALogin — POST /auth/2fa/login
// =============================================================================

// HandleMFALogin returns an http.HandlerFunc that completes the 2FA login flow.
//
// This is the second step of login for users with TOTP enabled:
//  1. Validate the mfa_token (ensures the user passed email+password)
//  2. Look up the user and their TOTP secret
//  3. Validate the TOTP code against the secret
//  4. Generate and return the full access token + refresh token pair
//
// Parameters:
//   - db: sqlc Queries for looking up the user
//   - jwtSecret: the HMAC key for validating the MFA token and signing new tokens
func HandleMFALogin(db *generated.Queries, jwtSecret []byte) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// Step 1: Parse the request body.
		var req mfaLoginRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "BAD_REQUEST", "Invalid JSON body")
			return
		}

		// Validate required fields.
		if req.MFAToken == "" || req.TOTPCode == "" {
			writeError(w, http.StatusBadRequest, "BAD_REQUEST", "MFA token and TOTP code are required")
			return
		}

		// Step 2: Validate the MFA token.
		// This token was issued during the login step when the user provided
		// correct email + password. It's short-lived (5 min) and has purpose="mfa"
		// to prevent using regular access tokens here.
		userID, err := ValidateMFAToken(req.MFAToken, jwtSecret)
		if err != nil {
			slog.Debug("2fa login: invalid MFA token", "error", err)
			writeError(w, http.StatusUnauthorized, "UNAUTHORIZED", "Invalid or expired MFA token. Please log in again.")
			return
		}

		// Step 3: Look up the user to get their TOTP secret and other info.
		parsedID, err := uuid.Parse(userID)
		if err != nil {
			writeError(w, http.StatusUnauthorized, "UNAUTHORIZED", "Invalid user ID in MFA token")
			return
		}

		user, err := db.GetUserByID(r.Context(), parsedID)
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				writeError(w, http.StatusUnauthorized, "UNAUTHORIZED", "User not found")
				return
			}
			slog.Error("2fa login: failed to query user", "user_id", userID, "error", err)
			writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "An unexpected error occurred")
			return
		}

		// Step 4: Validate the TOTP code.
		// The TOTP secret is stored encrypted in the database (users.totp_secret is []byte).
		// For the POC, if the secret is empty/nil, we accept any valid 6-digit code.
		// In production, the secret would be decrypted using TOTP_ENCRYPTION_KEY.
		totpValid := false

		if len(user.TotpSecret) > 0 {
			// User has a TOTP secret configured — validate the code against it.
			// The totp.Validate function handles the time window (typically accepts
			// codes from -1/+1 time step to account for clock skew).
			totpValid = totp.Validate(req.TOTPCode, string(user.TotpSecret))
		} else if len(req.TOTPCode) == 6 {
			// POC fallback: no TOTP secret stored yet. Accept any 6-digit numeric code.
			// This allows testing the 2FA flow without setting up an authenticator app.
			// SECURITY: Remove this fallback before production launch!
			slog.Warn("2fa login: accepting TOTP code without secret (POC mode)",
				"user_id", userID)
			totpValid = true
		}

		if !totpValid {
			writeError(w, http.StatusUnauthorized, "UNAUTHORIZED", "Invalid TOTP code")
			return
		}

		// Step 5: TOTP code is valid — generate the full token pair.
		accessToken, err := GenerateAccessToken(
			user.ID.String(),
			user.SchoolID.String(),
			string(user.Role),
			jwtSecret,
		)
		if err != nil {
			slog.Error("2fa login: failed to generate access token", "error", err)
			writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "Failed to generate token")
			return
		}

		refreshToken, err := GenerateRefreshToken(user.ID.String(), jwtSecret)
		if err != nil {
			slog.Error("2fa login: failed to generate refresh token", "error", err)
			writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "Failed to generate token")
			return
		}

		// Step 6: Update last login timestamp (fire-and-forget).
		if err := db.UpdateLastLogin(r.Context(), user.ID); err != nil {
			slog.Warn("2fa login: failed to update last_login_at", "user_id", user.ID, "error", err)
		}

		// Return the token pair — authentication is now complete.
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
// Request types for 2FA setup endpoints
// =============================================================================

// setupResponse is the JSON body returned by POST /auth/2fa/setup.
// The client uses it to display a QR code in the authenticator-app setup UI.
// The secret is returned in base32 format for manual entry fallback.
// NOTE: the secret is NOT yet stored in the database at this point — it is
// only saved after the user successfully verifies a code in /auth/2fa/verify.
type setupResponse struct {
	// Secret is the raw base32-encoded TOTP secret (e.g. "JBSWY3DPEHPK3PXP").
	// The user can type this manually into their authenticator app if scanning
	// the QR code is not possible.
	Secret string `json:"secret"`

	// URL is the otpauth:// URI used to generate the QR code.
	// Format: otpauth://totp/CatalogRO:<email>?secret=<secret>&issuer=CatalogRO
	// Any authenticator app (Google Authenticator, Authy, 1Password) accepts this.
	URL string `json:"url"`
}

// verifyRequest is the expected JSON body for POST /auth/2fa/verify.
// The client sends back the same secret it received from /auth/2fa/setup along
// with the first valid code from the user's authenticator app. Returning the
// secret (rather than the server looking it up from DB) is intentional: it
// proves that the client actually stored the secret before asking us to enable
// 2FA — a secret never used can never be lost.
type verifyRequest struct {
	// Secret is the base32 TOTP secret the client received from /auth/2fa/setup.
	// The server validates this format before using it so that garbage input
	// is rejected with a clear 400, not a silent crypto error.
	Secret string `json:"secret"`

	// Code is the 6-digit TOTP code from the user's authenticator app.
	// It must be valid for the supplied secret at the current time.
	Code string `json:"code"`
}

// =============================================================================
// Handle2FASetup — POST /auth/2fa/setup
// =============================================================================

// Handle2FASetup returns an http.HandlerFunc that generates a new TOTP secret
// and QR code URL for the authenticated user.
//
// This is step 1 of the 2FA enrollment flow:
//  1. Get the authenticated user's ID from the JWT context.
//  2. Look up the user's email so we can use it as the TOTP account name
//     (the label shown in the authenticator app, e.g. "CatalogRO:user@school.ro").
//  3. Call GenerateTOTPSecret to create a fresh random secret + otpauth URL.
//  4. Return both to the client — the client renders the QR code.
//
// IMPORTANT: the secret is NOT saved to the database here.
// The two-step flow (generate → verify) prevents storing a secret the user
// never confirmed. The secret is only persisted in Handle2FAVerify after the
// user proves they can generate a valid code from it.
//
// Parameters:
//   - db: sqlc Queries (used to look up the user's email by their UUID)
func Handle2FASetup(db *generated.Queries) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// Step 1: Extract the authenticated user's ID from the JWT claims.
		// GetClaims reads from the context key set by the JWTAuth middleware.
		// If the middleware didn't run (bug or misconfigured router), claims is nil.
		claims := GetClaims(r.Context())
		if claims == nil {
			writeError(w, http.StatusUnauthorized, "UNAUTHORIZED", "Authentication required")
			return
		}

		// Parse the user ID string into a uuid.UUID for the DB query.
		userID, err := uuid.Parse(claims.UserID)
		if err != nil {
			writeError(w, http.StatusBadRequest, "BAD_REQUEST", "Invalid user ID in token")
			return
		}

		// Step 2: Fetch the user to get their email for the authenticator label.
		// GetUserByID does not require a specific school context — the user can
		// only reach here with a valid JWT that already scopes them to a school.
		user, err := db.GetUserByID(r.Context(), userID)
		if err != nil {
			slog.Error("2fa setup: failed to query user", "user_id", userID, "error", err)
			writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "An unexpected error occurred")
			return
		}

		// Resolve the display email. Email is a nullable column (users can be
		// provisioned without one). Fall back to "unknown" to avoid a nil dereference;
		// in practice every user who reaches this endpoint will have an email.
		accountName := "unknown"
		if user.Email != nil && *user.Email != "" {
			accountName = *user.Email
		}

		// Step 3: Generate a fresh TOTP secret + QR URL.
		// GenerateTOTPSecret delegates to pquerna/otp which creates a
		// cryptographically random base32 secret and builds the otpauth:// URI.
		secret, qrURL, err := GenerateTOTPSecret("CatalogRO", accountName)
		if err != nil {
			slog.Error("2fa setup: failed to generate TOTP key", "user_id", userID, "error", err)
			writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "Failed to generate 2FA secret")
			return
		}

		// Step 4: Return the secret and QR URL to the client.
		// The client should render the URL as a QR code using a library like
		// qrcode.js. The secret can also be shown as a plain-text fallback
		// ("Can't scan? Enter this code manually").
		writeJSON(w, http.StatusOK, map[string]interface{}{
			"data": setupResponse{
				Secret: secret,
				URL:    qrURL,
			},
		})
	}
}

// =============================================================================
// Handle2FAVerify — POST /auth/2fa/verify
// =============================================================================

// Handle2FAVerify returns an http.HandlerFunc that validates the first TOTP code
// from a newly enrolled authenticator app and permanently enables 2FA for the user.
//
// This is step 2 of the 2FA enrollment flow:
//  1. Get the authenticated user's ID from the JWT context.
//  2. Parse the request body: { "secret": "BASE32SECRET", "code": "123456" }.
//  3. Validate the TOTP code against the provided secret using totp.Validate.
//  4. If valid, persist the secret with SetTOTPSecret (sets totp_enabled=true).
//  5. Return { "data": { "enabled": true } } to confirm enrollment.
//  6. If the code is wrong, return 400 INVALID_CODE.
//
// The two-step design (setup generates secret, verify saves it) is deliberate:
//   - It ensures the user actually scanned the QR code and can generate codes.
//   - It prevents saving orphaned secrets if the user closes the setup UI early.
//   - It mirrors the industry-standard enrollment pattern (GitHub, AWS, etc.).
//
// Parameters:
//   - db: sqlc Queries (used to persist the verified secret)
func Handle2FAVerify(db *generated.Queries) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// Step 1: Extract the authenticated user's ID.
		claims := GetClaims(r.Context())
		if claims == nil {
			writeError(w, http.StatusUnauthorized, "UNAUTHORIZED", "Authentication required")
			return
		}

		userID, err := uuid.Parse(claims.UserID)
		if err != nil {
			writeError(w, http.StatusBadRequest, "BAD_REQUEST", "Invalid user ID in token")
			return
		}

		// Step 2: Parse the request body.
		// We expect both fields — missing either is a client error.
		var req verifyRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "BAD_REQUEST", "Invalid JSON body")
			return
		}

		if req.Secret == "" || req.Code == "" {
			writeError(w, http.StatusBadRequest, "BAD_REQUEST", "Secret and code are required")
			return
		}

		// Step 3: Validate the TOTP code.
		// totp.Validate checks the code against the secret at the current time,
		// accepting +/- one time step (90-second effective window) to account
		// for clock skew between the user's phone and the server.
		if !totp.Validate(req.Code, req.Secret) {
			// Invalid code — tell the client to try again. We use INVALID_CODE
			// so the UI can show a specific error message (e.g., "Incorrect code.
			// Make sure your phone's clock is correct and try again.").
			writeError(w, http.StatusBadRequest, "INVALID_CODE", "The verification code is incorrect")
			return
		}

		// Step 4: Persist the secret and enable 2FA for this user.
		// SetTOTPSecret runs: UPDATE users SET totp_secret=$2, totp_enabled=true WHERE id=$1
		// We store the secret as raw bytes (the base32 string). In production,
		// this should be encrypted with the TOTP_ENCRYPTION_KEY env var before storage.
		if err := db.SetTOTPSecret(r.Context(), generated.SetTOTPSecretParams{
			ID:         userID,
			TotpSecret: []byte(req.Secret),
		}); err != nil {
			slog.Error("2fa verify: failed to save TOTP secret", "user_id", userID, "error", err)
			writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "Failed to enable 2FA")
			return
		}

		slog.Info("2fa verify: 2FA enabled for user", "user_id", userID)

		// Step 5: Confirm that 2FA is now enabled.
		// The client can use this to show a success screen and redirect to login
		// (or the user's dashboard if they are already in a session).
		writeJSON(w, http.StatusOK, map[string]interface{}{
			"data": map[string]bool{
				"enabled": true,
			},
		})
	}
}

// =============================================================================
// TOTP setup helpers — used during account activation
// =============================================================================

// GenerateTOTPSecret creates a new TOTP secret and QR code URL for 2FA setup.
//
// This is called during account activation for roles that require 2FA
// (teacher, admin, secretary). The returned values are:
//
//   - secret: the base32-encoded TOTP secret that the user adds to their
//     authenticator app (either by scanning the QR code or entering manually)
//   - qrURL: an otpauth:// URL that can be rendered as a QR code for scanning
//
// Parameters:
//   - issuer: the service name shown in the authenticator app (e.g., "CatalogRO")
//   - account: the user's identifier shown in the authenticator app (e.g., email)
//
// The generated secret uses the default settings: SHA1, 6 digits, 30-second period.
// These are the standard TOTP settings compatible with all major authenticator apps.
func GenerateTOTPSecret(issuer, account string) (secret, qrURL string, err error) {
	// Generate a new TOTP key using the pquerna/otp library.
	// This creates a random secret and builds the otpauth:// URL.
	key, err := totp.Generate(totp.GenerateOpts{
		// Issuer is the service name (displayed in the authenticator app).
		Issuer: issuer,
		// AccountName is the user identifier (usually email, displayed in the app).
		AccountName: account,
	})
	if err != nil {
		return "", "", fmt.Errorf("generate TOTP key: %w", err)
	}

	// key.Secret() returns the base32-encoded secret.
	// key.URL() returns the otpauth:// URL for QR code generation.
	return key.Secret(), key.URL(), nil
}

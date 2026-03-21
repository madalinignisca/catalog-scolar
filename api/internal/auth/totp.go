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

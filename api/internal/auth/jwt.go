// Package auth provides authentication and authorization for the CatalogRO API.
//
// This package implements:
//   - JWT access/refresh token generation and validation
//   - Chi middleware for authentication, tenant isolation (RLS), and role checks
//   - HTTP handlers for login, token refresh, logout, and profile retrieval
//   - TOTP-based two-factor authentication (2FA) for sensitive roles
//
// The JWT flow works like this:
//  1. User sends email + password to POST /auth/login
//  2. Server validates credentials and returns an access token (15 min) + refresh token (7 days)
//  3. If the user has 2FA enabled, step 2 returns an mfa_token instead, and the user
//     must call POST /auth/2fa/login with the mfa_token + TOTP code to get the real tokens
//  4. The access token is sent as "Authorization: Bearer <token>" on every API request
//  5. When the access token expires, the client calls POST /auth/refresh with the refresh token
package auth

import (
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// =============================================================================
// Claims — the data we embed inside every JWT token
// =============================================================================

// Claims holds the custom data stored inside a CatalogRO JWT.
//
// Every access token contains:
//   - UserID:   the UUID of the authenticated user (maps to users.id in the DB)
//   - SchoolID: the UUID of the user's school (used by RLS to isolate data)
//   - Role:     the user's role (admin, secretary, teacher, parent, student)
//
// The standard RegisteredClaims include "sub" (subject = UserID), "exp" (expiry),
// "iat" (issued at), and "iss" (issuer = "catalogro").
type Claims struct {
	// UserID is the unique identifier for the user. Same as the "sub" (subject)
	// claim in the JWT standard, but stored here for convenience so we don't have
	// to parse RegisteredClaims.Subject every time.
	UserID string `json:"user_id"`

	// SchoolID is the tenant identifier. The RLS middleware uses this to set
	// "app.current_school_id" in PostgreSQL, ensuring that every query only sees
	// data belonging to this school.
	SchoolID string `json:"school_id"`

	// Role is the user's role within their school. Used by RequireRole middleware
	// to enforce route-level access control (e.g., only admins can provision users).
	Role string `json:"role"`

	// RegisteredClaims contains standard JWT fields: exp, iat, sub, iss, etc.
	jwt.RegisteredClaims
}

// MFAClaims is a limited-purpose JWT used during the 2FA login flow.
//
// When a user with TOTP enabled sends correct email+password, they don't get
// a full access token yet. Instead they get an mfa_token (short-lived, 5 min)
// that they must exchange + a valid TOTP code for the real tokens.
//
// This token only contains the UserID — no SchoolID or Role — so it cannot be
// used to access any protected endpoints. The "purpose" field is set to "mfa"
// to distinguish it from regular access/refresh tokens.
type MFAClaims struct {
	// UserID is the user who successfully authenticated with email+password
	// but still needs to provide their TOTP code.
	UserID string `json:"user_id"`

	// Purpose is always "mfa" — this prevents someone from using a regular
	// access token in place of an MFA token (the validation checks this field).
	Purpose string `json:"purpose"`

	// RegisteredClaims contains standard JWT fields.
	jwt.RegisteredClaims
}

// =============================================================================
// Token lifetimes — defined as constants for easy tuning
// =============================================================================

const (
	// AccessTokenDuration is how long an access token is valid.
	// 15 minutes is short enough that a leaked token has limited damage potential,
	// but long enough that the client doesn't need to refresh constantly.
	AccessTokenDuration = 15 * time.Minute

	// RefreshTokenDuration is how long a refresh token is valid.
	// 7 days matches common session expectations for school staff who use the
	// app daily — they stay logged in for a week without re-entering credentials.
	RefreshTokenDuration = 7 * 24 * time.Hour

	// MFATokenDuration is how long the intermediate MFA token is valid.
	// 5 minutes gives the user enough time to open their authenticator app
	// and type in the 6-digit code, without leaving a wide attack window.
	MFATokenDuration = 5 * time.Minute

	// TokenIssuer is the "iss" claim in every JWT. This identifies CatalogRO
	// as the system that issued the token, which is useful if tokens are ever
	// validated by external systems.
	TokenIssuer = "catalogro"
)

// =============================================================================
// Token generation functions
// =============================================================================

// GenerateAccessToken creates a new JWT access token for an authenticated user.
//
// Parameters:
//   - userID:   the user's UUID (from users.id)
//   - schoolID: the user's school UUID (from users.school_id)
//   - role:     the user's role string (e.g., "teacher", "admin")
//   - secret:   the HMAC secret key used to sign the token (from config.JWTSecret)
//
// Returns the signed token string, or an error if signing fails.
//
// The resulting token contains all the information needed to:
//  1. Identify the user (sub / user_id)
//  2. Set the RLS tenant context (school_id)
//  3. Check role-based permissions (role)
func GenerateAccessToken(userID, schoolID, role string, secret []byte) (string, error) {
	// Build the claims with the user's identity and a 15-minute expiry.
	now := time.Now()
	claims := Claims{
		UserID:   userID,
		SchoolID: schoolID,
		Role:     role,
		RegisteredClaims: jwt.RegisteredClaims{
			// Subject is the standard JWT field for "who is this token for?"
			Subject: userID,
			// Issuer identifies the system that created this token.
			Issuer: TokenIssuer,
			// IssuedAt records when the token was created (useful for audit logs).
			IssuedAt: jwt.NewNumericDate(now),
			// ExpiresAt is the hard deadline — after this, the token is rejected.
			ExpiresAt: jwt.NewNumericDate(now.Add(AccessTokenDuration)),
		},
	}

	// Create a new token using HMAC-SHA256 signing.
	// HS256 is chosen because:
	// - Both the issuer (API) and validator (API) are the same service
	// - It's simpler than RSA/ECDSA (no key pairs to manage)
	// - It's fast and widely supported
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)

	// Sign the token with our secret key. This produces the final "xxxxx.yyyyy.zzzzz" string.
	signed, err := token.SignedString(secret)
	if err != nil {
		return "", fmt.Errorf("sign access token: %w", err)
	}

	return signed, nil
}

// GenerateRefreshToken creates a new JWT refresh token for an authenticated user.
//
// Refresh tokens are longer-lived (7 days) and are used only to obtain new
// access tokens via POST /auth/refresh. They contain minimal claims (just the
// user ID) because they should never be used to access protected resources directly.
//
// Parameters:
//   - userID: the user's UUID
//   - secret: the HMAC secret key
//
// Returns the signed refresh token string, or an error if signing fails.
func GenerateRefreshToken(userID string, secret []byte) (string, error) {
	now := time.Now()
	claims := jwt.RegisteredClaims{
		Subject:   userID,
		Issuer:    TokenIssuer,
		IssuedAt:  jwt.NewNumericDate(now),
		ExpiresAt: jwt.NewNumericDate(now.Add(RefreshTokenDuration)),
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)

	signed, err := token.SignedString(secret)
	if err != nil {
		return "", fmt.Errorf("sign refresh token: %w", err)
	}

	return signed, nil
}

// GenerateMFAToken creates a short-lived token for the 2FA verification step.
//
// This token is returned by the login handler when the user has TOTP enabled.
// The client must send this token + a valid TOTP code to POST /auth/2fa/login
// to complete authentication and receive the real access + refresh tokens.
//
// Parameters:
//   - userID: the user's UUID (they've passed email+password but not TOTP yet)
//   - secret: the HMAC secret key
//
// Returns the signed MFA token string, or an error if signing fails.
func GenerateMFAToken(userID string, secret []byte) (string, error) {
	now := time.Now()
	claims := MFAClaims{
		UserID:  userID,
		Purpose: "mfa",
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   userID,
			Issuer:    TokenIssuer,
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(MFATokenDuration)),
		},
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)

	signed, err := token.SignedString(secret)
	if err != nil {
		return "", fmt.Errorf("sign mfa token: %w", err)
	}

	return signed, nil
}

// =============================================================================
// Token validation functions
// =============================================================================

// ValidateToken parses and validates a JWT access token string.
//
// It checks:
//  1. The token is well-formed (three base64 segments separated by dots)
//  2. The signature is valid (using the provided secret)
//  3. The token has not expired (exp claim is in the future)
//  4. The signing method is HMAC (prevents algorithm-switching attacks)
//
// Returns the extracted Claims on success, or an error describing what went wrong.
//
// This function is used by the JWTAuth middleware on every authenticated request.
func ValidateToken(tokenString string, secret []byte) (*Claims, error) {
	// Parse the token with a key function that validates the signing method.
	// The keyFunc is called by the parser to determine which key to use for
	// signature verification. We use it to enforce that only HMAC is accepted.
	token, err := jwt.ParseWithClaims(tokenString, &Claims{}, func(token *jwt.Token) (interface{}, error) {
		// SECURITY: Reject any signing method that isn't HMAC.
		// Without this check, an attacker could send a token signed with "none"
		// algorithm or an RSA public key, bypassing signature verification.
		if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", token.Header["alg"])
		}
		return secret, nil
	})
	if err != nil {
		return nil, fmt.Errorf("parse token: %w", err)
	}

	// Extract our custom Claims from the parsed token.
	claims, ok := token.Claims.(*Claims)
	if !ok || !token.Valid {
		return nil, fmt.Errorf("invalid token claims")
	}

	return claims, nil
}

// ValidateRefreshToken parses and validates a JWT refresh token string.
//
// Refresh tokens only contain RegisteredClaims (no custom fields like school_id
// or role), because they are only used to identify the user for token rotation.
//
// Returns the user ID (from the "sub" claim) on success, or an error.
func ValidateRefreshToken(tokenString string, secret []byte) (string, error) {
	token, err := jwt.ParseWithClaims(tokenString, &jwt.RegisteredClaims{}, func(token *jwt.Token) (interface{}, error) {
		// Same HMAC enforcement as ValidateToken — never trust the alg header.
		if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", token.Header["alg"])
		}
		return secret, nil
	})
	if err != nil {
		return "", fmt.Errorf("parse refresh token: %w", err)
	}

	claims, ok := token.Claims.(*jwt.RegisteredClaims)
	if !ok || !token.Valid {
		return "", fmt.Errorf("invalid refresh token claims")
	}

	return claims.Subject, nil
}

// ValidateMFAToken parses and validates an MFA-purpose JWT token.
//
// This is stricter than ValidateToken because it also checks:
//   - The "purpose" field is exactly "mfa" (prevents using access tokens as MFA tokens)
//
// Returns the user ID on success, or an error.
func ValidateMFAToken(tokenString string, secret []byte) (string, error) {
	token, err := jwt.ParseWithClaims(tokenString, &MFAClaims{}, func(token *jwt.Token) (interface{}, error) {
		if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", token.Header["alg"])
		}
		return secret, nil
	})
	if err != nil {
		return "", fmt.Errorf("parse mfa token: %w", err)
	}

	claims, ok := token.Claims.(*MFAClaims)
	if !ok || !token.Valid {
		return "", fmt.Errorf("invalid mfa token claims")
	}

	// SECURITY: Verify this is actually an MFA token, not a repurposed access token.
	if claims.Purpose != "mfa" {
		return "", fmt.Errorf("token purpose is %q, expected \"mfa\"", claims.Purpose)
	}

	return claims.UserID, nil
}

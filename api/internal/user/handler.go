// Package user implements HTTP handlers for user provisioning in the CatalogRO API.
//
// This file covers the user management endpoints that allow school admins
// and secretaries to create and inspect user accounts, as well as the
// self-service profile update endpoint for all authenticated users:
//
//	POST /users                              — provision a new user (teacher, parent, student, etc.)
//	GET  /users                              — list all active users in the current school
//	GET  /users/pending                      — list users who have not yet completed account activation
//	POST /users/{userId}/resend-activation   — generate a fresh activation token for a pending user
//	PUT  /users/me                           — update the current user's own profile (email, phone)
//
// Authorization context:
//   - POST /users, GET /users, and GET /users/pending are restricted to admin
//     and secretary roles. RequireRole middleware enforces this.
//   - PUT /users/me is accessible to ALL authenticated users. Each user can
//     only update their own profile — the user ID always comes from the JWT,
//     never from a URL parameter. This prevents horizontal privilege escalation.
//   - Row-Level Security (RLS) is set by TenantContext middleware before these
//     handlers run, so all DB queries are automatically scoped to the current school.
//
// Domain context:
//   - In CatalogRO there is NO self-registration. Every account is provisioned by
//     a secretary or admin with known user data (name, email, role).
//   - After provisioning, the user receives an activation link. Until they activate
//     their account (set a password + optional 2FA), they appear in /users/pending.
//   - The activation_token is a 32-byte random hex string. It is returned in the
//     POST /users response so the secretary can share it with the user, and it is
//     also used to build the activation URL.
//   - SECURITY INVARIANT: UpdateProfile NEVER allows changing role, school_id, or
//     is_active. Only email and phone are mutable via this endpoint.
package user

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/mail"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/vlahsh/catalogro/api/db/generated"
	"github.com/vlahsh/catalogro/api/internal/auth"
	"github.com/vlahsh/catalogro/api/internal/httputil"
)

// Handler holds the dependencies shared across all user-provisioning HTTP handlers.
// It is created once at application startup (via NewHandler) and reused for every
// request. All fields are safe for concurrent use.
type Handler struct {
	// queries is the sqlc-generated query interface backed by the connection pool.
	// For user provisioning, we use the transaction-scoped Queries stored in the
	// request context by TenantContext middleware (auth.GetQueries(ctx)), NOT this
	// field directly. This field is kept for potential future use (e.g., admin
	// queries that bypass RLS) and to match the Handler pattern used by catalog and school.
	queries *generated.Queries

	// logger is the structured logger for recording errors and debugging info.
	// Every unexpected error should be logged here with relevant context.
	logger *slog.Logger

	// baseURL is the application's public base URL (e.g., "http://localhost:3000"
	// in development, "https://app.catalogro.ro" in production). It is used to
	// construct the activation URL: {baseURL}/activate/{token}.
	baseURL string
}

// NewHandler creates a new user Handler with the given dependencies.
//
// Parameters:
//   - queries: pool-level sqlc Queries (not the transaction-scoped one — that
//     comes from auth.GetQueries(ctx) inside each handler).
//   - logger: structured logger from slog.
//   - baseURL: the APP_BASE_URL env var value (e.g., "http://localhost:3000").
func NewHandler(queries *generated.Queries, logger *slog.Logger, baseURL string) *Handler {
	return &Handler{
		queries: queries,
		logger:  logger,
		baseURL: baseURL,
	}
}

// =============================================================================
// POST /users — Provision a new user account
// =============================================================================

// provisionUserRequest is the expected JSON body for POST /users.
//
// The secretary or admin fills in all known information about the new user.
// NO self-registration: accounts are always created by a privileged user.
type provisionUserRequest struct {
	// Role is the user's role in the school system.
	// Must be one of: "admin", "secretary", "teacher", "parent", "student".
	// This determines what the user can see and do once their account is active.
	Role string `json:"role"`

	// Email is the user's email address (optional, but strongly recommended).
	// The activation link will be sent to this address.
	// For students under the GDPR minimum age, email may be omitted.
	Email string `json:"email,omitempty"`

	// Phone is the user's phone number (optional).
	// For students/parents without email, the activation link can be sent via SMS.
	Phone string `json:"phone,omitempty"`

	// FirstName is the user's first name as it appears on official documents.
	// Required — used in the activation confirmation screen.
	FirstName string `json:"first_name"`

	// LastName is the user's family name.
	// Required — used in the activation confirmation screen and for alphabetical sorting.
	LastName string `json:"last_name"`

	// SiiirStudentID is the student's ID in the national SIIIR system
	// (Sistemul Informatic Integrat al Învățământului din România).
	// Optional — only relevant for student accounts. Used for interoperability.
	SiiirStudentID *string `json:"siiir_student_id,omitempty"`
}

// provisionUserResponse is the JSON structure returned on successful user creation.
// It includes the activation URL so the provisioning admin/secretary can share it.
type provisionUserResponse struct {
	// ID is the UUID of the newly created user in the CatalogRO database.
	ID string `json:"id"`

	// Email is the email address stored on the user record (may be empty).
	Email string `json:"email,omitempty"`

	// Role is the role assigned to the new user (e.g., "teacher", "student").
	Role string `json:"role"`

	// ActivationToken is the raw 64-character hex token. The secretary may
	// use this to manually construct an activation URL if needed.
	ActivationToken string `json:"activation_token"`

	// ActivationURL is the full clickable URL the user should open to set their
	// password and complete account setup. Format: {APP_BASE_URL}/activate/{token}.
	ActivationURL string `json:"activation_url"`
}

// allowedRoles is the set of valid role strings for user provisioning.
// Using a map enables O(1) lookup instead of looping through a slice.
var allowedRoles = map[string]bool{
	"admin":     true,
	"secretary": true,
	"teacher":   true,
	"parent":    true,
	"student":   true,
}

// ProvisionUser handles POST /users.
//
// Creates a new user account for the current school. The new user does not
// yet have a password — they must follow the activation link to set one.
//
// Handler flow:
//  1. Retrieve the transaction-scoped Queries from context (RLS is active)
//  2. Parse and validate the JSON request body
//  3. Generate a cryptographically random 32-byte activation token
//  4. Get the provisioning user's ID from the JWT context (for audit trail)
//  5. Insert the new user into the database via ProvisionUser query
//  6. Return 201 Created with the new user's ID and activation URL
//
// Possible responses:
//   - 201 Created:             { "data": { "id": "...", "activation_url": "..." } }
//   - 400 Bad Request:         validation failure (missing fields, invalid role)
//   - 401 Unauthorized:        auth context missing (should not happen in protected group)
//   - 500 Internal Server Error: DB failure or token generation failure
func (h *Handler) ProvisionUser(w http.ResponseWriter, r *http.Request) {
	// Step 1: Retrieve the transaction-scoped Queries object from the request context.
	// TenantContext middleware sets this up before this handler runs. All queries
	// executed through this object are automatically scoped to the current school
	// (via PostgreSQL RLS using app.current_school_id).
	queries := auth.GetQueries(r.Context())
	if queries == nil {
		// This should never happen if the middleware chain is correctly wired.
		// If it does, it means TenantContext middleware did not run before this handler.
		h.logger.Error("provision_user: queries not found in context — is TenantContext middleware active?")
		httputil.InternalError(w)
		return
	}

	// Step 2: Parse the JSON request body.
	var req provisionUserRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		// Malformed JSON — reject immediately with a 400.
		httputil.BadRequest(w, "INVALID_JSON", "Request body must be valid JSON")
		return
	}

	// Step 3: Validate required fields.
	// Role, first_name, and last_name are always required.
	// Email is strongly recommended but optional (some accounts are phone-only).
	if req.Role == "" {
		httputil.BadRequest(w, "MISSING_FIELD", "role is required")
		return
	}
	if req.FirstName == "" {
		httputil.BadRequest(w, "MISSING_FIELD", "first_name is required")
		return
	}
	if req.LastName == "" {
		httputil.BadRequest(w, "MISSING_FIELD", "last_name is required")
		return
	}

	// Step 4: Validate that the provided role is one of the allowed values.
	// CatalogRO has exactly five roles: admin, secretary, teacher, parent, student.
	// Rejecting unknown roles prevents accidental privilege escalation via typos.
	if !allowedRoles[req.Role] {
		httputil.BadRequest(w, "INVALID_ROLE",
			"role must be one of: admin, secretary, teacher, parent, student")
		return
	}

	// Step 5: Generate a secure, random activation token.
	// We use 32 bytes of cryptographic randomness from crypto/rand (NOT math/rand).
	// Encoding as hex gives a 64-character string that is URL-safe.
	// This token is single-use: it is cleared from the DB when the user activates.
	activationToken, err := generateActivationToken()
	if err != nil {
		h.logger.Error("provision_user: failed to generate activation token", "error", err)
		httputil.InternalError(w)
		return
	}

	// Step 6: Get the provisioning user's ID from the JWT context.
	// This is stored in the audit column users.provisioned_by so we can trace
	// who created each account. auth.GetUserID reads from the JWT claims set by
	// JWTAuth middleware.
	provisionerID, err := auth.GetUserID(r.Context())
	if err != nil {
		// GetUserID only errors if the claims are missing — covered by JWTAuth.
		// Return 401 to be safe, but this path should not be reachable in practice.
		h.logger.Warn("provision_user: could not get provisioner user ID", "error", err)
		httputil.Unauthorized(w, "Authentication required")
		return
	}

	// Step 7: Convert optional string fields to the nullable *string pointers
	// that the sqlc-generated ProvisionUserParams expects.
	// A nil pointer maps to NULL in PostgreSQL; a non-nil pointer maps to the value.
	var emailPtr *string
	if req.Email != "" {
		emailPtr = &req.Email
	}

	var phonePtr *string
	if req.Phone != "" {
		phonePtr = &req.Phone
	}

	// pgtype.UUID is the type sqlc uses for nullable UUID columns.
	// ProvisionedBy maps to users.provisioned_by (nullable FK to users.id).
	provisionedBy := pgtype.UUID{
		Bytes: provisionerID,
		Valid: true,
	}

	// Step 8: Insert the new user into the database.
	// ProvisionUser uses current_school_id() for the school_id column, so RLS
	// ensures we only ever insert into the correct school — no school_id param needed.
	// The query also sets activation_sent_at = now() automatically.
	newUser, err := queries.ProvisionUser(r.Context(), generated.ProvisionUserParams{
		Role:            generated.UserRole(req.Role),
		Email:           emailPtr,
		Phone:           phonePtr,
		FirstName:       req.FirstName,
		LastName:        req.LastName,
		ProvisionedBy:   provisionedBy,
		SiiirStudentID:  req.SiiirStudentID,
		ActivationToken: &activationToken,
	})
	if err != nil {
		// Could be a DB constraint violation (e.g., duplicate email in the same school)
		// or an RLS error. Log the full error for debugging.
		h.logger.Error("provision_user: failed to insert user",
			"error", err,
			"role", req.Role,
			"provisioner_id", provisionerID,
		)
		httputil.InternalError(w)
		return
	}

	// Step 9: Build the activation URL.
	// The frontend activation page lives at /activate/{token} on the APP_BASE_URL.
	// The user (or the secretary on their behalf) opens this URL to set their password.
	activationURL := fmt.Sprintf("%s/activate/%s", h.baseURL, activationToken)

	// Step 10: Build the response — include the activation token and URL but
	// NEVER include sensitive fields like password_hash or totp_secret.
	resp := provisionUserResponse{
		ID:              newUser.ID.String(),
		Role:            string(newUser.Role),
		ActivationToken: activationToken,
		ActivationURL:   activationURL,
	}

	// Email is nullable — only include it if the user was created with one.
	if newUser.Email != nil {
		resp.Email = *newUser.Email
	}

	// Return 201 Created — this signals to the client that a new resource was created.
	httputil.Created(w, resp)
}

// =============================================================================
// GET /users — List all active users in the current school
// =============================================================================

// userResponse is the safe JSON shape for a user in list responses.
// It intentionally omits all sensitive fields: password_hash, totp_secret,
// activation_token, and the raw activation_sent_at timestamp.
type userResponse struct {
	// ID is the user's UUID primary key.
	ID string `json:"id"`

	// SchoolID is the tenant's school UUID. Included for convenience when
	// the admin views this data in a multi-school context.
	SchoolID string `json:"school_id"`

	// Role is one of: admin, secretary, teacher, parent, student.
	Role string `json:"role"`

	// Email is the user's email address. May be empty for phone-only accounts.
	Email string `json:"email,omitempty"`

	// Phone is the user's phone number. May be empty.
	Phone string `json:"phone,omitempty"`

	// FirstName is the user's given name.
	FirstName string `json:"first_name"`

	// LastName is the user's family/surname.
	LastName string `json:"last_name"`

	// IsActive indicates whether the account is enabled.
	// False means the account has been administratively deactivated.
	IsActive bool `json:"is_active"`

	// ActivatedAt is the timestamp when the user completed account activation
	// (set their password). Null/empty if the account is still pending activation.
	ActivatedAt *time.Time `json:"activated_at,omitempty"`

	// CreatedAt is when the account was provisioned.
	CreatedAt time.Time `json:"created_at"`
}

// ListUsers handles GET /users.
//
// Returns all active users in the current school, ordered alphabetically by
// last_name, first_name. Row-Level Security ensures that only users belonging
// to the current tenant school are returned — no school_id filter param needed.
//
// Possible responses:
//   - 200 OK:                  { "data": [ {...}, ... ] }
//   - 401 Unauthorized:        auth context missing
//   - 500 Internal Server Error: database failure
func (h *Handler) ListUsers(w http.ResponseWriter, r *http.Request) {
	// Step 1: Retrieve the transaction-scoped Queries from context.
	// All queries run through this object are automatically scoped to the
	// current school by PostgreSQL RLS (app.current_school_id is set).
	queries := auth.GetQueries(r.Context())
	if queries == nil {
		h.logger.Error("list_users: queries not found in context — is TenantContext middleware active?")
		httputil.InternalError(w)
		return
	}

	// Step 2: Fetch all active users for the current school from the database.
	// ListUsersBySchool filters by is_active = true and orders by last_name, first_name.
	// RLS restricts the results to the current school automatically.
	users, err := queries.ListUsersBySchool(r.Context())
	if err != nil {
		// Unexpected DB error — log and return 500.
		h.logger.Error("list_users: failed to query users", "error", err)
		httputil.InternalError(w)
		return
	}

	// Step 3: Map each database User model to the safe API response shape.
	// We use a pre-allocated slice for efficiency (avoid re-allocations in the loop).
	result := make([]userResponse, 0, len(users))
	for i := range users {
		// Use index-based iteration (&users[i]) to avoid copying the entire User
		// struct on each iteration — the struct is large due to nullable fields.
		result = append(result, mapUserToResponse(&users[i]))
	}

	// Step 4: Return the list wrapped in the standard { "data": [...] } envelope.
	httputil.Success(w, result)
}

// =============================================================================
// GET /users/pending — List users with pending account activation
// =============================================================================

// pendingUserResponse is the JSON shape for a pending activation entry.
// It is a subset of userResponse — we only need the fields relevant to
// tracking who still needs to activate their account.
type pendingUserResponse struct {
	// ID is the user's UUID.
	ID string `json:"id"`

	// Email is the email address to which the activation link was sent.
	Email string `json:"email,omitempty"`

	// Phone is the phone number (for SMS-activated accounts).
	Phone string `json:"phone,omitempty"`

	// Role is the role assigned to this user. Shown in the UI so the secretary
	// knows who each pending activation belongs to.
	Role string `json:"role"`

	// FirstName and LastName identify the user in the pending list.
	FirstName string `json:"first_name"`
	LastName  string `json:"last_name"`

	// ActivationSentAt is when the activation link was generated (= when the account
	// was provisioned, since activation_sent_at is set to now() on INSERT).
	// If this is far in the past, the secretary may want to resend the link.
	ActivationSentAt *time.Time `json:"activation_sent_at,omitempty"`

	// CreatedAt is when the account was provisioned (set_at same as activation_sent_at
	// for new accounts, but kept for clarity).
	CreatedAt time.Time `json:"created_at"`
}

// ListPendingActivations handles GET /users/pending.
//
// Returns all users in the current school who have not yet completed account
// activation (activated_at IS NULL). These are accounts that were provisioned
// but whose owners have not yet set a password via the activation link.
//
// Results are ordered by created_at DESC (most recently provisioned first),
// so the secretary can quickly see the latest additions.
//
// Possible responses:
//   - 200 OK:                  { "data": [ {...}, ... ] }
//   - 401 Unauthorized:        auth context missing
//   - 500 Internal Server Error: database failure
func (h *Handler) ListPendingActivations(w http.ResponseWriter, r *http.Request) {
	// Step 1: Retrieve the transaction-scoped Queries from context.
	// RLS is active — only pending users from the current school are returned.
	queries := auth.GetQueries(r.Context())
	if queries == nil {
		h.logger.Error("list_pending_activations: queries not found in context — is TenantContext middleware active?")
		httputil.InternalError(w)
		return
	}

	// Step 2: Fetch all pending users (activated_at IS NULL, is_active = true).
	// Ordered by created_at DESC — most recently provisioned accounts appear first,
	// making it easy for the secretary to spot freshly created accounts that
	// haven't received or clicked their activation link yet.
	pending, err := queries.ListPendingActivations(r.Context())
	if err != nil {
		h.logger.Error("list_pending_activations: failed to query pending users", "error", err)
		httputil.InternalError(w)
		return
	}

	// Step 3: Map each User record to the leaner pendingUserResponse shape.
	// We only expose the fields the secretary needs to manage pending activations.
	result := make([]pendingUserResponse, 0, len(pending))
	for i := range pending {
		result = append(result, mapUserToPendingResponse(&pending[i]))
	}

	// Step 4: Return the list wrapped in the standard { "data": [...] } envelope.
	httputil.Success(w, result)
}

// =============================================================================
// Helpers
// =============================================================================

// mapUserToResponse converts a generated.User database model to the safe API
// response struct, stripping all sensitive fields.
//
// This helper is used by ListUsers (and could be reused by a future GET /users/{id}
// endpoint). Keeping it separate avoids duplicating the nullable-field handling.
func mapUserToResponse(u *generated.User) userResponse {
	resp := userResponse{
		ID:        u.ID.String(),
		SchoolID:  u.SchoolID.String(),
		Role:      string(u.Role),
		FirstName: u.FirstName,
		LastName:  u.LastName,
		IsActive:  u.IsActive,
		CreatedAt: u.CreatedAt,
	}

	// Email is a nullable *string in the DB — only set it if the user has one.
	if u.Email != nil {
		resp.Email = *u.Email
	}

	// Phone is also nullable — only set it if present.
	if u.Phone != nil {
		resp.Phone = *u.Phone
	}

	// ActivatedAt is a pgtype.Timestamptz (nullable timestamp with time zone).
	// Valid=true means the column is NOT NULL (i.e., the user has activated).
	// We expose this so the UI can show "Activated on <date>" vs. "Pending".
	if u.ActivatedAt.Valid {
		t := u.ActivatedAt.Time
		resp.ActivatedAt = &t
	}

	return resp
}

// mapUserToPendingResponse converts a generated.User database model to the
// pending user API response struct. Similar to mapUserToResponse but includes
// activation_sent_at instead of activated_at, and omits is_active (pending
// users are always active — they just haven't set their password yet).
func mapUserToPendingResponse(u *generated.User) pendingUserResponse {
	resp := pendingUserResponse{
		ID:        u.ID.String(),
		Role:      string(u.Role),
		FirstName: u.FirstName,
		LastName:  u.LastName,
		CreatedAt: u.CreatedAt,
	}

	if u.Email != nil {
		resp.Email = *u.Email
	}

	if u.Phone != nil {
		resp.Phone = *u.Phone
	}

	if u.ActivationSentAt.Valid {
		t := u.ActivationSentAt.Time
		resp.ActivationSentAt = &t
	}

	return resp
}

// =============================================================================
// GET /users/me/children — List children linked to the current user
// =============================================================================

// childResponse is the JSON shape for a single child (student) record in the
// GET /users/me/children response. It combines basic identity fields with
// the child's current class enrollment details.
//
// Class fields are nullable because a child may not yet be enrolled in any class —
// for example, a newly provisioned student account that has not been assigned to
// a class yet.
type childResponse struct {
	// ID is the student's UUID primary key.
	ID string `json:"id"`

	// FirstName is the student's given name.
	FirstName string `json:"first_name"`

	// LastName is the student's family/surname.
	LastName string `json:"last_name"`

	// Email is the student's email address. May be empty for phone-only accounts
	// or for young students where no email was collected.
	Email string `json:"email,omitempty"`

	// Role is always "student" for children returned by this endpoint.
	Role string `json:"role"`

	// ClassID is the UUID of the class the student is currently enrolled in.
	// Null/empty if the student is not enrolled in any active class.
	ClassID string `json:"class_id,omitempty"`

	// ClassName is the human-readable class name (e.g., "2A", "6B", "10A").
	// Null/empty if the student is not enrolled in any active class.
	ClassName string `json:"class_name,omitempty"`

	// ClassEducationLevel is the education level of the class (primary/middle/high).
	// Null/empty if the student is not enrolled in any active class.
	ClassEducationLevel string `json:"class_education_level,omitempty"`
}

// ListChildren handles GET /users/me/children.
//
// Returns all students (children) linked to the currently authenticated user
// via the parent_student_links table. Each child entry also includes the class
// the student is currently enrolled in (if any).
//
// This endpoint is accessible to ALL authenticated users — not just parents.
// A teacher might also want to see which children are linked to their parent
// accounts for communication purposes. The data returned is always scoped to
// the current school via RLS.
//
// Handler flow:
//  1. Retrieve the transaction-scoped Queries from context (RLS is active)
//  2. Extract the current user's ID from the JWT claims
//  3. Query ListChildrenForParent with the user's ID as the parent_id
//  4. Map each row to a childResponse (with nullable class fields)
//  5. Return 200 OK with { "data": [ ... ] }
//
// Possible responses:
//   - 200 OK:                  { "data": [ {...}, ... ] } (empty array if no children)
//   - 401 Unauthorized:        auth context missing
//   - 500 Internal Server Error: database failure
func (h *Handler) ListChildren(w http.ResponseWriter, r *http.Request) {
	// Step 1: Retrieve the transaction-scoped Queries from context.
	// TenantContext middleware sets this up before this handler runs. All queries
	// executed through this object are automatically scoped to the current school
	// (via PostgreSQL RLS using app.current_school_id).
	queries := auth.GetQueries(r.Context())
	if queries == nil {
		h.logger.Error("list_children: queries not found in context — is TenantContext middleware active?")
		httputil.InternalError(w)
		return
	}

	// Step 2: Get the current user's UUID from the JWT claims.
	// auth.GetUserID reads the user_id field from the Claims struct stored in
	// context by the JWTAuth middleware.
	userID, err := auth.GetUserID(r.Context())
	if err != nil {
		h.logger.Warn("list_children: could not get user ID from context", "error", err)
		httputil.Unauthorized(w, "Authentication required")
		return
	}

	// Step 3: Query the database for all children (students) linked to this user.
	// ListChildrenForParent joins users → parent_student_links → class_enrollments → classes.
	// If the user has no linked children, the query returns an empty slice (not an error).
	// If a child has no active enrollment, class_id, class_name, and class_education_level
	// will be NULL (represented as pgtype.UUID{Valid: false} and nil *string in Go).
	children, err := queries.ListChildrenForParent(r.Context(), userID)
	if err != nil {
		h.logger.Error("list_children: failed to query children", "error", err, "user_id", userID)
		httputil.InternalError(w)
		return
	}

	// Step 4: Map each row to the safe API response shape.
	// We use a pre-allocated slice to avoid re-allocations in the loop.
	// Importantly, we always return an array (never null) — even for users
	// with no linked children. This makes the client-side handling easier
	// because the frontend can always iterate data without a nil check.
	result := make([]childResponse, 0, len(children))
	for i := range children {
		// Use index-based iteration to avoid copying the 128-byte ListChildrenForParentRow
		// struct on each iteration — the same pattern used throughout this package.
		row := &children[i]

		// Build the base response with the child's identity fields.
		resp := childResponse{
			ID:        row.ID.String(),
			FirstName: row.FirstName,
			LastName:  row.LastName,
			Role:      string(row.Role),
		}

		// Email is a nullable *string in the DB — only include it if present.
		if row.Email != nil {
			resp.Email = *row.Email
		}

		// ClassID is a nullable pgtype.UUID — Valid=true means the child IS enrolled.
		// LEFT JOIN means this will be a zero UUID with Valid=false when not enrolled.
		// pgtype.UUID.Bytes is [16]byte — we format it as the standard 8-4-4-4-12 UUID string.
		if row.ClassID.Valid {
			resp.ClassID = uuid.UUID(row.ClassID.Bytes).String()
		}

		// ClassName is a nullable *string — only include if the child is enrolled.
		if row.ClassName != nil {
			resp.ClassName = *row.ClassName
		}

		// ClassEducationLevel is a NullEducationLevel (custom nullable type generated
		// by sqlc). Valid=true means the column had a non-NULL value.
		if row.ClassEducationLevel.Valid {
			resp.ClassEducationLevel = string(row.ClassEducationLevel.EducationLevel)
		}

		result = append(result, resp)
	}

	// Step 5: Return the list wrapped in the standard { "data": [...] } envelope.
	httputil.Success(w, result)
}


// =============================================================================
// PUT /users/me — Update the current user's own profile
// =============================================================================

// updateProfileRequest is the JSON body for PUT /users/me.
//
// Both fields are optional. Omitting a field (or sending null) leaves the
// corresponding column unchanged in the database (COALESCE semantics in SQL).
// This means a client can update only the phone without touching the email,
// and vice versa.
//
// SECURITY NOTE: This struct intentionally does NOT include role, school_id,
// or is_active. Even if a malicious client sends those fields in the body, they
// are silently ignored because we only read Email and Phone here. The underlying
// SQL query also only updates email and phone — see UpdateUserProfile in users.sql.
type updateProfileRequest struct {
	// Email is the new email address. Optional — omit or send null to keep current.
	// If provided, must contain "@" (basic format check). CatalogRO does not do
	// full RFC 5321 validation here; the DB unique constraint prevents duplicates.
	Email *string `json:"email,omitempty"`

	// Phone is the new phone number. Optional — omit or send null to keep current.
	// Format is not validated server-side (Romanian phone numbers vary in notation:
	// "0741000001", "0741-000-001", "+40741000001" are all accepted).
	Phone *string `json:"phone,omitempty"`
}

// UpdateProfile handles PUT /users/me.
//
// Allows any authenticated user to update their own email and/or phone number.
// The user ID is taken from the JWT claims — users can NEVER update another
// user's profile via this endpoint (no URL parameter for ID).
//
// Security invariant: role, school_id, and is_active are NOT updatable via this
// endpoint. The SQL query (UpdateUserProfile) only touches email, phone, and
// updated_at. The handler does not even read those fields from the request body.
//
// Handler flow:
//  1. Retrieve the transaction-scoped Queries from context (RLS is active)
//  2. Extract the current user's UUID from the JWT claims
//  3. Parse and validate the JSON request body
//  4. Validate email format if a new email is provided
//  5. Call UpdateUserProfile with the new values (nulls preserved by COALESCE)
//  6. Return 200 OK with the updated user data (safe fields only)
//
// Possible responses:
//   - 200 OK:                  { "data": { "id": "...", "email": "...", ... } }
//   - 400 Bad Request:         invalid JSON or invalid email format
//   - 401 Unauthorized:        auth context missing
//   - 500 Internal Server Error: DB failure
func (h *Handler) UpdateProfile(w http.ResponseWriter, r *http.Request) {
	// Step 1: Retrieve the transaction-scoped Queries from context.
	// TenantContext middleware sets this up. All queries through this object
	// are scoped to the authenticated user's school via PostgreSQL RLS.
	queries := auth.GetQueries(r.Context())
	if queries == nil {
		// Shouldn't happen if TenantContext is in the middleware chain.
		h.logger.Error("update_profile: queries not found in context — is TenantContext middleware active?")
		httputil.InternalError(w)
		return
	}

	// Step 2: Get the current user's UUID from the JWT claims.
	// SECURITY: We ALWAYS use the ID from the JWT, never from a URL parameter.
	// This means a user can only ever update their own profile. There is no way
	// to escalate and update another user's record via this endpoint.
	userID, err := auth.GetUserID(r.Context())
	if err != nil {
		// GetUserID only errors if claims are missing — JWTAuth should have caught this.
		h.logger.Warn("update_profile: could not get user ID from context", "error", err)
		httputil.Unauthorized(w, "Authentication required")
		return
	}

	// Step 3: Parse the JSON request body.
	// We decode into updateProfileRequest where both fields are *string (nullable).
	// A missing field stays nil (no update); an explicit null also stays nil.
	// An empty body (io.EOF) is valid — it produces a no-op update that returns
	// the current values unchanged.
	var req updateProfileRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil && err != io.EOF {
		// Body is present but not valid JSON — return 400.
		httputil.BadRequest(w, "INVALID_JSON", "Request body must be valid JSON")
		return
	}

	// Step 4: Validate the email format if a new email is provided.
	// We use Go's net/mail.ParseAddress which validates against RFC 5322.
	// This catches edge cases like "@", "user@", "@domain.com", and missing TLDs
	// while still accepting valid formats like "user+tag@sub.domain.co.uk".
	if req.Email != nil {
		trimmed := strings.TrimSpace(*req.Email)
		if _, err := mail.ParseAddress(trimmed); err != nil {
			httputil.BadRequest(w, "INVALID_EMAIL", "Email must be a valid email address")
			return
		}
		req.Email = &trimmed
	}

	// Step 5: Run the UPDATE query.
	// UpdateUserProfile uses COALESCE($2, email) and COALESCE($3, phone) in SQL,
	// so a nil pointer means "keep the current value" — no explicit check needed here.
	// SECURITY: The query only touches email, phone, and updated_at. Fields like
	// role, school_id, and is_active are NOT part of the SET clause.
	updated, err := queries.UpdateUserProfile(r.Context(), generated.UpdateUserProfileParams{
		ID:    userID,
		Email: req.Email,
		Phone: req.Phone,
	})
	if err != nil {
		// This could be a unique constraint violation (duplicate email) or another
		// DB error. Log with the user ID for debugging.
		h.logger.Error("update_profile: failed to update user",
			"error", err,
			"user_id", userID,
		)
		httputil.InternalError(w)
		return
	}

	// Step 6: Map the updated user record to the safe API response shape.
	// mapUserToResponse strips sensitive fields: password_hash, totp_secret,
	// activation_token. This is the same helper used by ListUsers.
	resp := mapUserToResponse(&updated)

	// Return 200 OK — the profile was updated (or unchanged) successfully.
	httputil.Success(w, resp)
}

// =============================================================================
// POST /users/{userId}/resend-activation — Re-send an activation link
// =============================================================================

// resendActivationResponse is the JSON body returned by a successful
// POST /users/{userId}/resend-activation call.
// The secretary receives a fresh token and URL to share with the user.
type resendActivationResponse struct {
	// ActivationToken is the newly generated 64-character hex token.
	// The secretary can use this to manually construct a URL if needed,
	// or simply forward the activation_url below.
	ActivationToken string `json:"activation_token"`

	// ActivationURL is the full URL the user must open to set their password.
	// Format: {APP_BASE_URL}/activate/{token}.
	ActivationURL string `json:"activation_url"`

	// SentAt is the timestamp when this new activation token was generated
	// and stored in the database (activation_sent_at column, set to now()).
	// Useful for the secretary to know when the last link was issued.
	SentAt time.Time `json:"sent_at"`
}

// ResendActivation handles POST /users/{userId}/resend-activation.
//
// Generates a new activation token for a user who has NOT yet activated their
// account and replaces the old token in the database. This is useful when:
//   - The original activation email was lost or expired.
//   - The secretary provisioned the account but the user never received the link.
//   - The user requests a fresh link (e.g., they deleted the email by accident).
//
// SECURITY: Only unactivated users (activated_at IS NULL) can receive a new
// token. If the user has already activated, the UPDATE returns no rows and the
// handler responds with 404. This prevents accidentally replacing a live user's
// credentials.
//
// Authorization: restricted to admin and secretary roles. RequireRole middleware
// enforces this before the handler is called.
//
// Handler flow:
//  1. Extract userId from the URL path via chi.URLParam.
//  2. Validate it is a valid UUID — reject with 400 if not.
//  3. Retrieve the transaction-scoped Queries from context (RLS is active).
//  4. Generate a new cryptographically random 64-character hex activation token.
//  5. Call ResendActivation query: UPDATE users SET activation_token=..., activated_at IS NULL.
//  6. If no rows updated (ErrNoRows) → user not found or already activated → 404.
//  7. Return 200 with { "data": { "activation_token": ..., "activation_url": ..., "sent_at": ... } }.
//
// Possible responses:
//   - 200 OK:                  { "data": { "activation_token": "...", "activation_url": "...", "sent_at": "..." } }
//   - 400 Bad Request:         userId is not a valid UUID
//   - 404 Not Found:           user does not exist or is already activated
//   - 401 Unauthorized:        auth context missing
//   - 500 Internal Server Error: token generation failure or unexpected DB error
func (h *Handler) ResendActivation(w http.ResponseWriter, r *http.Request) {
	// Step 1: Extract the userId path parameter from the URL.
	// chi.URLParam reads the {userId} segment registered in main.go as
	// r.Post("/users/{userId}/resend-activation", ...).
	userIDStr := chi.URLParam(r, "userId")

	// Step 2: Parse and validate the UUID.
	// uuid.Parse returns an error if the string is not in standard UUID format
	// (e.g., "not-a-uuid" or an empty string). We surface this as a 400 so
	// the client knows the request itself was malformed — not a server error.
	userID, err := uuid.Parse(userIDStr)
	if err != nil {
		httputil.BadRequest(w, "INVALID_UUID", "userId must be a valid UUID")
		return
	}

	// Step 3: Retrieve the transaction-scoped Queries from context.
	// TenantContext middleware sets this up before this handler runs. All queries
	// executed through this object are automatically scoped to the current school
	// (via PostgreSQL RLS using app.current_school_id).
	queries := auth.GetQueries(r.Context())
	if queries == nil {
		// This should never happen if the middleware chain is correctly wired.
		// If it does, it means TenantContext middleware did not run before this handler.
		h.logger.Error("resend_activation: queries not found in context — is TenantContext middleware active?")
		httputil.InternalError(w)
		return
	}

	// Step 4: Generate a new cryptographically random 64-character hex token.
	// We call the package-level generateActivationToken helper, which reads 32
	// bytes from crypto/rand and hex-encodes them. The result is URL-safe and
	// unpredictable — suitable for use as a one-time activation secret.
	newToken, err := generateActivationToken()
	if err != nil {
		h.logger.Error("resend_activation: failed to generate activation token",
			"error", err,
			"user_id", userID,
		)
		httputil.InternalError(w)
		return
	}

	// Step 5: Update the database row with the new token.
	// ResendActivation runs:
	//   UPDATE users
	//   SET activation_token = $2, activation_sent_at = now(), updated_at = now()
	//   WHERE id = $1 AND activated_at IS NULL
	//   RETURNING *;
	//
	// The WHERE clause ensures we only update unactivated users. If the user has
	// already activated their account (activated_at IS NOT NULL), the UPDATE
	// matches zero rows and pgx returns ErrNoRows.
	//
	// RLS (set by TenantContext) also implicitly scopes the update to the
	// current school — a secretary of school A cannot resend for school B's users.
	updatedUser, err := queries.ResendActivation(r.Context(), generated.ResendActivationParams{
		ID:              userID,
		ActivationToken: &newToken,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			// No rows were updated. This means either:
			//   a) The user ID does not exist in this school (RLS filtered it out).
			//   b) The user exists but activated_at IS NOT NULL (already activated).
			// We return 404 in both cases — revealing which case applies would leak
			// information about other users to the secretary.
			httputil.NotFound(w, "User not found or already activated")
			return
		}
		// Unexpected database error — log and return 500.
		h.logger.Error("resend_activation: failed to update activation token",
			"error", err,
			"user_id", userID,
		)
		httputil.InternalError(w)
		return
	}

	// Step 6: Build the activation URL from the new token.
	// The frontend activation page lives at /activate/{token} on the APP_BASE_URL.
	// The user opens this URL to set their password and complete account setup.
	activationURL := fmt.Sprintf("%s/activate/%s", h.baseURL, newToken)

	// Step 7: Extract activation_sent_at from the updated row for the response.
	// The ResendActivation SQL sets activation_sent_at = now(), so this field
	// is guaranteed to be non-null (Valid=true) after a successful UPDATE.
	var sentAt time.Time
	if updatedUser.ActivationSentAt.Valid {
		sentAt = updatedUser.ActivationSentAt.Time
	}

	// Step 8: Return 200 OK with the new token and URL.
	// We return 200 (not 201) because we are updating an existing resource,
	// not creating a new one. The secretary can share activation_url with the user.
	httputil.Success(w, resendActivationResponse{
		ActivationToken: newToken,
		ActivationURL:   activationURL,
		SentAt:          sentAt,
	})
}

// generateActivationToken creates a cryptographically random 64-character hex
// string suitable for use as an activation token. It is not used directly by
// the handler (which inlines the generation) but is kept here for documentation
// and potential reuse by resend-activation flows.
//
// The token is 32 random bytes encoded as 64 hex characters.
// crypto/rand ensures unpredictability — unlike math/rand, it is seeded by the OS
// kernel and is safe for security-sensitive use cases.
func generateActivationToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("generate activation token: %w", err)
	}
	return hex.EncodeToString(b), nil
}

// =============================================================================
// POST /users/me/gdpr/consent — Record GDPR consent for the current user
// =============================================================================

// RecordConsent handles POST /users/me/gdpr/consent.
//
// Records the authenticated user's GDPR consent by stamping gdpr_consent_at
// with the current timestamp. This endpoint is required for parents who must
// accept the data processing terms before their children's accounts become
// visible to them in the platform.
//
// Romanian law and the EU GDPR (Regulation 2016/679) require that consent is:
//   - Freely given (no coercion)
//   - Specific and informed (user knows what data is processed)
//   - Unambiguous (an affirmative act — calling this endpoint)
//   - Recorded with a timestamp (stored in users.gdpr_consent_at)
//
// Handler flow:
//  1. Retrieve transaction-scoped Queries from context (RLS is active)
//  2. Extract the current user's UUID from the JWT claims
//  3. Call SetGDPRConsent to stamp gdpr_consent_at = now()
//  4. Return 200 OK with { "data": { "consent_recorded": true, "timestamp": "..." } }
//
// Possible responses:
//   - 200 OK:                  { "data": { "consent_recorded": true, "timestamp": "..." } }
//   - 401 Unauthorized:        auth context missing
//   - 500 Internal Server Error: database failure
func (h *Handler) RecordConsent(w http.ResponseWriter, r *http.Request) {
	// Step 1: Retrieve the transaction-scoped Queries from context.
	// TenantContext middleware creates this before our handler runs.
	// All queries run through this object respect the current school's RLS policy.
	queries := auth.GetQueries(r.Context())
	if queries == nil {
		h.logger.Error("record_consent: queries not found in context — is TenantContext middleware active?")
		httputil.InternalError(w)
		return
	}

	// Step 2: Get the current user's UUID from the JWT claims.
	// SECURITY: The user ID always comes from the verified JWT — never from a
	// URL parameter or request body. This prevents a user from recording consent
	// on behalf of another user (horizontal privilege escalation).
	userID, err := auth.GetUserID(r.Context())
	if err != nil {
		h.logger.Warn("record_consent: could not get user ID from context", "error", err)
		httputil.Unauthorized(w, "Authentication required")
		return
	}

	// Step 3: Stamp gdpr_consent_at = now() for this user.
	// SetGDPRConsent is an UPDATE query: UPDATE users SET gdpr_consent_at = now()
	// WHERE id = $1. It is safe to call multiple times (idempotent after first call).
	if err := queries.SetGDPRConsent(r.Context(), userID); err != nil {
		h.logger.Error("record_consent: failed to set GDPR consent",
			"error", err,
			"user_id", userID,
		)
		httputil.InternalError(w)
		return
	}

	// Step 4: Return 200 OK with the consent confirmation and current timestamp.
	// The timestamp is set to now() by the SQL query; we return the Go-level
	// now() as the response timestamp (it will be within milliseconds of the DB value).
	httputil.Success(w, map[string]any{
		"consent_recorded": true,
		"timestamp":        time.Now().UTC().Format(time.RFC3339),
	})
}

// =============================================================================
// POST /users/me/gdpr/export — Export all personal data for the current user
// =============================================================================

// gdprExportProfile is the safe subset of a user's profile included in the GDPR
// data export. Sensitive fields (password_hash, totp_secret, activation_token)
// are intentionally omitted — the export is for the data subject to review,
// not for authentication or security purposes.
//
// GDPR Article 20 (Right to Data Portability) requires that we provide the
// user's data in a "structured, commonly used and machine-readable format."
// JSON satisfies this requirement.
type gdprExportProfile struct {
	// ID is the user's UUID primary key in the CatalogRO system.
	ID string `json:"id"`

	// SchoolID is the school (tenant) the user belongs to.
	SchoolID string `json:"school_id"`

	// Role is the user's role: admin, secretary, teacher, parent, or student.
	Role string `json:"role"`

	// Email is the user's email address. May be null for phone-only accounts.
	Email *string `json:"email,omitempty"`

	// Phone is the user's phone number. May be null.
	Phone *string `json:"phone,omitempty"`

	// FirstName is the user's given name.
	FirstName string `json:"first_name"`

	// LastName is the user's family name.
	LastName string `json:"last_name"`

	// IsActive indicates whether the account is currently active.
	IsActive bool `json:"is_active"`

	// GdprConsentAt is when the user recorded their GDPR consent. Null if not yet consented.
	GdprConsentAt *time.Time `json:"gdpr_consent_at,omitempty"`

	// ActivatedAt is when the user first set their password. Null if account is still pending.
	ActivatedAt *time.Time `json:"activated_at,omitempty"`

	// LastLoginAt is the most recent successful login timestamp. Null if never logged in.
	LastLoginAt *time.Time `json:"last_login_at,omitempty"`

	// CreatedAt is when the account was provisioned.
	CreatedAt time.Time `json:"created_at"`
}

// gdprExportChild is the subset of a child's (student's) data included in the
// parent's GDPR export. We include enough to identify the child and their class,
// but not their grades or absences (deferred to a future bulk export feature).
type gdprExportChild struct {
	// ID is the student's UUID.
	ID string `json:"id"`

	// FirstName and LastName identify the child.
	FirstName string `json:"first_name"`
	LastName  string `json:"last_name"`

	// Email is the child's email address. May be omitted for young students.
	Email string `json:"email,omitempty"`

	// Role is always "student" for children in this export.
	Role string `json:"role"`

	// ClassID is the UUID of the class the student is enrolled in.
	ClassID string `json:"class_id,omitempty"`

	// ClassName is the human-readable class label (e.g., "6B", "10A").
	ClassName string `json:"class_name,omitempty"`

	// ClassEducationLevel is primary, middle, or high.
	ClassEducationLevel string `json:"class_education_level,omitempty"`
}

// ExportData handles POST /users/me/gdpr/export.
//
// Produces a GDPR data portability export for the authenticated user.
// The export includes:
//   - The user's own profile (all fields except password_hash, totp_secret,
//     activation_token — security-sensitive fields are excluded).
//   - If the user is a parent, the list of linked children with their current
//     class enrollment.
//
// NOTE: Grades and absences export is intentionally deferred to a future
// milestone. The profile + children export satisfies the minimum GDPR Art. 20
// requirement. A full audit history export would require joining many tables and
// is better served as an asynchronous PDF/CSV generation job.
//
// Handler flow:
//  1. Retrieve transaction-scoped Queries from context (RLS is active)
//  2. Extract user ID from JWT
//  3. Fetch user profile via GetUserDataExport
//  4. If the user is a parent, fetch children via ListChildrenForParent
//  5. Build export payload, omitting sensitive fields
//  6. Return 200 OK with the export data
//
// Possible responses:
//   - 200 OK:                  { "data": { "profile": {...}, "children": [...] } }
//   - 401 Unauthorized:        auth context missing
//   - 500 Internal Server Error: database failure
func (h *Handler) ExportData(w http.ResponseWriter, r *http.Request) {
	// Step 1: Retrieve the transaction-scoped Queries from context.
	queries := auth.GetQueries(r.Context())
	if queries == nil {
		h.logger.Error("export_data: queries not found in context — is TenantContext middleware active?")
		httputil.InternalError(w)
		return
	}

	// Step 2: Get the current user's UUID from the JWT claims.
	// The export is always scoped to the authenticated user — no other user's
	// data can be requested via this endpoint.
	userID, err := auth.GetUserID(r.Context())
	if err != nil {
		h.logger.Warn("export_data: could not get user ID from context", "error", err)
		httputil.Unauthorized(w, "Authentication required")
		return
	}

	// Step 3: Fetch the user's profile from the database.
	// GetUserDataExport explicitly selects only non-sensitive columns at the SQL
	// level — password_hash, totp_secret, and activation_token never enter memory.
	u, err := queries.GetUserDataExport(r.Context(), userID)
	if err != nil {
		h.logger.Error("export_data: failed to fetch user profile",
			"error", err,
			"user_id", userID,
		)
		httputil.InternalError(w)
		return
	}

	// Step 4: Build the export profile from the safe query result.
	// The SQL query already excludes sensitive columns — no filtering needed here.
	profile := gdprExportProfile{
		ID:        u.ID.String(),
		SchoolID:  u.SchoolID.String(),
		Role:      string(u.Role),
		FirstName: u.FirstName,
		LastName:  u.LastName,
		IsActive:  u.IsActive,
		CreatedAt: u.CreatedAt,
	}

	// Nullable fields — only set them if they have a DB value (Valid=true for
	// pgtype.Timestamptz, non-nil for *string).
	if u.Email != nil {
		profile.Email = u.Email
	}
	if u.Phone != nil {
		profile.Phone = u.Phone
	}
	if u.GdprConsentAt.Valid {
		t := u.GdprConsentAt.Time
		profile.GdprConsentAt = &t
	}
	if u.ActivatedAt.Valid {
		t := u.ActivatedAt.Time
		profile.ActivatedAt = &t
	}
	if u.LastLoginAt.Valid {
		t := u.LastLoginAt.Time
		profile.LastLoginAt = &t
	}

	// Step 5: If the user is a parent, also include their linked children.
	// Non-parent users will have an empty slice here. The children list is
	// included in all exports (it will simply be empty for non-parents), which
	// keeps the response shape consistent regardless of the user's role.
	children := make([]gdprExportChild, 0)
	if u.Role == "parent" {
		// ListChildrenForParent joins parent_student_links → users → class_enrollments → classes.
		// Returns an empty slice (not an error) if the parent has no linked children.
		rows, err := queries.ListChildrenForParent(r.Context(), userID)
		if err != nil {
			h.logger.Error("export_data: failed to fetch children",
				"error", err,
				"user_id", userID,
			)
			httputil.InternalError(w)
			return
		}

		// Map each child row to the export shape. Class fields may be absent
		// if the child is not currently enrolled in any class (LEFT JOIN nulls).
		for i := range rows {
			row := &rows[i]
			child := gdprExportChild{
				ID:        row.ID.String(),
				FirstName: row.FirstName,
				LastName:  row.LastName,
				Role:      string(row.Role),
			}
			if row.Email != nil {
				child.Email = *row.Email
			}
			if row.ClassID.Valid {
				child.ClassID = uuid.UUID(row.ClassID.Bytes).String()
			}
			if row.ClassName != nil {
				child.ClassName = *row.ClassName
			}
			if row.ClassEducationLevel.Valid {
				child.ClassEducationLevel = string(row.ClassEducationLevel.EducationLevel)
			}
			children = append(children, child)
		}
	}

	// Step 6: Return the complete export payload.
	// The response shape is { "data": { "profile": {...}, "children": [...] } }.
	// children is always an array (never null), even for non-parent users.
	httputil.Success(w, map[string]any{
		"profile":  profile,
		"children": children,
	})
}

// =============================================================================
// POST /users/me/gdpr/delete — Request deletion (anonymisation) of own account
// =============================================================================

// RequestDeletion handles POST /users/me/gdpr/delete.
//
// Soft-deletes the authenticated user's own account by:
//   - Setting is_active = false (prevents future logins)
//   - Anonymizing PII: email=NULL, phone=NULL, first_name='DELETED', last_name='USER'
//   - Clearing security secrets: password_hash=NULL, totp_secret=NULL, totp_enabled=false
//
// IMPORTANT: The user row is NEVER hard-deleted. Romanian education law (ROFUIP
// and Law 1/2011) requires that student academic records be retained for 10+ years
// for official transcripts and re-enrolment purposes. Even parent and teacher
// records are kept for audit trail integrity (e.g., who entered which grade).
//
// The JWT access token remains technically valid until it expires (up to 15 minutes),
// but the user will be unable to log in again because GetUserByEmailForLogin filters
// by is_active = true. Any in-flight requests after deletion will succeed only if
// their JWT has not expired yet — this is an acceptable trade-off (see RFC 9068).
//
// SECURITY NOTE: This is a destructive and irreversible action. In a future version,
// this endpoint should require password re-confirmation (re-authentication challenge)
// to prevent CSRF or session hijacking attacks from triggering accidental deletion.
//
// Handler flow:
//  1. Retrieve transaction-scoped Queries from context (RLS is active)
//  2. Extract user ID from JWT
//  3. Call SoftDeleteUser to anonymize the user row
//  4. Return 200 OK with { "data": { "deleted": true } }
//
// Possible responses:
//   - 200 OK:                  { "data": { "deleted": true } }
//   - 401 Unauthorized:        auth context missing
//   - 500 Internal Server Error: database failure
func (h *Handler) RequestDeletion(w http.ResponseWriter, r *http.Request) {
	// Step 1: Retrieve the transaction-scoped Queries from context.
	// TenantContext middleware must have run before this handler.
	queries := auth.GetQueries(r.Context())
	if queries == nil {
		h.logger.Error("request_deletion: queries not found in context — is TenantContext middleware active?")
		httputil.InternalError(w)
		return
	}

	// Step 2: Get the current user's UUID from the JWT claims.
	// SECURITY: User ID comes from the JWT, not from a URL parameter. A user
	// can only delete their own account — never another user's.
	userID, err := auth.GetUserID(r.Context())
	if err != nil {
		h.logger.Warn("request_deletion: could not get user ID from context", "error", err)
		httputil.Unauthorized(w, "Authentication required")
		return
	}

	// Step 3: Soft-delete the user row.
	// SoftDeleteUser runs:
	//   UPDATE users SET is_active=false, email=NULL, phone=NULL,
	//     first_name='DELETED', last_name='USER', password_hash=NULL,
	//     totp_secret=NULL, totp_enabled=false, updated_at=now()
	//   WHERE id = $1
	//
	// After this executes:
	//   - The user cannot log in again (is_active=false blocks login query)
	//   - No PII remains on the row (email, phone, name are cleared/anonymized)
	//   - The row itself is preserved for audit purposes (UUID, school_id, role,
	//     created_at, gdpr_consent_at, activated_at all remain intact)
	//
	// The existing JWT will still pass JWTAuth middleware until it expires
	// (access tokens have a 15-minute lifetime), but since is_active=false the
	// user cannot obtain a new token via login or refresh.
	if err := queries.SoftDeleteUser(r.Context(), userID); err != nil {
		h.logger.Error("request_deletion: failed to soft-delete user",
			"error", err,
			"user_id", userID,
		)
		httputil.InternalError(w)
		return
	}

	// Step 4: Log the deletion for compliance purposes.
	// This log line forms part of the audit trail required by GDPR Art. 5(2)
	// (accountability principle) and Romanian data protection law.
	h.logger.Info("request_deletion: user account anonymised (GDPR Art. 17)",
		"user_id", userID,
	)

	// Step 5: Return 200 OK confirming that the deletion was processed.
	httputil.Success(w, map[string]any{
		"deleted": true,
	})
}


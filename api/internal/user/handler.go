// Package user implements HTTP handlers for user provisioning in the CatalogRO API.
//
// This file covers the three user management endpoints that allow school admins
// and secretaries to create and inspect user accounts:
//
//	POST /users          — provision a new user (teacher, parent, student, etc.)
//	GET  /users          — list all active users in the current school
//	GET  /users/pending  — list users who have not yet completed account activation
//
// Authorization context:
//   - All three endpoints are restricted to admin and secretary roles.
//   - The RequireRole("admin", "secretary") middleware enforces this — it must be
//     applied when these handlers are wired into the router (see cmd/server/main.go).
//   - Row-Level Security (RLS) is set by TenantContext middleware before these handlers
//     run, so all DB queries are automatically scoped to the current school.
//
// Domain context:
//   - In CatalogRO there is NO self-registration. Every account is provisioned by
//     a secretary or admin with known user data (name, email, role).
//   - After provisioning, the user receives an activation link. Until they activate
//     their account (set a password + optional 2FA), they appear in /users/pending.
//   - The activation_token is a 32-byte random hex string. It is returned in the
//     POST /users response so the secretary can share it with the user, and it is
//     also used to build the activation URL.
package user

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"time"

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
			resp.ClassID = formatUUIDBytes(row.ClassID.Bytes)
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

// formatUUIDBytes converts a [16]byte UUID to the standard 8-4-4-4-12 string format.
// pgtype.UUID stores the UUID as raw bytes; this helper converts them to the
// human-readable hyphenated string that the API clients expect.
//
// Example output: "f1000000-0000-0000-0000-000000000001"
func formatUUIDBytes(b [16]byte) string {
	// The standard UUID format is: xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx
	// We use fmt.Sprintf with %x for each segment, zero-padded to the correct width.
	return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x",
		b[0:4],   // 8 hex chars  (4 bytes)
		b[4:6],   // 4 hex chars  (2 bytes)
		b[6:8],   // 4 hex chars  (2 bytes)
		b[8:10],  // 4 hex chars  (2 bytes)
		b[10:16], // 12 hex chars (6 bytes)
	)
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


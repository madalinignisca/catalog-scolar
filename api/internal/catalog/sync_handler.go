// sync_handler.go implements the POST /sync/push endpoint for CatalogRO's
// offline synchronisation system.
//
// OFFLINE SYNC OVERVIEW
// ─────────────────────
// Teachers can enter grades without a network connection. The Nuxt 3 frontend
// stores those mutations in an IndexedDB sync queue (Dexie.js). When the device
// reconnects, it calls POST /sync/push with a batch of mutations to replay.
//
// This handler processes that batch:
//   - Each mutation has a "type" (e.g. "grade") and "action" (create/update/delete).
//   - The client assigns a UUID (client_id) to every mutation BEFORE sending it.
//     If the same client_id is pushed again (duplicate / double-send), the server
//     detects the duplicate via the UNIQUE(school_id, client_id) constraint on
//     the grades table and returns the existing server_id rather than creating a
//     second record. This makes the push endpoint idempotent.
//   - For each mutation the handler returns a result entry: { client_id, status,
//     server_id } so the frontend can remove the item from its queue.
//
// DEDUPLICATION STRATEGY
// ──────────────────────
// Before inserting a new grade we call GetGradeByClientID. If the query
// returns a row, a grade with that client_id already exists on the server —
// this is an idempotent re-push. We return status="synced" with the existing
// server ID so the client can safely mark the item as done without creating
// a duplicate.
//
// ERROR HANDLING PER MUTATION
// ───────────────────────────
// Individual mutation failures do NOT abort the whole batch. A failed mutation
// gets status="error" with a message. The frontend should keep it in the queue
// for a future retry. This partial-success design is important so a single bad
// record does not block a teacher's entire pending queue.
//
// AUTHORIZATION
// ─────────────
// This endpoint lives inside the JWT + RLS middleware group (same as other
// catalog endpoints). Every DB call uses the RLS-scoped queries object from
// context (auth.GetQueries), which means the school_id is automatically set
// and checked at the database level. No additional tenant checks are needed.
package catalog

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/vlahsh/catalogro/api/db/generated"
	"github.com/vlahsh/catalogro/api/internal/auth"
	"github.com/vlahsh/catalogro/api/internal/httputil"
)

// ──────────────────────────────────────────────────────────────────────────────
// Request / response types
// ──────────────────────────────────────────────────────────────────────────────

// syncPushRequest is the top-level JSON body sent by the frontend for
// POST /sync/push. It bundles device metadata with the list of mutations.
//
// Fields:
//   - device_id: A stable UUID that the frontend generates once per device/browser.
//     Used for logging and future conflict resolution. Not stored in DB today.
//   - last_sync_at: The ISO-8601 timestamp of the device's previous successful
//     sync. Used for diagnostics / future pull-delta logic. Not stored today.
//   - mutations: Ordered list of offline mutations to replay on the server.
type syncPushRequest struct {
	// DeviceID is a stable identifier for the offline device (client-generated UUID).
	DeviceID string `json:"device_id"`

	// LastSyncAt is when the device last successfully synced (informational).
	LastSyncAt *time.Time `json:"last_sync_at"`

	// Mutations is the ordered list of offline changes to apply.
	Mutations []syncMutation `json:"mutations"`
}

// syncMutation describes a single offline operation the client wants to replay.
//
// The "type" field identifies the entity kind (currently only "grade").
// The "action" field identifies the operation (create / update / delete).
// The "client_id" is the UUID the client assigned to this mutation — used for
// idempotency: pushing the same client_id twice is safe and returns the same
// server_id both times.
//
// The "data" field holds the entity-specific payload. Its shape depends on
// "type"; for grades it matches createGradeRequest fields.
type syncMutation struct {
	// Type is the entity kind: "grade" (more types can be added later).
	Type string `json:"type"`

	// Action is the operation: "create", "update", or "delete".
	Action string `json:"action"`

	// ClientID is the unique ID the client assigned to this mutation for
	// idempotency. It matches the client_id field inside the data payload.
	ClientID uuid.UUID `json:"client_id"`

	// ClientTimestamp is when the mutation was created on the client device.
	ClientTimestamp time.Time `json:"client_timestamp"`

	// Data contains the entity-specific payload as a raw JSON object.
	// We delay decoding until we know the type, avoiding unnecessary allocations.
	Data json.RawMessage `json:"data"`
}

// gradeMutationData is the payload shape for type="grade" mutations.
// The fields mirror createGradeRequest so the client can reuse the same struct.
//
// Note: client_id and client_timestamp are duplicated from the parent
// syncMutation struct. We accept both locations for flexibility — the parent
// fields are the authoritative source; the nested ones are kept for
// compatibility with the create-grade flow.
type gradeMutationData struct {
	// StudentID is the UUID of the student receiving the grade.
	StudentID uuid.UUID `json:"student_id"`

	// ClassID is the UUID of the class the grade belongs to.
	ClassID uuid.UUID `json:"class_id"`

	// SubjectID is the UUID of the subject for the grade.
	SubjectID uuid.UUID `json:"subject_id"`

	// Semester is "I" or "II" (first or second semester).
	Semester string `json:"semester"`

	// NumericGrade is the 1–10 numeric grade (nil for primary-school qualifiers).
	NumericGrade *int16 `json:"numeric_grade"`

	// QualifierGrade is one of "FB", "B", "S", "I" (nil for numeric grades).
	QualifierGrade *string `json:"qualifier_grade"`

	// IsThesis marks whether this grade is a semester thesis (teza).
	IsThesis bool `json:"is_thesis"`

	// GradeDate is the date string in YYYY-MM-DD format when the grade was given.
	GradeDate string `json:"grade_date"`

	// Description is an optional teacher note attached to the grade.
	Description *string `json:"description"`

	// ServerID is the UUID of the grade on the server (used for update/delete).
	// Required for action="update" and action="delete"; ignored for action="create".
	ServerID *uuid.UUID `json:"server_id"`
}

// syncMutationResult is the per-mutation outcome returned to the client.
//
// The client matches results back to its local queue using client_id.
// A "synced" status means the grade is safely stored on the server.
// An "error" status means the mutation failed — the error_code tells the
// client whether to retry:
//
//   - "transient": temporary failure (DB timeout, connection issue) → retry with backoff
//   - "validation": invalid data (bad format, out of range) → don't retry, needs user fix
//   - "not_found": referenced entity doesn't exist → don't retry
//   - "forbidden": authorization failure → don't retry
//   - "conflict": duplicate or constraint violation → don't retry
type syncMutationResult struct {
	// ClientID is the client-assigned mutation ID (echoed back for matching).
	ClientID uuid.UUID `json:"client_id"`

	// Status is "synced" (success) or "error" (failed).
	Status string `json:"status"`

	// ServerID is the UUID of the grade on the server after a successful
	// create or update. Nil for delete operations.
	ServerID *uuid.UUID `json:"server_id,omitempty"`

	// ErrorCode is a machine-readable error classification for the client's
	// retry logic. Only present when status="error".
	ErrorCode *string `json:"error_code,omitempty"`

	// ErrorMessage is a human-readable description of why the mutation failed.
	// Only present when status="error".
	ErrorMessage *string `json:"error_message,omitempty"`

	// Retryable indicates whether the client should retry this mutation.
	// True for transient errors (DB timeouts), false for validation/auth errors.
	Retryable bool `json:"retryable"`
}

// syncPushResponseData is the body of the data envelope in the response.
type syncPushResponseData struct {
	// Results contains one entry per mutation, in the same order as the request.
	Results []syncMutationResult `json:"results"`

	// ServerTimestamp is the server's UTC clock at the time the batch completed.
	// The client should store this as its new last_sync_at for the next push.
	ServerTimestamp time.Time `json:"server_timestamp"`
}

// ──────────────────────────────────────────────────────────────────────────────
// SyncPush handler
// ──────────────────────────────────────────────────────────────────────────────

// SyncPush handles POST /sync/push.
//
// It accepts a batch of offline mutations from a teacher's device and replays
// them against the database. Each mutation is processed independently:
//   - A successful mutation gets status="synced" with its server_id.
//   - A failed mutation gets status="error" with a message (not aborted).
//
// The endpoint is idempotent: pushing the same client_id twice returns the
// same server_id without creating a duplicate record.
//
// Possible responses:
//   - 200 OK: { "data": { "results": [...], "server_timestamp": "..." } }
//   - 400 Bad Request: malformed JSON
//   - 401 Unauthorized: missing or invalid JWT
//   - 500 Internal Server Error: database failure unrelated to individual mutations
func (h *Handler) SyncPush(w http.ResponseWriter, r *http.Request) {
	// ── Step 1: Extract the authenticated user's identity ─────────────────────
	// We need the teacher's user_id to set as the grade's teacher_id.
	userID, err := auth.GetUserID(r.Context())
	if err != nil {
		httputil.Unauthorized(w, "Authentication required")
		return
	}

	// ── Step 1b: Retrieve the RLS-scoped query object from context ────────────
	// All DB calls in this handler must go through the transaction-scoped
	// Queries so that Row-Level Security (school_id filter) is active.
	queries := auth.GetQueries(r.Context())
	if queries == nil {
		httputil.InternalError(w)
		return
	}

	// ── Step 2: Parse the JSON request body ───────────────────────────────────
	var req syncPushRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httputil.BadRequest(w, "INVALID_JSON", "Request body must be valid JSON")
		return
	}

	// ── Step 3: Resolve the current school year ───────────────────────────────
	// Every grade record requires a school_year_id. We resolve it once here
	// rather than per-mutation to avoid N+1 DB roundtrips.
	//
	// If no school year is configured, all grade mutations will fail with a
	// meaningful error message rather than a cryptic DB error.
	schoolYear, err := queries.GetCurrentSchoolYear(r.Context())
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			httputil.BadRequest(w, "NO_SCHOOL_YEAR", "No current school year is configured")
			return
		}
		h.logger.Error("sync_push: failed to get current school year", "error", err)
		httputil.InternalError(w)
		return
	}

	// ── Step 4: Process each mutation ─────────────────────────────────────────
	// We iterate over the mutations in order and collect results. Failures do
	// NOT abort the whole batch — a single bad mutation gets status="error"
	// and the loop continues. This is intentional: if a teacher has 20 queued
	// grades and 1 is invalid, the other 19 should still sync successfully.
	results := make([]syncMutationResult, 0, len(req.Mutations))

	for i := range req.Mutations {
		// Process the mutation and collect the result.
		// We use indexing (req.Mutations[i]) to avoid copying the large struct.
		result := h.processMutation(r, queries, userID, schoolYear.ID, &req.Mutations[i])
		results = append(results, result)
	}

	// ── Step 5: Return the response ───────────────────────────────────────────
	// server_timestamp tells the client where to start its next sync window.
	httputil.Success(w, syncPushResponseData{
		Results:         results,
		ServerTimestamp: time.Now().UTC(),
	})
}

// ──────────────────────────────────────────────────────────────────────────────
// processMutation — dispatches a single mutation to the appropriate handler
// ──────────────────────────────────────────────────────────────────────────────

// processMutation routes a single sync mutation to the correct apply function
// based on the mutation's type and action. Returns a result that is always
// safe to include in the response (errors are captured as status="error").
//
// Currently supported:
//   - type="grade", action="create" → applyGradeCreate
//   - type="grade", action="update" → applyGradeUpdate
//   - type="grade", action="delete" → applyGradeDelete
//
// Unsupported type/action combinations return status="error" with a clear
// message so the client can log and skip them without retrying forever.
func (h *Handler) processMutation(
	r *http.Request,
	queries *generated.Queries,
	userID uuid.UUID,
	schoolYearID uuid.UUID,
	m *syncMutation,
) syncMutationResult {
	// Route by type first, then by action within that type.
	switch m.Type {
	case "grade":
		switch m.Action {
		case "create":
			return h.applyGradeCreate(r, queries, userID, schoolYearID, m)
		case "update":
			return h.applyGradeUpdate(r, queries, userID, m)
		case "delete":
			return h.applyGradeDelete(r, queries, userID, m)
		default:
			// Unknown action for a known type — return a descriptive error.
			// The client should not retry this because it is a logic error.
			msg := "unsupported action '" + m.Action + "' for type 'grade'"
			return errorResult(m.ClientID, "conflict", msg, false)
		}
	default:
		// Unknown mutation type — skip it gracefully.
		msg := "unsupported mutation type '" + m.Type + "'"
		return errorResult(m.ClientID, "conflict", msg, false)
	}
}

// ──────────────────────────────────────────────────────────────────────────────
// applyGradeCreate — idempotent grade creation
// ──────────────────────────────────────────────────────────────────────────────

// applyGradeCreate handles a type="grade", action="create" mutation.
//
// Deduplication: before inserting we check if a grade with the same client_id
// already exists on the server (UNIQUE(school_id, client_id) constraint).
// If it does, we return the existing server_id with status="synced" — this
// is the idempotent re-push case (double-send / retry after partial failure).
//
// Validation mirrors CreateGrade handler: semester, grade type, date, range.
// Errors that are fixable in a future push get status="error"; the client
// should keep the mutation in the queue and retry after user correction.
func (h *Handler) applyGradeCreate(
	r *http.Request,
	queries *generated.Queries,
	userID uuid.UUID,
	schoolYearID uuid.UUID,
	m *syncMutation,
) syncMutationResult {
	// ── Parse the grade data payload ──────────────────────────────────────────
	var data gradeMutationData
	if err := json.Unmarshal(m.Data, &data); err != nil {
		return errorResult(m.ClientID, "validation", "invalid grade data payload: "+err.Error(), false)
	}

	// ── Idempotency check: has this client_id already been synced? ────────────
	// Convert the mutation's client_id to pgtype.UUID for the DB query.
	clientUUID := pgtype.UUID{Bytes: m.ClientID, Valid: true}

	existing, err := queries.GetGradeByClientID(r.Context(), clientUUID)
	if err == nil {
		// A grade with this client_id already exists — this is a duplicate push.
		// Return the existing server_id so the client can clear its queue item.
		// We log at DEBUG level because this is expected behaviour on retry.
		h.logger.Debug("sync_push: duplicate grade create (idempotent)",
			"client_id", m.ClientID,
			"server_id", existing.ID,
		)
		serverID := existing.ID
		return syncMutationResult{
			ClientID: m.ClientID,
			Status:   "synced",
			ServerID: &serverID,
		}
	}
	// Any error other than "not found" is a real DB failure — return error.
	if !errors.Is(err, pgx.ErrNoRows) {
		h.logger.Error("sync_push: failed to check grade client_id",
			"error", err, "client_id", m.ClientID)
		return errorResult(m.ClientID, "transient", "database error during deduplication check", true)
	}
	// pgx.ErrNoRows means no duplicate — proceed to insert.

	// ── Validate semester ─────────────────────────────────────────────────────
	if data.Semester != "I" && data.Semester != "II" {
		return errorResult(m.ClientID, "validation", "semester must be 'I' or 'II'", false)
	}

	// ── Validate grade value (exactly one of numeric or qualifier) ────────────
	if data.NumericGrade == nil && data.QualifierGrade == nil {
		return errorResult(m.ClientID, "validation", "either numeric_grade or qualifier_grade is required", false)
	}
	if data.NumericGrade != nil && data.QualifierGrade != nil {
		return errorResult(m.ClientID, "validation", "provide either numeric_grade or qualifier_grade, not both", false)
	}
	if data.NumericGrade != nil {
		if *data.NumericGrade < 1 || *data.NumericGrade > 10 {
			return errorResult(m.ClientID, "validation", "numeric_grade must be between 1 and 10", false)
		}
	}

	// ── Build qualifier grade enum ────────────────────────────────────────────
	var qualifierGrade generated.NullQualifier
	if data.QualifierGrade != nil {
		switch *data.QualifierGrade {
		case "FB", "B", "S", "I":
			qualifierGrade = generated.NullQualifier{
				Qualifier: generated.Qualifier(*data.QualifierGrade),
				Valid:     true,
			}
		default:
			return errorResult(m.ClientID, "validation",
				"qualifier_grade must be one of: FB, B, S, I", false)
		}
	}

	// ── Parse grade date ──────────────────────────────────────────────────────
	gradeDate, err := time.Parse("2006-01-02", data.GradeDate)
	if err != nil {
		return errorResult(m.ClientID, "validation", "grade_date must be in YYYY-MM-DD format", false)
	}

	// ── Build client timestamp for the record ─────────────────────────────────
	// Use the mutation's client_timestamp from the parent struct (authoritative).
	// This preserves the offline timestamp so the server knows when the grade
	// was actually created on the device, which matters for conflict resolution.
	clientTimestamp := pgtype.Timestamptz{
		Time:  m.ClientTimestamp,
		Valid: true,
	}

	// ── Insert the grade ──────────────────────────────────────────────────────
	// SyncStatus is set to "synced" because the grade is being applied server-side
	// right now — it is no longer pending from the server's perspective.
	grade, err := queries.CreateGrade(r.Context(), generated.CreateGradeParams{
		StudentID:       data.StudentID,
		ClassID:         data.ClassID,
		SubjectID:       data.SubjectID,
		TeacherID:       userID,
		SchoolYearID:    schoolYearID,
		Semester:        generated.Semester(data.Semester),
		NumericGrade:    data.NumericGrade,
		QualifierGrade:  qualifierGrade,
		IsThesis:        data.IsThesis,
		GradeDate:       pgtype.Date{Time: gradeDate, Valid: true},
		Description:     data.Description,
		ClientID:        clientUUID,
		ClientTimestamp: clientTimestamp,
		SyncStatus:      generated.SyncStatusSynced,
	})
	if err != nil {
		h.logger.Error("sync_push: failed to create grade",
			"error", err,
			"client_id", m.ClientID,
			"student_id", data.StudentID,
		)
		return errorResult(m.ClientID, "transient", "failed to save grade to database", true)
	}

	// ── Return success with the new server-side UUID ──────────────────────────
	serverID := grade.ID
	return syncMutationResult{
		ClientID: m.ClientID,
		Status:   "synced",
		ServerID: &serverID,
	}
}

// ──────────────────────────────────────────────────────────────────────────────
// applyGradeUpdate — update an existing grade from offline queue
// ──────────────────────────────────────────────────────────────────────────────

// applyGradeUpdate handles a type="grade", action="update" mutation.
//
// The client must provide the server_id of the grade to update inside the data
// payload. The same validation rules as UpdateGrade apply.
//
// Authorization: only the teacher who created the grade (or an admin) can
// update it. This mirrors the regular UpdateGrade handler behaviour.
func (h *Handler) applyGradeUpdate(
	r *http.Request,
	queries *generated.Queries,
	userID uuid.UUID,
	m *syncMutation,
) syncMutationResult {
	// ── Parse the grade data payload ──────────────────────────────────────────
	var data gradeMutationData
	if err := json.Unmarshal(m.Data, &data); err != nil {
		return errorResult(m.ClientID, "validation", "invalid grade data payload: "+err.Error(), false)
	}

	// ── Require server_id for update ──────────────────────────────────────────
	// An update without a server_id is a client logic error — we cannot know
	// which grade to update. The client should not retry this as-is.
	if data.ServerID == nil {
		return errorResult(m.ClientID, "validation", "server_id is required for grade update", false)
	}
	gradeID := *data.ServerID

	// ── Validate grade value ──────────────────────────────────────────────────
	if data.NumericGrade == nil && data.QualifierGrade == nil {
		return errorResult(m.ClientID, "validation", "either numeric_grade or qualifier_grade is required", false)
	}
	if data.NumericGrade != nil && data.QualifierGrade != nil {
		return errorResult(m.ClientID, "validation", "provide either numeric_grade or qualifier_grade, not both", false)
	}
	if data.NumericGrade != nil {
		if *data.NumericGrade < 1 || *data.NumericGrade > 10 {
			return errorResult(m.ClientID, "validation", "numeric_grade must be between 1 and 10", false)
		}
	}

	// ── Build qualifier grade enum ────────────────────────────────────────────
	var qualifierGrade generated.NullQualifier
	if data.QualifierGrade != nil {
		switch *data.QualifierGrade {
		case "FB", "B", "S", "I":
			qualifierGrade = generated.NullQualifier{
				Qualifier: generated.Qualifier(*data.QualifierGrade),
				Valid:     true,
			}
		default:
			return errorResult(m.ClientID, "validation", "qualifier_grade must be one of: FB, B, S, I", false)
		}
	}

	// ── Parse grade date ──────────────────────────────────────────────────────
	gradeDate, err := time.Parse("2006-01-02", data.GradeDate)
	if err != nil {
		return errorResult(m.ClientID, "validation", "grade_date must be in YYYY-MM-DD format", false)
	}

	// ── Fetch the existing grade to check ownership ───────────────────────────
	// We need to verify the requesting teacher was the one who created the grade.
	// Admins bypass this check (same as UpdateGrade handler).
	role, err := auth.GetUserRole(r.Context())
	if err != nil {
		return errorResult(m.ClientID, "transient", "failed to determine user role", true)
	}

	existing, err := queries.GetGradeByID(r.Context(), gradeID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return errorResult(m.ClientID, "not_found", "grade not found: "+gradeID.String(), false)
		}
		h.logger.Error("sync_push: failed to fetch grade for update",
			"error", err, "grade_id", gradeID)
		return errorResult(m.ClientID, "transient", "database error fetching grade", true)
	}

	// ── Authorization check ───────────────────────────────────────────────────
	if role == "teacher" && existing.TeacherID != userID {
		return errorResult(m.ClientID, "forbidden", "only the teacher who created this grade can update it", false)
	}

	// ── Apply the update ──────────────────────────────────────────────────────
	updated, err := queries.UpdateGrade(r.Context(), generated.UpdateGradeParams{
		ID:             gradeID,
		NumericGrade:   data.NumericGrade,
		QualifierGrade: qualifierGrade,
		GradeDate:      pgtype.Date{Time: gradeDate, Valid: true},
		Description:    data.Description,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return errorResult(m.ClientID, "not_found", "grade not found or already deleted", false)
		}
		h.logger.Error("sync_push: failed to update grade",
			"error", err, "grade_id", gradeID)
		return errorResult(m.ClientID, "transient", "failed to update grade in database", true)
	}

	serverID := updated.ID
	return syncMutationResult{
		ClientID: m.ClientID,
		Status:   "synced",
		ServerID: &serverID,
	}
}

// ──────────────────────────────────────────────────────────────────────────────
// applyGradeDelete — soft-delete a grade from offline queue
// ──────────────────────────────────────────────────────────────────────────────

// applyGradeDelete handles a type="grade", action="delete" mutation.
//
// Performs a soft-delete (sets deleted_at) on the specified grade. Student
// data is never hard-deleted per Romanian education law requirements.
//
// Authorization: only the teacher who created the grade (or an admin) can
// delete it, mirroring the regular DeleteGrade handler behaviour.
//
// Idempotency: if the grade is already soft-deleted the DB query will not find
// it (GetGradeByID only returns non-deleted rows), and we return an error.
// The client should treat a repeated delete attempt as already done and drop
// the mutation from its queue.
func (h *Handler) applyGradeDelete(
	r *http.Request,
	queries *generated.Queries,
	userID uuid.UUID,
	m *syncMutation,
) syncMutationResult {
	// ── Parse the grade data payload ──────────────────────────────────────────
	var data gradeMutationData
	if err := json.Unmarshal(m.Data, &data); err != nil {
		return errorResult(m.ClientID, "validation", "invalid grade data payload: "+err.Error(), false)
	}

	// ── Require server_id for delete ──────────────────────────────────────────
	if data.ServerID == nil {
		return errorResult(m.ClientID, "validation", "server_id is required for grade delete", false)
	}
	gradeID := *data.ServerID

	// ── Determine role for authorization ─────────────────────────────────────
	role, err := auth.GetUserRole(r.Context())
	if err != nil {
		return errorResult(m.ClientID, "transient", "failed to determine user role", true)
	}

	// ── Fetch the existing grade to check ownership ───────────────────────────
	existing, err := queries.GetGradeByID(r.Context(), gradeID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			// Grade not found — it may already be deleted. Treat as success so
			// the client can clear this item from its queue without retrying.
			serverID := gradeID
			return syncMutationResult{
				ClientID: m.ClientID,
				Status:   "synced",
				ServerID: &serverID,
			}
		}
		h.logger.Error("sync_push: failed to fetch grade for delete",
			"error", err, "grade_id", gradeID)
		return errorResult(m.ClientID, "transient", "database error fetching grade", true)
	}

	// ── Authorization check ───────────────────────────────────────────────────
	if role == "teacher" && existing.TeacherID != userID {
		return errorResult(m.ClientID, "forbidden", "only the teacher who created this grade can delete it", false)
	}

	// ── Soft-delete the grade ─────────────────────────────────────────────────
	if err := queries.SoftDeleteGrade(r.Context(), gradeID); err != nil {
		h.logger.Error("sync_push: failed to soft-delete grade",
			"error", err, "grade_id", gradeID)
		return errorResult(m.ClientID, "transient", "failed to delete grade from database", true)
	}

	// Return success. server_id is set so the client can identify the record.
	serverID := gradeID
	return syncMutationResult{
		ClientID: m.ClientID,
		Status:   "synced",
		ServerID: &serverID,
	}
}

// ──────────────────────────────────────────────────────────────────────────────
// Helpers
// ──────────────────────────────────────────────────────────────────────────────

// errorResult builds a syncMutationResult with status="error".
// code classifies the error for the client's retry logic.
// retryable tells the client whether to retry this mutation.
func errorResult(clientID uuid.UUID, code, message string, retryable bool) syncMutationResult {
	slog.Warn("sync_push: mutation error",
		"client_id", clientID,
		"error_code", code,
		"error", message,
		"retryable", retryable,
	)
	return syncMutationResult{
		ClientID:     clientID,
		Status:       "error",
		ErrorCode:    &code,
		ErrorMessage: &message,
		Retryable:    retryable,
	}
}

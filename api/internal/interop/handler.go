// Package interop implements HTTP handlers for SIIIR import/export and
// interoperability features in the CatalogRO API.
//
// Endpoints covered:
//
//	POST /interop/import                    — upload SIIIR CSV, preview parsed data
//	POST /interop/import/{importId}/confirm — confirm and persist previewed import
//	GET  /interop/import/{importId}/status  — check import status
//	POST /interop/export/siiir              — export students as SIIIR-compatible CSV
//
// IMPORTANT DOMAIN CONTEXT:
//   - SIIIR = Sistemul Informatic Integrat al Învățământului din România
//     (Romania's integrated education information system)
//   - Schools must periodically import/export student and teacher data
//     to/from SIIIR for ISJ (county inspectorate) reporting.
//   - The CSV format varies between years; the parser auto-detects the version.
//
// Import workflow:
//  1. Secretary uploads CSV → handler parses it and returns a preview with
//     importId and list of mapped users.
//  2. Secretary reviews the preview and confirms → handler persists users
//     via ProvisionUser + source mappings.
//  3. Status can be checked at any time via the status endpoint.
//
// Authorization:
//   - Only admin and secretary can import/export data.
package interop

import (
	"context"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/riverqueue/river"
	"github.com/riverqueue/river/rivertype"

	"github.com/vlahsh/catalogro/api/db/generated"
	"github.com/vlahsh/catalogro/api/internal/auth"
	"github.com/vlahsh/catalogro/api/internal/httputil"
	"github.com/vlahsh/catalogro/api/internal/interop/siiir"
)

// Handler holds the dependencies needed by interop HTTP handlers.
type Handler struct {
	queries      *generated.Queries
	logger       *slog.Logger
	riverClient  RiverInserter
	sessionStore *SessionStore
}

// RiverInserter is the interface for inserting River jobs. Allows mocking in tests.
type RiverInserter interface {
	Insert(ctx context.Context, args river.JobArgs, opts *river.InsertOpts) (*rivertype.JobInsertResult, error)
}


// NewHandler creates a new interop Handler.
// riverClient can be nil (async import falls back to synchronous).
// sessionStore manages import sessions in Redis.
func NewHandler(queries *generated.Queries, logger *slog.Logger, riverClient RiverInserter, sessionStore *SessionStore) *Handler {
	return &Handler{
		queries:      queries,
		logger:       logger,
		riverClient:  riverClient,
		sessionStore: sessionStore,
	}
}

// ──────────────────────────────────────────────────────────────────────────────
// POST /interop/import — upload CSV, parse, return preview
// ──────────────────────────────────────────────────────────────────────────────

// Import handles POST /interop/import.
//
// Accepts a multipart/form-data upload with a "file" field containing a
// SIIIR CSV export. The handler:
//  1. Auto-detects the CSV format (encoding, delimiter, version).
//  2. Parses all student rows.
//  3. Maps them to internal CatalogRO user format.
//  4. Returns a preview with an import_id for confirmation.
//
// The file size is limited to 10 MB to prevent abuse.
func (h *Handler) Import(w http.ResponseWriter, r *http.Request) {
	role, err := auth.GetUserRole(r.Context())
	if err != nil {
		httputil.Unauthorized(w, "Authentication required")
		return
	}

	// Only admin and secretary can import data.
	if role != "admin" && role != "secretary" {
		httputil.Forbidden(w, "Only admins and secretaries can import data")
		return
	}

	// Limit upload size to 10 MB.
	const maxUploadSize = 10 << 20
	r.Body = http.MaxBytesReader(w, r.Body, maxUploadSize)

	// Parse the multipart form.
	if err := r.ParseMultipartForm(maxUploadSize); err != nil {
		httputil.BadRequest(w, "FILE_TOO_LARGE", "File size must be under 10 MB")
		return
	}

	file, _, err := r.FormFile("file")
	if err != nil {
		httputil.BadRequest(w, "MISSING_FILE", "A 'file' field with the SIIIR CSV is required")
		return
	}
	defer file.Close()

	// The multipart.File already implements io.ReadSeeker, so we can use it
	// directly without buffering the entire file into memory.

	// Step 1: Detect the CSV format.
	mapping, err := siiir.DetectFormat(file)
	if err != nil {
		httputil.BadRequest(w, "UNRECOGNIZED_FORMAT",
			fmt.Sprintf("Could not detect SIIIR format: %s", err.Error()))
		return
	}

	// Reset file reader after format detection.
	if _, err := file.Seek(0, 0); err != nil {
		httputil.InternalError(w)
		return
	}

	// Step 2: Parse student rows.
	students, err := siiir.ParseStudents(file, mapping)
	if err != nil {
		httputil.BadRequest(w, "PARSE_ERROR",
			fmt.Sprintf("Failed to parse CSV: %s", err.Error()))
		return
	}

	if len(students) == 0 {
		httputil.BadRequest(w, "NO_DATA", "The CSV file contains no valid student records")
		return
	}

	// Step 3: Map parsed students to internal format.
	mapper := siiir.NewMapper()
	var users []siiir.MappedUser
	var errors []string

	for i := range students {
		mapped, err := mapper.MapStudent(&students[i])
		if err != nil {
			errors = append(errors, fmt.Sprintf("Row %d: %s", i+1, err.Error()))
			continue
		}
		users = append(users, *mapped)
	}

	// Step 4: Create an import session with the preview.
	session := &ImportSession{
		ID:        uuid.New(),
		Status:    "preview",
		CreatedAt: time.Now(),
		Users:     users,
		Errors:    errors,
	}

	if err := h.sessionStore.Save(r.Context(), session); err != nil {
		h.logger.Error("failed to save import session", "error", err)
		httputil.InternalError(w)
		return
	}

	// Step 5: Return the preview (capped at 50 rows to keep response manageable).
	const maxPreview = 50
	preview := users
	if len(preview) > maxPreview {
		preview = preview[:maxPreview]
	}

	httputil.Success(w, map[string]any{
		"import_id":      session.ID,
		"format_version": mapping.Version,
		"total_parsed":   len(students),
		"valid_users":    len(users),
		"parse_errors":   len(errors),
		"errors":         errors,
		"preview":        preview,
	})
}

// ──────────────────────────────────────────────────────────────────────────────
// POST /interop/import/{importId}/confirm — persist previewed import
// ──────────────────────────────────────────────────────────────────────────────

// ConfirmImport handles POST /interop/import/{importId}/confirm.
//
// Persists the previewed import data: provisions user accounts and creates
// source mappings for each imported user.
func (h *Handler) ConfirmImport(w http.ResponseWriter, r *http.Request) {
	role, err := auth.GetUserRole(r.Context())
	if err != nil {
		httputil.Unauthorized(w, "Authentication required")
		return
	}

	if role != "admin" && role != "secretary" {
		httputil.Forbidden(w, "Only admins and secretaries can confirm imports")
		return
	}

	queries := auth.GetQueries(r.Context())
	if queries == nil {
		httputil.InternalError(w)
		return
	}

	importID, err := uuid.Parse(chi.URLParam(r, "importId"))
	if err != nil {
		httputil.BadRequest(w, "INVALID_ID", "importId must be a valid UUID")
		return
	}

	// Look up the import session from Redis.
	session, err := h.sessionStore.Get(r.Context(), importID)
	if err != nil {
		h.logger.Error("failed to get import session", "error", err)
		httputil.InternalError(w)
		return
	}
	if session == nil {
		httputil.NotFound(w, "Import session not found — it may have expired")
		return
	}
	if session.Status != "preview" {
		httputil.BadRequest(w, "INVALID_STATUS",
			fmt.Sprintf("Import is in '%s' state, expected 'preview'", session.Status))
		return
	}
	session.Status = "confirmed"
	if err := h.sessionStore.Save(r.Context(), session); err != nil {
		h.logger.Error("failed to update session status", "error", err)
		httputil.InternalError(w)
		return
	}

	// Get the current user ID for provisioned_by tracking.
	currentUserID, _ := auth.GetUserID(r.Context())

	// For large batches (200+ users), enqueue a River job for async processing.
	// This prevents HTTP request timeouts on big CSV imports.
	const asyncThreshold = 200
	if len(session.Users) >= asyncThreshold && h.riverClient != nil {
		_, err := h.riverClient.Insert(r.Context(), bulkImportArgs{
			ImportID:      importID,
			Users:         session.Users,
			ProvisionedBy: currentUserID,
		}, nil)
		if err != nil {
			h.logger.Error("failed to enqueue bulk import job", "error", err)
			httputil.InternalError(w)
			return
		}

		session.Status = "processing"
		_ = h.sessionStore.Save(r.Context(), session)

		httputil.Success(w, map[string]any{
			"import_id": importID,
			"status":    "processing",
			"message":   "Large import queued for background processing. Poll /status for progress.",
			"total":     len(session.Users),
		})
		return
	}

	// Persist each mapped user synchronously (small batch).
	imported := 0
	skipped := 0
	var importErrors []string

	for i := range session.Users {
		u := &session.Users[i]

		syntheticEmail := fmt.Sprintf("%s@siiir.import", u.SourceMapping.SourceID)
		activationToken := uuid.New().String()
		siiirID := u.SourceMapping.SourceID

		provisionedUser, err := queries.ProvisionUser(r.Context(), generated.ProvisionUserParams{
			FirstName:       u.FirstName,
			LastName:        u.LastName,
			Role:            generated.UserRole(u.Role),
			Email:           &syntheticEmail,
			SiiirStudentID:  &siiirID,
			ActivationToken: &activationToken,
			ProvisionedBy:   pgtype.UUID{Bytes: currentUserID, Valid: true},
		})
		if err != nil {
			importErrors = append(importErrors, fmt.Sprintf("%s %s: %s", u.FirstName, u.LastName, err.Error()))
			skipped++
			continue
		}

		// Create source mapping (non-fatal).
		_, _ = queries.UpsertSourceMapping(r.Context(), generated.UpsertSourceMappingParams{
			EntityType:     u.SourceMapping.EntityType,
			EntityID:       provisionedUser.ID,
			SourceSystem:   u.SourceMapping.SourceSystem,
			SourceID:       u.SourceMapping.SourceID,
			SourceMetadata: u.SourceMapping.SourceMetadata,
		})

		imported++
	}

	// Update session status in Redis.
	session.Status = "completed"
	session.Imported = imported
	session.Skipped = skipped
	if len(importErrors) > 0 {
		session.Errors = append(session.Errors, importErrors...)
	}
	_ = h.sessionStore.Save(r.Context(), session)

	httputil.Success(w, map[string]any{
		"import_id": importID,
		"status":    "completed",
		"imported":  imported,
		"skipped":   skipped,
		"errors":    importErrors,
	})
}

// ──────────────────────────────────────────────────────────────────────────────
// GET /interop/import/{importId}/status
// ──────────────────────────────────────────────────────────────────────────────

// ImportStatus handles GET /interop/import/{importId}/status.
func (h *Handler) ImportStatus(w http.ResponseWriter, r *http.Request) {
	_, err := auth.GetUserRole(r.Context())
	if err != nil {
		httputil.Unauthorized(w, "Authentication required")
		return
	}

	importID, err := uuid.Parse(chi.URLParam(r, "importId"))
	if err != nil {
		httputil.BadRequest(w, "INVALID_ID", "importId must be a valid UUID")
		return
	}

	session, err := h.sessionStore.Get(r.Context(), importID)
	if err != nil {
		h.logger.Error("failed to get import session", "error", err)
		httputil.InternalError(w)
		return
	}
	if session == nil {
		httputil.NotFound(w, "Import session not found or expired")
		return
	}

	httputil.Success(w, map[string]any{
		"import_id":  session.ID,
		"status":     session.Status,
		"created_at": session.CreatedAt,
		"imported":   session.Imported,
		"skipped":    session.Skipped,
		"errors":     session.Errors,
	})
}

// ──────────────────────────────────────────────────────────────────────────────
// POST /interop/export/siiir — export students as SIIIR CSV
// ──────────────────────────────────────────────────────────────────────────────

// ExportSIIIR handles POST /interop/export/siiir.
//
// Exports all active students in the current school year as a SIIIR-compatible CSV.
// The CSV uses the latest known format (2025-v1: UTF-8, comma-delimited).
func (h *Handler) ExportSIIIR(w http.ResponseWriter, r *http.Request) {
	role, err := auth.GetUserRole(r.Context())
	if err != nil {
		httputil.Unauthorized(w, "Authentication required")
		return
	}

	if role != "admin" && role != "secretary" {
		httputil.Forbidden(w, "Only admins and secretaries can export data")
		return
	}

	queries := auth.GetQueries(r.Context())
	if queries == nil {
		httputil.InternalError(w)
		return
	}

	// Get the current school year.
	schoolYear, err := queries.GetCurrentSchoolYear(r.Context())
	if err != nil {
		httputil.BadRequest(w, "NO_SCHOOL_YEAR", "No current school year is configured")
		return
	}

	// Get all classes for this school year.
	classes, err := queries.DashboardClassSummaries(r.Context(), schoolYear.ID)
	if err != nil {
		h.logger.Error("failed to list classes for export", "error", err)
		httputil.InternalError(w)
		return
	}

	// For each class, get enrolled students and write CSV rows.
	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", "attachment; filename=siiir_export.csv")

	csvWriter := csv.NewWriter(w)

	// Write header row (2025-v1 format).
	if err := csvWriter.Write([]string{
		"CNP", "Nume", "Prenume", "Data nasterii", "Gen",
		"Clasa", "Forma", "Statut",
	}); err != nil {
		h.logger.Error("failed to write CSV header", "error", err)
		return
	}

	// Batch-fetch all SIIIR source mappings for users to avoid N+1 queries.
	// This fetches all user→SIIIR mappings at once, then we look up by entity_id.
	allMappings, err := queries.ListSourceMappingsBySystem(r.Context(), generated.ListSourceMappingsBySystemParams{
		SourceSystem: "siiir",
		EntityType:   "user",
	})
	if err != nil {
		h.logger.Error("failed to list source mappings for export", "error", err)
		httputil.InternalError(w)
		return
	}

	// Build a lookup map: user UUID → SIIIR source_id (CNP).
	cnpByUserID := make(map[uuid.UUID]string, len(allMappings))
	for i := range allMappings {
		cnpByUserID[allMappings[i].EntityID] = allMappings[i].SourceID
	}

	for i := range classes {
		students, err := queries.ListStudentsByClass(r.Context(), classes[i].ID)
		if err != nil {
			h.logger.Error("failed to list students for export",
				"class_id", classes[i].ID, "error", err)
			continue
		}

		for j := range students {
			row := []string{
				cnpByUserID[students[j].ID], // CNP from source mapping (empty if none)
				students[j].LastName,
				students[j].FirstName,
				"", // birth_date — not stored in users table
				"", // gender — not stored in users table
				classes[i].Name,
				"zi", // default to "zi" (daytime education)
				"înscris",
			}
			if err := csvWriter.Write(row); err != nil {
				h.logger.Error("failed to write CSV row", "error", err)
				return
			}
		}
	}

	csvWriter.Flush()
}

// ──────────────────────────────────────────────────────────────────────────────
// GET /interop/source-mappings — list source mappings
// ──────────────────────────────────────────────────────────────────────────────

// ListSourceMappings handles GET /interop/source-mappings.
// Returns all source mappings for a given source system.
func (h *Handler) ListSourceMappings(w http.ResponseWriter, r *http.Request) {
	role, err := auth.GetUserRole(r.Context())
	if err != nil {
		httputil.Unauthorized(w, "Authentication required")
		return
	}

	// Only admin and secretary can view source mappings (contain external IDs).
	if role != "admin" && role != "secretary" {
		httputil.Forbidden(w, "Only admins and secretaries can view source mappings")
		return
	}

	queries := auth.GetQueries(r.Context())
	if queries == nil {
		httputil.InternalError(w)
		return
	}

	system := r.URL.Query().Get("system")
	if system == "" {
		system = "siiir"
	}
	entityType := r.URL.Query().Get("entity_type")
	if entityType == "" {
		entityType = "user"
	}

	mappings, err := queries.ListSourceMappingsBySystem(r.Context(), generated.ListSourceMappingsBySystemParams{
		SourceSystem: system,
		EntityType:   entityType,
	})
	if err != nil {
		h.logger.Error("failed to list source mappings", "error", err)
		httputil.InternalError(w)
		return
	}

	type mappingResponse struct {
		EntityType     string          `json:"entity_type"`
		EntityID       uuid.UUID       `json:"entity_id"`
		SourceSystem   string          `json:"source_system"`
		SourceID       string          `json:"source_id"`
		SourceMetadata json.RawMessage `json:"source_metadata"`
	}

	result := make([]mappingResponse, 0, len(mappings))
	for i := range mappings {
		result = append(result, mappingResponse{
			EntityType:     mappings[i].EntityType,
			EntityID:       mappings[i].EntityID,
			SourceSystem:   mappings[i].SourceSystem,
			SourceID:       mappings[i].SourceID,
			SourceMetadata: mappings[i].SourceMetadata,
		})
	}

	httputil.Success(w, result)
}

// bulkImportArgs mirrors jobs.BulkImportArgs to avoid an import cycle
// (interop → jobs → interop/siiir → interop would cycle).
// The Kind() must match jobs.BulkImportArgs.Kind() exactly so River
// routes the job to the correct worker.
type bulkImportArgs struct {
	ImportID      uuid.UUID          `json:"import_id"`
	Users         []siiir.MappedUser `json:"users"`
	ProvisionedBy uuid.UUID          `json:"provisioned_by"`
}

func (bulkImportArgs) Kind() string                 { return "bulk_import" }
func (bulkImportArgs) InsertOpts() river.InsertOpts { return river.InsertOpts{} }

// This file implements the GET /sync/pull endpoint for CatalogRO's offline
// synchronisation system.
//
// SYNC PULL OVERVIEW
// ──────────────────
// The pull endpoint complements POST /sync/push. While push sends local
// mutations to the server, pull fetches server-side changes since the last
// sync so the client can update its local IndexedDB cache.
//
// How it works:
//  1. The client sends ?since=<ISO timestamp> (or omits it for a full sync).
//  2. The server determines which classes the user can access:
//     - Teachers: only their assigned classes.
//     - Admins/secretaries: all classes in the current school year.
//  3. The server queries grades and absences modified since the timestamp
//     for those classes, and returns them in a single response.
//  4. The client merges the received data into its local IndexedDB store.
//
// The response includes a `server_timestamp` that the client should store
// and pass as `since` on the next pull, ensuring no changes are missed.
//
// AUTHORIZATION
// ─────────────
// This endpoint lives inside the JWT + RLS middleware group. Teachers only
// receive data for their assigned classes. RLS further enforces school-level
// isolation at the database level.
package catalog

import (
	"errors"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/vlahsh/catalogro/api/db/generated"
	"github.com/vlahsh/catalogro/api/internal/auth"
	"github.com/vlahsh/catalogro/api/internal/httputil"
)

// ──────────────────────────────────────────────────────────────────────────────
// GET /sync/pull
// ──────────────────────────────────────────────────────────────────────────────

// SyncPull handles GET /sync/pull.
//
// Query parameters:
//   - since (optional): ISO 8601 timestamp. Returns changes after this time.
//     If omitted, returns all data (full sync).
//
// Returns:
//
//	{
//	  "data": {
//	    "grades": [...],
//	    "absences": [...],
//	    "server_timestamp": "2026-10-15T12:00:00Z"
//	  }
//	}
func (h *Handler) SyncPull(w http.ResponseWriter, r *http.Request) {
	// Step 1: Authenticate.
	userID, err := auth.GetUserID(r.Context())
	if err != nil {
		httputil.Unauthorized(w, "Authentication required")
		return
	}

	role, err := auth.GetUserRole(r.Context())
	if err != nil {
		httputil.Unauthorized(w, "Authentication required")
		return
	}

	queries := auth.GetQueries(r.Context())
	if queries == nil {
		httputil.InternalError(w)
		return
	}

	// Step 2: Only teachers, admins, and secretaries can pull sync data.
	if role != "admin" && role != "secretary" && role != "teacher" {
		httputil.Forbidden(w, "Only staff can pull sync data")
		return
	}

	// Step 3: Parse the "since" timestamp from query parameters.
	// If omitted, use Unix epoch (1970-01-01) to get all data.
	sinceStr := r.URL.Query().Get("since")
	since := time.Date(1970, 1, 1, 0, 0, 0, 0, time.UTC)
	if sinceStr != "" {
		parsed, err := time.Parse(time.RFC3339, sinceStr)
		if err != nil {
			httputil.BadRequest(w, "INVALID_TIMESTAMP",
				"'since' must be an ISO 8601 / RFC 3339 timestamp (e.g. 2026-10-15T12:00:00Z)")
			return
		}
		since = parsed
	}

	// Step 4: Capture the server timestamp BEFORE running any queries.
	// This is the watermark the client will use as "since" on the next pull.
	// Capturing it before the reads ensures that any row updated between the
	// queries and the response is NOT skipped on the next pull.
	serverTimestamp := time.Now().UTC()

	// Step 5: Get the current school year.
	schoolYear, err := queries.GetCurrentSchoolYear(r.Context())
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			httputil.BadRequest(w, "NO_SCHOOL_YEAR", "No current school year is configured")
			return
		}
		h.logger.Error("failed to get current school year", "error", err)
		httputil.InternalError(w)
		return
	}

	// Step 6: Determine which class IDs this user can access.
	var classIDs []uuid.UUID

	if role == "teacher" {
		// Teachers only see classes they are assigned to.
		teacherClasses, err := queries.ListClassesByTeacher(r.Context(), generated.ListClassesByTeacherParams{
			TeacherID:    userID,
			SchoolYearID: schoolYear.ID,
		})
		if err != nil {
			h.logger.Error("failed to list teacher classes", "error", err, "user_id", userID)
			httputil.InternalError(w)
			return
		}
		classIDs = make([]uuid.UUID, len(teacherClasses))
		for i := range teacherClasses {
			classIDs[i] = teacherClasses[i].ID
		}
	} else {
		// Admins/secretaries see all classes in the school year.
		allClasses, err := queries.ListClassesBySchoolYear(r.Context(), schoolYear.ID)
		if err != nil {
			h.logger.Error("failed to list all classes", "error", err)
			httputil.InternalError(w)
			return
		}
		classIDs = make([]uuid.UUID, len(allClasses))
		for i := range allClasses {
			classIDs[i] = allClasses[i].ID
		}
	}

	if len(classIDs) == 0 {
		// No classes — return empty results.
		httputil.Success(w, map[string]any{
			"grades":           []any{},
			"absences":         []any{},
			"server_timestamp": serverTimestamp.Format(time.RFC3339),
		})
		return
	}

	// Step 7: Fetch grades modified since the given timestamp.
	grades, err := queries.ListGradesModifiedSince(r.Context(), generated.ListGradesModifiedSinceParams{
		UpdatedAt: since,
		Column2:   classIDs,
	})
	if err != nil {
		h.logger.Error("failed to list grades for sync pull", "error", err)
		httputil.InternalError(w)
		return
	}

	// Step 8: Fetch absences modified since the given timestamp.
	absences, err := queries.ListAbsencesModifiedSince(r.Context(), generated.ListAbsencesModifiedSinceParams{
		UpdatedAt: since,
		Column2:   classIDs,
	})
	if err != nil {
		h.logger.Error("failed to list absences for sync pull", "error", err)
		httputil.InternalError(w)
		return
	}

	// Step 9: Return the results with the pre-captured server timestamp.
	// The client should store server_timestamp and use it as "since" next time.
	httputil.Success(w, map[string]any{
		"grades":           grades,
		"absences":         absences,
		"server_timestamp": serverTimestamp.Format(time.RFC3339),
	})
}

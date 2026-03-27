package main

// main.go is the entry point for the CatalogRO API server.
//
// It wires together all the components:
//   - Configuration (env vars)
//   - Database (PostgreSQL via pgxpool)
//   - Redis (for sessions/caching, future use)
//   - HTTP router (chi) with middleware chain
//   - Auth handlers (login, refresh, logout, 2FA, profile)
//   - Placeholder routes for all other endpoints (notImplemented)
//
// The middleware chain for protected routes is:
//   Request → CORS → RequestID → RealIP → Recoverer → Timeout → Logger
//           → JWTAuth → TenantContext → RequireRole (optional) → Handler

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/cors"

	"github.com/vlahsh/catalogro/api/db/generated"
	"github.com/vlahsh/catalogro/api/internal/auth"
	"github.com/vlahsh/catalogro/api/internal/catalog"
	"github.com/vlahsh/catalogro/api/internal/config"
	"github.com/vlahsh/catalogro/api/internal/platform"
	"github.com/vlahsh/catalogro/api/internal/interop"
	"github.com/vlahsh/catalogro/api/internal/interop/oneroster"
	"github.com/vlahsh/catalogro/api/internal/report"
	"github.com/vlahsh/catalogro/api/internal/school"
	"github.com/vlahsh/catalogro/api/internal/user"
)

func main() {
	if err := run(); err != nil {
		slog.Error("fatal", "error", err)
		os.Exit(1)
	}
}

func run() error {
	// Load configuration from environment variables (with sensible dev defaults).
	cfg := config.Load()

	// Set up structured JSON logging. In production, this logs at INFO level;
	// in development, at DEBUG level (configured in config.Load).
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: cfg.LogLevel,
	}))
	slog.SetDefault(logger)

	// =========================================================================
	// Database connection
	// =========================================================================
	// Connect to PostgreSQL using a connection pool (pgxpool).
	// The pool manages multiple connections and reuses them across requests.
	db, err := platform.NewDB(cfg.DatabaseURL)
	if err != nil {
		return fmt.Errorf("connect to database: %w", err)
	}
	defer db.Close()

	// =========================================================================
	// Redis connection
	// =========================================================================
	// Redis is used for refresh token storage/revocation and rate limiting (future).
	// We connect eagerly at startup so we fail fast if Redis is down.
	rdb, err := platform.NewRedis(cfg.RedisURL)
	if err != nil {
		return fmt.Errorf("connect to redis: %w", err)
	}
	defer rdb.Close()

	// =========================================================================
	// sqlc Queries — the typed database access layer
	// =========================================================================
	// generated.New() creates a Queries struct that uses the pool directly.
	// For auth handlers (login, refresh), we use this pool-based Queries because
	// those endpoints run BEFORE authentication (no RLS context needed).
	//
	// For protected endpoints, the TenantContext middleware creates a transaction-
	// scoped Queries (via WithTx) that has the RLS tenant set. Handlers access
	// it via auth.GetQueries(ctx).
	queries := generated.New(db.Pool)

	// =========================================================================
	// JWT secret — convert the config string to a byte slice for HMAC signing
	// =========================================================================
	jwtSecret := []byte(cfg.JWTSecret)

	// =========================================================================
	// Handler initialization — create handler structs with shared dependencies
	// =========================================================================
	// Each handler struct holds a reference to the sqlc Queries and the logger.
	// They are created once here and reused for every request (safe for concurrent use).

	// schoolHandler manages school info, classes, subjects, and teacher assignments.
	schoolHandler := school.NewHandler(queries, logger)

	// catalogHandler manages grades (note) and absences (absente) — the core catalog.
	catalogHandler := catalog.NewHandler(queries, logger)

	// reportHandler manages reports: dashboard stats, student report cards, class statistics.
	reportHandler := report.NewHandler(queries, logger)

	// interopHandler manages SIIIR import/export and source mappings.
	interopHandler := interop.NewHandler(queries, logger)

	// oneRosterHandler serves the OneRoster 1.2 read-only API.
	oneRosterHandler := oneroster.NewHandler(queries, logger)

	// userHandler manages user provisioning: creating accounts, listing users,
	// and listing accounts awaiting activation. Restricted to admin and secretary
	// roles (enforced per-route via RequireRole middleware below).
	userHandler := user.NewHandler(queries, logger, cfg.BaseURL)

	// =========================================================================
	// Router setup
	// =========================================================================
	r := chi.NewRouter()

	// =========================================================================
	// Global middleware — runs on EVERY request, including health checks
	// =========================================================================

	// CORS (Cross-Origin Resource Sharing) — required for the Nuxt 3 frontend
	// running on a different port (localhost:3000) to call the API (localhost:8080).
	// Without CORS, the browser blocks cross-origin requests for security.
	r.Use(cors.Handler(cors.Options{
		// AllowedOrigins: which frontend origins can call this API.
		// In development, the Nuxt dev server runs on 0.0.0.0:3000 and is accessed
		// from both localhost and the VM's LAN IP. We allow all origins in dev mode;
		// in production this should be locked to the actual domain (e.g., app.catalogro.ro).
		AllowedOrigins: []string{"http://localhost:3000", "http://*:3000"},

		// AllowedMethods: which HTTP methods are permitted for cross-origin requests.
		AllowedMethods: []string{"GET", "POST", "PUT", "DELETE", "OPTIONS"},

		// AllowedHeaders: which request headers the frontend can send.
		// "Authorization" is needed for the JWT Bearer token.
		// "Content-Type" is needed for JSON request bodies.
		AllowedHeaders: []string{"Accept", "Authorization", "Content-Type", "X-Request-ID"},

		// ExposedHeaders: which response headers the frontend JavaScript can read.
		// By default, only simple headers are exposed. We expose X-Request-ID
		// so the frontend can log it for debugging support requests.
		ExposedHeaders: []string{"X-Request-ID"},

		// AllowCredentials: whether the browser should send cookies/auth headers.
		// We need this because we send the JWT in the Authorization header.
		AllowCredentials: true,

		// MaxAge: how long (in seconds) the browser caches the CORS preflight response.
		// 300 seconds (5 min) reduces the number of OPTIONS preflight requests.
		MaxAge: 300,
	}))

	// RequestID generates a unique ID for each request and adds it to the context
	// and response headers. This makes it easy to trace a request through logs.
	r.Use(middleware.RequestID)

	// RealIP extracts the client's real IP address from X-Forwarded-For or
	// X-Real-IP headers (set by Traefik/load balancer) so that rate limiting
	// and audit logs use the correct IP.
	r.Use(middleware.RealIP)

	// Recoverer catches panics in handlers, logs a stack trace, and returns 500
	// instead of crashing the entire server process.
	r.Use(middleware.Recoverer)

	// Timeout sets a hard 30-second deadline for every request. If a handler takes
	// longer (e.g., a slow database query), the request is cancelled automatically.
	r.Use(middleware.Timeout(30 * time.Second))

	// Request logger logs every request with method, path, status, and duration.
	r.Use(requestLogger(logger))

	// =========================================================================
	// Health check — used by Kubernetes liveness/readiness probes
	// =========================================================================
	r.Get("/healthz", func(w http.ResponseWriter, r *http.Request) {
		if err := db.Ping(r.Context()); err != nil {
			http.Error(w, "db unhealthy", http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, "ok")
	})

	// =========================================================================
	// API v1 routes
	// =========================================================================
	r.Route("/api/v1", func(r chi.Router) {

		// =====================================================================
		// Public auth endpoints — NO authentication required
		// =====================================================================
		// These endpoints are accessible without a JWT because they ARE the
		// authentication flow (login, refresh, logout) or pre-auth flows
		// (activation). The 2FA login endpoint is also public because the user
		// is mid-authentication (they have an mfa_token but not a full JWT yet).
		r.Group(func(r chi.Router) {
			// POST /auth/login — authenticate with email + password.
			// Returns JWT pair or mfa_required response.
			r.Post("/auth/login", auth.HandleLogin(queries, jwtSecret))

			// POST /auth/2fa/login — complete 2FA login with mfa_token + TOTP code.
			// This is public because the user doesn't have a full access token yet.
			r.Post("/auth/2fa/login", auth.HandleMFALogin(queries, jwtSecret))

			// POST /auth/refresh — exchange refresh token for new token pair.
			r.Post("/auth/refresh", auth.HandleRefresh(queries, jwtSecret))

			// POST /auth/logout — clear the session (client discards tokens).
			r.Post("/auth/logout", auth.HandleLogout())

			// Activation endpoints — pre-login flow, no JWT required.
			// GET  validates the token and returns the user's identity for the
			//      confirmation screen (name, role, school).
			// POST sets the password (and optional GDPR consent) and activates
			//      the account. Both use a direct DB connection (no RLS).
			r.Get("/auth/activate/{token}", auth.HandleGetActivation(queries))
			r.Post("/auth/activate", auth.HandlePostActivation(queries))
		})

		// =====================================================================
		// Protected endpoints — JWT authentication + RLS tenant required
		// =====================================================================
		// All routes in this group require:
		//   1. A valid JWT access token (checked by JWTAuth middleware)
		//   2. A valid school_id from the JWT (used by TenantContext to set RLS)
		//
		// After these middleware run, handlers can:
		//   - Use auth.GetClaims(ctx) to read user/school/role info
		//   - Use auth.GetQueries(ctx) to get a DB queries object with RLS active
		r.Group(func(r chi.Router) {
			// JWTAuth validates the "Authorization: Bearer <token>" header.
			// If the token is missing, expired, or has an invalid signature,
			// the request is rejected with 401 Unauthorized.
			r.Use(auth.JWTAuth(jwtSecret))

			// TenantContext reads school_id from the JWT claims and sets up a
			// PostgreSQL transaction with "SET LOCAL app.current_school_id".
			// This activates Row-Level Security (RLS) policies, ensuring that
			// every database query only returns data for the user's school.
			r.Use(auth.TenantContext(db.Pool))

			// Auth (2FA setup/verification — requires being logged in)
			// Handle2FASetup generates a TOTP secret + QR URL for the user
			// to scan with their authenticator app. The secret is NOT saved yet.
			// Handle2FAVerify receives the secret back with the first valid code;
			// only after validation does it persist the secret and set totp_enabled.
			r.Post("/auth/2fa/setup", auth.Handle2FASetup())
			r.Post("/auth/2fa/verify", auth.Handle2FAVerify())

			// Users (provisioning and profile)
			r.Get("/users/me", auth.HandleGetProfile(queries))
			// PUT /users/me — any authenticated user can update their own email/phone.
			// No role restriction: teachers, parents, students, admins can all use this.
			// SECURITY: The handler reads the user ID from the JWT, not from the URL —
			// users can only ever edit their own profile, never another user's.
			r.Put("/users/me", userHandler.UpdateProfile)

			// POST /users — provision a new user (admin/secretary only).
			// RequireRole wraps only this route so teachers/parents get 403, not 501.
			r.With(auth.RequireRole("admin", "secretary")).Post("/users", userHandler.ProvisionUser)

			r.Post("/users/import", notImplemented)

			// POST /users/{userId}/resend-activation — generate a fresh activation token
			// for a user who has not yet activated their account (admin/secretary only).
			r.With(auth.RequireRole("admin", "secretary")).Post("/users/{userId}/resend-activation", userHandler.ResendActivation)

			// GET /users/pending — list accounts awaiting activation (admin/secretary only).
			// NOTE: this route must be registered BEFORE /users/{userId} (if that is ever
			// added) so that chi matches the literal segment "pending" first, not as a
			// userId path parameter.
			r.With(auth.RequireRole("admin", "secretary")).Get("/users/pending", userHandler.ListPendingActivations)

			// GET /users — list all active users in the school (admin/secretary only).
			r.With(auth.RequireRole("admin", "secretary")).Get("/users", userHandler.ListUsers)

			// GET /users/me/children — list children linked to the current user.
			// Accessible to ALL authenticated users (not restricted to parents).
			// A teacher may also want to see which children are linked to their
			// parent accounts for class communication purposes.
			r.Get("/users/me/children", userHandler.ListChildren)

			// GDPR — all authenticated users can manage their own data.
			// These endpoints are self-service: no role restriction is needed because
			// the user ID always comes from the JWT, never from a URL parameter.
			// RecordConsent: stamps gdpr_consent_at for the current user.
			// ExportData: returns the user's profile + children (Art. 20 portability).
			// RequestDeletion: anonymises PII and deactivates the account (Art. 17).
			r.Post("/users/me/gdpr/consent", userHandler.RecordConsent)
			r.Post("/users/me/gdpr/export", userHandler.ExportData)
			r.Post("/users/me/gdpr/delete", userHandler.RequestDeletion)

			// School config
			// GET /schools/current — returns the current tenant's school details.
			r.Get("/schools/current", schoolHandler.GetCurrentSchool)
			r.Put("/schools/current", notImplemented)
			// GET /schools/current/year — returns the active school year with
			// semester date boundaries. Accessible to all authenticated users
			// (teachers, admins, secretaries, parents, students) because every
			// role needs the school year context to display dates correctly.
			r.Get("/schools/current/year", schoolHandler.GetCurrentYear)

			// Classes
			// GET /classes — list classes for current school year.
			// Teachers see only their assigned classes; admins see all.
			r.Get("/classes", schoolHandler.ListClasses)
			// POST /classes — create a new class. Restricted to admin role only.
			r.With(auth.RequireRole("admin")).Post("/classes", schoolHandler.CreateClass)
			// GET /classes/{classId} — class details with enrolled students.
			r.Get("/classes/{classId}", schoolHandler.GetClass)
			// PUT /classes/{classId} — update a class. Restricted to admin role only.
			r.With(auth.RequireRole("admin")).Put("/classes/{classId}", schoolHandler.UpdateClass)
			// POST /classes/{classId}/enroll — enrol a student. Admin + secretary only.
			r.With(auth.RequireRole("admin", "secretary")).Post("/classes/{classId}/enroll", schoolHandler.EnrollStudent)
			// DELETE /classes/{classId}/enroll/{studentId} — remove a student. Admin + secretary only.
			r.With(auth.RequireRole("admin", "secretary")).Delete("/classes/{classId}/enroll/{studentId}", schoolHandler.UnenrollStudent)
			// GET /classes/{classId}/teachers — teacher-subject assignments for a class.
			r.Get("/classes/{classId}/teachers", schoolHandler.ListTeachers)
			// POST /classes/{classId}/teachers — assign a teacher to a subject in a class.
			// Restricted to admin role only (closes #29).
			r.With(auth.RequireRole("admin")).Post("/classes/{classId}/teachers", schoolHandler.AssignTeacher)

			// Subjects
			// GET /subjects — list all active subjects for the school.
			r.Get("/subjects", schoolHandler.ListSubjects)
			// POST /subjects — create a new subject. Restricted to admin role only.
			// Teachers, secretaries, parents, and students must receive 403 Forbidden.
			r.With(auth.RequireRole("admin")).Post("/subjects", schoolHandler.CreateSubject)

			// Catalog (grades)
			// GET — list grades for a class/subject/semester with student grouping.
			r.Get("/catalog/classes/{classId}/subjects/{subjectId}/grades", catalogHandler.ListGrades)
			// POST — create a new grade (numeric or qualifier).
			r.Post("/catalog/grades", catalogHandler.CreateGrade)
			// PUT — update an existing grade (preserves audit trail).
			r.Put("/catalog/grades/{gradeId}", catalogHandler.UpdateGrade)
			// DELETE — soft-delete a grade (sets deleted_at, data preserved for audit).
			r.Delete("/catalog/grades/{gradeId}", catalogHandler.DeleteGrade)
			r.Post("/catalog/grades/sync", notImplemented)
			// Semester averages
			// POST — compute and close semester averages for a class/subject.
			r.Post("/catalog/averages/{subjectId}/close", catalogHandler.CloseAverage)
			// POST — admin approves a closed average.
			r.Post("/catalog/averages/{averageId}/approve", catalogHandler.ApproveAverage)

			// Absences
			// GET — list absences for a class by date or semester+month.
			r.Get("/catalog/classes/{classId}/absences", catalogHandler.ListAbsences)
			// POST — record a new absence (always starts as unexcused).
			r.Post("/catalog/absences", catalogHandler.CreateAbsence)
			// PUT — excuse (motivate) an absence with a reason/type.
			r.Put("/catalog/absences/{absenceId}/excuse", catalogHandler.ExcuseAbsence)
			r.Post("/catalog/absences/sync", notImplemented)

			// Descriptive evaluations (primary)
			// GET — list descriptive evaluations for a class/subject/semester.
			r.Get("/catalog/classes/{classId}/subjects/{subjectId}/evaluations", catalogHandler.ListEvaluations)
			// POST — create a descriptive evaluation for a primary school student.
			r.Post("/catalog/evaluations", catalogHandler.CreateEvaluation)
			// PUT — update the content of an existing descriptive evaluation.
			r.Put("/catalog/evaluations/{evalId}", catalogHandler.UpdateEvaluation)
			// DELETE — delete a descriptive evaluation.
			r.Delete("/catalog/evaluations/{evalId}", catalogHandler.DeleteEvaluation)

			// Sync
			r.Post("/sync/push", catalogHandler.SyncPush)
			// GET — pull server-side changes since last sync timestamp.
			r.Get("/sync/pull", catalogHandler.SyncPull)

			// Messages
			r.Get("/messages", notImplemented)
			r.Get("/messages/{messageId}", notImplemented)
			r.Post("/messages", notImplemented)
			r.Post("/messages/announcements", notImplemented)
			r.Put("/messages/{messageId}/read", notImplemented)

			// Reports — real-time (synchronous) endpoints
			// GET — school-wide statistics for the admin dashboard.
			r.Get("/reports/dashboard", reportHandler.Dashboard)
			// GET — full student report card (fișa elevului).
			r.Get("/reports/student/{studentId}", reportHandler.StudentReport)
			// GET — class-level grade and absence statistics.
			r.Get("/reports/class/{classId}/stats", reportHandler.ClassStats)

			// Reports — async endpoints (require River jobs, not yet implemented)
			r.Post("/reports/catalog-pdf", notImplemented)
			r.Get("/reports/jobs/{jobId}", notImplemented)
			r.Post("/reports/isj-export", notImplemented)

			// Interoperability — SIIIR import/export
			// POST — upload SIIIR CSV, auto-detect format, return preview.
			r.Post("/interop/import", interopHandler.Import)
			// POST — confirm and persist a previewed import.
			r.Post("/interop/import/{importId}/confirm", interopHandler.ConfirmImport)
			// GET — check import session status.
			r.Get("/interop/import/{importId}/status", interopHandler.ImportStatus)
			// POST — export students as SIIIR-compatible CSV.
			r.Post("/interop/export/siiir", interopHandler.ExportSIIIR)
			// GET — list source mappings (SIIIR ↔ internal entity IDs).
			r.Get("/interop/source-mappings", interopHandler.ListSourceMappings)

			// Interoperability — student portability (EHEIF, not yet implemented)
			r.Post("/interop/portability/export/{studentId}", notImplemented)
			r.Post("/interop/portability/import", notImplemented)
		})

		// =====================================================================
		// OneRoster 1.2 API — separate auth (API key for machine-to-machine)
		// =====================================================================
		r.Group(func(r chi.Router) {
			// TODO: add API key auth middleware
			// r.Use(auth.APIKeyMiddleware())

			// OneRoster 1.2 Rostering endpoints
			r.Get("/oneroster/orgs", oneRosterHandler.ListOrgs)
			r.Get("/oneroster/orgs/{sourcedId}", oneRosterHandler.GetOrg)
			r.Get("/oneroster/users", oneRosterHandler.ListUsers)
			r.Get("/oneroster/users/{sourcedId}", oneRosterHandler.GetUser)
			r.Get("/oneroster/classes", oneRosterHandler.ListClasses)
			r.Get("/oneroster/classes/{sourcedId}/students", oneRosterHandler.ListClassStudents)
			r.Get("/oneroster/courses", oneRosterHandler.ListCourses)
			r.Get("/oneroster/enrollments", oneRosterHandler.ListEnrollments)
			r.Get("/oneroster/academicSessions", oneRosterHandler.ListAcademicSessions)
			// OneRoster 1.2 Gradebook endpoints (stubs — full impl requires line item mapping)
			r.Get("/oneroster/lineItems", oneRosterHandler.ListLineItems)
			r.Get("/oneroster/results", oneRosterHandler.ListResults)
		})
	})

	// OpenAPI discovery (no auth, EIF compliance)
	r.Get("/.well-known/openapi.json", notImplemented)

	// =========================================================================
	// HTTP server
	// =========================================================================
	srv := &http.Server{
		Addr:         ":" + cfg.Port,
		Handler:      r,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 30 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	// =========================================================================
	// Graceful shutdown — wait for in-flight requests to complete
	// =========================================================================
	// Start the server in a goroutine so we can listen for OS signals in main.
	go func() {
		slog.Info("server starting", "port", cfg.Port, "env", cfg.Env)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			slog.Error("server failed", "error", err)
			os.Exit(1)
		}
	}()

	// Block until we receive SIGINT (Ctrl+C) or SIGTERM (Kubernetes pod stop).
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit
	slog.Info("shutting down server...")

	// Give in-flight requests 10 seconds to complete before forceful shutdown.
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := srv.Shutdown(ctx); err != nil {
		slog.Error("forced shutdown", "error", err)
	}
	slog.Info("server stopped")
	return nil
}

// notImplemented is a placeholder handler for endpoints that haven't been built yet.
// It returns a 501 Not Implemented with a JSON error body matching the CatalogRO
// API error format: { "error": { "code": "...", "message": "..." } }
func notImplemented(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusNotImplemented)
	fmt.Fprint(w, `{"error":{"code":"NOT_IMPLEMENTED","message":"Endpoint not yet implemented"}}`)
}

// requestLogger returns a chi middleware that logs every HTTP request with
// structured fields: method, path, status code, and duration in milliseconds.
// It uses the provided slog.Logger instance for consistent log formatting.
func requestLogger(logger *slog.Logger) func(next http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			// Wrap the ResponseWriter to capture the status code written by the handler.
			ww := middleware.NewWrapResponseWriter(w, r.ProtoMajor)
			next.ServeHTTP(ww, r)
			logger.Info("request",
				"method", r.Method,
				"path", r.URL.Path,
				"status", ww.Status(),
				"duration_ms", time.Since(start).Milliseconds(),
				"request_id", middleware.GetReqID(r.Context()),
			)
		})
	}
}

package main

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

	"github.com/vlahsh/catalogro/api/internal/config"
	"github.com/vlahsh/catalogro/api/internal/platform"
)

func main() {
	if err := run(); err != nil {
		slog.Error("fatal", "error", err)
		os.Exit(1)
	}
}

func run() error {
	cfg := config.Load()

	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: cfg.LogLevel,
	}))
	slog.SetDefault(logger)

	// Database
	db, err := platform.NewDB(cfg.DatabaseURL)
	if err != nil {
		return fmt.Errorf("connect to database: %w", err)
	}
	defer db.Close()

	// Redis
	rdb, err := platform.NewRedis(cfg.RedisURL)
	if err != nil {
		return fmt.Errorf("connect to redis: %w", err)
	}
	defer rdb.Close()

	// Router
	r := chi.NewRouter()

	// Global middleware
	r.Use(middleware.RequestID)
	r.Use(middleware.RealIP)
	r.Use(middleware.Recoverer)
	r.Use(middleware.Timeout(30 * time.Second))
	r.Use(requestLogger(logger))

	// Health check (no auth)
	r.Get("/healthz", func(w http.ResponseWriter, r *http.Request) {
		if err := db.Ping(r.Context()); err != nil {
			http.Error(w, "db unhealthy", http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, "ok")
	})

	// API v1
	r.Route("/api/v1", func(r chi.Router) {
		// Public auth endpoints
		r.Group(func(r chi.Router) {
			r.Post("/auth/login", notImplemented)
			r.Post("/auth/refresh", notImplemented)
			r.Post("/auth/logout", notImplemented)
			r.Get("/auth/activate/{token}", notImplemented)
			r.Post("/auth/activate", notImplemented)
		})

		// Protected endpoints
		r.Group(func(r chi.Router) {
			// TODO: add auth middleware + tenant middleware
			// r.Use(auth.Middleware(cfg.JWTSecret))
			// r.Use(tenant.Middleware(db))

			// Auth (2FA)
			r.Post("/auth/2fa/setup", notImplemented)
			r.Post("/auth/2fa/verify", notImplemented)

			// Users (provisioning)
			r.Get("/users/me", notImplemented)
			r.Put("/users/me", notImplemented)
			r.Get("/users", notImplemented)
			r.Post("/users", notImplemented)
			r.Post("/users/import", notImplemented)
			r.Post("/users/{userId}/resend-activation", notImplemented)
			r.Get("/users/pending", notImplemented)
			r.Get("/users/me/children", notImplemented)

			// GDPR
			r.Post("/users/me/gdpr/consent", notImplemented)
			r.Post("/users/me/gdpr/export", notImplemented)
			r.Post("/users/me/gdpr/delete", notImplemented)

			// School config
			r.Get("/schools/current", notImplemented)
			r.Put("/schools/current", notImplemented)
			r.Get("/schools/current/year", notImplemented)

			// Classes
			r.Get("/classes", notImplemented)
			r.Post("/classes", notImplemented)
			r.Get("/classes/{classId}", notImplemented)
			r.Put("/classes/{classId}", notImplemented)
			r.Post("/classes/{classId}/enroll", notImplemented)
			r.Delete("/classes/{classId}/enroll/{studentId}", notImplemented)
			r.Get("/classes/{classId}/teachers", notImplemented)
			r.Post("/classes/{classId}/teachers", notImplemented)

			// Subjects
			r.Get("/subjects", notImplemented)
			r.Post("/subjects", notImplemented)

			// Catalog (grades)
			r.Get("/catalog/classes/{classId}/subjects/{subjectId}/grades", notImplemented)
			r.Post("/catalog/grades", notImplemented)
			r.Put("/catalog/grades/{gradeId}", notImplemented)
			r.Delete("/catalog/grades/{gradeId}", notImplemented)
			r.Post("/catalog/grades/sync", notImplemented)
			r.Post("/catalog/averages/{subjectId}/close", notImplemented)
			r.Post("/catalog/averages/{averageId}/approve", notImplemented)

			// Absences
			r.Get("/catalog/classes/{classId}/absences", notImplemented)
			r.Post("/catalog/absences", notImplemented)
			r.Put("/catalog/absences/{absenceId}/excuse", notImplemented)
			r.Post("/catalog/absences/sync", notImplemented)

			// Descriptive evaluations (primary)
			r.Get("/catalog/classes/{classId}/subjects/{subjectId}/evaluations", notImplemented)
			r.Post("/catalog/evaluations", notImplemented)
			r.Put("/catalog/evaluations/{evalId}", notImplemented)

			// Sync
			r.Post("/sync/push", notImplemented)
			r.Get("/sync/pull", notImplemented)

			// Messages
			r.Get("/messages", notImplemented)
			r.Get("/messages/{messageId}", notImplemented)
			r.Post("/messages", notImplemented)
			r.Post("/messages/announcements", notImplemented)
			r.Put("/messages/{messageId}/read", notImplemented)

			// Reports
			r.Post("/reports/catalog-pdf", notImplemented)
			r.Get("/reports/jobs/{jobId}", notImplemented)
			r.Get("/reports/dashboard", notImplemented)
			r.Get("/reports/student/{studentId}", notImplemented)
			r.Get("/reports/class/{classId}/stats", notImplemented)
			r.Post("/reports/isj-export", notImplemented)

			// Interoperability (import/export)
			r.Post("/interop/import", notImplemented)                    // upload CSV, auto-detect format
			r.Post("/interop/import/{importId}/confirm", notImplemented) // confirm after preview
			r.Get("/interop/import/{importId}/status", notImplemented)
			r.Post("/interop/export/siiir", notImplemented)                   // export SIIIR format for ISJ
			r.Post("/interop/portability/export/{studentId}", notImplemented) // student record package (EHEIF)
			r.Post("/interop/portability/import", notImplemented)             // import transferred student
			r.Get("/interop/source-mappings", notImplemented)                 // list entity ↔ external ID mappings
		})

		// OneRoster 1.2 API (separate auth: API key for machine-to-machine)
		r.Group(func(r chi.Router) {
			// TODO: add API key auth middleware
			// r.Use(auth.APIKeyMiddleware())

			r.Get("/oneroster/orgs", notImplemented)
			r.Get("/oneroster/orgs/{sourcedId}", notImplemented)
			r.Get("/oneroster/users", notImplemented)
			r.Get("/oneroster/users/{sourcedId}", notImplemented)
			r.Get("/oneroster/classes", notImplemented)
			r.Get("/oneroster/classes/{sourcedId}/students", notImplemented)
			r.Get("/oneroster/courses", notImplemented)
			r.Get("/oneroster/enrollments", notImplemented)
			r.Get("/oneroster/academicSessions", notImplemented)
			r.Get("/oneroster/lineItems", notImplemented)
			r.Get("/oneroster/results", notImplemented)
		})
	})

	// OpenAPI discovery (no auth, EIF compliance)
	r.Get("/.well-known/openapi.json", notImplemented)

	// Server
	srv := &http.Server{
		Addr:         ":" + cfg.Port,
		Handler:      r,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 30 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	// Graceful shutdown
	go func() {
		slog.Info("server starting", "port", cfg.Port, "env", cfg.Env)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			slog.Error("server failed", "error", err)
			os.Exit(1)
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit
	slog.Info("shutting down server...")

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := srv.Shutdown(ctx); err != nil {
		slog.Error("forced shutdown", "error", err)
	}
	slog.Info("server stopped")
	return nil
}

func notImplemented(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusNotImplemented)
	fmt.Fprint(w, `{"error":{"code":"NOT_IMPLEMENTED","message":"Endpoint not yet implemented"}}`)
}

func requestLogger(logger *slog.Logger) func(next http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
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

package server

import (
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/cors"
	"github.com/go-chi/httprate"
)

// NewRouter builds the full HTTP handler with the standard middleware stack.
func NewRouter(a *App) http.Handler {
	r := chi.NewRouter()

	r.Use(middleware.RequestID)
	r.Use(middleware.RealIP)
	r.Use(skipLogger("/healthz", "/readyz"))
	r.Use(middleware.Recoverer)
	r.Use(securityHeaders)
	r.Use(cors.Handler(cors.Options{
		AllowedOrigins:   a.Cfg.AllowedOrigins,
		AllowedMethods:   []string{"GET", "POST", "PUT", "DELETE", "OPTIONS"},
		AllowedHeaders:   []string{"Authorization", "Content-Type"},
		AllowCredentials: false,
		MaxAge:           300,
	}))

	r.NotFound(handleNotFound)
	r.MethodNotAllowed(handleMethodNotAllowed)

	// Probes.
	r.Get("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		_, _ = w.Write([]byte("ok"))
	})
	r.Get("/readyz", func(w http.ResponseWriter, _ *http.Request) {
		if !a.ready.Load() {
			http.Error(w, "not ready", http.StatusServiceUnavailable)
			return
		}
		w.Header().Set("Content-Type", "text/plain")
		_, _ = w.Write([]byte("ok"))
	})

	r.Route("/api", func(r chi.Router) {
		r.Get("/version", a.handleVersion)

		// Public auth surface.
		r.Get("/auth/config", a.handleAuthConfig)
		r.Get("/auth/oidc/start", a.handleOIDCStart)
		r.Get("/auth/oidc/callback", a.handleOIDCCallback)

		// Rate-limited credential endpoints (per IP).
		r.Group(func(r chi.Router) {
			r.Use(httprate.LimitByIP(30, time.Minute))
			r.Post("/auth/login", a.handleDevLogin)
		})

		// Authenticated surface.
		r.Group(func(r chi.Router) {
			r.Use(a.authMiddleware)

			// Available to every authenticated user, viewers included: their own
			// profile and the static client config carry no platform data.
			r.Get("/me", a.handleMe)
			r.Get("/config", a.handleClientConfig)

			// Data-read surface — users and admins only. Viewers are provisioned
			// but see no repos, jobs, or logs until an admin promotes them.
			r.Group(func(r chi.Router) {
				r.Use(a.requireReaderMiddleware)

				r.Get("/repos", a.handleListRepos)
				r.Get("/jobs", a.handleListJobs)
				r.Get("/jobs/{id}", a.handleGetJob)
				r.Get("/jobs/{id}/logs", a.handleJobLogs)
			})

			// Write surface — admins only. Users have full read visibility but
			// cannot start, delete, or otherwise mutate anything.
			r.Group(func(r chi.Router) {
				r.Use(a.requireAdminMiddleware)

				r.Post("/jobs", a.handleCreateJob)
				r.Delete("/jobs/{id}", a.handleDeleteJob)

				r.Post("/repos", a.handleCreateRepo)
				r.Put("/repos/{id}", a.handleUpdateRepo)
				r.Delete("/repos/{id}", a.handleDeleteRepo)

				r.Get("/admin/users", a.handleListUsers)
				r.Put("/admin/users/{id}/role", a.handleSetUserRole)
			})
		})
	})

	return r
}

// skipLogger applies the chi request logger except on the given noisy paths.
func skipLogger(skip ...string) func(http.Handler) http.Handler {
	skipSet := make(map[string]bool, len(skip))
	for _, s := range skip {
		skipSet[s] = true
	}
	logger := middleware.Logger
	return func(next http.Handler) http.Handler {
		wrapped := logger(next)
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if skipSet[r.URL.Path] {
				next.ServeHTTP(w, r)
				return
			}
			wrapped.ServeHTTP(w, r)
		})
	}
}

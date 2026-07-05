// Package server holds all HTTP wiring: the App struct, router, middleware,
// auth, and domain handlers. Business logic that isn't transport-specific lives
// in sibling internal packages (deepseek, k8s).
package server

import (
	"database/sql"
	"encoding/json"
	"log"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	"github.com/go-chi/chi/v5/middleware"

	"github.com/oglimmer/coding-agent/backend/internal/config"
	"github.com/oglimmer/coding-agent/backend/internal/deepseek"
	"github.com/oglimmer/coding-agent/backend/internal/k8s"
)

// App carries shared dependencies. A nil collaborator (OIDC, K8s, DeepSeek not
// configured) is handled gracefully by the handlers that use it.
type App struct {
	Cfg      config.Config
	DB       *sql.DB
	Store    *Store
	DeepSeek *deepseek.Client
	K8s      *k8s.Client
	OIDC     *oidcRuntime

	ready atomic.Bool

	// cooldownMu guards lastRunPerUser, the per-user rate-limit clock.
	cooldownMu     sync.Mutex
	lastRunPerUser map[string]time.Time
}

// NewApp wires an App from its dependencies. K8s and OIDC may be nil.
func NewApp(cfg config.Config, pool *sql.DB, ds *deepseek.Client, kc *k8s.Client, oidc *oidcRuntime) *App {
	return &App{
		Cfg:            cfg,
		DB:             pool,
		Store:          &Store{DB: pool},
		DeepSeek:       ds,
		K8s:            kc,
		OIDC:           oidc,
		lastRunPerUser: make(map[string]time.Time),
	}
}

// MarkReady flips /readyz to 200.
func (a *App) MarkReady() { a.ready.Store(true) }

// --- JSON helpers ------------------------------------------------------------

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if v != nil {
		if err := json.NewEncoder(w).Encode(v); err != nil {
			log.Printf("ERROR writeJSON: %v", err)
		}
	}
}

// writeErr sends the standard {"error": "..."} body.
func writeErr(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

// serverErr logs the underlying error with the request ID and returns a generic
// 500 so internals never leak to the client.
func (a *App) serverErr(w http.ResponseWriter, r *http.Request, err error, publicMsg string) {
	log.Printf("ERROR request=%s %s %s: %v", middleware.GetReqID(r.Context()), r.Method, r.URL.Path, err)
	if publicMsg == "" {
		publicMsg = "internal server error"
	}
	writeErr(w, http.StatusInternalServerError, publicMsg)
}

func decodeJSON(r *http.Request, v any) error {
	dec := json.NewDecoder(http.MaxBytesReader(nil, r.Body, 1<<20))
	dec.DisallowUnknownFields()
	return dec.Decode(v)
}

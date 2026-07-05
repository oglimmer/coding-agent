package server

import (
	"net/http"

	"github.com/oglimmer/coding-agent/backend/internal/buildinfo"
)

func (a *App) handleVersion(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{
		"version": buildinfo.Version,
		"commit":  buildinfo.Commit,
		"time":    buildinfo.Time,
	})
}

package server

import (
	"net/http"
	"regexp"
	"strings"

	"github.com/go-chi/chi/v5"
)

var repoPartRe = regexp.MustCompile(`^[A-Za-z0-9_.-]+$`)

// handleListRepos returns every configured repo (any authenticated user).
func (a *App) handleListRepos(w http.ResponseWriter, r *http.Request) {
	repos, err := a.Store.ListRepos(r.Context())
	if err != nil {
		a.serverErr(w, r, err, "")
		return
	}
	if repos == nil {
		repos = []Repo{}
	}
	writeJSON(w, http.StatusOK, repos)
}

// handleCreateRepo adds a repo (admin only).
func (a *App) handleCreateRepo(w http.ResponseWriter, r *http.Request) {
	u, _ := userFromContext(r.Context())
	var req struct {
		Owner      string `json:"owner"`
		Name       string `json:"name"`
		BaseBranch string `json:"baseBranch"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid request body")
		return
	}
	req.Owner = strings.TrimSpace(req.Owner)
	req.Name = strings.TrimSpace(req.Name)
	req.BaseBranch = strings.TrimSpace(req.BaseBranch)

	// Accept "owner/name" pasted into the owner field for convenience.
	if req.Name == "" && strings.Contains(req.Owner, "/") {
		parts := strings.SplitN(req.Owner, "/", 2)
		req.Owner, req.Name = strings.TrimSpace(parts[0]), strings.TrimSpace(parts[1])
	}
	if !repoPartRe.MatchString(req.Owner) || !repoPartRe.MatchString(req.Name) {
		writeErr(w, http.StatusBadRequest, "owner and name are required and must be valid GitHub identifiers")
		return
	}

	repo, err := a.Store.CreateRepo(r.Context(), req.Owner, req.Name, req.BaseBranch, u.ID)
	if err != nil {
		if isUniqueViolation(err) {
			writeErr(w, http.StatusConflict, "that repository is already configured")
			return
		}
		a.serverErr(w, r, err, "")
		return
	}
	writeJSON(w, http.StatusCreated, repo)
}

// handleDeleteRepo removes a repo (admin only).
func (a *App) handleDeleteRepo(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if err := a.Store.DeleteRepo(r.Context(), id); err != nil {
		if err == ErrNotFound {
			writeErr(w, http.StatusNotFound, "repository not found")
			return
		}
		a.serverErr(w, r, err, "")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func isUniqueViolation(err error) bool {
	// pgx surfaces SQLSTATE 23505 in the error text; avoid a hard dependency on
	// the pgconn error type to keep the store transport-agnostic.
	return strings.Contains(err.Error(), "23505") || strings.Contains(err.Error(), "duplicate key")
}

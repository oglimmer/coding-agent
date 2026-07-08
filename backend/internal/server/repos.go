package server

import (
	"net/http"
	"regexp"
	"strings"

	"github.com/go-chi/chi/v5"
)

var repoPartRe = regexp.MustCompile(`^[A-Za-z0-9_.-]+$`)

// repoInput is the shared create/update request body.
type repoInput struct {
	Owner         string `json:"owner"`
	Name          string `json:"name"`
	BaseBranch    string `json:"baseBranch"`
	VerifyCommand string `json:"verifyCommand"`
	TestCommand   string `json:"testCommand"`
}

// normalize trims the fields and accepts an "owner/name" string pasted into the
// owner field for convenience. It reports whether owner and name are valid
// GitHub identifiers.
func (in *repoInput) normalize() bool {
	in.Owner = strings.TrimSpace(in.Owner)
	in.Name = strings.TrimSpace(in.Name)
	in.BaseBranch = strings.TrimSpace(in.BaseBranch)
	in.VerifyCommand = strings.TrimSpace(in.VerifyCommand)
	in.TestCommand = strings.TrimSpace(in.TestCommand)

	if in.Name == "" && strings.Contains(in.Owner, "/") {
		parts := strings.SplitN(in.Owner, "/", 2)
		in.Owner, in.Name = strings.TrimSpace(parts[0]), strings.TrimSpace(parts[1])
	}
	return repoPartRe.MatchString(in.Owner) && repoPartRe.MatchString(in.Name)
}

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
	var req repoInput
	if err := decodeJSON(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if !req.normalize() {
		writeErr(w, http.StatusBadRequest, "owner and name are required and must be valid GitHub identifiers")
		return
	}

	repo, err := a.Store.CreateRepo(r.Context(), req.Owner, req.Name, req.BaseBranch, req.VerifyCommand, req.TestCommand, u.ID)
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

// handleUpdateRepo changes an existing repo's configuration (admin only).
func (a *App) handleUpdateRepo(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	var req repoInput
	if err := decodeJSON(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if !req.normalize() {
		writeErr(w, http.StatusBadRequest, "owner and name are required and must be valid GitHub identifiers")
		return
	}

	repo, err := a.Store.UpdateRepo(r.Context(), id, req.Owner, req.Name, req.BaseBranch, req.VerifyCommand, req.TestCommand)
	if err != nil {
		switch {
		case err == ErrNotFound:
			writeErr(w, http.StatusNotFound, "repository not found")
		case isUniqueViolation(err):
			writeErr(w, http.StatusConflict, "that repository is already configured")
		default:
			a.serverErr(w, r, err, "")
		}
		return
	}
	writeJSON(w, http.StatusOK, repo)
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

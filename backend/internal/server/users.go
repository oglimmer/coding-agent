package server

import (
	"net/http"

	"github.com/go-chi/chi/v5"
)

// handleListUsers returns all users (admin only).
func (a *App) handleListUsers(w http.ResponseWriter, r *http.Request) {
	users, err := a.Store.ListUsers(r.Context())
	if err != nil {
		a.serverErr(w, r, err, "")
		return
	}
	if users == nil {
		users = []User{}
	}
	writeJSON(w, http.StatusOK, users)
}

// handleSetUserRole changes a user's role (admin only). It refuses to demote the
// last remaining admin so the platform can never be locked out.
func (a *App) handleSetUserRole(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	var req struct {
		Role Role `json:"role"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if !req.Role.Valid() {
		writeErr(w, http.StatusBadRequest, "role must be one of: viewer, user, admin")
		return
	}

	target, err := a.Store.UserByID(r.Context(), id)
	if err != nil {
		if err == ErrNotFound {
			writeErr(w, http.StatusNotFound, "user not found")
			return
		}
		a.serverErr(w, r, err, "")
		return
	}

	// Guard the last admin from being demoted out of existence.
	if target.IsAdmin() && req.Role != RoleAdmin {
		count, err := a.Store.CountAdmins(r.Context())
		if err != nil {
			a.serverErr(w, r, err, "")
			return
		}
		if count <= 1 {
			writeErr(w, http.StatusConflict, "cannot demote the last admin")
			return
		}
	}

	updated, err := a.Store.SetRole(r.Context(), id, req.Role)
	if err != nil {
		if err == ErrNotFound {
			writeErr(w, http.StatusNotFound, "user not found")
			return
		}
		a.serverErr(w, r, err, "")
		return
	}
	writeJSON(w, http.StatusOK, updated)
}

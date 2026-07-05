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

// handleGrantAdmin promotes a user to admin (admin only).
func (a *App) handleGrantAdmin(w http.ResponseWriter, r *http.Request) {
	a.setAdmin(w, r, true)
}

// handleRevokeAdmin demotes a user, refusing to remove the last admin.
func (a *App) handleRevokeAdmin(w http.ResponseWriter, r *http.Request) {
	a.setAdmin(w, r, false)
}

func (a *App) setAdmin(w http.ResponseWriter, r *http.Request, admin bool) {
	id := chi.URLParam(r, "id")
	target, err := a.Store.UserByID(r.Context(), id)
	if err != nil {
		if err == ErrNotFound {
			writeErr(w, http.StatusNotFound, "user not found")
			return
		}
		a.serverErr(w, r, err, "")
		return
	}

	if !admin && target.IsAdmin {
		count, err := a.Store.CountAdmins(r.Context())
		if err != nil {
			a.serverErr(w, r, err, "")
			return
		}
		if count <= 1 {
			writeErr(w, http.StatusConflict, "cannot remove the last admin")
			return
		}
	}

	updated, err := a.Store.SetAdmin(r.Context(), id, admin)
	if err != nil {
		a.serverErr(w, r, err, "")
		return
	}
	writeJSON(w, http.StatusOK, updated)
}

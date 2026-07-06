package server

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestRoleCapabilities(t *testing.T) {
	cases := []struct {
		role     Role
		valid    bool
		canWrite bool
		isAdmin  bool
	}{
		{RoleViewer, true, false, false},
		{RoleUser, true, true, false},
		{RoleAdmin, true, true, true},
		{Role("bogus"), false, false, false},
		{Role(""), false, false, false},
	}
	for _, c := range cases {
		if got := c.role.Valid(); got != c.valid {
			t.Errorf("%q.Valid() = %v, want %v", c.role, got, c.valid)
		}
		if got := c.role.CanWrite(); got != c.canWrite {
			t.Errorf("%q.CanWrite() = %v, want %v", c.role, got, c.canWrite)
		}
		if got := (User{Role: c.role}).IsAdmin(); got != c.isAdmin {
			t.Errorf("User{%q}.IsAdmin() = %v, want %v", c.role, got, c.isAdmin)
		}
	}
}

// withUser returns a request carrying u in its context, as authMiddleware would.
func withUser(u *User) *http.Request {
	r := httptest.NewRequest(http.MethodPost, "/", nil)
	if u != nil {
		r = r.WithContext(context.WithValue(r.Context(), userCtxKey, *u))
	}
	return r
}

func okHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
}

func TestRequireWriterMiddleware(t *testing.T) {
	a := &App{}
	cases := []struct {
		name string
		user *User
		want int
	}{
		{"viewer forbidden", &User{Role: RoleViewer}, http.StatusForbidden},
		{"user allowed", &User{Role: RoleUser}, http.StatusOK},
		{"admin allowed", &User{Role: RoleAdmin}, http.StatusOK},
		{"no user forbidden", nil, http.StatusForbidden},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			a.requireWriterMiddleware(okHandler()).ServeHTTP(rec, withUser(c.user))
			if rec.Code != c.want {
				t.Errorf("status = %d, want %d", rec.Code, c.want)
			}
		})
	}
}

func TestRequireAdminMiddleware(t *testing.T) {
	a := &App{}
	cases := []struct {
		name string
		user *User
		want int
	}{
		{"viewer forbidden", &User{Role: RoleViewer}, http.StatusForbidden},
		{"user forbidden", &User{Role: RoleUser}, http.StatusForbidden},
		{"admin allowed", &User{Role: RoleAdmin}, http.StatusOK},
		{"no user forbidden", nil, http.StatusForbidden},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			a.requireAdminMiddleware(okHandler()).ServeHTTP(rec, withUser(c.user))
			if rec.Code != c.want {
				t.Errorf("status = %d, want %d", rec.Code, c.want)
			}
		})
	}
}

package server

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

type ctxKey int

const userCtxKey ctxKey = iota

// sessionClaims is the JWT payload for an authenticated session.
type sessionClaims struct {
	Ver int `json:"ver"`
	jwt.RegisteredClaims
}

// issueToken signs a session JWT for a user.
func (a *App) issueToken(u User) (string, error) {
	now := time.Now()
	claims := sessionClaims{
		Ver: u.TokenVersion,
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   u.ID,
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(a.Cfg.SessionTTL)),
		},
	}
	return jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString([]byte(a.Cfg.JWTSecret))
}

// parseToken validates a JWT and returns its claims.
func (a *App) parseToken(tokenStr string) (*sessionClaims, error) {
	claims := &sessionClaims{}
	_, err := jwt.ParseWithClaims(tokenStr, claims, func(t *jwt.Token) (any, error) {
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, errors.New("unexpected signing method")
		}
		return []byte(a.Cfg.JWTSecret), nil
	})
	if err != nil {
		return nil, err
	}
	return claims, nil
}

// userFromRequest resolves the authenticated user, re-checking the token
// version against the DB so revoked/privilege-changed sessions are rejected.
func (a *App) userFromRequest(r *http.Request) (User, error) {
	authz := r.Header.Get("Authorization")
	if !strings.HasPrefix(authz, "Bearer ") {
		return User{}, errors.New("missing bearer token")
	}
	claims, err := a.parseToken(strings.TrimPrefix(authz, "Bearer "))
	if err != nil {
		return User{}, err
	}
	u, err := a.Store.UserByID(r.Context(), claims.Subject)
	if err != nil {
		return User{}, err
	}
	if claims.Ver != u.TokenVersion {
		return User{}, errors.New("token revoked")
	}
	return u, nil
}

// authMiddleware requires a valid session and stashes the user in the context.
func (a *App) authMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		u, err := a.userFromRequest(r)
		if err != nil {
			writeErr(w, http.StatusUnauthorized, "unauthorized")
			return
		}
		ctx := context.WithValue(r.Context(), userCtxKey, u)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// requireAdminMiddleware gates admin-only routes.
func (a *App) requireAdminMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		u, ok := userFromContext(r.Context())
		if !ok || !u.IsAdmin {
			writeErr(w, http.StatusForbidden, "admin access required")
			return
		}
		next.ServeHTTP(w, r)
	})
}

func userFromContext(ctx context.Context) (User, bool) {
	u, ok := ctx.Value(userCtxKey).(User)
	return u, ok
}

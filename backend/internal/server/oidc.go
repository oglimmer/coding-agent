package server

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	oidc "github.com/coreos/go-oidc/v3/oidc"
	"golang.org/x/oauth2"

	"github.com/oglimmer/coding-agent/backend/internal/config"
)

// oidcRuntime holds the discovered provider and OAuth2 config. It is nil when
// OIDC is not configured.
type oidcRuntime struct {
	provider *oidc.Provider
	verifier *oidc.IDTokenVerifier
	oauth    oauth2.Config
}

// InitOIDC performs provider discovery. Returns nil (not an error) when OIDC is
// not enabled, so startup can continue with the dev password stub.
func InitOIDC(ctx context.Context, cfg config.Config) (*oidcRuntime, error) {
	if !cfg.OIDCEnabled() {
		return nil, nil
	}
	provider, err := oidc.NewProvider(ctx, cfg.OIDCIssuer)
	if err != nil {
		return nil, fmt.Errorf("oidc discovery: %w", err)
	}
	return &oidcRuntime{
		provider: provider,
		verifier: provider.Verifier(&oidc.Config{ClientID: cfg.OIDCClientID}),
		oauth: oauth2.Config{
			ClientID:     cfg.OIDCClientID,
			ClientSecret: cfg.OIDCClientSecret,
			Endpoint:     provider.Endpoint(),
			RedirectURL:  strings.TrimRight(cfg.PublicBaseURL, "/") + "/api/auth/oidc/callback",
			Scopes:       cfg.OIDCScopes,
		},
	}, nil
}

func randomState() string {
	b := make([]byte, 24)
	_, _ = rand.Read(b)
	return base64.RawURLEncoding.EncodeToString(b)
}

func (a *App) setCookie(w http.ResponseWriter, name, value string) {
	http.SetCookie(w, &http.Cookie{
		Name:     name,
		Value:    value,
		Path:     "/api/auth",
		HttpOnly: true,
		Secure:   strings.HasPrefix(a.Cfg.PublicBaseURL, "https://"),
		SameSite: http.SameSiteLaxMode,
		MaxAge:   600,
	})
}

// handleAuthConfig is the unauthenticated bootstrap the frontend reads to render
// the correct login UI.
func (a *App) handleAuthConfig(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"mode":        a.Cfg.AuthMode,
		"oidcEnabled": a.OIDC != nil,
	})
}

// handleOIDCStart redirects the browser to the provider with state+nonce.
func (a *App) handleOIDCStart(w http.ResponseWriter, r *http.Request) {
	if a.OIDC == nil {
		writeErr(w, http.StatusNotFound, "OIDC is not configured")
		return
	}
	state := randomState()
	nonce := randomState()
	a.setCookie(w, "oidc_state", state)
	a.setCookie(w, "oidc_nonce", nonce)
	url := a.OIDC.oauth.AuthCodeURL(state, oidc.Nonce(nonce))
	http.Redirect(w, r, url, http.StatusFound)
}

// handleOIDCCallback verifies the provider response, upserts the user, issues a
// session JWT, and redirects back to the SPA with the token in the fragment.
func (a *App) handleOIDCCallback(w http.ResponseWriter, r *http.Request) {
	if a.OIDC == nil {
		writeErr(w, http.StatusNotFound, "OIDC is not configured")
		return
	}
	ctx := r.Context()

	stateCookie, err := r.Cookie("oidc_state")
	if err != nil || r.URL.Query().Get("state") != stateCookie.Value {
		a.redirectLoginError(w, r, "invalid_state")
		return
	}
	nonceCookie, err := r.Cookie("oidc_nonce")
	if err != nil {
		a.redirectLoginError(w, r, "missing_nonce")
		return
	}

	oauth2Token, err := a.OIDC.oauth.Exchange(ctx, r.URL.Query().Get("code"))
	if err != nil {
		a.redirectLoginError(w, r, "exchange_failed")
		return
	}
	rawIDToken, ok := oauth2Token.Extra("id_token").(string)
	if !ok {
		a.redirectLoginError(w, r, "no_id_token")
		return
	}
	idToken, err := a.OIDC.verifier.Verify(ctx, rawIDToken)
	if err != nil {
		a.redirectLoginError(w, r, "invalid_id_token")
		return
	}
	if idToken.Nonce != nonceCookie.Value {
		a.redirectLoginError(w, r, "nonce_mismatch")
		return
	}

	var claims struct {
		Email string `json:"email"`
		Name  string `json:"name"`
	}
	if err := idToken.Claims(&claims); err != nil {
		a.redirectLoginError(w, r, "claims_failed")
		return
	}
	name := claims.Name
	if name == "" {
		name = claims.Email
	}

	u, err := a.Store.UpsertOIDCUser(ctx, idToken.Subject, claims.Email, name)
	if err != nil {
		a.serverErr(w, r, err, "")
		return
	}
	token, err := a.issueToken(u)
	if err != nil {
		a.serverErr(w, r, err, "")
		return
	}
	// Hand the token to the SPA via the callback route's URL fragment.
	dest := strings.TrimRight(a.Cfg.PublicBaseURL, "/") + "/auth/callback#token=" + url.QueryEscape(token)
	http.Redirect(w, r, dest, http.StatusFound)
}

func (a *App) redirectLoginError(w http.ResponseWriter, r *http.Request, code string) {
	dest := strings.TrimRight(a.Cfg.PublicBaseURL, "/") + "/login?error=" + url.QueryEscape(code)
	http.Redirect(w, r, dest, http.StatusFound)
}

// handleDevLogin is the password stub, active only when AUTH_MODE=password.
func (a *App) handleDevLogin(w http.ResponseWriter, r *http.Request) {
	if a.Cfg.AuthMode != "password" {
		writeErr(w, http.StatusNotFound, "password login is disabled")
		return
	}
	var req struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid request body")
		return
	}
	req.Username = strings.TrimSpace(req.Username)
	if req.Username == "" || req.Password != a.Cfg.DevPassword {
		writeErr(w, http.StatusUnauthorized, "invalid credentials")
		return
	}
	u, err := a.Store.UpsertPasswordUser(r.Context(), req.Username)
	if err != nil {
		a.serverErr(w, r, err, "")
		return
	}
	token, err := a.issueToken(u)
	if err != nil {
		a.serverErr(w, r, err, "")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"token": token, "user": u})
}

// handleMe returns the current authenticated user.
func (a *App) handleMe(w http.ResponseWriter, r *http.Request) {
	u, _ := userFromContext(r.Context())
	writeJSON(w, http.StatusOK, u)
}

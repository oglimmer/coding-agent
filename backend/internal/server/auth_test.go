package server

import (
	"testing"
	"time"

	"github.com/oglimmer/coding-agent/backend/internal/config"
)

func testApp() *App {
	return &App{Cfg: config.Config{
		JWTSecret:  "test-secret-that-is-at-least-32-chars-long",
		SessionTTL: time.Hour,
	}}
}

func TestIssueAndParseToken(t *testing.T) {
	a := testApp()
	u := User{ID: "user-123", TokenVersion: 4}

	token, err := a.issueToken(u)
	if err != nil {
		t.Fatalf("issueToken: %v", err)
	}
	claims, err := a.parseToken(token)
	if err != nil {
		t.Fatalf("parseToken: %v", err)
	}
	if claims.Subject != u.ID {
		t.Errorf("subject = %q, want %q", claims.Subject, u.ID)
	}
	if claims.Ver != u.TokenVersion {
		t.Errorf("ver = %d, want %d", claims.Ver, u.TokenVersion)
	}
}

func TestParseTokenRejectsWrongSecret(t *testing.T) {
	a := testApp()
	token, err := a.issueToken(User{ID: "x"})
	if err != nil {
		t.Fatalf("issueToken: %v", err)
	}
	other := &App{Cfg: config.Config{JWTSecret: "a-completely-different-secret-32-chars", SessionTTL: time.Hour}}
	if _, err := other.parseToken(token); err == nil {
		t.Error("expected parse to fail with a different secret")
	}
}

func TestParseTokenRejectsExpired(t *testing.T) {
	a := &App{Cfg: config.Config{JWTSecret: "test-secret-that-is-at-least-32-chars-long", SessionTTL: -time.Hour}}
	token, err := a.issueToken(User{ID: "x"})
	if err != nil {
		t.Fatalf("issueToken: %v", err)
	}
	if _, err := a.parseToken(token); err == nil {
		t.Error("expected parse to fail for expired token")
	}
}

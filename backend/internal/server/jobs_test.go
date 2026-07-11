package server

import (
	"strings"
	"testing"

	"github.com/oglimmer/coding-agent/backend/internal/config"
	"github.com/oglimmer/coding-agent/backend/internal/k8s"
)

func modelApp() *App {
	return &App{Cfg: config.Config{
		WorkerModel:        "deepseek/deepseek-v4-pro",
		WorkerEditorModel:  "deepseek/deepseek-chat",
		WorkerAiderModels:  []string{"deepseek/deepseek-v4-pro", "deepseek/deepseek-chat", "anthropic/claude-opus-4-8"},
		WorkerClaudeModel:  "deepseek-v4-pro",
		WorkerClaudeModels: []string{"deepseek-v4-pro", "deepseek-v4-flash"},
	}}
}

func TestResolveModelsDefaults(t *testing.T) {
	a := modelApp()

	// Empty request → the engine's deployment defaults.
	m, em, errMsg := a.resolveModels(k8s.EngineAider, "", "")
	if errMsg != "" {
		t.Fatalf("unexpected error: %s", errMsg)
	}
	if m != "deepseek/deepseek-v4-pro" || em != "deepseek/deepseek-chat" {
		t.Errorf("aider defaults = (%q, %q)", m, em)
	}

	m, em, errMsg = a.resolveModels(k8s.EngineClaudeCode, "", "")
	if errMsg != "" {
		t.Fatalf("unexpected error: %s", errMsg)
	}
	if m != "deepseek-v4-pro" || em != "" {
		t.Errorf("claude defaults = (%q, %q), want editor empty", m, em)
	}
}

func TestResolveModelsAllowsListedAndRejectsUnknown(t *testing.T) {
	a := modelApp()

	m, em, errMsg := a.resolveModels(k8s.EngineAider, "anthropic/claude-opus-4-8", "deepseek/deepseek-chat")
	if errMsg != "" {
		t.Fatalf("listed models should be accepted: %s", errMsg)
	}
	if m != "anthropic/claude-opus-4-8" || em != "deepseek/deepseek-chat" {
		t.Errorf("resolved = (%q, %q)", m, em)
	}

	if _, _, errMsg = a.resolveModels(k8s.EngineAider, "gpt-4o", ""); errMsg == "" {
		t.Error("an off-list model should be rejected")
	}
	if _, _, errMsg = a.resolveModels(k8s.EngineAider, "", "gpt-4o"); errMsg == "" {
		t.Error("an off-list editor model should be rejected")
	}
	// A claude-code model from aider's list must not be accepted (per-engine lists).
	if _, _, errMsg = a.resolveModels(k8s.EngineClaudeCode, "deepseek/deepseek-v4-pro", ""); errMsg == "" {
		t.Error("aider-namespaced model should be rejected for claude-code")
	}
}

func TestResolveModelsClaudeIgnoresEditor(t *testing.T) {
	a := modelApp()
	// claude-code has no editor split; a supplied editor model is ignored, not rejected.
	m, em, errMsg := a.resolveModels(k8s.EngineClaudeCode, "deepseek-v4-flash", "whatever")
	if errMsg != "" {
		t.Fatalf("unexpected error: %s", errMsg)
	}
	if m != "deepseek-v4-flash" || em != "" {
		t.Errorf("resolved = (%q, %q), want editor empty", m, em)
	}
}

func TestBuildPromptAlwaysRequiresTests(t *testing.T) {
	prompt := buildPrompt("oglimmer/example", "alice", "add a dark mode toggle")
	if !strings.Contains(prompt, "oglimmer/example") {
		t.Error("prompt should mention the repo")
	}
	if !strings.Contains(prompt, "add a dark mode toggle") {
		t.Error("prompt should include the feature request")
	}
	if !strings.Contains(strings.ToUpper(prompt), "TESTS ARE MANDATORY") {
		t.Error("prompt must always demand tests")
	}
	if !strings.Contains(prompt, "independently reviewed") {
		t.Error("prompt should mention the downstream review loop")
	}
	if !strings.Contains(prompt, "WHERE it belongs") {
		t.Error("prompt should demand locating the right place before editing")
	}
}

func TestSanitizeSlug(t *testing.T) {
	tests := []struct {
		in   string
		want string
	}{
		{"Add Dark Mode!", "add-dark-mode"},
		{"   ", "feature"},
		{"a/b/c", "a-b-c"},
		{"UPPER_case 123", "upper-case-123"},
	}
	for _, tt := range tests {
		if got := sanitizeSlug(tt.in, 30); got != tt.want {
			t.Errorf("sanitizeSlug(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestSanitizeSlugTruncates(t *testing.T) {
	got := sanitizeSlug(strings.Repeat("abc ", 20), 10)
	if len(got) > 10 {
		t.Errorf("slug too long: %q", got)
	}
}

func TestTruncate(t *testing.T) {
	if got := truncate("hello world", 5); got != "hello" {
		t.Errorf("truncate = %q", got)
	}
	if got := truncate("hi", 5); got != "hi" {
		t.Errorf("truncate = %q", got)
	}
}

package server

import (
	"strings"
	"testing"
)

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

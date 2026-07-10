// Package config loads all runtime configuration from environment variables
// into a single Config struct. Load() is the only public entry point.
package config

import (
	"log"
	"os"
	"strconv"
	"strings"
	"time"
)

// insecureJWTSecrets are well-known development defaults that must never reach
// a shared deployment.
var insecureJWTSecrets = map[string]bool{
	"":                      true,
	"change-me":             true,
	"dev-secret":            true,
	"insecure-dev-secret":   true,
	"please-change-this-32": true,
}

// Config holds every tunable for the service. Zero values mean "not
// configured" for optional collaborators (OIDC, DeepSeek, k8s).
type Config struct {
	// HTTP
	Addr           string
	AllowedOrigins []string
	MetricsToken   string

	// Auth
	AuthMode         string // "oidc" | "password"
	JWTSecret        string
	SessionTTL       time.Duration
	DevPassword      string // only honoured when AuthMode == "password"
	PublicBaseURL    string // external URL of the frontend, used for OIDC redirect
	AllowInsecureJWT bool

	// OIDC
	OIDCIssuer       string
	OIDCClientID     string
	OIDCClientSecret string
	OIDCScopes       []string

	// Database
	DatabaseURL string

	// DeepSeek harmful-content gate
	DeepSeekAPIKey  string
	DeepSeekBaseURL string
	DeepSeekModel   string

	// k8s worker jobs
	WorkerImage           string
	WorkerImagePullPolicy string
	WorkerModel           string
	WorkerEditorModel     string
	WorkerClaudeImage     string // image for the claude-code engine worker
	WorkerClaudeModel     string // Claude Code primary model (a DeepSeek model id)
	ClaudeTimeoutSec      int    // seconds per Claude Code round before it is killed
	WorkerNamespace       string
	WorkerSecretName      string
	WorkerImagePullSecret string
	WorkerServiceAccount  string
	GitHubBotLogin        string // reviewer login the worker must NOT treat as its own review
	JobTimeout            time.Duration
	JobCooldown           time.Duration
	MaxConcurrentJobs     int
	ReviewMaxRounds       int
	AiderTimeoutSec       int // seconds per aider round before it is killed
	WatchInterval         time.Duration
}

// Load reads configuration from the environment. Malformed optional values log
// a warning and fall back to a default rather than crashing.
func Load() Config {
	c := Config{
		Addr:           getenv("ADDR", ":8080"),
		AllowedOrigins: splitList(getenv("ALLOWED_ORIGINS", "http://localhost:5173")),
		MetricsToken:   os.Getenv("METRICS_TOKEN"),

		AuthMode:         getenv("AUTH_MODE", "oidc"),
		JWTSecret:        os.Getenv("JWT_SECRET"),
		SessionTTL:       parseDuration("SESSION_TTL", 30*24*time.Hour),
		DevPassword:      getenv("DEV_PASSWORD", "dev"),
		PublicBaseURL:    getenv("PUBLIC_BASE_URL", "http://localhost:5173"),
		AllowInsecureJWT: parseBool("ALLOW_INSECURE_JWT_SECRET", false),

		OIDCIssuer:       os.Getenv("OIDC_ISSUER"),
		OIDCClientID:     os.Getenv("OIDC_CLIENT_ID"),
		OIDCClientSecret: os.Getenv("OIDC_CLIENT_SECRET"),
		OIDCScopes:       splitList(getenv("OIDC_SCOPES", "openid,profile,email")),

		DatabaseURL: getenv("DATABASE_URL", "postgres://coding_agent:coding_agent@localhost:5432/coding_agent?sslmode=disable"),

		DeepSeekAPIKey:  os.Getenv("DEEPSEEK_API_KEY"),
		DeepSeekBaseURL: getenv("DEEPSEEK_BASE_URL", "https://api.deepseek.com"),
		DeepSeekModel:   getenv("DEEPSEEK_MODEL", "deepseek-chat"),

		WorkerImage:           getenv("WORKER_IMAGE", "ghcr.io/oglimmer/coding-agent-worker:latest"),
		WorkerImagePullPolicy: getenv("WORKER_IMAGE_PULL_POLICY", "Always"),
		WorkerModel:           getenv("WORKER_MODEL", "deepseek/deepseek-v4-pro"),
		WorkerEditorModel:     getenv("WORKER_EDITOR_MODEL", "deepseek/deepseek-chat"),
		WorkerClaudeImage:     getenv("WORKER_CLAUDE_IMAGE", "ghcr.io/oglimmer/coding-agent-worker-claude:latest"),
		WorkerClaudeModel:     getenv("WORKER_CLAUDE_MODEL", "deepseek-v4-pro"),
		ClaudeTimeoutSec:      parseInt("CLAUDE_TIMEOUT", 3600),
		WorkerNamespace:       os.Getenv("WORKER_NAMESPACE"),
		WorkerSecretName:      getenv("WORKER_SECRET_NAME", "coding-agent-secret"),
		WorkerImagePullSecret: os.Getenv("WORKER_IMAGE_PULL_SECRET"),
		WorkerServiceAccount:  os.Getenv("WORKER_SERVICE_ACCOUNT"),
		GitHubBotLogin:        getenv("GITHUB_BOT_LOGIN", "coding-agent-bot"),
		JobTimeout:            parseDuration("JOB_TIMEOUT", 120*time.Minute),
		JobCooldown:           parseDuration("JOB_COOLDOWN", 5*time.Minute),
		MaxConcurrentJobs:     parseInt("MAX_CONCURRENT_JOBS", 3),
		ReviewMaxRounds:       parseInt("REVIEW_MAX_ROUNDS", 3),
		AiderTimeoutSec:       parseInt("AIDER_TIMEOUT", 3600),
		WatchInterval:         parseDuration("WATCH_INTERVAL", 20*time.Second),
	}

	c.validate()
	return c
}

// OIDCEnabled reports whether OIDC login is configured and selected.
func (c Config) OIDCEnabled() bool {
	return c.AuthMode == "oidc" && c.OIDCIssuer != "" && c.OIDCClientID != ""
}

// DeepSeekEnabled reports whether the harmful-content gate can run.
func (c Config) DeepSeekEnabled() bool { return c.DeepSeekAPIKey != "" }

func (c *Config) validate() {
	if c.AuthMode != "oidc" && c.AuthMode != "password" {
		log.Printf("WARN config: unknown AUTH_MODE %q, falling back to oidc", c.AuthMode)
		c.AuthMode = "oidc"
	}
	if len(c.JWTSecret) < 32 && !c.AllowInsecureJWT {
		if insecureJWTSecrets[c.JWTSecret] {
			log.Fatalf("config: JWT_SECRET is empty or a known dev default; set a random 32+ char secret (or ALLOW_INSECURE_JWT_SECRET=true for local dev)")
		}
		log.Fatalf("config: JWT_SECRET must be at least 32 chars (or ALLOW_INSECURE_JWT_SECRET=true for local dev)")
	}
	if c.AuthMode == "oidc" && !c.OIDCEnabled() {
		log.Printf("WARN config: AUTH_MODE=oidc but OIDC_ISSUER/OIDC_CLIENT_ID missing; login will be unavailable until configured")
	}
	if !c.DeepSeekEnabled() {
		log.Printf("WARN config: DEEPSEEK_API_KEY not set; harmful-content gate will fail closed (reject all jobs)")
	}
}

// --- typed env helpers -------------------------------------------------------

func getenv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func splitList(v string) []string {
	parts := strings.Split(v, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

func parseInt(key string, fallback int) int {
	v := os.Getenv(key)
	if v == "" {
		return fallback
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		log.Printf("WARN config: %s=%q is not an int, using %d", key, v, fallback)
		return fallback
	}
	return n
}

func parseBool(key string, fallback bool) bool {
	v := os.Getenv(key)
	if v == "" {
		return fallback
	}
	b, err := strconv.ParseBool(v)
	if err != nil {
		log.Printf("WARN config: %s=%q is not a bool, using %v", key, v, fallback)
		return fallback
	}
	return b
}

func parseDuration(key string, fallback time.Duration) time.Duration {
	v := os.Getenv(key)
	if v == "" {
		return fallback
	}
	d, err := time.ParseDuration(v)
	if err != nil {
		log.Printf("WARN config: %s=%q is not a duration, using %s", key, v, fallback)
		return fallback
	}
	return d
}

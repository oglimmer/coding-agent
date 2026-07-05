package server

import "net/http"

// securityHeaders sets baseline security headers on every response. The API is
// JSON-only so a restrictive default CSP is safe.
func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("X-Frame-Options", "DENY")
		h.Set("Referrer-Policy", "no-referrer")
		next.ServeHTTP(w, r)
	})
}

func handleNotFound(w http.ResponseWriter, r *http.Request) {
	writeErr(w, http.StatusNotFound, "not found")
}

func handleMethodNotAllowed(w http.ResponseWriter, r *http.Request) {
	writeErr(w, http.StatusMethodNotAllowed, "method not allowed")
}

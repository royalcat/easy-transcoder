package worker

import (
	"net/http"
	"strings"
)

// AuthMiddleware returns an HTTP middleware that validates the Bearer token
// against the configured API token. When no token is configured, all requests
// are rejected with 404 (API not enabled).
func (m *Manager) AuthMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if m.config.APIToken == "" {
			http.Error(w, "Worker API not enabled", http.StatusNotFound)
			return
		}

		auth := r.Header.Get("Authorization")
		if !strings.HasPrefix(auth, "Bearer ") || auth[7:] != m.config.APIToken {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}

		next.ServeHTTP(w, r)
	})
}

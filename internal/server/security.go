package server

import (
	"net/http"
	"strings"
)

// securityHeaders preserves the ResponseWriter, including SSE flushing.
func (s *Server) securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("Content-Security-Policy", "frame-ancestors 'none'; base-uri 'self'; object-src 'none'")
		if strings.HasPrefix(r.URL.Path, "/api/") || strings.HasPrefix(r.URL.Path, "/v1/") || r.URL.Path == "/metrics" {
			w.Header().Set("Cache-Control", "no-store")
		}
		if s.d.RequireAPIToken && (strings.HasPrefix(r.URL.Path, "/v1/") || r.URL.Path == "/metrics") {
			parts := strings.Fields(r.Header.Get("Authorization"))
			bearer := len(parts) == 2 && strings.EqualFold(parts[0], "Bearer") && s.d.Store.CheckToken(parts[1])
			if !bearer && !s.d.Store.Check(sessionToken(r)) {
				w.Header().Set("WWW-Authenticate", `Bearer realm="ai-hub"`)
				writeJSON(w, http.StatusUnauthorized, map[string]interface{}{
					"error": map[string]string{"message": "A valid ai-hub bearer token or session is required", "type": "authentication_error"},
				})
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}

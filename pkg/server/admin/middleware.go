package admin

import (
	"crypto/subtle"
	"net/http"
)

// BasicAuthMiddleware returns a middleware that validates basic auth credentials.
func (s *Server) BasicAuthMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		username, password, ok := r.BasicAuth()
		if !ok {
			w.Header().Set("WWW-Authenticate", `Basic realm="Marionette Admin"`)
			WriteError(w, http.StatusUnauthorized, "unauthorized", "authentication required")
			return
		}

		// Use constant-time comparison to prevent timing attacks
		usernameMatch := subtle.ConstantTimeCompare([]byte(username), []byte(s.username)) == 1
		passwordMatch := subtle.ConstantTimeCompare([]byte(password), []byte(s.password)) == 1

		if !usernameMatch || !passwordMatch {
			w.Header().Set("WWW-Authenticate", `Basic realm="Marionette Admin"`)
			WriteError(w, http.StatusUnauthorized, "unauthorized", "invalid credentials")
			return
		}

		next.ServeHTTP(w, r)
	})
}

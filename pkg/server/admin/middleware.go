package admin

import (
	"crypto/subtle"
	"net/http"

	"github.com/chunlea/marionette/pkg/store"
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

		// The admin API is the operator console: one credential, no tenant, and
		// a purpose that spans the deployment. Grant it cross-tenant access
		// explicitly rather than leaving it to see nothing in multi-tenant
		// mode - or, worse, to be run as a database superuser, which turns
		// every policy off at once.
		next.ServeHTTP(w, r.WithContext(store.WithSystemAccess(r.Context())))
	})
}

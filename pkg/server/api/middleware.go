package api

import (
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5/middleware"
	"go.uber.org/zap"

	"github.com/chunlea/marionette/pkg/auth"
	"github.com/chunlea/marionette/pkg/store"
)

// Context keys for request-scoped values.
type contextKey string

const (
	// APIKeyContextKey is the context key for the authenticated API key.
	APIKeyContextKey contextKey = "api_key"
)

// GetAPIKey retrieves the authenticated API key from the request context.
func GetAPIKey(ctx context.Context) *store.APIKey {
	if key, ok := ctx.Value(APIKeyContextKey).(*store.APIKey); ok {
		return key
	}
	return nil
}

// isWebSocketUpgrade reports whether r is a WebSocket handshake.
//
// Connection is a comma-separated list of tokens: browsers send
// "Connection: keep-alive, Upgrade", so an exact match on "upgrade" misses
// them.
func isWebSocketUpgrade(r *http.Request) bool {
	if !strings.EqualFold(r.Header.Get("Upgrade"), "websocket") {
		return false
	}
	for _, token := range strings.Split(r.Header.Get("Connection"), ",") {
		if strings.EqualFold(strings.TrimSpace(token), "upgrade") {
			return true
		}
	}
	return false
}

// tokenFromRequest extracts the API key a request authenticates with.
//
// Normally that is the Authorization header. A WebSocket handshake started by
// a browser cannot carry one — the WebSocket constructor takes a URL and
// nothing else — so for handshakes only, the key may travel as ?token=. The
// dashboard has always sent it that way and the stream handlers have always
// read it, but the check sat behind this middleware and was unreachable, so
// every browser WebSocket got a 401.
//
// The query form is deliberately not accepted for ordinary requests: URLs end
// up in access logs, proxies and Referer headers, and there is no reason to
// put a credential there when a header will do.
func tokenFromRequest(r *http.Request) (string, *apiError) {
	if authHeader := r.Header.Get("Authorization"); authHeader != "" {
		parts := strings.SplitN(authHeader, " ", 2)
		if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") {
			return "", &apiError{"invalid_auth", "Invalid authorization header format"}
		}
		return parts[1], nil
	}

	if isWebSocketUpgrade(r) {
		if token := r.URL.Query().Get("token"); token != "" {
			return token, nil
		}
		return "", &apiError{"missing_auth", "Authorization header or token query parameter required"}
	}

	return "", &apiError{"missing_auth", "Authorization header required"}
}

// apiError carries the code and message of an authentication failure.
type apiError struct {
	code    string
	message string
}

// AuthMiddleware validates API key authentication.
func (s *Server) AuthMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token, authErr := tokenFromRequest(r)
		if authErr != nil {
			WriteError(w, http.StatusUnauthorized, authErr.code, authErr.message)
			return
		}

		// Validate API key
		if s.apiKeyService == nil {
			WriteError(w, http.StatusInternalServerError, "config_error", "API key service not configured")
			return
		}

		apiKey, err := s.apiKeyService.Validate(r.Context(), token)
		if err != nil {
			switch err {
			case auth.ErrInvalidToken, auth.ErrInvalidPrefix:
				WriteError(w, http.StatusUnauthorized, "invalid_token", "Invalid API token")
			case auth.ErrTokenNotFound:
				WriteError(w, http.StatusUnauthorized, "token_not_found", "API key not found")
			case auth.ErrTokenRevoked:
				WriteError(w, http.StatusUnauthorized, "token_revoked", "API key has been revoked")
			case auth.ErrTokenExpired:
				WriteError(w, http.StatusUnauthorized, "token_expired", "API key has expired")
			default:
				WriteError(w, http.StatusInternalServerError, "auth_error", "Authentication failed")
			}
			return
		}

		// Update last used timestamp (async to not block request)
		go func(keyID string) {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			if err := s.apiKeyService.UpdateLastUsed(ctx, keyID); err != nil {
				s.logger.Warn("failed to update API key last used timestamp",
					zap.String("key_id", keyID),
					zap.Error(err),
				)
			}
		}(apiKey.ID)

		// Add API key to context
		ctx := context.WithValue(r.Context(), APIKeyContextKey, apiKey)

		// Bind the tenant the key belongs to. This is the only place a tenant
		// enters a request: it comes from the credential, never from a header,
		// a query parameter or a body field the caller controls. Everything
		// downstream - the store's row level security, the cross-entity checks
		// in core - reads it from here.
		if apiKey.TenantID != nil && *apiKey.TenantID != "" {
			ctx = store.WithTenant(ctx, *apiKey.TenantID)
		} else if s.multiTenant {
			// A key with no tenant in a multi-tenant deployment cannot be
			// scoped to anything. Serving it would mean either showing it
			// every tenant's rows or none of them, and both are wrong answers
			// to a question that should not have been asked.
			s.logger.Error("api key has no tenant in multi-tenant mode",
				zap.String("api_key_id", apiKey.ID),
				zap.String("path", r.URL.Path),
			)
			WriteError(w, http.StatusInternalServerError, "tenant_unresolved",
				"This API key is not scoped to a tenant")
			return
		}

		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// RequireScope returns a middleware that checks if the authenticated API key has the required scope.
func RequireScope(scope string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			apiKey := GetAPIKey(r.Context())
			if apiKey == nil {
				WriteError(w, http.StatusUnauthorized, "not_authenticated", "Authentication required")
				return
			}

			if !auth.HasScope(apiKey, scope) {
				WriteError(w, http.StatusForbidden, "forbidden", "Insufficient permissions: requires scope "+scope)
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

// RequestLogger returns a middleware that logs HTTP requests.
func RequestLogger(logger *zap.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			ww := middleware.NewWrapResponseWriter(w, r.ProtoMajor)

			defer func() {
				logger.Debug("http request",
					zap.String("method", r.Method),
					zap.String("path", r.URL.Path),
					zap.Int("status", ww.Status()),
					zap.Duration("duration", time.Since(start)),
					zap.String("request_id", middleware.GetReqID(r.Context())),
				)
			}()

			next.ServeHTTP(ww, r)
		})
	}
}

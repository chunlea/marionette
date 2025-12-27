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

// AuthMiddleware validates API key authentication.
func (s *Server) AuthMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Extract token from Authorization header
		authHeader := r.Header.Get("Authorization")
		if authHeader == "" {
			WriteError(w, http.StatusUnauthorized, "missing_auth", "Authorization header required")
			return
		}

		// Parse Bearer token
		parts := strings.SplitN(authHeader, " ", 2)
		if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") {
			WriteError(w, http.StatusUnauthorized, "invalid_auth", "Invalid authorization header format")
			return
		}
		token := parts[1]

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

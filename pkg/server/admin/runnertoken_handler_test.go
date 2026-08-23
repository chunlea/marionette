package admin

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/chunlea/marionette/pkg/server/admin/admintypes"
	"github.com/chunlea/marionette/pkg/store"
)

// MockRunnerTokenService is a mock implementation of RunnerTokenAdminService.
type MockRunnerTokenService struct {
	tokens        map[string]*store.RunnerToken
	internalError error
}

// NewMockRunnerTokenService creates a new mock runner token service.
func NewMockRunnerTokenService() *MockRunnerTokenService {
	return &MockRunnerTokenService{
		tokens: make(map[string]*store.RunnerToken),
	}
}

// SetInternalError sets an internal error to be returned by all methods.
func (s *MockRunnerTokenService) SetInternalError(err error) {
	s.internalError = err
}

// AddToken adds a token to the mock store.
func (s *MockRunnerTokenService) AddToken(token *store.RunnerToken) {
	s.tokens[token.ID] = token
}

func (s *MockRunnerTokenService) Create(ctx context.Context, opts CreateRunnerTokenOptions) (*store.RunnerToken, string, error) {
	if s.internalError != nil {
		return nil, "", s.internalError
	}
	if opts.PoolName == "" {
		return nil, "", errors.New("pool_name is required")
	}
	token := &store.RunnerToken{
		ID:          "rtok_test123",
		TokenPrefix: "rtok_abcd1234",
		PoolName:    opts.PoolName,
		Status:      "active",
		Labels:      json.RawMessage("{}"),
		CreatedAt:   time.Now(),
	}
	if opts.Labels != nil {
		labelsJSON, _ := json.Marshal(opts.Labels)
		token.Labels = labelsJSON
	}
	if opts.ExpiresAt != nil {
		token.ExpiresAt = opts.ExpiresAt
	}
	s.tokens[token.ID] = token
	return token, "rtok_fullsecrettoken", nil
}

func (s *MockRunnerTokenService) Get(ctx context.Context, id string) (*store.RunnerToken, error) {
	if s.internalError != nil {
		return nil, s.internalError
	}
	token, ok := s.tokens[id]
	if !ok {
		return nil, store.ErrNotFound
	}
	return token, nil
}

func (s *MockRunnerTokenService) List(ctx context.Context, opts ListRunnerTokensOptions) (*ListResult[store.RunnerToken], error) {
	if s.internalError != nil {
		return nil, s.internalError
	}
	var items []*store.RunnerToken
	for _, token := range s.tokens {
		if !opts.IncludeRevoked && token.Status == "revoked" {
			continue
		}
		if opts.PoolName != "" && token.PoolName != opts.PoolName {
			continue
		}
		items = append(items, token)
	}
	return &ListResult[store.RunnerToken]{Items: items}, nil
}

func (s *MockRunnerTokenService) Revoke(ctx context.Context, id, reason string) error {
	if s.internalError != nil {
		return s.internalError
	}
	token, ok := s.tokens[id]
	if !ok {
		return store.ErrNotFound
	}
	now := time.Now()
	token.Status = "revoked"
	token.RevokedAt = &now
	token.RevokeReason = &reason
	return nil
}

func (s *MockRunnerTokenService) Rotate(ctx context.Context, id string) (*store.RunnerToken, string, error) {
	if s.internalError != nil {
		return nil, "", s.internalError
	}
	token, ok := s.tokens[id]
	if !ok {
		return nil, "", store.ErrNotFound
	}
	token.Status = "rotating"
	deadline := time.Now().Add(1 * time.Hour)
	token.RotationDeadline = &deadline
	return token, "rtok_newrotatedtoken", nil
}

// Verify MockRunnerTokenService implements RunnerTokenAdminService
var _ RunnerTokenAdminService = (*MockRunnerTokenService)(nil)

func TestRunnerTokenHandlers(t *testing.T) {
	mockService := NewMockRunnerTokenService()
	srv := newTestServer(WithRunnerTokenAdminService(mockService))

	// Add test data
	testToken := &store.RunnerToken{
		ID:          "rtok_test123",
		TokenPrefix: "rtok_abcd1234",
		PoolName:    "default",
		Status:      "active",
		Labels:      json.RawMessage("{}"),
		CreatedAt:   time.Now(),
	}
	mockService.AddToken(testToken)

	t.Run("list runner tokens", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/admin/api/v1/runner-tokens", nil)
		req.SetBasicAuth("admin", "secret")

		rr := httptest.NewRecorder()
		srv.Router().ServeHTTP(rr, req)

		if rr.Code != http.StatusOK {
			t.Errorf("expected status %d, got %d: %s", http.StatusOK, rr.Code, rr.Body.String())
		}

		var result ListResult[store.RunnerToken]
		if err := json.NewDecoder(rr.Body).Decode(&result); err != nil {
			t.Fatalf("failed to decode response: %v", err)
		}

		if len(result.Items) != 1 {
			t.Errorf("expected 1 item, got %d", len(result.Items))
		}
	})

	t.Run("list runner tokens with pool filter", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/admin/api/v1/runner-tokens?pool_name=default", nil)
		req.SetBasicAuth("admin", "secret")

		rr := httptest.NewRecorder()
		srv.Router().ServeHTTP(rr, req)

		if rr.Code != http.StatusOK {
			t.Errorf("expected status %d, got %d", http.StatusOK, rr.Code)
		}
	})

	t.Run("list runner tokens with include_revoked", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/admin/api/v1/runner-tokens?include_revoked=true", nil)
		req.SetBasicAuth("admin", "secret")

		rr := httptest.NewRecorder()
		srv.Router().ServeHTTP(rr, req)

		if rr.Code != http.StatusOK {
			t.Errorf("expected status %d, got %d", http.StatusOK, rr.Code)
		}
	})

	t.Run("get runner token", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/admin/api/v1/runner-tokens/rtok_test123", nil)
		req.SetBasicAuth("admin", "secret")

		rr := httptest.NewRecorder()
		srv.Router().ServeHTTP(rr, req)

		if rr.Code != http.StatusOK {
			t.Errorf("expected status %d, got %d: %s", http.StatusOK, rr.Code, rr.Body.String())
		}

		var token store.RunnerToken
		if err := json.NewDecoder(rr.Body).Decode(&token); err != nil {
			t.Fatalf("failed to decode response: %v", err)
		}

		if token.ID != "rtok_test123" {
			t.Errorf("expected token ID 'rtok_test123', got %q", token.ID)
		}
	})

	t.Run("get runner token - not found", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/admin/api/v1/runner-tokens/rtok_notexist", nil)
		req.SetBasicAuth("admin", "secret")

		rr := httptest.NewRecorder()
		srv.Router().ServeHTTP(rr, req)

		if rr.Code != http.StatusNotFound {
			t.Errorf("expected status %d, got %d", http.StatusNotFound, rr.Code)
		}
	})

	t.Run("create runner token", func(t *testing.T) {
		body := CreateRunnerTokenRequest{
			PoolName: "new-pool",
		}
		bodyBytes, _ := json.Marshal(body)

		req := httptest.NewRequest(http.MethodPost, "/admin/api/v1/runner-tokens", bytes.NewReader(bodyBytes))
		req.SetBasicAuth("admin", "secret")
		req.Header.Set("Content-Type", "application/json")

		rr := httptest.NewRecorder()
		srv.Router().ServeHTTP(rr, req)

		if rr.Code != http.StatusCreated {
			t.Errorf("expected status %d, got %d: %s", http.StatusCreated, rr.Code, rr.Body.String())
		}

		var resp admintypes.CreatedRunnerToken
		if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
			t.Fatalf("failed to decode response: %v", err)
		}

		if resp.Token == nil {
			t.Fatal("expected non-nil token")
		}
		if resp.RawToken == "" {
			t.Error("expected non-empty raw token")
		}
	})

	t.Run("create runner token - empty pool name", func(t *testing.T) {
		body := CreateRunnerTokenRequest{
			PoolName: "",
		}
		bodyBytes, _ := json.Marshal(body)

		req := httptest.NewRequest(http.MethodPost, "/admin/api/v1/runner-tokens", bytes.NewReader(bodyBytes))
		req.SetBasicAuth("admin", "secret")
		req.Header.Set("Content-Type", "application/json")

		rr := httptest.NewRecorder()
		srv.Router().ServeHTTP(rr, req)

		if rr.Code != http.StatusBadRequest {
			t.Errorf("expected status %d, got %d: %s", http.StatusBadRequest, rr.Code, rr.Body.String())
		}
	})

	t.Run("revoke runner token", func(t *testing.T) {
		body := RevokeRunnerTokenRequest{
			Reason: "no longer needed",
		}
		bodyBytes, _ := json.Marshal(body)

		req := httptest.NewRequest(http.MethodDelete, "/admin/api/v1/runner-tokens/rtok_test123", bytes.NewReader(bodyBytes))
		req.SetBasicAuth("admin", "secret")
		req.Header.Set("Content-Type", "application/json")

		rr := httptest.NewRecorder()
		srv.Router().ServeHTTP(rr, req)

		if rr.Code != http.StatusNoContent {
			t.Errorf("expected status %d, got %d: %s", http.StatusNoContent, rr.Code, rr.Body.String())
		}
	})

	t.Run("revoke runner token - not found", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodDelete, "/admin/api/v1/runner-tokens/rtok_notexist", nil)
		req.SetBasicAuth("admin", "secret")

		rr := httptest.NewRecorder()
		srv.Router().ServeHTTP(rr, req)

		if rr.Code != http.StatusNotFound {
			t.Errorf("expected status %d, got %d", http.StatusNotFound, rr.Code)
		}
	})

	t.Run("rotate runner token", func(t *testing.T) {
		// Re-add test token since it was revoked
		mockService.AddToken(&store.RunnerToken{
			ID:          "rtok_torotate",
			TokenPrefix: "rtok_rotate12",
			PoolName:    "default",
			Status:      "active",
			Labels:      json.RawMessage("{}"),
			CreatedAt:   time.Now(),
		})

		req := httptest.NewRequest(http.MethodPost, "/admin/api/v1/runner-tokens/rtok_torotate/rotate", nil)
		req.SetBasicAuth("admin", "secret")

		rr := httptest.NewRecorder()
		srv.Router().ServeHTTP(rr, req)

		if rr.Code != http.StatusOK {
			t.Errorf("expected status %d, got %d: %s", http.StatusOK, rr.Code, rr.Body.String())
		}

		var resp admintypes.CreatedRunnerToken
		if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
			t.Fatalf("failed to decode response: %v", err)
		}

		if resp.Token == nil {
			t.Fatal("expected non-nil token")
		}
		if resp.RawToken == "" {
			t.Error("expected non-empty raw token")
		}
		if resp.Token.Status != "rotating" {
			t.Errorf("expected status 'rotating', got %q", resp.Token.Status)
		}
	})

	t.Run("rotate runner token - not found", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/admin/api/v1/runner-tokens/rtok_notexist/rotate", nil)
		req.SetBasicAuth("admin", "secret")

		rr := httptest.NewRecorder()
		srv.Router().ServeHTTP(rr, req)

		if rr.Code != http.StatusNotFound {
			t.Errorf("expected status %d, got %d", http.StatusNotFound, rr.Code)
		}
	})
}

func TestRunnerTokenHandlers_ServiceNotConfigured(t *testing.T) {
	srv := newTestServer() // No runner token service

	t.Run("list runner tokens - service not configured", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/admin/api/v1/runner-tokens", nil)
		req.SetBasicAuth("admin", "secret")

		rr := httptest.NewRecorder()
		srv.Router().ServeHTTP(rr, req)

		if rr.Code != http.StatusNotImplemented {
			t.Errorf("expected status %d, got %d", http.StatusNotImplemented, rr.Code)
		}
	})

	t.Run("get runner token - service not configured", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/admin/api/v1/runner-tokens/rtok_test", nil)
		req.SetBasicAuth("admin", "secret")

		rr := httptest.NewRecorder()
		srv.Router().ServeHTTP(rr, req)

		if rr.Code != http.StatusNotImplemented {
			t.Errorf("expected status %d, got %d", http.StatusNotImplemented, rr.Code)
		}
	})

	t.Run("create runner token - service not configured", func(t *testing.T) {
		body := CreateRunnerTokenRequest{PoolName: "test"}
		bodyBytes, _ := json.Marshal(body)

		req := httptest.NewRequest(http.MethodPost, "/admin/api/v1/runner-tokens", bytes.NewReader(bodyBytes))
		req.SetBasicAuth("admin", "secret")
		req.Header.Set("Content-Type", "application/json")

		rr := httptest.NewRecorder()
		srv.Router().ServeHTTP(rr, req)

		if rr.Code != http.StatusNotImplemented {
			t.Errorf("expected status %d, got %d", http.StatusNotImplemented, rr.Code)
		}
	})

	t.Run("revoke runner token - service not configured", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodDelete, "/admin/api/v1/runner-tokens/rtok_test", nil)
		req.SetBasicAuth("admin", "secret")

		rr := httptest.NewRecorder()
		srv.Router().ServeHTTP(rr, req)

		if rr.Code != http.StatusNotImplemented {
			t.Errorf("expected status %d, got %d", http.StatusNotImplemented, rr.Code)
		}
	})

	t.Run("rotate runner token - service not configured", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/admin/api/v1/runner-tokens/rtok_test/rotate", nil)
		req.SetBasicAuth("admin", "secret")

		rr := httptest.NewRecorder()
		srv.Router().ServeHTTP(rr, req)

		if rr.Code != http.StatusNotImplemented {
			t.Errorf("expected status %d, got %d", http.StatusNotImplemented, rr.Code)
		}
	})
}

func TestRunnerTokenHandlers_RequiresAuth(t *testing.T) {
	mockService := NewMockRunnerTokenService()
	srv := newTestServer(WithRunnerTokenAdminService(mockService))

	t.Run("list runner tokens - no auth", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/admin/api/v1/runner-tokens", nil)

		rr := httptest.NewRecorder()
		srv.Router().ServeHTTP(rr, req)

		if rr.Code != http.StatusUnauthorized {
			t.Errorf("expected status %d, got %d", http.StatusUnauthorized, rr.Code)
		}
	})

	t.Run("create runner token - no auth", func(t *testing.T) {
		body := CreateRunnerTokenRequest{PoolName: "test"}
		bodyBytes, _ := json.Marshal(body)

		req := httptest.NewRequest(http.MethodPost, "/admin/api/v1/runner-tokens", bytes.NewReader(bodyBytes))
		req.Header.Set("Content-Type", "application/json")

		rr := httptest.NewRecorder()
		srv.Router().ServeHTTP(rr, req)

		if rr.Code != http.StatusUnauthorized {
			t.Errorf("expected status %d, got %d", http.StatusUnauthorized, rr.Code)
		}
	})
}

func TestRunnerTokenHandlers_InternalError(t *testing.T) {
	mockService := NewMockRunnerTokenService()
	srv := newTestServer(WithRunnerTokenAdminService(mockService))

	t.Run("list runner tokens - internal error", func(t *testing.T) {
		mockService.SetInternalError(errors.New("database error"))
		defer mockService.SetInternalError(nil)

		req := httptest.NewRequest(http.MethodGet, "/admin/api/v1/runner-tokens", nil)
		req.SetBasicAuth("admin", "secret")

		rr := httptest.NewRecorder()
		srv.Router().ServeHTTP(rr, req)

		if rr.Code != http.StatusInternalServerError {
			t.Errorf("expected status %d, got %d", http.StatusInternalServerError, rr.Code)
		}
	})

	t.Run("get runner token - internal error", func(t *testing.T) {
		mockService.SetInternalError(errors.New("database error"))
		defer mockService.SetInternalError(nil)

		req := httptest.NewRequest(http.MethodGet, "/admin/api/v1/runner-tokens/rtok_test", nil)
		req.SetBasicAuth("admin", "secret")

		rr := httptest.NewRecorder()
		srv.Router().ServeHTTP(rr, req)

		if rr.Code != http.StatusInternalServerError {
			t.Errorf("expected status %d, got %d", http.StatusInternalServerError, rr.Code)
		}
	})

	t.Run("create runner token - internal error", func(t *testing.T) {
		mockService.SetInternalError(errors.New("database error"))
		defer mockService.SetInternalError(nil)

		body := CreateRunnerTokenRequest{PoolName: "test"}
		bodyBytes, _ := json.Marshal(body)

		req := httptest.NewRequest(http.MethodPost, "/admin/api/v1/runner-tokens", bytes.NewReader(bodyBytes))
		req.SetBasicAuth("admin", "secret")
		req.Header.Set("Content-Type", "application/json")

		rr := httptest.NewRecorder()
		srv.Router().ServeHTTP(rr, req)

		if rr.Code != http.StatusInternalServerError {
			t.Errorf("expected status %d, got %d", http.StatusInternalServerError, rr.Code)
		}
	})

	t.Run("revoke runner token - internal error", func(t *testing.T) {
		mockService.SetInternalError(errors.New("database error"))
		defer mockService.SetInternalError(nil)

		req := httptest.NewRequest(http.MethodDelete, "/admin/api/v1/runner-tokens/rtok_test", nil)
		req.SetBasicAuth("admin", "secret")

		rr := httptest.NewRecorder()
		srv.Router().ServeHTTP(rr, req)

		if rr.Code != http.StatusInternalServerError {
			t.Errorf("expected status %d, got %d", http.StatusInternalServerError, rr.Code)
		}
	})

	t.Run("rotate runner token - internal error", func(t *testing.T) {
		mockService.SetInternalError(errors.New("database error"))
		defer mockService.SetInternalError(nil)

		req := httptest.NewRequest(http.MethodPost, "/admin/api/v1/runner-tokens/rtok_test/rotate", nil)
		req.SetBasicAuth("admin", "secret")

		rr := httptest.NewRecorder()
		srv.Router().ServeHTTP(rr, req)

		if rr.Code != http.StatusInternalServerError {
			t.Errorf("expected status %d, got %d", http.StatusInternalServerError, rr.Code)
		}
	})
}

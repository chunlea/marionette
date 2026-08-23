package admin

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/chunlea/marionette/pkg/server/admin/admintypes"
	"github.com/chunlea/marionette/pkg/store"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func newTestServer(opts ...Option) *Server {
	logger := zap.NewNop()
	cfg := Config{
		Host:     "localhost",
		Port:     8081,
		Username: "admin",
		Password: "secret",
	}
	srv, err := New(cfg, logger, opts...)
	if err != nil {
		panic(err)
	}
	return srv
}

func TestBasicAuthMiddleware(t *testing.T) {
	// Configure with mock service so endpoints return proper responses
	srv := newTestServer(WithAPIKeyService(NewMockAPIKeyService()))

	tests := []struct {
		name           string
		username       string
		password       string
		expectedStatus int
	}{
		{
			name:           "valid credentials",
			username:       "admin",
			password:       "secret",
			expectedStatus: http.StatusOK,
		},
		{
			name:           "invalid username",
			username:       "wrong",
			password:       "secret",
			expectedStatus: http.StatusUnauthorized,
		},
		{
			name:           "invalid password",
			username:       "admin",
			password:       "wrong",
			expectedStatus: http.StatusUnauthorized,
		},
		{
			name:           "no credentials",
			username:       "",
			password:       "",
			expectedStatus: http.StatusUnauthorized,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/admin/api/v1/keys", nil)
			if tt.username != "" || tt.password != "" {
				req.SetBasicAuth(tt.username, tt.password)
			}

			rr := httptest.NewRecorder()
			srv.Router().ServeHTTP(rr, req)

			if rr.Code != tt.expectedStatus {
				t.Errorf("expected status %d, got %d", tt.expectedStatus, rr.Code)
			}
		})
	}
}

func TestAPIKeyHandlers(t *testing.T) {
	mockService := NewMockAPIKeyService()
	srv := newTestServer(WithAPIKeyService(mockService))

	t.Run("create API key", func(t *testing.T) {
		body := CreateAPIKeyRequest{
			Name:   "test-key",
			Scopes: []string{"read", "write"},
		}
		bodyBytes, _ := json.Marshal(body)

		req := httptest.NewRequest(http.MethodPost, "/admin/api/v1/keys/", bytes.NewReader(bodyBytes))
		req.SetBasicAuth("admin", "secret")
		req.Header.Set("Content-Type", "application/json")

		rr := httptest.NewRecorder()
		srv.Router().ServeHTTP(rr, req)

		if rr.Code != http.StatusCreated {
			t.Errorf("expected status %d, got %d: %s", http.StatusCreated, rr.Code, rr.Body.String())
		}

		var resp admintypes.CreatedAPIKey
		if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
			t.Fatalf("failed to decode response: %v", err)
		}

		if resp.Key.Name != "test-key" {
			t.Errorf("expected key name 'test-key', got %q", resp.Key.Name)
		}
		if resp.RawToken == "" {
			t.Error("expected raw token to be set")
		}
	})

	t.Run("create API key - validation error", func(t *testing.T) {
		body := CreateAPIKeyRequest{
			Name: "", // empty name
		}
		bodyBytes, _ := json.Marshal(body)

		req := httptest.NewRequest(http.MethodPost, "/admin/api/v1/keys/", bytes.NewReader(bodyBytes))
		req.SetBasicAuth("admin", "secret")
		req.Header.Set("Content-Type", "application/json")

		rr := httptest.NewRecorder()
		srv.Router().ServeHTTP(rr, req)

		if rr.Code != http.StatusBadRequest {
			t.Errorf("expected status %d, got %d", http.StatusBadRequest, rr.Code)
		}
	})

	t.Run("list API keys", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/admin/api/v1/keys/", nil)
		req.SetBasicAuth("admin", "secret")

		rr := httptest.NewRecorder()
		srv.Router().ServeHTTP(rr, req)

		if rr.Code != http.StatusOK {
			t.Errorf("expected status %d, got %d", http.StatusOK, rr.Code)
		}

		var result ListResult[store.APIKey]
		if err := json.NewDecoder(rr.Body).Decode(&result); err != nil {
			t.Fatalf("failed to decode response: %v", err)
		}
	})

	t.Run("get API key", func(t *testing.T) {
		// First create a key
		mockService.AddKey(&store.APIKey{
			ID:        "key_test123",
			Name:      "get-test-key",
			KeyPrefix: "mk_test1234",
			KeyHash:   "testhash",
			CreatedAt: time.Now(),
		})

		req := httptest.NewRequest(http.MethodGet, "/admin/api/v1/keys/key_test123", nil)
		req.SetBasicAuth("admin", "secret")

		rr := httptest.NewRecorder()
		srv.Router().ServeHTTP(rr, req)

		if rr.Code != http.StatusOK {
			t.Errorf("expected status %d, got %d: %s", http.StatusOK, rr.Code, rr.Body.String())
		}
	})

	t.Run("get API key - not found", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/admin/api/v1/keys/nonexistent", nil)
		req.SetBasicAuth("admin", "secret")

		rr := httptest.NewRecorder()
		srv.Router().ServeHTTP(rr, req)

		if rr.Code != http.StatusNotFound {
			t.Errorf("expected status %d, got %d", http.StatusNotFound, rr.Code)
		}
	})

	t.Run("revoke API key", func(t *testing.T) {
		mockService.AddKey(&store.APIKey{
			ID:        "key_revoke123",
			Name:      "revoke-test-key",
			KeyPrefix: "mk_revoke1234",
			KeyHash:   "revokehash",
			CreatedAt: time.Now(),
		})

		body := RevokeAPIKeyRequest{Reason: "test revocation"}
		bodyBytes, _ := json.Marshal(body)

		req := httptest.NewRequest(http.MethodDelete, "/admin/api/v1/keys/key_revoke123", bytes.NewReader(bodyBytes))
		req.SetBasicAuth("admin", "secret")

		rr := httptest.NewRecorder()
		srv.Router().ServeHTTP(rr, req)

		if rr.Code != http.StatusNoContent {
			t.Errorf("expected status %d, got %d: %s", http.StatusNoContent, rr.Code, rr.Body.String())
		}
	})
}

func TestAgentConfigHandlers(t *testing.T) {
	mockService := NewMockAgentConfigService()
	srv := newTestServer(WithAgentConfigService(mockService))

	t.Run("create agent config", func(t *testing.T) {
		body := CreateAgentConfigRequest{
			Name:   "test-agent",
			Agent:  "claude",
			APIKey: "sk-test-key",
			Model:  "claude-3-opus",
		}
		bodyBytes, _ := json.Marshal(body)

		req := httptest.NewRequest(http.MethodPost, "/admin/api/v1/agent-configs/", bytes.NewReader(bodyBytes))
		req.SetBasicAuth("admin", "secret")
		req.Header.Set("Content-Type", "application/json")

		rr := httptest.NewRecorder()
		srv.Router().ServeHTTP(rr, req)

		if rr.Code != http.StatusCreated {
			t.Errorf("expected status %d, got %d: %s", http.StatusCreated, rr.Code, rr.Body.String())
		}
	})

	t.Run("create agent config - validation error", func(t *testing.T) {
		body := CreateAgentConfigRequest{
			Name: "test-agent-2",
			// Missing required fields
		}
		bodyBytes, _ := json.Marshal(body)

		req := httptest.NewRequest(http.MethodPost, "/admin/api/v1/agent-configs/", bytes.NewReader(bodyBytes))
		req.SetBasicAuth("admin", "secret")
		req.Header.Set("Content-Type", "application/json")

		rr := httptest.NewRecorder()
		srv.Router().ServeHTTP(rr, req)

		if rr.Code != http.StatusBadRequest {
			t.Errorf("expected status %d, got %d", http.StatusBadRequest, rr.Code)
		}
	})

	t.Run("list agent configs", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/admin/api/v1/agent-configs/", nil)
		req.SetBasicAuth("admin", "secret")

		rr := httptest.NewRecorder()
		srv.Router().ServeHTTP(rr, req)

		if rr.Code != http.StatusOK {
			t.Errorf("expected status %d, got %d", http.StatusOK, rr.Code)
		}
	})

	t.Run("get agent config", func(t *testing.T) {
		mockService.AddConfig(&store.AgentConfig{
			ID:    "acfg_test123",
			Name:  "get-test-config",
			Agent: "claude",
		})

		req := httptest.NewRequest(http.MethodGet, "/admin/api/v1/agent-configs/acfg_test123", nil)
		req.SetBasicAuth("admin", "secret")

		rr := httptest.NewRecorder()
		srv.Router().ServeHTTP(rr, req)

		if rr.Code != http.StatusOK {
			t.Errorf("expected status %d, got %d: %s", http.StatusOK, rr.Code, rr.Body.String())
		}
	})

	t.Run("update agent config", func(t *testing.T) {
		mockService.AddConfig(&store.AgentConfig{
			ID:    "acfg_update123",
			Name:  "update-test-config",
			Agent: "claude",
		})

		newName := "updated-name"
		body := UpdateAgentConfigRequest{
			Name: &newName,
		}
		bodyBytes, _ := json.Marshal(body)

		req := httptest.NewRequest(http.MethodPut, "/admin/api/v1/agent-configs/acfg_update123", bytes.NewReader(bodyBytes))
		req.SetBasicAuth("admin", "secret")
		req.Header.Set("Content-Type", "application/json")

		rr := httptest.NewRecorder()
		srv.Router().ServeHTTP(rr, req)

		if rr.Code != http.StatusOK {
			t.Errorf("expected status %d, got %d: %s", http.StatusOK, rr.Code, rr.Body.String())
		}
	})

	t.Run("delete agent config", func(t *testing.T) {
		mockService.AddConfig(&store.AgentConfig{
			ID:    "acfg_delete123",
			Name:  "delete-test-config",
			Agent: "claude",
		})

		req := httptest.NewRequest(http.MethodDelete, "/admin/api/v1/agent-configs/acfg_delete123", nil)
		req.SetBasicAuth("admin", "secret")

		rr := httptest.NewRecorder()
		srv.Router().ServeHTTP(rr, req)

		if rr.Code != http.StatusNoContent {
			t.Errorf("expected status %d, got %d: %s", http.StatusNoContent, rr.Code, rr.Body.String())
		}
	})
}

func TestProviderConfigHandlers(t *testing.T) {
	mockService := NewMockProviderConfigService()
	srv := newTestServer(WithProviderConfigService(mockService))

	t.Run("create provider config", func(t *testing.T) {
		body := CreateProviderConfigRequest{
			Name:     "test-provider",
			Provider: "docker",
		}
		bodyBytes, _ := json.Marshal(body)

		req := httptest.NewRequest(http.MethodPost, "/admin/api/v1/provider-configs/", bytes.NewReader(bodyBytes))
		req.SetBasicAuth("admin", "secret")
		req.Header.Set("Content-Type", "application/json")

		rr := httptest.NewRecorder()
		srv.Router().ServeHTTP(rr, req)

		if rr.Code != http.StatusCreated {
			t.Errorf("expected status %d, got %d: %s", http.StatusCreated, rr.Code, rr.Body.String())
		}
	})

	t.Run("list provider configs", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/admin/api/v1/provider-configs/", nil)
		req.SetBasicAuth("admin", "secret")

		rr := httptest.NewRecorder()
		srv.Router().ServeHTTP(rr, req)

		if rr.Code != http.StatusOK {
			t.Errorf("expected status %d, got %d", http.StatusOK, rr.Code)
		}
	})

	t.Run("get provider config", func(t *testing.T) {
		mockService.AddConfig(&store.ProviderConfig{
			ID:       "pcfg_test123",
			Name:     "get-test-provider",
			Provider: "docker",
		})

		req := httptest.NewRequest(http.MethodGet, "/admin/api/v1/provider-configs/pcfg_test123", nil)
		req.SetBasicAuth("admin", "secret")

		rr := httptest.NewRecorder()
		srv.Router().ServeHTTP(rr, req)

		if rr.Code != http.StatusOK {
			t.Errorf("expected status %d, got %d: %s", http.StatusOK, rr.Code, rr.Body.String())
		}
	})

	t.Run("update provider config", func(t *testing.T) {
		mockService.AddConfig(&store.ProviderConfig{
			ID:       "pcfg_update123",
			Name:     "update-test-provider",
			Provider: "docker",
		})

		newName := "updated-provider"
		body := UpdateProviderConfigRequest{
			Name: &newName,
		}
		bodyBytes, _ := json.Marshal(body)

		req := httptest.NewRequest(http.MethodPut, "/admin/api/v1/provider-configs/pcfg_update123", bytes.NewReader(bodyBytes))
		req.SetBasicAuth("admin", "secret")
		req.Header.Set("Content-Type", "application/json")

		rr := httptest.NewRecorder()
		srv.Router().ServeHTTP(rr, req)

		if rr.Code != http.StatusOK {
			t.Errorf("expected status %d, got %d: %s", http.StatusOK, rr.Code, rr.Body.String())
		}
	})

	t.Run("delete provider config", func(t *testing.T) {
		mockService.AddConfig(&store.ProviderConfig{
			ID:       "pcfg_delete123",
			Name:     "delete-test-provider",
			Provider: "docker",
		})

		req := httptest.NewRequest(http.MethodDelete, "/admin/api/v1/provider-configs/pcfg_delete123", nil)
		req.SetBasicAuth("admin", "secret")

		rr := httptest.NewRecorder()
		srv.Router().ServeHTTP(rr, req)

		if rr.Code != http.StatusNoContent {
			t.Errorf("expected status %d, got %d: %s", http.StatusNoContent, rr.Code, rr.Body.String())
		}
	})
}

func TestRunnerHandlers(t *testing.T) {
	mockService := NewMockRunnerAdminService()
	srv := newTestServer(WithRunnerAdminService(mockService))

	t.Run("spawn runner", func(t *testing.T) {
		body := SpawnRunnerRequest{
			Name:             "test-runner",
			ProviderConfigID: "pcfg_test",
		}
		bodyBytes, _ := json.Marshal(body)

		req := httptest.NewRequest(http.MethodPost, "/admin/api/v1/runners/spawn", bytes.NewReader(bodyBytes))
		req.SetBasicAuth("admin", "secret")
		req.Header.Set("Content-Type", "application/json")

		rr := httptest.NewRecorder()
		srv.Router().ServeHTTP(rr, req)

		if rr.Code != http.StatusCreated {
			t.Errorf("expected status %d, got %d: %s", http.StatusCreated, rr.Code, rr.Body.String())
		}
	})

	t.Run("list runners", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/admin/api/v1/runners/", nil)
		req.SetBasicAuth("admin", "secret")

		rr := httptest.NewRecorder()
		srv.Router().ServeHTTP(rr, req)

		if rr.Code != http.StatusOK {
			t.Errorf("expected status %d, got %d", http.StatusOK, rr.Code)
		}
	})

	t.Run("list runners with filters", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/admin/api/v1/runners/?status=idle,busy&pool_name=test-pool", nil)
		req.SetBasicAuth("admin", "secret")

		rr := httptest.NewRecorder()
		srv.Router().ServeHTTP(rr, req)

		if rr.Code != http.StatusOK {
			t.Errorf("expected status %d, got %d", http.StatusOK, rr.Code)
		}
	})

	t.Run("get runner", func(t *testing.T) {
		mockService.AddRunner(&store.Runner{
			ID:       "run_test123",
			Name:     "get-test-runner",
			Hostname: "test-host",
			Status:   "idle",
		})

		req := httptest.NewRequest(http.MethodGet, "/admin/api/v1/runners/run_test123", nil)
		req.SetBasicAuth("admin", "secret")

		rr := httptest.NewRecorder()
		srv.Router().ServeHTTP(rr, req)

		if rr.Code != http.StatusOK {
			t.Errorf("expected status %d, got %d: %s", http.StatusOK, rr.Code, rr.Body.String())
		}
	})

	t.Run("destroy runner", func(t *testing.T) {
		mockService.AddRunner(&store.Runner{
			ID:       "run_destroy123",
			Name:     "destroy-test-runner",
			Hostname: "test-host",
			Status:   "idle",
		})

		req := httptest.NewRequest(http.MethodDelete, "/admin/api/v1/runners/run_destroy123", nil)
		req.SetBasicAuth("admin", "secret")

		rr := httptest.NewRecorder()
		srv.Router().ServeHTTP(rr, req)

		if rr.Code != http.StatusNoContent {
			t.Errorf("expected status %d, got %d: %s", http.StatusNoContent, rr.Code, rr.Body.String())
		}
	})

	t.Run("destroy busy runner - conflict", func(t *testing.T) {
		mockService.AddRunner(&store.Runner{
			ID:       "run_busy123",
			Name:     "busy-runner",
			Hostname: "test-host",
			Status:   "busy",
		})

		req := httptest.NewRequest(http.MethodDelete, "/admin/api/v1/runners/run_busy123", nil)
		req.SetBasicAuth("admin", "secret")

		rr := httptest.NewRecorder()
		srv.Router().ServeHTTP(rr, req)

		if rr.Code != http.StatusConflict {
			t.Errorf("expected status %d, got %d: %s", http.StatusConflict, rr.Code, rr.Body.String())
		}
	})
}

func TestServiceNotConfigured(t *testing.T) {
	// Server without any services configured
	srv := newTestServer()

	tests := []struct {
		name   string
		method string
		path   string
	}{
		{"create API key without service", http.MethodPost, "/admin/api/v1/keys/"},
		{"list API keys without service", http.MethodGet, "/admin/api/v1/keys/"},
		{"create agent config without service", http.MethodPost, "/admin/api/v1/agent-configs/"},
		{"list agent configs without service", http.MethodGet, "/admin/api/v1/agent-configs/"},
		{"create provider config without service", http.MethodPost, "/admin/api/v1/provider-configs/"},
		{"list provider configs without service", http.MethodGet, "/admin/api/v1/provider-configs/"},
		{"spawn runner without service", http.MethodPost, "/admin/api/v1/runners/spawn"},
		{"list runners without service", http.MethodGet, "/admin/api/v1/runners/"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(tt.method, tt.path, nil)
			req.SetBasicAuth("admin", "secret")

			rr := httptest.NewRecorder()
			srv.Router().ServeHTTP(rr, req)

			if rr.Code != http.StatusNotImplemented {
				t.Errorf("expected status %d, got %d: %s", http.StatusNotImplemented, rr.Code, rr.Body.String())
			}
		})
	}
}

func TestParseLabels(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected map[string]string
	}{
		{
			name:     "empty string",
			input:    "",
			expected: nil,
		},
		{
			name:     "single label",
			input:    "env=prod",
			expected: map[string]string{"env": "prod"},
		},
		{
			name:     "multiple labels",
			input:    "env=prod,team=backend",
			expected: map[string]string{"env": "prod", "team": "backend"},
		},
		{
			name:     "with spaces",
			input:    "env = prod , team = backend",
			expected: map[string]string{"env": "prod", "team": "backend"},
		},
		{
			name:     "invalid format (no equals)",
			input:    "envprod",
			expected: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := parseLabels(tt.input)
			if len(result) != len(tt.expected) {
				t.Errorf("expected %d labels, got %d", len(tt.expected), len(result))
				return
			}
			for k, v := range tt.expected {
				if result[k] != v {
					t.Errorf("expected label %q=%q, got %q", k, v, result[k])
				}
			}
		})
	}
}

func TestParseLimit(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected int
	}{
		{"empty string", "", 50},
		{"valid limit", "25", 25},
		{"zero", "0", 50},
		{"negative", "-5", 50},
		{"over max", "200", 100},
		{"invalid", "abc", 50},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := parseLimit(tt.input)
			if result != tt.expected {
				t.Errorf("expected %d, got %d", tt.expected, result)
			}
		})
	}
}

func TestHelpers(t *testing.T) {
	t.Run("logError", func(t *testing.T) {
		err := logError(store.ErrNotFound)
		if err.Key != "error" {
			t.Errorf("expected key 'error', got %q", err.Key)
		}
	})

	t.Run("matchLabelsJSON - empty required", func(t *testing.T) {
		result := matchLabelsJSON(json.RawMessage(`{"a":"b"}`), map[string]string{})
		if !result {
			t.Error("expected true for empty required labels")
		}
	})

	t.Run("matchLabelsJSON - match found", func(t *testing.T) {
		result := matchLabelsJSON(json.RawMessage(`{"env":"prod","team":"backend"}`), map[string]string{"env": "prod"})
		if !result {
			t.Error("expected true when label matches")
		}
	})

	t.Run("matchLabelsJSON - match not found", func(t *testing.T) {
		result := matchLabelsJSON(json.RawMessage(`{"env":"prod"}`), map[string]string{"env": "staging"})
		if result {
			t.Error("expected false when label doesn't match")
		}
	})

	t.Run("matchLabelsJSON - invalid JSON", func(t *testing.T) {
		result := matchLabelsJSON(json.RawMessage(`invalid`), map[string]string{"env": "prod"})
		if result {
			t.Error("expected false for invalid JSON")
		}
	})

	t.Run("matchLabelsJSON - nil or empty JSON", func(t *testing.T) {
		result := matchLabelsJSON(nil, map[string]string{"env": "prod"})
		if result {
			t.Error("expected false for nil JSON with required labels")
		}

		result = matchLabelsJSON(json.RawMessage(`{}`), map[string]string{})
		if !result {
			t.Error("expected true for empty JSON with no required labels")
		}
	})

	t.Run("ValidationError", func(t *testing.T) {
		err1 := &ValidationError{Field: "name", Message: "is required"}
		if err1.Error() != "name: is required" {
			t.Errorf("unexpected error string: %s", err1.Error())
		}

		err2 := &ValidationError{Message: "validation failed"}
		if err2.Error() != "validation failed" {
			t.Errorf("unexpected error string: %s", err2.Error())
		}
	})

	t.Run("InvalidStateError", func(t *testing.T) {
		err := &InvalidStateError{
			Resource: "runner",
			ID:       "run_123",
			Current:  "busy",
			Expected: "idle",
		}
		if err.Error() != "runner run_123 is in state busy, expected idle" {
			t.Errorf("unexpected error string: %s", err.Error())
		}

		if !IsInvalidState(err) {
			t.Error("expected IsInvalidState to return true")
		}

		if IsInvalidState(store.ErrNotFound) {
			t.Error("expected IsInvalidState to return false for other error")
		}
	})

	t.Run("IsValidation", func(t *testing.T) {
		if !IsValidation(&ValidationError{}) {
			t.Error("expected IsValidation to return true")
		}
		if IsValidation(store.ErrNotFound) {
			t.Error("expected IsValidation to return false for other error")
		}
	})
}

func TestStaticHandler(t *testing.T) {
	srv := newTestServer()

	req := httptest.NewRequest(http.MethodGet, "/some/unknown/path", nil)
	rr := httptest.NewRecorder()
	srv.Router().ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("expected status %d, got %d", http.StatusOK, rr.Code)
	}
}

func TestWriteJSONNil(t *testing.T) {
	rr := httptest.NewRecorder()
	WriteJSON(rr, http.StatusNoContent, nil)

	if rr.Code != http.StatusNoContent {
		t.Errorf("expected status %d, got %d", http.StatusNoContent, rr.Code)
	}

	if rr.Body.Len() != 0 {
		t.Errorf("expected empty body, got %s", rr.Body.String())
	}
}

func TestGetRunnerNotFound(t *testing.T) {
	srv := newTestServer(WithRunnerAdminService(NewMockRunnerAdminService()))

	req := httptest.NewRequest(http.MethodGet, "/admin/api/v1/runners/nonexistent", nil)
	req.SetBasicAuth("admin", "secret")

	rr := httptest.NewRecorder()
	srv.Router().ServeHTTP(rr, req)

	if rr.Code != http.StatusNotFound {
		t.Errorf("expected status %d, got %d: %s", http.StatusNotFound, rr.Code, rr.Body.String())
	}
}

func TestDestroyRunnerNotFound(t *testing.T) {
	srv := newTestServer(WithRunnerAdminService(NewMockRunnerAdminService()))

	req := httptest.NewRequest(http.MethodDelete, "/admin/api/v1/runners/nonexistent", nil)
	req.SetBasicAuth("admin", "secret")

	rr := httptest.NewRecorder()
	srv.Router().ServeHTTP(rr, req)

	if rr.Code != http.StatusNotFound {
		t.Errorf("expected status %d, got %d: %s", http.StatusNotFound, rr.Code, rr.Body.String())
	}
}

func TestRevokeAPIKeyNotFound(t *testing.T) {
	srv := newTestServer(WithAPIKeyService(NewMockAPIKeyService()))

	req := httptest.NewRequest(http.MethodDelete, "/admin/api/v1/keys/nonexistent", nil)
	req.SetBasicAuth("admin", "secret")

	rr := httptest.NewRecorder()
	srv.Router().ServeHTTP(rr, req)

	if rr.Code != http.StatusNotFound {
		t.Errorf("expected status %d, got %d: %s", http.StatusNotFound, rr.Code, rr.Body.String())
	}
}

func TestNotFoundErrors(t *testing.T) {
	srv := newTestServer(
		WithAgentConfigService(NewMockAgentConfigService()),
		WithProviderConfigService(NewMockProviderConfigService()),
	)

	tests := []struct {
		name   string
		method string
		path   string
	}{
		{"get agent config not found", http.MethodGet, "/admin/api/v1/agent-configs/nonexistent"},
		{"update agent config not found", http.MethodPut, "/admin/api/v1/agent-configs/nonexistent"},
		{"delete agent config not found", http.MethodDelete, "/admin/api/v1/agent-configs/nonexistent"},
		{"get provider config not found", http.MethodGet, "/admin/api/v1/provider-configs/nonexistent"},
		{"update provider config not found", http.MethodPut, "/admin/api/v1/provider-configs/nonexistent"},
		{"delete provider config not found", http.MethodDelete, "/admin/api/v1/provider-configs/nonexistent"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var body []byte
			if tt.method == http.MethodPut {
				body = []byte(`{"name": "test"}`)
			}
			req := httptest.NewRequest(tt.method, tt.path, bytes.NewReader(body))
			req.SetBasicAuth("admin", "secret")
			if body != nil {
				req.Header.Set("Content-Type", "application/json")
			}

			rr := httptest.NewRecorder()
			srv.Router().ServeHTTP(rr, req)

			if rr.Code != http.StatusNotFound {
				t.Errorf("expected status %d, got %d: %s", http.StatusNotFound, rr.Code, rr.Body.String())
			}
		})
	}
}

func TestInvalidJSONBody(t *testing.T) {
	srv := newTestServer(
		WithAPIKeyService(NewMockAPIKeyService()),
		WithAgentConfigService(NewMockAgentConfigService()),
		WithProviderConfigService(NewMockProviderConfigService()),
		WithRunnerAdminService(NewMockRunnerAdminService()),
	)

	tests := []struct {
		name string
		path string
	}{
		{"create API key", "/admin/api/v1/keys/"},
		{"create agent config", "/admin/api/v1/agent-configs/"},
		{"create provider config", "/admin/api/v1/provider-configs/"},
		{"spawn runner", "/admin/api/v1/runners/spawn"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, tt.path, bytes.NewReader([]byte("invalid json")))
			req.SetBasicAuth("admin", "secret")
			req.Header.Set("Content-Type", "application/json")

			rr := httptest.NewRecorder()
			srv.Router().ServeHTTP(rr, req)

			if rr.Code != http.StatusBadRequest {
				t.Errorf("expected status %d, got %d: %s", http.StatusBadRequest, rr.Code, rr.Body.String())
			}
		})
	}
}

func TestUpdateInvalidJSONBody(t *testing.T) {
	mockAgent := NewMockAgentConfigService()
	mockProvider := NewMockProviderConfigService()
	srv := newTestServer(
		WithAgentConfigService(mockAgent),
		WithProviderConfigService(mockProvider),
	)

	// Add configs first
	mockAgent.AddConfig(&store.AgentConfig{ID: "acfg_test123", Name: "test", Agent: "claude"})
	mockProvider.AddConfig(&store.ProviderConfig{ID: "pcfg_test123", Name: "test", Provider: "docker"})

	tests := []struct {
		name string
		path string
	}{
		{"update agent config", "/admin/api/v1/agent-configs/acfg_test123"},
		{"update provider config", "/admin/api/v1/provider-configs/pcfg_test123"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPut, tt.path, bytes.NewReader([]byte("invalid json")))
			req.SetBasicAuth("admin", "secret")
			req.Header.Set("Content-Type", "application/json")

			rr := httptest.NewRecorder()
			srv.Router().ServeHTTP(rr, req)

			if rr.Code != http.StatusBadRequest {
				t.Errorf("expected status %d, got %d: %s", http.StatusBadRequest, rr.Code, rr.Body.String())
			}
		})
	}
}

func TestProviderConfigValidationError(t *testing.T) {
	mockService := NewMockProviderConfigService()
	srv := newTestServer(WithProviderConfigService(mockService))

	// Test create with missing required fields
	body := CreateProviderConfigRequest{
		Name: "test",
		// Missing Provider field
	}
	bodyBytes, _ := json.Marshal(body)

	req := httptest.NewRequest(http.MethodPost, "/admin/api/v1/provider-configs/", bytes.NewReader(bodyBytes))
	req.SetBasicAuth("admin", "secret")
	req.Header.Set("Content-Type", "application/json")

	rr := httptest.NewRecorder()
	srv.Router().ServeHTTP(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Errorf("expected status %d, got %d: %s", http.StatusBadRequest, rr.Code, rr.Body.String())
	}
}

func TestSpawnRunnerValidationError(t *testing.T) {
	mockService := NewMockRunnerAdminService()
	srv := newTestServer(WithRunnerAdminService(mockService))

	// Set validation error to be returned on spawn
	mockService.SetValidationError("provider_config_id", "provider config not found")

	body := SpawnRunnerRequest{
		Name:             "test-runner",
		ProviderConfigID: "invalid_pcfg",
	}
	bodyBytes, _ := json.Marshal(body)

	req := httptest.NewRequest(http.MethodPost, "/admin/api/v1/runners/spawn", bytes.NewReader(bodyBytes))
	req.SetBasicAuth("admin", "secret")
	req.Header.Set("Content-Type", "application/json")

	rr := httptest.NewRecorder()
	srv.Router().ServeHTTP(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Errorf("expected status %d, got %d: %s", http.StatusBadRequest, rr.Code, rr.Body.String())
	}
}

func TestListWithFiltersAndPagination(t *testing.T) {
	t.Run("list API keys with filters", func(t *testing.T) {
		mockService := NewMockAPIKeyService()
		mockService.AddKey(&store.APIKey{
			ID:     "key_test1",
			Name:   "test-key-1",
			Labels: json.RawMessage(`{"env":"prod"}`),
		})
		srv := newTestServer(WithAPIKeyService(mockService))

		req := httptest.NewRequest(http.MethodGet, "/admin/api/v1/keys/?labels=env%3Dprod&limit=10&cursor=abc", nil)
		req.SetBasicAuth("admin", "secret")

		rr := httptest.NewRecorder()
		srv.Router().ServeHTTP(rr, req)

		if rr.Code != http.StatusOK {
			t.Errorf("expected status %d, got %d: %s", http.StatusOK, rr.Code, rr.Body.String())
		}
	})

	t.Run("list agent configs with filters", func(t *testing.T) {
		mockService := NewMockAgentConfigService()
		mockService.AddConfig(&store.AgentConfig{
			ID:    "acfg_test1",
			Name:  "test-config",
			Agent: "claude",
		})
		srv := newTestServer(WithAgentConfigService(mockService))

		req := httptest.NewRequest(http.MethodGet, "/admin/api/v1/agent-configs/?agent=claude&limit=10", nil)
		req.SetBasicAuth("admin", "secret")

		rr := httptest.NewRecorder()
		srv.Router().ServeHTTP(rr, req)

		if rr.Code != http.StatusOK {
			t.Errorf("expected status %d, got %d: %s", http.StatusOK, rr.Code, rr.Body.String())
		}
	})

	t.Run("list provider configs with filters", func(t *testing.T) {
		mockService := NewMockProviderConfigService()
		mockService.AddConfig(&store.ProviderConfig{
			ID:       "pcfg_test1",
			Name:     "test-config",
			Provider: "docker",
		})
		srv := newTestServer(WithProviderConfigService(mockService))

		req := httptest.NewRequest(http.MethodGet, "/admin/api/v1/provider-configs/?provider=docker&limit=10", nil)
		req.SetBasicAuth("admin", "secret")

		rr := httptest.NewRecorder()
		srv.Router().ServeHTTP(rr, req)

		if rr.Code != http.StatusOK {
			t.Errorf("expected status %d, got %d: %s", http.StatusOK, rr.Code, rr.Body.String())
		}
	})
}

func TestUpdateConfigValidationErrors(t *testing.T) {
	t.Run("update agent config validation error", func(t *testing.T) {
		mockService := NewMockAgentConfigService()
		mockService.AddConfig(&store.AgentConfig{
			ID:    "acfg_test123",
			Name:  "existing-config",
			Agent: "claude",
		})
		// Set up to return validation error
		mockService.SetValidationError("name", "already exists")
		srv := newTestServer(WithAgentConfigService(mockService))

		newName := "updated-name"
		body := UpdateAgentConfigRequest{Name: &newName}
		bodyBytes, _ := json.Marshal(body)

		req := httptest.NewRequest(http.MethodPut, "/admin/api/v1/agent-configs/acfg_test123", bytes.NewReader(bodyBytes))
		req.SetBasicAuth("admin", "secret")
		req.Header.Set("Content-Type", "application/json")

		rr := httptest.NewRecorder()
		srv.Router().ServeHTTP(rr, req)

		if rr.Code != http.StatusBadRequest {
			t.Errorf("expected status %d, got %d: %s", http.StatusBadRequest, rr.Code, rr.Body.String())
		}
		mockService.ClearValidationError()
	})

	t.Run("update provider config validation error", func(t *testing.T) {
		mockService := NewMockProviderConfigService()
		mockService.AddConfig(&store.ProviderConfig{
			ID:       "pcfg_test123",
			Name:     "existing-config",
			Provider: "docker",
		})
		// Set up to return validation error
		mockService.SetValidationError("name", "already exists")
		srv := newTestServer(WithProviderConfigService(mockService))

		newName := "updated-name"
		body := UpdateProviderConfigRequest{Name: &newName}
		bodyBytes, _ := json.Marshal(body)

		req := httptest.NewRequest(http.MethodPut, "/admin/api/v1/provider-configs/pcfg_test123", bytes.NewReader(bodyBytes))
		req.SetBasicAuth("admin", "secret")
		req.Header.Set("Content-Type", "application/json")

		rr := httptest.NewRecorder()
		srv.Router().ServeHTTP(rr, req)

		if rr.Code != http.StatusBadRequest {
			t.Errorf("expected status %d, got %d: %s", http.StatusBadRequest, rr.Code, rr.Body.String())
		}
		mockService.ClearValidationError()
	})
}

func TestStaticHandlerPaths(t *testing.T) {
	srv := newTestServer()

	tests := []struct {
		path string
	}{
		{"/"},
		{"/dashboard"},
		{"/admin"},
		{"/some/nested/path"},
	}

	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, tt.path, nil)
			rr := httptest.NewRecorder()
			srv.Router().ServeHTTP(rr, req)

			if rr.Code != http.StatusOK {
				t.Errorf("expected status %d, got %d for path %s", http.StatusOK, rr.Code, tt.path)
			}
		})
	}
}

func TestHealthEndpoints(t *testing.T) {
	srv := newTestServer()

	tests := []struct {
		path string
	}{
		{"/health"},
		{"/healthz"},
	}

	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, tt.path, nil)
			rr := httptest.NewRecorder()
			srv.Router().ServeHTTP(rr, req)

			if rr.Code != http.StatusOK {
				t.Errorf("expected status %d, got %d", http.StatusOK, rr.Code)
			}
		})
	}
}

func TestStatusEndpoint(t *testing.T) {
	srv := newTestServer()

	req := httptest.NewRequest(http.MethodGet, "/admin/api/status", nil)
	req.SetBasicAuth("admin", "secret")
	rr := httptest.NewRecorder()
	srv.Router().ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("expected status %d, got %d", http.StatusOK, rr.Code)
	}
}

func TestAgentConfigWithAllOptions(t *testing.T) {
	mockService := NewMockAgentConfigService()
	srv := newTestServer(WithAgentConfigService(mockService))

	// Create with all optional fields
	body := CreateAgentConfigRequest{
		Name:      "full-config",
		Agent:     "claude",
		APIKey:    "sk-test",
		Model:     "claude-3-opus",
		BaseURL:   "https://api.anthropic.com",
		Extra:     map[string]any{"timeout": 30},
		IsDefault: true,
		Labels:    map[string]string{"env": "prod"},
	}
	bodyBytes, _ := json.Marshal(body)

	req := httptest.NewRequest(http.MethodPost, "/admin/api/v1/agent-configs/", bytes.NewReader(bodyBytes))
	req.SetBasicAuth("admin", "secret")
	req.Header.Set("Content-Type", "application/json")

	rr := httptest.NewRecorder()
	srv.Router().ServeHTTP(rr, req)

	if rr.Code != http.StatusCreated {
		t.Errorf("expected status %d, got %d: %s", http.StatusCreated, rr.Code, rr.Body.String())
	}
}

func TestProviderConfigWithAllOptions(t *testing.T) {
	mockService := NewMockProviderConfigService()
	srv := newTestServer(WithProviderConfigService(mockService))

	// Create with all optional fields
	body := CreateProviderConfigRequest{
		Name:          "full-config",
		Provider:      "docker",
		Config:        map[string]any{"image": "marionette/agent:latest"},
		SuspendConfig: map[string]any{"strategy": "pause"},
		IsDefault:     true,
		Labels:        map[string]string{"env": "prod"},
	}
	bodyBytes, _ := json.Marshal(body)

	req := httptest.NewRequest(http.MethodPost, "/admin/api/v1/provider-configs/", bytes.NewReader(bodyBytes))
	req.SetBasicAuth("admin", "secret")
	req.Header.Set("Content-Type", "application/json")

	rr := httptest.NewRecorder()
	srv.Router().ServeHTTP(rr, req)

	if rr.Code != http.StatusCreated {
		t.Errorf("expected status %d, got %d: %s", http.StatusCreated, rr.Code, rr.Body.String())
	}
}

func TestSpawnRunnerWithAllOptions(t *testing.T) {
	mockService := NewMockRunnerAdminService()
	srv := newTestServer(WithRunnerAdminService(mockService))

	// Spawn with all optional fields
	body := SpawnRunnerRequest{
		Name:             "full-runner",
		ProviderConfigID: "pcfg_test",
		ProfileID:        "prof_test",
		Labels:           map[string]string{"env": "prod"},
	}
	bodyBytes, _ := json.Marshal(body)

	req := httptest.NewRequest(http.MethodPost, "/admin/api/v1/runners/spawn", bytes.NewReader(bodyBytes))
	req.SetBasicAuth("admin", "secret")
	req.Header.Set("Content-Type", "application/json")

	rr := httptest.NewRecorder()
	srv.Router().ServeHTTP(rr, req)

	if rr.Code != http.StatusCreated {
		t.Errorf("expected status %d, got %d: %s", http.StatusCreated, rr.Code, rr.Body.String())
	}
}

func TestAPIKeyWithAllOptions(t *testing.T) {
	mockService := NewMockAPIKeyService()
	srv := newTestServer(WithAPIKeyService(mockService))

	expiresAt := time.Now().Add(24 * time.Hour)
	// Create with all optional fields
	body := CreateAPIKeyRequest{
		Name:        "full-key",
		Scopes:      []string{"read", "write"},
		Labels:      map[string]string{"env": "prod"},
		Annotations: map[string]string{"note": "test key"},
		ExpiresAt:   &expiresAt,
	}
	bodyBytes, _ := json.Marshal(body)

	req := httptest.NewRequest(http.MethodPost, "/admin/api/v1/keys/", bytes.NewReader(bodyBytes))
	req.SetBasicAuth("admin", "secret")
	req.Header.Set("Content-Type", "application/json")

	rr := httptest.NewRecorder()
	srv.Router().ServeHTTP(rr, req)

	if rr.Code != http.StatusCreated {
		t.Errorf("expected status %d, got %d: %s", http.StatusCreated, rr.Code, rr.Body.String())
	}
}

func TestServerRouter(t *testing.T) {
	srv := newTestServer()
	router := srv.Router()
	if router == nil {
		t.Error("expected router to be non-nil")
	}
}

func TestWriteError(t *testing.T) {
	rr := httptest.NewRecorder()
	WriteError(rr, http.StatusBadRequest, "test_error", "test message")

	if rr.Code != http.StatusBadRequest {
		t.Errorf("expected status %d, got %d", http.StatusBadRequest, rr.Code)
	}

	var resp ErrorResponse
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if resp.Code != "test_error" {
		t.Errorf("expected code 'test_error', got %q", resp.Code)
	}
	if resp.Message != "test message" {
		t.Errorf("expected message 'test message', got %q", resp.Message)
	}
}

func TestAgentConfigNotFoundErrors(t *testing.T) {
	mockService := NewMockAgentConfigService()
	srv := newTestServer(WithAgentConfigService(mockService))

	t.Run("get not found", func(_ *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/admin/api/v1/agent-configs/acfg_nonexistent", nil)
		req.SetBasicAuth("admin", "secret")
		rr := httptest.NewRecorder()
		srv.Router().ServeHTTP(rr, req)
		// Not found is expected
	})

	t.Run("update not found", func(_ *testing.T) {
		body := UpdateAgentConfigRequest{
			Name: strPtr("new-name"),
		}
		bodyBytes, _ := json.Marshal(body)
		req := httptest.NewRequest(http.MethodPut, "/admin/api/v1/agent-configs/acfg_nonexistent", bytes.NewReader(bodyBytes))
		req.SetBasicAuth("admin", "secret")
		req.Header.Set("Content-Type", "application/json")
		rr := httptest.NewRecorder()
		srv.Router().ServeHTTP(rr, req)
		// Not found is expected
	})

	t.Run("delete not found", func(_ *testing.T) {
		req := httptest.NewRequest(http.MethodDelete, "/admin/api/v1/agent-configs/acfg_nonexistent", nil)
		req.SetBasicAuth("admin", "secret")
		rr := httptest.NewRecorder()
		srv.Router().ServeHTTP(rr, req)
		// Not found is expected
	})
}

func TestProviderConfigNotFoundErrors(t *testing.T) {
	mockService := NewMockProviderConfigService()
	srv := newTestServer(WithProviderConfigService(mockService))

	t.Run("get not found", func(_ *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/admin/api/v1/provider-configs/pcfg_nonexistent", nil)
		req.SetBasicAuth("admin", "secret")
		rr := httptest.NewRecorder()
		srv.Router().ServeHTTP(rr, req)
		// Not found is expected
	})

	t.Run("update not found", func(_ *testing.T) {
		body := UpdateProviderConfigRequest{
			Name: strPtr("new-name"),
		}
		bodyBytes, _ := json.Marshal(body)
		req := httptest.NewRequest(http.MethodPut, "/admin/api/v1/provider-configs/pcfg_nonexistent", bytes.NewReader(bodyBytes))
		req.SetBasicAuth("admin", "secret")
		req.Header.Set("Content-Type", "application/json")
		rr := httptest.NewRecorder()
		srv.Router().ServeHTTP(rr, req)
		// Not found is expected
	})

	t.Run("delete not found", func(_ *testing.T) {
		req := httptest.NewRequest(http.MethodDelete, "/admin/api/v1/provider-configs/pcfg_nonexistent", nil)
		req.SetBasicAuth("admin", "secret")
		rr := httptest.NewRecorder()
		srv.Router().ServeHTTP(rr, req)
		// Not found is expected
	})
}

func TestAPIKeyNotFoundErrors(t *testing.T) {
	mockService := NewMockAPIKeyService()
	srv := newTestServer(WithAPIKeyService(mockService))

	t.Run("get not found", func(_ *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/admin/api/v1/keys/key_nonexistent", nil)
		req.SetBasicAuth("admin", "secret")
		rr := httptest.NewRecorder()
		srv.Router().ServeHTTP(rr, req)
		// Not found is expected
	})

	t.Run("revoke not found", func(_ *testing.T) {
		req := httptest.NewRequest(http.MethodDelete, "/admin/api/v1/keys/key_nonexistent", nil)
		req.SetBasicAuth("admin", "secret")
		rr := httptest.NewRecorder()
		srv.Router().ServeHTTP(rr, req)
		// Not found is expected
	})
}

func TestRunnerNotFoundErrors(t *testing.T) {
	mockService := NewMockRunnerAdminService()
	srv := newTestServer(WithRunnerAdminService(mockService))

	t.Run("get not found", func(_ *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/admin/api/v1/runners/run_nonexistent", nil)
		req.SetBasicAuth("admin", "secret")
		rr := httptest.NewRecorder()
		srv.Router().ServeHTTP(rr, req)
		// Not found is expected
	})

	t.Run("destroy not found", func(_ *testing.T) {
		req := httptest.NewRequest(http.MethodDelete, "/admin/api/v1/runners/run_nonexistent", nil)
		req.SetBasicAuth("admin", "secret")
		rr := httptest.NewRecorder()
		srv.Router().ServeHTTP(rr, req)
		// Not found is expected
	})
}

func TestAgentConfigValidationErrors(t *testing.T) {
	mockService := NewMockAgentConfigService()
	srv := newTestServer(WithAgentConfigService(mockService))

	// Create a config first
	body := CreateAgentConfigRequest{
		Name:   "test-config",
		Agent:  "claude",
		APIKey: "test-key",
	}
	bodyBytes, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, "/admin/api/v1/agent-configs", bytes.NewReader(bodyBytes))
	req.SetBasicAuth("admin", "secret")
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	srv.Router().ServeHTTP(rr, req)

	var created store.AgentConfig
	_ = json.NewDecoder(rr.Body).Decode(&created)

	t.Run("update with validation error", func(t *testing.T) {
		mockService.SetValidationError("name", "name is invalid")
		updateBody := UpdateAgentConfigRequest{
			Name: strPtr("invalid!name"),
		}
		updateBytes, _ := json.Marshal(updateBody)
		req := httptest.NewRequest(http.MethodPut, "/admin/api/v1/agent-configs/"+created.ID, bytes.NewReader(updateBytes))
		req.SetBasicAuth("admin", "secret")
		req.Header.Set("Content-Type", "application/json")
		rr := httptest.NewRecorder()
		srv.Router().ServeHTTP(rr, req)

		if rr.Code != http.StatusBadRequest {
			t.Errorf("expected status %d, got %d: %s", http.StatusBadRequest, rr.Code, rr.Body.String())
		}
	})
}

func TestProviderConfigValidationErrors(t *testing.T) {
	mockService := NewMockProviderConfigService()
	srv := newTestServer(WithProviderConfigService(mockService))

	// Create a config first
	body := CreateProviderConfigRequest{
		Name:     "test-config",
		Provider: "docker",
	}
	bodyBytes, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, "/admin/api/v1/provider-configs", bytes.NewReader(bodyBytes))
	req.SetBasicAuth("admin", "secret")
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	srv.Router().ServeHTTP(rr, req)

	var created store.ProviderConfig
	_ = json.NewDecoder(rr.Body).Decode(&created)

	t.Run("update with validation error", func(t *testing.T) {
		mockService.SetValidationError("name", "name is invalid")
		updateBody := UpdateProviderConfigRequest{
			Name: strPtr("invalid!name"),
		}
		updateBytes, _ := json.Marshal(updateBody)
		req := httptest.NewRequest(http.MethodPut, "/admin/api/v1/provider-configs/"+created.ID, bytes.NewReader(updateBytes))
		req.SetBasicAuth("admin", "secret")
		req.Header.Set("Content-Type", "application/json")
		rr := httptest.NewRecorder()
		srv.Router().ServeHTTP(rr, req)

		if rr.Code != http.StatusBadRequest {
			t.Errorf("expected status %d, got %d: %s", http.StatusBadRequest, rr.Code, rr.Body.String())
		}
	})
}

func TestDeleteSuccessPaths(t *testing.T) {
	t.Run("delete agent config success", func(t *testing.T) {
		mockService := NewMockAgentConfigService()
		srv := newTestServer(WithAgentConfigService(mockService))

		// Create a config first
		body := CreateAgentConfigRequest{
			Name:   "to-delete",
			Agent:  "claude",
			APIKey: "test-key",
		}
		bodyBytes, _ := json.Marshal(body)
		req := httptest.NewRequest(http.MethodPost, "/admin/api/v1/agent-configs", bytes.NewReader(bodyBytes))
		req.SetBasicAuth("admin", "secret")
		req.Header.Set("Content-Type", "application/json")
		rr := httptest.NewRecorder()
		srv.Router().ServeHTTP(rr, req)

		var created store.AgentConfig
		_ = json.NewDecoder(rr.Body).Decode(&created)

		// Now delete it
		req = httptest.NewRequest(http.MethodDelete, "/admin/api/v1/agent-configs/"+created.ID, nil)
		req.SetBasicAuth("admin", "secret")
		rr = httptest.NewRecorder()
		srv.Router().ServeHTTP(rr, req)

		if rr.Code != http.StatusNoContent {
			t.Errorf("expected status %d, got %d", http.StatusNoContent, rr.Code)
		}
	})

	t.Run("delete provider config success", func(t *testing.T) {
		mockService := NewMockProviderConfigService()
		srv := newTestServer(WithProviderConfigService(mockService))

		// Create a config first
		body := CreateProviderConfigRequest{
			Name:     "to-delete",
			Provider: "docker",
		}
		bodyBytes, _ := json.Marshal(body)
		req := httptest.NewRequest(http.MethodPost, "/admin/api/v1/provider-configs", bytes.NewReader(bodyBytes))
		req.SetBasicAuth("admin", "secret")
		req.Header.Set("Content-Type", "application/json")
		rr := httptest.NewRecorder()
		srv.Router().ServeHTTP(rr, req)

		var created store.ProviderConfig
		_ = json.NewDecoder(rr.Body).Decode(&created)

		// Now delete it
		req = httptest.NewRequest(http.MethodDelete, "/admin/api/v1/provider-configs/"+created.ID, nil)
		req.SetBasicAuth("admin", "secret")
		rr = httptest.NewRecorder()
		srv.Router().ServeHTTP(rr, req)

		if rr.Code != http.StatusNoContent {
			t.Errorf("expected status %d, got %d", http.StatusNoContent, rr.Code)
		}
	})

	t.Run("destroy runner success", func(t *testing.T) {
		mockService := NewMockRunnerAdminService()
		srv := newTestServer(WithRunnerAdminService(mockService))

		// Add a runner directly to the mock
		mockService.AddRunner(&store.Runner{
			ID:       "run_to_delete",
			Name:     "to-delete",
			Hostname: "host",
			Status:   "idle",
		})

		// Now destroy it
		req := httptest.NewRequest(http.MethodDelete, "/admin/api/v1/runners/run_to_delete", nil)
		req.SetBasicAuth("admin", "secret")
		rr := httptest.NewRecorder()
		srv.Router().ServeHTTP(rr, req)

		if rr.Code != http.StatusNoContent {
			t.Errorf("expected status %d, got %d", http.StatusNoContent, rr.Code)
		}
	})
}

func TestUpdateSuccessPaths(t *testing.T) {
	t.Run("update agent config success", func(t *testing.T) {
		mockService := NewMockAgentConfigService()
		srv := newTestServer(WithAgentConfigService(mockService))

		// Create a config first
		body := CreateAgentConfigRequest{
			Name:   "to-update",
			Agent:  "claude",
			APIKey: "test-key",
		}
		bodyBytes, _ := json.Marshal(body)
		req := httptest.NewRequest(http.MethodPost, "/admin/api/v1/agent-configs", bytes.NewReader(bodyBytes))
		req.SetBasicAuth("admin", "secret")
		req.Header.Set("Content-Type", "application/json")
		rr := httptest.NewRecorder()
		srv.Router().ServeHTTP(rr, req)

		var created store.AgentConfig
		_ = json.NewDecoder(rr.Body).Decode(&created)

		// Now update it
		updateBody := UpdateAgentConfigRequest{
			Name: strPtr("updated-name"),
		}
		updateBytes, _ := json.Marshal(updateBody)
		req = httptest.NewRequest(http.MethodPut, "/admin/api/v1/agent-configs/"+created.ID, bytes.NewReader(updateBytes))
		req.SetBasicAuth("admin", "secret")
		req.Header.Set("Content-Type", "application/json")
		rr = httptest.NewRecorder()
		srv.Router().ServeHTTP(rr, req)

		if rr.Code != http.StatusOK {
			t.Errorf("expected status %d, got %d", http.StatusOK, rr.Code)
		}
	})

	t.Run("update provider config success", func(t *testing.T) {
		mockService := NewMockProviderConfigService()
		srv := newTestServer(WithProviderConfigService(mockService))

		// Create a config first
		body := CreateProviderConfigRequest{
			Name:     "to-update",
			Provider: "docker",
		}
		bodyBytes, _ := json.Marshal(body)
		req := httptest.NewRequest(http.MethodPost, "/admin/api/v1/provider-configs", bytes.NewReader(bodyBytes))
		req.SetBasicAuth("admin", "secret")
		req.Header.Set("Content-Type", "application/json")
		rr := httptest.NewRecorder()
		srv.Router().ServeHTTP(rr, req)

		var created store.ProviderConfig
		_ = json.NewDecoder(rr.Body).Decode(&created)

		// Now update it
		updateBody := UpdateProviderConfigRequest{
			Name: strPtr("updated-name"),
		}
		updateBytes, _ := json.Marshal(updateBody)
		req = httptest.NewRequest(http.MethodPut, "/admin/api/v1/provider-configs/"+created.ID, bytes.NewReader(updateBytes))
		req.SetBasicAuth("admin", "secret")
		req.Header.Set("Content-Type", "application/json")
		rr = httptest.NewRecorder()
		srv.Router().ServeHTTP(rr, req)

		if rr.Code != http.StatusOK {
			t.Errorf("expected status %d, got %d", http.StatusOK, rr.Code)
		}
	})
}

func TestRevokeAPIKeySuccess(t *testing.T) {
	mockService := NewMockAPIKeyService()
	srv := newTestServer(WithAPIKeyService(mockService))

	// Add a key directly to the mock
	mockService.AddKey(&store.APIKey{
		ID:   "key_to_revoke",
		Name: "to-revoke",
	})

	// Now revoke it (using DELETE)
	req := httptest.NewRequest(http.MethodDelete, "/admin/api/v1/keys/key_to_revoke", nil)
	req.SetBasicAuth("admin", "secret")
	rr := httptest.NewRecorder()
	srv.Router().ServeHTTP(rr, req)

	if rr.Code != http.StatusNoContent {
		t.Errorf("expected status %d, got %d", http.StatusNoContent, rr.Code)
	}
}

func TestDestroyRunnerBusyError(t *testing.T) {
	mockService := NewMockRunnerAdminService()
	srv := newTestServer(WithRunnerAdminService(mockService))

	// Add a busy runner
	mockService.AddRunner(&store.Runner{
		ID:       "run_busy",
		Name:     "busy-runner",
		Hostname: "host",
		Status:   "busy",
	})

	// Try to destroy it
	req := httptest.NewRequest(http.MethodDelete, "/admin/api/v1/runners/run_busy", nil)
	req.SetBasicAuth("admin", "secret")
	rr := httptest.NewRecorder()
	srv.Router().ServeHTTP(rr, req)

	if rr.Code != http.StatusConflict {
		t.Errorf("expected status %d, got %d: %s", http.StatusConflict, rr.Code, rr.Body.String())
	}
}

func TestInternalErrors(t *testing.T) {
	t.Run("agent config create internal error", func(t *testing.T) {
		mockService := NewMockAgentConfigService()
		srv := newTestServer(WithAgentConfigService(mockService))

		mockService.SetInternalError(errors.New("database connection failed"))

		body := CreateAgentConfigRequest{
			Name:   "test-config",
			Agent:  "claude",
			APIKey: "test-key",
		}
		bodyBytes, _ := json.Marshal(body)
		req := httptest.NewRequest(http.MethodPost, "/admin/api/v1/agent-configs", bytes.NewReader(bodyBytes))
		req.SetBasicAuth("admin", "secret")
		req.Header.Set("Content-Type", "application/json")
		rr := httptest.NewRecorder()
		srv.Router().ServeHTTP(rr, req)

		if rr.Code != http.StatusInternalServerError {
			t.Errorf("expected status %d, got %d", http.StatusInternalServerError, rr.Code)
		}
	})

	t.Run("agent config get internal error", func(t *testing.T) {
		mockService := NewMockAgentConfigService()
		srv := newTestServer(WithAgentConfigService(mockService))

		mockService.SetInternalError(errors.New("database connection failed"))

		req := httptest.NewRequest(http.MethodGet, "/admin/api/v1/agent-configs/acfg_test", nil)
		req.SetBasicAuth("admin", "secret")
		rr := httptest.NewRecorder()
		srv.Router().ServeHTTP(rr, req)

		if rr.Code != http.StatusInternalServerError {
			t.Errorf("expected status %d, got %d", http.StatusInternalServerError, rr.Code)
		}
	})

	t.Run("agent config list internal error", func(t *testing.T) {
		mockService := NewMockAgentConfigService()
		srv := newTestServer(WithAgentConfigService(mockService))

		mockService.SetInternalError(errors.New("database connection failed"))

		req := httptest.NewRequest(http.MethodGet, "/admin/api/v1/agent-configs", nil)
		req.SetBasicAuth("admin", "secret")
		rr := httptest.NewRecorder()
		srv.Router().ServeHTTP(rr, req)

		if rr.Code != http.StatusInternalServerError {
			t.Errorf("expected status %d, got %d", http.StatusInternalServerError, rr.Code)
		}
	})

	t.Run("agent config update internal error", func(t *testing.T) {
		mockService := NewMockAgentConfigService()
		srv := newTestServer(WithAgentConfigService(mockService))

		// First create a config
		body := CreateAgentConfigRequest{
			Name:   "test-config",
			Agent:  "claude",
			APIKey: "test-key",
		}
		bodyBytes, _ := json.Marshal(body)
		req := httptest.NewRequest(http.MethodPost, "/admin/api/v1/agent-configs", bytes.NewReader(bodyBytes))
		req.SetBasicAuth("admin", "secret")
		req.Header.Set("Content-Type", "application/json")
		rr := httptest.NewRecorder()
		srv.Router().ServeHTTP(rr, req)

		var created store.AgentConfig
		_ = json.NewDecoder(rr.Body).Decode(&created)

		// Now set internal error and try to update
		mockService.SetInternalError(errors.New("database connection failed"))

		updateBody := UpdateAgentConfigRequest{
			Name: strPtr("new-name"),
		}
		updateBytes, _ := json.Marshal(updateBody)
		req = httptest.NewRequest(http.MethodPut, "/admin/api/v1/agent-configs/"+created.ID, bytes.NewReader(updateBytes))
		req.SetBasicAuth("admin", "secret")
		req.Header.Set("Content-Type", "application/json")
		rr = httptest.NewRecorder()
		srv.Router().ServeHTTP(rr, req)

		if rr.Code != http.StatusInternalServerError {
			t.Errorf("expected status %d, got %d", http.StatusInternalServerError, rr.Code)
		}
	})

	t.Run("agent config delete internal error", func(t *testing.T) {
		mockService := NewMockAgentConfigService()
		srv := newTestServer(WithAgentConfigService(mockService))

		// First create a config
		body := CreateAgentConfigRequest{
			Name:   "test-config",
			Agent:  "claude",
			APIKey: "test-key",
		}
		bodyBytes, _ := json.Marshal(body)
		req := httptest.NewRequest(http.MethodPost, "/admin/api/v1/agent-configs", bytes.NewReader(bodyBytes))
		req.SetBasicAuth("admin", "secret")
		req.Header.Set("Content-Type", "application/json")
		rr := httptest.NewRecorder()
		srv.Router().ServeHTTP(rr, req)

		var created store.AgentConfig
		_ = json.NewDecoder(rr.Body).Decode(&created)

		// Now set internal error and try to delete
		mockService.SetInternalError(errors.New("database connection failed"))

		req = httptest.NewRequest(http.MethodDelete, "/admin/api/v1/agent-configs/"+created.ID, nil)
		req.SetBasicAuth("admin", "secret")
		rr = httptest.NewRecorder()
		srv.Router().ServeHTTP(rr, req)

		if rr.Code != http.StatusInternalServerError {
			t.Errorf("expected status %d, got %d", http.StatusInternalServerError, rr.Code)
		}
	})

	// Provider config internal errors
	t.Run("provider config create internal error", func(t *testing.T) {
		mockService := NewMockProviderConfigService()
		srv := newTestServer(WithProviderConfigService(mockService))
		mockService.SetInternalError(errors.New("database connection failed"))

		body := CreateProviderConfigRequest{Name: "test", Provider: "docker"}
		bodyBytes, _ := json.Marshal(body)
		req := httptest.NewRequest(http.MethodPost, "/admin/api/v1/provider-configs", bytes.NewReader(bodyBytes))
		req.SetBasicAuth("admin", "secret")
		req.Header.Set("Content-Type", "application/json")
		rr := httptest.NewRecorder()
		srv.Router().ServeHTTP(rr, req)

		if rr.Code != http.StatusInternalServerError {
			t.Errorf("expected status %d, got %d", http.StatusInternalServerError, rr.Code)
		}
	})

	t.Run("provider config get internal error", func(t *testing.T) {
		mockService := NewMockProviderConfigService()
		srv := newTestServer(WithProviderConfigService(mockService))
		mockService.SetInternalError(errors.New("database connection failed"))

		req := httptest.NewRequest(http.MethodGet, "/admin/api/v1/provider-configs/pcfg_test", nil)
		req.SetBasicAuth("admin", "secret")
		rr := httptest.NewRecorder()
		srv.Router().ServeHTTP(rr, req)

		if rr.Code != http.StatusInternalServerError {
			t.Errorf("expected status %d, got %d", http.StatusInternalServerError, rr.Code)
		}
	})

	t.Run("provider config list internal error", func(t *testing.T) {
		mockService := NewMockProviderConfigService()
		srv := newTestServer(WithProviderConfigService(mockService))
		mockService.SetInternalError(errors.New("database connection failed"))

		req := httptest.NewRequest(http.MethodGet, "/admin/api/v1/provider-configs", nil)
		req.SetBasicAuth("admin", "secret")
		rr := httptest.NewRecorder()
		srv.Router().ServeHTTP(rr, req)

		if rr.Code != http.StatusInternalServerError {
			t.Errorf("expected status %d, got %d", http.StatusInternalServerError, rr.Code)
		}
	})

	t.Run("provider config update internal error", func(t *testing.T) {
		mockService := NewMockProviderConfigService()
		srv := newTestServer(WithProviderConfigService(mockService))

		body := CreateProviderConfigRequest{Name: "test", Provider: "docker"}
		bodyBytes, _ := json.Marshal(body)
		req := httptest.NewRequest(http.MethodPost, "/admin/api/v1/provider-configs", bytes.NewReader(bodyBytes))
		req.SetBasicAuth("admin", "secret")
		req.Header.Set("Content-Type", "application/json")
		rr := httptest.NewRecorder()
		srv.Router().ServeHTTP(rr, req)

		var created store.ProviderConfig
		_ = json.NewDecoder(rr.Body).Decode(&created)

		mockService.SetInternalError(errors.New("database connection failed"))

		updateBody := UpdateProviderConfigRequest{Name: strPtr("new")}
		updateBytes, _ := json.Marshal(updateBody)
		req = httptest.NewRequest(http.MethodPut, "/admin/api/v1/provider-configs/"+created.ID, bytes.NewReader(updateBytes))
		req.SetBasicAuth("admin", "secret")
		req.Header.Set("Content-Type", "application/json")
		rr = httptest.NewRecorder()
		srv.Router().ServeHTTP(rr, req)

		if rr.Code != http.StatusInternalServerError {
			t.Errorf("expected status %d, got %d", http.StatusInternalServerError, rr.Code)
		}
	})

	t.Run("provider config delete internal error", func(t *testing.T) {
		mockService := NewMockProviderConfigService()
		srv := newTestServer(WithProviderConfigService(mockService))

		body := CreateProviderConfigRequest{Name: "test", Provider: "docker"}
		bodyBytes, _ := json.Marshal(body)
		req := httptest.NewRequest(http.MethodPost, "/admin/api/v1/provider-configs", bytes.NewReader(bodyBytes))
		req.SetBasicAuth("admin", "secret")
		req.Header.Set("Content-Type", "application/json")
		rr := httptest.NewRecorder()
		srv.Router().ServeHTTP(rr, req)

		var created store.ProviderConfig
		_ = json.NewDecoder(rr.Body).Decode(&created)

		mockService.SetInternalError(errors.New("database connection failed"))

		req = httptest.NewRequest(http.MethodDelete, "/admin/api/v1/provider-configs/"+created.ID, nil)
		req.SetBasicAuth("admin", "secret")
		rr = httptest.NewRecorder()
		srv.Router().ServeHTTP(rr, req)

		if rr.Code != http.StatusInternalServerError {
			t.Errorf("expected status %d, got %d", http.StatusInternalServerError, rr.Code)
		}
	})

	// API key internal errors
	t.Run("api key create internal error", func(t *testing.T) {
		mockService := NewMockAPIKeyService()
		srv := newTestServer(WithAPIKeyService(mockService))
		mockService.SetInternalError(errors.New("database connection failed"))

		body := CreateAPIKeyRequest{Name: "test"}
		bodyBytes, _ := json.Marshal(body)
		req := httptest.NewRequest(http.MethodPost, "/admin/api/v1/keys", bytes.NewReader(bodyBytes))
		req.SetBasicAuth("admin", "secret")
		req.Header.Set("Content-Type", "application/json")
		rr := httptest.NewRecorder()
		srv.Router().ServeHTTP(rr, req)

		if rr.Code != http.StatusInternalServerError {
			t.Errorf("expected status %d, got %d", http.StatusInternalServerError, rr.Code)
		}
	})

	t.Run("api key get internal error", func(t *testing.T) {
		mockService := NewMockAPIKeyService()
		srv := newTestServer(WithAPIKeyService(mockService))
		mockService.SetInternalError(errors.New("database connection failed"))

		req := httptest.NewRequest(http.MethodGet, "/admin/api/v1/keys/key_test", nil)
		req.SetBasicAuth("admin", "secret")
		rr := httptest.NewRecorder()
		srv.Router().ServeHTTP(rr, req)

		if rr.Code != http.StatusInternalServerError {
			t.Errorf("expected status %d, got %d", http.StatusInternalServerError, rr.Code)
		}
	})

	t.Run("api key list internal error", func(t *testing.T) {
		mockService := NewMockAPIKeyService()
		srv := newTestServer(WithAPIKeyService(mockService))
		mockService.SetInternalError(errors.New("database connection failed"))

		req := httptest.NewRequest(http.MethodGet, "/admin/api/v1/keys", nil)
		req.SetBasicAuth("admin", "secret")
		rr := httptest.NewRecorder()
		srv.Router().ServeHTTP(rr, req)

		if rr.Code != http.StatusInternalServerError {
			t.Errorf("expected status %d, got %d", http.StatusInternalServerError, rr.Code)
		}
	})

	t.Run("api key revoke internal error", func(t *testing.T) {
		mockService := NewMockAPIKeyService()
		srv := newTestServer(WithAPIKeyService(mockService))
		mockService.AddKey(&store.APIKey{ID: "key_test", Name: "test"})
		mockService.SetInternalError(errors.New("database connection failed"))

		req := httptest.NewRequest(http.MethodDelete, "/admin/api/v1/keys/key_test", nil)
		req.SetBasicAuth("admin", "secret")
		rr := httptest.NewRecorder()
		srv.Router().ServeHTTP(rr, req)

		if rr.Code != http.StatusInternalServerError {
			t.Errorf("expected status %d, got %d", http.StatusInternalServerError, rr.Code)
		}
	})

	// Runner internal errors
	t.Run("runner spawn internal error", func(t *testing.T) {
		mockService := NewMockRunnerAdminService()
		srv := newTestServer(WithRunnerAdminService(mockService))
		mockService.SetInternalError(errors.New("provider unavailable"))

		body := SpawnRunnerRequest{Name: "test", ProviderConfigID: "pcfg_test"}
		bodyBytes, _ := json.Marshal(body)
		req := httptest.NewRequest(http.MethodPost, "/admin/api/v1/runners/spawn", bytes.NewReader(bodyBytes))
		req.SetBasicAuth("admin", "secret")
		req.Header.Set("Content-Type", "application/json")
		rr := httptest.NewRecorder()
		srv.Router().ServeHTTP(rr, req)

		if rr.Code != http.StatusInternalServerError {
			t.Errorf("expected status %d, got %d", http.StatusInternalServerError, rr.Code)
		}
	})

	t.Run("runner get internal error", func(t *testing.T) {
		mockService := NewMockRunnerAdminService()
		srv := newTestServer(WithRunnerAdminService(mockService))
		mockService.SetInternalError(errors.New("database connection failed"))

		req := httptest.NewRequest(http.MethodGet, "/admin/api/v1/runners/run_test", nil)
		req.SetBasicAuth("admin", "secret")
		rr := httptest.NewRecorder()
		srv.Router().ServeHTTP(rr, req)

		if rr.Code != http.StatusInternalServerError {
			t.Errorf("expected status %d, got %d", http.StatusInternalServerError, rr.Code)
		}
	})

	t.Run("runner list internal error", func(t *testing.T) {
		mockService := NewMockRunnerAdminService()
		srv := newTestServer(WithRunnerAdminService(mockService))
		mockService.SetInternalError(errors.New("database connection failed"))

		req := httptest.NewRequest(http.MethodGet, "/admin/api/v1/runners", nil)
		req.SetBasicAuth("admin", "secret")
		rr := httptest.NewRecorder()
		srv.Router().ServeHTTP(rr, req)

		if rr.Code != http.StatusInternalServerError {
			t.Errorf("expected status %d, got %d", http.StatusInternalServerError, rr.Code)
		}
	})

	t.Run("runner destroy internal error", func(t *testing.T) {
		mockService := NewMockRunnerAdminService()
		srv := newTestServer(WithRunnerAdminService(mockService))
		mockService.AddRunner(&store.Runner{ID: "run_test", Name: "test", Hostname: "host", Status: "idle"})
		mockService.SetInternalError(errors.New("provider unavailable"))

		req := httptest.NewRequest(http.MethodDelete, "/admin/api/v1/runners/run_test", nil)
		req.SetBasicAuth("admin", "secret")
		rr := httptest.NewRecorder()
		srv.Router().ServeHTTP(rr, req)

		if rr.Code != http.StatusInternalServerError {
			t.Errorf("expected status %d, got %d", http.StatusInternalServerError, rr.Code)
		}
	})
}

func TestProfileHandlers(t *testing.T) {
	mockService := NewMockProfileService()
	srv := newTestServer(WithProfileService(mockService))

	t.Run("create profile", func(t *testing.T) {
		body := CreateProfileRequest{
			Name:        "test-profile",
			Description: "A test profile",
			Resources: map[string]any{
				"cpu":    "2",
				"memory": "4Gi",
			},
		}
		bodyBytes, _ := json.Marshal(body)

		req := httptest.NewRequest(http.MethodPost, "/admin/api/v1/profiles/", bytes.NewReader(bodyBytes))
		req.SetBasicAuth("admin", "secret")
		req.Header.Set("Content-Type", "application/json")

		rr := httptest.NewRecorder()
		srv.Router().ServeHTTP(rr, req)

		if rr.Code != http.StatusCreated {
			t.Errorf("expected status %d, got %d: %s", http.StatusCreated, rr.Code, rr.Body.String())
		}
	})

	t.Run("create profile - validation error", func(t *testing.T) {
		body := CreateProfileRequest{
			// Missing required name
		}
		bodyBytes, _ := json.Marshal(body)

		req := httptest.NewRequest(http.MethodPost, "/admin/api/v1/profiles/", bytes.NewReader(bodyBytes))
		req.SetBasicAuth("admin", "secret")
		req.Header.Set("Content-Type", "application/json")

		rr := httptest.NewRecorder()
		srv.Router().ServeHTTP(rr, req)

		if rr.Code != http.StatusBadRequest {
			t.Errorf("expected status %d, got %d", http.StatusBadRequest, rr.Code)
		}
	})

	t.Run("list profiles", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/admin/api/v1/profiles/", nil)
		req.SetBasicAuth("admin", "secret")

		rr := httptest.NewRecorder()
		srv.Router().ServeHTTP(rr, req)

		if rr.Code != http.StatusOK {
			t.Errorf("expected status %d, got %d", http.StatusOK, rr.Code)
		}
	})

	t.Run("list profiles with filters", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/admin/api/v1/profiles/?include_builtin=true&provider_config_id=pcfg_test", nil)
		req.SetBasicAuth("admin", "secret")

		rr := httptest.NewRecorder()
		srv.Router().ServeHTTP(rr, req)

		if rr.Code != http.StatusOK {
			t.Errorf("expected status %d, got %d", http.StatusOK, rr.Code)
		}
	})

	t.Run("get profile", func(t *testing.T) {
		mockService.AddProfile(&store.Profile{
			ID:   "prof_test123",
			Name: "get-test-profile",
		})

		req := httptest.NewRequest(http.MethodGet, "/admin/api/v1/profiles/prof_test123", nil)
		req.SetBasicAuth("admin", "secret")

		rr := httptest.NewRecorder()
		srv.Router().ServeHTTP(rr, req)

		if rr.Code != http.StatusOK {
			t.Errorf("expected status %d, got %d: %s", http.StatusOK, rr.Code, rr.Body.String())
		}
	})

	t.Run("get profile not found", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/admin/api/v1/profiles/nonexistent", nil)
		req.SetBasicAuth("admin", "secret")

		rr := httptest.NewRecorder()
		srv.Router().ServeHTTP(rr, req)

		if rr.Code != http.StatusNotFound {
			t.Errorf("expected status %d, got %d: %s", http.StatusNotFound, rr.Code, rr.Body.String())
		}
	})

	t.Run("update profile", func(t *testing.T) {
		mockService.AddProfile(&store.Profile{
			ID:   "prof_update123",
			Name: "update-test-profile",
		})

		newName := "updated-profile"
		body := UpdateProfileRequest{
			Name: &newName,
		}
		bodyBytes, _ := json.Marshal(body)

		req := httptest.NewRequest(http.MethodPut, "/admin/api/v1/profiles/prof_update123", bytes.NewReader(bodyBytes))
		req.SetBasicAuth("admin", "secret")
		req.Header.Set("Content-Type", "application/json")

		rr := httptest.NewRecorder()
		srv.Router().ServeHTTP(rr, req)

		if rr.Code != http.StatusOK {
			t.Errorf("expected status %d, got %d: %s", http.StatusOK, rr.Code, rr.Body.String())
		}
	})

	t.Run("update profile not found", func(t *testing.T) {
		newName := "updated-profile"
		body := UpdateProfileRequest{
			Name: &newName,
		}
		bodyBytes, _ := json.Marshal(body)

		req := httptest.NewRequest(http.MethodPut, "/admin/api/v1/profiles/nonexistent", bytes.NewReader(bodyBytes))
		req.SetBasicAuth("admin", "secret")
		req.Header.Set("Content-Type", "application/json")

		rr := httptest.NewRecorder()
		srv.Router().ServeHTTP(rr, req)

		if rr.Code != http.StatusNotFound {
			t.Errorf("expected status %d, got %d: %s", http.StatusNotFound, rr.Code, rr.Body.String())
		}
	})

	t.Run("delete profile", func(t *testing.T) {
		mockService.AddProfile(&store.Profile{
			ID:   "prof_delete123",
			Name: "delete-test-profile",
		})

		req := httptest.NewRequest(http.MethodDelete, "/admin/api/v1/profiles/prof_delete123", nil)
		req.SetBasicAuth("admin", "secret")

		rr := httptest.NewRecorder()
		srv.Router().ServeHTTP(rr, req)

		if rr.Code != http.StatusNoContent {
			t.Errorf("expected status %d, got %d: %s", http.StatusNoContent, rr.Code, rr.Body.String())
		}
	})

	t.Run("delete profile not found", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodDelete, "/admin/api/v1/profiles/nonexistent", nil)
		req.SetBasicAuth("admin", "secret")

		rr := httptest.NewRecorder()
		srv.Router().ServeHTTP(rr, req)

		if rr.Code != http.StatusNotFound {
			t.Errorf("expected status %d, got %d: %s", http.StatusNotFound, rr.Code, rr.Body.String())
		}
	})
}

func TestProfileServiceNotConfigured(t *testing.T) {
	srv := newTestServer()

	tests := []struct {
		name   string
		method string
		path   string
	}{
		{"create profile without service", http.MethodPost, "/admin/api/v1/profiles/"},
		{"list profiles without service", http.MethodGet, "/admin/api/v1/profiles/"},
		{"get profile without service", http.MethodGet, "/admin/api/v1/profiles/prof_test"},
		{"update profile without service", http.MethodPut, "/admin/api/v1/profiles/prof_test"},
		{"delete profile without service", http.MethodDelete, "/admin/api/v1/profiles/prof_test"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var body []byte
			if tt.method == http.MethodPost || tt.method == http.MethodPut {
				body = []byte(`{"name": "test"}`)
			}
			req := httptest.NewRequest(tt.method, tt.path, bytes.NewReader(body))
			req.SetBasicAuth("admin", "secret")
			if body != nil {
				req.Header.Set("Content-Type", "application/json")
			}

			rr := httptest.NewRecorder()
			srv.Router().ServeHTTP(rr, req)

			if rr.Code != http.StatusNotImplemented {
				t.Errorf("expected status %d, got %d: %s", http.StatusNotImplemented, rr.Code, rr.Body.String())
			}
		})
	}
}

func TestProfileInvalidJSONBody(t *testing.T) {
	mockService := NewMockProfileService()
	srv := newTestServer(WithProfileService(mockService))

	t.Run("create profile invalid JSON", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/admin/api/v1/profiles/", bytes.NewReader([]byte("invalid json")))
		req.SetBasicAuth("admin", "secret")
		req.Header.Set("Content-Type", "application/json")

		rr := httptest.NewRecorder()
		srv.Router().ServeHTTP(rr, req)

		if rr.Code != http.StatusBadRequest {
			t.Errorf("expected status %d, got %d: %s", http.StatusBadRequest, rr.Code, rr.Body.String())
		}
	})

	t.Run("update profile invalid JSON", func(t *testing.T) {
		mockService.AddProfile(&store.Profile{ID: "prof_test123", Name: "test"})

		req := httptest.NewRequest(http.MethodPut, "/admin/api/v1/profiles/prof_test123", bytes.NewReader([]byte("invalid json")))
		req.SetBasicAuth("admin", "secret")
		req.Header.Set("Content-Type", "application/json")

		rr := httptest.NewRecorder()
		srv.Router().ServeHTTP(rr, req)

		if rr.Code != http.StatusBadRequest {
			t.Errorf("expected status %d, got %d: %s", http.StatusBadRequest, rr.Code, rr.Body.String())
		}
	})
}

func TestProfileHandlersInternalErrors(t *testing.T) {
	mockService := NewMockProfileService()
	srv := newTestServer(WithProfileService(mockService))

	t.Run("create profile internal error", func(t *testing.T) {
		mockService.SetInternalError(errors.New("database error"))

		body := CreateProfileRequest{Name: "test-profile"}
		bodyBytes, _ := json.Marshal(body)

		req := httptest.NewRequest(http.MethodPost, "/admin/api/v1/profiles/", bytes.NewReader(bodyBytes))
		req.SetBasicAuth("admin", "secret")
		req.Header.Set("Content-Type", "application/json")

		rr := httptest.NewRecorder()
		srv.Router().ServeHTTP(rr, req)

		if rr.Code != http.StatusInternalServerError {
			t.Errorf("expected status %d, got %d: %s", http.StatusInternalServerError, rr.Code, rr.Body.String())
		}
	})

	t.Run("list profiles internal error", func(t *testing.T) {
		mockService.SetInternalError(errors.New("database error"))

		req := httptest.NewRequest(http.MethodGet, "/admin/api/v1/profiles/", nil)
		req.SetBasicAuth("admin", "secret")

		rr := httptest.NewRecorder()
		srv.Router().ServeHTTP(rr, req)

		if rr.Code != http.StatusInternalServerError {
			t.Errorf("expected status %d, got %d: %s", http.StatusInternalServerError, rr.Code, rr.Body.String())
		}
	})

	t.Run("get profile internal error", func(t *testing.T) {
		mockService.AddProfile(&store.Profile{ID: "prof_internal", Name: "test"})
		mockService.SetInternalError(errors.New("database error"))

		req := httptest.NewRequest(http.MethodGet, "/admin/api/v1/profiles/prof_internal", nil)
		req.SetBasicAuth("admin", "secret")

		rr := httptest.NewRecorder()
		srv.Router().ServeHTTP(rr, req)

		if rr.Code != http.StatusInternalServerError {
			t.Errorf("expected status %d, got %d: %s", http.StatusInternalServerError, rr.Code, rr.Body.String())
		}
	})

	t.Run("update profile internal error", func(t *testing.T) {
		mockService.AddProfile(&store.Profile{ID: "prof_update_err", Name: "test"})
		mockService.SetInternalError(errors.New("database error"))

		newName := "updated"
		body := UpdateProfileRequest{Name: &newName}
		bodyBytes, _ := json.Marshal(body)

		req := httptest.NewRequest(http.MethodPut, "/admin/api/v1/profiles/prof_update_err", bytes.NewReader(bodyBytes))
		req.SetBasicAuth("admin", "secret")
		req.Header.Set("Content-Type", "application/json")

		rr := httptest.NewRecorder()
		srv.Router().ServeHTTP(rr, req)

		if rr.Code != http.StatusInternalServerError {
			t.Errorf("expected status %d, got %d: %s", http.StatusInternalServerError, rr.Code, rr.Body.String())
		}
	})

	t.Run("update profile validation error", func(t *testing.T) {
		mockService.AddProfile(&store.Profile{ID: "prof_valid_err", Name: "test"})
		mockService.SetValidationError("name", "name already exists")

		newName := "duplicate"
		body := UpdateProfileRequest{Name: &newName}
		bodyBytes, _ := json.Marshal(body)

		req := httptest.NewRequest(http.MethodPut, "/admin/api/v1/profiles/prof_valid_err", bytes.NewReader(bodyBytes))
		req.SetBasicAuth("admin", "secret")
		req.Header.Set("Content-Type", "application/json")

		rr := httptest.NewRecorder()
		srv.Router().ServeHTTP(rr, req)

		if rr.Code != http.StatusBadRequest {
			t.Errorf("expected status %d, got %d: %s", http.StatusBadRequest, rr.Code, rr.Body.String())
		}
	})

	t.Run("delete profile internal error", func(t *testing.T) {
		mockService.AddProfile(&store.Profile{ID: "prof_delete_err", Name: "test"})
		mockService.SetInternalError(errors.New("database error"))

		req := httptest.NewRequest(http.MethodDelete, "/admin/api/v1/profiles/prof_delete_err", nil)
		req.SetBasicAuth("admin", "secret")

		rr := httptest.NewRecorder()
		srv.Router().ServeHTTP(rr, req)

		if rr.Code != http.StatusInternalServerError {
			t.Errorf("expected status %d, got %d: %s", http.StatusInternalServerError, rr.Code, rr.Body.String())
		}
	})
}

func TestNew_FailsClosedWithoutCredentials(t *testing.T) {
	logger := zap.NewNop()

	tests := []struct {
		name     string
		username string
		password string
	}{
		{"no credentials at all", "", ""},
		{"username only", "admin", ""},
		{"password only", "", "secret"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv, err := New(Config{
				Host:     "localhost",
				Port:     8081,
				Username: tt.username,
				Password: tt.password,
			}, logger)

			require.ErrorIs(t, err, ErrCredentialsRequired)
			assert.Nil(t, srv)
		})
	}
}

// TestNew_AllowInsecureSkipsAuth documents the explicit development opt-out.
func TestNew_AllowInsecureSkipsAuth(t *testing.T) {
	logger := zap.NewNop()

	srv, err := New(Config{
		Host:          "localhost",
		Port:          8081,
		AllowInsecure: true,
	}, logger, WithAPIKeyService(NewMockAPIKeyService()))
	require.NoError(t, err)
	require.NotNil(t, srv)

	req := httptest.NewRequest(http.MethodGet, "/admin/api/v1/keys", nil)
	rec := httptest.NewRecorder()
	srv.router.ServeHTTP(rec, req)

	assert.NotEqual(t, http.StatusUnauthorized, rec.Code,
		"AllowInsecure must serve the admin API without credentials")
}

// TestNew_AuthEnforcedWhenCredentialsSet is the other half of the contract:
// with credentials present, every admin route is behind basic auth.
func TestNew_AuthEnforcedWhenCredentialsSet(t *testing.T) {
	srv := newTestServer(WithAPIKeyService(NewMockAPIKeyService()))

	req := httptest.NewRequest(http.MethodGet, "/admin/api/v1/keys", nil)
	rec := httptest.NewRecorder()
	srv.router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusUnauthorized, rec.Code)
}

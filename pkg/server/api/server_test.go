package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/chunlea/marionette/pkg/auth"
	"github.com/chunlea/marionette/pkg/server/api/apitypes"
	"github.com/chunlea/marionette/pkg/store"
)

// mockAPIKeyStore implements store.APIKeyStore for testing.
type mockAPIKeyStore struct {
	mu   sync.RWMutex
	keys map[string]*store.APIKey
}

func newMockAPIKeyStore() *mockAPIKeyStore {
	return &mockAPIKeyStore{
		keys: make(map[string]*store.APIKey),
	}
}

func (m *mockAPIKeyStore) CreateAPIKey(_ context.Context, key *store.APIKey) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.keys[key.ID] = key
	return nil
}

func (m *mockAPIKeyStore) GetAPIKey(_ context.Context, id string) (*store.APIKey, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	key, ok := m.keys[id]
	if !ok {
		return nil, store.ErrNotFound
	}
	return key, nil
}

func (m *mockAPIKeyStore) GetAPIKeyByHash(_ context.Context, hash string) (*store.APIKey, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	for _, key := range m.keys {
		if key.KeyHash == hash {
			return key, nil
		}
	}
	return nil, store.ErrNotFound
}

func (m *mockAPIKeyStore) ListAPIKeys(_ context.Context, _ store.ListAPIKeysOptions) (*store.ListResult[store.APIKey], error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	items := make([]*store.APIKey, 0, len(m.keys))
	for _, key := range m.keys {
		items = append(items, key)
	}
	return &store.ListResult[store.APIKey]{Items: items, TotalCount: int64(len(items))}, nil
}

func (m *mockAPIKeyStore) UpdateAPIKey(_ context.Context, id string, updates store.APIKeyUpdates) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	key, ok := m.keys[id]
	if !ok {
		return store.ErrNotFound
	}
	if updates.LastUsedAt != nil {
		key.LastUsedAt = updates.LastUsedAt
	}
	if updates.RevokedAt != nil {
		key.RevokedAt = updates.RevokedAt
	}
	if updates.RevokeReason != nil {
		key.RevokeReason = updates.RevokeReason
	}
	return nil
}

func (m *mockAPIKeyStore) DeleteAPIKey(_ context.Context, id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.keys, id)
	return nil
}

// testServer creates a test server with mock services.
//
//nolint:unparam // apiKeyService may be used in future tests
func testServer(t *testing.T, opts ...Option) (*Server, *auth.APIKeyService, string) {
	t.Helper()

	logger := zap.NewNop()
	keyStore := newMockAPIKeyStore()
	apiKeyService := auth.NewAPIKeyService(keyStore, func() string { return "key_test123" })

	// Create an API key for testing
	key, token, err := apiKeyService.Create(context.Background(), auth.CreateAPIKeyOptions{
		Name:   "test-key",
		Scopes: []string{"*"}, // Full access
	})
	require.NoError(t, err)
	require.NotNil(t, key)

	allOpts := append([]Option{WithAPIKeyService(apiKeyService)}, opts...)
	srv := New(Config{Host: "localhost", Port: 8080}, logger, allOpts...)

	return srv, apiKeyService, token
}

func TestHealth(t *testing.T) {
	srv, _, _ := testServer(t)

	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	rec := httptest.NewRecorder()

	srv.Router().ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.JSONEq(t, `{"status":"ok"}`, rec.Body.String())
}

func TestAuthMiddleware(t *testing.T) {
	sessionSvc := NewMockSessionService()
	srv, _, validToken := testServer(t, WithSessionService(sessionSvc))

	t.Run("missing authorization header", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/sessions", nil)
		rec := httptest.NewRecorder()

		srv.Router().ServeHTTP(rec, req)

		assert.Equal(t, http.StatusUnauthorized, rec.Code)
		var resp apitypes.ErrorResponse
		err := json.NewDecoder(rec.Body).Decode(&resp)
		require.NoError(t, err)
		assert.Equal(t, "missing_auth", resp.Code)
	})

	t.Run("invalid authorization format", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/sessions", nil)
		req.Header.Set("Authorization", "InvalidFormat")
		rec := httptest.NewRecorder()

		srv.Router().ServeHTTP(rec, req)

		assert.Equal(t, http.StatusUnauthorized, rec.Code)
	})

	t.Run("invalid token", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/sessions", nil)
		req.Header.Set("Authorization", "Bearer mk_invalid_token_12345678901234567890")
		rec := httptest.NewRecorder()

		srv.Router().ServeHTTP(rec, req)

		assert.Equal(t, http.StatusUnauthorized, rec.Code)
	})

	t.Run("valid token", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/sessions", nil)
		req.Header.Set("Authorization", "Bearer "+validToken)
		rec := httptest.NewRecorder()

		srv.Router().ServeHTTP(rec, req)

		assert.Equal(t, http.StatusOK, rec.Code)
	})
}

func TestSessions(t *testing.T) {
	sessionSvc := NewMockSessionService()
	srv, _, token := testServer(t, WithSessionService(sessionSvc))

	// Add a test session
	testSession := &store.Session{
		ID:        "sess_test123",
		Status:    "active",
		Agent:     "claude",
		CreatedAt: time.Now(),
	}
	sessionSvc.AddSession(testSession)

	t.Run("create session", func(t *testing.T) {
		body := `{"agent": "claude", "name": "test-session"}`
		req := httptest.NewRequest(http.MethodPost, "/api/v1/sessions", bytes.NewBufferString(body))
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()

		srv.Router().ServeHTTP(rec, req)

		assert.Equal(t, http.StatusCreated, rec.Code)
	})

	t.Run("create session missing agent", func(t *testing.T) {
		body := `{"name": "test-session"}`
		req := httptest.NewRequest(http.MethodPost, "/api/v1/sessions", bytes.NewBufferString(body))
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()

		srv.Router().ServeHTTP(rec, req)

		assert.Equal(t, http.StatusBadRequest, rec.Code)
	})

	t.Run("list sessions", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/sessions", nil)
		req.Header.Set("Authorization", "Bearer "+token)
		rec := httptest.NewRecorder()

		srv.Router().ServeHTTP(rec, req)

		assert.Equal(t, http.StatusOK, rec.Code)
	})

	t.Run("list sessions with filters", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/sessions?status=active&agent=claude&limit=10", nil)
		req.Header.Set("Authorization", "Bearer "+token)
		rec := httptest.NewRecorder()

		srv.Router().ServeHTTP(rec, req)

		assert.Equal(t, http.StatusOK, rec.Code)
	})

	t.Run("get session", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/sessions/sess_test123", nil)
		req.Header.Set("Authorization", "Bearer "+token)
		rec := httptest.NewRecorder()

		srv.Router().ServeHTTP(rec, req)

		assert.Equal(t, http.StatusOK, rec.Code)
	})

	t.Run("get session not found", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/sessions/sess_notfound", nil)
		req.Header.Set("Authorization", "Bearer "+token)
		rec := httptest.NewRecorder()

		srv.Router().ServeHTTP(rec, req)

		assert.Equal(t, http.StatusNotFound, rec.Code)
	})

	t.Run("suspend session", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/sessions/sess_test123/suspend", nil)
		req.Header.Set("Authorization", "Bearer "+token)
		rec := httptest.NewRecorder()

		srv.Router().ServeHTTP(rec, req)

		assert.Equal(t, http.StatusNoContent, rec.Code)
	})

	t.Run("resume session", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/sessions/sess_test123/resume", nil)
		req.Header.Set("Authorization", "Bearer "+token)
		rec := httptest.NewRecorder()

		srv.Router().ServeHTTP(rec, req)

		assert.Equal(t, http.StatusNoContent, rec.Code)
	})

	t.Run("terminate session", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodDelete, "/api/v1/sessions/sess_test123", nil)
		req.Header.Set("Authorization", "Bearer "+token)
		rec := httptest.NewRecorder()

		srv.Router().ServeHTTP(rec, req)

		assert.Equal(t, http.StatusNoContent, rec.Code)
	})
}

func TestTasks(t *testing.T) {
	sessionSvc := NewMockSessionService()
	taskSvc := NewMockTaskService()
	srv, _, token := testServer(t, WithSessionService(sessionSvc), WithTaskService(taskSvc))

	// Add test session and task
	sessionSvc.AddSession(&store.Session{ID: "sess_test123", Status: "active", Agent: "claude", CreatedAt: time.Now()})
	taskSvc.AddTask(&store.Task{ID: "task_test123", SessionID: "sess_test123", Status: "running", Prompt: "Build API", CreatedAt: time.Now()})

	t.Run("create task", func(t *testing.T) {
		body := `{"session_id": "sess_test123", "prompt": "Build an API"}`
		req := httptest.NewRequest(http.MethodPost, "/api/v1/tasks", bytes.NewBufferString(body))
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()

		srv.Router().ServeHTTP(rec, req)

		assert.Equal(t, http.StatusCreated, rec.Code)
	})

	t.Run("create task missing session_id", func(t *testing.T) {
		body := `{"prompt": "Build an API"}`
		req := httptest.NewRequest(http.MethodPost, "/api/v1/tasks", bytes.NewBufferString(body))
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()

		srv.Router().ServeHTTP(rec, req)

		assert.Equal(t, http.StatusBadRequest, rec.Code)
	})

	t.Run("create task missing prompt", func(t *testing.T) {
		body := `{"session_id": "sess_test123"}`
		req := httptest.NewRequest(http.MethodPost, "/api/v1/tasks", bytes.NewBufferString(body))
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()

		srv.Router().ServeHTTP(rec, req)

		assert.Equal(t, http.StatusBadRequest, rec.Code)
	})

	t.Run("list tasks", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/tasks?session_id=sess_test123", nil)
		req.Header.Set("Authorization", "Bearer "+token)
		rec := httptest.NewRecorder()

		srv.Router().ServeHTTP(rec, req)

		assert.Equal(t, http.StatusOK, rec.Code)
	})

	t.Run("get task", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/tasks/task_test123", nil)
		req.Header.Set("Authorization", "Bearer "+token)
		rec := httptest.NewRecorder()

		srv.Router().ServeHTTP(rec, req)

		assert.Equal(t, http.StatusOK, rec.Code)
	})

	t.Run("get task not found", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/tasks/task_notfound", nil)
		req.Header.Set("Authorization", "Bearer "+token)
		rec := httptest.NewRecorder()

		srv.Router().ServeHTTP(rec, req)

		assert.Equal(t, http.StatusNotFound, rec.Code)
	})

	t.Run("cancel task", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/tasks/task_test123/cancel", nil)
		req.Header.Set("Authorization", "Bearer "+token)
		rec := httptest.NewRecorder()

		srv.Router().ServeHTTP(rec, req)

		assert.Equal(t, http.StatusNoContent, rec.Code)
	})

	t.Run("retry task", func(t *testing.T) {
		// Add a failed task for retry test (with MaxRetries > 0)
		taskSvc.AddTask(&store.Task{ID: "task_failed", SessionID: "sess_test123", Status: "failed", Prompt: "Failed task", MaxRetries: 3, CreatedAt: time.Now()})

		req := httptest.NewRequest(http.MethodPost, "/api/v1/tasks/task_failed/retry", nil)
		req.Header.Set("Authorization", "Bearer "+token)
		rec := httptest.NewRecorder()

		srv.Router().ServeHTTP(rec, req)

		assert.Equal(t, http.StatusNoContent, rec.Code)
	})

	t.Run("get task logs", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/tasks/task_test123/logs", nil)
		req.Header.Set("Authorization", "Bearer "+token)
		rec := httptest.NewRecorder()

		srv.Router().ServeHTTP(rec, req)

		assert.Equal(t, http.StatusOK, rec.Code)
	})
}

func TestRunners(t *testing.T) {
	runnerSvc := NewMockRunnerService()
	srv, _, token := testServer(t, WithRunnerService(runnerSvc))

	// Add test runner
	runnerSvc.AddRunner(&store.Runner{ID: "run_test123", Name: "test-runner", Hostname: "localhost", Status: "idle", CreatedAt: time.Now()})

	t.Run("list runners", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/runners", nil)
		req.Header.Set("Authorization", "Bearer "+token)
		rec := httptest.NewRecorder()

		srv.Router().ServeHTTP(rec, req)

		assert.Equal(t, http.StatusOK, rec.Code)
	})

	t.Run("list runners with filters", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/runners?status=idle&pool_name=default", nil)
		req.Header.Set("Authorization", "Bearer "+token)
		rec := httptest.NewRecorder()

		srv.Router().ServeHTTP(rec, req)

		assert.Equal(t, http.StatusOK, rec.Code)
	})

	t.Run("get runner", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/runners/run_test123", nil)
		req.Header.Set("Authorization", "Bearer "+token)
		rec := httptest.NewRecorder()

		srv.Router().ServeHTTP(rec, req)

		assert.Equal(t, http.StatusOK, rec.Code)
	})

	t.Run("get runner not found", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/runners/run_notfound", nil)
		req.Header.Set("Authorization", "Bearer "+token)
		rec := httptest.NewRecorder()

		srv.Router().ServeHTTP(rec, req)

		assert.Equal(t, http.StatusNotFound, rec.Code)
	})
}

func TestPermissions(t *testing.T) {
	permSvc := NewMockPermissionService()
	srv, _, token := testServer(t, WithPermissionService(permSvc))

	// Add test permission
	permSvc.AddPermission(&store.PermissionRequest{
		ID:        "perm_test123",
		SessionID: "sess_test123",
		TaskID:    "task_test123",
		RunID:     "trun_test123",
		Tool:      "bash",
		Action:    "rm -rf /tmp/test",
		Status:    "pending",
		RiskLevel: "high",
		CreatedAt: time.Now(),
	})

	t.Run("list permissions", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/permissions", nil)
		req.Header.Set("Authorization", "Bearer "+token)
		rec := httptest.NewRecorder()

		srv.Router().ServeHTTP(rec, req)

		assert.Equal(t, http.StatusOK, rec.Code)
	})

	t.Run("list permissions with filters", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/permissions?session_id=sess_test123&status=pending", nil)
		req.Header.Set("Authorization", "Bearer "+token)
		rec := httptest.NewRecorder()

		srv.Router().ServeHTTP(rec, req)

		assert.Equal(t, http.StatusOK, rec.Code)
	})

	t.Run("get permission", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/permissions/perm_test123", nil)
		req.Header.Set("Authorization", "Bearer "+token)
		rec := httptest.NewRecorder()

		srv.Router().ServeHTTP(rec, req)

		assert.Equal(t, http.StatusOK, rec.Code)
	})

	t.Run("get permission not found", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/permissions/perm_notfound", nil)
		req.Header.Set("Authorization", "Bearer "+token)
		rec := httptest.NewRecorder()

		srv.Router().ServeHTTP(rec, req)

		assert.Equal(t, http.StatusNotFound, rec.Code)
	})

	t.Run("approve permission", func(t *testing.T) {
		body := `{"reason": "Approved for testing"}`
		req := httptest.NewRequest(http.MethodPost, "/api/v1/permissions/perm_test123/approve", bytes.NewBufferString(body))
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()

		srv.Router().ServeHTTP(rec, req)

		assert.Equal(t, http.StatusNoContent, rec.Code)
	})

	t.Run("deny permission", func(t *testing.T) {
		// Add another permission for deny test
		permSvc.AddPermission(&store.PermissionRequest{
			ID:        "perm_test456",
			SessionID: "sess_test123",
			TaskID:    "task_test123",
			RunID:     "trun_test123",
			Tool:      "bash",
			Action:    "dangerous command",
			Status:    "pending",
			RiskLevel: "critical",
			CreatedAt: time.Now(),
		})

		body := `{"reason": "Too risky"}`
		req := httptest.NewRequest(http.MethodPost, "/api/v1/permissions/perm_test456/deny", bytes.NewBufferString(body))
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()

		srv.Router().ServeHTTP(rec, req)

		assert.Equal(t, http.StatusNoContent, rec.Code)
	})
}

func TestScopeMiddleware(t *testing.T) {
	sessionSvc := NewMockSessionService()
	logger := zap.NewNop()
	keyStore := newMockAPIKeyStore()
	apiKeyService := auth.NewAPIKeyService(keyStore, func() string { return "key_test123" })

	// Create API key with limited scope
	_, token, err := apiKeyService.Create(context.Background(), auth.CreateAPIKeyOptions{
		Name:   "read-only-key",
		Scopes: []string{"sessions:read"}, // Read-only access to sessions
	})
	require.NoError(t, err)

	srv := New(Config{Host: "localhost", Port: 8080}, logger,
		WithAPIKeyService(apiKeyService),
		WithSessionService(sessionSvc),
	)

	t.Run("allowed scope", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/sessions", nil)
		req.Header.Set("Authorization", "Bearer "+token)
		rec := httptest.NewRecorder()

		srv.Router().ServeHTTP(rec, req)

		assert.Equal(t, http.StatusOK, rec.Code)
	})

	t.Run("forbidden scope", func(t *testing.T) {
		body := `{"agent": "claude"}`
		req := httptest.NewRequest(http.MethodPost, "/api/v1/sessions", bytes.NewBufferString(body))
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()

		srv.Router().ServeHTTP(rec, req)

		assert.Equal(t, http.StatusForbidden, rec.Code)
	})
}

func TestServiceUnavailable(t *testing.T) {
	srv, _, token := testServer(t) // No services configured

	t.Run("sessions not configured", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/sessions", nil)
		req.Header.Set("Authorization", "Bearer "+token)
		rec := httptest.NewRecorder()

		srv.Router().ServeHTTP(rec, req)

		assert.Equal(t, http.StatusInternalServerError, rec.Code)
		var resp apitypes.ErrorResponse
		err := json.NewDecoder(rec.Body).Decode(&resp)
		require.NoError(t, err)
		assert.Equal(t, "service_unavailable", resp.Code)
	})

	t.Run("tasks not configured", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/tasks", nil)
		req.Header.Set("Authorization", "Bearer "+token)
		rec := httptest.NewRecorder()

		srv.Router().ServeHTTP(rec, req)

		assert.Equal(t, http.StatusInternalServerError, rec.Code)
	})

	t.Run("runners not configured", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/runners", nil)
		req.Header.Set("Authorization", "Bearer "+token)
		rec := httptest.NewRecorder()

		srv.Router().ServeHTTP(rec, req)

		assert.Equal(t, http.StatusInternalServerError, rec.Code)
	})

	t.Run("permissions not configured", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/permissions", nil)
		req.Header.Set("Authorization", "Bearer "+token)
		rec := httptest.NewRecorder()

		srv.Router().ServeHTTP(rec, req)

		assert.Equal(t, http.StatusInternalServerError, rec.Code)
	})
}

func TestParseLabelsQuery(t *testing.T) {
	tests := []struct {
		name     string
		query    string
		expected map[string]string
	}{
		{
			name:     "no labels",
			query:    "",
			expected: nil,
		},
		{
			name:     "single label",
			query:    "labels[env]=prod",
			expected: map[string]string{"env": "prod"},
		},
		{
			name:     "multiple labels",
			query:    "labels[env]=prod&labels[team]=backend",
			expected: map[string]string{"env": "prod", "team": "backend"},
		},
		{
			name:     "mixed params",
			query:    "status=active&labels[env]=prod&limit=10",
			expected: map[string]string{"env": "prod"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/test?"+tt.query, nil)
			result := parseLabelsQuery(req)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestHandleServiceError(t *testing.T) {
	tests := []struct {
		name           string
		err            error
		expectedStatus int
		expectedCode   string
	}{
		{
			name:           "not found error",
			err:            store.ErrNotFound,
			expectedStatus: http.StatusNotFound,
			expectedCode:   "not_found",
		},
		{
			name:           "validation error",
			err:            &ValidationError{Field: "name", Message: "required"},
			expectedStatus: http.StatusBadRequest,
			expectedCode:   "validation_error",
		},
		{
			name:           "invalid state error",
			err:            &InvalidStateError{Resource: "session", ID: "sess_123", Current: "terminated", Expected: "active"},
			expectedStatus: http.StatusConflict,
			expectedCode:   "invalid_state",
		},
		{
			name:           "not authorized error",
			err:            &NotAuthorizedError{Operation: "delete", Resource: "session", ID: "sess_123"},
			expectedStatus: http.StatusForbidden,
			expectedCode:   "forbidden",
		},
		{
			name:           "max retries exceeded error",
			err:            &MaxRetriesExceededError{TaskID: "task_123", RetryCount: 3, MaxRetries: 3},
			expectedStatus: http.StatusConflict,
			expectedCode:   "max_retries_exceeded",
		},
		{
			name:           "generic error",
			err:            errors.New("unknown error"),
			expectedStatus: http.StatusInternalServerError,
			expectedCode:   "internal_error",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			handleServiceError(rec, tt.err)

			assert.Equal(t, tt.expectedStatus, rec.Code)
			var resp apitypes.ErrorResponse
			err := json.NewDecoder(rec.Body).Decode(&resp)
			require.NoError(t, err)
			assert.Equal(t, tt.expectedCode, resp.Code)
		})
	}
}

func TestSessionsInvalidJSON(t *testing.T) {
	sessionSvc := NewMockSessionService()
	srv, _, token := testServer(t, WithSessionService(sessionSvc))

	req := httptest.NewRequest(http.MethodPost, "/api/v1/sessions", bytes.NewBufferString("{invalid json"))
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	srv.Router().ServeHTTP(rec, req)

	assert.Equal(t, http.StatusBadRequest, rec.Code)
	var resp apitypes.ErrorResponse
	err := json.NewDecoder(rec.Body).Decode(&resp)
	require.NoError(t, err)
	assert.Equal(t, "invalid_json", resp.Code)
}

func TestTasksInvalidJSON(t *testing.T) {
	taskSvc := NewMockTaskService()
	srv, _, token := testServer(t, WithTaskService(taskSvc))

	req := httptest.NewRequest(http.MethodPost, "/api/v1/tasks", bytes.NewBufferString("{invalid json"))
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	srv.Router().ServeHTTP(rec, req)

	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestPermissionsInvalidJSON(t *testing.T) {
	permSvc := NewMockPermissionService()
	srv, _, token := testServer(t, WithPermissionService(permSvc))

	permSvc.AddPermission(&store.PermissionRequest{
		ID:        "perm_test123",
		SessionID: "sess_test123",
		TaskID:    "task_test123",
		RunID:     "trun_test123",
		Tool:      "bash",
		Action:    "test",
		Status:    "pending",
		RiskLevel: "medium",
		CreatedAt: time.Now(),
	})

	t.Run("approve invalid json", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/permissions/perm_test123/approve", bytes.NewBufferString("{invalid"))
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()

		srv.Router().ServeHTTP(rec, req)

		assert.Equal(t, http.StatusBadRequest, rec.Code)
	})

	t.Run("deny invalid json", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/permissions/perm_test123/deny", bytes.NewBufferString("{invalid"))
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()

		srv.Router().ServeHTTP(rec, req)

		assert.Equal(t, http.StatusBadRequest, rec.Code)
	})

	t.Run("approve empty body", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/permissions/perm_test123/approve", nil)
		req.Header.Set("Authorization", "Bearer "+token)
		rec := httptest.NewRecorder()

		srv.Router().ServeHTTP(rec, req)

		// Empty body should be OK (optional reason)
		assert.Equal(t, http.StatusNoContent, rec.Code)
	})
}

func TestMiddlewareEdgeCases(t *testing.T) {
	sessionSvc := NewMockSessionService()

	t.Run("no api key service configured", func(t *testing.T) {
		logger := zap.NewNop()
		srv := New(Config{Host: "localhost", Port: 8080}, logger,
			WithSessionService(sessionSvc),
			// Intentionally NOT setting WithAPIKeyService
		)

		req := httptest.NewRequest(http.MethodGet, "/api/v1/sessions", nil)
		req.Header.Set("Authorization", "Bearer mk_test_token_12345678901234567890")
		rec := httptest.NewRecorder()

		srv.Router().ServeHTTP(rec, req)

		assert.Equal(t, http.StatusInternalServerError, rec.Code)
	})
}

func TestWriteJSON(t *testing.T) {
	rec := httptest.NewRecorder()
	data := map[string]string{"key": "value"}
	WriteJSON(rec, http.StatusOK, data)

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "application/json", rec.Header().Get("Content-Type"))
}

func TestWriteError(t *testing.T) {
	rec := httptest.NewRecorder()
	WriteError(rec, http.StatusBadRequest, "test_error", "Test error message")

	assert.Equal(t, http.StatusBadRequest, rec.Code)
	var resp apitypes.ErrorResponse
	err := json.NewDecoder(rec.Body).Decode(&resp)
	require.NoError(t, err)
	assert.Equal(t, "test_error", resp.Code)
	assert.Equal(t, "Test error message", resp.Message)
}

func TestGetAPIKey(t *testing.T) {
	t.Run("no api key in context", func(t *testing.T) {
		ctx := context.Background()
		key := GetAPIKey(ctx)
		assert.Nil(t, key)
	})

	t.Run("api key in context", func(t *testing.T) {
		apiKey := &store.APIKey{ID: "key_123", Name: "test"}
		ctx := context.WithValue(context.Background(), APIKeyContextKey, apiKey)
		key := GetAPIKey(ctx)
		assert.Equal(t, "key_123", key.ID)
	})
}

func TestParseIntQuery(t *testing.T) {
	tests := []struct {
		name       string
		query      string
		key        string
		defaultVal int
		expected   int
	}{
		{
			name:       "missing param uses default",
			query:      "",
			key:        "limit",
			defaultVal: 50,
			expected:   50,
		},
		{
			name:       "valid param",
			query:      "limit=100",
			key:        "limit",
			defaultVal: 50,
			expected:   100,
		},
		{
			name:       "invalid param uses default",
			query:      "limit=abc",
			key:        "limit",
			defaultVal: 50,
			expected:   50,
		},
		{
			name:       "negative param uses default",
			query:      "limit=-5",
			key:        "limit",
			defaultVal: 50,
			expected:   50,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/test?"+tt.query, nil)
			result := parseIntQuery(req, tt.key, tt.defaultVal)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestErrorTypes(t *testing.T) {
	t.Run("ValidationError", func(t *testing.T) {
		t.Run("with field", func(t *testing.T) {
			err := &ValidationError{Field: "name", Message: "required"}
			assert.Equal(t, "validation error: name: required", err.Error())
			assert.True(t, IsValidation(err))
		})
		t.Run("without field", func(t *testing.T) {
			err := &ValidationError{Message: "invalid request"}
			assert.Equal(t, "validation error: invalid request", err.Error())
			assert.True(t, IsValidation(err))
		})
		t.Run("not validation error", func(t *testing.T) {
			assert.False(t, IsValidation(errors.New("some error")))
		})
	})

	t.Run("NotAuthorizedError", func(t *testing.T) {
		t.Run("with ID", func(t *testing.T) {
			err := &NotAuthorizedError{Operation: "delete", Resource: "session", ID: "sess_123"}
			assert.Equal(t, "not authorized to delete session sess_123", err.Error())
			assert.True(t, IsNotAuthorized(err))
		})
		t.Run("without ID", func(t *testing.T) {
			err := &NotAuthorizedError{Operation: "list", Resource: "sessions"}
			assert.Equal(t, "not authorized to list sessions", err.Error())
			assert.True(t, IsNotAuthorized(err))
		})
		t.Run("not authorized error", func(t *testing.T) {
			assert.False(t, IsNotAuthorized(errors.New("some error")))
		})
	})

	t.Run("InvalidStateError", func(t *testing.T) {
		err := &InvalidStateError{Resource: "session", ID: "sess_123", Current: "terminated", Expected: "active"}
		assert.Contains(t, err.Error(), "sess_123")
		assert.True(t, IsInvalidState(err))
		assert.False(t, IsInvalidState(errors.New("some error")))
	})

	t.Run("MaxRetriesExceededError", func(t *testing.T) {
		err := &MaxRetriesExceededError{TaskID: "task_123", RetryCount: 3, MaxRetries: 3}
		assert.Contains(t, err.Error(), "task_123")
		assert.True(t, IsMaxRetriesExceeded(err))
		assert.False(t, IsMaxRetriesExceeded(errors.New("some error")))
	})
}

func TestServerConfig(t *testing.T) {
	logger := zap.NewNop()

	t.Run("creates server with default config", func(t *testing.T) {
		srv := New(Config{Host: "127.0.0.1", Port: 9999}, logger)
		assert.NotNil(t, srv)
		assert.NotNil(t, srv.Router())
	})

	t.Run("creates server with all options", func(t *testing.T) {
		sessionSvc := NewMockSessionService()
		taskSvc := NewMockTaskService()
		runnerSvc := NewMockRunnerService()
		permSvc := NewMockPermissionService()

		srv := New(Config{Host: "localhost", Port: 8080}, logger,
			WithSessionService(sessionSvc),
			WithTaskService(taskSvc),
			WithRunnerService(runnerSvc),
			WithPermissionService(permSvc),
		)

		assert.NotNil(t, srv)
	})
}

func TestListFiltersWithLabels(t *testing.T) {
	sessionSvc := NewMockSessionService()
	sessionSvc.AddSession(&store.Session{
		ID:     "sess_labeled",
		Status: "active",
		Agent:  "claude",
		Labels: json.RawMessage(`{"env": "prod", "team": "backend"}`),
	})

	srv, _, token := testServer(t, WithSessionService(sessionSvc))

	t.Run("list with label filters", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/sessions?labels[env]=prod&labels[team]=backend", nil)
		req.Header.Set("Authorization", "Bearer "+token)
		rec := httptest.NewRecorder()

		srv.Router().ServeHTTP(rec, req)

		assert.Equal(t, http.StatusOK, rec.Code)
	})

	t.Run("list with lifecycle_mode filter", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/sessions?lifecycle_mode=on_demand", nil)
		req.Header.Set("Authorization", "Bearer "+token)
		rec := httptest.NewRecorder()

		srv.Router().ServeHTTP(rec, req)

		assert.Equal(t, http.StatusOK, rec.Code)
	})
}

func TestTaskListFilters(t *testing.T) {
	taskSvc := NewMockTaskService()
	taskSvc.AddTask(&store.Task{
		ID:        "task_test",
		SessionID: "sess_test",
		Status:    "running",
		Prompt:    "test",
		Labels:    json.RawMessage(`{"priority": "high"}`),
	})

	srv, _, token := testServer(t, WithTaskService(taskSvc))

	t.Run("list with status filter", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/tasks?status=running&status=pending", nil)
		req.Header.Set("Authorization", "Bearer "+token)
		rec := httptest.NewRecorder()

		srv.Router().ServeHTTP(rec, req)

		assert.Equal(t, http.StatusOK, rec.Code)
	})

	t.Run("list with labels filter", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/tasks?labels[priority]=high", nil)
		req.Header.Set("Authorization", "Bearer "+token)
		rec := httptest.NewRecorder()

		srv.Router().ServeHTTP(rec, req)

		assert.Equal(t, http.StatusOK, rec.Code)
	})

	t.Run("get logs with after parameter", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/tasks/task_test/logs?after=1234567890", nil)
		req.Header.Set("Authorization", "Bearer "+token)
		rec := httptest.NewRecorder()

		srv.Router().ServeHTTP(rec, req)

		assert.Equal(t, http.StatusOK, rec.Code)
	})
}

func TestPermissionListFilters(t *testing.T) {
	permSvc := NewMockPermissionService()
	permSvc.AddPermission(&store.PermissionRequest{
		ID:        "perm_test",
		SessionID: "sess_test",
		TaskID:    "task_test",
		RunID:     "trun_test",
		Tool:      "bash",
		Action:    "test",
		Status:    "pending",
		RiskLevel: "high",
	})

	srv, _, token := testServer(t, WithPermissionService(permSvc))

	t.Run("list with risk_level filter", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/permissions?risk_level=high&risk_level=critical", nil)
		req.Header.Set("Authorization", "Bearer "+token)
		rec := httptest.NewRecorder()

		srv.Router().ServeHTTP(rec, req)

		assert.Equal(t, http.StatusOK, rec.Code)
	})

	t.Run("list with task_id filter", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/permissions?task_id=task_test", nil)
		req.Header.Set("Authorization", "Bearer "+token)
		rec := httptest.NewRecorder()

		srv.Router().ServeHTTP(rec, req)

		assert.Equal(t, http.StatusOK, rec.Code)
	})
}

func TestRunnerListFilters(t *testing.T) {
	runnerSvc := NewMockRunnerService()
	runnerSvc.AddRunner(&store.Runner{
		ID:       "run_test",
		Name:     "test-runner",
		Hostname: "localhost",
		Status:   "idle",
		PoolName: strPtr("default"),
		Labels:   json.RawMessage(`{"region": "us-west"}`),
	})

	srv, _, token := testServer(t, WithRunnerService(runnerSvc))

	t.Run("list with labels filter", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/runners?labels[region]=us-west", nil)
		req.Header.Set("Authorization", "Bearer "+token)
		rec := httptest.NewRecorder()

		srv.Router().ServeHTTP(rec, req)

		assert.Equal(t, http.StatusOK, rec.Code)
	})
}

func strPtr(s string) *string {
	return &s
}

func TestSessionActionsNotFound(t *testing.T) {
	sessionSvc := NewMockSessionService()
	srv, _, token := testServer(t, WithSessionService(sessionSvc))

	tests := []struct {
		name   string
		method string
		path   string
	}{
		{"suspend not found", http.MethodPost, "/api/v1/sessions/sess_nonexistent/suspend"},
		{"resume not found", http.MethodPost, "/api/v1/sessions/sess_nonexistent/resume"},
		{"terminate not found", http.MethodDelete, "/api/v1/sessions/sess_nonexistent"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(tt.method, tt.path, nil)
			req.Header.Set("Authorization", "Bearer "+token)
			rec := httptest.NewRecorder()

			srv.Router().ServeHTTP(rec, req)

			assert.Equal(t, http.StatusNotFound, rec.Code)
		})
	}
}

func TestTaskActionsNotFound(t *testing.T) {
	taskSvc := NewMockTaskService()
	srv, _, token := testServer(t, WithTaskService(taskSvc))

	tests := []struct {
		name   string
		method string
		path   string
	}{
		{"cancel not found", http.MethodPost, "/api/v1/tasks/task_nonexistent/cancel"},
		{"retry not found", http.MethodPost, "/api/v1/tasks/task_nonexistent/retry"},
		{"logs not found", http.MethodGet, "/api/v1/tasks/task_nonexistent/logs"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(tt.method, tt.path, nil)
			req.Header.Set("Authorization", "Bearer "+token)
			rec := httptest.NewRecorder()

			srv.Router().ServeHTTP(rec, req)

			assert.Equal(t, http.StatusNotFound, rec.Code)
		})
	}
}

func TestPermissionActionsNotFound(t *testing.T) {
	permSvc := NewMockPermissionService()
	srv, _, token := testServer(t, WithPermissionService(permSvc))

	tests := []struct {
		name   string
		method string
		path   string
	}{
		{"approve not found", http.MethodPost, "/api/v1/permissions/perm_nonexistent/approve"},
		{"deny not found", http.MethodPost, "/api/v1/permissions/perm_nonexistent/deny"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(tt.method, tt.path, nil)
			req.Header.Set("Authorization", "Bearer "+token)
			rec := httptest.NewRecorder()

			srv.Router().ServeHTTP(rec, req)

			assert.Equal(t, http.StatusNotFound, rec.Code)
		})
	}
}

func TestServiceUnavailableActions(t *testing.T) {
	srv, _, token := testServer(t) // No services configured

	tests := []struct {
		name   string
		method string
		path   string
	}{
		{"create session", http.MethodPost, "/api/v1/sessions"},
		{"get session", http.MethodGet, "/api/v1/sessions/sess_test"},
		{"suspend session", http.MethodPost, "/api/v1/sessions/sess_test/suspend"},
		{"resume session", http.MethodPost, "/api/v1/sessions/sess_test/resume"},
		{"terminate session", http.MethodDelete, "/api/v1/sessions/sess_test"},
		{"create task", http.MethodPost, "/api/v1/tasks"},
		{"get task", http.MethodGet, "/api/v1/tasks/task_test"},
		{"cancel task", http.MethodPost, "/api/v1/tasks/task_test/cancel"},
		{"retry task", http.MethodPost, "/api/v1/tasks/task_test/retry"},
		{"get task logs", http.MethodGet, "/api/v1/tasks/task_test/logs"},
		{"get runner", http.MethodGet, "/api/v1/runners/run_test"},
		{"get permission", http.MethodGet, "/api/v1/permissions/perm_test"},
		{"approve permission", http.MethodPost, "/api/v1/permissions/perm_test/approve"},
		{"deny permission", http.MethodPost, "/api/v1/permissions/perm_test/deny"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var body *bytes.Buffer
			if tt.method == http.MethodPost && (tt.path == "/api/v1/sessions" || tt.path == "/api/v1/tasks") {
				body = bytes.NewBufferString(`{"agent": "claude", "prompt": "test"}`)
			}
			var req *http.Request
			if body != nil {
				req = httptest.NewRequest(tt.method, tt.path, body)
				req.Header.Set("Content-Type", "application/json")
			} else {
				req = httptest.NewRequest(tt.method, tt.path, nil)
			}
			req.Header.Set("Authorization", "Bearer "+token)
			rec := httptest.NewRecorder()

			srv.Router().ServeHTTP(rec, req)

			assert.Equal(t, http.StatusInternalServerError, rec.Code)
			var resp apitypes.ErrorResponse
			err := json.NewDecoder(rec.Body).Decode(&resp)
			require.NoError(t, err)
			assert.Equal(t, "service_unavailable", resp.Code)
		})
	}
}

func TestAuthMiddlewareRevoked(t *testing.T) {
	sessionSvc := NewMockSessionService()
	logger := zap.NewNop()
	keyStore := newMockAPIKeyStore()
	apiKeyService := auth.NewAPIKeyService(keyStore, func() string { return "key_test123" })

	// Create and then revoke an API key
	key, token, err := apiKeyService.Create(context.Background(), auth.CreateAPIKeyOptions{
		Name:   "revoked-key",
		Scopes: []string{"*"},
	})
	require.NoError(t, err)

	// Revoke the key
	now := time.Now()
	err = keyStore.UpdateAPIKey(context.Background(), key.ID, store.APIKeyUpdates{
		RevokedAt:    &now,
		RevokeReason: strPtr("test revocation"),
	})
	require.NoError(t, err)

	srv := New(Config{Host: "localhost", Port: 8080}, logger,
		WithAPIKeyService(apiKeyService),
		WithSessionService(sessionSvc),
	)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/sessions", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()

	srv.Router().ServeHTTP(rec, req)

	assert.Equal(t, http.StatusUnauthorized, rec.Code)
}

func TestAuthMiddlewareExpired(t *testing.T) {
	sessionSvc := NewMockSessionService()
	logger := zap.NewNop()
	keyStore := newMockAPIKeyStore()
	apiKeyService := auth.NewAPIKeyService(keyStore, func() string { return "key_test123" })

	// Create an expired API key
	pastDate := time.Now().Add(-24 * time.Hour)
	_, token, err := apiKeyService.Create(context.Background(), auth.CreateAPIKeyOptions{
		Name:      "expired-key",
		Scopes:    []string{"*"},
		ExpiresAt: &pastDate,
	})
	require.NoError(t, err)

	srv := New(Config{Host: "localhost", Port: 8080}, logger,
		WithAPIKeyService(apiKeyService),
		WithSessionService(sessionSvc),
	)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/sessions", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()

	srv.Router().ServeHTTP(rec, req)

	assert.Equal(t, http.StatusUnauthorized, rec.Code)
}

func TestOpenAPIDocumentation(t *testing.T) {
	srv, _, _ := testServer(t)

	t.Run("swagger UI returns HTML", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/docs", nil)
		rec := httptest.NewRecorder()

		srv.Router().ServeHTTP(rec, req)

		assert.Equal(t, http.StatusOK, rec.Code)
		assert.Equal(t, "text/html; charset=utf-8", rec.Header().Get("Content-Type"))
		assert.Contains(t, rec.Body.String(), "<!DOCTYPE html>")
		assert.Contains(t, rec.Body.String(), "swagger-ui")
		assert.Contains(t, rec.Body.String(), "Marionette API Documentation")
	})

	t.Run("openapi spec returns YAML", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/openapi.yaml", nil)
		rec := httptest.NewRecorder()

		srv.Router().ServeHTTP(rec, req)

		assert.Equal(t, http.StatusOK, rec.Code)
		assert.Equal(t, "application/yaml", rec.Header().Get("Content-Type"))
		assert.Equal(t, "*", rec.Header().Get("Access-Control-Allow-Origin"))
		assert.Contains(t, rec.Body.String(), "openapi:")
		assert.Contains(t, rec.Body.String(), "Marionette Public API")
	})
}

func TestWorkspaces(t *testing.T) {
	workspaceSvc := NewMockWorkspaceService()
	srv, _, token := testServer(t, WithWorkspaceService(workspaceSvc))

	// Add a test workspace
	testWorkspace := &store.Workspace{
		ID:          "ws_test123",
		Name:        "test-workspace",
		Persist:     true,
		StorageType: "volume",
		Mobility:    "local",
		Labels:      json.RawMessage(`{}`),
		Annotations: json.RawMessage(`{}`),
		CreatedAt:   time.Now(),
		UpdatedAt:   time.Now(),
	}
	workspaceSvc.AddWorkspace(testWorkspace)

	t.Run("create workspace", func(t *testing.T) {
		body := `{"name": "new-workspace"}`
		req := httptest.NewRequest(http.MethodPost, "/api/v1/workspaces", bytes.NewBufferString(body))
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()

		srv.Router().ServeHTTP(rec, req)

		assert.Equal(t, http.StatusCreated, rec.Code)
	})

	t.Run("create workspace with options", func(t *testing.T) {
		body := `{"name": "workspace-with-opts", "persist": false, "storage_type": "volume", "disk_quota_mb": 2048}`
		req := httptest.NewRequest(http.MethodPost, "/api/v1/workspaces", bytes.NewBufferString(body))
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()

		srv.Router().ServeHTTP(rec, req)

		assert.Equal(t, http.StatusCreated, rec.Code)
	})

	t.Run("create workspace invalid json", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/workspaces", bytes.NewBufferString("{invalid json"))
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()

		srv.Router().ServeHTTP(rec, req)

		assert.Equal(t, http.StatusBadRequest, rec.Code)
		var resp apitypes.ErrorResponse
		err := json.NewDecoder(rec.Body).Decode(&resp)
		require.NoError(t, err)
		assert.Equal(t, "invalid_json", resp.Code)
	})

	t.Run("list workspaces", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/workspaces", nil)
		req.Header.Set("Authorization", "Bearer "+token)
		rec := httptest.NewRecorder()

		srv.Router().ServeHTTP(rec, req)

		assert.Equal(t, http.StatusOK, rec.Code)
	})

	t.Run("list workspaces with limit", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/workspaces?limit=10", nil)
		req.Header.Set("Authorization", "Bearer "+token)
		rec := httptest.NewRecorder()

		srv.Router().ServeHTTP(rec, req)

		assert.Equal(t, http.StatusOK, rec.Code)
	})

	t.Run("get workspace", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/workspaces/ws_test123", nil)
		req.Header.Set("Authorization", "Bearer "+token)
		rec := httptest.NewRecorder()

		srv.Router().ServeHTTP(rec, req)

		assert.Equal(t, http.StatusOK, rec.Code)
	})

	t.Run("get workspace not found", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/workspaces/ws_notfound", nil)
		req.Header.Set("Authorization", "Bearer "+token)
		rec := httptest.NewRecorder()

		srv.Router().ServeHTTP(rec, req)

		assert.Equal(t, http.StatusNotFound, rec.Code)
	})

	t.Run("update workspace", func(t *testing.T) {
		body := `{"name": "updated-workspace"}`
		req := httptest.NewRequest(http.MethodPatch, "/api/v1/workspaces/ws_test123", bytes.NewBufferString(body))
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()

		srv.Router().ServeHTTP(rec, req)

		assert.Equal(t, http.StatusOK, rec.Code)
	})

	t.Run("update workspace not found", func(t *testing.T) {
		body := `{"name": "test"}`
		req := httptest.NewRequest(http.MethodPatch, "/api/v1/workspaces/ws_notfound", bytes.NewBufferString(body))
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()

		srv.Router().ServeHTTP(rec, req)

		assert.Equal(t, http.StatusNotFound, rec.Code)
	})

	t.Run("update workspace invalid json", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPatch, "/api/v1/workspaces/ws_test123", bytes.NewBufferString("{invalid"))
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()

		srv.Router().ServeHTTP(rec, req)

		assert.Equal(t, http.StatusBadRequest, rec.Code)
	})

	t.Run("delete workspace", func(t *testing.T) {
		// Create a workspace to delete
		delWorkspace := &store.Workspace{
			ID:          "ws_todelete",
			Name:        "to-delete",
			Persist:     true,
			StorageType: "volume",
			Mobility:    "local",
			Labels:      json.RawMessage(`{}`),
			Annotations: json.RawMessage(`{}`),
			CreatedAt:   time.Now(),
		}
		workspaceSvc.AddWorkspace(delWorkspace)

		req := httptest.NewRequest(http.MethodDelete, "/api/v1/workspaces/ws_todelete", nil)
		req.Header.Set("Authorization", "Bearer "+token)
		rec := httptest.NewRecorder()

		srv.Router().ServeHTTP(rec, req)

		assert.Equal(t, http.StatusNoContent, rec.Code)
	})

	t.Run("delete workspace not found", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodDelete, "/api/v1/workspaces/ws_notfound", nil)
		req.Header.Set("Authorization", "Bearer "+token)
		rec := httptest.NewRecorder()

		srv.Router().ServeHTTP(rec, req)

		assert.Equal(t, http.StatusNotFound, rec.Code)
	})

	t.Run("get deleted workspace returns gone", func(t *testing.T) {
		// Create and delete a workspace
		goneWorkspace := &store.Workspace{
			ID:          "ws_gone",
			Name:        "gone-workspace",
			Persist:     true,
			StorageType: "volume",
			Mobility:    "local",
			Labels:      json.RawMessage(`{}`),
			Annotations: json.RawMessage(`{}`),
			CreatedAt:   time.Now(),
		}
		workspaceSvc.AddWorkspace(goneWorkspace)

		// Delete it first
		delReq := httptest.NewRequest(http.MethodDelete, "/api/v1/workspaces/ws_gone", nil)
		delReq.Header.Set("Authorization", "Bearer "+token)
		delRec := httptest.NewRecorder()
		srv.Router().ServeHTTP(delRec, delReq)

		// Now try to get it
		req := httptest.NewRequest(http.MethodGet, "/api/v1/workspaces/ws_gone", nil)
		req.Header.Set("Authorization", "Bearer "+token)
		rec := httptest.NewRecorder()

		srv.Router().ServeHTTP(rec, req)

		assert.Equal(t, http.StatusGone, rec.Code)
	})
}

func TestWorkspaceServiceUnavailable(t *testing.T) {
	srv, _, token := testServer(t) // No workspace service configured

	tests := []struct {
		name   string
		method string
		path   string
		body   string
	}{
		{"create workspace", http.MethodPost, "/api/v1/workspaces", `{"name": "test"}`},
		{"list workspaces", http.MethodGet, "/api/v1/workspaces", ""},
		{"get workspace", http.MethodGet, "/api/v1/workspaces/ws_test", ""},
		{"update workspace", http.MethodPatch, "/api/v1/workspaces/ws_test", `{"name": "test"}`},
		{"delete workspace", http.MethodDelete, "/api/v1/workspaces/ws_test", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var req *http.Request
			if tt.body != "" {
				req = httptest.NewRequest(tt.method, tt.path, bytes.NewBufferString(tt.body))
				req.Header.Set("Content-Type", "application/json")
			} else {
				req = httptest.NewRequest(tt.method, tt.path, nil)
			}
			req.Header.Set("Authorization", "Bearer "+token)
			rec := httptest.NewRecorder()

			srv.Router().ServeHTTP(rec, req)

			assert.Equal(t, http.StatusInternalServerError, rec.Code)
			var resp apitypes.ErrorResponse
			err := json.NewDecoder(rec.Body).Decode(&resp)
			require.NoError(t, err)
			assert.Equal(t, "service_unavailable", resp.Code)
		})
	}
}

func TestWorkspaceScopeMiddleware(t *testing.T) {
	workspaceSvc := NewMockWorkspaceService()
	logger := zap.NewNop()
	keyStore := newMockAPIKeyStore()
	apiKeyService := auth.NewAPIKeyService(keyStore, func() string { return "key_test123" })

	// Create API key with limited scope (only read)
	_, token, err := apiKeyService.Create(context.Background(), auth.CreateAPIKeyOptions{
		Name:   "read-only-key",
		Scopes: []string{"workspaces:read"},
	})
	require.NoError(t, err)

	srv := New(Config{Host: "localhost", Port: 8080}, logger,
		WithAPIKeyService(apiKeyService),
		WithWorkspaceService(workspaceSvc),
	)

	t.Run("allowed scope - list", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/workspaces", nil)
		req.Header.Set("Authorization", "Bearer "+token)
		rec := httptest.NewRecorder()

		srv.Router().ServeHTTP(rec, req)

		assert.Equal(t, http.StatusOK, rec.Code)
	})

	t.Run("forbidden scope - create", func(t *testing.T) {
		body := `{"name": "test"}`
		req := httptest.NewRequest(http.MethodPost, "/api/v1/workspaces", bytes.NewBufferString(body))
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()

		srv.Router().ServeHTTP(rec, req)

		assert.Equal(t, http.StatusForbidden, rec.Code)
	})

	t.Run("forbidden scope - delete", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodDelete, "/api/v1/workspaces/ws_test", nil)
		req.Header.Set("Authorization", "Bearer "+token)
		rec := httptest.NewRecorder()

		srv.Router().ServeHTTP(rec, req)

		assert.Equal(t, http.StatusForbidden, rec.Code)
	})
}

func TestTunnelProxyRedirect(t *testing.T) {
	// Create a mock tunnel proxy service that returns 404 for all tunnels
	mockSvc := &mockTunnelProxyService{
		validateTunnelFn: func(ctx context.Context, tunnelID string) (*TunnelInfo, error) {
			return nil, errors.New("tunnel not found")
		},
	}

	// Create a tunnel proxy handler with the mock service
	tunnelProxy := NewTunnelProxyHandler(
		WithTPLogger(zap.NewNop()),
		WithTPService(mockSvc),
	)

	srv, _, _ := testServer(t, WithTunnelProxy(tunnelProxy))

	t.Run("redirects tunnel path without trailing slash", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/tunnels/tun_test123", nil)
		rec := httptest.NewRecorder()

		srv.Router().ServeHTTP(rec, req)

		assert.Equal(t, http.StatusMovedPermanently, rec.Code)
		assert.Equal(t, "/tunnels/tun_test123/", rec.Header().Get("Location"))
	})

	t.Run("does not redirect tunnel path with trailing slash", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/tunnels/tun_test123/", nil)
		rec := httptest.NewRecorder()

		srv.Router().ServeHTTP(rec, req)

		// Should not be a redirect, should go to tunnel proxy (which returns 404 because mock says tunnel not found)
		assert.NotEqual(t, http.StatusMovedPermanently, rec.Code)
		assert.Equal(t, http.StatusNotFound, rec.Code)
	})

	t.Run("does not redirect subpaths", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/tunnels/tun_test123/some/path", nil)
		rec := httptest.NewRecorder()

		srv.Router().ServeHTTP(rec, req)

		// Should not be a redirect, should go to tunnel proxy (which returns 404 because mock says tunnel not found)
		assert.NotEqual(t, http.StatusMovedPermanently, rec.Code)
		assert.Equal(t, http.StatusNotFound, rec.Code)
	})
}

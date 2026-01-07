package admin

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/chunlea/marionette/pkg/store"
)

func TestActionLogHandlers(t *testing.T) {
	mockService := NewMockActionLogService()
	srv := newTestServer(WithActionLogService(mockService))

	// Add test data
	testLog := &store.ActionLog{
		ID:           "alog_test123",
		ActorType:    "api_key",
		Action:       "permission.approved",
		ResourceType: "permission_request",
		ResourceID:   "perm_abc",
		Success:      true,
		CreatedAt:    time.Now(),
	}
	mockService.AddLog(testLog)

	t.Run("list action logs", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/admin/api/v1/action-logs", nil)
		req.SetBasicAuth("admin", "secret")

		rr := httptest.NewRecorder()
		srv.Router().ServeHTTP(rr, req)

		if rr.Code != http.StatusOK {
			t.Errorf("expected status %d, got %d: %s", http.StatusOK, rr.Code, rr.Body.String())
		}

		var result ListResult[store.ActionLog]
		if err := json.NewDecoder(rr.Body).Decode(&result); err != nil {
			t.Fatalf("failed to decode response: %v", err)
		}

		if len(result.Items) != 1 {
			t.Errorf("expected 1 item, got %d", len(result.Items))
		}
		if result.TotalCount != 1 {
			t.Errorf("expected total count 1, got %d", result.TotalCount)
		}
	})

	t.Run("list action logs with limit", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/admin/api/v1/action-logs?limit=10", nil)
		req.SetBasicAuth("admin", "secret")

		rr := httptest.NewRecorder()
		srv.Router().ServeHTTP(rr, req)

		if rr.Code != http.StatusOK {
			t.Errorf("expected status %d, got %d", http.StatusOK, rr.Code)
		}
	})

	t.Run("list action logs with cursor", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/admin/api/v1/action-logs?cursor=alog_prev", nil)
		req.SetBasicAuth("admin", "secret")

		rr := httptest.NewRecorder()
		srv.Router().ServeHTTP(rr, req)

		if rr.Code != http.StatusOK {
			t.Errorf("expected status %d, got %d", http.StatusOK, rr.Code)
		}
	})

	t.Run("get action log", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/admin/api/v1/action-logs/alog_test123", nil)
		req.SetBasicAuth("admin", "secret")

		rr := httptest.NewRecorder()
		srv.Router().ServeHTTP(rr, req)

		if rr.Code != http.StatusOK {
			t.Errorf("expected status %d, got %d: %s", http.StatusOK, rr.Code, rr.Body.String())
		}

		var log store.ActionLog
		if err := json.NewDecoder(rr.Body).Decode(&log); err != nil {
			t.Fatalf("failed to decode response: %v", err)
		}

		if log.ID != "alog_test123" {
			t.Errorf("expected log ID 'alog_test123', got %q", log.ID)
		}
		if log.Action != "permission.approved" {
			t.Errorf("expected action 'permission.approved', got %q", log.Action)
		}
	})

	t.Run("get action log - not found", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/admin/api/v1/action-logs/alog_notexist", nil)
		req.SetBasicAuth("admin", "secret")

		rr := httptest.NewRecorder()
		srv.Router().ServeHTTP(rr, req)

		if rr.Code != http.StatusNotFound {
			t.Errorf("expected status %d, got %d", http.StatusNotFound, rr.Code)
		}

		var errResp ErrorResponse
		if err := json.NewDecoder(rr.Body).Decode(&errResp); err != nil {
			t.Fatalf("failed to decode error response: %v", err)
		}

		if errResp.Code != "not_found" {
			t.Errorf("expected error code 'not_found', got %q", errResp.Code)
		}
	})

	t.Run("list action logs - internal error", func(t *testing.T) {
		mockService.SetInternalError(errors.New("database error"))
		defer mockService.SetInternalError(nil)

		req := httptest.NewRequest(http.MethodGet, "/admin/api/v1/action-logs", nil)
		req.SetBasicAuth("admin", "secret")

		rr := httptest.NewRecorder()
		srv.Router().ServeHTTP(rr, req)

		if rr.Code != http.StatusInternalServerError {
			t.Errorf("expected status %d, got %d", http.StatusInternalServerError, rr.Code)
		}
	})

	t.Run("get action log - internal error", func(t *testing.T) {
		mockService.SetInternalError(errors.New("database error"))
		defer mockService.SetInternalError(nil)

		req := httptest.NewRequest(http.MethodGet, "/admin/api/v1/action-logs/alog_test123", nil)
		req.SetBasicAuth("admin", "secret")

		rr := httptest.NewRecorder()
		srv.Router().ServeHTTP(rr, req)

		if rr.Code != http.StatusInternalServerError {
			t.Errorf("expected status %d, got %d", http.StatusInternalServerError, rr.Code)
		}
	})
}

func TestActionLogHandlers_ServiceNotConfigured(t *testing.T) {
	// Create server without action log service
	srv := newTestServer()

	t.Run("list action logs - service not configured", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/admin/api/v1/action-logs", nil)
		req.SetBasicAuth("admin", "secret")

		rr := httptest.NewRecorder()
		srv.Router().ServeHTTP(rr, req)

		if rr.Code != http.StatusNotImplemented {
			t.Errorf("expected status %d, got %d", http.StatusNotImplemented, rr.Code)
		}

		var errResp ErrorResponse
		if err := json.NewDecoder(rr.Body).Decode(&errResp); err != nil {
			t.Fatalf("failed to decode error response: %v", err)
		}

		if errResp.Code != "not_implemented" {
			t.Errorf("expected error code 'not_implemented', got %q", errResp.Code)
		}
	})

	t.Run("get action log - service not configured", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/admin/api/v1/action-logs/alog_test123", nil)
		req.SetBasicAuth("admin", "secret")

		rr := httptest.NewRecorder()
		srv.Router().ServeHTTP(rr, req)

		if rr.Code != http.StatusNotImplemented {
			t.Errorf("expected status %d, got %d", http.StatusNotImplemented, rr.Code)
		}
	})
}

func TestActionLogHandlers_RequiresAuth(t *testing.T) {
	mockService := NewMockActionLogService()
	srv := newTestServer(WithActionLogService(mockService))

	t.Run("list action logs - no auth", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/admin/api/v1/action-logs", nil)
		// No basic auth set

		rr := httptest.NewRecorder()
		srv.Router().ServeHTTP(rr, req)

		if rr.Code != http.StatusUnauthorized {
			t.Errorf("expected status %d, got %d", http.StatusUnauthorized, rr.Code)
		}
	})

	t.Run("get action log - no auth", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/admin/api/v1/action-logs/alog_test123", nil)
		// No basic auth set

		rr := httptest.NewRecorder()
		srv.Router().ServeHTTP(rr, req)

		if rr.Code != http.StatusUnauthorized {
			t.Errorf("expected status %d, got %d", http.StatusUnauthorized, rr.Code)
		}
	})
}

func TestActionLogHandlers_Filters(t *testing.T) {
	mockService := NewMockActionLogService()
	srv := newTestServer(WithActionLogService(mockService))

	// Add test data
	mockService.AddLog(&store.ActionLog{
		ID:           "alog_test1",
		ActorType:    "api_key",
		Action:       "permission.approved",
		ResourceType: "permission_request",
		ResourceID:   "perm_abc",
		Success:      true,
		CreatedAt:    time.Now(),
	})

	t.Run("list with actor_type filter", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/admin/api/v1/action-logs?actor_type=api_key", nil)
		req.SetBasicAuth("admin", "secret")

		rr := httptest.NewRecorder()
		srv.Router().ServeHTTP(rr, req)

		if rr.Code != http.StatusOK {
			t.Errorf("expected status %d, got %d", http.StatusOK, rr.Code)
		}
	})

	t.Run("list with action filter", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/admin/api/v1/action-logs?action=permission.approved", nil)
		req.SetBasicAuth("admin", "secret")

		rr := httptest.NewRecorder()
		srv.Router().ServeHTTP(rr, req)

		if rr.Code != http.StatusOK {
			t.Errorf("expected status %d, got %d", http.StatusOK, rr.Code)
		}
	})

	t.Run("list with action_prefix filter", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/admin/api/v1/action-logs?action_prefix=permission.", nil)
		req.SetBasicAuth("admin", "secret")

		rr := httptest.NewRecorder()
		srv.Router().ServeHTTP(rr, req)

		if rr.Code != http.StatusOK {
			t.Errorf("expected status %d, got %d", http.StatusOK, rr.Code)
		}
	})

	t.Run("list with resource filters", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/admin/api/v1/action-logs?resource_type=permission_request&resource_id=perm_abc", nil)
		req.SetBasicAuth("admin", "secret")

		rr := httptest.NewRecorder()
		srv.Router().ServeHTTP(rr, req)

		if rr.Code != http.StatusOK {
			t.Errorf("expected status %d, got %d", http.StatusOK, rr.Code)
		}
	})

	t.Run("list with session_id filter", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/admin/api/v1/action-logs?session_id=sess_abc", nil)
		req.SetBasicAuth("admin", "secret")

		rr := httptest.NewRecorder()
		srv.Router().ServeHTTP(rr, req)

		if rr.Code != http.StatusOK {
			t.Errorf("expected status %d, got %d", http.StatusOK, rr.Code)
		}
	})

	t.Run("list with success filter", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/admin/api/v1/action-logs?success=true", nil)
		req.SetBasicAuth("admin", "secret")

		rr := httptest.NewRecorder()
		srv.Router().ServeHTTP(rr, req)

		if rr.Code != http.StatusOK {
			t.Errorf("expected status %d, got %d", http.StatusOK, rr.Code)
		}
	})

	t.Run("list with time range filters", func(t *testing.T) {
		from := time.Now().Add(-24 * time.Hour).Format(time.RFC3339)
		to := time.Now().Format(time.RFC3339)
		req := httptest.NewRequest(http.MethodGet, "/admin/api/v1/action-logs?from="+from+"&to="+to, nil)
		req.SetBasicAuth("admin", "secret")

		rr := httptest.NewRecorder()
		srv.Router().ServeHTTP(rr, req)

		if rr.Code != http.StatusOK {
			t.Errorf("expected status %d, got %d", http.StatusOK, rr.Code)
		}
	})

	t.Run("list with invalid from time format", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/admin/api/v1/action-logs?from=invalid-time", nil)
		req.SetBasicAuth("admin", "secret")

		rr := httptest.NewRecorder()
		srv.Router().ServeHTTP(rr, req)

		if rr.Code != http.StatusBadRequest {
			t.Errorf("expected status %d, got %d", http.StatusBadRequest, rr.Code)
		}

		var errResp ErrorResponse
		if err := json.NewDecoder(rr.Body).Decode(&errResp); err != nil {
			t.Fatalf("failed to decode error response: %v", err)
		}

		if errResp.Code != "invalid_parameter" {
			t.Errorf("expected error code 'invalid_parameter', got %q", errResp.Code)
		}
	})

	t.Run("list with invalid to time format", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/admin/api/v1/action-logs?to=2024-01-01", nil)
		req.SetBasicAuth("admin", "secret")

		rr := httptest.NewRecorder()
		srv.Router().ServeHTTP(rr, req)

		if rr.Code != http.StatusBadRequest {
			t.Errorf("expected status %d, got %d", http.StatusBadRequest, rr.Code)
		}
	})

	t.Run("list with multiple filters", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/admin/api/v1/action-logs?actor_type=api_key&action=permission.approved&success=true", nil)
		req.SetBasicAuth("admin", "secret")

		rr := httptest.NewRecorder()
		srv.Router().ServeHTTP(rr, req)

		if rr.Code != http.StatusOK {
			t.Errorf("expected status %d, got %d", http.StatusOK, rr.Code)
		}
	})
}

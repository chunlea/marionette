package e2b

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestClientCreateSandbox(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodPost, r.Method)
		assert.Equal(t, "/sandboxes", r.URL.Path)
		assert.Equal(t, "application/json", r.Header.Get("Content-Type"))
		assert.Equal(t, "test-api-key", r.Header.Get("X-API-Key"))

		var req CreateSandboxRequest
		err := json.NewDecoder(r.Body).Decode(&req)
		require.NoError(t, err)
		assert.Equal(t, "base", req.TemplateID)
		assert.Equal(t, 300, req.Timeout)

		resp := CreateSandboxResponse{
			SandboxID:  "sandbox-123",
			TemplateID: "base",
			ClientID:   "client-456",
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	client := NewClient(server.URL, "test-api-key")
	resp, err := client.CreateSandbox(context.Background(), &CreateSandboxRequest{
		TemplateID: "base",
		Timeout:    300,
		Metadata: map[string]string{
			"runner-id": "run_123",
		},
	})

	require.NoError(t, err)
	assert.Equal(t, "sandbox-123", resp.SandboxID)
	assert.Equal(t, "base", resp.TemplateID)
	assert.Equal(t, "client-456", resp.ClientID)
}

func TestClientGetSandbox(t *testing.T) {
	startTime := time.Now().Truncate(time.Second)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodGet, r.Method)
		assert.Equal(t, "/sandboxes/sandbox-123", r.URL.Path)

		resp := Sandbox{
			SandboxID:  "sandbox-123",
			TemplateID: "base",
			ClientID:   "client-456",
			StartedAt:  startTime,
			Metadata: map[string]string{
				"runner-id": "run_123",
			},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	client := NewClient(server.URL, "test-api-key")
	sandbox, err := client.GetSandbox(context.Background(), "sandbox-123")

	require.NoError(t, err)
	assert.Equal(t, "sandbox-123", sandbox.SandboxID)
	assert.Equal(t, "base", sandbox.TemplateID)
	assert.Equal(t, "run_123", sandbox.Metadata["runner-id"])
}

func TestClientGetSandboxNotFound(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_ = json.NewEncoder(w).Encode(APIError{Message: "sandbox not found"})
	}))
	defer server.Close()

	client := NewClient(server.URL, "test-api-key")
	_, err := client.GetSandbox(context.Background(), "not-found")

	require.Error(t, err)
	assert.True(t, IsNotFoundError(err))
}

func TestClientListSandboxes(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodGet, r.Method)
		assert.Equal(t, "/sandboxes", r.URL.Path)

		resp := []Sandbox{
			{SandboxID: "sandbox-1", TemplateID: "base"},
			{SandboxID: "sandbox-2", TemplateID: "custom"},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	client := NewClient(server.URL, "test-api-key")
	sandboxes, err := client.ListSandboxes(context.Background())

	require.NoError(t, err)
	assert.Len(t, sandboxes, 2)
	assert.Equal(t, "sandbox-1", sandboxes[0].SandboxID)
	assert.Equal(t, "sandbox-2", sandboxes[1].SandboxID)
}

func TestClientKillSandbox(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodDelete, r.Method)
		assert.Equal(t, "/sandboxes/sandbox-123", r.URL.Path)

		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	client := NewClient(server.URL, "test-api-key")
	err := client.KillSandbox(context.Background(), "sandbox-123")

	require.NoError(t, err)
}

func TestClientKillSandboxNotFound(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_ = json.NewEncoder(w).Encode(APIError{Message: "sandbox not found"})
	}))
	defer server.Close()

	client := NewClient(server.URL, "test-api-key")
	err := client.KillSandbox(context.Background(), "not-found")

	require.Error(t, err)
	assert.True(t, IsNotFoundError(err))
}

func TestClientSetTimeout(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodPost, r.Method)
		assert.Equal(t, "/sandboxes/sandbox-123/timeout", r.URL.Path)

		var req SetTimeoutRequest
		err := json.NewDecoder(r.Body).Decode(&req)
		require.NoError(t, err)
		assert.Equal(t, 600, req.Timeout)

		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	client := NewClient(server.URL, "test-api-key")
	err := client.SetTimeout(context.Background(), "sandbox-123", 600)

	require.NoError(t, err)
}

func TestClientPauseSandbox(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodPost, r.Method)
		assert.Equal(t, "/sandboxes/sandbox-123/pause", r.URL.Path)

		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	client := NewClient(server.URL, "test-api-key")
	err := client.PauseSandbox(context.Background(), "sandbox-123")

	require.NoError(t, err)
}

func TestClientResumeSandbox(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodPost, r.Method)
		assert.Equal(t, "/sandboxes/sandbox-123/resume", r.URL.Path)

		// Verify request body contains timeout
		var req ResumeSandboxRequest
		_ = json.NewDecoder(r.Body).Decode(&req)
		assert.Equal(t, 300, req.Timeout)

		resp := Sandbox{
			SandboxID:  "sandbox-123",
			TemplateID: "base",
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	client := NewClient(server.URL, "test-api-key")
	sandbox, err := client.ResumeSandbox(context.Background(), "sandbox-123", 300)

	require.NoError(t, err)
	assert.Equal(t, "sandbox-123", sandbox.SandboxID)
}

func TestClientAPIError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(APIError{Message: "invalid request"})
	}))
	defer server.Close()

	client := NewClient(server.URL, "test-api-key")
	_, err := client.CreateSandbox(context.Background(), &CreateSandboxRequest{})

	require.Error(t, err)
	var apiErr *APIError
	require.ErrorAs(t, err, &apiErr)
	assert.Equal(t, http.StatusBadRequest, apiErr.Code)
	assert.Equal(t, "invalid request", apiErr.Message)
}

func TestClientAPIErrorGeneric(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte("internal server error"))
	}))
	defer server.Close()

	client := NewClient(server.URL, "test-api-key")
	_, err := client.CreateSandbox(context.Background(), &CreateSandboxRequest{})

	require.Error(t, err)
	var apiErr *APIError
	require.ErrorAs(t, err, &apiErr)
	assert.Equal(t, http.StatusInternalServerError, apiErr.Code)
	assert.Contains(t, apiErr.Message, "internal server error")
}

func TestAPIErrorString(t *testing.T) {
	err := &APIError{Code: 404, Message: "not found"}
	assert.Equal(t, "e2b api error 404: not found", err.Error())
}

func TestIsNotFoundError(t *testing.T) {
	assert.True(t, IsNotFoundError(&APIError{Code: 404}))
	assert.False(t, IsNotFoundError(&APIError{Code: 400}))
	assert.False(t, IsNotFoundError(assert.AnError))
}

func TestIsPausedError(t *testing.T) {
	assert.True(t, IsPausedError(&APIError{Code: 409}))
	assert.False(t, IsPausedError(&APIError{Code: 400}))
	assert.False(t, IsPausedError(assert.AnError))
}

func TestClientContextCancellation(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Simulate slow response
		time.Sleep(100 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	client := NewClient(server.URL, "test-api-key")
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	_, err := client.CreateSandbox(ctx, &CreateSandboxRequest{})
	require.Error(t, err)
}

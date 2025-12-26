package client

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/chunlea/marionette/pkg/store"
)

func TestHTTPClient_CreateSession(t *testing.T) {
	now := time.Now()
	expected := &store.Session{
		ID:        "sess_test123",
		Name:      strPtr("test-session"),
		Status:    "pending",
		Agent:     "claude",
		CreatedAt: now,
		UpdatedAt: now,
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		if r.URL.Path != "/api/v1/sessions" {
			t.Errorf("expected /api/v1/sessions, got %s", r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer test-api-key" {
			t.Errorf("expected Bearer test-api-key, got %s", r.Header.Get("Authorization"))
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(expected)
	}))
	defer server.Close()

	client := NewHTTPClient(server.URL, "test-api-key")
	session, err := client.CreateSession(context.Background(), CreateSessionOptions{
		Name:  "test-session",
		Agent: "claude",
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if session.ID != expected.ID {
		t.Errorf("expected ID %s, got %s", expected.ID, session.ID)
	}
	if *session.Name != *expected.Name {
		t.Errorf("expected Name %s, got %s", *expected.Name, *session.Name)
	}
}

func TestHTTPClient_GetSession(t *testing.T) {
	now := time.Now()
	expected := &store.Session{
		ID:        "sess_test123",
		Name:      strPtr("my-session"),
		Status:    "active",
		Agent:     "claude",
		CreatedAt: now,
		UpdatedAt: now,
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("expected GET, got %s", r.Method)
		}
		if r.URL.Path != "/api/v1/sessions/sess_test123" {
			t.Errorf("expected /api/v1/sessions/sess_test123, got %s", r.URL.Path)
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(expected)
	}))
	defer server.Close()

	client := NewHTTPClient(server.URL, "test-api-key")
	session, err := client.GetSession(context.Background(), "sess_test123")

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if session.ID != expected.ID {
		t.Errorf("expected ID %s, got %s", expected.ID, session.ID)
	}
}

func TestHTTPClient_ListSessions(t *testing.T) {
	now := time.Now()
	expected := &ListResult[Session]{
		Items: []*store.Session{
			{ID: "sess_1", Name: strPtr("session-1"), Status: "active", Agent: "claude", CreatedAt: now, UpdatedAt: now},
			{ID: "sess_2", Name: strPtr("session-2"), Status: "pending", Agent: "claude", CreatedAt: now, UpdatedAt: now},
		},
		NextCursor: "cursor123",
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("expected GET, got %s", r.Method)
		}
		if r.URL.Path != "/api/v1/sessions" {
			t.Errorf("expected /api/v1/sessions, got %s", r.URL.Path)
		}
		// Check query params
		if r.URL.Query().Get("limit") != "10" {
			t.Errorf("expected limit=10, got %s", r.URL.Query().Get("limit"))
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(expected)
	}))
	defer server.Close()

	client := NewHTTPClient(server.URL, "test-api-key")
	result, err := client.ListSessions(context.Background(), ListSessionsOptions{Limit: 10})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.Items) != 2 {
		t.Errorf("expected 2 sessions, got %d", len(result.Items))
	}
	if result.NextCursor != "cursor123" {
		t.Errorf("expected cursor123, got %s", result.NextCursor)
	}
}

func TestHTTPClient_SuspendSession(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		if r.URL.Path != "/api/v1/sessions/sess_test123/suspend" {
			t.Errorf("expected /api/v1/sessions/sess_test123/suspend, got %s", r.URL.Path)
		}

		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	client := NewHTTPClient(server.URL, "test-api-key")
	err := client.SuspendSession(context.Background(), "sess_test123")

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestHTTPClient_CreateTask(t *testing.T) {
	now := time.Now()
	expected := &store.Task{
		ID:        "task_test123",
		SessionID: "sess_xxx",
		Prompt:    "Build an API",
		Status:    "pending",
		CreatedAt: now,
		UpdatedAt: now,
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		if r.URL.Path != "/api/v1/tasks" {
			t.Errorf("expected /api/v1/tasks, got %s", r.URL.Path)
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(expected)
	}))
	defer server.Close()

	client := NewHTTPClient(server.URL, "test-api-key")
	task, err := client.CreateTask(context.Background(), CreateTaskOptions{
		SessionID: "sess_xxx",
		Prompt:    "Build an API",
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if task.ID != expected.ID {
		t.Errorf("expected ID %s, got %s", expected.ID, task.ID)
	}
}

func TestHTTPClient_GetTask(t *testing.T) {
	now := time.Now()
	expected := &store.Task{
		ID:        "task_test123",
		SessionID: "sess_xxx",
		Prompt:    "Build an API",
		Status:    "completed",
		CreatedAt: now,
		UpdatedAt: now,
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("expected GET, got %s", r.Method)
		}
		if r.URL.Path != "/api/v1/tasks/task_test123" {
			t.Errorf("expected /api/v1/tasks/task_test123, got %s", r.URL.Path)
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(expected)
	}))
	defer server.Close()

	client := NewHTTPClient(server.URL, "test-api-key")
	task, err := client.GetTask(context.Background(), "task_test123")

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if task.ID != expected.ID {
		t.Errorf("expected ID %s, got %s", expected.ID, task.ID)
	}
}

func TestHTTPClient_ErrorHandling(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		_ = json.NewEncoder(w).Encode(map[string]string{
			"code":    "not_found",
			"message": "Session not found",
		})
	}))
	defer server.Close()

	client := NewHTTPClient(server.URL, "test-api-key")
	_, err := client.GetSession(context.Background(), "sess_nonexistent")

	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !IsNotFound(err) {
		t.Errorf("expected IsNotFound to return true, got false: %v", err)
	}
}

func TestHTTPClient_GetRunner(t *testing.T) {
	now := time.Now()
	expected := &store.Runner{
		ID:        "run_test123",
		Name:      "test-runner",
		Hostname:  "localhost",
		Status:    "idle",
		CreatedAt: now,
		UpdatedAt: now,
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("expected GET, got %s", r.Method)
		}
		if r.URL.Path != "/api/v1/runners/run_test123" {
			t.Errorf("expected /api/v1/runners/run_test123, got %s", r.URL.Path)
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(expected)
	}))
	defer server.Close()

	client := NewHTTPClient(server.URL, "test-api-key")
	runner, err := client.GetRunner(context.Background(), "run_test123")

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if runner.ID != expected.ID {
		t.Errorf("expected ID %s, got %s", expected.ID, runner.ID)
	}
}

func TestHTTPClient_ApprovePermission(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		if r.URL.Path != "/api/v1/permissions/perm_test123/approve" {
			t.Errorf("expected /api/v1/permissions/perm_test123/approve, got %s", r.URL.Path)
		}

		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	client := NewHTTPClient(server.URL, "test-api-key")
	err := client.ApprovePermission(context.Background(), "perm_test123", "approved for testing")

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestHTTPClient_DenyPermission(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		if r.URL.Path != "/api/v1/permissions/perm_test123/deny" {
			t.Errorf("expected /api/v1/permissions/perm_test123/deny, got %s", r.URL.Path)
		}

		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	client := NewHTTPClient(server.URL, "test-api-key")
	err := client.DenyPermission(context.Background(), "perm_test123", "not authorized")

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

// Helper function
func strPtr(s string) *string {
	return &s
}

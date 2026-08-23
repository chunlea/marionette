package client

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestHTTPClient_CreateSession(t *testing.T) {
	now := time.Now()
	expected := &Session{
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
	expected := &Session{
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
		Items: []*Session{
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
	expected := &Task{
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
	expected := &Task{
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
	expected := &Runner{
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

func TestHTTPClient_ListTasks(t *testing.T) {
	now := time.Now()
	expected := &ListResult[Task]{
		Items: []*Task{
			{ID: "task_1", SessionID: "sess_xxx", Status: "running", Prompt: "Task 1", CreatedAt: now, UpdatedAt: now},
			{ID: "task_2", SessionID: "sess_xxx", Status: "pending", Prompt: "Task 2", CreatedAt: now, UpdatedAt: now},
		},
		NextCursor: "cursor456",
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("expected GET, got %s", r.Method)
		}
		if r.URL.Path != "/api/v1/tasks" {
			t.Errorf("expected /api/v1/tasks, got %s", r.URL.Path)
		}
		if r.URL.Query().Get("session_id") != "sess_xxx" {
			t.Errorf("expected session_id=sess_xxx, got %s", r.URL.Query().Get("session_id"))
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(expected)
	}))
	defer server.Close()

	client := NewHTTPClient(server.URL, "test-api-key")
	result, err := client.ListTasks(context.Background(), ListTasksOptions{
		SessionID: "sess_xxx",
		Limit:     50,
		Status:    []string{"running", "pending"},
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.Items) != 2 {
		t.Errorf("expected 2 tasks, got %d", len(result.Items))
	}
}

func TestHTTPClient_CancelTask(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		if r.URL.Path != "/api/v1/tasks/task_test123/cancel" {
			t.Errorf("expected /api/v1/tasks/task_test123/cancel, got %s", r.URL.Path)
		}

		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	client := NewHTTPClient(server.URL, "test-api-key")
	err := client.CancelTask(context.Background(), "task_test123")

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestHTTPClient_GetTaskLogs(t *testing.T) {
	expected := struct {
		Items      []*Log `json:"items"`
		TotalCount int64  `json:"total_count"`
	}{
		Items: []*Log{
			{ID: "log_1", Content: "Hello", Stream: "stdout", Sequence: 1},
			{ID: "log_2", Content: "World", Stream: "stdout", Sequence: 2},
		},
		TotalCount: 2,
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("expected GET, got %s", r.Method)
		}
		if r.URL.Path != "/api/v1/tasks/task_test123/logs" {
			t.Errorf("expected /api/v1/tasks/task_test123/logs, got %s", r.URL.Path)
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(expected)
	}))
	defer server.Close()

	client := NewHTTPClient(server.URL, "test-api-key")
	iter, err := client.GetTaskLogs(context.Background(), "task_test123", GetLogsOptions{
		Tail:          100,
		SinceSequence: 0,
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer func() { _ = iter.Close() }()

	// Read first log
	log1, err := iter.Next()
	if err != nil {
		t.Fatalf("unexpected error reading log: %v", err)
	}
	if log1.Content != "Hello" {
		t.Errorf("expected Hello, got %s", log1.Content)
	}

	// Read second log
	log2, err := iter.Next()
	if err != nil {
		t.Fatalf("unexpected error reading log: %v", err)
	}
	if log2.Content != "World" {
		t.Errorf("expected World, got %s", log2.Content)
	}

	// Read past end
	_, err = iter.Next()
	if err == nil {
		t.Error("expected EOF error")
	}
}

func TestHTTPClient_ListRunners(t *testing.T) {
	now := time.Now()
	expected := &ListResult[Runner]{
		Items: []*Runner{
			{ID: "run_1", Name: "runner-1", Hostname: "host1", Status: "idle", CreatedAt: now, UpdatedAt: now},
			{ID: "run_2", Name: "runner-2", Hostname: "host2", Status: "busy", CreatedAt: now, UpdatedAt: now},
		},
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("expected GET, got %s", r.Method)
		}
		if r.URL.Path != "/api/v1/runners" {
			t.Errorf("expected /api/v1/runners, got %s", r.URL.Path)
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(expected)
	}))
	defer server.Close()

	client := NewHTTPClient(server.URL, "test-api-key")
	result, err := client.ListRunners(context.Background(), ListRunnersOptions{
		Limit:    50,
		Status:   []string{"idle", "busy"},
		PoolName: "default",
		Labels:   map[string]string{"env": "prod"},
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.Items) != 2 {
		t.Errorf("expected 2 runners, got %d", len(result.Items))
	}
}

func TestHTTPClient_GetPermission(t *testing.T) {
	now := time.Now()
	expected := &PermissionRequest{
		ID:        "perm_test123",
		SessionID: "sess_xxx",
		TaskID:    "task_xxx",
		RunID:     "trun_xxx",
		Tool:      "bash",
		Action:    "rm -rf /tmp/test",
		Status:    "pending",
		RiskLevel: "high",
		CreatedAt: now,
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("expected GET, got %s", r.Method)
		}
		if r.URL.Path != "/api/v1/permissions/perm_test123" {
			t.Errorf("expected /api/v1/permissions/perm_test123, got %s", r.URL.Path)
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(expected)
	}))
	defer server.Close()

	client := NewHTTPClient(server.URL, "test-api-key")
	perm, err := client.GetPermission(context.Background(), "perm_test123")

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if perm.ID != expected.ID {
		t.Errorf("expected ID %s, got %s", expected.ID, perm.ID)
	}
}

func TestHTTPClient_ListPermissions(t *testing.T) {
	now := time.Now()
	expected := &ListResult[PermissionRequest]{
		Items: []*PermissionRequest{
			{ID: "perm_1", SessionID: "sess_xxx", TaskID: "task_xxx", RunID: "trun_xxx", Tool: "bash", Action: "cmd1", Status: "pending", RiskLevel: "high", CreatedAt: now},
		},
		NextCursor: "cursor789",
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("expected GET, got %s", r.Method)
		}
		if r.URL.Path != "/api/v1/permissions" {
			t.Errorf("expected /api/v1/permissions, got %s", r.URL.Path)
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(expected)
	}))
	defer server.Close()

	client := NewHTTPClient(server.URL, "test-api-key")
	result, err := client.ListPermissions(context.Background(), ListPermissionsOptions{
		Limit:     50,
		SessionID: "sess_xxx",
		TaskID:    "task_xxx",
		Status:    []string{"pending"},
		RiskLevel: []string{"high", "critical"},
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.Items) != 1 {
		t.Errorf("expected 1 permission, got %d", len(result.Items))
	}
}

func TestHTTPClient_ResumeSession(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		if r.URL.Path != "/api/v1/sessions/sess_test123/resume" {
			t.Errorf("expected /api/v1/sessions/sess_test123/resume, got %s", r.URL.Path)
		}

		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	client := NewHTTPClient(server.URL, "test-api-key")
	err := client.ResumeSession(context.Background(), "sess_test123")

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestHTTPClient_TerminateSession(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodDelete {
			t.Errorf("expected DELETE, got %s", r.Method)
		}
		if r.URL.Path != "/api/v1/sessions/sess_test123" {
			t.Errorf("expected /api/v1/sessions/sess_test123, got %s", r.URL.Path)
		}

		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	client := NewHTTPClient(server.URL, "test-api-key")
	err := client.TerminateSession(context.Background(), "sess_test123")

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestHTTPClient_CreateSessionWithAllOptions(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)

		// Verify all options are passed
		if body["agent"] != "claude" {
			t.Errorf("expected agent=claude, got %v", body["agent"])
		}
		if body["name"] != "test-session" {
			t.Errorf("expected name=test-session, got %v", body["name"])
		}
		if body["agent_config_id"] != "acfg_xxx" {
			t.Errorf("expected agent_config_id=acfg_xxx, got %v", body["agent_config_id"])
		}
		if body["api_key"] != "sk-xxx" {
			t.Errorf("expected api_key=sk-xxx, got %v", body["api_key"])
		}
		if body["lifecycle_mode"] != "always_on" {
			t.Errorf("expected lifecycle_mode=always_on, got %v", body["lifecycle_mode"])
		}
		if body["idle_timeout_seconds"] != float64(3600) {
			t.Errorf("expected idle_timeout_seconds=3600, got %v", body["idle_timeout_seconds"])
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(&Session{ID: "sess_new"})
	}))
	defer server.Close()

	client := NewHTTPClient(server.URL, "test-api-key")
	_, err := client.CreateSession(context.Background(), CreateSessionOptions{
		Name:               "test-session",
		Agent:              "claude",
		AgentConfigID:      "acfg_xxx",
		APIKey:             "sk-xxx",
		LifecycleMode:      "always_on",
		IdleTimeoutSeconds: 3600,
		Labels:             map[string]string{"env": "prod"},
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestHTTPClient_CreateTaskWithAllOptions(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)

		if body["continue_from"] != "task_prev" {
			t.Errorf("expected continue_from=task_prev, got %v", body["continue_from"])
		}
		if body["timeout_seconds"] != float64(7200) {
			t.Errorf("expected timeout_seconds=7200, got %v", body["timeout_seconds"])
		}
		if body["max_retries"] != float64(3) {
			t.Errorf("expected max_retries=3, got %v", body["max_retries"])
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(&Task{ID: "task_new"})
	}))
	defer server.Close()

	client := NewHTTPClient(server.URL, "test-api-key")
	_, err := client.CreateTask(context.Background(), CreateTaskOptions{
		SessionID:      "sess_xxx",
		Prompt:         "Build an API",
		ContinueFrom:   "task_prev",
		TimeoutSeconds: 7200,
		MaxRetries:     3,
		Labels:         map[string]string{"priority": "high"},
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestHTTPClient_ListSessionsWithAllFilters(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		if q.Get("cursor") != "cursor123" {
			t.Errorf("expected cursor=cursor123, got %s", q.Get("cursor"))
		}
		if q.Get("agent") != "claude" {
			t.Errorf("expected agent=claude, got %s", q.Get("agent"))
		}
		if q.Get("labels[env]") != "prod" {
			t.Errorf("expected labels[env]=prod, got %s", q.Get("labels[env]"))
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(&ListResult[Session]{Items: []*Session{}})
	}))
	defer server.Close()

	client := NewHTTPClient(server.URL, "test-api-key")
	_, err := client.ListSessions(context.Background(), ListSessionsOptions{
		Limit:  50,
		Cursor: "cursor123",
		Status: []string{"active", "suspended"},
		Agent:  "claude",
		Labels: map[string]string{"env": "prod"},
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestHTTPClient_ErrorParsingInvalidJSON(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte("not valid json"))
	}))
	defer server.Close()

	client := NewHTTPClient(server.URL, "test-api-key")
	_, err := client.GetSession(context.Background(), "sess_xxx")

	if err == nil {
		t.Fatal("expected error, got nil")
	}
	apiErr, ok := err.(*APIError)
	if !ok {
		t.Fatalf("expected APIError, got %T", err)
	}
	if apiErr.StatusCode != http.StatusInternalServerError {
		t.Errorf("expected status 500, got %d", apiErr.StatusCode)
	}
}

func TestHTTPClient_ApprovePermissionWithoutReason(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Body should be nil when no reason provided
		body := make([]byte, 100)
		n, _ := r.Body.Read(body)
		if n > 0 {
			t.Errorf("expected empty body, got %s", string(body[:n]))
		}

		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	client := NewHTTPClient(server.URL, "test-api-key")
	err := client.ApprovePermission(context.Background(), "perm_test123", "")

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestHTTPClient_WithOptions(t *testing.T) {
	customClient := &http.Client{Timeout: 60 * time.Second}

	client := NewHTTPClient("http://localhost:8080", "test-key",
		WithHTTPClient(customClient),
	)

	if client.httpClient != customClient {
		t.Error("expected custom http client to be set")
	}

	// Test WithTimeout
	client2 := NewHTTPClient("http://localhost:8080", "test-key",
		WithTimeout(120*time.Second),
	)

	if client2.httpClient.Timeout != 120*time.Second {
		t.Errorf("expected 120s timeout, got %v", client2.httpClient.Timeout)
	}
}

func TestHTTPClient_TrailingSlashInBaseURL(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/sessions/sess_test" {
			t.Errorf("expected /api/v1/sessions/sess_test, got %s", r.URL.Path)
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(&Session{ID: "sess_test"})
	}))
	defer server.Close()

	// URL with trailing slash should work
	client := NewHTTPClient(server.URL+"/", "test-api-key")
	_, err := client.GetSession(context.Background(), "sess_test")

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

// Helper function
func strPtr(s string) *string {
	return &s
}

// A session long enough to have been archived does not fit in one page, and
// stopping at the first one used to look exactly like reaching the end.
func TestHTTPClient_LogIteratorFollowsPagination(t *testing.T) {
	pages := []struct {
		items      []*Log
		hasMore    bool
		nextCursor string
	}{
		{items: []*Log{{ID: "log_1", Content: "one"}}, hasMore: true, nextCursor: "archive:1"},
		{items: []*Log{{ID: "log_2", Content: "two"}}, hasMore: true, nextCursor: "hot-cursor"},
		{items: []*Log{{ID: "log_3", Content: "three"}}},
	}

	var cursors []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cursor := r.URL.Query().Get("cursor")
		cursors = append(cursors, cursor)

		// The cursor changes shape mid-stream - an archive offset, then a
		// database cursor - which is exactly what the iterator has to carry
		// without interpreting.
		var page = pages[0]
		switch cursor {
		case "archive:1":
			page = pages[1]
		case "hot-cursor":
			page = pages[2]
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(struct {
			Items      []*Log `json:"items"`
			TotalCount int64  `json:"total_count"`
			HasMore    bool   `json:"has_more"`
			NextCursor string `json:"next_cursor,omitempty"`
		}{Items: page.items, TotalCount: 3, HasMore: page.hasMore, NextCursor: page.nextCursor})
	}))
	defer server.Close()

	c := NewHTTPClient(server.URL, "test-api-key")
	iter, err := c.GetSessionLogs(context.Background(), "sess_1", GetLogsOptions{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer func() { _ = iter.Close() }()

	var got []string
	for {
		log, err := iter.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("unexpected error reading log: %v", err)
		}
		got = append(got, log.Content)
	}

	if len(got) != 3 || got[0] != "one" || got[2] != "three" {
		t.Fatalf("expected all three pages, got %v", got)
	}
	if len(cursors) != 3 {
		t.Fatalf("expected three requests, got %v", cursors)
	}
}

// --tail N means N lines, not N lines per page.
func TestHTTPClient_LogIteratorTailStopsAtTheLimit(t *testing.T) {
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		if got := r.URL.Query().Get("limit"); got != "2" {
			t.Errorf("expected limit=2, got %q", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(struct {
			Items      []*Log `json:"items"`
			HasMore    bool   `json:"has_more"`
			NextCursor string `json:"next_cursor,omitempty"`
		}{
			Items:      []*Log{{ID: "log_1", Content: "one"}, {ID: "log_2", Content: "two"}},
			HasMore:    true,
			NextCursor: "more",
		})
	}))
	defer server.Close()

	c := NewHTTPClient(server.URL, "test-api-key")
	iter, err := c.GetTaskLogs(context.Background(), "task_1", GetLogsOptions{Tail: 2})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer func() { _ = iter.Close() }()

	count := 0
	for {
		if _, err := iter.Next(); err == io.EOF {
			break
		} else if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		count++
	}

	if count != 2 {
		t.Fatalf("expected 2 lines, got %d", count)
	}
	if requests != 1 {
		t.Fatalf("expected one request, got %d", requests)
	}
}

func TestHTTPClient_GetSessionLogsPassesArchivedFilter(t *testing.T) {
	var gotPath, gotArchived string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotArchived = r.URL.Query().Get("archived")
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(struct {
			Items []*Log `json:"items"`
		}{})
	}))
	defer server.Close()

	c := NewHTTPClient(server.URL, "test-api-key")
	iter, err := c.GetSessionLogs(context.Background(), "sess_1", GetLogsOptions{Archived: "true"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	_ = iter.Close()

	if gotPath != "/api/v1/sessions/sess_1/logs" {
		t.Fatalf("unexpected path %q", gotPath)
	}
	if gotArchived != "true" {
		t.Fatalf("expected archived=true, got %q", gotArchived)
	}
}

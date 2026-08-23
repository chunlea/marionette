package client

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestHTTPAdminClient_CreateAPIKey(t *testing.T) {
	now := time.Now()
	expected := &APIKeyWithSecret{
		APIKey: APIKey{
			ID:        "key_test123",
			Name:      "test-key",
			KeyPrefix: "mk_testxxxx",
			Scopes:    []string{"tasks:*"},
			CreatedAt: now,
		},
		RawToken: "mk_full_secret_key_here",
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Check Basic Auth
		username, password, ok := r.BasicAuth()
		if !ok {
			t.Error("expected Basic Auth")
		}
		if username != "admin" || password != "secret" {
			t.Errorf("expected admin:secret, got %s:%s", username, password)
		}

		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		if r.URL.Path != "/admin/api/v1/keys" {
			t.Errorf("expected /admin/api/v1/keys, got %s", r.URL.Path)
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(expected)
	}))
	defer server.Close()

	client := NewHTTPAdminClient(server.URL, "admin", "secret")
	result, err := client.CreateAPIKey(context.Background(), CreateAPIKeyOptions{
		Name:   "test-key",
		Scopes: []string{"tasks:*"},
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.ID != expected.ID {
		t.Errorf("expected ID %s, got %s", expected.ID, result.ID)
	}
	if result.RawToken != expected.RawToken {
		t.Errorf("expected RawToken %s, got %s", expected.RawToken, result.RawToken)
	}
}

func TestHTTPAdminClient_ListAPIKeys(t *testing.T) {
	now := time.Now()
	expected := &ListResult[APIKey]{
		Items: []*APIKey{
			{ID: "key_1", Name: "key-1", KeyPrefix: "mk_xxx1", CreatedAt: now},
			{ID: "key_2", Name: "key-2", KeyPrefix: "mk_xxx2", CreatedAt: now},
		},
		NextCursor: "cursor123",
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("expected GET, got %s", r.Method)
		}
		if r.URL.Path != "/admin/api/v1/keys" {
			t.Errorf("expected /admin/api/v1/keys, got %s", r.URL.Path)
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(expected)
	}))
	defer server.Close()

	client := NewHTTPAdminClient(server.URL, "admin", "secret")
	result, err := client.ListAPIKeys(context.Background(), ListAPIKeysOptions{Limit: 50})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.Items) != 2 {
		t.Errorf("expected 2 keys, got %d", len(result.Items))
	}
}

func TestHTTPAdminClient_RevokeAPIKey(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodDelete {
			t.Errorf("expected DELETE, got %s", r.Method)
		}
		if r.URL.Path != "/admin/api/v1/keys/key_test123" {
			t.Errorf("expected /admin/api/v1/keys/key_test123, got %s", r.URL.Path)
		}

		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	client := NewHTTPAdminClient(server.URL, "admin", "secret")
	err := client.RevokeAPIKey(context.Background(), "key_test123", "no longer needed")

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestHTTPAdminClient_CreateAgentConfig(t *testing.T) {
	now := time.Now()
	expected := &AgentConfig{
		ID:        "acfg_test123",
		Name:      "claude-prod",
		Agent:     "claude",
		IsDefault: true,
		CreatedAt: now,
		UpdatedAt: now,
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		if r.URL.Path != "/admin/api/v1/agent-configs" {
			t.Errorf("expected /admin/api/v1/agent-configs, got %s", r.URL.Path)
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(expected)
	}))
	defer server.Close()

	client := NewHTTPAdminClient(server.URL, "admin", "secret")
	result, err := client.CreateAgentConfig(context.Background(), CreateAgentConfigOptions{
		Name:      "claude-prod",
		Agent:     "claude",
		APIKey:    "sk-xxx",
		IsDefault: true,
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.ID != expected.ID {
		t.Errorf("expected ID %s, got %s", expected.ID, result.ID)
	}
}

func TestHTTPAdminClient_ListAgentConfigs(t *testing.T) {
	now := time.Now()
	expected := &ListResult[AgentConfig]{
		Items: []*AgentConfig{
			{ID: "acfg_1", Name: "claude-dev", Agent: "claude", CreatedAt: now, UpdatedAt: now},
			{ID: "acfg_2", Name: "claude-prod", Agent: "claude", CreatedAt: now, UpdatedAt: now},
		},
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("expected GET, got %s", r.Method)
		}
		if r.URL.Path != "/admin/api/v1/agent-configs" {
			t.Errorf("expected /admin/api/v1/agent-configs, got %s", r.URL.Path)
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(expected)
	}))
	defer server.Close()

	client := NewHTTPAdminClient(server.URL, "admin", "secret")
	result, err := client.ListAgentConfigs(context.Background(), ListAgentConfigsOptions{})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.Items) != 2 {
		t.Errorf("expected 2 configs, got %d", len(result.Items))
	}
}

func TestHTTPAdminClient_DeleteAgentConfig(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodDelete {
			t.Errorf("expected DELETE, got %s", r.Method)
		}
		if r.URL.Path != "/admin/api/v1/agent-configs/acfg_test123" {
			t.Errorf("expected /admin/api/v1/agent-configs/acfg_test123, got %s", r.URL.Path)
		}

		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	client := NewHTTPAdminClient(server.URL, "admin", "secret")
	err := client.DeleteAgentConfig(context.Background(), "acfg_test123")

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestHTTPAdminClient_SpawnRunner(t *testing.T) {
	now := time.Now()
	expected := &AdminRunner{
		ID:        "run_test123",
		Name:      "spawned-runner",
		Hostname:  "localhost",
		Status:    "offline",
		CreatedAt: now,
		UpdatedAt: now,
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		if r.URL.Path != "/admin/api/v1/runners/spawn" {
			t.Errorf("expected /admin/api/v1/runners/spawn, got %s", r.URL.Path)
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(expected)
	}))
	defer server.Close()

	client := NewHTTPAdminClient(server.URL, "admin", "secret")
	runner, err := client.SpawnRunner(context.Background(), SpawnRunnerOptions{
		Name: "spawned-runner",
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if runner.ID != expected.ID {
		t.Errorf("expected ID %s, got %s", expected.ID, runner.ID)
	}
}

func TestHTTPAdminClient_DestroyRunner(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodDelete {
			t.Errorf("expected DELETE, got %s", r.Method)
		}
		if r.URL.Path != "/admin/api/v1/runners/run_test123" {
			t.Errorf("expected /admin/api/v1/runners/run_test123, got %s", r.URL.Path)
		}

		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	client := NewHTTPAdminClient(server.URL, "admin", "secret")
	err := client.DestroyRunner(context.Background(), "run_test123")

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestHTTPAdminClient_ErrorHandling(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		_ = json.NewEncoder(w).Encode(map[string]string{
			"code":    "not_found",
			"message": "API key not found",
		})
	}))
	defer server.Close()

	client := NewHTTPAdminClient(server.URL, "admin", "secret")
	_, err := client.GetAPIKey(context.Background(), "key_nonexistent")

	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !IsNotFound(err) {
		t.Errorf("expected IsNotFound to return true, got false: %v", err)
	}
}

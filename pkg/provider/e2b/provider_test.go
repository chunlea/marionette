package e2b

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/chunlea/marionette/pkg/provider"
	"github.com/chunlea/marionette/pkg/store"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newTestProvider(t *testing.T, serverURL string) *Provider {
	t.Helper()

	cfg := &store.ProviderConfig{
		Name:     "test-e2b",
		Provider: "e2b",
		Config: json.RawMessage(`{
			"api_url": "` + serverURL + `",
			"api_key": "test-key",
			"template": "base"
		}`),
		SuspendConfig: json.RawMessage(`{}`),
	}

	p, err := New(cfg)
	require.NoError(t, err)
	return p
}

func TestNew(t *testing.T) {
	// Set up environment for test
	t.Setenv("MARIONETTE_E2B_API_KEY", "env-key")

	tests := []struct {
		name    string
		config  json.RawMessage
		wantErr bool
	}{
		{
			name: "valid config with API key",
			config: json.RawMessage(`{
				"api_key": "test-key"
			}`),
			wantErr: false,
		},
		{
			name:    "API key from environment",
			config:  json.RawMessage(`{}`),
			wantErr: false,
		},
		{
			name:    "invalid json",
			config:  json.RawMessage(`{invalid`),
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &store.ProviderConfig{
				Name:          "test",
				Provider:      "e2b",
				Config:        tt.config,
				SuspendConfig: json.RawMessage(`{}`),
			}
			_, err := New(cfg)
			if tt.wantErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestNewMissingAPIKey(t *testing.T) {
	// Ensure no env var is set (t.Setenv with empty value ensures it's unset)
	t.Setenv("MARIONETTE_E2B_API_KEY", "")

	cfg := &store.ProviderConfig{
		Name:          "test",
		Provider:      "e2b",
		Config:        json.RawMessage(`{}`),
		SuspendConfig: json.RawMessage(`{}`),
	}
	_, err := New(cfg)
	require.Error(t, err)

	var cfgErr *provider.ErrInvalidConfig
	require.ErrorAs(t, err, &cfgErr)
	assert.Equal(t, "api_key", cfgErr.Field)
}

func TestProviderName(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	defer server.Close()

	p := newTestProvider(t, server.URL)
	assert.Equal(t, "test-e2b", p.Name())
}

func TestProviderType(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	defer server.Close()

	p := newTestProvider(t, server.URL)
	assert.Equal(t, provider.ProviderTypeManaged, p.Type())
}

func TestProviderCapabilities(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	defer server.Close()

	p := newTestProvider(t, server.URL)
	caps := p.Capabilities()

	assert.True(t, caps.Pause)
	assert.False(t, caps.Snapshot)
	assert.Equal(t, provider.SuspendStrategyPause, caps.Suspend.Default)
	assert.Contains(t, caps.Suspend.Strategies, provider.SuspendStrategyPause)
	assert.Contains(t, caps.Suspend.Strategies, provider.SuspendStrategyTerminate)
}

func TestProviderSpawn(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost && r.URL.Path == "/sandboxes" {
			var req CreateSandboxRequest
			_ = json.NewDecoder(r.Body).Decode(&req)

			assert.Equal(t, "base", req.TemplateID)
			assert.Equal(t, "run_123", req.Metadata["marionette.dev/runner-id"])
			assert.Equal(t, "http://server:9090", req.EnvVars["MARIONETTE_SERVER"])
			assert.Equal(t, "token-123", req.EnvVars["MARIONETTE_RUNNER_TOKEN"])

			resp := CreateSandboxResponse{
				SandboxID:  "sandbox-abc",
				TemplateID: "base",
				ClientID:   "client-xyz",
			}
			_ = json.NewEncoder(w).Encode(resp)
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	p := newTestProvider(t, server.URL)
	instance, err := p.Spawn(context.Background(), provider.SpawnOptions{
		RunnerID:    "run_123",
		Name:        "test-runner",
		ServerURL:   "http://server:9090",
		RunnerToken: "token-123",
		SandboxMode: "runner-is-sandbox",
		Labels: map[string]string{
			"team": "backend",
		},
		Environment: map[string]string{
			"CUSTOM_VAR": "value",
		},
	})

	require.NoError(t, err)
	assert.Equal(t, "run_123", instance.ID)
	assert.Equal(t, "sandbox-abc", instance.ProviderID)
	assert.Equal(t, "test-runner", instance.Name)
	assert.Equal(t, provider.InstanceStatusRunning, instance.Status)
	assert.Equal(t, "sandbox-abc", instance.Metadata["sandbox_id"])
}

func TestProviderSpawnError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(APIError{Message: "failed to create sandbox"})
	}))
	defer server.Close()

	p := newTestProvider(t, server.URL)
	_, err := p.Spawn(context.Background(), provider.SpawnOptions{
		RunnerID: "run_123",
	})

	require.Error(t, err)
	var spawnErr *provider.ErrSpawnFailed
	require.ErrorAs(t, err, &spawnErr)
}

func TestProviderDestroy(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet && r.URL.Path == "/sandboxes" {
			resp := []Sandbox{
				{
					SandboxID: "sandbox-abc",
					Metadata: map[string]string{
						"marionette.dev/runner-id": "run_123",
					},
				},
			}
			_ = json.NewEncoder(w).Encode(resp)
			return
		}
		if r.Method == http.MethodDelete && r.URL.Path == "/sandboxes/sandbox-abc" {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	p := newTestProvider(t, server.URL)
	err := p.Destroy(context.Background(), "run_123", provider.DestroyOptions{})

	require.NoError(t, err)
}

func TestProviderDestroyNotFound(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet && r.URL.Path == "/sandboxes" {
			_ = json.NewEncoder(w).Encode([]Sandbox{})
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	p := newTestProvider(t, server.URL)
	// Should not error when sandbox not found
	err := p.Destroy(context.Background(), "run_123", provider.DestroyOptions{})

	require.NoError(t, err)
}

func TestProviderStatus(t *testing.T) {
	startTime := time.Now()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet && r.URL.Path == "/sandboxes" {
			resp := []Sandbox{
				{
					SandboxID: "sandbox-abc",
					Metadata: map[string]string{
						"marionette.dev/runner-id": "run_123",
					},
				},
			}
			_ = json.NewEncoder(w).Encode(resp)
			return
		}
		if r.Method == http.MethodGet && r.URL.Path == "/sandboxes/sandbox-abc" {
			resp := Sandbox{
				SandboxID: "sandbox-abc",
				StartedAt: startTime,
			}
			_ = json.NewEncoder(w).Encode(resp)
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	p := newTestProvider(t, server.URL)
	status, err := p.Status(context.Background(), "run_123")

	require.NoError(t, err)
	assert.Equal(t, provider.InstanceStatusRunning, status.Status)
}

func TestProviderStatusNotFound(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet && r.URL.Path == "/sandboxes" {
			_ = json.NewEncoder(w).Encode([]Sandbox{})
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	p := newTestProvider(t, server.URL)
	_, err := p.Status(context.Background(), "run_123")

	require.Error(t, err)
	var notFoundErr *provider.ErrRunnerNotFound
	require.ErrorAs(t, err, &notFoundErr)
}

func TestProviderStatusPaused(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet && r.URL.Path == "/sandboxes" {
			resp := []Sandbox{
				{
					SandboxID: "sandbox-abc",
					Metadata: map[string]string{
						"marionette.dev/runner-id": "run_123",
					},
				},
			}
			_ = json.NewEncoder(w).Encode(resp)
			return
		}
		if r.Method == http.MethodGet && r.URL.Path == "/sandboxes/sandbox-abc" {
			w.WriteHeader(http.StatusConflict)
			_ = json.NewEncoder(w).Encode(APIError{Message: "sandbox is paused"})
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	p := newTestProvider(t, server.URL)
	status, err := p.Status(context.Background(), "run_123")

	require.NoError(t, err)
	assert.Equal(t, provider.InstanceStatusPaused, status.Status)
}

func TestProviderList(t *testing.T) {
	startTime := time.Now()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := []Sandbox{
			{
				SandboxID:  "sandbox-1",
				TemplateID: "base",
				StartedAt:  startTime,
				Metadata: map[string]string{
					"marionette.dev/runner-id": "run_1",
					"marionette.dev/team":      "backend",
				},
			},
			{
				SandboxID:  "sandbox-2",
				TemplateID: "custom",
				StartedAt:  startTime,
				Metadata: map[string]string{
					"marionette.dev/runner-id": "run_2",
				},
			},
			{
				// Non-marionette sandbox (no runner-id)
				SandboxID:  "sandbox-3",
				TemplateID: "other",
			},
		}
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	p := newTestProvider(t, server.URL)
	instances, err := p.List(context.Background())

	require.NoError(t, err)
	assert.Len(t, instances, 2) // Only marionette sandboxes
	assert.Equal(t, "run_1", instances[0].ID)
	assert.Equal(t, "sandbox-1", instances[0].ProviderID)
	assert.Equal(t, "backend", instances[0].Labels["team"])
	assert.Equal(t, "run_2", instances[1].ID)
}

func TestProviderPause(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet && r.URL.Path == "/sandboxes" {
			resp := []Sandbox{
				{
					SandboxID: "sandbox-abc",
					Metadata: map[string]string{
						"marionette.dev/runner-id": "run_123",
					},
				},
			}
			_ = json.NewEncoder(w).Encode(resp)
			return
		}
		if r.Method == http.MethodPost && r.URL.Path == "/sandboxes/sandbox-abc/pause" {
			w.WriteHeader(http.StatusOK)
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	p := newTestProvider(t, server.URL)
	err := p.Pause(context.Background(), "run_123")

	require.NoError(t, err)
}

func TestProviderUnpause(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet && r.URL.Path == "/sandboxes" {
			resp := []Sandbox{
				{
					SandboxID: "sandbox-abc",
					Metadata: map[string]string{
						"marionette.dev/runner-id": "run_123",
					},
				},
			}
			_ = json.NewEncoder(w).Encode(resp)
			return
		}
		if r.Method == http.MethodPost && r.URL.Path == "/sandboxes/sandbox-abc/resume" {
			resp := Sandbox{SandboxID: "sandbox-abc"}
			_ = json.NewEncoder(w).Encode(resp)
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	p := newTestProvider(t, server.URL)
	err := p.Unpause(context.Background(), "run_123")

	require.NoError(t, err)
}

func TestProviderFactory(t *testing.T) {
	t.Setenv("MARIONETTE_E2B_API_KEY", "test-key")

	factory := NewProviderFactory()
	cfg := &store.ProviderConfig{
		Name:          "test",
		Provider:      "e2b",
		Config:        json.RawMessage(`{}`),
		SuspendConfig: json.RawMessage(`{}`),
	}

	p, err := factory(cfg)
	require.NoError(t, err)
	assert.Equal(t, "test", p.Name())
}

func TestProviderDestroyError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet && r.URL.Path == "/sandboxes" {
			resp := []Sandbox{
				{
					SandboxID: "sandbox-abc",
					Metadata: map[string]string{
						"marionette.dev/runner-id": "run_123",
					},
				},
			}
			_ = json.NewEncoder(w).Encode(resp)
			return
		}
		if r.Method == http.MethodDelete && r.URL.Path == "/sandboxes/sandbox-abc" {
			w.WriteHeader(http.StatusInternalServerError)
			_ = json.NewEncoder(w).Encode(APIError{Message: "internal error"})
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	p := newTestProvider(t, server.URL)
	err := p.Destroy(context.Background(), "run_123", provider.DestroyOptions{})

	require.Error(t, err)
	var destroyErr *provider.ErrDestroyFailed
	require.ErrorAs(t, err, &destroyErr)
}

func TestProviderDestroyListError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(APIError{Message: "internal error"})
	}))
	defer server.Close()

	p := newTestProvider(t, server.URL)
	err := p.Destroy(context.Background(), "run_123", provider.DestroyOptions{})

	require.Error(t, err)
	var destroyErr *provider.ErrDestroyFailed
	require.ErrorAs(t, err, &destroyErr)
}

func TestProviderStatusGetSandboxError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet && r.URL.Path == "/sandboxes" {
			resp := []Sandbox{
				{
					SandboxID: "sandbox-abc",
					Metadata: map[string]string{
						"marionette.dev/runner-id": "run_123",
					},
				},
			}
			_ = json.NewEncoder(w).Encode(resp)
			return
		}
		if r.Method == http.MethodGet && r.URL.Path == "/sandboxes/sandbox-abc" {
			w.WriteHeader(http.StatusInternalServerError)
			_ = json.NewEncoder(w).Encode(APIError{Message: "internal error"})
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	p := newTestProvider(t, server.URL)
	_, err := p.Status(context.Background(), "run_123")

	require.Error(t, err)
}

func TestProviderStatusGetSandboxNotFound(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet && r.URL.Path == "/sandboxes" {
			resp := []Sandbox{
				{
					SandboxID: "sandbox-abc",
					Metadata: map[string]string{
						"marionette.dev/runner-id": "run_123",
					},
				},
			}
			_ = json.NewEncoder(w).Encode(resp)
			return
		}
		if r.Method == http.MethodGet && r.URL.Path == "/sandboxes/sandbox-abc" {
			w.WriteHeader(http.StatusNotFound)
			_ = json.NewEncoder(w).Encode(APIError{Message: "not found"})
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	p := newTestProvider(t, server.URL)
	_, err := p.Status(context.Background(), "run_123")

	require.Error(t, err)
	var notFoundErr *provider.ErrRunnerNotFound
	require.ErrorAs(t, err, &notFoundErr)
}

func TestProviderStatusStopped(t *testing.T) {
	endTime := time.Now()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet && r.URL.Path == "/sandboxes" {
			resp := []Sandbox{
				{
					SandboxID: "sandbox-abc",
					Metadata: map[string]string{
						"marionette.dev/runner-id": "run_123",
					},
				},
			}
			_ = json.NewEncoder(w).Encode(resp)
			return
		}
		if r.Method == http.MethodGet && r.URL.Path == "/sandboxes/sandbox-abc" {
			resp := Sandbox{
				SandboxID: "sandbox-abc",
				EndedAt:   &endTime,
			}
			_ = json.NewEncoder(w).Encode(resp)
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	p := newTestProvider(t, server.URL)
	status, err := p.Status(context.Background(), "run_123")

	require.NoError(t, err)
	assert.Equal(t, provider.InstanceStatusStopped, status.Status)
}

func TestProviderStatusListError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(APIError{Message: "internal error"})
	}))
	defer server.Close()

	p := newTestProvider(t, server.URL)
	_, err := p.Status(context.Background(), "run_123")

	require.Error(t, err)
}

func TestProviderPauseError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet && r.URL.Path == "/sandboxes" {
			resp := []Sandbox{
				{
					SandboxID: "sandbox-abc",
					Metadata: map[string]string{
						"marionette.dev/runner-id": "run_123",
					},
				},
			}
			_ = json.NewEncoder(w).Encode(resp)
			return
		}
		if r.Method == http.MethodPost && r.URL.Path == "/sandboxes/sandbox-abc/pause" {
			w.WriteHeader(http.StatusInternalServerError)
			_ = json.NewEncoder(w).Encode(APIError{Message: "pause failed"})
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	p := newTestProvider(t, server.URL)
	err := p.Pause(context.Background(), "run_123")

	require.Error(t, err)
	var pauseErr *provider.ErrPauseFailed
	require.ErrorAs(t, err, &pauseErr)
}

func TestProviderPauseFindError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(APIError{Message: "internal error"})
	}))
	defer server.Close()

	p := newTestProvider(t, server.URL)
	err := p.Pause(context.Background(), "run_123")

	require.Error(t, err)
	var pauseErr *provider.ErrPauseFailed
	require.ErrorAs(t, err, &pauseErr)
}

func TestProviderUnpauseFindError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(APIError{Message: "internal error"})
	}))
	defer server.Close()

	p := newTestProvider(t, server.URL)
	err := p.Unpause(context.Background(), "run_123")

	require.Error(t, err)
	var resumeErr *provider.ErrResumeFailed
	require.ErrorAs(t, err, &resumeErr)
}

func TestProviderUnpauseResumeError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet && r.URL.Path == "/sandboxes" {
			resp := []Sandbox{
				{
					SandboxID: "sandbox-abc",
					Metadata: map[string]string{
						"marionette.dev/runner-id": "run_123",
					},
				},
			}
			_ = json.NewEncoder(w).Encode(resp)
			return
		}
		if r.Method == http.MethodPost && r.URL.Path == "/sandboxes/sandbox-abc/resume" {
			w.WriteHeader(http.StatusInternalServerError)
			_ = json.NewEncoder(w).Encode(APIError{Message: "resume failed"})
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	p := newTestProvider(t, server.URL)
	err := p.Unpause(context.Background(), "run_123")

	require.Error(t, err)
	var resumeErr *provider.ErrResumeFailed
	require.ErrorAs(t, err, &resumeErr)
}

func TestProviderSpawnWithTenantID(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost && r.URL.Path == "/sandboxes" {
			var req CreateSandboxRequest
			_ = json.NewDecoder(r.Body).Decode(&req)

			assert.Equal(t, "tenant_123", req.Metadata["marionette.dev/tenant-id"])

			resp := CreateSandboxResponse{
				SandboxID:  "sandbox-abc",
				TemplateID: "base",
			}
			_ = json.NewEncoder(w).Encode(resp)
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	p := newTestProvider(t, server.URL)
	instance, err := p.Spawn(context.Background(), provider.SpawnOptions{
		RunnerID: "run_123",
		TenantID: "tenant_123",
	})

	require.NoError(t, err)
	assert.Equal(t, "run_123", instance.ID)
}

func TestProviderListError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(APIError{Message: "internal error"})
	}))
	defer server.Close()

	p := newTestProvider(t, server.URL)
	_, err := p.List(context.Background())

	require.Error(t, err)
}

func TestProviderListWithStoppedSandbox(t *testing.T) {
	endTime := time.Now()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := []Sandbox{
			{
				SandboxID:  "sandbox-1",
				TemplateID: "base",
				EndedAt:    &endTime,
				Metadata: map[string]string{
					"marionette.dev/runner-id": "run_1",
				},
			},
		}
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	p := newTestProvider(t, server.URL)
	instances, err := p.List(context.Background())

	require.NoError(t, err)
	assert.Len(t, instances, 1)
	assert.Equal(t, provider.InstanceStatusStopped, instances[0].Status)
}

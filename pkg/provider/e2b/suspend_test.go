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

func newTestProviderWithSuspendConfig(t *testing.T, serverURL string, suspendConfig json.RawMessage) *Provider {
	t.Helper()

	cfg := &store.ProviderConfig{
		Name:     "test-e2b",
		Provider: "e2b",
		Config: json.RawMessage(`{
			"api_url": "` + serverURL + `",
			"api_key": "test-key",
			"template": "base"
		}`),
		SuspendConfig: suspendConfig,
	}

	p, err := New(cfg)
	require.NoError(t, err)
	return p
}

func TestProviderSuspendWithPause(t *testing.T) {
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
	result, err := p.Suspend(context.Background(), "run_123", provider.SuspendOptions{
		Strategy: provider.SuspendStrategyPause,
	})

	require.NoError(t, err)
	assert.Equal(t, provider.SuspendStrategyPause, result.Strategy)
	assert.False(t, result.SuspendedAt.IsZero())
}

func TestProviderSuspendWithTerminate(t *testing.T) {
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
	result, err := p.Suspend(context.Background(), "run_123", provider.SuspendOptions{
		Strategy: provider.SuspendStrategyTerminate,
	})

	require.NoError(t, err)
	assert.Equal(t, provider.SuspendStrategyTerminate, result.Strategy)
}

func TestProviderSuspendDefaultStrategy(t *testing.T) {
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
	// No strategy specified - should use default (pause)
	result, err := p.Suspend(context.Background(), "run_123", provider.SuspendOptions{})

	require.NoError(t, err)
	assert.Equal(t, provider.SuspendStrategyPause, result.Strategy)
}

func TestProviderSuspendUnsupportedStrategy(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	defer server.Close()

	p := newTestProvider(t, server.URL)
	_, err := p.Suspend(context.Background(), "run_123", provider.SuspendOptions{
		Strategy: provider.SuspendStrategySnapshot, // Not supported by E2B
	})

	require.Error(t, err)
	var strategyErr *provider.ErrStrategyNotSupported
	require.ErrorAs(t, err, &strategyErr)
}

func TestProviderSuspendFallback(t *testing.T) {
	pauseFailed := false
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
			// First pause attempt fails
			if !pauseFailed {
				pauseFailed = true
				w.WriteHeader(http.StatusInternalServerError)
				_ = json.NewEncoder(w).Encode(APIError{Message: "pause failed"})
				return
			}
			w.WriteHeader(http.StatusOK)
			return
		}
		if r.Method == http.MethodDelete && r.URL.Path == "/sandboxes/sandbox-abc" {
			// Fallback to terminate succeeds
			w.WriteHeader(http.StatusNoContent)
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	p := newTestProviderWithSuspendConfig(t, server.URL, json.RawMessage(`{
		"strategy": "pause",
		"fallback": "terminate"
	}`))

	result, err := p.Suspend(context.Background(), "run_123", provider.SuspendOptions{})

	require.NoError(t, err)
	assert.Equal(t, provider.SuspendStrategyTerminate, result.Strategy)
}

func TestProviderResumeFromPaused(t *testing.T) {
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
			// Return conflict (paused)
			w.WriteHeader(http.StatusConflict)
			_ = json.NewEncoder(w).Encode(APIError{Message: "sandbox is paused"})
			return
		}
		if r.Method == http.MethodPost && r.URL.Path == "/sandboxes/sandbox-abc/resume" {
			resp := Sandbox{
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
	instance, err := p.Resume(context.Background(), "sess_123", provider.ResumeOptions{
		RunnerID: "run_123",
	})

	require.NoError(t, err)
	assert.Equal(t, "run_123", instance.ID)
	assert.Equal(t, "sandbox-abc", instance.ProviderID)
	assert.Equal(t, provider.InstanceStatusRunning, instance.Status)
}

func TestProviderResumeFromTerminated(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet && r.URL.Path == "/sandboxes" {
			// No sandboxes found for this runner
			_ = json.NewEncoder(w).Encode([]Sandbox{})
			return
		}
		if r.Method == http.MethodPost && r.URL.Path == "/sandboxes" {
			resp := CreateSandboxResponse{
				SandboxID:  "sandbox-new",
				TemplateID: "base",
			}
			_ = json.NewEncoder(w).Encode(resp)
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	p := newTestProvider(t, server.URL)
	instance, err := p.Resume(context.Background(), "sess_123", provider.ResumeOptions{
		RunnerID: "run_123",
		SpawnOpts: &provider.SpawnOptions{
			RunnerID: "run_123",
			Name:     "new-runner",
		},
	})

	require.NoError(t, err)
	assert.Equal(t, "run_123", instance.ID)
	assert.Equal(t, "sandbox-new", instance.ProviderID)
}

func TestProviderResumeNoRunnerID(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	defer server.Close()

	p := newTestProvider(t, server.URL)
	_, err := p.Resume(context.Background(), "sess_123", provider.ResumeOptions{})

	require.Error(t, err)
	var resumeErr *provider.ErrResumeFailed
	require.ErrorAs(t, err, &resumeErr)
	assert.Contains(t, resumeErr.Cause.Error(), "runner ID is required")
}

func TestProviderResumeNoSpawnOpts(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet && r.URL.Path == "/sandboxes" {
			_ = json.NewEncoder(w).Encode([]Sandbox{})
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	p := newTestProvider(t, server.URL)
	_, err := p.Resume(context.Background(), "sess_123", provider.ResumeOptions{
		RunnerID: "run_123",
		// No SpawnOpts provided
	})

	require.Error(t, err)
	var resumeErr *provider.ErrResumeFailed
	require.ErrorAs(t, err, &resumeErr)
	assert.Contains(t, resumeErr.Cause.Error(), "spawn options required")
}

func TestProviderResumeAlreadyRunning(t *testing.T) {
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
				SandboxID:  "sandbox-abc",
				TemplateID: "base",
			}
			_ = json.NewEncoder(w).Encode(resp)
			return
		}
		if r.Method == http.MethodPost && r.URL.Path == "/sandboxes/sandbox-abc/resume" {
			// Sandbox is already running, resume is a no-op
			resp := Sandbox{
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
	instance, err := p.Resume(context.Background(), "sess_123", provider.ResumeOptions{
		RunnerID: "run_123",
	})

	require.NoError(t, err)
	assert.Equal(t, "run_123", instance.ID)
	assert.Equal(t, provider.InstanceStatusRunning, instance.Status)
}

func TestSupportsStrategy(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	defer server.Close()

	p := newTestProvider(t, server.URL)

	assert.True(t, p.suspendDispatcher().Supports(provider.SuspendStrategyPause))
	assert.True(t, p.suspendDispatcher().Supports(provider.SuspendStrategyTerminate))
	assert.False(t, p.suspendDispatcher().Supports(provider.SuspendStrategySnapshot))
	assert.False(t, p.suspendDispatcher().Supports(provider.SuspendStrategyTerminatePreserveStorage))
	assert.False(t, p.suspendDispatcher().Supports(provider.SuspendStrategyReleaseToPool))
}

func TestProviderSuspendNoFallback(t *testing.T) {
	pauseAttempts := 0
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
			pauseAttempts++
			w.WriteHeader(http.StatusInternalServerError)
			_ = json.NewEncoder(w).Encode(APIError{Message: "pause failed"})
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	// Use config with same fallback as primary (no actual fallback)
	p := newTestProviderWithSuspendConfig(t, server.URL, json.RawMessage(`{
		"strategy": "pause",
		"fallback": "pause"
	}`))

	_, err := p.Suspend(context.Background(), "run_123", provider.SuspendOptions{})

	require.Error(t, err)
	var suspendErr *provider.ErrSuspendFailed
	require.ErrorAs(t, err, &suspendErr)
}

func TestProviderResumeError(t *testing.T) {
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
	_, err := p.Resume(context.Background(), "sess_123", provider.ResumeOptions{
		RunnerID: "run_123",
	})

	require.Error(t, err)
	var resumeErr *provider.ErrResumeFailed
	require.ErrorAs(t, err, &resumeErr)
}

func TestProviderResumeFindSandboxError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(APIError{Message: "internal error"})
	}))
	defer server.Close()

	p := newTestProvider(t, server.URL)
	_, err := p.Resume(context.Background(), "sess_123", provider.ResumeOptions{
		RunnerID: "run_123",
	})

	require.Error(t, err)
	var resumeErr *provider.ErrResumeFailed
	require.ErrorAs(t, err, &resumeErr)
}

func TestProviderResumeFromEndedSandbox(t *testing.T) {
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
				SandboxID:  "sandbox-abc",
				TemplateID: "base",
				EndedAt:    &endTime,
			}
			_ = json.NewEncoder(w).Encode(resp)
			return
		}
		if r.Method == http.MethodPost && r.URL.Path == "/sandboxes" {
			resp := CreateSandboxResponse{
				SandboxID:  "sandbox-new",
				TemplateID: "base",
			}
			_ = json.NewEncoder(w).Encode(resp)
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	p := newTestProvider(t, server.URL)
	instance, err := p.Resume(context.Background(), "sess_123", provider.ResumeOptions{
		RunnerID: "run_123",
		SpawnOpts: &provider.SpawnOptions{
			RunnerID: "run_123",
		},
	})

	require.NoError(t, err)
	assert.Equal(t, "sandbox-new", instance.ProviderID)
}

func TestProviderResumeResumeError(t *testing.T) {
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
				SandboxID:  "sandbox-abc",
				TemplateID: "base",
			}
			_ = json.NewEncoder(w).Encode(resp)
			return
		}
		if r.Method == http.MethodPost && r.URL.Path == "/sandboxes/sandbox-abc/resume" {
			// Conflict means sandbox is paused
			w.WriteHeader(http.StatusConflict)
			_ = json.NewEncoder(w).Encode(APIError{Message: "resume failed"})
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	p := newTestProvider(t, server.URL)
	_, err := p.Resume(context.Background(), "sess_123", provider.ResumeOptions{
		RunnerID: "run_123",
	})

	require.Error(t, err)
	var resumeErr *provider.ErrResumeFailed
	require.ErrorAs(t, err, &resumeErr)
}

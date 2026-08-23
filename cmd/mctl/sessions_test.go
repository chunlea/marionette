package main

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/chunlea/marionette/pkg/client"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSessionsCreate(t *testing.T) {
	tests := []struct {
		name       string
		args       []string
		mockSetup  func(*client.MockClient)
		wantErr    bool
		wantOutput string
	}{
		{
			name: "create session successfully",
			args: []string{"sessions", "create", "--agent", "claude", "--name", "test-session"},
			mockSetup: func(m *client.MockClient) {
				m.CreateSessionFunc = func(_ context.Context, opts client.CreateSessionOptions) (*client.Session, error) {
					name := "test-session"
					return &client.Session{
						ID:        "sess_test123",
						Name:      &name,
						Status:    "pending",
						Agent:     opts.Agent,
						CreatedAt: time.Now(),
					}, nil
				}
			},
			wantOutput: "sess_test123",
		},
		{
			name: "create session with API key",
			args: []string{"sessions", "create", "--agent", "claude", "--agent-api-key", "sk-xxx"},
			mockSetup: func(m *client.MockClient) {
				m.CreateSessionFunc = func(_ context.Context, opts client.CreateSessionOptions) (*client.Session, error) {
					assert.Equal(t, "sk-xxx", opts.APIKey)
					return &client.Session{
						ID:        "sess_byok123",
						Status:    "pending",
						Agent:     "claude",
						CreatedAt: time.Now(),
					}, nil
				}
			},
			wantOutput: "sess_byok123",
		},
		{
			name:       "no client configured",
			args:       []string{"sessions", "create", "--agent", "claude"},
			mockSetup:  nil, // apiClient will be nil
			wantErr:    true,
			wantOutput: "no API client configured",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Setup mock client
			if tt.mockSetup != nil {
				mock := &client.MockClient{}
				tt.mockSetup(mock)
				SetClient(mock)
			} else {
				SetClient(nil)
			}

			// Capture output
			buf := &bytes.Buffer{}
			oldGetOutput := getOutput
			defer func() {
				// Reset global state
				ResetClient()
				// Restore getOutput - can't easily do this, so we skip
				_ = oldGetOutput
			}()

			// Execute command
			rootCmd.SetOut(buf)
			rootCmd.SetErr(buf)
			rootCmd.SetArgs(tt.args)
			err := rootCmd.Execute()

			if tt.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.wantOutput)
			} else {
				require.NoError(t, err)
				// Note: output goes to getOutput(), not SetOut()
			}
		})
	}
}

func TestSessionsList(t *testing.T) {
	name1 := "project-1"
	name2 := "project-2"

	tests := []struct {
		name       string
		args       []string
		mockSetup  func(*client.MockClient)
		wantErr    bool
		wantOutput string
	}{
		{
			name: "list sessions successfully",
			args: []string{"sessions", "list"},
			mockSetup: func(m *client.MockClient) {
				m.ListSessionsFunc = func(_ context.Context, _ client.ListSessionsOptions) (*client.ListResult[client.Session], error) {
					return &client.ListResult[client.Session]{
						Items: []*client.Session{
							{ID: "sess_1", Name: &name1, Status: "active", Agent: "claude", CreatedAt: time.Now()},
							{ID: "sess_2", Name: &name2, Status: "pending", Agent: "claude", CreatedAt: time.Now()},
						},
						TotalCount: 2,
					}, nil
				}
			},
			wantOutput: "sess_1",
		},
		{
			name: "list with filters",
			args: []string{"sessions", "list", "--status", "active", "--agent", "claude"},
			mockSetup: func(m *client.MockClient) {
				m.ListSessionsFunc = func(_ context.Context, opts client.ListSessionsOptions) (*client.ListResult[client.Session], error) {
					assert.Equal(t, []string{"active"}, opts.Status)
					assert.Equal(t, "claude", opts.Agent)
					return &client.ListResult[client.Session]{Items: []*client.Session{}}, nil
				}
			},
			wantOutput: "No sessions found",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.mockSetup != nil {
				mock := &client.MockClient{}
				tt.mockSetup(mock)
				SetClient(mock)
			} else {
				SetClient(nil)
			}
			defer ResetClient()

			rootCmd.SetArgs(tt.args)
			err := rootCmd.Execute()

			if tt.wantErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestSessionsGet(t *testing.T) {
	tests := []struct {
		name       string
		args       []string
		mockSetup  func(*client.MockClient)
		wantErr    bool
		wantOutput string
	}{
		{
			name: "get session successfully",
			args: []string{"sessions", "get", "sess_test123"},
			mockSetup: func(m *client.MockClient) {
				m.GetSessionFunc = func(_ context.Context, id string) (*client.Session, error) {
					assert.Equal(t, "sess_test123", id)
					name := "test"
					return &client.Session{
						ID:        id,
						Name:      &name,
						Status:    "active",
						Agent:     "claude",
						CreatedAt: time.Now(),
					}, nil
				}
			},
			wantOutput: "sess_test123",
		},
		{
			name: "session not found",
			args: []string{"sessions", "get", "sess_notfound"},
			mockSetup: func(m *client.MockClient) {
				m.GetSessionFunc = func(_ context.Context, _ string) (*client.Session, error) {
					return nil, client.ErrNotFound
				}
			},
			wantErr:    true,
			wantOutput: "not found",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.mockSetup != nil {
				mock := &client.MockClient{}
				tt.mockSetup(mock)
				SetClient(mock)
			} else {
				SetClient(nil)
			}
			defer ResetClient()

			rootCmd.SetArgs(tt.args)
			err := rootCmd.Execute()

			if tt.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.wantOutput)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestSessionsSuspend(t *testing.T) {
	tests := []struct {
		name      string
		args      []string
		mockSetup func(*client.MockClient)
		wantErr   bool
	}{
		{
			name: "suspend session successfully",
			args: []string{"sessions", "suspend", "sess_test123"},
			mockSetup: func(m *client.MockClient) {
				m.SuspendSessionFunc = func(_ context.Context, id string) error {
					assert.Equal(t, "sess_test123", id)
					return nil
				}
			},
			wantErr: false,
		},
		{
			name: "session not found",
			args: []string{"sessions", "suspend", "sess_notfound"},
			mockSetup: func(m *client.MockClient) {
				m.SuspendSessionFunc = func(_ context.Context, _ string) error {
					return client.ErrNotFound
				}
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.mockSetup != nil {
				mock := &client.MockClient{}
				tt.mockSetup(mock)
				SetClient(mock)
			} else {
				SetClient(nil)
			}
			defer ResetClient()

			rootCmd.SetArgs(tt.args)
			err := rootCmd.Execute()

			if tt.wantErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestSessionsResume(t *testing.T) {
	mock := &client.MockClient{}
	mock.ResumeSessionFunc = func(_ context.Context, id string) error {
		assert.Equal(t, "sess_test123", id)
		return nil
	}
	SetClient(mock)
	defer ResetClient()

	rootCmd.SetArgs([]string{"sessions", "resume", "sess_test123"})
	err := rootCmd.Execute()
	require.NoError(t, err)
}

func TestSessionsTerminate(t *testing.T) {
	mock := &client.MockClient{}
	mock.TerminateSessionFunc = func(_ context.Context, id string) error {
		assert.Equal(t, "sess_test123", id)
		return nil
	}
	SetClient(mock)
	defer ResetClient()

	rootCmd.SetArgs([]string{"sessions", "terminate", "sess_test123"})
	err := rootCmd.Execute()
	require.NoError(t, err)
}

func TestSessionsOutputFormat(t *testing.T) {
	name := "test-session"
	session := &client.Session{
		ID:        "sess_json123",
		Name:      &name,
		Status:    "active",
		Agent:     "claude",
		CreatedAt: time.Now(),
	}

	mock := &client.MockClient{}
	mock.GetSessionFunc = func(_ context.Context, _ string) (*client.Session, error) {
		return session, nil
	}
	SetClient(mock)
	defer ResetClient()

	// Test JSON output
	buf := &bytes.Buffer{}
	printer := NewPrinter("json", buf)
	err := printer.PrintSession(session)
	require.NoError(t, err)

	var result map[string]interface{}
	err = json.Unmarshal(buf.Bytes(), &result)
	require.NoError(t, err)
	assert.Equal(t, "sess_json123", result["id"])
}

func TestSessionsLogsCommand(t *testing.T) {
	mock := &client.MockClient{}
	mock.GetSessionLogsFunc = func(_ context.Context, id string, opts client.GetLogsOptions) (client.LogIterator, error) {
		if id != "sess_test123" {
			t.Errorf("unexpected session id %q", id)
		}
		if opts.Archived != "true" {
			t.Errorf("expected archived=true, got %q", opts.Archived)
		}
		return &client.MockLogIterator{
			Logs: []*client.Log{
				{ID: "log_1", Level: "info", Content: "from the archive"},
			},
		}, nil
	}

	SetClient(mock)
	t.Cleanup(ResetClient)

	out := captureOutput(t, func() {
		rootCmd.SetArgs([]string{"sessions", "logs", "sess_test123", "--archived", "true"})
		if err := rootCmd.Execute(); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	if !strings.Contains(out, "from the archive") {
		t.Fatalf("expected the archived line in the output, got %q", out)
	}
}

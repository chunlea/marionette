package main

import (
	"bytes"
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/chunlea/marionette/pkg/client"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTasksCreate(t *testing.T) {
	tests := []struct {
		name       string
		args       []string
		mockSetup  func(*client.MockClient)
		wantErr    bool
		wantOutput string
	}{
		{
			name: "create task successfully",
			args: []string{"tasks", "create", "--session", "sess_xxx", "--prompt", "Build an API"},
			mockSetup: func(m *client.MockClient) {
				m.CreateTaskFunc = func(_ context.Context, opts client.CreateTaskOptions) (*client.Task, error) {
					assert.Equal(t, "sess_xxx", opts.SessionID)
					assert.Equal(t, "Build an API", opts.Prompt)
					return &client.Task{
						ID:        "task_test123",
						SessionID: opts.SessionID,
						Prompt:    opts.Prompt,
						Status:    "pending",
						CreatedAt: time.Now(),
					}, nil
				}
			},
			wantOutput: "task_test123",
		},
		{
			name:       "no client configured",
			args:       []string{"tasks", "create", "--session", "sess_xxx", "--prompt", "test"},
			mockSetup:  nil, // apiClient will be nil
			wantErr:    true,
			wantOutput: "no API client configured",
		},
		{
			name: "continue from previous task",
			args: []string{"tasks", "create", "--continue", "task_prev", "--prompt", "Add auth"},
			mockSetup: func(m *client.MockClient) {
				m.CreateTaskFunc = func(_ context.Context, opts client.CreateTaskOptions) (*client.Task, error) {
					assert.Equal(t, "task_prev", opts.ContinueFrom)
					return &client.Task{
						ID:        "task_new123",
						SessionID: "sess_inherited",
						Prompt:    opts.Prompt,
						Status:    "pending",
						CreatedAt: time.Now(),
					}, nil
				}
			},
			wantOutput: "task_new123",
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

func TestTasksList(t *testing.T) {
	tests := []struct {
		name       string
		args       []string
		mockSetup  func(*client.MockClient)
		wantErr    bool
		wantOutput string
	}{
		{
			name: "list tasks successfully",
			args: []string{"tasks", "list", "--session", "sess_xxx"},
			mockSetup: func(m *client.MockClient) {
				m.ListTasksFunc = func(_ context.Context, opts client.ListTasksOptions) (*client.ListResult[client.Task], error) {
					assert.Equal(t, "sess_xxx", opts.SessionID)
					return &client.ListResult[client.Task]{
						Items: []*client.Task{
							{ID: "task_1", SessionID: "sess_xxx", Prompt: "Build API", Status: "completed", CreatedAt: time.Now()},
							{ID: "task_2", SessionID: "sess_xxx", Prompt: "Add tests", Status: "running", CreatedAt: time.Now()},
						},
						TotalCount: 2,
					}, nil
				}
			},
			wantOutput: "task_1",
		},
		{
			name: "list with status filter",
			args: []string{"tasks", "list", "--status", "running,pending"},
			mockSetup: func(m *client.MockClient) {
				m.ListTasksFunc = func(_ context.Context, opts client.ListTasksOptions) (*client.ListResult[client.Task], error) {
					assert.Equal(t, []string{"running", "pending"}, opts.Status)
					return &client.ListResult[client.Task]{Items: []*client.Task{}}, nil
				}
			},
			wantOutput: "No tasks found",
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

func TestTasksGet(t *testing.T) {
	tests := []struct {
		name       string
		args       []string
		mockSetup  func(*client.MockClient)
		wantErr    bool
		wantOutput string
	}{
		{
			name: "get task successfully",
			args: []string{"tasks", "get", "task_test123"},
			mockSetup: func(m *client.MockClient) {
				m.GetTaskFunc = func(_ context.Context, id string) (*client.Task, error) {
					assert.Equal(t, "task_test123", id)
					return &client.Task{
						ID:        id,
						SessionID: "sess_xxx",
						Prompt:    "Build API",
						Status:    "completed",
						CreatedAt: time.Now(),
					}, nil
				}
			},
			wantOutput: "task_test123",
		},
		{
			name: "task not found",
			args: []string{"tasks", "get", "task_notfound"},
			mockSetup: func(m *client.MockClient) {
				m.GetTaskFunc = func(_ context.Context, _ string) (*client.Task, error) {
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

func TestTasksCancel(t *testing.T) {
	tests := []struct {
		name      string
		args      []string
		mockSetup func(*client.MockClient)
		wantErr   bool
	}{
		{
			name: "cancel task successfully",
			args: []string{"tasks", "cancel", "task_test123"},
			mockSetup: func(m *client.MockClient) {
				m.CancelTaskFunc = func(_ context.Context, id string) error {
					assert.Equal(t, "task_test123", id)
					return nil
				}
			},
			wantErr: false,
		},
		{
			name: "task not found",
			args: []string{"tasks", "cancel", "task_notfound"},
			mockSetup: func(m *client.MockClient) {
				m.CancelTaskFunc = func(_ context.Context, _ string) error {
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

func TestTasksLogs(t *testing.T) {
	mock := &client.MockClient{}
	mock.GetTaskLogsFunc = func(_ context.Context, id string, _ client.GetLogsOptions) (client.LogIterator, error) {
		assert.Equal(t, "task_test123", id)
		return &client.MockLogIterator{
			Logs: []*client.Log{
				{ID: "log_1", Content: "Starting task...", Level: "info", CreatedAt: time.Now()},
				{ID: "log_2", Content: "Task completed", Level: "info", CreatedAt: time.Now()},
			},
		}, nil
	}
	SetClient(mock)
	defer ResetClient()

	rootCmd.SetArgs([]string{"tasks", "logs", "task_test123"})
	err := rootCmd.Execute()
	require.NoError(t, err)
}

func TestTasksOutputFormat(t *testing.T) {
	task := &client.Task{
		ID:        "task_json123",
		SessionID: "sess_xxx",
		Prompt:    "Build API",
		Status:    "completed",
		CreatedAt: time.Now(),
	}

	// Test JSON output
	buf := &bytes.Buffer{}
	printer := NewPrinter("json", buf)
	err := printer.PrintTask(task)
	require.NoError(t, err)

	var result map[string]interface{}
	err = json.Unmarshal(buf.Bytes(), &result)
	require.NoError(t, err)
	assert.Equal(t, "task_json123", result["id"])
	assert.Equal(t, "sess_xxx", result["session_id"])
}

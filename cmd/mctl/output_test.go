package main

import (
	"bytes"
	"encoding/json"
	"testing"
	"time"

	"github.com/chunlea/marionette/pkg/client"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

func TestNewPrinter(t *testing.T) {
	buf := &bytes.Buffer{}
	printer := NewPrinter("json", buf)

	assert.Equal(t, OutputJSON, printer.format)
	assert.Equal(t, buf, printer.writer)
}

func TestPrinter_PrintJSON(t *testing.T) {
	buf := &bytes.Buffer{}
	printer := NewPrinter("json", buf)

	data := map[string]string{"key": "value"}
	err := printer.Print(data)
	require.NoError(t, err)

	var result map[string]string
	err = json.Unmarshal(buf.Bytes(), &result)
	require.NoError(t, err)
	assert.Equal(t, "value", result["key"])
}

func TestPrinter_PrintYAML(t *testing.T) {
	buf := &bytes.Buffer{}
	printer := NewPrinter("yaml", buf)

	data := map[string]string{"key": "value"}
	err := printer.Print(data)
	require.NoError(t, err)

	var result map[string]string
	err = yaml.Unmarshal(buf.Bytes(), &result)
	require.NoError(t, err)
	assert.Equal(t, "value", result["key"])
}

func TestPrinter_PrintYAML_ComplexStructure(t *testing.T) {
	buf := &bytes.Buffer{}
	printer := NewPrinter("yaml", buf)

	data := map[string]any{
		"name":   "test",
		"count":  42,
		"nested": map[string]string{"a": "b"},
		"list":   []string{"one", "two"},
	}
	err := printer.Print(data)
	require.NoError(t, err)

	var result map[string]any
	err = yaml.Unmarshal(buf.Bytes(), &result)
	require.NoError(t, err)
	assert.Equal(t, "test", result["name"])
	assert.Equal(t, 42, result["count"])
}

func TestPrinter_PrintTable_Unsupported(t *testing.T) {
	buf := &bytes.Buffer{}
	printer := NewPrinter("table", buf)

	// Print() on table format returns error for arbitrary types
	err := printer.Print(map[string]string{"key": "value"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "cannot print type")
}

func TestPrinter_PrintTable(t *testing.T) {
	buf := &bytes.Buffer{}
	printer := NewPrinter("table", buf)

	headers := []string{"ID", "NAME", "STATUS"}
	rows := [][]string{
		{"1", "first", "active"},
		{"2", "second", "pending"},
	}

	err := printer.PrintTable(headers, rows)
	require.NoError(t, err)

	output := buf.String()
	assert.Contains(t, output, "ID")
	assert.Contains(t, output, "NAME")
	assert.Contains(t, output, "STATUS")
	assert.Contains(t, output, "first")
	assert.Contains(t, output, "second")
}

func TestPrinter_PrintSession_Table(t *testing.T) {
	buf := &bytes.Buffer{}
	printer := NewPrinter("table", buf)

	name := "test-session"
	runnerID := "run_123"
	session := &client.Session{
		ID:        "sess_123",
		Name:      &name,
		Status:    "active",
		Agent:     "claude",
		RunnerID:  &runnerID,
		CreatedAt: time.Now(),
	}

	err := printer.PrintSession(session)
	require.NoError(t, err)

	output := buf.String()
	assert.Contains(t, output, "sess_123")
	assert.Contains(t, output, "test-session")
	assert.Contains(t, output, "active")
	assert.Contains(t, output, "claude")
	assert.Contains(t, output, "run_123")
}

func TestPrinter_PrintSession_JSON(t *testing.T) {
	buf := &bytes.Buffer{}
	printer := NewPrinter("json", buf)

	name := "test-session"
	session := &client.Session{
		ID:        "sess_123",
		Name:      &name,
		Status:    "active",
		Agent:     "claude",
		CreatedAt: time.Now(),
	}

	err := printer.PrintSession(session)
	require.NoError(t, err)

	var result map[string]any
	err = json.Unmarshal(buf.Bytes(), &result)
	require.NoError(t, err)
	assert.Equal(t, "sess_123", result["id"])
}

func TestPrinter_PrintSession_YAML(t *testing.T) {
	buf := &bytes.Buffer{}
	printer := NewPrinter("yaml", buf)

	name := "test-session"
	session := &client.Session{
		ID:        "sess_123",
		Name:      &name,
		Status:    "active",
		Agent:     "claude",
		CreatedAt: time.Now(),
	}

	err := printer.PrintSession(session)
	require.NoError(t, err)

	var result map[string]any
	err = yaml.Unmarshal(buf.Bytes(), &result)
	require.NoError(t, err)
	assert.Equal(t, "sess_123", result["id"])
}

func TestPrinter_PrintSessionList_Table(t *testing.T) {
	buf := &bytes.Buffer{}
	printer := NewPrinter("table", buf)

	name1 := "session-1"
	name2 := "session-2"
	sessions := []*client.Session{
		{ID: "sess_1", Name: &name1, Status: "active", Agent: "claude", CreatedAt: time.Now()},
		{ID: "sess_2", Name: &name2, Status: "pending", Agent: "codex", CreatedAt: time.Now()},
	}

	err := printer.PrintSessionList(sessions)
	require.NoError(t, err)

	output := buf.String()
	assert.Contains(t, output, "sess_1")
	assert.Contains(t, output, "sess_2")
	assert.Contains(t, output, "session-1")
	assert.Contains(t, output, "session-2")
}

func TestPrinter_PrintSessionList_JSON(t *testing.T) {
	buf := &bytes.Buffer{}
	printer := NewPrinter("json", buf)

	name := "session-1"
	sessions := []*client.Session{
		{ID: "sess_1", Name: &name, Status: "active", Agent: "claude", CreatedAt: time.Now()},
	}

	err := printer.PrintSessionList(sessions)
	require.NoError(t, err)

	var result []map[string]any
	err = json.Unmarshal(buf.Bytes(), &result)
	require.NoError(t, err)
	assert.Len(t, result, 1)
	assert.Equal(t, "sess_1", result[0]["id"])
}

func TestPrinter_PrintTask_Table(t *testing.T) {
	buf := &bytes.Buffer{}
	printer := NewPrinter("table", buf)

	task := &client.Task{
		ID:        "task_123",
		SessionID: "sess_456",
		Status:    "running",
		Prompt:    "Build an API",
		CreatedAt: time.Now(),
	}

	err := printer.PrintTask(task)
	require.NoError(t, err)

	output := buf.String()
	assert.Contains(t, output, "task_123")
	assert.Contains(t, output, "sess_456")
	assert.Contains(t, output, "running")
	assert.Contains(t, output, "Build an API")
}

func TestPrinter_PrintTask_LongPrompt(t *testing.T) {
	buf := &bytes.Buffer{}
	printer := NewPrinter("table", buf)

	longPrompt := "This is a very long prompt that exceeds 40 characters and should be truncated with ellipsis"
	task := &client.Task{
		ID:        "task_123",
		SessionID: "sess_456",
		Status:    "running",
		Prompt:    longPrompt,
		CreatedAt: time.Now(),
	}

	err := printer.PrintTask(task)
	require.NoError(t, err)

	output := buf.String()
	// First 37 characters + "..." = 40 total
	assert.Contains(t, output, "This is a very long prompt that exce")
	assert.Contains(t, output, "...")
	assert.NotContains(t, output, "should be truncated")
}

func TestPrinter_PrintTaskList_Table(t *testing.T) {
	buf := &bytes.Buffer{}
	printer := NewPrinter("table", buf)

	tasks := []*client.Task{
		{ID: "task_1", SessionID: "sess_1", Status: "completed", Prompt: "Task 1", CreatedAt: time.Now()},
		{ID: "task_2", SessionID: "sess_1", Status: "running", Prompt: "Task 2", CreatedAt: time.Now()},
	}

	err := printer.PrintTaskList(tasks)
	require.NoError(t, err)

	output := buf.String()
	assert.Contains(t, output, "task_1")
	assert.Contains(t, output, "task_2")
}

func TestPrinter_PrintTaskList_JSON(t *testing.T) {
	buf := &bytes.Buffer{}
	printer := NewPrinter("json", buf)

	tasks := []*client.Task{
		{ID: "task_1", SessionID: "sess_1", Status: "completed", Prompt: "Task 1", CreatedAt: time.Now()},
	}

	err := printer.PrintTaskList(tasks)
	require.NoError(t, err)

	var result []map[string]any
	err = json.Unmarshal(buf.Bytes(), &result)
	require.NoError(t, err)
	assert.Len(t, result, 1)
	assert.Equal(t, "task_1", result[0]["id"])
}

func TestFormatTime(t *testing.T) {
	tests := []struct {
		name     string
		duration time.Duration
		want     string
	}{
		{
			name:     "just now (seconds)",
			duration: 30 * time.Second,
			want:     "just now",
		},
		{
			name:     "1 minute ago",
			duration: 1 * time.Minute,
			want:     "1 minute ago",
		},
		{
			name:     "multiple minutes ago",
			duration: 5 * time.Minute,
			want:     "5 minutes ago",
		},
		{
			name:     "1 hour ago",
			duration: 1 * time.Hour,
			want:     "1 hour ago",
		},
		{
			name:     "multiple hours ago",
			duration: 3 * time.Hour,
			want:     "3 hours ago",
		},
		{
			name:     "1 day ago",
			duration: 24 * time.Hour,
			want:     "1 day ago",
		},
		{
			name:     "multiple days ago",
			duration: 3 * 24 * time.Hour,
			want:     "3 days ago",
		},
		{
			name:     "more than a week ago - date format",
			duration: 8 * 24 * time.Hour,
			want:     time.Now().Add(-8 * 24 * time.Hour).Format("2006-01-02"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			testTime := time.Now().Add(-tt.duration)
			result := formatTime(testTime)
			assert.Equal(t, tt.want, result)
		})
	}
}

func TestFormatTime_EdgeCases(t *testing.T) {
	// Test boundary between minutes and hours
	t.Run("59 minutes ago", func(t *testing.T) {
		testTime := time.Now().Add(-59 * time.Minute)
		result := formatTime(testTime)
		assert.Equal(t, "59 minutes ago", result)
	})

	// Test boundary between hours and days
	t.Run("23 hours ago", func(t *testing.T) {
		testTime := time.Now().Add(-23 * time.Hour)
		result := formatTime(testTime)
		assert.Equal(t, "23 hours ago", result)
	})

	// Test boundary between days and week
	t.Run("6 days ago", func(t *testing.T) {
		testTime := time.Now().Add(-6 * 24 * time.Hour)
		result := formatTime(testTime)
		assert.Equal(t, "6 days ago", result)
	})
}

func TestSessionToRow(t *testing.T) {
	t.Run("with all fields", func(t *testing.T) {
		name := "test-session"
		runnerID := "run_123"
		session := &client.Session{
			ID:        "sess_123",
			Name:      &name,
			Status:    "active",
			Agent:     "claude",
			RunnerID:  &runnerID,
			CreatedAt: time.Now(),
		}

		row := sessionToRow(session)
		assert.Len(t, row, 6)
		assert.Equal(t, "sess_123", row[0])
		assert.Equal(t, "test-session", row[1])
		assert.Equal(t, "active", row[2])
		assert.Equal(t, "claude", row[3])
		assert.Equal(t, "run_123", row[4])
	})

	t.Run("with nil name and runner", func(t *testing.T) {
		session := &client.Session{
			ID:        "sess_123",
			Name:      nil,
			Status:    "pending",
			Agent:     "claude",
			RunnerID:  nil,
			CreatedAt: time.Now(),
		}

		row := sessionToRow(session)
		assert.Equal(t, "", row[1]) // name should be empty
		assert.Equal(t, "", row[4]) // runner should be empty
	})
}

func TestTaskToRow(t *testing.T) {
	t.Run("with short prompt", func(t *testing.T) {
		task := &client.Task{
			ID:        "task_123",
			SessionID: "sess_456",
			Status:    "running",
			Prompt:    "Short prompt",
			CreatedAt: time.Now(),
		}

		row := taskToRow(task)
		assert.Len(t, row, 5)
		assert.Equal(t, "task_123", row[0])
		assert.Equal(t, "sess_456", row[1])
		assert.Equal(t, "running", row[2])
		assert.Equal(t, "Short prompt", row[3])
	})

	t.Run("with prompt exactly 40 chars", func(t *testing.T) {
		prompt := "This is exactly forty characters long!!" // 40 chars
		task := &client.Task{
			ID:        "task_123",
			SessionID: "sess_456",
			Status:    "running",
			Prompt:    prompt,
			CreatedAt: time.Now(),
		}

		row := taskToRow(task)
		assert.Equal(t, prompt, row[3]) // Should not be truncated
	})

	t.Run("with prompt longer than 40 chars", func(t *testing.T) {
		prompt := "This prompt is definitely longer than forty characters and needs truncation"
		task := &client.Task{
			ID:        "task_123",
			SessionID: "sess_456",
			Status:    "running",
			Prompt:    prompt,
			CreatedAt: time.Now(),
		}

		row := taskToRow(task)
		assert.Len(t, row[3], 40)
		assert.True(t, len(row[3]) <= 40)
		assert.Equal(t, "...", row[3][37:40])
	})
}

func TestParseLabels(t *testing.T) {
	tests := []struct {
		name   string
		labels []string
		want   map[string]string
	}{
		{
			name:   "empty labels",
			labels: nil,
			want:   nil,
		},
		{
			name:   "empty slice",
			labels: []string{},
			want:   nil,
		},
		{
			name:   "single label",
			labels: []string{"key=value"},
			want:   map[string]string{"key": "value"},
		},
		{
			name:   "multiple labels",
			labels: []string{"env=prod", "team=backend"},
			want:   map[string]string{"env": "prod", "team": "backend"},
		},
		{
			name:   "label with multiple equals signs",
			labels: []string{"key=value=with=equals"},
			want:   map[string]string{"key": "value=with=equals"},
		},
		{
			name:   "label without value",
			labels: []string{"key="},
			want:   map[string]string{"key": ""},
		},
		{
			name:   "invalid label without equals",
			labels: []string{"invalid"},
			want:   map[string]string{},
		},
		{
			name:   "mixed valid and invalid labels",
			labels: []string{"valid=yes", "invalid", "also=valid"},
			want:   map[string]string{"valid": "yes", "also": "valid"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := parseLabels(tt.labels)
			assert.Equal(t, tt.want, result)
		})
	}
}

package core

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewContextSnapshot(t *testing.T) {
	snapshot := NewContextSnapshot()

	assert.Equal(t, CurrentSnapshotVersion, snapshot.Version)
	assert.NotZero(t, snapshot.LastActivity)
	assert.NotNil(t, snapshot.Environment)
}

func TestContextSnapshot_ToJSON(t *testing.T) {
	snapshot := &ContextSnapshot{
		Version:          1,
		WorkingDirectory: "/workspace/project",
		ConversationID:   "conv_123",
		Environment: map[string]string{
			"FOO": "bar",
		},
		LastActivity: time.Now(),
	}

	data, err := snapshot.ToJSON()
	require.NoError(t, err)
	assert.NotEmpty(t, data)

	// Verify it's valid JSON
	var parsed map[string]interface{}
	err = json.Unmarshal(data, &parsed)
	require.NoError(t, err)
	assert.Equal(t, "/workspace/project", parsed["working_directory"])
}

func TestParseContextSnapshot(t *testing.T) {
	t.Run("valid snapshot", func(t *testing.T) {
		input := json.RawMessage(`{
			"version": 1,
			"working_directory": "/workspace",
			"conversation_id": "conv_abc",
			"environment": {"KEY": "value"},
			"last_activity": "2024-01-01T00:00:00Z"
		}`)

		snapshot, err := ParseContextSnapshot(input)
		require.NoError(t, err)
		require.NotNil(t, snapshot)
		assert.Equal(t, 1, snapshot.Version)
		assert.Equal(t, "/workspace", snapshot.WorkingDirectory)
		assert.Equal(t, "conv_abc", snapshot.ConversationID)
		assert.Equal(t, "value", snapshot.Environment["KEY"])
	})

	t.Run("empty data", func(t *testing.T) {
		snapshot, err := ParseContextSnapshot(nil)
		require.NoError(t, err)
		assert.Nil(t, snapshot)

		snapshot, err = ParseContextSnapshot(json.RawMessage{})
		require.NoError(t, err)
		assert.Nil(t, snapshot)
	})

	t.Run("invalid JSON", func(t *testing.T) {
		_, err := ParseContextSnapshot(json.RawMessage(`{invalid`))
		assert.Error(t, err)
	})
}

func TestContextSnapshot_IsCompatible(t *testing.T) {
	snapshot := &ContextSnapshot{Version: 1}

	assert.True(t, snapshot.IsCompatible("1.0.0"))
	assert.True(t, snapshot.IsCompatible("2.0.0"))
	assert.True(t, snapshot.IsCompatible(""))
}

func TestContextSnapshot_Merge(t *testing.T) {
	t.Run("merge non-empty fields", func(t *testing.T) {
		base := &ContextSnapshot{
			Version:          1,
			WorkingDirectory: "/old",
			Environment: map[string]string{
				"OLD": "value",
			},
		}

		other := &ContextSnapshot{
			WorkingDirectory: "/new",
			ConversationID:   "conv_new",
			Environment: map[string]string{
				"NEW": "value",
			},
		}

		base.Merge(other)

		assert.Equal(t, "/new", base.WorkingDirectory)
		assert.Equal(t, "conv_new", base.ConversationID)
		assert.Equal(t, "value", base.Environment["OLD"])
		assert.Equal(t, "value", base.Environment["NEW"])
	})

	t.Run("merge nil", func(t *testing.T) {
		base := NewContextSnapshot()
		base.WorkingDirectory = "/test"

		base.Merge(nil)
		assert.Equal(t, "/test", base.WorkingDirectory)
	})

	t.Run("merge pending permissions without duplicates", func(t *testing.T) {
		base := &ContextSnapshot{
			PendingPermissions: []string{"perm_1", "perm_2"},
		}

		other := &ContextSnapshot{
			PendingPermissions: []string{"perm_2", "perm_3"},
		}

		base.Merge(other)

		assert.Len(t, base.PendingPermissions, 3)
		assert.Contains(t, base.PendingPermissions, "perm_1")
		assert.Contains(t, base.PendingPermissions, "perm_2")
		assert.Contains(t, base.PendingPermissions, "perm_3")
	})

	t.Run("merge task context", func(t *testing.T) {
		base := &ContextSnapshot{}

		other := &ContextSnapshot{
			TaskContext: &TaskContextSnapshot{
				TaskID:   "task_123",
				RunID:    "run_456",
				Status:   "running",
				Progress: 50,
			},
		}

		base.Merge(other)

		require.NotNil(t, base.TaskContext)
		assert.Equal(t, "task_123", base.TaskContext.TaskID)
		assert.Equal(t, 50, base.TaskContext.Progress)
	})
}

func TestContextSnapshot_AgentState(t *testing.T) {
	agentState := json.RawMessage(`{"cursor_position":100,"files_open":["main.go"]}`)

	snapshot := &ContextSnapshot{
		Version:    1,
		AgentState: agentState,
	}

	data, err := snapshot.ToJSON()
	require.NoError(t, err)

	parsed, err := ParseContextSnapshot(data)
	require.NoError(t, err)

	// Compare as unmarshaled JSON to avoid formatting differences
	var expected, actual map[string]interface{}
	require.NoError(t, json.Unmarshal(agentState, &expected))
	require.NoError(t, json.Unmarshal(parsed.AgentState, &actual))
	assert.Equal(t, expected, actual)
}

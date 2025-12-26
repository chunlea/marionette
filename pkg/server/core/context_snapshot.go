package core

import (
	"encoding/json"
	"time"
)

// ContextSnapshot represents the saved state of a session for suspend/resume.
// This allows sessions to be resumed on different runners while preserving
// the agent's context and working state.
type ContextSnapshot struct {
	// WorkingDirectory is the agent's current working directory.
	WorkingDirectory string `json:"working_directory,omitempty"`

	// Environment contains environment variables to restore.
	Environment map[string]string `json:"environment,omitempty"`

	// ConversationID is the agent's conversation ID (for agents that support it).
	// This allows the agent to continue the conversation after resume.
	ConversationID string `json:"conversation_id,omitempty"`

	// AgentState contains agent-specific state as raw JSON.
	// This is opaque to the server and passed directly to the agent on resume.
	AgentState json.RawMessage `json:"agent_state,omitempty"`

	// LastActivity is when the session was last active.
	LastActivity time.Time `json:"last_activity"`

	// Version is the snapshot format version for compatibility checking.
	Version int `json:"version"`

	// AgentVersion is the agent version that created this snapshot.
	// Used to check compatibility on resume.
	AgentVersion string `json:"agent_version,omitempty"`

	// PendingPermissions contains IDs of pending permission requests.
	// These need to be delivered to the agent on resume.
	PendingPermissions []string `json:"pending_permissions,omitempty"`

	// TaskContext contains information about the current/last task.
	TaskContext *TaskContextSnapshot `json:"task_context,omitempty"`
}

// TaskContextSnapshot contains task-related state for resume.
type TaskContextSnapshot struct {
	// TaskID is the current task ID.
	TaskID string `json:"task_id,omitempty"`

	// RunID is the current task run ID.
	RunID string `json:"run_id,omitempty"`

	// Status is the task status when suspended.
	Status string `json:"status,omitempty"`

	// Progress is the estimated task progress (0-100).
	Progress int `json:"progress,omitempty"`
}

// CurrentSnapshotVersion is the current snapshot format version.
const CurrentSnapshotVersion = 1

// NewContextSnapshot creates a new context snapshot with default values.
func NewContextSnapshot() *ContextSnapshot {
	return &ContextSnapshot{
		Version:      CurrentSnapshotVersion,
		LastActivity: time.Now(),
		Environment:  make(map[string]string),
	}
}

// ToJSON serializes the snapshot to JSON.
func (s *ContextSnapshot) ToJSON() (json.RawMessage, error) {
	return json.Marshal(s)
}

// ParseContextSnapshot deserializes a snapshot from JSON.
func ParseContextSnapshot(data json.RawMessage) (*ContextSnapshot, error) {
	if len(data) == 0 {
		return nil, nil
	}

	var snapshot ContextSnapshot
	if err := json.Unmarshal(data, &snapshot); err != nil {
		return nil, err
	}
	return &snapshot, nil
}

// IsCompatible checks if the snapshot is compatible with the given agent version.
// For now, we only check the snapshot version, not the agent version.
func (s *ContextSnapshot) IsCompatible(agentVersion string) bool {
	// Version 1 is compatible with all agent versions.
	// Future versions may add version-specific compatibility checks.
	return s.Version <= CurrentSnapshotVersion
}

// Merge merges another snapshot into this one, overwriting non-empty fields.
func (s *ContextSnapshot) Merge(other *ContextSnapshot) {
	if other == nil {
		return
	}

	if other.WorkingDirectory != "" {
		s.WorkingDirectory = other.WorkingDirectory
	}
	if other.ConversationID != "" {
		s.ConversationID = other.ConversationID
	}
	if len(other.AgentState) > 0 {
		s.AgentState = other.AgentState
	}
	if other.AgentVersion != "" {
		s.AgentVersion = other.AgentVersion
	}
	if !other.LastActivity.IsZero() {
		s.LastActivity = other.LastActivity
	}
	if other.TaskContext != nil {
		s.TaskContext = other.TaskContext
	}

	// Merge environment.
	for k, v := range other.Environment {
		s.Environment[k] = v
	}

	// Merge pending permissions (avoid duplicates).
	if len(other.PendingPermissions) > 0 {
		seen := make(map[string]bool)
		for _, id := range s.PendingPermissions {
			seen[id] = true
		}
		for _, id := range other.PendingPermissions {
			if !seen[id] {
				s.PendingPermissions = append(s.PendingPermissions, id)
				seen[id] = true
			}
		}
	}
}

package webhook

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestMatcher_Matches(t *testing.T) {
	m := NewMatcher()

	tests := []struct {
		name      string
		eventType string
		patterns  []string
		want      bool
	}{
		// Exact match
		{
			name:      "exact match",
			eventType: "task.created",
			patterns:  []string{"task.created"},
			want:      true,
		},
		{
			name:      "exact match no match",
			eventType: "task.created",
			patterns:  []string{"task.completed"},
			want:      false,
		},
		// Wildcard match
		{
			name:      "wildcard match",
			eventType: "task.created",
			patterns:  []string{"task.*"},
			want:      true,
		},
		{
			name:      "wildcard match completed",
			eventType: "task.completed",
			patterns:  []string{"task.*"},
			want:      true,
		},
		{
			name:      "wildcard no match different category",
			eventType: "session.created",
			patterns:  []string{"task.*"},
			want:      false,
		},
		// Category match
		{
			name:      "category match",
			eventType: "task.created",
			patterns:  []string{"task"},
			want:      true,
		},
		{
			name:      "category no match",
			eventType: "session.created",
			patterns:  []string{"task"},
			want:      false,
		},
		// Star-only wildcard
		{
			name:      "star matches all",
			eventType: "task.created",
			patterns:  []string{"*"},
			want:      true,
		},
		{
			name:      "star matches session",
			eventType: "session.suspended",
			patterns:  []string{"*"},
			want:      true,
		},
		// Multiple patterns
		{
			name:      "multiple patterns first match",
			eventType: "task.created",
			patterns:  []string{"task.created", "session.*"},
			want:      true,
		},
		{
			name:      "multiple patterns second match",
			eventType: "session.resumed",
			patterns:  []string{"task.*", "session.*"},
			want:      true,
		},
		{
			name:      "multiple patterns no match",
			eventType: "runner.connected",
			patterns:  []string{"task.*", "session.*"},
			want:      false,
		},
		// Empty patterns
		{
			name:      "empty patterns",
			eventType: "task.created",
			patterns:  []string{},
			want:      false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := m.Matches(tt.eventType, tt.patterns)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestValidateEventPattern(t *testing.T) {
	tests := []struct {
		pattern string
		valid   bool
	}{
		// Valid patterns
		{"task.created", true},
		{"task.*", true},
		{"task", true},
		{"*", true},
		{"session.suspended", true},
		{"runner.connected", true},
		{"permission.approved", true},
		{"task_run.completed", true},
		{"my-event.custom", true},

		// Invalid patterns
		{"", false},
		{"task.*.foo", false},   // Wildcard not at end
		{".*", false},           // Wildcard without prefix
		{"task..created", true}, // Double dot is allowed by char validation
		{"task@created", false}, // Invalid character
	}

	for _, tt := range tests {
		t.Run(tt.pattern, func(t *testing.T) {
			got := ValidateEventPattern(tt.pattern)
			assert.Equal(t, tt.valid, got, "pattern: %s", tt.pattern)
		})
	}
}

func TestParseEventCategory(t *testing.T) {
	tests := []struct {
		eventType string
		want      string
	}{
		{"task.created", "task"},
		{"session.suspended", "session"},
		{"runner.connected", "runner"},
		{"permission.approved", "permission"},
		{"task", "task"},
		{"", ""},
	}

	for _, tt := range tests {
		t.Run(tt.eventType, func(t *testing.T) {
			got := ParseEventCategory(tt.eventType)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestParseEventAction(t *testing.T) {
	tests := []struct {
		eventType string
		want      string
	}{
		{"task.created", "created"},
		{"session.suspended", "suspended"},
		{"runner.connected", "connected"},
		{"permission.approved", "approved"},
		{"task", ""},
		{"", ""},
	}

	for _, tt := range tests {
		t.Run(tt.eventType, func(t *testing.T) {
			got := ParseEventAction(tt.eventType)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestMatcher_MatchesAny(t *testing.T) {
	m := NewMatcher()

	webhookEvents := [][]string{
		{"task.*"},
		{"session.created", "session.terminated"},
		{"*"},
	}

	tests := []struct {
		eventType string
		want      []int
	}{
		{"task.created", []int{0, 2}},
		{"session.created", []int{1, 2}},
		{"session.terminated", []int{1, 2}},
		{"runner.connected", []int{2}},
	}

	for _, tt := range tests {
		t.Run(tt.eventType, func(t *testing.T) {
			got := m.MatchesAny(tt.eventType, webhookEvents)
			assert.Equal(t, tt.want, got)
		})
	}
}

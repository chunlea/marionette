package webhook

import (
	"strings"
)

// Matcher checks if an event type matches a subscription pattern.
type Matcher struct{}

// NewMatcher creates a new event matcher.
func NewMatcher() *Matcher {
	return &Matcher{}
}

// Matches checks if the event type matches any of the given patterns.
// Patterns support:
//   - Exact match: "task.created" matches "task.created"
//   - Wildcard suffix: "task.*" matches "task.created", "task.completed", etc.
//   - Category match: "task" matches all task events
func (m *Matcher) Matches(eventType string, patterns []string) bool {
	for _, pattern := range patterns {
		if m.matchPattern(eventType, pattern) {
			return true
		}
	}
	return false
}

// matchPattern checks if a single pattern matches the event type.
func (m *Matcher) matchPattern(eventType, pattern string) bool {
	// Exact match
	if eventType == pattern {
		return true
	}

	// Wildcard match: "task.*" matches "task.created"
	if prefix, found := strings.CutSuffix(pattern, ".*"); found {
		if strings.HasPrefix(eventType, prefix+".") {
			return true
		}
	}

	// Category match: "task" matches "task.created"
	if !strings.Contains(pattern, ".") && !strings.Contains(pattern, "*") {
		if strings.HasPrefix(eventType, pattern+".") {
			return true
		}
	}

	// Star-only wildcard: "*" matches everything
	if pattern == "*" {
		return true
	}

	return false
}

// MatchesAny checks if the event type matches any webhook's subscription.
// This is a convenience method for filtering webhooks.
func (m *Matcher) MatchesAny(eventType string, webhookEvents [][]string) []int {
	var matches []int
	for i, events := range webhookEvents {
		if m.Matches(eventType, events) {
			matches = append(matches, i)
		}
	}
	return matches
}

// ParseEventCategory extracts the category from an event type.
// For example, "task.created" returns "task".
func ParseEventCategory(eventType string) string {
	parts := strings.SplitN(eventType, ".", 2)
	if len(parts) > 0 {
		return parts[0]
	}
	return eventType
}

// ParseEventAction extracts the action from an event type.
// For example, "task.created" returns "created".
func ParseEventAction(eventType string) string {
	parts := strings.SplitN(eventType, ".", 2)
	if len(parts) > 1 {
		return parts[1]
	}
	return ""
}

// ValidateEventPattern checks if a pattern is valid.
// Valid patterns are:
//   - Exact event names: "task.created"
//   - Wildcard patterns: "task.*"
//   - Category patterns: "task"
//   - All events: "*"
func ValidateEventPattern(pattern string) bool {
	if pattern == "" {
		return false
	}

	// "*" is valid (matches all)
	if pattern == "*" {
		return true
	}

	// Check for valid characters
	for _, c := range pattern {
		if !isValidPatternChar(c) {
			return false
		}
	}

	// Wildcard must be at the end
	if strings.Contains(pattern, "*") && !strings.HasSuffix(pattern, "*") {
		return false
	}

	// ".* " pattern requires a prefix
	if strings.HasSuffix(pattern, ".*") && len(pattern) < 3 {
		return false
	}

	return true
}

func isValidPatternChar(c rune) bool {
	return (c >= 'a' && c <= 'z') ||
		(c >= 'A' && c <= 'Z') ||
		(c >= '0' && c <= '9') ||
		c == '.' || c == '*' || c == '_' || c == '-'
}

// KnownEventTypes returns all known webhook event types.
func KnownEventTypes() []string {
	return []string{
		EventSessionCreated,
		EventSessionSuspended,
		EventSessionResumed,
		EventSessionTerminated,
		EventTaskCreated,
		EventTaskStarted,
		EventTaskCompleted,
		EventTaskFailed,
		EventTaskCanceled,
		EventRunnerConnected,
		EventRunnerDisconnected,
		EventRunnerAssigned,
		EventRunnerReleased,
		EventPermissionRequested,
		EventPermissionApproved,
		EventPermissionDenied,
		EventPermissionCanceled,
	}
}

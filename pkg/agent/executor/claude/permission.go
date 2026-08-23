package claude

// IsPermissionRequired reports whether a tool invocation should be gated.
//
// TODO(lane-a1 task 4): this is the legacy hardcoded list. It is kept only so
// the executor keeps building while the message model is rewritten; it is
// replaced by a real policy (current tool names plus mcp__* handling) in the
// permission-gating commit.
func IsPermissionRequired(toolName string) bool {
	switch toolName {
	case "Bash", "Write", "Edit", "NotebookEdit", "computer":
		return true
	default:
		return false
	}
}

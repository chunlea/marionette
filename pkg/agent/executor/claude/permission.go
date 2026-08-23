package claude

import (
	"strings"

	"github.com/chunlea/marionette/pkg/agent/executor"
)

// Permission policy for Claude Code tool calls.
//
// The policy is deny-by-default on the unknown: anything not explicitly known
// to be read-only is gated. The previous implementation was an allow-list of
// five tool names, so every tool added since (and every `mcp__*` tool from a
// third-party server) ran ungated. Tool names below are the built-ins reported
// by CLI 2.1.241's `system`/`init` line.

// readOnlyTools inspect state without changing anything outside the agent's own
// bookkeeping, so they are not worth interrupting a human for.
var readOnlyTools = map[string]bool{
	"Read":                   true,
	"Glob":                   true,
	"Grep":                   true,
	"NotebookRead":           true,
	"TodoWrite":              true,
	"LSP":                    true,
	"ToolSearch":             true,
	"Skill":                  true,
	"CronList":               true,
	"ListAgents":             true,
	"TaskOutput":             true,
	"ReportFindings":         true,
	"ListMcpResourcesTool":   true,
	"ReadMcpResourceTool":    true,
	"ReadMcpResourceDirTool": true,
}

// toolRisk assigns a risk level to the gated built-ins. Tools absent from this
// map are still gated; they just fall back to the default risk level.
var toolRisk = map[string]executor.RiskLevel{
	// Arbitrary command execution.
	"Bash": executor.RiskCritical,

	// Filesystem and repository mutation.
	"Write":         executor.RiskHigh,
	"Edit":          executor.RiskHigh,
	"NotebookEdit":  executor.RiskHigh,
	"EnterWorktree": executor.RiskHigh,
	"ExitWorktree":  executor.RiskHigh,

	// Spawning more agents or work.
	"Task":     executor.RiskHigh,
	"Workflow": executor.RiskHigh,
	"TaskStop": executor.RiskMedium,
	"Monitor":  executor.RiskMedium,

	// Scheduling and outbound side effects that outlive the run.
	"CronCreate":       executor.RiskHigh,
	"CronDelete":       executor.RiskHigh,
	"ScheduleWakeup":   executor.RiskMedium,
	"RemoteTrigger":    executor.RiskHigh,
	"SendMessage":      executor.RiskHigh,
	"PushNotification": executor.RiskHigh,
	"DesignSync":       executor.RiskMedium,

	// Network egress.
	"WebFetch":  executor.RiskMedium,
	"WebSearch": executor.RiskMedium,
}

// mcpToolPrefix marks a tool provided by an MCP server. What such a tool does
// is unknowable from here, so it is always gated.
const mcpToolPrefix = "mcp__"

// defaultToolRisk applies to gated tools with no explicit entry, including
// every mcp__* tool and any built-in added by a future CLI release.
const defaultToolRisk = executor.RiskMedium

// RequiresPermission reports whether a tool call must be approved before it
// runs. Unknown tools are gated: a CLI upgrade that introduces a tool must not
// silently open a hole.
func RequiresPermission(toolName string) bool {
	if toolName == "" {
		return true
	}
	if strings.HasPrefix(toolName, mcpToolPrefix) {
		return true
	}
	return !readOnlyTools[toolName]
}

// RiskLevelFor returns the risk level reported to the operator for a tool.
func RiskLevelFor(toolName string) executor.RiskLevel {
	if risk, ok := toolRisk[toolName]; ok {
		return risk
	}
	if readOnlyTools[toolName] {
		return executor.RiskLow
	}
	return defaultToolRisk
}

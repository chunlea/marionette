// Package permission provides permission detection and handling for Claude Code output.
package permission

import (
	"regexp"
	"strings"

	"github.com/chunlea/marionette/pkg/agent/executor"
	"github.com/chunlea/marionette/pkg/id"
)

// PermissionPatterns defines the regex patterns for detecting permission requests.
// Claude Code outputs permission requests in a specific format.
var (
	// Pattern for tool permission request header
	// Example: "Claude wants to use the Bash tool to run:"
	toolRequestPattern = regexp.MustCompile(`(?i)Claude\s+wants\s+to\s+(?:use\s+the\s+)?(\w+)(?:\s+tool)?\s+to\s+(?:run|execute|perform|do):\s*`)

	// Pattern for the confirmation prompt
	// Example: "Allow this action? (y)es / (n)o"
	confirmPattern = regexp.MustCompile(`(?i)[Aa]llow\s+this\s+(?:action|command|operation)\s*\?.*\([yY]\)`)

	// Alternative pattern for direct permission blocks
	// Claude Code sometimes outputs permission requests in a box format
	// Note: Both top and bottom can use the same ─ character
	boxBorderPattern = regexp.MustCompile(`^[─╭┌╰└]+$`)
	boxLinePattern   = regexp.MustCompile(`^[│┃|]\s*(.+?)\s*[│┃|]$`)

	// Risk keywords for determining risk level
	highRiskKeywords = []string{
		"rm -rf", "rm -r", "delete", "remove",
		"sudo", "chmod", "chown",
		"ssh", "scp",
		"password", "secret", "key", "token",
		"eval", "exec",
		"DROP", "TRUNCATE", "DELETE FROM",
	}

	mediumRiskKeywords = []string{
		"curl", "wget", "http",
		"mv", "cp", "mkdir", "rmdir",
		"git push", "git commit",
		"npm install", "pip install",
		"make", "build",
	}
)

// Parser detects permission requests from Claude Code output.
type Parser struct {
	buffer        strings.Builder
	inBox         bool
	boxLines      []string
	pendingTool   string
	pendingAction string
}

// NewParser creates a new permission parser.
func NewParser() *Parser {
	return &Parser{}
}

// ParseLine processes a line of output and returns a PermissionRequest if detected.
// Returns nil if no permission request is detected yet.
func (p *Parser) ParseLine(line string) *executor.PermissionRequest {
	trimmed := strings.TrimSpace(line)

	// Check for box-style permission request
	if boxBorderPattern.MatchString(trimmed) {
		if p.inBox {
			// End of box, parse contents
			p.inBox = false
			req := p.parseBoxContents()
			p.boxLines = nil
			return req
		}
		// Start of box
		p.inBox = true
		p.boxLines = nil
		return nil
	}

	if p.inBox {
		// Collect box lines
		if matches := boxLinePattern.FindStringSubmatch(trimmed); len(matches) > 1 {
			p.boxLines = append(p.boxLines, matches[1])
		} else if trimmed != "" {
			p.boxLines = append(p.boxLines, trimmed)
		}
		return nil
	}

	// Check for inline tool request pattern
	if matches := toolRequestPattern.FindStringSubmatch(line); len(matches) > 1 {
		p.pendingTool = normalizeToolName(matches[1])
		p.pendingAction = "" // Reset action when new tool is detected
		return nil
	}

	// Check for confirmation prompt BEFORE setting action
	// (this confirms a pending request when tool and action are set)
	if p.pendingTool != "" && p.pendingAction != "" && confirmPattern.MatchString(line) {
		req := &executor.PermissionRequest{
			ID:        id.New("perm"),
			Tool:      p.pendingTool,
			Action:    p.pendingAction,
			RiskLevel: determineRiskLevel(p.pendingTool, p.pendingAction),
		}
		p.pendingTool = ""
		p.pendingAction = ""
		return req
	}

	// If we have a pending tool but no action yet, the next non-empty line is the action
	if p.pendingTool != "" && p.pendingAction == "" && trimmed != "" {
		// Skip if it looks like the confirm pattern
		if !confirmPattern.MatchString(line) {
			p.pendingAction = trimmed
		}
		return nil
	}

	return nil
}

// parseBoxContents extracts permission request from collected box lines.
func (p *Parser) parseBoxContents() *executor.PermissionRequest {
	if len(p.boxLines) == 0 {
		return nil
	}

	var tool, action, context string

	for i, line := range p.boxLines {
		// Check for tool pattern
		if matches := toolRequestPattern.FindStringSubmatch(line); len(matches) > 1 {
			tool = normalizeToolName(matches[1])
			// Action might be on the same line or next line
			remaining := toolRequestPattern.ReplaceAllString(line, "")
			if strings.TrimSpace(remaining) != "" {
				action = strings.TrimSpace(remaining)
			} else if i+1 < len(p.boxLines) {
				action = strings.TrimSpace(p.boxLines[i+1])
			}
			continue
		}

		// Check for confirmation (skip)
		if confirmPattern.MatchString(line) {
			continue
		}

		// Collect context from other lines
		if tool != "" && action != "" && strings.TrimSpace(line) != "" {
			if context != "" {
				context += " "
			}
			context += strings.TrimSpace(line)
		}
	}

	if tool == "" || action == "" {
		return nil
	}

	return &executor.PermissionRequest{
		ID:        id.New("perm"),
		Tool:      tool,
		Action:    action,
		Context:   context,
		RiskLevel: determineRiskLevel(tool, action),
	}
}

// Reset clears the parser state.
func (p *Parser) Reset() {
	p.buffer.Reset()
	p.inBox = false
	p.boxLines = nil
	p.pendingTool = ""
	p.pendingAction = ""
}

// normalizeToolName converts tool names to standard format.
func normalizeToolName(tool string) string {
	tool = strings.ToLower(strings.TrimSpace(tool))

	switch tool {
	case "bash", "shell", "terminal", "command":
		return "bash"
	case "edit", "write", "file", "editor":
		return "edit"
	case "read", "cat", "view":
		return "read"
	case "browser", "web", "fetch":
		return "browser"
	case "mcp", "tool":
		return "mcp"
	default:
		return tool
	}
}

// determineRiskLevel determines the risk level based on tool and action.
func determineRiskLevel(tool, action string) executor.RiskLevel {
	actionLower := strings.ToLower(action)

	// Check high risk patterns
	for _, keyword := range highRiskKeywords {
		if strings.Contains(actionLower, strings.ToLower(keyword)) {
			return executor.RiskHigh
		}
	}

	// Check medium risk patterns
	for _, keyword := range mediumRiskKeywords {
		if strings.Contains(actionLower, strings.ToLower(keyword)) {
			return executor.RiskMedium
		}
	}

	// Tool-based default risk levels
	switch tool {
	case "bash":
		return executor.RiskMedium
	case "edit", "write":
		return executor.RiskMedium
	case "browser":
		return executor.RiskLow
	case "read":
		return executor.RiskLow
	default:
		return executor.RiskMedium
	}
}

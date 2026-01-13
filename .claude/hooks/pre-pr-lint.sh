#!/bin/bash
# Run lint check before PR creation
# Hook type: PreToolUse (Bash)

set -euo pipefail

# Read JSON input from stdin
read -r INPUT
COMMAND=$(echo "$INPUT" | jq -r '.tool_input.command // empty')

# Only process gh pr create commands
if ! echo "$COMMAND" | grep -qE "^gh pr create"; then
  echo '{"decision": "allow"}'
  exit 0
fi

# Run lint check
echo "Running lint check before PR creation..." >&2

LINT_OUTPUT=$(make lint 2>&1) || LINT_FAILED=1

if [[ "${LINT_FAILED:-0}" == "1" ]]; then
  # Extract first few errors for the message
  ERRORS=$(echo "$LINT_OUTPUT" | grep -E "^[^:]+:[0-9]+:" | head -5 | tr '\n' ' ')

  # Avoid heredoc for sandbox compatibility - use printf with JSON escaping
  printf '{"decision": "block", "reason": "Lint check failed. Please fix issues before creating PR. Run make lint to see all issues."}\n'
  exit 0
fi

# Lint passed
echo '{"decision": "allow"}'

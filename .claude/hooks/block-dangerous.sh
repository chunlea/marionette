#!/bin/bash
# Block dangerous operations before execution
# Hook type: PreToolUse (Bash)

# Don't use set -e, we want to continue even if commands fail
set -uo pipefail

# Read JSON input from stdin (handle empty input gracefully)
INPUT=$(cat) || INPUT=""
if [[ -z "$INPUT" ]]; then
  echo '{"decision": "allow"}'
  exit 0
fi

COMMAND=$(echo "$INPUT" | jq -r '.tool_input.command // empty' 2>/dev/null) || COMMAND=""

if [[ -z "$COMMAND" ]]; then
  echo '{"decision": "allow"}'
  exit 0
fi

# Dangerous patterns that require manual confirmation
DANGEROUS_PATTERNS=(
  "git push.*-f"
  "git push.*--force"
  "git reset --hard"
  "rm -rf /"
  "rm -rf \*"
  "rm -rf \."
  "DROP TABLE"
  "DROP DATABASE"
  "TRUNCATE"
  "> /dev/sd"
  "mkfs\."
  "dd if=.*/dev/"
)

for pattern in "${DANGEROUS_PATTERNS[@]}"; do
  if echo "$COMMAND" | grep -qiE "$pattern"; then
    echo '{"decision": "block", "reason": "Dangerous operation blocked: '"$pattern"'. Execute manually if needed."}'
    exit 0
  fi
done

# Allow the command
echo '{"decision": "allow"}'

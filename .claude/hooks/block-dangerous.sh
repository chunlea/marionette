#!/bin/bash
# Block dangerous operations before execution
# Hook type: PreToolUse (Bash)

set -euo pipefail

# Read JSON input from stdin
read -r INPUT
COMMAND=$(echo "$INPUT" | jq -r '.tool_input.command // empty')

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

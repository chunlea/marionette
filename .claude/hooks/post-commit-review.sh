#!/bin/bash
# Check for TODOs after successful git commit
# Hook type: PostToolUse (Bash)

set -euo pipefail

# Read JSON input from stdin
read -r INPUT
COMMAND=$(echo "$INPUT" | jq -r '.tool_input.command // empty')
STDOUT=$(echo "$INPUT" | jq -r '.tool_result.stdout // empty')

# Only process git commit commands
if ! echo "$COMMAND" | grep -qE "^git commit"; then
  exit 0
fi

# Check if commit was successful (output contains commit hash pattern)
if ! echo "$STDOUT" | grep -qE "^\[.+\s+[a-f0-9]+\]"; then
  exit 0
fi

# Check for TODO/FIXME in changed files
CHANGED_FILES=$(git diff HEAD~1 --name-only 2>/dev/null || echo "")

if [[ -z "$CHANGED_FILES" ]]; then
  exit 0
fi

# Find files with TODO/FIXME
TODOS=""
for file in $CHANGED_FILES; do
  if [[ -f "$file" ]] && grep -qE "TODO|FIXME" "$file" 2>/dev/null; then
    TODOS="$TODOS$file "
  fi
done

if [[ -n "$TODOS" ]]; then
  # Output reminder about TODOs (avoid heredoc for sandbox compatibility)
  echo '{"suppressOutput": false, "hookSpecificOutput": {"reminder": "Files with TODO/FIXME detected in this commit. Consider addressing them."}}'
else
  # Silent success
  echo '{"suppressOutput": true}'
fi

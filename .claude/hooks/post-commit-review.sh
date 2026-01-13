#!/bin/bash
# Check for TODOs after successful git commit
# Hook type: PostToolUse (Bash)

# Don't use set -e, we want to continue even if commands fail
set -uo pipefail

# Read JSON input from stdin (handle empty input gracefully)
INPUT=$(cat) || INPUT=""
if [[ -z "$INPUT" ]]; then
  exit 0
fi

COMMAND=$(echo "$INPUT" | jq -r '.tool_input.command // empty' 2>/dev/null) || COMMAND=""
STDOUT=$(echo "$INPUT" | jq -r '.tool_result.stdout // empty' 2>/dev/null) || STDOUT=""

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

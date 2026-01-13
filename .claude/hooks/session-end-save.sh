#!/bin/bash
# Auto-save task state when session ends
# Hook type: SessionEnd

# Don't use set -e, we want to continue even if commands fail
set -uo pipefail

REPO_ROOT=$(git rev-parse --show-toplevel 2>/dev/null || echo "")

if [[ -z "$REPO_ROOT" ]]; then
  exit 0
fi

TASK_FILE="$REPO_ROOT/.claude/current-task.md"

# Skip if no task file
if [[ ! -f "$TASK_FILE" ]]; then
  exit 0
fi

# Get current state
BRANCH=$(git branch --show-current 2>/dev/null || echo "unknown")
TIMESTAMP=$(date '+%Y-%m-%d %H:%M')

# Check for uncommitted changes
UNCOMMITTED=""
if ! git diff --quiet 2>/dev/null || ! git diff --cached --quiet 2>/dev/null; then
  UNCOMMITTED=" (uncommitted changes)"
fi

# Get last commit message if any
LAST_COMMIT=$(git log -1 --pretty=format:"%s" 2>/dev/null || echo "")

# Update history section in task file
if grep -q "^## History" "$TASK_FILE"; then
  # Add entry to history
  HISTORY_ENTRY="- $TIMESTAMP: Session ended on branch $BRANCH$UNCOMMITTED"
  if [[ -n "$LAST_COMMIT" ]]; then
    HISTORY_ENTRY="$HISTORY_ENTRY (last: $LAST_COMMIT)"
  fi

  # Use temp file for safe editing (use project .claude/tmp for sandbox compatibility)
  TEMP_DIR="$CLAUDE_PROJECT_DIR/.claude/tmp"
  mkdir -p "$TEMP_DIR"
  TEMP_FILE="$TEMP_DIR/task-$$"
  awk -v entry="$HISTORY_ENTRY" '
    /^## History/ { print; getline; print entry; print; next }
    { print }
  ' "$TASK_FILE" > "$TEMP_FILE"
  mv "$TEMP_FILE" "$TASK_FILE"
  rm -f "$TEMP_FILE" 2>/dev/null || true
fi

# SessionEnd hook doesn't need output - just exit successfully
# State is saved to .claude/current-task.md History section
exit 0

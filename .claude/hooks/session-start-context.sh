#!/bin/bash
# Display context at session start
# Hook type: SessionStart

set -euo pipefail

# Get current directory info
CURRENT_DIR=$(pwd)
REPO_ROOT=$(git rev-parse --show-toplevel 2>/dev/null || echo "")

if [[ -z "$REPO_ROOT" ]]; then
  exit 0
fi

# Check if in a worktree
WORKTREE_INFO=""
GIT_DIR=$(git rev-parse --git-dir 2>/dev/null || echo "")

if [[ "$GIT_DIR" == *".git/worktrees/"* ]]; then
  WORKTREE_NAME=$(basename "$(dirname "$GIT_DIR")" 2>/dev/null || echo "unknown")
  WORKTREE_INFO="Worktree: $WORKTREE_NAME"
fi

# Get current branch
BRANCH=$(git branch --show-current 2>/dev/null || echo "detached")

# Check for uncommitted changes
CHANGES=""
if ! git diff --quiet 2>/dev/null || ! git diff --cached --quiet 2>/dev/null; then
  CHANGES="(uncommitted changes)"
fi

# Check for current task (create from template if missing)
TASK_FILE="$REPO_ROOT/.claude/current-task.md"
TASK_TEMPLATE="$REPO_ROOT/.claude/current-task.template.md"
if [[ ! -f "$TASK_FILE" ]] && [[ -f "$TASK_TEMPLATE" ]]; then
  cp "$TASK_TEMPLATE" "$TASK_FILE"
fi

CURRENT_TASK=""
if [[ -f "$TASK_FILE" ]]; then
  TASK_STATUS=$(grep -E "^## Status" -A 1 "$TASK_FILE" 2>/dev/null | tail -1 | tr -d '\n' || echo "")
  TASK_DESC=$(grep -E "^## Description" -A 1 "$TASK_FILE" 2>/dev/null | tail -1 | tr -d '\n' || echo "")
  if [[ -n "$TASK_DESC" ]] && [[ "$TASK_DESC" != "No active task"* ]]; then
    CURRENT_TASK="Task: $TASK_DESC"
  fi
fi

# Build context message
CONTEXT=""
if [[ -n "$WORKTREE_INFO" ]]; then
  CONTEXT="$WORKTREE_INFO | "
fi
CONTEXT="${CONTEXT}Branch: $BRANCH $CHANGES"
if [[ -n "$CURRENT_TASK" ]]; then
  CONTEXT="$CONTEXT | $CURRENT_TASK"
fi

# Output context via systemMessage (visible to Claude)
# Avoid heredoc for sandbox compatibility - escape special chars for JSON
CONTEXT_ESCAPED=$(echo "$CONTEXT" | sed 's/"/\\"/g' | tr '\n' ' ')
printf '{"systemMessage": "Session context: %s"}\n' "$CONTEXT_ESCAPED"

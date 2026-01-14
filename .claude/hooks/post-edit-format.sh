#!/bin/bash
# Auto-format files after edit/write operations
# Hook type: PostToolUse (Edit|Write)

# Don't use set -e, we want to continue even if commands fail
set -uo pipefail

# Read JSON input from stdin (handle empty input gracefully)
INPUT=$(cat) || INPUT=""
if [[ -z "$INPUT" ]]; then
  exit 0
fi

FILE_PATH=$(echo "$INPUT" | jq -r '.tool_input.file_path // empty' 2>/dev/null) || FILE_PATH=""

# Skip if no file path or file doesn't exist
if [[ -z "$FILE_PATH" ]] || [[ ! -f "$FILE_PATH" ]]; then
  exit 0
fi

# Format based on file extension
case "$FILE_PATH" in
  *.go)
    # Format Go files (only if tools are available)
    if command -v gofmt &> /dev/null; then
      gofmt -w "$FILE_PATH" 2>/dev/null || true
    fi
    if command -v goimports &> /dev/null; then
      goimports -w "$FILE_PATH" 2>/dev/null || true
    fi
    ;;
  *.proto)
    # Format protobuf files
    if command -v buf &> /dev/null; then
      buf format -w "$FILE_PATH" 2>/dev/null || true
    fi
    ;;
  *.ts|*.tsx|*.js|*.jsx|*.json)
    # Format TypeScript/JavaScript files
    if command -v npx &> /dev/null; then
      npx prettier --write "$FILE_PATH" 2>/dev/null || true
    fi
    ;;
  *.yaml|*.yml)
    # Format YAML files
    if command -v npx &> /dev/null; then
      npx prettier --write "$FILE_PATH" 2>/dev/null || true
    fi
    ;;
esac

# Silent success - don't output anything to avoid noise
exit 0

#!/bin/bash
# Auto-format files after edit/write operations
# Hook type: PostToolUse (Edit|Write)

set -euo pipefail

# Read JSON input from stdin
read -r INPUT
FILE_PATH=$(echo "$INPUT" | jq -r '.tool_input.file_path // empty')

# Skip if no file path or file doesn't exist
if [[ -z "$FILE_PATH" ]] || [[ ! -f "$FILE_PATH" ]]; then
  exit 0
fi

# Format based on file extension
case "$FILE_PATH" in
  *.go)
    # Format Go files
    gofmt -w "$FILE_PATH" 2>/dev/null || true
    goimports -w "$FILE_PATH" 2>/dev/null || true
    ;;
  *.proto)
    # Format protobuf files
    buf format -w "$FILE_PATH" 2>/dev/null || true
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

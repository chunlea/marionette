#!/bin/bash
# One-click script to test desktop streaming
set -e

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
cd "$SCRIPT_DIR"

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

log_info() { echo -e "${BLUE}[INFO]${NC} $1"; }
log_success() { echo -e "${GREEN}[OK]${NC} $1"; }
log_warn() { echo -e "${YELLOW}[WARN]${NC} $1"; }
log_error() { echo -e "${RED}[ERROR]${NC} $1"; }

# Cleanup function
cleanup() {
    log_info "Cleaning up..."
    docker compose -f docker-compose.yml -f docker-compose.desktop.yml --profile desktop down 2>/dev/null || true
}

# Parse arguments
ACTION="${1:-up}"

case "$ACTION" in
    up|start)
        log_info "Starting desktop streaming test environment..."
        ;;
    down|stop)
        cleanup
        log_success "Environment stopped"
        exit 0
        ;;
    logs)
        docker compose -f docker-compose.yml -f docker-compose.desktop.yml logs -f agent-desktop
        exit 0
        ;;
    *)
        echo "Usage: $0 [up|down|logs]"
        echo "  up    - Start the test environment (default)"
        echo "  down  - Stop the test environment"
        echo "  logs  - Follow agent-desktop logs"
        exit 1
        ;;
esac

# Step 1: Build images
log_info "Building Docker images..."
docker compose -f docker-compose.yml build --quiet
docker build -f Dockerfile.agent-desktop -t marionette-agent-desktop:latest ../.. --quiet
log_success "Images built"

# Step 2: Start postgres and server
log_info "Starting postgres and server..."
docker compose up -d postgres server
log_success "Started postgres and server"

# Step 3: Wait for server to be healthy
log_info "Waiting for server to be ready..."
MAX_RETRIES=30
RETRY_COUNT=0
until curl -s http://localhost:8081/health > /dev/null 2>&1; do
    RETRY_COUNT=$((RETRY_COUNT + 1))
    if [ $RETRY_COUNT -ge $MAX_RETRIES ]; then
        log_error "Server failed to start after ${MAX_RETRIES} attempts"
        docker compose logs server
        exit 1
    fi
    echo -n "."
    sleep 1
done
echo ""
log_success "Server is ready"

# Step 4: Create runner token using Go script
log_info "Creating runner token..."
cd "$SCRIPT_DIR/../.."

# Run the token generator
OUTPUT=$(MARIONETTE_DATABASE_URL="postgres://marionette:marionette@localhost:5432/marionette?sslmode=disable" \
    POOL_NAME="desktop-pool" \
    go run .claude/tmp/create_runner_token.go 2>&1)

if [ $? -ne 0 ]; then
    log_error "Failed to create runner token"
    echo "$OUTPUT"
    exit 1
fi

TOKEN=$(echo "$OUTPUT" | grep "Runner Token:" | awk '{print $3}')
if [ -z "$TOKEN" ]; then
    log_error "Failed to parse runner token from output"
    echo "$OUTPUT"
    exit 1
fi
log_success "Runner token created: ${TOKEN:0:20}..."

cd "$SCRIPT_DIR"

# Step 5: Start desktop agent
log_info "Starting desktop agent..."
export MARIONETTE_RUNNER_TOKEN="$TOKEN"
docker compose -f docker-compose.yml -f docker-compose.desktop.yml --profile desktop up -d agent-desktop
log_success "Desktop agent started"

# Step 6: Wait for agent to connect
log_info "Waiting for agent to connect..."
sleep 3

# Check agent logs
if docker compose -f docker-compose.yml -f docker-compose.desktop.yml logs agent-desktop 2>&1 | grep -q "connected\|registered\|ready"; then
    log_success "Agent connected to server"
else
    log_warn "Agent may still be connecting, check logs with: $0 logs"
fi

# Print summary
echo ""
echo "=========================================="
log_success "Desktop streaming test environment is ready!"
echo "=========================================="
echo ""
echo "Services:"
echo "  - Server API:    http://localhost:8080"
echo "  - Admin API:     http://localhost:8081"
echo "  - gRPC:          localhost:9090"
echo ""
echo "Commands:"
echo "  - View logs:     $0 logs"
echo "  - Stop:          $0 down"
echo ""
echo "Next steps:"
echo "  1. Create a session with the desktop agent"
echo "  2. Start a desktop stream via API"
echo "  3. Connect with WebRTC viewer"
echo ""

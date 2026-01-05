#!/bin/bash
# Marionette Docker Compose Startup Script
set -e

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

echo -e "${GREEN}Starting Marionette...${NC}"

# Change to script directory
cd "$(dirname "$0")"

# Start services
echo "Starting services..."
docker compose up -d

# Wait for PostgreSQL to be ready
echo "Waiting for PostgreSQL..."
until docker compose exec -T postgres pg_isready -U marionette > /dev/null 2>&1; do
    sleep 1
done
echo -e "${GREEN}PostgreSQL is ready${NC}"

# Wait for server to be ready
echo "Waiting for server..."
until curl -s http://localhost:8080/health > /dev/null 2>&1; do
    sleep 1
done
echo -e "${GREEN}Server is ready${NC}"

echo ""
echo -e "${GREEN}Marionette is running!${NC}"
echo ""
echo "Services:"
echo "  Public API:  http://localhost:8080"
echo "  Admin API:   http://localhost:8081"
echo "  gRPC:        localhost:9090"
echo ""
echo "Quick start:"
echo "  # Create an API key"
echo "  curl -X POST http://localhost:8081/api/keys \\"
echo "    -H 'Content-Type: application/json' \\"
echo "    -d '{\"name\": \"dev-key\", \"scopes\": [\"*\"]}'"
echo ""
echo "  # View logs"
echo "  docker compose logs -f"
echo ""
echo "  # Stop services"
echo "  docker compose down"

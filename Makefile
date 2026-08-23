.PHONY: deps build test lint proto migrate dev clean help \
	schema schema-check generate test-store \
	certs certs-clean certs-verify \
	web-install web-dev web-build web-lint web-clean

# Go parameters
GOCMD=go
GOBUILD=$(GOCMD) build
GOTEST=$(GOCMD) test
GOMOD=$(GOCMD) mod
GOVET=$(GOCMD) vet

# Binary names
SERVER_BINARY=bin/server
AGENT_BINARY=bin/agent
MCTL_BINARY=bin/mctl

# Build flags
LDFLAGS=-ldflags "-s -w"

# Default target
.DEFAULT_GOAL := help

## help: Show this help message
help:
	@echo "Marionette - Remote Agent Orchestration Platform"
	@echo ""
	@echo "Usage: make [target]"
	@echo ""
	@echo "Targets:"
	@sed -n 's/^## //p' $(MAKEFILE_LIST) | column -t -s ':'

# Toolchain versions. Keep in sync with .github/workflows/ci.yml so a local
# build and a CI build generate the same code. PROTOC_GEN_GO_VERSION tracks
# google.golang.org/protobuf in go.mod; bump them together.
BUF_VERSION=v1.72.0
PROTOC_GEN_GO_VERSION=v1.36.11
PROTOC_GEN_GO_GRPC_VERSION=v1.6.2
GOLANGCI_LINT_VERSION=v2.13.1

## deps: Install dependencies and development tools
deps:
	$(GOMOD) download
	$(GOMOD) tidy
	@echo "Installing development tools..."
	go install github.com/air-verse/air@latest
	go install github.com/bufbuild/buf/cmd/buf@$(BUF_VERSION)
	go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@$(GOLANGCI_LINT_VERSION)
	go install golang.org/x/tools/gopls@latest
	go install google.golang.org/protobuf/cmd/protoc-gen-go@$(PROTOC_GEN_GO_VERSION)
	go install google.golang.org/grpc/cmd/protoc-gen-go-grpc@$(PROTOC_GEN_GO_GRPC_VERSION)
	@echo "Done installing tools"

## build: Build all binaries
build: proto
	@mkdir -p bin
	$(GOBUILD) $(LDFLAGS) -o $(SERVER_BINARY) ./cmd/server
	$(GOBUILD) $(LDFLAGS) -o $(AGENT_BINARY) ./cmd/agent
	$(GOBUILD) $(LDFLAGS) -o $(MCTL_BINARY) ./cmd/mctl
	@echo "Binaries built in ./bin/"

## build-server: Build server binary only
build-server:
	@mkdir -p bin
	$(GOBUILD) $(LDFLAGS) -o $(SERVER_BINARY) ./cmd/server

## build-agent: Build agent binary only
build-agent:
	@mkdir -p bin
	$(GOBUILD) $(LDFLAGS) -o $(AGENT_BINARY) ./cmd/agent

## build-mctl: Build mctl CLI only
build-mctl:
	@mkdir -p bin
	$(GOBUILD) $(LDFLAGS) -o $(MCTL_BINARY) ./cmd/mctl

## test: Run tests with race detector
test: proto
	$(GOTEST) -race -v ./...

## test-coverage: Run tests with coverage report
test-coverage: proto
	$(GOTEST) -race -coverprofile=coverage.out ./...
	$(GOCMD) tool cover -html=coverage.out -o coverage.html
	@echo "Coverage report: coverage.html"

## lint: Run linter
lint:
	golangci-lint run ./...

## lint-fix: Run linter and fix issues
lint-fix:
	golangci-lint run --fix ./...

## generate: Run go:generate across the tree (mocks etc.)
generate:
	$(GOCMD) generate ./...

## proto: Generate protobuf code
proto:
	buf generate

## proto-lint: Lint protobuf files
proto-lint:
	buf lint

## migrate: Run database migrations
migrate:
	@echo "Running migrations..."
	@if [ -z "$(MARIONETTE_DATABASE_URL)" ]; then \
		echo "Error: MARIONETTE_DATABASE_URL is not set"; \
		exit 1; \
	fi
	migrate -path migrations -database "$(MARIONETTE_DATABASE_URL)" up

## migrate-down: Rollback last migration
migrate-down:
	@if [ -z "$(MARIONETTE_DATABASE_URL)" ]; then \
		echo "Error: MARIONETTE_DATABASE_URL is not set"; \
		exit 1; \
	fi
	migrate -path migrations -database "$(MARIONETTE_DATABASE_URL)" down 1

## schema: Regenerate docs/schema.sql from migrations (requires Docker)
schema:
	./scripts/gen-schema.sh

## schema-check: Fail if docs/schema.sql has drifted from migrations (requires Docker)
schema-check:
	./scripts/gen-schema.sh --check

## migrate-create: Create a new migration (usage: make migrate-create name=migration_name)
migrate-create:
	@if [ -z "$(name)" ]; then \
		echo "Error: name is required. Usage: make migrate-create name=migration_name"; \
		exit 1; \
	fi
	migrate create -ext sql -dir migrations -seq $(name)

## dev: Run server with hot reload
dev:
	air -c .air.toml

## clean: Remove build artifacts
clean:
	rm -rf bin/
	rm -rf gen/proto/
	rm -f coverage.out coverage.html

## vet: Run go vet
vet:
	$(GOVET) ./...

## fmt: Format code
fmt:
	gofmt -s -w .
	goimports -w .

## test-pkg: Run tests for a specific package (usage: make test-pkg PKG=./pkg/agent/...)
test-pkg:
	@if [ -z "$(PKG)" ]; then \
		echo "Error: PKG is required. Usage: make test-pkg PKG=./pkg/agent/..."; \
		exit 1; \
	fi
	$(GOTEST) -race -v $(PKG)

## test-coverage-pkg: Run tests with coverage for a specific package
test-coverage-pkg:
	@if [ -z "$(PKG)" ]; then \
		echo "Error: PKG is required. Usage: make test-coverage-pkg PKG=./pkg/agent/..."; \
		exit 1; \
	fi
	$(GOTEST) -race -coverprofile=coverage.out $(PKG)
	$(GOCMD) tool cover -func=coverage.out
	$(GOCMD) tool cover -html=coverage.out -o coverage.html
	@echo ""
	@echo "Coverage report: coverage.html"

## test-executor: Run tests for the Claude executor package
test-executor:
	@mkdir -p /tmp/claude
	$(GOTEST) -race -v ./pkg/agent/executor/claude/...

## test-executor-coverage: Run tests with coverage for Claude executor
test-executor-coverage:
	@mkdir -p /tmp/claude
	$(GOTEST) -race -coverprofile=coverage.out ./pkg/agent/executor/claude/...
	$(GOCMD) tool cover -func=coverage.out
	$(GOCMD) tool cover -html=coverage.out -o coverage.html
	@echo ""
	@echo "Coverage report: coverage.html"

# pkg/store/postgres drives testcontainers, which needs a reachable Docker
# daemon. Inside the test container that means mounting the host socket, so the
# PostgreSQL it starts is a sibling container rather than a nested one.
#
# Its published ports therefore live on the host, not on localhost inside the
# test container: TESTCONTAINERS_HOST_OVERRIDE is what makes the connection
# string point somewhere reachable. host.docker.internal is built in on Docker
# Desktop; --add-host supplies it on Linux.
#
# Without the socket the store tests fail loudly rather than skipping, so these
# targets can no longer go green while silently missing the only tests that
# touch real SQL — which is exactly what they did before.
DOCKER_TEST_FLAGS = --rm \
	-v /var/run/docker.sock:/var/run/docker.sock \
	--add-host host.docker.internal:host-gateway \
	-e DOCKER_HOST=unix:///var/run/docker.sock \
	-e TESTCONTAINERS_HOST_OVERRIDE=host.docker.internal

## test-linux: Run tests in Linux Docker container (for Linux-specific code)
test-linux:
	docker build -t marionette/test:latest -f deploy/docker/test.Dockerfile .
	docker run $(DOCKER_TEST_FLAGS) marionette/test:latest

## test-linux-pkg: Run tests for a specific package in Docker
test-linux-pkg:
	@if [ -z "$(PKG)" ]; then \
		echo "Error: PKG is required. Usage: make test-linux-pkg PKG=./pkg/agent/..."; \
		exit 1; \
	fi
	docker build -t marionette/test:latest -f deploy/docker/test.Dockerfile .
	docker run $(DOCKER_TEST_FLAGS) marionette/test:latest go test -race -v $(PKG)

## test-linux-root: Run tests as root in Linux Docker container (for namespace detection)
test-linux-root:
	docker build -t marionette/test:latest -f deploy/docker/test.Dockerfile .
	docker run $(DOCKER_TEST_FLAGS) --user root marionette/test:latest

## test-store: Run the store tests on the host (needs Docker; no container indirection)
test-store:
	$(GOTEST) -race -count=1 ./pkg/store/...

## docker-build: Build Docker images
docker-build:
	docker build -t marionette/server:latest -f deploy/docker/Dockerfile.server .
	docker build -t marionette/agent:latest -f deploy/docker/Dockerfile.agent .

## docker-up: Start services with docker-compose
docker-up:
	docker-compose -f deploy/docker/docker-compose.yml up -d

## docker-down: Stop services with docker-compose
docker-down:
	docker-compose -f deploy/docker/docker-compose.yml down

# =============================================================================
# TLS Certificates
# =============================================================================

## certs: Generate TLS certificates for mTLS
certs:
	@$(MAKE) -C scripts/certs all

## certs-clean: Remove generated TLS certificates
certs-clean:
	@$(MAKE) -C scripts/certs clean

## certs-verify: Verify generated TLS certificates
certs-verify:
	@$(MAKE) -C scripts/certs verify

# =============================================================================
# Frontend (Web UI)
# =============================================================================

## web-install: Install frontend dependencies
web-install:
	cd web && pnpm install

## web-dev: Start frontend dev server
web-dev:
	cd web && pnpm dev

## web-build: Build frontend for production
web-build:
	cd web && pnpm build
	rm -rf pkg/server/admin/dist
	cp -r web/dist pkg/server/admin/dist
	@echo "Frontend built and copied to pkg/server/admin/dist"

## web-lint: Lint frontend code
web-lint:
	cd web && pnpm lint

## web-clean: Clean frontend artifacts
web-clean:
	rm -rf web/node_modules web/dist
	rm -rf pkg/server/admin/dist
	@echo "Frontend artifacts cleaned"

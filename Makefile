.PHONY: deps build test lint proto migrate dev clean help

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

## deps: Install dependencies and development tools
deps:
	$(GOMOD) download
	$(GOMOD) tidy
	@echo "Installing development tools..."
	go install github.com/air-verse/air@latest
	go install github.com/bufbuild/buf/cmd/buf@latest
	go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest
	go install golang.org/x/tools/gopls@latest
	go install google.golang.org/protobuf/cmd/protoc-gen-go@latest
	go install google.golang.org/grpc/cmd/protoc-gen-go-grpc@latest
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

## test-linux: Run tests in Linux Docker container (for Linux-specific code)
test-linux:
	docker build -t marionette/test:latest -f deploy/docker/test.Dockerfile .
	docker run --rm marionette/test:latest

## test-linux-pkg: Run tests for a specific package in Docker
test-linux-pkg:
	@if [ -z "$(PKG)" ]; then \
		echo "Error: PKG is required. Usage: make test-linux-pkg PKG=./pkg/agent/..."; \
		exit 1; \
	fi
	docker build -t marionette/test:latest -f deploy/docker/test.Dockerfile .
	docker run --rm marionette/test:latest go test -race -v $(PKG)

## test-linux-root: Run tests as root in Linux Docker container (for namespace detection)
test-linux-root:
	docker build -t marionette/test:latest -f deploy/docker/test.Dockerfile .
	docker run --rm --user root marionette/test:latest

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

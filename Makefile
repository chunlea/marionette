.PHONY: deps build test lint proto migrate dev clean help \
	test-docker test-docker-v test-docker-pkg test-docker-root test-docker-coverage

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

## test-docker: Run tests in Docker (bypasses sandbox restrictions)
test-docker:
	docker build -t marionette/test:latest -f deploy/docker/test.Dockerfile .
	docker run --rm marionette/test:latest

## test-docker-v: Run tests in Docker with verbose output
test-docker-v:
	docker build -t marionette/test:latest -f deploy/docker/test.Dockerfile .
	docker run --rm marionette/test:latest go test -race -cover -v ./pkg/agent/...

## test-docker-pkg: Run tests for specific package (usage: make test-docker-pkg PKG=./pkg/agent/executor/permission)
test-docker-pkg:
	@if [ -z "$(PKG)" ]; then \
		echo "Error: PKG is required. Usage: make test-docker-pkg PKG=./pkg/agent/..."; \
		exit 1; \
	fi
	docker build -t marionette/test:latest -f deploy/docker/test.Dockerfile .
	docker run --rm marionette/test:latest go test -race -cover -v $(PKG)

## test-docker-root: Run tests as root in Docker (for namespace detection)
test-docker-root:
	docker build -t marionette/test:latest -f deploy/docker/test.Dockerfile .
	docker run --rm --user root marionette/test:latest

## test-docker-coverage: Run tests in Docker with coverage report
test-docker-coverage:
	docker build -t marionette/test:latest -f deploy/docker/test.Dockerfile .
	docker run --rm -v "$(PWD)":/app marionette/test:latest go test -race -coverprofile=coverage.out ./pkg/agent/...
	$(GOCMD) tool cover -html=coverage.out -o coverage.html
	@echo "Coverage report: coverage.html"

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

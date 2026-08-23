.PHONY: deps build test lint proto migrate dev clean help \
	schema schema-check openapi openapi-check generate test-store \
	bench bench-core bench-store bench-gate bench-check loadtest \
	version dist docker-build docker-up docker-down \
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

# Version stamping. Derived from git so a local build is honest about what it
# is: an exact tag reads as v0.1.0, anything else as v0.1.0-3-g1234abc, with
# -dirty appended when the tree has uncommitted changes.
#
# Expanded once (:=) so every binary in a single `make build` carries the same
# stamp; VERSION=... from the environment or the command line still wins, which
# is how the release workflow passes the tag it is building.
VERSION := $(or $(VERSION),$(shell git describe --tags --always --dirty 2>/dev/null),dev)
COMMIT := $(or $(COMMIT),$(shell git rev-parse --short HEAD 2>/dev/null),unknown)
BUILD_DATE := $(or $(BUILD_DATE),$(shell date -u +%Y-%m-%dT%H:%M:%SZ))

# The linker silently ignores an -X for a symbol that does not exist, so one
# set of flags is safe for all three binaries. Today only mctl declares the vars
# (cmd/mctl/version.go); server and agent light up the moment they do.
VERSION_LDFLAGS=-X main.version=$(VERSION) -X main.commit=$(COMMIT) -X main.buildDate=$(BUILD_DATE)

# The same stamp for image builds, where it also becomes the OCI labels.
DOCKER_BUILD_ARGS=--build-arg VERSION=$(VERSION) \
	--build-arg COMMIT=$(COMMIT) \
	--build-arg BUILD_DATE=$(BUILD_DATE)

# Build flags
LDFLAGS=-ldflags "-s -w $(VERSION_LDFLAGS)"

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

## openapi: Regenerate the public and admin OpenAPI documents from the Go route tables and DTOs
openapi:
	$(GOCMD) run pkg/server/api/openapi_generate.go pkg/server/api/openapi.yaml
	$(GOCMD) run pkg/server/admin/openapi_generate.go pkg/server/admin/openapi.yaml

## openapi-check: Fail if either OpenAPI document has drifted from the code
openapi-check:
	$(GOTEST) -run TestOpenAPIDocumentIsUpToDate ./pkg/server/api/
	$(GOTEST) -run TestAdminOpenAPIDocumentIsUpToDate ./pkg/server/admin/

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
	rm -rf dist/
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

# Performance baseline. See test/perf/BASELINE.md for recorded numbers, the
# machine they came from, and the gate's calibration.
#
# Benchmarks never run with -race. The race detector changes timing by an order
# of magnitude, so a "benchmark" under it measures the detector.
BENCHTIME ?= 2s
BENCHCOUNT ?= 1

# Where bench-gate parks the run it compares. Under tmp/, which is gitignored,
# so a gate run leaves a shared working tree clean.
BENCH_OUT ?= tmp/bench.txt

# The ratio at which bench-gate calls a benchmark a regression. 2x, from the
# run-to-run spread measured on the baseline machine — see the gate section of
# test/perf/BASELINE.md before lowering it.
BENCH_THRESHOLD ?= 2.0

## bench: Run every benchmark (core in-process + store against real PostgreSQL)
bench: bench-core bench-store

## bench-core: Benchmark the scheduler's in-process hot paths
bench-core: proto
	$(GOTEST) -run '^$$' -bench . -benchmem \
		-benchtime $(BENCHTIME) -count $(BENCHCOUNT) ./pkg/server/core/...

## bench-store: Benchmark the hot store queries (needs Docker; skips without it)
bench-store:
	$(GOTEST) -run '^$$' -bench . -benchmem \
		-benchtime $(BENCHTIME) -count $(BENCHCOUNT) ./test/perf/store/...

## bench-gate: Run the core benchmarks and fail on a regression against BASELINE.md
##
## The gate is on here and off in CI, deliberately: BASELINE.md was recorded on
## a developer machine, so the ratio only means something when the run comes
## from one too. See the bench job in .github/workflows/ci.yml.
##
## Output goes to a file rather than through a pipe so a build failure in the
## benchmarks fails this target instead of being swallowed by `tee`.
##
## -count 3, not BENCHCOUNT: the comparison takes a median, and a median of one
## sample is the cold outlier it exists to reject.
bench-gate: proto
	@mkdir -p $(dir $(BENCH_OUT))
	$(GOTEST) -run '^$$' -bench . -benchmem \
		-benchtime $(BENCHTIME) -count 3 ./pkg/server/core/... > $(BENCH_OUT)
	@cat $(BENCH_OUT)
	$(GOCMD) run ./test/perf/benchcmp \
		-bench $(BENCH_OUT) -threshold $(BENCH_THRESHOLD) -gate

## bench-check: Compare an already-recorded run against BASELINE.md, report only
##
## Point BENCH_OUT at a bench.txt — CI's `benchmarks` artifact, for instance.
bench-check:
	$(GOCMD) run ./test/perf/benchcmp \
		-bench $(BENCH_OUT) -threshold $(BENCH_THRESHOLD)

## loadtest: Drive the real stack with fake runners (no model tokens are spent)
loadtest: build
	./scripts/loadtest.sh

# =============================================================================
# Release artifacts
# =============================================================================

## version: Print the version stamp a build would use right now
version:
	@echo "version:    $(VERSION)"
	@echo "commit:     $(COMMIT)"
	@echo "build date: $(BUILD_DATE)"

# The same three targets .github/workflows/release.yml attaches to a release.
# The workflow calls this target rather than repeating the build, so what you
# can produce locally and what lands on the release page cannot drift.
DIST_PLATFORMS ?= darwin/arm64 linux/amd64 linux/arm64

## dist: Cross-compile the mctl tarballs a release attaches (into ./dist)
dist: proto
	@rm -rf dist && mkdir -p dist
	@for platform in $(DIST_PLATFORMS); do \
		os=$${platform%/*}; arch=$${platform#*/}; \
		echo "building mctl $(VERSION) $$os/$$arch"; \
		CGO_ENABLED=0 GOOS=$$os GOARCH=$$arch \
			$(GOBUILD) $(LDFLAGS) -o dist/mctl ./cmd/mctl || exit 1; \
		tar -czf "dist/mctl_$(VERSION)_$${os}_$${arch}.tar.gz" -C dist mctl; \
		rm dist/mctl; \
	done
	@cd dist && { command -v sha256sum >/dev/null 2>&1 \
		&& sha256sum *.tar.gz > SHA256SUMS \
		|| shasum -a 256 *.tar.gz > SHA256SUMS; }
	@echo "Artifacts in ./dist:"
	@ls -1 dist

## docker-build: Build Docker images
docker-build:
	docker build $(DOCKER_BUILD_ARGS) -t marionette/server:latest -f deploy/docker/Dockerfile.server .
	docker build $(DOCKER_BUILD_ARGS) -t marionette/agent:latest -f deploy/docker/Dockerfile.agent .

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
	# The embed directive needs the directory to exist on a clean checkout, and
	# .gitkeep is the only tracked thing in it. Copying over the top used to
	# delete it, leaving `git status` claiming a tracked file had been removed.
	touch pkg/server/admin/dist/.gitkeep
	@echo "Frontend built and copied to pkg/server/admin/dist"

## web-lint: Lint frontend code
web-lint:
	cd web && pnpm lint

## web-clean: Clean frontend artifacts
web-clean:
	rm -rf web/node_modules web/dist
	rm -rf pkg/server/admin/dist
	@echo "Frontend artifacts cleaned"

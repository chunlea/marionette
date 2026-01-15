# Contributing

Guide for contributing to Marionette.

## Getting Started

### Prerequisites

- Go 1.22+
- PostgreSQL 15+
- Docker
- Make

### Setup

```bash
# Clone repository
git clone https://github.com/chunlea/marionette.git
cd marionette

# Install dependencies
make deps

# Setup database
createdb marionette
make migrate

# Build
make build

# Run tests
make test
```

## Development Workflow

### Running Locally

```bash
# Terminal 1: Start server
./bin/server --config configs/local.yaml

# Terminal 2: Start agent
./bin/agent --server localhost:9090 --token dev-token

# Terminal 3: Use CLI
./bin/mctl sessions list
```

### Hot Reload

```bash
make dev
```

### Running Tests

```bash
# All tests
make test

# Specific package
go test -v ./pkg/store/...

# With coverage
make test-coverage
```

### Linting

```bash
make lint
```

## Code Style

### Go Style

- Follow [Effective Go](https://go.dev/doc/effective_go)
- Use `gofmt` for formatting
- Run `golangci-lint` before committing

### Naming Conventions

| Type | Convention | Example |
|------|------------|---------|
| Packages | lowercase | `store`, `provider` |
| Interfaces | -er suffix | `Provider`, `Checker` |
| Structs | PascalCase | `SessionManager` |
| Functions | PascalCase | `CreateSession` |
| Variables | camelCase | `sessionID` |
| Constants | PascalCase | `DefaultTimeout` |

### Error Handling

```go
// Good: wrap errors with context
if err != nil {
    return fmt.Errorf("creating session: %w", err)
}

// Good: use typed errors
var ErrNotFound = errors.New("not found")

if errors.Is(err, ErrNotFound) {
    // handle not found
}
```

### Logging

```go
// Use structured logging
logger.Info("session created",
    zap.String("session_id", session.ID),
    zap.String("agent", session.Agent),
)
```

## Project Structure

```
marionette/
├── cmd/                    # Entry points
│   ├── server/main.go
│   ├── agent/main.go
│   └── mctl/
├── pkg/                    # Public packages
│   ├── server/             # Server implementation
│   │   ├── grpc/           # gRPC handlers
│   │   ├── api/            # Public HTTP API
│   │   ├── admin/          # Admin HTTP API
│   │   └── core/           # Business logic
│   ├── agent/              # Agent implementation
│   ├── provider/           # Provider implementations
│   ├── store/              # Database layer
│   └── client/             # Go SDK
├── internal/               # Private packages
├── api/proto/              # Protocol definitions
├── gen/proto/              # Generated code
├── configs/                # Example configs
├── deploy/                 # Deployment configs
└── docs/                   # Documentation
```

## Pull Request Process

### Branch Naming

```
{developer}/{phase}-{feature}

Examples:
  alice/g8-documentation
  bob/g9-pool-provider
```

### Commit Messages

Follow [Conventional Commits](https://www.conventionalcommits.org/):

```
feat: add session suspend/resume
fix: handle permission timeout correctly
docs: update API reference
test: add provider tests
refactor: simplify task scheduler
```

### PR Template

```markdown
## Summary
Brief description of changes

## Changes
- Change 1
- Change 2

## Test Plan
- [ ] Unit tests added
- [ ] Integration tests pass
- [ ] Manual testing done

## Documentation
- [ ] README updated (if needed)
- [ ] API docs updated (if needed)
```

### Review Checklist

- [ ] Code follows style guidelines
- [ ] Tests pass (`make test`)
- [ ] Linter passes (`make lint`)
- [ ] Documentation updated
- [ ] No security vulnerabilities
- [ ] Backwards compatible (or noted)

## Testing Guidelines

### Test Coverage

- Minimum 90% coverage for new code
- Run `make test-coverage` to check

### Test Types

| Type | Location | Purpose |
|------|----------|---------|
| Unit | `*_test.go` | Test individual functions |
| Integration | `*_integration_test.go` | Test component interactions |
| E2E | `test/e2e/` | Test full workflows |

### Test Naming

```go
func TestSessionManager_Create(t *testing.T) {
    t.Run("creates session with valid input", func(t *testing.T) {
        // ...
    })

    t.Run("returns error for invalid agent", func(t *testing.T) {
        // ...
    })
}
```

## Release Process

1. Update version in `version.go`
2. Update CHANGELOG.md
3. Create release PR
4. After merge, tag release
5. CI builds and publishes artifacts

## Getting Help

- [GitHub Issues](https://github.com/chunlea/marionette/issues)
- [Discussions](https://github.com/chunlea/marionette/discussions)

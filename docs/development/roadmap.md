# Roadmap

Marionette development roadmap and current status.

## Current Status

Marionette is actively developed. Core features are implemented and production-ready.

## Phases Overview

| Phase | Status | Description |
|-------|--------|-------------|
| Phase 1: Foundation | ✅ Complete | Project setup, database, IDs |
| Phase 2: gRPC & Runner | ✅ Complete | Agent protocol, runner management |
| Phase 3: HTTP APIs | ✅ Complete | Public API, Admin API |
| Phase 4: CLI & Streaming | ✅ Complete | mctl CLI, log streaming |
| Phase 5: Auth & Encryption | ✅ Complete | API keys, credential encryption |
| Phase 6: Sessions & Tasks | ✅ Complete | Session lifecycle, task execution |
| Phase 7: Storage | ✅ Complete | CAS, workspace sync |
| Phase 8: Production Ready | 🟡 In Progress | Docker, K8s, observability |
| Phase 9: Advanced Providers | 📋 Planned | Pool providers, profiles |
| Phase 10: Advanced Features | 📋 Planned | Tunnels, scheduling |

## Phase Details

### Phase 8: Production Ready (Current)

- [x] Docker & Kubernetes deployment
- [x] Health endpoints (liveness/readiness)
- [x] Prometheus metrics
- [x] OpenTelemetry tracing
- [x] Documentation site
- [ ] Performance testing

### Phase 9: Advanced Providers

- [ ] Pool provider implementation
- [ ] Profile management
- [ ] Suspend strategies (pause, snapshot)
- [ ] Runner watchdog

### Phase 10: Advanced Features

- [ ] HTTP/TCP tunnels
- [ ] Desktop streaming (WebRTC)
- [ ] Scheduled tasks
- [ ] Webhooks

## Full Roadmap

For detailed implementation plans, see the full roadmap:

[`docs/roadmap.md`](https://github.com/chunlea/marionette/blob/main/docs/roadmap.md)

## Contributing

Want to help? Check out:

- [Contributing Guide](contributing.md)
- [GitHub Issues](https://github.com/chunlea/marionette/issues)
- [Good First Issues](https://github.com/chunlea/marionette/labels/good%20first%20issue)

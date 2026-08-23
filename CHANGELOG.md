# Changelog

Notable changes to Marionette, newest first. The format follows
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and versions follow
[semantic versioning](https://semver.org/spec/v2.0.0.html).

Commits use conventional-commit subjects (`feat(scope):`, `fix(scope):`,
`build(scope):` …), so the first draft of a release's entries is mechanical:
`git log --no-merges --pretty='- %s' <previous-tag>..HEAD`, which is also what
the release workflow puts into the draft release notes. Turning that list into
what belongs here is a human edit — this file records what changed for someone
*using* Marionette, not every change in the tree.

How a release is cut: [docs/development/releasing.md](docs/development/releasing.md).

## [Unreleased]

Nothing yet.

## [0.1.0] — 2026-08-23

First release. Marionette was paused in January 2026 with the architecture in
place and almost nothing wired together: the main path — create a session, run a
task on a real Claude CLI, suspend, resume — had never run end to end. This
release is the product of three restart rounds ([#127], [#128], [#129]) that
made it run, and then made it operable.

The whole user journey is automated in `scripts/smoke.sh`, 20 steps: fresh
database from migrations → fail-closed admin → pool runner joins → session
allocates a runner → task dispatches → a real Claude CLI executes it →
`PreToolUse` permission gate fires → approve over the API → completion with real
token counts → logs → suspend / resume / terminate.

### Added

- **Sessions, tasks and task runs** with the documented lifecycles, executed
  against a real Claude Code CLI: honest token accounting, outcomes, and the
  agent conversation carried across suspend and resume.
- **Permission gating before a tool runs**, not after: Claude Code's
  `PreToolUse` hook asks the server, the task waits for an answer, and the gate
  fails closed — deny-by-default on anything unknown, including MCP tools.
- **Runners** through the Docker provider and pools, with heartbeats, automatic
  allocation on task creation, and an orphan reaper.
- **Multi-tenancy** injected from the authenticating credential, enforced in
  every statement, with PostgreSQL row-level security as a fail-closed backstop.
  The server refuses to start when isolation would be unenforceable.
  Single-tenant deployments stay zero-config.
- **Workspaces that survive runner changes**: content-addressed sync on suspend,
  restore on attach, encryption at rest, and content-defined chunking for large
  trees — bounded memory, incremental re-sync (touch one file in a thousand and
  only that file's chunks upload).
- **Network isolation you can prove**: restricted containers are created with no
  interface but loopback and get their rules before any egress is possible; DNS
  allow-lists refresh on TTL; air-gapped and proxy modes enforce what they
  promise; Kubernetes NetworkPolicy parity where the primitive allows it.
- **Scheduling**: database-arbitrated idempotent dispatch, automatic redispatch
  with backoff that parks poisoned tasks, exactly-once execution under
  concurrent wakes, and a recorded performance baseline with a fake-executor
  load test (50 sessions / 200 tasks, none lost).
- **Log archiving**: a terminated session's logs stream into one compressed,
  optionally encrypted object; partition drops are gated on archive coverage;
  retrieval falls back to the archive transparently.
- **HTTP and TCP tunnels** into a session's runner, with a per-connection pump
  so one stalled consumer cannot stall the control stream.
- **A generated API contract**: one OpenAPI document per API (public and admin)
  generated from the code that serves it, route-coverage tests both ways,
  generated TypeScript types, and drift checks in CI.
- **Admin web UI** served from the server binary, behind basic auth.
- **`mctl`**, the CLI, with `-o json|yaml|table` on every command.
- **Published images**: `ghcr.io/chunlea/marionette-server` and
  `ghcr.io/chunlea/marionette-agent`.

### Fixed

The live-fire bugs the restart review found — each of these was reachable from
the main path:

- A send on a closed channel that panicked the server.
- The admin API served without authentication when credentials were absent; it
  now fails closed and the server will not start without them.
- Multi-step writes that could half-apply; they run in transactions now.
- Chunk garbage collection that could delete live chunks.
- Tunnel drop-on-burst, a close race, and a TCP relay that mixed connections.
- A log-partition time bomb: no default partition and no maintainer job.
- `WorkspaceSynced` reported success when nothing had been synced.

### Known gaps

- Desktop, browser and Android streaming is **frozen**: the code compiles behind
  a build tag and a config flag, but the SFU has no media source and cannot
  deliver a frame. Do not build on it.
- The E2B provider is implemented but unverified against the live API.
- Multi-process server deployments need a database-arbitrated runner claim;
  single-process is correct today.
- `mctl admin runner-tokens create` prints empty fields, and there is no
  `mctl runners` command — list runners via `GET /api/v1/runners`.
- The published v0.1.0 images were built by hand before the release workflow
  existed: they are `linux/arm64` only, and no `mctl` binaries are attached to
  this release. Both are fixed from the next tag on.

[#127]: https://github.com/chunlea/marionette/pull/127
[#128]: https://github.com/chunlea/marionette/pull/128
[#129]: https://github.com/chunlea/marionette/pull/129

[Unreleased]: https://github.com/chunlea/marionette/compare/v0.1.0...HEAD
[0.1.0]: https://github.com/chunlea/marionette/releases/tag/v0.1.0

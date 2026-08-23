# Performance baseline

First recorded numbers for roadmap 8.4. These exist so a later change has
something to be compared against, and so an argument about performance can
start from a measurement instead of an intuition.

There is a gate now, but only where it means something: `make bench-gate` on
this machine class fails on a >2x regression, and CI compares and reports
without failing. See [The regression gate](#the-regression-gate) for the
calibration and why CI does not gate.

Recorded **2026-08-23** on branch `restart/round-3`.

## Machine

| | |
|---|---|
| CPU | Apple M4 Max, 16 cores |
| Memory | 128 GB |
| OS | macOS 27.0 (darwin/arm64) |
| Go | go1.26.5 |
| Docker | 29.4.0 |
| PostgreSQL | `postgres:15-alpine` (benchmarks) / `postgres:16-alpine` (load test), in a local container |

A laptop is not a server. Treat the absolute numbers as a shape, and the ratios
between them as the thing worth remembering; repeat runs of the load test move
by roughly 10%. CI records its own on a shared
runner (`bench` job, artifact `benchmarks`), and those will be slower and
noisier — which is exactly why that job does not gate.

## How to reproduce

```bash
make bench          # everything below except the load test
make bench-core     # in-process only, no Docker needed
make bench-store    # real PostgreSQL in a container
make loadtest       # the full stack, fake executor, no model tokens spent
```

`BENCHTIME` and `BENCHCOUNT` are overridable (`make bench BENCHTIME=5s
BENCHCOUNT=6`). Benchmarks never run under `-race`: the detector changes timing
by an order of magnitude, so a benchmark under it measures the detector.

## The regression gate

```bash
make bench-gate                      # run the core benchmarks, fail on >2x
make bench-check BENCH_OUT=bench.txt # compare a recorded run, report only
```

`test/perf/benchcmp` reads this file's tables and a `go test -bench` run, and
prints every benchmark it can match with its ratio. It takes the **median** of
repeated measurements, not the mean, and treats a missing baseline as a `SKIP`
rather than a failure.

### Threshold: 2x, and where that came from

Seven runs of `./pkg/server/core/...` on the machine above, on an unmodified
tree, at `-benchtime 1s`:

| Benchmark | baseline | steady-state spread | worst single run |
|---|---:|---:|---:|
| `TaskDispatch_CreateToAssigned` | 5,095 | 4,883–5,532 (1.13x) | **14,524 (2.85x)** |
| `TaskDispatch_DispatchNext` | 3,812 | 3,707–4,771 (1.29x) | 4,771 (1.25x) |
| `TaskDispatch_NoWorkToDo` | 112 | 111.9–117.7 (1.05x) | 117.7 (1.05x) |
| `RedispatchPass_Empty/sessions=1` | 204 | 207.0–223.7 (1.08x) | 223.7 (1.10x) |
| `RedispatchPass_Empty/sessions=10` | 1,300 | 1,327–1,540 (1.16x) | 1,540 (1.18x) |
| `RedispatchPass_Empty/sessions=50` | 6,102 | 6,261–7,182 (1.15x) | 7,182 (1.18x) |
| `Permission_RoundTrip` | 1,125 | 1,121–1,184 (1.06x) | 1,184 (1.05x) |

Steady state is tight — nothing moves more than ~1.3x, and repeated
measurements inside one process (`-count 5`) stay inside 10%. So 2x leaves
roughly a 1.5x margin over the noise, which is enough that a firing gate is
worth reading and not so tight that it fires on its own.

The outlier is the interesting number. `CreateToAssigned` came in at 2.85x its
own steady state on one run — the first run after a full `go build`, with the
machine still busy compiling. Nothing about the code changed. That single
sample is why the gate is off in CI:

- **The machines do not match.** These numbers are an Apple M4 Max laptop; the
  `bench` job is a shared 4-core runner inside a container. A ratio between
  them is mostly the hardware gap, and would trip 2x before any code did.
- **A busy machine is CI's permanent condition.** The one local run that broke
  2x is the condition a shared runner is always in.

An honest report beats a gate people learn to click past. So CI runs the same
comparison with the gate off and folds the report into the `benchmarks`
artifact, and `make bench-gate` — same tool, same threshold, one machine on
both sides of the comparison — is where the gate has teeth.

Re-record this file when the hot paths legitimately change, and prefer
re-recording on this machine class so the gate keeps meaning what it says.
`benchcmp` lists any baseline entry a run did not produce under **not
measured** rather than failing, so a partial run (the store suite skips itself
without Docker) reports cleanly.

## Scheduler, in process

`go test -bench . -benchmem -benchtime 1s ./pkg/server/core/...`

These run against the in-memory test store. What they measure is **manager
logic** — transaction shape, round-trip count, locking, allocations — not the
database. Asked about the database they would report the speed of a Go map.

| Benchmark | ns/op | B/op | allocs/op |
|---|---:|---:|---:|
| `TaskDispatch_CreateToAssigned` | 5,095 | 4,585 | 46 |
| `TaskDispatch_DispatchNext` | 3,812 | 2,616 | 28 |
| `TaskDispatch_NoWorkToDo` | 112 | 200 | 7 |
| `RedispatchPass_Empty/sessions=1` | 204 | 664 | 7 |
| `RedispatchPass_Empty/sessions=10` | 1,300 | 5,776 | 43 |
| `RedispatchPass_Empty/sessions=50` | 6,102 | 28,512 | 203 |
| `Permission_RoundTrip` | 1,125 | 1,759 | 17 |

What these say:

- **A trigger that finds nothing costs ~112ns and 7 allocations.** That is the
  number the redispatch design rests on: every suspend, detach, terminate,
  busy→idle and sweep tick fires a wake, and most find nothing. If this were
  microseconds the "wakes are cheap and idempotent" rule would be a wish.
- **An empty pass is linear in sessions and flat per session** (~120ns each).
  50 parked sessions cost 6µs of Go time per wake, before any database.
- **`DispatchNext` is 3.8µs**, and a pass calls it once per session it moves.
  The database dominates that (see below), which is the right shape.

`CreateToAssigned` and `DispatchNext` clear the fake store's tables between
iterations. Without that they measure the fake: its `ListTasks` scans a whole
map and its `CreateTaskRun` scans every run to enforce `UNIQUE(task_id,
attempt)`, so both go linear in `b.N` and the reported per-op cost climbs with
the iteration count — 244µs/op at `-benchtime 2s`, entirely artifact. A real
store answers both from an index.

## Store, against real PostgreSQL

`go test -bench . -benchmem -benchtime 1s ./test/perf/store/...`

Container on the same machine, so this is loopback latency plus Postgres, with
no network in between. Row level security wraps every statement in a
transaction (see `pkg/store/postgres/tenant.go`), and that shows.

| Benchmark | ns/op | µs/op | B/op | allocs/op |
|---|---:|---:|---:|---:|
| `Store_GetSession` | 107,381 | 107 | 6,355 | 95 |
| `Store_PendingTaskScan/backlog=1` | 215,439 | 215 | 4,382 | 91 |
| `Store_PendingTaskScan/backlog=10` | 231,622 | 232 | 12,196 | 202 |
| `Store_PendingTaskScan/backlog=100` | 342,747 | 343 | 90,651 | 1,285 |
| `Store_CreateLog` | 111,487 | 111 | 1,028 | 23 |
| `Store_CreateLogs/batch=10` | 181,092 | 181 | 21,787 | 178 |
| `Store_CreateLogs/batch=100` | 976,612 | 977 | 327,747 | 2,469 |

What these say:

- **The database is four orders of magnitude more expensive than the manager
  logic above it.** A dispatch decision costs 3.8µs of Go and ~215µs of
  PostgreSQL. Any optimisation that does not remove a query is noise.
- **The pending-task scan barely notices backlog size** — 215µs at 1 row,
  343µs at 100. The cost is the round trip, not the rows. This is why the
  redispatch pass reads pending and running in one query: halving the query
  count halves the real cost, and pre-filtering in Go is free by comparison.
- **Batched log writes are worth it, up to a point.** 10 lines batched cost
  18µs each against 111µs unbatched — 6x. At 100 the per-line cost is 9.8µs,
  so most of the win is already collected by 10. The log streamer's buffering
  earns its complexity.
- ~95 allocations for a single-row read is high, and worth a look before
  anything else here. It is the tenant transaction plus pgx row scanning.

## Load test: the whole stack

`make loadtest`, which is `scripts/loadtest.sh`. Real server, real PostgreSQL,
real gRPC, real control channel, real log streaming, real store. The only
substitution is the coding agent: `test/perf/fakeexec` scripts its output, so a
run spends no model tokens and needs no Claude CLI.

Each fake task emits 20 log lines and takes ~200ms.

### Fully provisioned — 50 runners, 50 sessions, 200 tasks

The brief's target shape: one runner per session, four tasks each.

```
wall clock        1.084s
tasks             200   completed 200, failed 0, canceled 0, never terminal 0
runner executions 200
task runs         200   (0 tasks needed more than one)
throughput        184.6 tasks/s

dispatch latency (task created -> run queued on a runner)
  samples  200
  min      33ms
  p50      289ms
  p95      565ms
  p99      584ms
  max      610ms
```

**Targets met**: zero lost tasks, zero stuck tasks, and exactly one runner
execution and one run row per task — no task reached two runners.

The p50 of 289ms against a 3.8µs in-process dispatch is contention, not the
scheduler being slow: 200 tasks are created concurrently against 50 sessions and
50 runners, so most of that number is queueing behind the 4-per-session
sequential invariant and the ~215µs-per-query database.

### Under-provisioned — 10 runners, 50 sessions, 200 tasks

`RUNNERS=10 RELEASE_IDLE=1 scripts/loadtest.sh`. Forty of the fifty sessions
have no runner when their tasks are created and can only proceed when another
session gives one back. `-release-idle` suspends a session as soon as its
backlog is done, which is what frees a runner.

```
wall clock        5.443s
tasks             200   completed 200, failed 0, canceled 0, never terminal 0
runner executions 200
task runs         200   (0 tasks needed more than one)
throughput        36.7 tasks/s

dispatch latency
  p50      2.686s
  p95      5.002s
  max      5.163s
```

This is the runner-freed trigger measured against the real stack rather than a
fake store. From the server log for that run:

```
wake triggers:          runner_freed only (4-7 passes across repeat runs)
parked sessions woken:  40
sweeps:                 0
```

All forty parked sessions were woken by the edge trigger, and the whole run
finished in 5-6s — comfortably inside a single 60s sweep interval, so the
backstop never had to carry any of it. A handful of coalesced `runner_freed`
passes covered all forty sessions between them, which is the coalescing design
doing what it claims: a storm of triggers costs a bounded number of passes, not
one per trigger.

Latency is five seconds at p95 here **by construction**: a task whose session
has no runner cannot start until one is handed back, so the number measures
capacity, not the scheduler. It is recorded because the contrast with the
provisioned run is the point.

## Two bugs this found

Worth recording, because they are the argument for having built it:

1. **Nothing dispatched a session's next task when one completed.** The backlog
   waited for the 60s sweeper, so a session with a queue ran one task per
   minute regardless of task duration. First run: 8 tasks over 4 sessions took
   two minutes and left two stuck. Fixed in `OnTaskCompleted`; regression test
   `TestTaskManager_OnTaskCompleted_DispatchesTheNextTask`.

2. **Concurrent session creation handed the same runner to two sessions.**
   `runnerClaimed` reads the database, so between choosing an idle runner and
   writing that choice there is a window in which it still looks free; `Activate`
   then detached the loser. Four sessions against four idle runners left two
   with nothing. Fixed with an in-process reservation; regression test
   `TestSessionManager_ConcurrentEnsureRunner_GivesEachSessionItsOwnRunner`.
   **Still open across processes** — that needs a claim the database arbitrates.

## What is not measured

Stated so nobody mistakes silence for a clean bill:

- **No multi-process or multi-replica runs.** Every number here is one server
  process. The runner-allocation reservation above is process-local, and its
  cross-process behaviour is untested because there is nothing yet to test it
  with.
- **No sustained or soak load.** The longest run is 5.4 seconds. Nothing here
  says anything about connection-pool exhaustion, partition growth, memory over
  hours, or what happens when the log table has a hundred million rows.
- **No permission round trip under load.** The fake executor can request
  permissions (`PermissionEvery`) but the load test leaves it off: a pending
  permission blocks its task until something answers, which measures the
  responder rather than the scheduler.
- **No real agent.** By design — that is what makes the load test free to run —
  but it means the numbers say nothing about workspace I/O, sandbox setup, or
  the Claude CLI's own startup cost. `scripts/smoke.sh` is what covers the real
  agent, one task at a time.
- **No tenant fan-out.** Every run is single-tenant, so the row-level-security
  transaction cost above is measured but its behaviour under many tenants is
  not.

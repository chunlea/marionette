#!/usr/bin/env bash
# loadtest.sh — bring up the smoke topology and drive it with fake runners.
#
# The topology is the one scripts/smoke.sh proves works: a fresh database from
# the migrations, the real server, and pool runners registering over gRPC. The
# only substitution is the coding agent: test/perf/loadtest builds its runners
# from the same exported pkg/agent pieces cmd/agent uses and swaps in a scripted
# executor, so a run costs nothing in model tokens and needs no Claude CLI.
#
# It runs on its own ports by default so it does not fight a development server
# or a smoke run sharing the machine. Override with API_PORT/ADMIN_PORT/GRPC_PORT.
#
# Requirements: docker, and the four ports below free.
#
# Usage:
#   scripts/loadtest.sh                       # 50 runners, 50 sessions, 200 tasks
#   RUNNERS=8 RELEASE_IDLE=1 scripts/loadtest.sh  # under-provision: exercises
#                                                 # the runner-freed trigger
#   SESSIONS=10 TASKS=40 scripts/loadtest.sh  # a quick shape check
#
# Everything it starts, it stops. Everything it writes goes under LOAD_DIR.
set -u -o pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
LOAD_DIR="${LOAD_DIR:-$ROOT/.claude/tmp/loadtest}"
PG_PORT="${PG_PORT:-15433}"
PG_NAME=marionette-loadtest-pg
API_PORT="${API_PORT:-18080}"
ADMIN_PORT="${ADMIN_PORT:-18081}"
GRPC_PORT="${GRPC_PORT:-19090}"
API="http://localhost:${API_PORT}"
ADMIN="http://localhost:${ADMIN_PORT}"
GRPC="localhost:${GRPC_PORT}"
ADMIN_USER=admin
ADMIN_PASS="${LOADTEST_ADMIN_PASS:-loadpass123}"
DB_URL="postgres://marionette:marionette@localhost:${PG_PORT}/marionette?sslmode=disable"

RUNNERS="${RUNNERS:-50}"
SESSIONS="${SESSIONS:-50}"
TASKS="${TASKS:-200}"
LOG_LINES="${LOG_LINES:-20}"
TASK_MS="${TASK_MS:-200}"
DEADLINE="${DEADLINE:-10m}"
# With fewer runners than sessions, most sessions park at creation and can only
# proceed when another session gives its runner back. RELEASE_IDLE=1 suspends a
# session as soon as its backlog is done, which is what makes an
# under-provisioned run a test of the runner-freed trigger.
RELEASE_IDLE="${RELEASE_IDLE:-0}"
RELEASE_FLAG=""
[ "$RELEASE_IDLE" = "1" ] && RELEASE_FLAG="-release-idle"

mkdir -p "$LOAD_DIR"

fail() { echo "FAIL  $*" >&2; cleanup; exit 1; }

cleanup() {
  if [ -f "$LOAD_DIR/server.pid" ]; then
    kill "$(cat "$LOAD_DIR/server.pid")" 2>/dev/null
    rm -f "$LOAD_DIR/server.pid"
  fi
  docker rm -f "$PG_NAME" >/dev/null 2>&1
  # The harness removes its own workspaces; this catches an interrupted run.
  rm -rf "$LOAD_DIR/workspace" "$LOAD_DIR/workspaces" "$LOAD_DIR/storage"
  true
}
trap cleanup INT TERM EXIT

echo "== marionette load test =="
echo "   runners=$RUNNERS sessions=$SESSIONS tasks=$TASKS"
echo

echo "-- build"
make -C "$ROOT" build >"$LOAD_DIR/build.log" 2>&1 || fail "build (see $LOAD_DIR/build.log)"
go build -o "$LOAD_DIR/loadtest" "$ROOT/test/perf/loadtest" >>"$LOAD_DIR/build.log" 2>&1 \
  || fail "building the harness (see $LOAD_DIR/build.log)"

echo "-- config"
python3 - "$ROOT/configs/local.yaml" "$LOAD_DIR" "$API_PORT" "$ADMIN_PORT" "$GRPC_PORT" <<'PYEOF' || fail "writing the server config"
import sys, re, os, pathlib

src, load_dir, api_port, admin_port, grpc_port = sys.argv[1:6]
text = pathlib.Path(src).read_text()

# Ports, in the order they appear under server.{api,admin,grpc}.
ports = iter([api_port, admin_port, grpc_port])
text = re.sub(r"^(\s*port:\s*)\d+", lambda m: m.group(1) + next(ports), text, count=3, flags=re.M)

# Keep every byte the run writes inside LOAD_DIR.
text = text.replace('path: "./data/storage"', 'path: "%s/storage"' % load_dir)
text = text.replace('base_dir: "./data/workspaces"', 'base_dir: "%s/workspaces"' % load_dir)

# 50 runners at debug level write more log than the run does work.
text = text.replace("level: debug", "level: info")

pathlib.Path(os.path.join(load_dir, "server.yaml")).write_text(text)
PYEOF

echo "-- database"
docker rm -f "$PG_NAME" >/dev/null 2>&1
docker run -d --name "$PG_NAME" \
  -e POSTGRES_USER=marionette -e POSTGRES_PASSWORD=marionette -e POSTGRES_DB=marionette \
  -p "${PG_PORT}:5432" postgres:16-alpine >/dev/null || fail "starting postgres"
sleep 5

docker run --rm -v "$ROOT/migrations:/migrations" migrate/migrate:v4.18.1 \
  -path=/migrations \
  -database "postgres://marionette:marionette@host.docker.internal:${PG_PORT}/marionette?sslmode=disable" \
  up >"$LOAD_DIR/migrate.log" 2>&1 || fail "migrations (see $LOAD_DIR/migrate.log)"

echo "-- server"
(
  cd "$ROOT" || exit 1
  MARIONETTE_DATABASE_URL="$DB_URL" \
  MARIONETTE_MASTER_KEY="$(openssl rand -hex 32)" \
  MARIONETTE_ENCRYPTION_KEY="$(openssl rand -hex 32)" \
  MARIONETTE_UI_USERNAME="$ADMIN_USER" \
  MARIONETTE_UI_PASSWORD="$ADMIN_PASS" \
  nohup ./bin/server --config "$LOAD_DIR/server.yaml" >"$LOAD_DIR/server.log" 2>&1 &
  echo $! >"$LOAD_DIR/server.pid"
)

for _ in $(seq 1 30); do
  [ "$(curl -s -o /dev/null -w '%{http_code}' "$API/health")" = 200 ] && break
  sleep 1
done
[ "$(curl -s -o /dev/null -w '%{http_code}' "$API/health")" = 200 ] \
  || fail "server did not become healthy (see $LOAD_DIR/server.log)"

echo "-- credentials"
API_KEY=$(curl -s -u "$ADMIN_USER:$ADMIN_PASS" -X POST "$ADMIN/admin/api/v1/keys" \
  -H 'Content-Type: application/json' -d '{"name":"loadtest","scopes":["*"]}' \
  | python3 -c 'import json,sys;print(json.load(sys.stdin)["raw_token"])') \
  || fail "creating an API key"
[ -n "$API_KEY" ] || fail "the API key came back empty"

# One token per runner. A runner token binds to the first runner that presents
# it, so a shared token makes runner two through N fail with "already connected".
echo "   minting $RUNNERS runner tokens"
RUNNER_TOKENS=""
for _ in $(seq 1 "$RUNNERS"); do
  TOKEN=$(curl -s -u "$ADMIN_USER:$ADMIN_PASS" -X POST "$ADMIN/admin/api/v1/runner-tokens" \
    -H 'Content-Type: application/json' -d '{"pool_name":"default"}' \
    | python3 -c 'import json,sys;print(json.load(sys.stdin)["raw_token"])')
  [ -n "$TOKEN" ] || fail "a runner token came back empty"
  RUNNER_TOKENS="${RUNNER_TOKENS:+$RUNNER_TOKENS,}$TOKEN"
done

echo "-- run"
MARIONETTE_API_KEY="$API_KEY" MARIONETTE_RUNNER_TOKENS="$RUNNER_TOKENS" \
"$LOAD_DIR/loadtest" \
  -api "$API" -grpc "$GRPC" -pool default \
  -runners "$RUNNERS" -sessions "$SESSIONS" -tasks "$TASKS" \
  -log-lines "$LOG_LINES" -task-ms "$TASK_MS" -deadline "$DEADLINE" \
  -workspace "$LOAD_DIR/workspace" $RELEASE_FLAG \
  | tee "$LOAD_DIR/result.txt"
STATUS=${PIPESTATUS[0]}

echo
echo "server log: $LOAD_DIR/server.log"
echo "result:     $LOAD_DIR/result.txt"

exit "$STATUS"

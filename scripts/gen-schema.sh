#!/usr/bin/env bash
#
# Generate docs/schema.sql from migrations/*.up.sql.
#
# docs/schema.sql used to be hand-maintained and called "the source of truth".
# It drifted: it was missing migrations 002, 003 and 006 entirely, and
# deploy/docker/docker-compose.yml provisioned the dev database from it, so a
# fresh `docker compose up` produced a database where the webhook and stream
# code paths failed at runtime.
#
# Now migrations are the source of truth and this script renders them into
# docs/schema.sql: it applies every migration to a throwaway PostgreSQL
# container and dumps the resulting schema.
#
# Usage:
#   scripts/gen-schema.sh            # rewrite docs/schema.sql
#   scripts/gen-schema.sh --check    # fail if docs/schema.sql is out of date
#
# Requires Docker.

set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
MIGRATIONS_DIR="$REPO_ROOT/migrations"
HEADER_FILE="$REPO_ROOT/scripts/schema-header.sql"
OUTPUT_FILE="$REPO_ROOT/docs/schema.sql"

# Pinned: pg_dump output differs between major versions, so the generated file
# would flap if this floated. Keep in sync with deploy/docker/docker-compose.yml.
PG_IMAGE="postgres:16-alpine"
DB_NAME="marionette_schema"

CHECK_ONLY=false
if [[ "${1:-}" == "--check" ]]; then
    CHECK_ONLY=true
elif [[ $# -gt 0 ]]; then
    echo "usage: $(basename "$0") [--check]" >&2
    exit 2
fi

if ! command -v docker >/dev/null 2>&1; then
    echo "error: docker is required to generate the schema" >&2
    exit 1
fi

CONTAINER=""
cleanup() {
    if [[ -n "$CONTAINER" ]]; then
        docker rm -f "$CONTAINER" >/dev/null 2>&1 || true
    fi
}
trap cleanup EXIT

echo "==> starting $PG_IMAGE"
CONTAINER=$(docker run -d --rm \
    -e POSTGRES_PASSWORD=schema \
    -e POSTGRES_DB="$DB_NAME" \
    "$PG_IMAGE")

for _ in $(seq 1 60); do
    if docker exec "$CONTAINER" pg_isready -U postgres -d "$DB_NAME" >/dev/null 2>&1; then
        break
    fi
    sleep 1
done
if ! docker exec "$CONTAINER" pg_isready -U postgres -d "$DB_NAME" >/dev/null 2>&1; then
    echo "error: postgres did not become ready" >&2
    exit 1
fi

echo "==> applying migrations"
shopt -s nullglob
migrations=("$MIGRATIONS_DIR"/*.up.sql)
shopt -u nullglob
if [[ ${#migrations[@]} -eq 0 ]]; then
    echo "error: no migrations found in $MIGRATIONS_DIR" >&2
    exit 1
fi
# Numeric prefixes are zero-padded, so lexical order is migration order.
IFS=$'\n' migrations=($(printf '%s\n' "${migrations[@]}" | sort)); unset IFS

for migration in "${migrations[@]}"; do
    echo "    $(basename "$migration")"
    docker exec -i "$CONTAINER" \
        psql -q -v ON_ERROR_STOP=1 -U postgres -d "$DB_NAME" >/dev/null < "$migration"
done

# The daily log partitions are created from the current date, so dumping them
# would make this file change every day. They are runtime state maintained by
# pkg/jobs.PartitionMaintainer, not schema. logs_default is schema and stays.
echo "==> dropping date-dependent log partitions"
docker exec -i "$CONTAINER" psql -q -v ON_ERROR_STOP=1 -U postgres -d "$DB_NAME" >/dev/null <<'SQL'
DO $$
DECLARE
    part RECORD;
BEGIN
    FOR part IN
        SELECT c.relname AS name
        FROM pg_class c
        JOIN pg_inherits i ON c.oid = i.inhrelid
        JOIN pg_class p ON i.inhparent = p.oid
        WHERE p.relname = 'logs' AND c.relname <> 'logs_default'
    LOOP
        EXECUTE format('DROP TABLE %I', part.name);
    END LOOP;
END $$;
SQL

echo "==> dumping schema"
TMP_OUT="$(mktemp)"
trap 'cleanup; rm -f "$TMP_OUT"' EXIT

{
    cat <<EOF
--
-- GENERATED FILE — DO NOT EDIT.
--
-- Source of truth: migrations/*.up.sql
-- Prose/design notes: scripts/schema-header.sql
-- Regenerate with:  make schema
-- Drift is checked in CI (make schema-check).
--
-- Rendered from $PG_IMAGE. The daily partitions of \`logs\` are omitted: they
-- are created at runtime by pkg/jobs.PartitionMaintainer, not by migrations.
--

EOF
    cat "$HEADER_FILE"
    echo
    docker exec "$CONTAINER" \
        pg_dump --schema-only --no-owner --no-privileges -U postgres -d "$DB_NAME" |
        # \restrict / \unrestrict carry a random token per dump, so they must
        # go or every check would report drift. The version banner and the
        # session SET preamble are noise for a reference document.
        grep -v '^\\restrict ' |
        grep -v '^\\unrestrict ' |
        grep -v '^-- Dumped from database version ' |
        grep -v '^-- Dumped by pg_dump version ' |
        grep -v '^SET statement_timeout = ' |
        grep -v '^SET lock_timeout = ' |
        grep -v '^SET idle_in_transaction_session_timeout = ' |
        grep -v '^SET client_encoding = ' |
        grep -v '^SET standard_conforming_strings = ' |
        grep -v "^SELECT pg_catalog.set_config('search_path'" |
        grep -v '^SET check_function_bodies = ' |
        grep -v '^SET xmloption = ' |
        grep -v '^SET client_min_messages = ' |
        grep -v '^SET row_security = ' |
        # Collapse the runs of blank lines the filters leave behind.
        cat -s
} > "$TMP_OUT"

if $CHECK_ONLY; then
    if diff -u "$OUTPUT_FILE" "$TMP_OUT" > /dev/null 2>&1; then
        echo "==> docs/schema.sql is up to date"
        exit 0
    fi
    echo >&2
    echo "error: docs/schema.sql does not match migrations/" >&2
    echo "       run 'make schema' and commit the result" >&2
    echo >&2
    diff -u "$OUTPUT_FILE" "$TMP_OUT" >&2 || true
    exit 1
fi

cp "$TMP_OUT" "$OUTPUT_FILE"
echo "==> wrote $OUTPUT_FILE"

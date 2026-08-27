#!/usr/bin/env bash
# =============================================================================
#  Copyright (c) 2026 Nyx Software, Inc.  All rights reserved.
#
#  CodeDistill
#
#  Property of Nyx Software, Inc., provided under a dual license: the GNU Affero General
#  Public License v3.0 (see the LICENSE file) and, separately, a commercial
#  license available from Nyx Software, Inc. Use outside the terms of one of those
#  licenses is prohibited.
#
#  SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-Nyx-Commercial
# =============================================================================

# Run the storage conformance suite against a real Postgres in a throwaway
# container, proving the SQL store behaves identically on Postgres and SQLite
# (Postgres backend, Phase 4 — docs/design/postgres-backend.md).
#
# The same tests that run against in-memory SQLite by default run against
# Postgres when $CODEDISTILL_TEST_PG is set; newTestStore isolates each test in
# its own schema. Usage:
#
#   scripts/test-pg.sh                 # full storage suite vs Postgres
#   scripts/test-pg.sh -run TestTodo   # forward extra `go test` args
#
# Honors $CONTAINER (podman|docker, default podman) and $PG_PORT (default 55432).
set -euo pipefail

CONTAINER="${CONTAINER:-podman}"
IMG="docker.io/library/postgres:16"
PORT="${PG_PORT:-55432}"
NAME="cd-pgconf-$$"

cleanup() { "$CONTAINER" rm -f "$NAME" >/dev/null 2>&1 || true; }
trap cleanup EXIT

echo "starting $IMG (via $CONTAINER)…"
"$CONTAINER" run -d --name "$NAME" \
  -e POSTGRES_PASSWORD=test -e POSTGRES_DB=cdtest -p "$PORT:5432" "$IMG" >/dev/null

for i in $(seq 1 60); do
  "$CONTAINER" exec "$NAME" pg_isready -U postgres -d cdtest >/dev/null 2>&1 && break
  sleep 1
done

export CODEDISTILL_TEST_PG="postgres://postgres:test@localhost:${PORT}/cdtest"
echo "running storage conformance suite against Postgres…"
go test -count=1 ./internal/storage/sqlite/ "$@"

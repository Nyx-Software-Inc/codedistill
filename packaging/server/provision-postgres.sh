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

# Idempotent first-boot provisioning for the codedistill-server package, run from
# the RPM %post / deb postinst. "Full hands-off": after this, the server has a
# working Postgres to talk to with zero manual DB steps (Rich's packaging
# decision, docs/design/postgres-backend.md).
#
# Strategy: peer/socket auth — no passwords, no pg_hba edits. A system user
# `codedistill` runs the service and connects over the unix socket; a matching
# Postgres role + database are created (peer auth maps OS user -> same-named
# role). The connection string is written to /etc/codedistill/codedistill.env
# ONLY if absent, so an operator's managed-Postgres override (Cloud SQL/RDS) is
# never clobbered.
#
# Safe to re-run: every step checks-before-acting.
set -euo pipefail

SVC_USER=codedistill
PG_ROLE=codedistill
PG_DB=codedistill
ENV_FILE=/etc/codedistill/codedistill.env
STATE_DIR=/var/lib/codedistill
BLOB_DIR="$STATE_DIR/blobs"
SOCKET_DIR=/var/run/postgresql

log() { echo "codedistill-server: $*"; }

# 1. System user that runs the service (and maps to the PG role via peer auth).
if ! id "$SVC_USER" >/dev/null 2>&1; then
  useradd --system --home-dir "$STATE_DIR" --shell /usr/sbin/nologin "$SVC_USER" 2>/dev/null \
    || useradd --system --home-dir "$STATE_DIR" --shell /sbin/nologin "$SVC_USER"
  log "created system user $SVC_USER"
fi
install -d -o "$SVC_USER" -g "$SVC_USER" -m 0750 "$STATE_DIR" "$BLOB_DIR"
install -d -m 0755 /etc/codedistill

# 2. Ensure a Postgres cluster exists and is running. RHEL/Fedora ship an
#    uninitialized data dir; Debian/Ubuntu auto-init a cluster on install.
if command -v postgresql-setup >/dev/null 2>&1; then
  if [ ! -s /var/lib/pgsql/data/PG_VERSION ]; then
    postgresql-setup --initdb 2>/dev/null || postgresql-setup initdb 2>/dev/null || true
    log "initialized Postgres data dir"
  fi
fi
if ! systemctl enable --now postgresql.service 2>/dev/null && ! systemctl enable --now postgresql 2>/dev/null; then
  log "WARNING: could not start postgresql via systemctl. Start it, then re-run $0"
fi

# Wait briefly for the server socket to appear.
for _ in $(seq 1 30); do
  [ -S "$SOCKET_DIR/.s.PGSQL.5432" ] && break
  sleep 1
done

# 3. Create role + database (idempotent), database owned by the role.
psql_super() { sudo -u postgres psql -v ON_ERROR_STOP=1 -tAc "$1"; }
if [ "$(psql_super "SELECT 1 FROM pg_roles WHERE rolname='$PG_ROLE'" 2>/dev/null || true)" != "1" ]; then
  psql_super "CREATE ROLE \"$PG_ROLE\" LOGIN" && log "created Postgres role $PG_ROLE"
fi
if [ "$(psql_super "SELECT 1 FROM pg_database WHERE datname='$PG_DB'" 2>/dev/null || true)" != "1" ]; then
  sudo -u postgres createdb -O "$PG_ROLE" "$PG_DB" && log "created database $PG_DB owned by $PG_ROLE"
fi

# 4. Write the env file the service reads — only if absent.
if [ ! -f "$ENV_FILE" ]; then
  cat > "$ENV_FILE" <<EOF
# CodeDistill server configuration.
# Point DATABASE_URL at a managed/remote Postgres (Cloud SQL/RDS) to override the
# local one provisioned on install. CODEDISTILL_ADDR binds the HTTP listener
# (front it with TLS + a reverse proxy for network access).
DATABASE_URL=postgres://$PG_ROLE@/$PG_DB?host=$SOCKET_DIR
CODEDISTILL_BLOBS=$BLOB_DIR
CODEDISTILL_ADDR=127.0.0.1:8080
CODEDISTILL_LICENSE=/etc/codedistill/codedistill.license
EOF
  chmod 0640 "$ENV_FILE"
  chown root:"$SVC_USER" "$ENV_FILE" 2>/dev/null || true
  log "wrote $ENV_FILE (local Postgres via peer/socket auth)"
else
  log "$ENV_FILE exists — leaving it (managed-Postgres override respected)"
fi

log "provisioning complete. Place a Pro/Enterprise license at /etc/codedistill/codedistill.license,"
log "then: systemctl enable --now codedistill-server"

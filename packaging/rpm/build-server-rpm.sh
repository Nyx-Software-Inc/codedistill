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

# Build the CodeDistill SERVER Fedora/RHEL RPM (multi-user, Postgres-backed).
# Mirrors build-rpm.sh but stages the system unit + the Postgres provisioner and
# builds codedistill-server.spec. Assumes ./codedistill already exists (run
# ./build.sh first). Output: ./dist/codedistill-server-<VERSION>-1.*.rpm
set -euo pipefail

REPO_ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
cd "$REPO_ROOT"

VERSION="$(cat VERSION)"
BIN="$REPO_ROOT/codedistill"
SPEC="$REPO_ROOT/packaging/rpm/codedistill-server.spec"
SERVICE="$REPO_ROOT/packaging/server/codedistill-server.service"
PROVISION="$REPO_ROOT/packaging/server/provision-postgres.sh"
PROVISION_OLLAMA="$REPO_ROOT/packaging/server/provision-ollama.sh"
PROFILE="$REPO_ROOT/packaging/profile.d/codedistill.sh"

if [ ! -x "$BIN" ]; then
  echo "==> codedistill binary missing at $BIN — running build.sh first"
  "$REPO_ROOT/build.sh"
fi

RPM_TOP="$(mktemp -d)"
trap 'rm -rf "$RPM_TOP"' EXIT
mkdir -p "$RPM_TOP/BUILD" "$RPM_TOP/RPMS" "$RPM_TOP/SOURCES" "$RPM_TOP/SPECS" "$RPM_TOP/SRPMS"

STAGE="$RPM_TOP/stage/codedistill-server-${VERSION}"
mkdir -p "$STAGE"
install -m 0755 "$BIN" "$STAGE/codedistill"
install -m 0644 "$SERVICE" "$STAGE/codedistill-server.service"
install -m 0755 "$PROVISION" "$STAGE/provision-postgres.sh"
install -m 0755 "$PROVISION_OLLAMA" "$STAGE/provision-ollama.sh"
install -m 0644 "$PROFILE" "$STAGE/codedistill-profile.sh"

cat > "$STAGE/README.txt" <<'EOF'
CodeDistill server — multi-user, Postgres-backed.

Installs to /opt/CodeDistill (on PATH for login shells via
/etc/profile.d/codedistill.sh — takes effect on next login).

Installing this package pulls in Postgres and provisions it automatically
(a `codedistill` role + database over the local socket), writes
/etc/codedistill/codedistill.env, and installs Ollama with a classification
model sized to the machine's RAM (<16GB -> qwen2.5:7b, >=16GB -> qwen2.5:14b;
recorded as CODEDISTILL_MODEL in the env file — edit to override).

To finish setup:
  1. Place a Pro/Enterprise license:
       /etc/codedistill/codedistill.license
  2. Start the service:
       systemctl enable --now codedistill-server
       systemctl status codedistill-server
       journalctl -u codedistill-server -f
  3. The UI/API listens on CODEDISTILL_ADDR (default 127.0.0.1:8080). For network
     access, edit /etc/codedistill/codedistill.env and front it with TLS + a
     reverse proxy (nginx/Caddy).

Use a managed Postgres (Cloud SQL/RDS) instead: set DATABASE_URL in
/etc/codedistill/codedistill.env before first start.

Air-gapped? The Ollama step skips gracefully offline — install Ollama manually
and pull the model named in /etc/codedistill/codedistill.env.

Re-run provisioning any time:
  /opt/CodeDistill/libexec/provision-postgres.sh
  /opt/CodeDistill/libexec/provision-ollama.sh
EOF

tar -C "$RPM_TOP/stage" -czf "$RPM_TOP/SOURCES/codedistill-server-${VERSION}.tar.gz" "codedistill-server-${VERSION}"
cp "$SPEC" "$RPM_TOP/SPECS/codedistill-server.spec"

CHANGELOG_DATE="$(LC_TIME=C date '+%a %b %d %Y')"
rpmbuild -bb \
  --define "_topdir $RPM_TOP" \
  --define "_version $VERSION" \
  --define "_changelog_date $CHANGELOG_DATE" \
  "$RPM_TOP/SPECS/codedistill-server.spec"

mkdir -p "$REPO_ROOT/dist"
shopt -s nullglob
RPMS=("$RPM_TOP/RPMS/x86_64/codedistill-server-${VERSION}"-*.rpm)
if [ ${#RPMS[@]} -eq 0 ]; then
  echo "rpmbuild produced no output under $RPM_TOP/RPMS/x86_64/" >&2
  exit 1
fi
for r in "${RPMS[@]}"; do
  cp "$r" "$REPO_ROOT/dist/"
  echo "==> $REPO_ROOT/dist/$(basename "$r")"
done

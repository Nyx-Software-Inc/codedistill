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

# Build the CodeDistill SERVER .deb (Debian 12+ / Ubuntu 22.04+) — multi-user,
# Postgres-backed. Mirrors build-server-rpm.sh: Depends on postgresql, installs
# the system unit + provisioner, and auto-provisions Postgres in postinst.
# Assumes ./codedistill exists (run ./build.sh first).
# Output: ./dist/codedistill-server_<VERSION>-1_amd64.deb
set -euo pipefail

REPO_ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
cd "$REPO_ROOT"

VERSION="$(cat VERSION)"
BIN="$REPO_ROOT/codedistill"
SERVICE="$REPO_ROOT/packaging/server/codedistill-server.service"
PROVISION="$REPO_ROOT/packaging/server/provision-postgres.sh"
PROVISION_OLLAMA="$REPO_ROOT/packaging/server/provision-ollama.sh"
PROFILE="$REPO_ROOT/packaging/profile.d/codedistill.sh"

if [ ! -x "$BIN" ]; then
  echo "==> codedistill binary missing at $BIN — running build.sh first"
  "$REPO_ROOT/build.sh"
fi
command -v dpkg-deb >/dev/null || { echo "dpkg-deb not found (dnf install -y dpkg)" >&2; exit 1; }

PKG="$(mktemp -d)"
trap 'rm -rf "$PKG"' EXIT

# ── payload ──────────────────────────────────────────────────────────
install -D -m 0755 "$BIN" "$PKG/opt/CodeDistill/bin/codedistill"
install -D -m 0644 "$SERVICE" "$PKG/usr/lib/systemd/system/codedistill-server.service"
install -D -m 0755 "$PROVISION" "$PKG/opt/CodeDistill/libexec/provision-postgres.sh"
install -D -m 0755 "$PROVISION_OLLAMA" "$PKG/opt/CodeDistill/libexec/provision-ollama.sh"
install -D -m 0644 "$PROFILE" "$PKG/etc/profile.d/codedistill.sh"
install -D -m 0644 /dev/stdin "$PKG/usr/share/doc/codedistill-server/README" <<'EOF'
CodeDistill server — multi-user, Postgres-backed.
Installs to /opt/CodeDistill (on PATH for login shells via /etc/profile.d).
Installing pulls in PostgreSQL and provisions it automatically (a codedistill
role + database over the local socket), writes /etc/codedistill/codedistill.env,
and installs Ollama with a model sized to the machine's RAM (<16GB ->
qwen2.5:7b, >=16GB -> qwen2.5:14b; CODEDISTILL_MODEL in the env file overrides).
Finish: place /etc/codedistill/codedistill.license, then
  systemctl enable --now codedistill-server
Listens on CODEDISTILL_ADDR (default 127.0.0.1:8080); front with TLS + a proxy.
Managed Postgres: set DATABASE_URL in /etc/codedistill/codedistill.env first.
Re-run provisioning: /opt/CodeDistill/libexec/provision-{postgres,ollama}.sh
EOF

# ── control + maintainer scripts ─────────────────────────────────────
SIZE_KB="$(du -sk "$PKG" | cut -f1)"
mkdir -p "$PKG/DEBIAN"
cat > "$PKG/DEBIAN/control" <<EOF
Package: codedistill-server
Version: ${VERSION}-1
Section: utils
Priority: optional
Architecture: amd64
Installed-Size: ${SIZE_KB}
Maintainer: CodeDistill <noreply@codedistill.dev>
Homepage: https://github.com/codedistill/codedistill
Depends: postgresql, adduser, systemd, curl
Conflicts: codedistill
Description: CodeDistill multi-user server (Postgres-backed)
 The multi-user, Postgres-backed deployment. Installing pulls in PostgreSQL and
 provisions it hands-off (a codedistill role + database over the local socket),
 then you place a Pro/Enterprise license and start the service.
 .
 For the single-user desktop install (SQLite), use the codedistill package.
EOF

# postinst: provision Postgres + Ollama, then enable the unit.
cat > "$PKG/DEBIAN/postinst" <<'EOF'
#!/bin/sh
set -e
if [ "$1" = "configure" ]; then
  /opt/CodeDistill/libexec/provision-postgres.sh || \
    echo "codedistill-server: provisioning incomplete — re-run /opt/CodeDistill/libexec/provision-postgres.sh"
  /opt/CodeDistill/libexec/provision-ollama.sh || \
    echo "codedistill-server: Ollama setup incomplete — re-run /opt/CodeDistill/libexec/provision-ollama.sh"
  systemctl daemon-reload >/dev/null 2>&1 || true
  echo "codedistill-server: place a license at /etc/codedistill/codedistill.license, then 'systemctl enable --now codedistill-server'"
fi
EOF
chmod 0755 "$PKG/DEBIAN/postinst"

# prerm/postrm: stop + clean the unit (leave data alone).
cat > "$PKG/DEBIAN/prerm" <<'EOF'
#!/bin/sh
set -e
if [ "$1" = "remove" ]; then
  systemctl disable --now codedistill-server >/dev/null 2>&1 || true
fi
EOF
chmod 0755 "$PKG/DEBIAN/prerm"
cat > "$PKG/DEBIAN/postrm" <<'EOF'
#!/bin/sh
set -e
systemctl daemon-reload >/dev/null 2>&1 || true
EOF
chmod 0755 "$PKG/DEBIAN/postrm"

# ── build ────────────────────────────────────────────────────────────
mkdir -p "$REPO_ROOT/dist"
OUT="$REPO_ROOT/dist/codedistill-server_${VERSION}-1_amd64.deb"
dpkg-deb --build --root-owner-group "$PKG" "$OUT"
echo "==> $OUT"

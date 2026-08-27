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

# Build the CodeDistill .deb (Debian 12+ / Ubuntu 22.04+).
#
# Mirrors packaging/rpm exactly: /opt/CodeDistill/bin/codedistill (+ PATH via
# /etc/profile.d), the user-scope systemd unit, and a doc README. Single-user
# desktop install; the multi-user hosted deployment uses the container image.
#
# Prereqs (Jenkins agent setup, works fine on Fedora):
#   dnf install -y dpkg
#
# Outputs:
#   ./dist/codedistill_<VERSION>-1_amd64.deb
#
# Assumes ./codedistill (the built binary) already exists at the repo
# root — run ./build.sh first if not. Idempotent: re-running
# overwrites the previous .deb at the same version.

set -euo pipefail

REPO_ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
cd "$REPO_ROOT"

VERSION="$(cat VERSION)"
BIN="$REPO_ROOT/codedistill"
# The systemd user unit is shared with the RPM — one canonical copy.
SERVICE="$REPO_ROOT/packaging/rpm/codedistill.service"
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
install -D -m 0644 "$SERVICE" "$PKG/usr/lib/systemd/user/codedistill.service"
install -D -m 0644 "$PROFILE" "$PKG/etc/profile.d/codedistill.sh"

mkdir -p "$PKG/usr/share/doc/codedistill"
cat > "$PKG/usr/share/doc/codedistill/README" <<'EOF'
CodeDistill — local-first scratchpad capture funnel.

Installs to /opt/CodeDistill (on PATH for login shells via
/etc/profile.d/codedistill.sh — takes effect on next login).

Quick start (run in foreground):
  codedistill init
  codedistill serve

Run as a user-scope systemd service:
  systemctl --user enable --now codedistill
  systemctl --user status codedistill
  journalctl --user -u codedistill -f

Open the UI at http://127.0.0.1:8080

Classification requires Ollama running on http://localhost:11434.
Install Ollama (https://ollama.com) and pull a model:
  ollama pull qwen2.5:7b
  ollama pull nomic-embed-text

Data lives under ~/.local/share/codedistill/ (DB + blobs).
Reset everything with `codedistill reset -force`.
EOF

# ── control ──────────────────────────────────────────────────────────
# Installed-Size is in KiB, per Debian policy.
SIZE_KB="$(du -sk "$PKG" | cut -f1)"
mkdir -p "$PKG/DEBIAN"
cat > "$PKG/DEBIAN/control" <<EOF
Package: codedistill
Version: ${VERSION}-1
Section: utils
Priority: optional
Architecture: amd64
Installed-Size: ${SIZE_KB}
Maintainer: CodeDistill <noreply@codedistill.dev>
Homepage: https://github.com/codedistill/codedistill
Description: Scratchpad to auto-classified todos, bugs, and knowledge
 CodeDistill turns the messy paste-everything scratchpad into a
 funnel of classified todos, bugs, and knowledge entries. Local-
 first single-binary install: bring your own Ollama for the LLM
 side; everything else is self-contained.
 .
 This package installs the single-user desktop binary. For the
 multi-user hosted deployment, use the container image.
EOF

# No Depends: line — the binary is pure Go (modernc.org/sqlite is
# in-process), so there are no shared-library deps to declare.
# No postinst — the systemd USER unit is discovered lazily per user,
# matching the RPM's empty %post.

# ── build ────────────────────────────────────────────────────────────
mkdir -p "$REPO_ROOT/dist"
OUT="$REPO_ROOT/dist/codedistill_${VERSION}-1_amd64.deb"
# --root-owner-group: payload owned by root:root without needing
# fakeroot — required when building as the unprivileged CI user.
dpkg-deb --build --root-owner-group "$PKG" "$OUT"
echo "==> $OUT"

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
install -D -m 0755 "$REPO_ROOT/packaging/provision-ollama.sh" \
  "$PKG/opt/CodeDistill/libexec/provision-ollama.sh"

mkdir -p "$PKG/usr/share/doc/codedistill"
# AGPL binaries must carry their terms. Debian has no control License:
# field — the license belongs in the package's copyright file, and the full
# text ships beside it (CE-review item 18).
install -m 0644 "$REPO_ROOT/LICENSE" "$PKG/usr/share/doc/codedistill/LICENSE"
cat > "$PKG/usr/share/doc/codedistill/copyright" <<'COPYRIGHT'
Format: https://www.debian.org/doc/packaging-manuals/copyright-format/1.0/
Upstream-Name: CodeDistill
Source: https://github.com/Nyx-Software-Inc/codedistill

Files: *
Copyright: 2026 Nyx Software, Inc.
License: AGPL-3.0-only or LicenseRef-Nyx-Commercial
 CodeDistill is dual-licensed. You may use it under the GNU Affero General
 Public License version 3 ONLY, or under a separate commercial license
 available from Nyx Software, Inc.
 .
 This program is distributed in the hope that it will be useful, but WITHOUT
 ANY WARRANTY; without even the implied warranty of MERCHANTABILITY or FITNESS
 FOR A PARTICULAR PURPOSE. See the GNU Affero General Public License for more
 details.
 .
 The complete text of the GNU AGPL v3 is shipped alongside this file as
 /usr/share/doc/codedistill/LICENSE, is available in
 /usr/share/common-licenses/AGPL-3 on Debian systems, and online at
 https://www.gnu.org/licenses/agpl-3.0.txt
 .
 Corresponding source for this binary is at https://github.com/Nyx-Software-Inc/codedistill
COPYRIGHT

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

Classification runs locally against Ollama on http://localhost:11434.
Install-time provisioning already installed Ollama and pulled a model
sized to this machine's RAM (<16GB -> qwen2.5:7b, >=16GB -> qwen2.5:14b)
plus nomic-embed-text for embeddings.

If the machine was offline at install time, re-run it any time — it is
idempotent and resumes:
  sudo /opt/CodeDistill/libexec/provision-ollama.sh

To change the model: edit CODEDISTILL_MODEL in /etc/codedistill/codedistill.env,
`ollama pull <model>`, then `systemctl --user restart codedistill`.

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
Homepage: https://codedistill.dev
Description: Scratchpad to auto-classified todos, bugs, and knowledge
 CodeDistill turns the messy paste-everything scratchpad into a
 funnel of classified todos, bugs, and knowledge entries. Local-
 first single-binary install: Ollama and a RAM-sized model are
 provisioned automatically at install time; everything else is
 self-contained. Nothing leaves the machine.
 .
 This package installs the single-user desktop binary. For the
 multi-user hosted deployment, use the container image.
EOF

# No Depends: line — the binary is pure Go (modernc.org/sqlite is
# in-process), so there are no shared-library deps to declare. Ollama is
# NOT a Depends either: it isn't in Debian/Ubuntu archives, so postinst
# provisions it directly, exactly as the RPM's %post does.

# postinst: provision Ollama + a RAM-sized model. The systemd USER unit is
# still discovered lazily per user, so there is nothing to enable here.
# Non-fatal by design — the script warns and exits 0 offline, so a
# provisioning hiccup never fails the install.
cat > "$PKG/DEBIAN/postinst" <<'POSTINST'
#!/bin/sh
set -e
if [ "$1" = "configure" ]; then
  /opt/CodeDistill/libexec/provision-ollama.sh || \
    echo "codedistill: Ollama setup incomplete — re-run /opt/CodeDistill/libexec/provision-ollama.sh"
fi
POSTINST
chmod 0755 "$PKG/DEBIAN/postinst"

# ── build ────────────────────────────────────────────────────────────
mkdir -p "$REPO_ROOT/dist"
OUT="$REPO_ROOT/dist/codedistill_${VERSION}-1_amd64.deb"
# --root-owner-group: payload owned by root:root without needing
# fakeroot — required when building as the unprivileged CI user.
dpkg-deb --build --root-owner-group "$PKG" "$OUT"
echo "==> $OUT"

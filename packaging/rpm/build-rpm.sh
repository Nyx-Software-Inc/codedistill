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

# Build the CodeDistill Fedora RPM.
#
# Prereqs (Jenkins agent setup):
#   dnf install -y rpm-build rpmdevtools
#
# Outputs:
#   ./dist/codedistill-<VERSION>-1.fc<NN>.x86_64.rpm
#
# This script assumes ./codedistill (the built binary) already
# exists at the repo root. Run `./build.sh` first if not.
#
# Idempotent — re-running overwrites the previous RPM at the same
# version.

set -euo pipefail

REPO_ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
cd "$REPO_ROOT"

VERSION="$(cat VERSION)"
BIN="$REPO_ROOT/codedistill"
SPEC="$REPO_ROOT/packaging/rpm/codedistill.spec"
SERVICE="$REPO_ROOT/packaging/rpm/codedistill.service"
PROFILE="$REPO_ROOT/packaging/profile.d/codedistill.sh"

if [ ! -x "$BIN" ]; then
  echo "==> codedistill binary missing at $BIN — running build.sh first"
  "$REPO_ROOT/build.sh"
fi

# rpmbuild expects a strict source tree layout under ~/rpmbuild (or
# whatever %_topdir overrides). Build everything inside a per-run
# tmpdir so successive runs (especially in CI) don't leak state.
RPM_TOP="$(mktemp -d)"
trap 'rm -rf "$RPM_TOP"' EXIT
mkdir -p "$RPM_TOP/BUILD" "$RPM_TOP/RPMS" "$RPM_TOP/SOURCES" "$RPM_TOP/SPECS" "$RPM_TOP/SRPMS"

# Assemble the source tarball that the spec's %prep step unpacks.
# Layout matches what the %install step expects to find in
# $RPM_SOURCE_DIR after %setup.
STAGE="$RPM_TOP/stage/codedistill-${VERSION}"
mkdir -p "$STAGE"
install -m 0755 "$BIN" "$STAGE/codedistill"
install -m 0644 "$SERVICE" "$STAGE/codedistill.service"
install -m 0644 "$PROFILE" "$STAGE/codedistill-profile.sh"

# README inside the RPM. Short pointer to the canonical docs +
# the systemd activation incantation.
cat > "$STAGE/README.txt" <<'EOF'
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

tar -C "$RPM_TOP/stage" -czf "$RPM_TOP/SOURCES/codedistill-${VERSION}.tar.gz" "codedistill-${VERSION}"

# Stage the spec — rpmbuild can also read it in place, but copying
# matches the standard layout + makes the build self-contained.
cp "$SPEC" "$RPM_TOP/SPECS/codedistill.spec"

# Build. %_topdir override keeps everything under the tmpdir.
# %_version is injected from VERSION so we don't hand-edit the
# spec on every release.
CHANGELOG_DATE="$(LC_TIME=C date '+%a %b %d %Y')"
rpmbuild -bb \
  --define "_topdir $RPM_TOP" \
  --define "_version $VERSION" \
  --define "_changelog_date $CHANGELOG_DATE" \
  "$RPM_TOP/SPECS/codedistill.spec"

mkdir -p "$REPO_ROOT/dist"
# rpmbuild output lands under RPMS/<arch>/ — copy whichever .rpm
# landed there into the project dist/.
shopt -s nullglob
RPMS=("$RPM_TOP/RPMS/x86_64/codedistill-${VERSION}"-*.rpm)
if [ ${#RPMS[@]} -eq 0 ]; then
  echo "rpmbuild produced no output under $RPM_TOP/RPMS/x86_64/" >&2
  exit 1
fi
for r in "${RPMS[@]}"; do
  cp "$r" "$REPO_ROOT/dist/"
  echo "==> $REPO_ROOT/dist/$(basename "$r")"
done

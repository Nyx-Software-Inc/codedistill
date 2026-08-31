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

# Builds the CodeDistill COMMUNITY EDITION single-binary:
#   1. Compiles the Svelte frontend into internal/webui/dist/
#      (skipped if the shipped dist/ is present and node is absent)
#   2. Builds the Go backend with -tags oss, which pins the
#      commercial features off at compile time
# Output: ./codedistill

set -euo pipefail
cd "$(dirname "$0")"

BIN="${BIN:-codedistill}"
VERSION="$(cat VERSION)"

if command -v npm > /dev/null && [ -d web ]; then
  echo "==> frontend"
  pushd web > /dev/null
  if [ ! -d node_modules ]; then
    echo "    node_modules missing — running npm ci"
    npm ci
  fi
  # NOTE: web/package.json's version is deliberately NOT synced here. Nothing
  # consumes it — the PWA manifest carries no version field, no source file reads
  # it, and the UI gets its version from the Go API (/api/v1/version, injected via
  # ldflags below). Writing it only dirtied a TRACKED file on every build, which
  # left the tree modified mid-release and shipped a stale version into the CE
  # mirror (git archive takes the COMMITTED value). CE-review item 17.
  npm run build
  popd > /dev/null
else
  echo "==> frontend: npm not found — using the shipped internal/webui/dist"
fi

echo "==> backend (community edition: -tags oss)"
SHA="$(git rev-parse --short HEAD 2>/dev/null || echo unknown)"
DATE="$(date -u +%Y-%m-%d)"
LDFLAGS="-X main.version=${VERSION}-ce -X main.gitSHA=${SHA} -X main.buildDate=${DATE}"
go build -trimpath -tags oss -ldflags "$LDFLAGS" -o "$BIN" ./cmd/codedistill

size=$(wc -c < "$BIN" | awk '{printf "%.1f MiB", $1/1024/1024}')
echo "==> done: ./$BIN ${VERSION}-ce ($SHA) ($size)"

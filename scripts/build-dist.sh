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

# Unified distribution builder for CodeDistill.
# Runs on any host with: bash, Go, npm, zip. macOS targets additionally
# need the .dmg tools (scripts/setup-dmg-tools.sh) + mkfs.hfsplus.
# Produces one artifact per (os, arch) target into ./dist/:
#   linux/windows → .zip     macOS → .dmg
#
# Targets supported:
#   linux/amd64    flat zip: codedistill, install.sh, README.txt
#   linux/arm64    same shape
#   darwin/arm64   .dmg containing CodeDistill.app (Apple Silicon)
#   darwin/amd64   .dmg containing CodeDistill.app (Intel Mac)
#   windows/amd64  flat zip: codedistill.exe, CodeDistill.bat, README.txt
#
# Artifact contents:
#   Linux (.zip):
#     codedistill           ← the binary
#     install.sh            ← one-shot Ollama + models + DB setup
#     README.txt            ← quick start
#   macOS (.dmg, HFS+ volume "CodeDistill <version>"):
#     CodeDistill.app/      ← double-click to launch (exec bits set)
#     install.sh
#     README.txt
#
# Cross-compile works without a C toolchain because modernc.org/sqlite
# is pure Go. The .dmg is built on Linux via libdmg-hfsplus (no Mac, no
# root). The macOS build is UNSIGNED — Gatekeeper requires the user to
# right-click → Open on first launch.
#
# Usage:
#   scripts/build-dist.sh                  # build for the host platform
#   scripts/build-dist.sh --target linux   # both linux arches (amd64+arm64)
#   scripts/build-dist.sh --target darwin  # both darwin arches (arm64+amd64)
#   scripts/build-dist.sh --target all     # everything
#   scripts/build-dist.sh --target linux --arch amd64
#   scripts/build-dist.sh -h | --help
#
# Idempotent — repeated runs overwrite the matching zips in dist/.

set -euo pipefail

# ──────────────────────────────────────────────────────────────────────
# Colors when stdout is a TTY.
# ──────────────────────────────────────────────────────────────────────
if [[ -t 1 ]]; then
  C_RESET=$'\033[0m'; C_BOLD=$'\033[1m'; C_DIM=$'\033[2m'
  C_GREEN=$'\033[32m'; C_CYAN=$'\033[36m'; C_RED=$'\033[31m'
else
  C_RESET=''; C_BOLD=''; C_DIM=''; C_GREEN=''; C_CYAN=''; C_RED=''
fi
step() { printf '%s==>%s %s\n' "$C_CYAN$C_BOLD" "$C_RESET" "$*"; }
info() { printf '    %s\n' "$*"; }
ok()   { printf '%s    ok:%s %s\n' "$C_GREEN" "$C_RESET" "$*"; }
die()  { printf '%serror:%s %s\n' "$C_RED$C_BOLD" "$C_RESET" "$*" >&2; exit 1; }

# ──────────────────────────────────────────────────────────────────────
# macOS .dmg tooling (see scripts/setup-dmg-tools.sh). Discovered lazily,
# only when a darwin target is requested. Override with DMG_TOOL /
# HFSPLUS_TOOL / MKFS_HFSPLUS.
# ──────────────────────────────────────────────────────────────────────
DMG_TOOL="${DMG_TOOL:-}"; HFSPLUS_TOOL="${HFSPLUS_TOOL:-}"; MKFS_HFSPLUS="${MKFS_HFSPLUS:-}"
find_dmg_tools() {
  if [[ -z "$DMG_TOOL" ]]; then
    DMG_TOOL="$(command -v dmg || true)"
    [[ -z "$DMG_TOOL" && -x "$HOME/.local/bin/dmg" ]] && DMG_TOOL="$HOME/.local/bin/dmg"
    [[ -z "$DMG_TOOL" && -x "$HOME/.local/src/libdmg-hfsplus/build/dmg/dmg" ]] && DMG_TOOL="$HOME/.local/src/libdmg-hfsplus/build/dmg/dmg"
  fi
  if [[ -z "$HFSPLUS_TOOL" ]]; then
    HFSPLUS_TOOL="$(command -v hfsplus || true)"
    [[ -z "$HFSPLUS_TOOL" && -x "$HOME/.local/bin/hfsplus" ]] && HFSPLUS_TOOL="$HOME/.local/bin/hfsplus"
    [[ -z "$HFSPLUS_TOOL" && -x "$HOME/.local/src/planetbeing-libdmg/build/hfs/hfsplus" ]] && HFSPLUS_TOOL="$HOME/.local/src/planetbeing-libdmg/build/hfs/hfsplus"
  fi
  [[ -z "$MKFS_HFSPLUS" ]] && MKFS_HFSPLUS="$(command -v mkfs.hfsplus || true)"
  local missing=()
  [[ -x "$DMG_TOOL"     ]] || missing+=("dmg (fanquake/libdmg-hfsplus)")
  [[ -x "$HFSPLUS_TOOL" ]] || missing+=("hfsplus (planetbeing/libdmg-hfsplus)")
  [[ -n "$MKFS_HFSPLUS" ]] || missing+=("mkfs.hfsplus (hfsprogs/hfsplus-tools)")
  if (( ${#missing[@]} )); then
    die "macOS .dmg build needs: ${missing[*]}
    Build them once with scripts/setup-dmg-tools.sh (no root), or set
    DMG_TOOL / HFSPLUS_TOOL / MKFS_HFSPLUS to explicit paths."
  fi
}

# ──────────────────────────────────────────────────────────────────────
# Args.
# ──────────────────────────────────────────────────────────────────────
TARGET=""
ARCH=""

usage() { sed -n '2,28p' "$0" | sed 's/^#\s\?//'; }

while [[ $# -gt 0 ]]; do
  case "$1" in
    --target) shift; TARGET="$1" ;;
    --arch)   shift; ARCH="$1"   ;;
    -h|--help) usage; exit 0 ;;
    *) die "unknown argument: $1 (try --help)" ;;
  esac
  shift
done

# ──────────────────────────────────────────────────────────────────────
# Resolve target list.
# ──────────────────────────────────────────────────────────────────────
HOST_OS="$(uname -s | tr '[:upper:]' '[:lower:]')"
case "$HOST_OS" in
  linux)  HOST_OS=linux ;;
  darwin) HOST_OS=darwin ;;
  *) die "unsupported host OS: $HOST_OS" ;;
esac
HOST_ARCH="$(uname -m)"
case "$HOST_ARCH" in
  x86_64|amd64) HOST_ARCH=amd64 ;;
  aarch64|arm64) HOST_ARCH=arm64 ;;
  *) die "unsupported host arch: $HOST_ARCH" ;;
esac

# A target list is an array of "os/arch" strings.
TARGETS=()
case "${TARGET:-host}" in
  host)
    TARGETS=("$HOST_OS/$HOST_ARCH")
    ;;
  linux)
    case "$ARCH" in
      "")    TARGETS=(linux/amd64 linux/arm64) ;;
      amd64) TARGETS=(linux/amd64) ;;
      arm64) TARGETS=(linux/arm64) ;;
      *) die "unsupported --arch for linux: $ARCH" ;;
    esac
    ;;
  darwin)
    case "$ARCH" in
      "")    TARGETS=(darwin/arm64 darwin/amd64) ;;
      arm64) TARGETS=(darwin/arm64) ;;
      amd64) TARGETS=(darwin/amd64) ;;
      *) die "unsupported --arch for darwin: $ARCH" ;;
    esac
    ;;
  windows)
    case "$ARCH" in
      ""|amd64) TARGETS=(windows/amd64) ;;
      *) die "unsupported --arch for windows: $ARCH" ;;
    esac
    ;;
  all)
    TARGETS=(linux/amd64 linux/arm64 darwin/arm64 darwin/amd64 windows/amd64)
    ;;
  *) die "unsupported --target: $TARGET" ;;
esac

# ──────────────────────────────────────────────────────────────────────
# Layout.
# ──────────────────────────────────────────────────────────────────────
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT"
DIST="$ROOT/dist"
mkdir -p "$DIST"

VERSION="$(cat VERSION)"
SHA="$(git rev-parse --short HEAD 2>/dev/null || echo unknown)"
DATE="$(date -u +%Y-%m-%d)"
LDFLAGS="-X main.version=${VERSION} -X main.gitSHA=${SHA} -X main.buildDate=${DATE}"

# ──────────────────────────────────────────────────────────────────────
# Frontend — build once, every target reuses the embedded dist.
# ──────────────────────────────────────────────────────────────────────
step "frontend (web/dist)"
pushd web > /dev/null
if [[ ! -d node_modules ]]; then
  info "node_modules missing — running npm install"
  npm install
fi
npm pkg set version="$VERSION" > /dev/null
npm run build
popd > /dev/null

# ──────────────────────────────────────────────────────────────────────
# Per-target build + package.
# ──────────────────────────────────────────────────────────────────────
package_linux() {
  local os="$1" arch="$2" bin="$3"
  local stage="$DIST/_stage-$os-$arch"
  local zip="$DIST/CodeDistill-$VERSION-$os-$arch.zip"
  rm -rf "$stage"
  mkdir -p "$stage"
  cp "$bin" "$stage/codedistill"
  chmod +x "$stage/codedistill"
  cp "$ROOT/scripts/install.sh" "$stage/install.sh"
  chmod +x "$stage/install.sh"
  cat > "$stage/README.txt" <<README
CodeDistill — $os/$arch build (v$VERSION)

QUICK START (recommended for fresh machines)
  bash install.sh
  It installs Ollama (if missing), pulls the two models CodeDistill
  needs (~5 GB total), and initializes a local database. About 5-10
  minutes on a decent connection.

  After that:
    ./codedistill serve
  Then open http://localhost:8080.

ALREADY HAVE OLLAMA RUNNING?
    ollama pull qwen2.5:7b
    ollama pull nomic-embed-text
    ./codedistill serve
README
  rm -f "$zip"
  ( cd "$stage" && zip -r -q "$zip" codedistill install.sh README.txt )
  rm -rf "$stage"
  ok "$zip"
}

package_darwin() {
  local os="$1" arch="$2" bin="$3"
  local stage="$DIST/_stage-$os-$arch"
  local app="$stage/CodeDistill.app"
  rm -rf "$stage"
  mkdir -p "$app/Contents/MacOS" "$app/Contents/Resources"

  cp "$bin" "$app/Contents/MacOS/codedistill-bin"
  chmod +x "$app/Contents/MacOS/codedistill-bin"

  cat > "$app/Contents/MacOS/CodeDistill" <<'LAUNCHER'
#!/bin/sh
# Launcher for the CodeDistill .app bundle.
#   - starts the Go server (which auto-launches Ollama if installed)
#   - opens the default browser to the local UI once it's listening
#   - keeps the server in the foreground so macOS treats the app as
#     "running" until Cmd-Q from the Dock.
DIR="$(cd "$(dirname "$0")" && pwd)"
SUPPORT="$HOME/Library/Application Support/CodeDistill"
mkdir -p "$SUPPORT"
DB="$SUPPORT/codedistill.db"
URL="http://localhost:8080"
( sleep 2 && open "$URL" ) &
exec "$DIR/codedistill-bin" -db "$DB" -addr ":8080" serve
LAUNCHER
  chmod +x "$app/Contents/MacOS/CodeDistill"

  cat > "$app/Contents/Info.plist" <<PLIST
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
    <key>CFBundleExecutable</key><string>CodeDistill</string>
    <key>CFBundleIdentifier</key><string>com.codedistill.app</string>
    <key>CFBundleName</key><string>CodeDistill</string>
    <key>CFBundleDisplayName</key><string>CodeDistill</string>
    <key>CFBundleVersion</key><string>$VERSION</string>
    <key>CFBundleShortVersionString</key><string>$VERSION</string>
    <key>CFBundlePackageType</key><string>APPL</string>
    <key>CFBundleSignature</key><string>????</string>
    <key>LSMinimumSystemVersion</key><string>11.0</string>
    <key>LSUIElement</key><false/>
    <key>NSHighResolutionCapable</key><true/>
    <key>NSHumanReadableCopyright</key><string>CodeDistill</string>
</dict>
</plist>
PLIST

  cp "$ROOT/scripts/install.sh" "$stage/install.sh"
  chmod +x "$stage/install.sh"

  cat > "$stage/README.txt" <<README
CodeDistill — macOS ($arch) build (v$VERSION)

INSTALL
  Drag CodeDistill.app to your Applications folder.

QUICK START (recommended for fresh machines)
  Open Terminal in this volume and run:
    bash install.sh
  Installs Ollama (if missing), pulls models, initializes the DB.
  Then double-click CodeDistill.app.

ALREADY HAVE OLLAMA?
  Skip install.sh — just double-click CodeDistill.app. You'll need:
    ollama pull qwen2.5:7b
    ollama pull nomic-embed-text

GATEKEEPER (UNSIGNED BUILD)
  Right-click CodeDistill.app → Open → Open on first launch to bypass
  the "developer cannot be verified" warning. Subsequent launches go
  straight through.
README

  # Finder metadata so the volume opens as a styled drag-to-install window —
  # icon view, CodeDistill.app on the left, Applications on the right — instead
  # of a plain folder listing (bug #76). Synthesized on Linux via ds_store;
  # without it the DMG still drag-installs, just in the default view.
  if command -v python3 >/dev/null 2>&1 && python3 -c 'import ds_store' 2>/dev/null; then
    python3 "$ROOT/scripts/make-dmg-dsstore.py" "$stage/.DS_Store"
  else
    echo "  warn: ds_store missing — DMG opens in default view (run scripts/setup-dmg-tools.sh)" >&2
  fi

  # ── Assemble a real UDIF .dmg on Linux (no Mac, no root) ──────────────
  # $stage holds exactly {CodeDistill.app, install.sh, README.txt}; that is
  # the DMG's contents. Pipeline: blank image → format HFS+ → populate →
  # fix exec bits → wrap as UDIF. See scripts/setup-dmg-tools.sh.
  local dmg="$DIST/CodeDistill-$VERSION-$os-$arch.dmg"
  local raw="$stage.hfs.img"
  local kb mb
  kb="$(du -sk "$stage" | cut -f1)"
  mb=$(( kb / 1024 + 32 ))            # staged size + 32 MiB slack
  rm -f "$raw" "$dmg"
  dd if=/dev/zero of="$raw" bs=1M count="$mb" status=none
  "$MKFS_HFSPLUS" -v "CodeDistill $VERSION" "$raw" >/dev/null
  "$HFSPLUS_TOOL" "$raw" addall "$stage" >/dev/null
  # addall does not carry over the executable bit, so re-set it explicitly on
  # the bundle's launcher + binary (otherwise macOS reports "permission denied").
  "$HFSPLUS_TOOL" "$raw" chmod 755 /CodeDistill.app/Contents/MacOS/CodeDistill
  "$HFSPLUS_TOOL" "$raw" chmod 755 /CodeDistill.app/Contents/MacOS/codedistill-bin
  "$HFSPLUS_TOOL" "$raw" chmod 755 /install.sh
  # Applications symlink → the DMG's drag-to-install target. The absolute target
  # resolves on the user's Mac; the build host has no /Applications (bug #76).
  "$HFSPLUS_TOOL" "$raw" symlink /Applications /Applications >/dev/null
  "$DMG_TOOL" "$raw" "$dmg" >/dev/null
  rm -f "$raw"
  rm -rf "$stage"
  ok "$dmg"
}

package_windows() {
  local os="$1" arch="$2" bin="$3"
  local stage="$DIST/_stage-$os-$arch"
  local zip="$DIST/CodeDistill-$VERSION-$os-$arch.zip"
  rm -rf "$stage"
  mkdir -p "$stage"
  cp "$bin" "$stage/codedistill.exe"

  # Double-click launcher: parity with the macOS .app. DB lands under
  # %LOCALAPPDATA%\CodeDistill; browser opens after a short delay so
  # the server is (usually) listening by then. CRLF line endings via
  # printf — some cmd.exe versions mis-parse LF-only batch files.
  printf '%s\r\n' \
    '@echo off' \
    'setlocal' \
    'set "DATA=%LOCALAPPDATA%\CodeDistill"' \
    'if not exist "%DATA%" mkdir "%DATA%"' \
    'start "" /b cmd /c "timeout /t 3 /nobreak >nul & start "" http://localhost:8080"' \
    '"%~dp0codedistill.exe" -db "%DATA%\codedistill.db" -addr :8080 serve' \
    > "$stage/CodeDistill.bat"

  cat > "$stage/README.txt" <<README
CodeDistill — Windows ($arch) build (v$VERSION)

QUICK START
  1. Install Ollama from https://ollama.com/download/windows
  2. In a terminal:
       ollama pull qwen2.5:7b
       ollama pull nomic-embed-text
  3. Double-click CodeDistill.bat (starts the server + opens the UI),
     or run from a terminal:
       codedistill.exe serve
     and open http://localhost:8080.

SMARTSCREEN (UNSIGNED BUILD)
  Windows may show "Windows protected your PC" on first launch.
  Click "More info" → "Run anyway". The build is unsigned; code
  signing is planned.

DATA
  Database + blobs live under %LOCALAPPDATA%\CodeDistill\ when
  launched via CodeDistill.bat (or next to the .exe when run
  directly with no -db flag).
README

  rm -f "$zip"
  ( cd "$stage" && zip -r -q "$zip" codedistill.exe CodeDistill.bat README.txt )
  rm -rf "$stage"
  ok "$zip"
}

# Fail fast if a darwin target is requested but the .dmg tools are absent,
# before spending time compiling binaries.
if printf '%s\n' "${TARGETS[@]}" | grep -q '^darwin/'; then
  find_dmg_tools
fi

for t in "${TARGETS[@]}"; do
  os="${t%/*}"
  arch="${t#*/}"
  step "build $os/$arch (v$VERSION $SHA)"
  bin="$DIST/codedistill-$os-$arch"
  # Windows binaries need the .exe suffix or nothing will run them.
  [[ "$os" == "windows" ]] && bin="$bin.exe"
  GOOS="$os" GOARCH="$arch" go build -trimpath -ldflags "$LDFLAGS" -o "$bin" ./cmd/codedistill
  size="$(wc -c < "$bin" | awk '{printf "%.1f MiB", $1/1024/1024}')"
  info "binary: $bin ($size)"
  case "$os" in
    linux)   package_linux   "$os" "$arch" "$bin" ;;
    darwin)  package_darwin  "$os" "$arch" "$bin" ;;
    windows) package_windows "$os" "$arch" "$bin" ;;
    *) die "no packager wired for $os" ;;
  esac
done

# ──────────────────────────────────────────────────────────────────────
# Summary.
# ──────────────────────────────────────────────────────────────────────
echo
printf '%sdone:%s\n' "$C_BOLD$C_GREEN" "$C_RESET"
for t in "${TARGETS[@]}"; do
  os="${t%/*}"; arch="${t#*/}"
  if [[ "$os" == "darwin" ]]; then
    art="$DIST/CodeDistill-$VERSION-$os-$arch.dmg"
  else
    art="$DIST/CodeDistill-$VERSION-$os-$arch.zip"
  fi
  size="$(wc -c < "$art" | awk '{printf "%.1f MiB", $1/1024/1024}')"
  printf '  %s%-24s%s %s\n' "$C_BOLD" "$os/$arch" "$C_RESET" "$art ($size)"
done

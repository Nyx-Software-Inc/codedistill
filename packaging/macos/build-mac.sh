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
#
# Runs on the macOS Jenkins agent. Takes the app bundles that
# scripts/build-dist.sh --target darwin --stage-only left under dist/mac/<arch>/
# (built on Linux), merges them into one universal CodeDistill.app, signs and
# notarises it when the Nyx Developer ID credentials are present, and wraps it
# in dist/CodeDistill-<version>-macos.dmg with hdiutil.
#
# Signing inputs (environment, from Jenkins credentials; all optional):
#   P12, P12PASS              Developer ID Application certificate (.p12) + password
#   NOTARY_KEY, NOTARY_KEY_ID, NOTARY_ISSUER
#                             App Store Connect API key (.p8), its id and issuer
# Without P12 the dmg is produced unsigned. Nothing here touches the login
# keychain: over SSH it is locked, so the certificate goes into a throwaway
# keychain and notarytool gets the API key directly.
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
cd "$ROOT"
DIST="$ROOT/dist"
VERSION="$(cat VERSION)"
say() { printf '\033[1;34m==>\033[0m %s\n' "$*"; }

# ── 1. One universal bundle from the per-architecture ones ─────────────────
arches=()
for a in arm64 amd64; do [ -d "$DIST/mac/$a/CodeDistill.app" ] && arches+=("$a"); done
[ ${#arches[@]} -gt 0 ] || { echo "no staged bundles under dist/mac/<arch>/ — run scripts/build-dist.sh --target darwin --stage-only first" >&2; exit 1; }
STAGE="$DIST/mac/universal"
rm -rf "$STAGE"
mkdir -p "$STAGE"
cp -R "$DIST/mac/${arches[0]}/." "$STAGE/"
APP="$STAGE/CodeDistill.app"
rm -f "$STAGE/README.txt"
if [ ${#arches[@]} -gt 1 ]; then
  say "universal binaries (${arches[*]})"
  for b in CodeDistill codedistill-bin; do
    inputs=()
    for a in "${arches[@]}"; do inputs+=("$DIST/mac/$a/CodeDistill.app/Contents/MacOS/$b"); done
    lipo -create "${inputs[@]}" -output "$APP/Contents/MacOS/$b"
    say "  $b: $(lipo -archs "$APP/Contents/MacOS/$b")"
  done
fi

# ── 2. Icon from the 512px PNG, when the tools are here ───────────────────
if [ -f CodeDistill-icon.png ] && command -v iconutil >/dev/null 2>&1; then
  ICONSET="$DIST/mac/CodeDistill.iconset"
  rm -rf "$ICONSET"; mkdir -p "$ICONSET"
  for sz in 16 32 128 256 512; do
    sips -z $sz $sz CodeDistill-icon.png --out "$ICONSET/icon_${sz}x${sz}.png" >/dev/null
    [ $sz -lt 512 ] && sips -z $((sz*2)) $((sz*2)) CodeDistill-icon.png --out "$ICONSET/icon_${sz}x${sz}@2x.png" >/dev/null
  done
  iconutil -c icns "$ICONSET" -o "$APP/Contents/Resources/CodeDistill.icns"
  rm -rf "$ICONSET"
  # Register the icon in the plist (the staged plist has no icon key).
  /usr/libexec/PlistBuddy -c "Delete :CFBundleIconFile" "$APP/Contents/Info.plist" >/dev/null 2>&1 || true
  /usr/libexec/PlistBuddy -c "Add :CFBundleIconFile string CodeDistill" "$APP/Contents/Info.plist"
fi

# ── 3. README for the volume ───────────────────────────────────────────────
SIGNED=no
[ -n "${P12:-}" ] && SIGNED=yes
{
  echo "CodeDistill — macOS build (v$VERSION, Apple Silicon and Intel)"
  echo
  echo "INSTALL"
  echo "  Drag CodeDistill.app to your Applications folder, then double-click it."
  echo "  The server starts and your browser opens http://localhost:8080."
  echo
  echo "QUICK START (fresh machines)"
  echo "  Open Terminal in this volume and run:  bash install.sh"
  echo "  Installs Ollama (if missing), pulls models, initializes the DB."
  echo
  echo "ALREADY HAVE OLLAMA?"
  echo "  Skip install.sh. You need:  ollama pull qwen2.5:7b  and  ollama pull nomic-embed-text"
  echo
  if [ "$SIGNED" = yes ]; then
    echo "This build is signed with a Nyx Software, Inc. Developer ID and notarized by Apple."
  else
    echo "GATEKEEPER (UNSIGNED BUILD)"
    echo "  Right-click CodeDistill.app → Open → Open on first launch, or approve it under"
    echo "  System Settings → Privacy & Security after the first blocked attempt."
  fi
  echo
  echo "DATA"
  echo "  Database + blobs live under ~/Library/Application Support/CodeDistill/."
} > "$STAGE/README.txt"

# ── 4. Sign ────────────────────────────────────────────────────────────────
KEYCHAIN=codedistill-build.keychain-db
IDENTITY=""
if [ "$SIGNED" = yes ]; then
  say "signing"
  echo "certificate file: $(stat -f %z "$P12") bytes, $(file -b "$P12")"
  WORK=$(mktemp -d)
  trap 'rm -rf "$WORK"; security delete-keychain "$KEYCHAIN" 2>/dev/null || true' EXIT
  # macOS's importer rejects some PKCS#12 encryption variants; re-encode with the system openssl.
  openssl pkcs12 -in "$P12" -passin env:P12PASS -nodes -out "$WORK/id.pem"
  openssl pkcs12 -export -in "$WORK/id.pem" -passout env:P12PASS -out "$WORK/id.p12"
  security delete-keychain "$KEYCHAIN" 2>/dev/null || true
  security create-keychain -p "" "$KEYCHAIN"
  security unlock-keychain -p "" "$KEYCHAIN"
  security set-keychain-settings "$KEYCHAIN"
  security import "$WORK/id.p12" -f pkcs12 -k "$KEYCHAIN" -P "$P12PASS" -T /usr/bin/codesign -T /usr/bin/security
  # Apple's Developer ID intermediate is missing without Xcode; fetch and import it.
  for ca in DeveloperIDG2CA DeveloperIDCA; do
    curl -fsSL -o "$WORK/$ca.cer" "https://www.apple.com/certificateauthority/$ca.cer" && \
      security import "$WORK/$ca.cer" -k "$KEYCHAIN" -T /usr/bin/codesign >/dev/null 2>&1 || true
  done
  security set-key-partition-list -S apple-tool:,apple:,codesign: -s -k "" "$KEYCHAIN" >/dev/null
  security list-keychains -d user -s "$KEYCHAIN" login.keychain-db
  IDENTITY=$(security find-identity -v -p codesigning "$KEYCHAIN" | awk -F'"' 'NR==1{print $2}')
  if [ -z "$IDENTITY" ]; then
    echo "No valid code-signing identity in the imported certificate:" >&2
    security find-identity -p codesigning "$KEYCHAIN" >&2
    exit 1
  fi
  say "signing as: $IDENTITY"
  # Helpers first, bundle last: codesign treats the main executable's path as
  # the bundle itself and refuses while any nested binary is still unsigned.
  for bin in "$APP"/Contents/MacOS/*; do
    [ "$(basename "$bin")" = CodeDistill ] && continue
    codesign --force --options runtime --timestamp --sign "$IDENTITY" --entitlements packaging/macos/entitlements.plist "$bin"
  done
  codesign --force --options runtime --timestamp --sign "$IDENTITY" --entitlements packaging/macos/entitlements.plist "$APP"
  codesign --verify --deep --strict "$APP"
fi

# ── 5. The disk image: app + install.sh + README + Applications shortcut ──
say "disk image"
ln -sfn /Applications "$STAGE/Applications"
DMG="$DIST/CodeDistill-$VERSION-macos.dmg"
rm -f "$DMG"
hdiutil create -volname "CodeDistill $VERSION" -srcfolder "$STAGE" -ov -format UDZO "$DMG" >/dev/null

# ── 6. Notarise the image (covers the app inside) and staple ───────────────
if [ "$SIGNED" = yes ]; then
  codesign --force --timestamp --sign "$IDENTITY" "$DMG"
  say "notarising (Apple usually answers within a few minutes)"
  xcrun notarytool submit "$DMG" --key "$NOTARY_KEY" --key-id "$NOTARY_KEY_ID" --issuer "$NOTARY_ISSUER" --wait
  xcrun stapler staple "$DMG"
  spctl -a -t open --context context:primary-signature -v "$DMG" || true
fi
say "-> $DMG ($(du -h "$DMG" | cut -f1))"

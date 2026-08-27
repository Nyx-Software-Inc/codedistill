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

# Builds the two userspace tools that build-dist.sh needs to produce a real
# macOS .dmg on Linux — no Mac, no root, no loopback mount:
#
#   dmg      — fanquake/libdmg-hfsplus : wraps a raw HFS+ image into a UDIF .dmg
#   hfsplus  — planetbeing/libdmg-hfsplus : populates an HFS+ image (addall/chmod)
#
# We take each from a different fork on purpose:
#   * fanquake's fork is the only one whose `dmg` converter builds against
#     OpenSSL 3 (the modern one); its CMake only builds `dmg`, not a populator.
#   * planetbeing's original still ships the `hfsplus` populator, but its `dmg`
#     target fails to compile on OpenSSL 3 (removed HMAC_CTX API) — so we build
#     ONLY its `hfsplus` target and ignore its `dmg`.
# mkfs.hfsplus (formatter) comes from the distro: hfsprogs / hfsplus-tools.
#
#   scripts/setup-dmg-tools.sh                 # installs to ~/.local/bin
#   DMG_TOOLS_BIN=/usr/local/bin scripts/setup-dmg-tools.sh   # system (needs write perm)
#
# build-dist.sh auto-discovers the results (PATH, ~/.local/bin, or the build
# dirs below); or point it explicitly with DMG_TOOL / HFSPLUS_TOOL.
#
# One-time per build host. Idempotent — re-run to rebuild.

set -euo pipefail

SRC="${DMG_TOOLS_SRC:-$HOME/.local/src}"
BIN="${DMG_TOOLS_BIN:-$HOME/.local/bin}"
mkdir -p "$SRC" "$BIN"

need() { command -v "$1" >/dev/null 2>&1 || { echo "missing build dep: $1" >&2; exit 1; }; }
need git; need cmake; need make; need cc

echo "==> fanquake/libdmg-hfsplus  (the 'dmg' UDIF converter)"
fq="$SRC/libdmg-hfsplus"
rm -rf "$fq"
git clone --depth 1 https://github.com/fanquake/libdmg-hfsplus "$fq" >/dev/null 2>&1
cmake -S "$fq" -B "$fq/build" >/dev/null
# Build the whole (tiny) tree: the executable target is `dmg-bin`, while
# `dmg` is just the static lib — this fork ships only the converter.
cmake --build "$fq/build" -j"$(nproc)" >/dev/null
install -m 0755 "$fq/build/dmg/dmg" "$BIN/dmg"
echo "    installed: $BIN/dmg"

echo "==> planetbeing/libdmg-hfsplus  (the 'hfsplus' populator)"
pb="$SRC/planetbeing-libdmg"
rm -rf "$pb"
git clone --depth 1 https://github.com/planetbeing/libdmg-hfsplus "$pb" >/dev/null 2>&1
# -DCMAKE_POLICY_VERSION_MINIMUM=3.5: the tree predates CMake 3.5's minimum
# policy floor and CMake 4 refuses it otherwise. Build only the hfsplus target
# so the OpenSSL-3-incompatible `dmg` target never compiles.
cmake -S "$pb" -B "$pb/build" -DCMAKE_POLICY_VERSION_MINIMUM=3.5 >/dev/null
cmake --build "$pb/build" --target hfsplus -j"$(nproc)" >/dev/null
install -m 0755 "$pb/build/hfs/hfsplus" "$BIN/hfsplus"
echo "    installed: $BIN/hfsplus"

echo "==> distro formatter (mkfs.hfsplus)"
if command -v mkfs.hfsplus >/dev/null 2>&1; then
  echo "    found: $(command -v mkfs.hfsplus)"
else
  echo "    NOT FOUND — install it:"
  echo "      Fedora/RHEL : sudo dnf install -y hfsplus-tools   (or hfsprogs)"
  echo "      Debian/Ubuntu: sudo apt-get install -y hfsprogs"
fi

echo "==> DMG Finder metadata (ds_store, pure-python)"
if command -v python3 >/dev/null 2>&1; then
  if python3 -c 'import ds_store' 2>/dev/null; then
    echo "    found: ds_store"
  elif python3 -m pip install --user --quiet ds_store 2>/dev/null; then
    echo "    installed: ds_store (styled drag-to-install DMG window, bug #76)"
  else
    echo "    NOT installed — 'pip install ds_store' failed; the DMG still"
    echo "    drag-installs but opens in the default view. Retry: pip install ds_store"
  fi
else
  echo "    python3 not found — skipping (DMG opens in the default view)"
fi

echo
echo "==> done. Tools in: $BIN"
case ":$PATH:" in
  *":$BIN:"*) : ;;
  *) echo "    NOTE: $BIN is not on PATH. Either add it, or export"
     echo "          DMG_TOOL=$BIN/dmg  HFSPLUS_TOOL=$BIN/hfsplus  before build-dist.sh." ;;
esac

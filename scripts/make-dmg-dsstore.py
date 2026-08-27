#!/usr/bin/env python3
# =============================================================================
#  Copyright (c) 2026 Nyx Software, Inc.  All rights reserved.
#
#  CodeDistill
#
#  Property of Nyx Software, Inc., provided under a dual license: the GNU Affero
#  General Public License v3.0 (see the LICENSE file) and, separately, a
#  commercial license available from Nyx Software, Inc. Use outside the terms of
#  one of those licenses is prohibited.
#
#  SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-Nyx-Commercial
# =============================================================================
"""Synthesize the .DS_Store Finder metadata for the CodeDistill macOS install DMG.

On a Mac, Finder writes this file when you arrange a DMG window. We build the DMG
on Linux (see build-dist.sh — no Mac, no root), so we generate the metadata with
the pure-python `ds_store` library instead. The result makes the volume open as a
drag-to-install window — icon view, CodeDistill.app on the left, the /Applications
shortcut on the right — rather than a plain folder listing (bug #76).

Usage: make-dmg-dsstore.py <output .DS_Store path>
"""
import sys

from ds_store import DSStore


def build(out: str) -> None:
    with DSStore.open(out, "w+") as d:
        d["."]["vSrn"] = ("long", 1)
        # Window: icon view, fixed bounds, chrome off — the installer look.
        d["."]["bwsp"] = {
            "WindowBounds": "{{300, 200}, {520, 400}}",
            "ShowStatusBar": False,
            "ShowTabView": False,
            "ShowToolbar": False,
            "ShowPathbar": False,
            "ShowSidebar": False,
        }
        d["."]["icvp"] = {
            "viewOptionsVersion": 1,
            "backgroundType": 1,  # 1 = solid color (a branded image is a later add)
            # On-brand dark background (~#1a1a1e), matching CodeDistill's theme.
            "backgroundColorRed": 0.102,
            "backgroundColorGreen": 0.102,
            "backgroundColorBlue": 0.118,
            "gridOffsetX": 0.0,
            "gridOffsetY": 0.0,
            "gridSpacing": 100.0,
            "arrangeBy": "none",
            "showIconPreview": True,
            "showItemInfo": False,
            "labelOnBottom": True,
            "textSize": 12.0,
            "iconSize": 96.0,
            "scrollPositionX": 0.0,
            "scrollPositionY": 0.0,
        }
        # Icon positions within the 520x400 window (title bar ~ y < 60). The
        # names must match the DMG's root entries exactly.
        d["CodeDistill.app"]["Iloc"] = (150, 170)
        d["Applications"]["Iloc"] = (370, 170)
        d["README.txt"]["Iloc"] = (150, 310)
        d["install.sh"]["Iloc"] = (370, 310)


if __name__ == "__main__":
    if len(sys.argv) != 2:
        sys.exit("usage: make-dmg-dsstore.py <output .DS_Store path>")
    build(sys.argv[1])

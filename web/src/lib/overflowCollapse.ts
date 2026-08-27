// =============================================================================
//  Copyright (c) 2026 Nyx Software, Inc.  All rights reserved.
//
//  CodeDistill
//
//  Property of Nyx Software, Inc., provided under a dual license: the GNU Affero General
//  Public License v3.0 (see the LICENSE file) and, separately, a commercial
//  license available from Nyx Software, Inc. Use outside the terms of one of those
//  licenses is prohibited.
//
//  SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-Nyx-Commercial
// =============================================================================

// Overflow-driven label collapse for button bars (header + canvas toolbar).
//
// Viewport media queries rot twice over: they don't track the bar's actual
// content (buttons get added), and they don't track the bar's actual WIDTH —
// the canvas toolbar lives in a pane narrowed by the drawer and the code
// canvas, so a 1920px viewport can hold a 900px toolbar (Slavko's bug: labels
// never collapsed, buttons piled up). Fonts render wider on some platforms
// too, moving the true overflow point per machine.
//
// This action measures the element itself: report tight the moment content
// overflows; report roomy again only when the width the expanded content
// needed (plus a buffer) fits — hysteresis, so the boundary can't flap.
// All state lives in the closure: no reactive reads, so no effect-dependency
// traps (the v0.20.5 header regression).
export function collapseOnOverflow(node: HTMLElement, onTight: (tight: boolean) => void) {
  let tight = false;
  let neededWidth = 0;

  const check = () => {
    if (!tight && node.scrollWidth > node.clientWidth + 1) {
      neededWidth = node.scrollWidth;
      tight = true;
      onTight(true);
    } else if (tight && neededWidth > 0 && node.clientWidth >= neededWidth + 24) {
      tight = false;
      onTight(false);
    }
  };

  const ro = new ResizeObserver(check);
  const observeChildren = () => {
    for (const c of node.children) ro.observe(c);
  };
  // The bar is width-constrained by its container, so its own box never
  // resizes when content changes — observe children for that, and re-wire
  // when buttons mount/unmount (license state, view switches).
  const mo = new MutationObserver(() => {
    observeChildren();
    check();
  });

  check();
  ro.observe(node);
  observeChildren();
  mo.observe(node, { childList: true });

  return {
    destroy() {
      ro.disconnect();
      mo.disconnect();
    },
  };
}

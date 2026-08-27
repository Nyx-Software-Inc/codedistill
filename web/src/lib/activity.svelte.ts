/* =============================================================================
 *  Copyright (c) 2026 Nyx Software, Inc.  All rights reserved.
 *
 *  CodeDistill
 *
 *  Property of Nyx Software, Inc., provided under a dual license: the GNU Affero General
 *  Public License v3.0 (see the LICENSE file) and, separately, a commercial
 *  license available from Nyx Software, Inc. Use outside the terms of one of those
 *  licenses is prohibited.
 *
 *  SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-Nyx-Commercial
 * ============================================================================= */

import { untrack } from 'svelte';

// Global in-flight request tracker (backlog item #20). Every API call passes
// through here, so a single global indicator can give immediate "something's
// happening" feedback for any async action — no per-button wiring needed.
//
// CRITICAL: begin()/end() mutate `count` via untrack(). Without it, `count++`
// (a read+write of $state) called synchronously inside an $effect — which every
// API helper is, when invoked from an effect — would make that effect read AND
// write `count`, an infinite effect loop (effect_update_depth_exceeded). untrack
// keeps the *read* out of the caller's dependency graph; the write still
// notifies the indicator that reads `busy`.
let count = $state(0);

// Minimum time the indicator stays up once shown, so a fast request doesn't
// flash it for 50ms (a flicker the eye reads as "did anything happen?"). 700ms
// is long enough to perceive, short enough to never feel laggy — deliberately
// NOT the literal "3 seconds" the acceptance criterion proposed, which would
// make every quick action feel sluggish (UC-35).
const MIN_VISIBLE_MS = 700;
let linger = $state(false); // latched true while inside the minimum-display window
let shownAt = 0;
let lingerTimer: ReturnType<typeof setTimeout> | undefined;

export const activity = {
  begin() {
    untrack(() => {
      if (count === 0) {
        shownAt = Date.now();
        linger = true;
        clearTimeout(lingerTimer);
      }
      count++;
    });
  },
  end() {
    untrack(() => {
      if (count > 0) count--;
      if (count === 0) {
        const wait = Math.max(0, MIN_VISIBLE_MS - (Date.now() - shownAt));
        clearTimeout(lingerTimer);
        lingerTimer = setTimeout(() => {
          linger = false;
        }, wait);
      }
    });
  },
  // True while requests are in flight OR within the minimum-display window after
  // the last one finished. Drives both the activity bar and the "Updating…" pill.
  get busy() {
    return count > 0 || linger;
  },
};

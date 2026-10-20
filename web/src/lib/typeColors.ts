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

import { loadUserObject, saveUserObject } from './userSettings';

// Per-type card colour (UC-18). ON by default as of the canvas-UI work: colour
// now REPLACES the type word in the card header, which only works if it is
// there to begin with. A user who sets every type to one colour has chosen to
// give that up.
//
// Two carriers, because one was not enough:
//
//   stripe — a full-strength bar down the card's left edge. The PRIMARY signal.
//            Four hues are hard to tell apart at a 30% blend and trivial at
//            full saturation, so the stripe is what actually makes the colour
//            readable — it is not merely an accessibility extra.
//   tint   — the ~30% body blend (the original UC-18 behaviour), now ambient
//            reinforcement rather than the only cue.
//
// `indicator` makes that configurable, including a 'label' mode that puts the
// type word back for anyone who cannot use colour at all. Roughly 8% of men
// cannot separate the red/violet pair, and colour as the sole encoding would
// simply fail for them.

export type TypeIndicator = 'both' | 'stripe' | 'tint' | 'label';

export interface TypeColorsConfig {
  enabled: boolean;
  /** How a card states its type. Default 'both'. */
  indicator?: TypeIndicator;
  colors: Record<string, string>;
}

export const TYPE_COLORS_KEY = 'canvas.type_colors';

// Re-picked for this work. The old defaults could not carry the type on their
// own, in two ways that only mattered once the type word went away:
//
//   todo #3a6890 and kb #4080b8 were both blue — indistinguishable at a 30%
//   blend on a dark card.
//
//   use_case was #d4a54d and bug was #b04545 — the EXACT hexes of the due-date
//   header bar (.bar.due-warn and .bar.due-urgent). Amber meant both "use case"
//   and "due soon"; red meant both "bug" and "overdue".
//
// Now four separated hues: blue, red, green, violet. Due-date colouring keeps
// amber/red because urgency conventions should not move, and it paints the
// HEADER BAR while type paints the stripe and body — different surfaces. A bug
// that is also overdue shows a red stripe and a red bar, which is honest: both
// are telling you to look.
export const DEFAULT_TYPE_COLORS: TypeColorsConfig = {
  enabled: true,
  indicator: 'both',
  colors: {
    todo: '#3f7fb0',
    bug: '#c04f4f',
    kb: '#4f9470',
    use_case: '#7a62c0',
    unclassified: '#5a5a5a',
  },
};

/** indicatorOf reads the mode with a default, so configs stored before this
 *  field existed behave like 'both' rather than like nothing. */
export function indicatorOf(cfg: TypeColorsConfig | undefined): TypeIndicator {
  return cfg?.indicator ?? 'both';
}

export const showsStripe = (cfg: TypeColorsConfig | undefined): boolean =>
  !!cfg?.enabled && (indicatorOf(cfg) === 'both' || indicatorOf(cfg) === 'stripe');

export const showsTint = (cfg: TypeColorsConfig | undefined): boolean =>
  !!cfg?.enabled && (indicatorOf(cfg) === 'both' || indicatorOf(cfg) === 'tint');

/** The type WORD is shown when colour is off entirely, or when the user asked
 *  for the label. Otherwise the stripe and tint say it. */
export const showsTypeLabel = (cfg: TypeColorsConfig | undefined): boolean =>
  !cfg?.enabled || indicatorOf(cfg) === 'label';

export const GROUP_TINT = '#5a5a5a'; // fixed, not configurable

export const loadTypeColors = () =>
  loadUserObject<TypeColorsConfig>(TYPE_COLORS_KEY, DEFAULT_TYPE_COLORS);

export const saveTypeColors = (cfg: TypeColorsConfig) =>
  saveUserObject(TYPE_COLORS_KEY, cfg);

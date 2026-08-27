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

// Per-type canvas card background colors (UC-18). Off by default; the
// chosen color renders as a ~30% tint blended with the theme's card
// background (color-mix) so text stays readable in both themes.
// Groups get a fixed gray tint when the feature is on.

export interface TypeColorsConfig {
  enabled: boolean;
  colors: Record<string, string>;
}

export const TYPE_COLORS_KEY = 'canvas.type_colors';

// Defaults drawn from the app's existing type accents.
export const DEFAULT_TYPE_COLORS: TypeColorsConfig = {
  enabled: false,
  colors: {
    todo: '#3a6890',
    bug: '#b04545',
    kb: '#4080b8',
    use_case: '#d4a54d',
    unclassified: '#5a5a5a',
  },
};

export const GROUP_TINT = '#5a5a5a'; // fixed, not configurable

export const loadTypeColors = () =>
  loadUserObject<TypeColorsConfig>(TYPE_COLORS_KEY, DEFAULT_TYPE_COLORS);

export const saveTypeColors = (cfg: TypeColorsConfig) =>
  saveUserObject(TYPE_COLORS_KEY, cfg);

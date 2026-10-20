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

import { describe, expect, it } from 'vitest';
import {
  DEFAULT_TYPE_COLORS, indicatorOf, showsStripe, showsTint, showsTypeLabel,
  type TypeColorsConfig,
} from './typeColors';

const cfg = (over: Partial<TypeColorsConfig> = {}): TypeColorsConfig => ({
  ...DEFAULT_TYPE_COLORS,
  ...over,
});

describe('the default palette', () => {
  it('is on by default, because color now replaces the type word', () => {
    // A card header that prints no type AND no color would state nothing.
    expect(DEFAULT_TYPE_COLORS.enabled).toBe(true);
    expect(DEFAULT_TYPE_COLORS.indicator).toBe('both');
  });

  it('gives every type a distinct hue', () => {
    const { todo, bug, kb, use_case } = DEFAULT_TYPE_COLORS.colors;
    const hues = [todo, bug, kb, use_case];
    // todo and kb were BOTH blue before this change (#3a6890 / #4080b8), which
    // is unreadable at a 30% blend once the word is gone.
    expect(new Set(hues).size).toBe(4);
  });

  it('does not reuse either due-date bar color', () => {
    // The sharp edge of the old palette: use_case was #d4a54d and bug was
    // #b04545 — the exact hexes of .bar.due-warn and .bar.due-urgent. Amber
    // meant "use case" AND "due soon"; red meant "bug" AND "overdue".
    const dueWarn = '#d4a54d';
    const dueUrgent = '#b04545';
    const picked = Object.values(DEFAULT_TYPE_COLORS.colors).map((c) => c.toLowerCase());
    expect(picked).not.toContain(dueWarn);
    expect(picked).not.toContain(dueUrgent);
  });
});

describe('what a card shows for its type', () => {
  it('shows stripe and tint, and no word, by default', () => {
    const c = cfg();
    expect(showsStripe(c)).toBe(true);
    expect(showsTint(c)).toBe(true);
    expect(showsTypeLabel(c)).toBe(false);
  });

  it('puts the word back when color is switched off entirely', () => {
    const c = cfg({ enabled: false });
    expect(showsStripe(c)).toBe(false);
    expect(showsTint(c)).toBe(false);
    // Otherwise the card would state its type nowhere at all.
    expect(showsTypeLabel(c)).toBe(true);
  });

  it('label mode keeps the word and drops both color carriers', () => {
    const c = cfg({ indicator: 'label' });
    expect(showsTypeLabel(c)).toBe(true);
    expect(showsStripe(c)).toBe(false);
    expect(showsTint(c)).toBe(false);
  });

  it('stripe-only and tint-only each carry exactly one', () => {
    expect(showsStripe(cfg({ indicator: 'stripe' }))).toBe(true);
    expect(showsTint(cfg({ indicator: 'stripe' }))).toBe(false);

    expect(showsTint(cfg({ indicator: 'tint' }))).toBe(true);
    expect(showsStripe(cfg({ indicator: 'tint' }))).toBe(false);

    // Neither hides the type, so neither needs the word.
    expect(showsTypeLabel(cfg({ indicator: 'stripe' }))).toBe(false);
    expect(showsTypeLabel(cfg({ indicator: 'tint' }))).toBe(false);
  });

  it('treats a config stored before indicator existed as both', () => {
    // Settings outlive the code that wrote them: anyone who had UC-18 on gets
    // the stripe too, rather than silently losing the type cue.
    const legacy = { enabled: true, colors: DEFAULT_TYPE_COLORS.colors } as TypeColorsConfig;
    expect(indicatorOf(legacy)).toBe('both');
    expect(showsStripe(legacy)).toBe(true);
    expect(showsTypeLabel(legacy)).toBe(false);
  });

  it('survives an undefined config', () => {
    // The prop is optional; a card must still render.
    expect(showsStripe(undefined)).toBe(false);
    expect(showsTint(undefined)).toBe(false);
    expect(showsTypeLabel(undefined)).toBe(true);
  });
});

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
import { parseShortcut, matchesShortcut, formatShortcut, firesWhileTyping, isBrowserOwned } from './shortcuts';

const key = (k: string, m: Partial<{ ctrl: boolean; alt: boolean; shift: boolean; meta: boolean }> = {}) => ({
  key: k, ctrlKey: !!m.ctrl, altKey: !!m.alt, shiftKey: !!m.shift, metaKey: !!m.meta,
});

describe('parseShortcut', () => {
  it('reads modifiers in any order and lower-cases a letter key', () => {
    expect(parseShortcut('Shift+Mod+F')).toEqual({ mod: true, ctrl: false, alt: false, shift: true, meta: false, key: 'f' });
  });
  it('keeps named keys as KeyboardEvent spells them', () => {
    expect(parseShortcut('F1')?.key).toBe('F1');
    expect(parseShortcut('Mod+,')?.key).toBe(',');
  });
  it('rejects nonsense', () => {
    expect(parseShortcut('')).toBeNull();
    expect(parseShortcut('Mod+Shift')).toBeNull();
    expect(parseShortcut('A+B')).toBeNull();
  });
});

describe('matchesShortcut', () => {
  const sc = parseShortcut('Mod+Shift+F')!;
  it('Mod is Ctrl off a Mac and Cmd on one', () => {
    expect(matchesShortcut(sc, key('F', { ctrl: true, shift: true }), false)).toBe(true);
    expect(matchesShortcut(sc, key('F', { meta: true, shift: true }), true)).toBe(true);
    expect(matchesShortcut(sc, key('F', { meta: true, shift: true }), false)).toBe(false);
    expect(matchesShortcut(sc, key('F', { ctrl: true, shift: true }), true)).toBe(false);
  });
  it('is exact about modifiers: Mod+F is not Mod+Shift+F', () => {
    expect(matchesShortcut(parseShortcut('Mod+F')!, key('F', { ctrl: true, shift: true }), false)).toBe(false);
    expect(matchesShortcut(sc, key('f', { ctrl: true }), false)).toBe(false);
  });
  it('ignores the case Shift puts on a letter', () => {
    expect(matchesShortcut(sc, key('f', { ctrl: true, shift: true }), false)).toBe(true);
  });
  it('matches function keys with nothing held', () => {
    expect(matchesShortcut(parseShortcut('F1')!, key('F1'), false)).toBe(true);
    expect(matchesShortcut(parseShortcut('F1')!, key('F1', { ctrl: true }), false)).toBe(false);
  });
});

describe('formatShortcut', () => {
  it('renders platform conventions', () => {
    const sc = parseShortcut('Mod+Shift+F')!;
    expect(formatShortcut(sc, false)).toBe('Ctrl+Shift+F');
    expect(formatShortcut(sc, true)).toBe('⇧⌘F');
    expect(formatShortcut(parseShortcut('Mod+,')!, true)).toBe('⌘,');
    expect(formatShortcut(parseShortcut('F1')!, false)).toBe('F1');
  });
});

describe('firesWhileTyping', () => {
  it('lets only Mod+Shift chords and function keys through a text field', () => {
    expect(firesWhileTyping(parseShortcut('Mod+Shift+F')!)).toBe(true);
    expect(firesWhileTyping(parseShortcut('F1')!)).toBe(true);
    expect(firesWhileTyping(parseShortcut('Mod+B')!)).toBe(false);
    expect(firesWhileTyping(parseShortcut('Mod+,')!)).toBe(false);
    expect(firesWhileTyping(parseShortcut('Shift+F')!)).toBe(false);
  });
});

describe('isBrowserOwned', () => {
  it('refuses reload, devtools and tab chords, in either modifier order', () => {
    expect(isBrowserOwned(parseShortcut('Mod+Shift+R')!)).toBe(true);
    expect(isBrowserOwned(parseShortcut('Shift+Mod+r')!)).toBe(true);
    expect(isBrowserOwned(parseShortcut('F12')!)).toBe(true);
    expect(isBrowserOwned(parseShortcut('Mod+W')!)).toBe(true);
  });
  it('leaves app chords alone', () => {
    expect(isBrowserOwned(parseShortcut('Mod+Shift+E')!)).toBe(false);
    expect(isBrowserOwned(parseShortcut('Mod+Shift+F')!)).toBe(false);
    expect(isBrowserOwned(parseShortcut('Mod+B')!)).toBe(false);
    expect(isBrowserOwned(parseShortcut('F1')!)).toBe(false);
    expect(isBrowserOwned(parseShortcut('Mod+,')!)).toBe(false);
  });
});

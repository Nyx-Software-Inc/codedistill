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

// Keyboard shortcuts: the notation, matching and display.
//
// PURE. No DOM listeners, no runes. The listener that feeds keystrokes into
// this lives in commands.svelte.ts; everything that can be subtly wrong about
// a shortcut (does Mod+Shift+F match Ctrl+Shift+F with the modifiers in a
// different order? does it fire inside a textarea?) is decided here where a
// test can hold it still.
//
// Notation: '+'-joined tokens, case-insensitive. 'Mod' is Cmd on macOS and
// Ctrl elsewhere — the one portable token, so a command declares a shortcut
// once and it reads naturally on every platform. 'Ctrl', 'Alt', 'Shift' and
// 'Meta' name the literal keys. The last token is the key as KeyboardEvent.key
// spells it ('F', ',', 'F1', 'Escape'); letters are matched case-insensitively
// because Shift changes what .key reports.

export interface Shortcut {
  mod: boolean;
  ctrl: boolean;
  alt: boolean;
  shift: boolean;
  meta: boolean;
  key: string;
}

/** parseShortcut turns 'Mod+Shift+F' into a Shortcut. Returns null when the
 *  string names no key, so a typo in a command definition is a null rather
 *  than a shortcut that silently matches nothing. */
export function parseShortcut(s: string): Shortcut | null {
  const parts = s.split('+').map((p) => p.trim()).filter(Boolean);
  if (parts.length === 0) return null;
  const sc: Shortcut = { mod: false, ctrl: false, alt: false, shift: false, meta: false, key: '' };
  for (const p of parts) {
    switch (p.toLowerCase()) {
      case 'mod': case 'cmdorctrl': sc.mod = true; break;
      case 'ctrl': case 'control': sc.ctrl = true; break;
      case 'alt': case 'option': sc.alt = true; break;
      case 'shift': sc.shift = true; break;
      case 'meta': case 'cmd': case 'command': sc.meta = true; break;
      default:
        if (sc.key) return null; // two non-modifier tokens
        sc.key = p.length === 1 ? p.toLowerCase() : p;
    }
  }
  return sc.key ? sc : null;
}

/** The subset of KeyboardEvent a match needs. A plain object so tests need
 *  no DOM. */
export interface KeyLike {
  key: string;
  ctrlKey: boolean;
  altKey: boolean;
  shiftKey: boolean;
  metaKey: boolean;
}

/** matchesShortcut: does this keystroke fire this shortcut on this platform?
 *  Exact modifier match — Mod+F does not fire on Mod+Shift+F, because that
 *  is how a user means to reach a different command. */
export function matchesShortcut(sc: Shortcut, e: KeyLike, mac: boolean): boolean {
  const wantCtrl = sc.ctrl || (sc.mod && !mac);
  const wantMeta = sc.meta || (sc.mod && mac);
  if (e.ctrlKey !== wantCtrl) return false;
  if (e.metaKey !== wantMeta) return false;
  if (e.altKey !== sc.alt) return false;
  if (e.shiftKey !== sc.shift) return false;
  const k = e.key.length === 1 ? e.key.toLowerCase() : e.key;
  return k === sc.key;
}

/** formatShortcut renders a shortcut the way the platform's users read it:
 *  '⌘⇧F' on a Mac, 'Ctrl+Shift+F' elsewhere. Order follows each platform's
 *  convention (Apple: Ctrl, Alt, Shift, Cmd). */
export function formatShortcut(sc: Shortcut, mac: boolean): string {
  const key = sc.key.length === 1 ? sc.key.toUpperCase() : sc.key;
  if (mac) {
    let out = '';
    if (sc.ctrl) out += '⌃';
    if (sc.alt) out += '⌥';
    if (sc.shift) out += '⇧';
    if (sc.mod || sc.meta) out += '⌘';
    return out + key;
  }
  const parts: string[] = [];
  if (sc.mod || sc.ctrl) parts.push('Ctrl');
  if (sc.alt) parts.push('Alt');
  if (sc.shift) parts.push('Shift');
  if (sc.meta) parts.push('Meta');
  parts.push(key);
  return parts.join('+');
}

/** firesWhileTyping: should this shortcut fire when the focus is in a text
 *  field? Only chords a text editor would never want — Mod+Shift+key and the
 *  function keys. Mod+B inside the markdown editor means bold, not "toggle the
 *  drawer", and a bare letter is just typing. */
export function firesWhileTyping(sc: Shortcut): boolean {
  if (/^F\d{1,2}$/.test(sc.key)) return true;
  return (sc.mod || sc.ctrl || sc.meta) && sc.shift;
}

/** isEditableTarget: is the keystroke going into something that takes text? */
export function isEditableTarget(t: EventTarget | null): boolean {
  if (!t || !(t instanceof HTMLElement)) return false;
  const tag = t.tagName;
  if (tag === 'INPUT' || tag === 'TEXTAREA' || tag === 'SELECT') return true;
  return t.isContentEditable;
}

/** isMacPlatform reads the platform once; exported so the display helpers and
 *  the matcher agree on what 'Mod' means. Tests pass the flag explicitly. */
export function isMacPlatform(): boolean {
  if (typeof navigator === 'undefined') return false;
  const p = (navigator as { userAgentData?: { platform?: string } }).userAgentData?.platform
    ?? navigator.platform ?? '';
  return /mac|iphone|ipad/i.test(p);
}

// Chords the browser owns. Binding one of these would swallow reload,
// devtools, tab management or the address bar — Mod+Shift+R once toggled a
// pane and nobody could hard-reload. Kept portable ('Mod'): the set is the
// union across Chromium and Firefox on every platform, so a chord that is
// free on one is still refused if another browser owns it.
const BROWSER_OWNED = [
  'Mod+R', 'Mod+Shift+R', 'F5', 'Mod+F5',
  'Mod+Shift+I', 'Mod+Shift+J', 'Mod+Shift+C', 'Mod+Shift+K', 'Mod+Shift+M', 'F12',
  'Mod+T', 'Mod+W', 'Mod+N', 'Mod+Shift+T', 'Mod+Shift+W', 'Mod+Shift+N', 'Mod+Shift+P',
  'Mod+Tab', 'Mod+Shift+Tab', 'Mod+Shift+A', 'Mod+Shift+Q', 'Mod+Q',
  'Mod+L', 'Mod+E', 'Mod+D', 'Mod+Shift+D', 'Mod+Shift+B', 'Mod+Shift+O', 'Mod+H', 'Mod+J',
  'Mod+F', 'Mod+G', 'Mod+Shift+G', 'Mod+P', 'Mod+S', 'Mod+O', 'Mod+U', 'Mod+Shift+Delete',
  'Mod+Shift+V', 'Mod+Shift+U', 'Mod+Plus', 'Mod+-', 'Mod+0',
];

/** isBrowserOwned: should the registry refuse this chord? */
export function isBrowserOwned(sc: Shortcut): boolean {
  return BROWSER_OWNED.some((b) => {
    const o = parseShortcut(b);
    return !!o && o.mod === sc.mod && o.ctrl === sc.ctrl && o.alt === sc.alt
      && o.shift === sc.shift && o.meta === sc.meta && o.key === sc.key;
  });
}

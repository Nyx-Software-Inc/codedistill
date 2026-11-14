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

import { beforeEach, describe, expect, it, vi } from 'vitest';
import {
  _resetForTests, registerCommand, registerCommands, getCommand, commandsById,
  setCommandContext, isEnabled, runCommand, matchShortcut, shortcutLabel,
} from './commands.svelte';

const noop = () => {};

beforeEach(() => _resetForTests());

describe('registry', () => {
  it('replaces by id instead of duplicating', () => {
    registerCommand({ id: 'a', label: 'A', category: 'View', run: noop });
    registerCommand({ id: 'a', label: 'A2', category: 'View', run: noop });
    expect(commandsById(['a']).map((c) => c.label)).toEqual(['A2']);
  });
  it('resolves an ordered id list and drops unknown ids', () => {
    registerCommands([
      { id: 'b', label: 'B', category: 'View', run: noop },
      { id: 'a', label: 'A', category: 'View', run: noop },
    ]);
    expect(commandsById(['a', 'zzz', 'b']).map((c) => c.id)).toEqual(['a', 'b']);
  });
  it('drops a browser-owned shortcut but keeps the command', () => {
    registerCommand({ id: 'r', label: 'R', category: 'View', shortcut: 'Mod+Shift+R', run: noop });
    const c = getCommand('r')!;
    expect(c.keys).toBeNull();
    expect(c.shortcut).toBeUndefined();
    expect(runCommand('r')).toBe(true);
  });
  it('parses the shortcut at registration', () => {
    registerCommand({ id: 'a', label: 'A', category: 'View', shortcut: 'Mod+Shift+F', run: noop });
    expect(shortcutLabel(getCommand('a')!, false)).toBe('Ctrl+Shift+F');
  });
});

describe('enablement', () => {
  it('needs a project when scoped to one', () => {
    registerCommand({ id: 'p', label: 'P', category: 'Project', scope: 'project', run: noop });
    expect(isEnabled(getCommand('p')!)).toBe(false);
    setCommandContext({ hasProject: true });
    expect(isEnabled(getCommand('p')!)).toBe(true);
  });
  it('needs the licence feature', () => {
    registerCommand({ id: 'f', label: 'F', category: 'View', feature: 'mcp', run: noop });
    setCommandContext({ licensed: (f) => f !== 'mcp' });
    expect(isEnabled(getCommand('f')!)).toBe(false);
    setCommandContext({ licensed: () => true });
    expect(isEnabled(getCommand('f')!)).toBe(true);
  });
  it('honours the command\'s own rule last', () => {
    let ok = false;
    registerCommand({ id: 'e', label: 'E', category: 'View', enabled: () => ok, run: noop });
    expect(isEnabled(getCommand('e')!)).toBe(false);
    ok = true;
    expect(isEnabled(getCommand('e')!)).toBe(true);
  });
});

describe('runCommand', () => {
  it('runs an enabled command and reports it', () => {
    const run = vi.fn();
    registerCommand({ id: 'a', label: 'A', category: 'View', run });
    expect(runCommand('a')).toBe(true);
    expect(run).toHaveBeenCalledOnce();
  });
  it('refuses unknown, hidden and disabled commands without throwing', () => {
    const run = vi.fn();
    registerCommand({ id: 'h', label: 'H', category: 'View', hidden: () => true, run });
    registerCommand({ id: 'd', label: 'D', category: 'View', enabled: () => false, run });
    expect(runCommand('nope')).toBe(false);
    expect(runCommand('h')).toBe(false);
    expect(runCommand('d')).toBe(false);
    expect(run).not.toHaveBeenCalled();
  });
});

describe('matchShortcut', () => {
  const ev = (k: string, m: Partial<{ ctrl: boolean; shift: boolean; meta: boolean }> = {}) => ({
    key: k, ctrlKey: !!m.ctrl, altKey: false, shiftKey: !!m.shift, metaKey: !!m.meta,
  });
  beforeEach(() => {
    registerCommands([
      { id: 'drawer', label: 'Drawer', category: 'View', shortcut: 'Mod+B', run: noop },
      { id: 'search', label: 'Search', category: 'View', shortcut: 'Mod+Shift+F', run: noop },
      { id: 'help', label: 'Help', category: 'Help', shortcut: 'F1', run: noop },
    ]);
  });
  it('finds the command for a chord on each platform', () => {
    expect(matchShortcut(ev('b', { ctrl: true }), { mac: false, editable: false })?.id).toBe('drawer');
    expect(matchShortcut(ev('b', { meta: true }), { mac: true, editable: false })?.id).toBe('drawer');
    expect(matchShortcut(ev('b', { ctrl: true }), { mac: true, editable: false })).toBeNull();
  });
  it('keeps editor chords out of text fields but lets Mod+Shift and F-keys in', () => {
    expect(matchShortcut(ev('b', { ctrl: true }), { mac: false, editable: true })).toBeNull();
    expect(matchShortcut(ev('F', { ctrl: true, shift: true }), { mac: false, editable: true })?.id).toBe('search');
    expect(matchShortcut(ev('F1'), { mac: false, editable: true })?.id).toBe('help');
  });
  it('matches nothing for an unbound key', () => {
    expect(matchShortcut(ev('x', { ctrl: true }), { mac: false, editable: false })).toBeNull();
  });
});

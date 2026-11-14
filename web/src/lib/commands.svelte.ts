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

// The command registry: every action the shell can take, declared once.
//
// A command is the primitive; the header buttons, the profile menu, keyboard
// shortcuts and (next) a palette and menubar are VIEWS of this list. Add a
// command here and it is reachable from every surface that reads the
// registry; remove it and it is gone from all of them. That is what makes
// consistency structural rather than something a review has to catch:
// there is one label, one icon, one shortcut and one enablement rule per
// action, because there is one definition.
//
// The registry owns WHAT exists and WHETHER it may run. It does not own the
// state a command acts on — `run`, `active` and `badge` are closures the
// owner of that state (App.svelte, today) supplies, so reading them inside a
// template stays reactive through the closure. Enablement that is the same
// for every command (needs a project, needs a license feature) is declared as
// data and decided here against a context the shell keeps current.
//
// See docs/design/commands.md.

import {
  parseShortcut, matchesShortcut, formatShortcut, firesWhileTyping,
  isEditableTarget, isMacPlatform, isBrowserOwned, type Shortcut,
} from './shortcuts';

/** What a command needs to be meaningful. 'app' is always available. */
export type CommandScope = 'app' | 'project' | 'scratchpad';

/** Where a command groups. This is the menubar's skeleton, written down now so
 *  nothing has to be re-sorted when menus render from the registry. */
export type CommandCategory = 'View' | 'Project' | 'Scratchpad' | 'Account' | 'Help';

export interface CommandDef {
  /** Stable, dotted, never shown: 'view.needsReview'. Shortcuts, menus and
   *  (later) MCP refer to a command by this. */
  id: string;
  /** What a human sees on a button or menu row. */
  label: string;
  /** The longer form a tooltip or palette row shows. */
  description?: string;
  category: CommandCategory;
  /** Icon name for Icon.svelte, and its signal colour on the toolbar. */
  icon?: string;
  color?: string;
  /** Portable notation — see shortcuts.ts. */
  shortcut?: string;
  /** Licence feature the command needs; unlicensed users see it disabled. */
  feature?: string;
  scope?: CommandScope;
  /** The return value is ignored, so an assignment expression is a fine body. */
  run: () => unknown;
  /** For toggles: is the thing this opens currently open? Drives the pressed
   *  state wherever the command is rendered. */
  active?: () => boolean;
  /** Extra enablement beyond scope and feature. */
  enabled?: () => boolean;
  /** Only some users should see it at all (admin-only, multi-user-only). */
  hidden?: () => boolean;
  /** A count worth showing beside the label; 0 shows nothing. */
  badge?: () => number;
}

/** A registered command: the definition plus its parsed shortcut. */
export interface Command extends CommandDef {
  keys: Shortcut | null;
}

/** What the shell knows that enablement depends on. Kept current by App. */
export interface CommandContext {
  hasProject: boolean;
  hasScratchpad: boolean;
  licensed: (feature: string) => boolean;
}

// A rune, not a plain object: the header renders `disabled` from ctx, and
// the project id arrives after first paint. The first cut was a bare TS cast
// here (a heredoc ate the `$`) and every project-scoped header button stayed
// disabled for the life of the page while the keyboard, which reads ctx at
// keypress, worked — which is why it went unnoticed.
const state = $state<{ list: Command[]; ctx: CommandContext }>({
  list: [],
  ctx: { hasProject: false, hasScratchpad: false, licensed: () => true },
});

/** registerCommand adds or REPLACES a command by id, so re-running a
 *  component's setup never duplicates an entry. */
export function registerCommand(def: CommandDef): void {
  const cmd: Command = { ...def, keys: def.shortcut ? parseShortcut(def.shortcut) : null };
  if (def.shortcut && !cmd.keys) {
    console.warn(`command ${def.id}: unparseable shortcut '${def.shortcut}'`);
  }
  // A browser-owned chord is dropped, not just warned about: the command
  // still registers and is reachable every other way, but the keystroke
  // stays the browser's.
  if (cmd.keys && isBrowserOwned(cmd.keys)) {
    console.warn(`command ${def.id}: shortcut '${def.shortcut}' belongs to the browser; dropped`);
    cmd.keys = null;
    cmd.shortcut = undefined;
  }
  const i = state.list.findIndex((c) => c.id === def.id);
  if (i >= 0) state.list[i] = cmd;
  else state.list.push(cmd);
}

export function registerCommands(defs: CommandDef[]): void {
  for (const d of defs) registerCommand(d);
}

export function unregisterCommand(id: string): void {
  const i = state.list.findIndex((c) => c.id === id);
  if (i >= 0) state.list.splice(i, 1);
}

export function getCommand(id: string): Command | undefined {
  return state.list.find((c) => c.id === id);
}

/** allCommands in registration order. Read inside a derived, not into a local
 *  at init — the list is a rune and grows as components register. */
export function allCommands(): readonly Command[] {
  return state.list;
}

/** commandsById resolves an ordered id list, dropping ids nothing registered.
 *  This is how a surface states ITS order (the toolbar's, say) without the
 *  registry having to know about surfaces. */
export function commandsById(ids: readonly string[]): Command[] {
  const out: Command[] = [];
  for (const id of ids) {
    const c = getCommand(id);
    if (c) out.push(c);
  }
  return out;
}

export function setCommandContext(patch: Partial<CommandContext>): void {
  Object.assign(state.ctx, patch);
}

export function commandContext(): CommandContext {
  return state.ctx;
}

/** isEnabled applies the rules that are data (scope, feature) and then the
 *  command's own rule. The order matters for the message a disabled control
 *  could show later: "needs a project" before "needs Pro" before whatever the
 *  command itself says. */
export function isEnabled(cmd: Command): boolean {
  const ctx = state.ctx;
  if (cmd.scope === 'project' && !ctx.hasProject) return false;
  if (cmd.scope === 'scratchpad' && !ctx.hasScratchpad) return false;
  if (cmd.feature && !ctx.licensed(cmd.feature)) return false;
  if (cmd.enabled && !cmd.enabled()) return false;
  return true;
}

export function isVisible(cmd: Command): boolean {
  return !(cmd.hidden && cmd.hidden());
}

/** runCommand is the one way anything invokes a command. Returns false when
 *  the command is unknown, hidden or disabled, so a caller can tell a refusal
 *  from a run; it never throws for those. */
export function runCommand(id: string): boolean {
  const cmd = getCommand(id);
  if (!cmd || !isVisible(cmd) || !isEnabled(cmd)) return false;
  void cmd.run();
  return true;
}

/** shortcutLabel is what a tooltip appends: '' when there is none. */
export function shortcutLabel(cmd: Command, mac: boolean = isMacPlatform()): string {
  return cmd.keys ? formatShortcut(cmd.keys, mac) : '';
}

/** matchShortcut finds the command a keystroke means, honouring where the
 *  focus is. Exported for tests; the listener below is the production path. */
export function matchShortcut(
  e: { key: string; ctrlKey: boolean; altKey: boolean; shiftKey: boolean; metaKey: boolean },
  opts: { mac: boolean; editable: boolean },
): Command | null {
  for (const cmd of state.list) {
    if (!cmd.keys) continue;
    if (opts.editable && !firesWhileTyping(cmd.keys)) continue;
    if (matchesShortcut(cmd.keys, e, opts.mac)) return cmd;
  }
  return null;
}

/** installShortcuts wires the window's keydown to the registry. Returns the
 *  uninstaller so a component can own its lifetime. A matched-but-disabled
 *  command still swallows the keystroke: Mod+Shift+F with no project open
 *  should do nothing, not fall through to the browser's find-in-page. */
export function installShortcuts(target: Window = window): () => void {
  const mac = isMacPlatform();
  const onKey = (e: KeyboardEvent) => {
    if (e.defaultPrevented || e.isComposing) return;
    const cmd = matchShortcut(e, { mac, editable: isEditableTarget(e.target) });
    if (!cmd || !isVisible(cmd)) return;
    e.preventDefault();
    if (isEnabled(cmd)) void cmd.run();
  };
  target.addEventListener('keydown', onKey);
  return () => target.removeEventListener('keydown', onKey);
}

/** _resetForTests empties the registry between test cases. */
export function _resetForTests(): void {
  state.list.length = 0;
  state.ctx = { hasProject: false, hasScratchpad: false, licensed: () => true };
}

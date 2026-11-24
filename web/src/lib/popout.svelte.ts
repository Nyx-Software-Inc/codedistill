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

// Popped-out panes: the windows, and the channel between them and the main one.
//
// A popped-out window is a separate copy of the app showing one pane
// (`/?pane=jobs`), with its own data and its own event stream. Nothing is drawn
// into it from here. What crosses windows goes over a BroadcastChannel, and it
// is little: the active project and changeset one way; "open this in the main
// window", the window's bounds and its comings and goings the other.
//
// The main window owns the layout. A popped-out window never saves it; it
// reports, and the main window saves. See docs/design/panes.md, "Popping a pane
// out of the window", including what the Linux test found.

import type { ChangesetPick } from '../components/ChangesetPicker.svelte';
import { paneDef, type Bounds } from './layout';
import {
  dockPane, floatPane, floatingPanes, isPaneFloating, layoutLoaded,
  setFloatBounds, setFloatingHooks,
} from './layoutStore.svelte';

const CHANNEL = 'codedistill-panes';

/** What the main window tells every pane window. */
export interface SharedState {
  projectId: string;
  changeset: ChangesetPick | null;
}

/** Things a pane asks the main window to do. Each is one of the callbacks the
 *  pane takes in the main window (App.svelte's paneContent). */
export type Call =
  | { fn: 'openItem'; ownerType: string; ownerId: string }
  | { fn: 'openFile'; path: string }
  | { fn: 'openChangeset'; changeset: ChangesetPick }
  | { fn: 'pickChangeset'; changeset: ChangesetPick | null }
  | { fn: 'reviewJob'; jobId: string }
  | { fn: 'newJob' }
  | { fn: 'projectImported'; projectId: string }
  | { fn: 'changed' };

export type Msg =
  // pane window → main
  | { t: 'hello'; pane: string }
  | { t: 'bye'; pane: string }
  | { t: 'bounds'; pane: string; b: Bounds }
  | { t: 'dock'; pane: string }
  | { t: 'call'; pane: string; call: Call }
  // main → pane windows
  | { t: 'state'; state: SharedState }
  | { t: 'ask' }
  | { t: 'close'; pane: string }
  | { t: 'focus'; pane: string }
  | { t: 'main-hello' }
  | { t: 'main-bye' }
  // either: a pane's tab was dropped on this window and taken
  | { t: 'taken'; pane: string };

let channel: BroadcastChannel | null = null;
const listeners = new Set<(m: Msg) => void>();

function bus(): BroadcastChannel | null {
  if (channel) return channel;
  if (typeof BroadcastChannel === 'undefined') return null;
  channel = new BroadcastChannel(CHANNEL);
  channel.onmessage = (e) => {
    for (const l of listeners) l(e.data as Msg);
  };
  return channel;
}

export function send(m: Msg): void {
  bus()?.postMessage(m);
}

export function listen(fn: (m: Msg) => void): () => void {
  bus();
  listeners.add(fn);
  return () => listeners.delete(fn);
}

/** The name a pane's window is opened under. Opening the same name again
 *  reaches the same window rather than making a second one. */
function windowName(id: string): string {
  return 'codedistill-pane-' + id;
}

/** The URL of a pane's window. Its size travels with it: the desktop app's
 *  Chrome opens every window at its launch size whatever window.open asks for,
 *  so the window resizes itself once open (docs/design/panes.md, "Linux test"). */
function paneURL(id: string, w: number, h: number): string {
  const u = new URL(location.href);
  u.search = '';
  u.hash = '';
  u.searchParams.set('pane', id);
  u.searchParams.set('w', String(w));
  u.searchParams.set('h', String(h));
  return u.toString();
}

// ── The main window ─────────────────────────────────────────────────────────

/** Panes whose window the browser refused to open, so the main window can say
 *  so. Cleared when the user opens them by hand. */
export const popoutNotice = $state({ blocked: [] as string[] });

let getState: () => SharedState = () => ({ projectId: '', changeset: null });
let onCall: (c: Call) => void = () => {};
/** Panes a window has said hello for since this window started. */
const alive = new Set<string>();
/** A pane whose window said goodbye: docked unless it says hello again soon,
 *  which is what a reload does. */
const leaving = new Map<string, ReturnType<typeof setTimeout>>();

/** How long a closed window has to come back (a reload) before its pane is
 *  put back in its slot. */
const BYE_GRACE_MS = 1500;
/** How long a starting main window waits for open pane windows to answer
 *  before reopening the ones that did not. */
const ASK_WAIT_MS = 600;

/** initMainWindow wires the main window to its popped-out panes. `state` is read
 *  whenever a window needs it; `call` runs what a pane asks for. Call once,
 *  after the layout has loaded; returns the cleanup. */
export function initMainWindow(state: () => SharedState, call: (c: Call) => void): () => void {
  getState = state;
  onCall = call;
  setFloatingHooks(
    (id) => send({ t: 'close', pane: id }),
    (id) => focusPane(id),
  );
  const stop = listen((m) => {
    switch (m.t) {
      case 'hello': {
        alive.add(m.pane);
        const t = leaving.get(m.pane);
        if (t) { clearTimeout(t); leaving.delete(m.pane); }
        if (!isPaneFloating(m.pane)) {
          // A window for a pane that is not popped out: left over from a layout
          // that has since changed (docked from another main window, or reset).
          send({ t: 'close', pane: m.pane });
          return;
        }
        send({ t: 'state', state: getState() });
        return;
      }
      case 'bye': {
        alive.delete(m.pane);
        clearTimeout(leaving.get(m.pane));
        leaving.set(m.pane, setTimeout(() => {
          leaving.delete(m.pane);
          // Closing a popped-out window puts its pane back where it came from,
          // as closing an editor window does in other IDEs: the pane does not
          // simply vanish.
          if (isPaneFloating(m.pane)) dockPane(m.pane);
        }, BYE_GRACE_MS));
        return;
      }
      case 'bounds':
        setFloatBounds(m.pane, m.b);
        return;
      case 'dock':
        if (isPaneFloating(m.pane)) dockPane(m.pane);
        send({ t: 'close', pane: m.pane });
        return;
      case 'call':
        onCall(m.call);
        return;
      case 'taken':
        takenAt = Date.now();
        return;
    }
  });
  const bye = () => send({ t: 'main-bye' });
  window.addEventListener('pagehide', bye);
  send({ t: 'main-hello' });
  void restore();
  return () => {
    stop();
    window.removeEventListener('pagehide', bye);
  };
}

/** shareState tells every pane window the main window's current state. */
export function shareState(): void {
  send({ t: 'state', state: getState() });
}

/** restore reopens popped-out panes whose windows are not already open. */
async function restore(): Promise<void> {
  if (!layoutLoaded() || floatingPanes().length === 0) return;
  send({ t: 'ask' });
  await new Promise((r) => setTimeout(r, ASK_WAIT_MS));
  const blocked: string[] = [];
  for (const f of floatingPanes()) {
    if (alive.has(f.pane)) continue;
    if (!openWindow(f.pane, f.w, f.h, f.x, f.y)) blocked.push(f.pane);
  }
  popoutNotice.blocked = blocked;
}

function openWindow(id: string, w: number, h: number, x?: number, y?: number): boolean {
  const feat = [`popup`, `width=${w}`, `height=${h}`];
  if (x !== undefined && y !== undefined) feat.push(`left=${x}`, `top=${y}`);
  const win = window.open(paneURL(id, w, h), windowName(id), feat.join(','));
  return !!win;
}

/** popOut moves a pane into a window of its own, at `at` (screen coordinates)
 *  where the platform allows. Returns false when the browser refused to open
 *  the window; the pane then stays where it was. */
export function popOut(id: string, at?: { x: number; y: number }): boolean {
  if (!paneDef(id)?.floats) return false;
  if (isPaneFloating(id) && alive.has(id)) {
    focusPane(id);
    return true;
  }
  floatPane(id, at);
  const f = floatingPanes().find((p) => p.pane === id)!;
  if (!openWindow(id, f.w, f.h, f.x, f.y)) {
    dockPane(id);
    popoutNotice.blocked = [...new Set([...popoutNotice.blocked, id])];
    return false;
  }
  popoutNotice.blocked = popoutNotice.blocked.filter((p) => p !== id);
  return true;
}

/** reopenBlocked retries the windows the browser refused, from a click, which
 *  is a gesture a popup blocker accepts. */
export function reopenBlocked(): void {
  const ids = popoutNotice.blocked;
  popoutNotice.blocked = [];
  for (const id of ids) {
    const f = floatingPanes().find((p) => p.pane === id);
    if (f) openWindow(id, f.w, f.h, f.x, f.y);
    else popOut(id);
  }
}

/** focusPane brings a popped-out pane's window forward. Desktops that prevent
 *  focus stealing may only flag the window instead. */
export function focusPane(id: string): void {
  send({ t: 'focus', pane: id });
}

// ── Dragging a tab out of the window ────────────────────────────────────────
//
// A tab dropped outside the window ends with dropEffect "none" — exactly what
// a tab dropped on ANOTHER of our windows ends with, because Chrome never tells
// the window a drag came from that a different window took the drop (the
// Linux test). So a drop that looks like "outside" waits briefly for any
// window to say it took the pane before popping it out.

let takenAt = 0;
let leftWindow = false;

if (typeof window !== 'undefined') {
  // Where the pointer is during a drag is the one thing dragend's coordinates
  // may not say reliably everywhere; leaving the document says it for certain.
  document.addEventListener('dragleave', (e) => {
    if (e.relatedTarget === null) leftWindow = true;
  });
  document.addEventListener('dragenter', () => (leftWindow = false));
  document.addEventListener('dragstart', () => (leftWindow = false));
}

/** True when a drag that just ended let go outside this window. */
export function droppedOutside(e: DragEvent): boolean {
  if (e.dataTransfer?.dropEffect !== 'none') return false;
  const outside = e.clientX < 0 || e.clientY < 0 || e.clientX > innerWidth || e.clientY > innerHeight;
  return outside || leftWindow;
}

const TAKEN_WAIT_MS = 250;

/** dragOut pops a pane out after a drag ended outside the window, unless
 *  another of our windows took it in the meantime. */
export function dragOut(id: string, screenX: number, screenY: number): void {
  const started = Date.now();
  setTimeout(() => {
    if (takenAt >= started) return;
    // Offset so the pointer lands on the window's header, not its corner.
    popOut(id, { x: Math.round(screenX - 60), y: Math.round(screenY - 16) });
  }, TAKEN_WAIT_MS);
}

/** paneTaken is called by a window that accepted a pane's tab, so the window
 *  the drag came from does not also pop it out. */
export function paneTaken(id: string): void {
  send({ t: 'taken', pane: id });
}

// ── A popped-out window ─────────────────────────────────────────────────────

/** paneWindowParams reads which pane this window shows, or null when it is
 *  the main window. */
export function paneWindowParams(): { pane: string; w: number; h: number } | null {
  const q = new URLSearchParams(location.search);
  const pane = q.get('pane');
  if (!pane || !paneDef(pane)?.floats) return null;
  return { pane, w: Number(q.get('w')) || 0, h: Number(q.get('h')) || 0 };
}


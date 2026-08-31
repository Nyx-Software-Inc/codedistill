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

// Submit-shortcut preference for multi-line composers (new-item entry,
// Q&A input). Two values:
//
//   'ctrl-enter' — Enter inserts newline, Ctrl/Cmd+Enter submits.
//                  This is the default (preserves pre-setting behavior).
//   'enter'      — Enter submits, Shift+Enter inserts newline. Ctrl+Enter
//                  also submits as a muscle-memory fallback.
//
// Module-level state shared across every component that imports this
// file. No Svelte reactivity is needed: keyboard handlers read the
// current value imperatively on each keystroke, and the Settings UI
// writes synchronously through setSubmitShortcut before persisting.

import { LOCAL_USER } from './userSettings';
import { getUserSetting, setUserSetting } from './api';

const KEY = 'input.submit_shortcut';

export type SubmitShortcut = 'enter' | 'ctrl-enter';
export const DEFAULT_SUBMIT_SHORTCUT: SubmitShortcut = 'ctrl-enter';
export const SUBMIT_SHORTCUT_KEY = KEY;

let _shortcut: SubmitShortcut = DEFAULT_SUBMIT_SHORTCUT;
let _loadPromise: Promise<SubmitShortcut> | null = null;

// getSubmitShortcut returns the current value synchronously. Returns
// the default until loadSubmitShortcut resolves on app boot.
export function getSubmitShortcut(): SubmitShortcut {
  return _shortcut;
}

// shouldSubmit decides whether a keystroke in a multi-line composer
// should fire the submit action. Returns false for any non-Enter key.
// Ctrl/Cmd+Enter ALWAYS submits regardless of preference so the muscle
// memory of long-time users never loses an in-progress message.
export function shouldSubmit(e: KeyboardEvent): boolean {
  if (e.key !== 'Enter') return false;
  if (e.ctrlKey || e.metaKey) return true;
  if (_shortcut === 'enter') {
    // Plain Enter submits unless Shift is held (newline escape hatch).
    return !e.shiftKey;
  }
  return false;
}

// loadSubmitShortcut fetches the persisted preference once per session.
// Memoized so repeat callers on different components share the same
// network round-trip. Called early in App.svelte's onMount so keyboard
// handlers later in the session see the right value.
export function loadSubmitShortcut(): Promise<SubmitShortcut> {
  if (_loadPromise) return _loadPromise;
  _loadPromise = (async () => {
    try {
      const r = await getUserSetting(LOCAL_USER, KEY);
      // undefined = 204, never set — leave _shortcut at its default.
      if (r && (r.value === 'enter' || r.value === 'ctrl-enter')) {
        _shortcut = r.value;
      }
    } catch {
      // Transport failure — leave the default in place.
    }
    return _shortcut;
  })();
  return _loadPromise;
}

// setSubmitShortcut updates the in-memory value immediately so the
// Settings UI feels responsive, then persists. Persist errors are
// logged, not surfaced — the user notices at most on next reload.
export async function setSubmitShortcut(v: SubmitShortcut): Promise<void> {
  _shortcut = v;
  try {
    await setUserSetting(LOCAL_USER, KEY, v);
  } catch (e) {
    console.warn('setSubmitShortcut:', e);
  }
}

// submitHintText returns the trailing affordance for placeholders /
// button labels. Helps users discover the right key combo when they
// switch the setting (or arrive on a machine with a different default).
export function submitHintText(s: SubmitShortcut = _shortcut): string {
  return s === 'enter' ? 'Enter to send' : 'Ctrl+Enter to send';
}

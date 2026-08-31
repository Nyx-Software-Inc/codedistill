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

// Lightweight wrapper around the user_settings REST API for the persisted
// UI state that the drawer (and future preferences) need.
//
// In single-user local mode the user id is hardcoded to "local" — this is
// the implicit user seeded by migration 0006. When SSO ships, the caller
// will replace LOCAL_USER with the authenticated user's id.

import { getUserSetting, setUserSetting } from './api';

export const LOCAL_USER = 'local';

// loadUserBool fetches a boolean preference, falling back to `defaultValue`
// when the key has never been set or the request fails. The 404 path is
// the common case on first run, so we never surface it as an error.
export async function loadUserBool(key: string, defaultValue: boolean): Promise<boolean> {
  try {
    const r = await getUserSetting(LOCAL_USER, key);
    if (!r) return defaultValue; // 204 — never set; use the caller's default
    if (typeof r.value === 'boolean') return r.value;
    return defaultValue;
  } catch {
    return defaultValue;
  }
}

// saveUserBool fires-and-forgets a write. The drawer toggle should feel
// instant; we don't want to block on the round-trip. Errors are logged
// rather than surfaced — if it doesn't persist this session, the user
// notices on next reload at most.
export function saveUserBool(key: string, value: boolean): void {
  setUserSetting(LOCAL_USER, key, value).catch((e) => {
    console.warn(`saveUserBool ${key}=${value}:`, e);
  });
}

export async function loadUserNum(key: string, defaultValue: number): Promise<number> {
  try {
    const r = await getUserSetting(LOCAL_USER, key);
    if (!r) return defaultValue; // 204 — never set; use the caller's default
    if (typeof r.value === 'number' && Number.isFinite(r.value)) return r.value;
    return defaultValue;
  } catch {
    return defaultValue;
  }
}

export function saveUserNum(key: string, value: number): void {
  setUserSetting(LOCAL_USER, key, value).catch((e) => {
    console.warn(`saveUserNum ${key}=${value}:`, e);
  });
}

// loadUserString fetches a string preference, falling back to defaultValue
// on 404 / network errors / non-string stored values.
export async function loadUserString(key: string, defaultValue: string): Promise<string> {
  try {
    const r = await getUserSetting(LOCAL_USER, key);
    if (!r) return defaultValue; // 204 — never set; use the caller's default
    return typeof r.value === 'string' ? r.value : defaultValue;
  } catch {
    return defaultValue;
  }
}

export function saveUserString(key: string, value: string): void {
  void setUserSetting(LOCAL_USER, key, value).catch((e) => {
    console.warn(`save setting ${key}:`, e);
  });
}

// loadUserObject fetches a JSON-object preference, falling back to
// defaultValue on 404 / errors / non-object stored values. The default
// is also used to fill missing keys shallowly so configs can grow.
export async function loadUserObject<T extends object>(key: string, defaultValue: T): Promise<T> {
  try {
    const r = await getUserSetting(LOCAL_USER, key);
    if (!r) return defaultValue; // 204 — never set; use the caller's default
    if (r.value && typeof r.value === 'object' && !Array.isArray(r.value)) {
      return { ...defaultValue, ...(r.value as Partial<T>) };
    }
    return defaultValue;
  } catch {
    return defaultValue;
  }
}

export function saveUserObject(key: string, value: object): void {
  void setUserSetting(LOCAL_USER, key, value).catch((e) => {
    console.warn(`save setting ${key}:`, e);
  });
}

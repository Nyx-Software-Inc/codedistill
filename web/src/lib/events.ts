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

// Client-side event channel for the server's coarse change feed.
//
// EventChannel is the transport-agnostic interface — open/close +
// per-event handler registration + connection-state observer. The
// SSE impl below wraps the browser's native EventSource. When/if we
// add a WebSocket impl, it slots in behind the same interface and
// the consumer (App.svelte) swaps the constructor.
//
// Event names are coarse and must match the constants in
// internal/events/events.go on the server side. Handlers receive
// just the name — the agreed contract is "refetch the affected
// resource yourself" rather than embedding payload data in the
// event.

// Event names — keep in sync with internal/events/events.go.
export const EventNames = {
  ItemsChanged: 'items.changed',
  FilesChanged: 'files.changed',
  InboxChanged: 'inbox.changed',
  TodosChanged: 'todos.changed',
  BugsChanged: 'bugs.changed',
  KBChanged: 'kb.changed',
  UseCasesChanged: 'use_cases.changed',
  AnchorsChanged: 'anchors.changed',
  ProjectsChanged: 'projects.changed',
  ScratchpadsChanged: 'scratchpads.changed',
} as const;
export type EventName = (typeof EventNames)[keyof typeof EventNames];

export type ConnectionState = 'connecting' | 'connected' | 'disconnected';

export interface EventChannel {
  open(): void;
  close(): void;
  // Register a handler for a specific event name. Returns an
  // unsubscribe function. The same channel supports multiple
  // handlers per event.
  on(event: EventName, handler: () => void): () => void;
  // Subscribe to connection-state transitions. Returns
  // unsubscribe. Useful for the UI's "live" indicator + for the
  // App.svelte polling fallback (poll only when disconnected).
  onState(handler: (state: ConnectionState) => void): () => void;
  // Current state (read at any time).
  state(): ConnectionState;
}

// SSEChannel wraps the browser's native EventSource. EventSource
// handles auto-reconnect (3s default) on its own; we only have to
// reflect connection state and dispatch messages.
//
// Event format on the wire (set by internal/api/events.go):
//
//   data: items.changed\n\n
//
// We map message.data to the registered handlers.
export class SSEChannel implements EventChannel {
  private url: string;
  private es: EventSource | null = null;
  private handlers = new Map<string, Set<() => void>>();
  private stateHandlers = new Set<(s: ConnectionState) => void>();
  private currentState: ConnectionState = 'disconnected';
  // Self-healing (the "silently stale tab" class of bug):
  //  - lastSeenAt tracks the newest frame INCLUDING the server's 15s named
  //    ping; a "connected" stream that goes quiet past STALE_MS is a zombie
  //    (dead TCP after a laptop sleep the browser never noticed) → rebuild.
  //  - a CLOSED EventSource never reconnects on its own → rebuild after a
  //    short delay.
  //  - tab-visible / network-online are the natural recovery moments →
  //    rebuild immediately if not healthy.
  private lastSeenAt = 0;
  private watchdog: ReturnType<typeof setInterval> | undefined;
  private reopenTimer: ReturnType<typeof setTimeout> | undefined;
  private wantOpen = false;
  private static readonly STALE_MS = 45_000; // 3 missed 15s pings
  private onVisible = () => {
    if (document.visibilityState === 'visible') this.ensureHealthy();
  };
  private onOnline = () => this.ensureHealthy();

  constructor(url = '/api/v1/events') {
    this.url = url;
  }

  open(): void {
    this.wantOpen = true;
    if (!this.watchdog) {
      this.watchdog = setInterval(() => {
        if (!this.wantOpen) return;
        if (this.es && this.currentState === 'connected' && Date.now() - this.lastSeenAt > SSEChannel.STALE_MS) {
          // Zombie: browser thinks it's connected but nothing — not even the
          // server's heartbeat — has arrived. Tear down and rebuild.
          this.rebuild();
        }
      }, 10_000);
      document.addEventListener('visibilitychange', this.onVisible);
      window.addEventListener('online', this.onOnline);
    }
    if (this.es) return;
    this.setState('connecting');
    this.es = new EventSource(this.url);
    // EventSource fires 'open' on first successful connect AND on
    // each successful reconnect — the transition to 'connected' is
    // re-fireable, which matches our state model.
    this.es.onopen = () => {
      this.lastSeenAt = Date.now();
      this.setState('connected');
    };
    // 'error' fires on disconnect. EventSource auto-reconnects on its own
    // UNLESS readyState === CLOSED — that state is permanent, so we schedule
    // our own rebuild for it.
    this.es.onerror = () => {
      if (this.es?.readyState === EventSource.CLOSED) {
        this.setState('disconnected');
        if (this.wantOpen && !this.reopenTimer) {
          this.reopenTimer = setTimeout(() => {
            this.reopenTimer = undefined;
            if (this.wantOpen) this.rebuild();
          }, 5_000);
        }
      } else {
        this.setState('connecting');
      }
    };
    this.es.onmessage = (e) => {
      this.lastSeenAt = Date.now();
      const name = (e.data ?? '').trim();
      if (!name) return;
      const set = this.handlers.get(name);
      if (!set) return;
      for (const h of set) {
        try { h(); } catch (err) { console.warn(`event handler for ${name} threw:`, err); }
      }
    };
    // Named frames the server sends outside the normal change feed: 'ready'
    // on connect (connection state is covered by onopen) and the 15s 'ping'
    // heartbeat — both count as liveness for the zombie watchdog.
    this.es.addEventListener('ready', () => { this.lastSeenAt = Date.now(); });
    this.es.addEventListener('ping', () => { this.lastSeenAt = Date.now(); });
  }

  close(): void {
    this.wantOpen = false;
    if (this.watchdog) {
      clearInterval(this.watchdog);
      this.watchdog = undefined;
      document.removeEventListener('visibilitychange', this.onVisible);
      window.removeEventListener('online', this.onOnline);
    }
    if (this.reopenTimer) {
      clearTimeout(this.reopenTimer);
      this.reopenTimer = undefined;
    }
    if (!this.es) return;
    this.es.close();
    this.es = null;
    this.setState('disconnected');
  }

  // Tear down the current EventSource and open a fresh one. The state
  // handlers see disconnected → connecting → connected, so App's
  // reconnect-refresh catches up on anything missed while dead.
  private rebuild(): void {
    if (this.es) {
      this.es.close();
      this.es = null;
    }
    this.setState('disconnected');
    this.open();
  }

  // On tab-visible / network-online: if the stream isn't demonstrably
  // healthy, rebuild it now rather than waiting for the watchdog.
  private ensureHealthy(): void {
    if (!this.wantOpen) return;
    const stale = Date.now() - this.lastSeenAt > SSEChannel.STALE_MS;
    if (!this.es || this.currentState !== 'connected' || stale) this.rebuild();
  }

  on(event: EventName, handler: () => void): () => void {
    let set = this.handlers.get(event);
    if (!set) {
      set = new Set();
      this.handlers.set(event, set);
    }
    set.add(handler);
    return () => set!.delete(handler);
  }

  onState(handler: (state: ConnectionState) => void): () => void {
    this.stateHandlers.add(handler);
    // Fire the current state immediately so callers don't have to
    // separately read state() right after subscribing.
    handler(this.currentState);
    return () => this.stateHandlers.delete(handler);
  }

  state(): ConnectionState {
    return this.currentState;
  }

  private setState(s: ConnectionState) {
    if (this.currentState === s) return;
    this.currentState = s;
    for (const h of this.stateHandlers) {
      try { h(s); } catch (err) { console.warn('state handler threw:', err); }
    }
  }
}

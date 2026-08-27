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

package api

import (
	"fmt"
	"net/http"
	"time"
)

// sseHeartbeatInterval is the gap between keep-alive frames sent
// when there's no real event traffic. Long-lived HTTP connections
// get killed by intermediaries (corporate proxies, GCP Load
// Balancer, Cloud Run) after 30-60s of idle. 15s leaves comfortable
// margin under the tightest common timeout.
const sseHeartbeatInterval = 15 * time.Second

// streamEvents is the SSE endpoint backing the SPA's real-time
// change feed. Subscribes to the in-process Bus, streams every
// event as an SSE frame, sends a ping every 15s to keep proxies
// from killing the connection. Cleans up the subscription when
// the client disconnects (ctx.Done() fires).
//
// Event format on the wire:
//
//	data: items.changed\n\n
//
// Client (EventSource) parses message.data and dispatches a
// targeted refetch. Coarse event names match the constants in
// internal/events — the client maps them to fetch calls.
//
// No per-subscriber filtering yet: every connection receives every
// event. For single-user + small-team scale this is fine; when
// multi-tenant arrives we'll add project_id filtering on the bus
// side (or here as a select-time predicate). Designed to slot in
// cleanly without breaking the wire format.
//
// 503 when no event bus is wired (test setups, or a deployment
// mode where SSE is intentionally disabled).
func (s *Server) streamEvents(w http.ResponseWriter, r *http.Request) {
	if s.bus == nil {
		writeMsg(w, http.StatusServiceUnavailable, "event bus not configured")
		return
	}
	flusher, ok := w.(http.Flusher)
	if !ok {
		// All standard net/http response writers implement Flusher.
		// If we hit this, it's a bug in a middleware that wrapped
		// the writer without preserving the Flusher interface.
		writeMsg(w, http.StatusInternalServerError, "streaming not supported")
		return
	}

	// SSE headers. Cache-Control: no-cache + Connection: keep-alive
	// are the two non-obvious ones — without no-cache, proxies will
	// happily cache the response and replay stale frames to
	// reconnecting clients. X-Accel-Buffering: no defeats nginx's
	// default response buffering which would otherwise hold frames
	// until the buffer fills (defeating the live-stream point).
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)

	// Initial "ready" frame so clients can observe the connection
	// is alive and registered before any real events fire. Useful
	// for the UI's "connected vs reconnecting" indicator.
	fmt.Fprint(w, "event: ready\ndata: ok\n\n")
	flusher.Flush()

	ch, cancel := s.bus.Subscribe()
	defer cancel()

	ticker := time.NewTicker(sseHeartbeatInterval)
	defer ticker.Stop()

	ctx := r.Context()
	for {
		select {
		case <-ctx.Done():
			// Client disconnected (closed tab, navigated away,
			// network drop). Bus.Subscribe's cancel cleans up the
			// subscription via the deferred call.
			return
		case ev, ok := <-ch:
			if !ok {
				return // bus closed the channel
			}
			if _, err := fmt.Fprintf(w, "data: %s\n\n", ev); err != nil {
				return // write failure = client gone
			}
			flusher.Flush()
		case <-ticker.C:
			// Heartbeat as a NAMED event, not an SSE comment: comments reset
			// proxy idle timers but are invisible to browser EventSource by
			// spec, so the client can't tell a quiet stream from a zombie
			// connection (dead TCP after a laptop sleep that the browser
			// never notices). A named ping is visible client-side and drives
			// its staleness watchdog — while still keeping proxies happy.
			if _, err := fmt.Fprint(w, "event: ping\ndata: ok\n\n"); err != nil {
				return
			}
			flusher.Flush()
		}
	}
}

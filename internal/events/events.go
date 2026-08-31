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

// Package events is a tiny in-process pub/sub for cache-invalidation messages.
//
// Events are coarse: "items.changed", "todos.changed", etc. Subscribers receive
// the event name and refetch the affected resource. A slow subscriber will
// miss events (dropped from a full buffered channel) — that's intentional;
// the next event catches things up, and a disconnected SSE client will refetch
// on reconnect anyway.
package events

import "sync"

// Standard event names. Keep in sync with web/src/lib/events.ts.
// Coarse on purpose — clients refetch the affected resource on
// receiving an event rather than receiving a payload. Fine-grained
// payload events would buy little for our scale.
const (
	ItemsChanged    = "items.changed"
	InboxChanged    = "inbox.changed"
	TodosChanged    = "todos.changed"
	BugsChanged     = "bugs.changed"
	KBChanged       = "kb.changed"
	UseCasesChanged = "use_cases.changed"
	AnchorsChanged  = "anchors.changed"
	ProjectsChanged = "projects.changed"
	// FilesChanged fires when the code-index watcher's scan wrote or
	// deleted chunks — a proxy for "the repo's tree/content moved".
	// The Files panel refetches its tree on this.
	FilesChanged       = "files.changed"
	ScratchpadsChanged = "scratchpads.changed"
)

type Bus struct {
	mu   sync.Mutex
	subs []chan string
}

func NewBus() *Bus { return &Bus{} }

// Subscribe returns a channel that receives event names and a cleanup function.
// Always defer the cleanup; it removes the subscription and closes the channel.
func (b *Bus) Subscribe() (<-chan string, func()) {
	ch := make(chan string, 16)
	b.mu.Lock()
	b.subs = append(b.subs, ch)
	b.mu.Unlock()

	cancel := func() {
		b.mu.Lock()
		defer b.mu.Unlock()
		for i, s := range b.subs {
			if s == ch {
				b.subs = append(b.subs[:i], b.subs[i+1:]...)
				close(ch)
				return
			}
		}
	}
	return ch, cancel
}

// Publish broadcasts to all subscribers. Non-blocking: a full channel drops
// the event for that subscriber.
func (b *Bus) Publish(event string) {
	if b == nil {
		return
	}
	b.mu.Lock()
	subs := append([]chan string(nil), b.subs...)
	b.mu.Unlock()
	for _, ch := range subs {
		select {
		case ch <- event:
		default:
		}
	}
}

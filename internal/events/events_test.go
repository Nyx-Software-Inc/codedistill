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

package events

import (
	"sync"
	"testing"
	"time"
)

func TestBusPublishToOneSubscriber(t *testing.T) {
	b := NewBus()
	ch, cancel := b.Subscribe()
	defer cancel()

	b.Publish(ItemsChanged)
	select {
	case ev := <-ch:
		if ev != ItemsChanged {
			t.Errorf("got %q, want %q", ev, ItemsChanged)
		}
	case <-time.After(100 * time.Millisecond):
		t.Fatal("subscriber didn't receive event")
	}
}

func TestBusFansOutToMultipleSubscribers(t *testing.T) {
	b := NewBus()
	ch1, c1 := b.Subscribe()
	ch2, c2 := b.Subscribe()
	defer c1()
	defer c2()

	b.Publish(ProjectsChanged)
	for i, ch := range []<-chan string{ch1, ch2} {
		select {
		case ev := <-ch:
			if ev != ProjectsChanged {
				t.Errorf("sub %d: got %q", i, ev)
			}
		case <-time.After(100 * time.Millisecond):
			t.Errorf("sub %d: didn't receive event", i)
		}
	}
}

func TestBusCancelRemovesSubscriber(t *testing.T) {
	b := NewBus()
	ch, cancel := b.Subscribe()
	cancel()

	// After cancel, the channel must be closed (zero-value reads).
	select {
	case _, ok := <-ch:
		if ok {
			t.Errorf("channel should be closed after cancel")
		}
	case <-time.After(100 * time.Millisecond):
		t.Fatal("expected channel close, got block")
	}

	// Publishing after cancel must not panic and must not deliver
	// to the cancelled subscriber (no panic from sending on closed chan).
	b.Publish(InboxChanged)
}

func TestBusNonBlockingDropsOnFullBuffer(t *testing.T) {
	b := NewBus()
	ch, cancel := b.Subscribe()
	defer cancel()

	// Buffer is 16 slots. Publish 100 — the extras must drop, not block.
	done := make(chan struct{})
	go func() {
		for i := 0; i < 100; i++ {
			b.Publish(ItemsChanged)
		}
		close(done)
	}()
	select {
	case <-done:
		// good — Publish loop completed without blocking
	case <-time.After(200 * time.Millisecond):
		t.Fatal("Publish blocked on full subscriber buffer; should drop instead")
	}

	// Drain — should have between 1 and 16 events buffered.
	drained := 0
loop:
	for {
		select {
		case <-ch:
			drained++
		default:
			break loop
		}
	}
	if drained < 1 || drained > 16 {
		t.Errorf("drained = %d, want 1..16 (buffer size)", drained)
	}
}

func TestBusNilReceiverPublishIsNoOp(t *testing.T) {
	// Servers wired without an events.Bus should be safe to call.
	var b *Bus              // nil
	b.Publish(ItemsChanged) // must not panic
}

func TestBusConcurrentSubscribeAndPublish(t *testing.T) {
	b := NewBus()
	const N = 50
	var wg sync.WaitGroup
	for i := 0; i < N; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			ch, cancel := b.Subscribe()
			defer cancel()
			// Drain a few events so Publish doesn't drop everything.
			done := time.After(50 * time.Millisecond)
			for {
				select {
				case <-ch:
				case <-done:
					return
				}
			}
		}()
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 10; j++ {
				b.Publish(ItemsChanged)
			}
		}()
	}
	wg.Wait()
	// If we got here without a data race (run with -race) the bus is OK.
}

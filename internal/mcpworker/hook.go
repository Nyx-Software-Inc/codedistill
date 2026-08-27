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

package mcpworker

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"

	"codedistill/internal/domain"
	"codedistill/internal/id"
	"codedistill/internal/mcpmap"
	"codedistill/internal/storage"
)

// Op constants the API handlers pass to Notify. Use these instead of
// raw string literals so a typo doesn't silently produce an unmapped
// queue row.
const (
	OpCreate = "create"
	OpUpdate = "update"
	// OpStatusChange is a status transition — a refinement of update that
	// destinations can route specially (e.g. "close the ticket"). The worker
	// falls back to the update tool when a destination hasn't mapped it.
	OpStatusChange = "status_change"
	OpDelete       = "delete"
)

// Hook is the API-side glue: handlers call Notify after a successful
// local write; Hook checks if the user has a destination configured
// for that item type and, if so, enqueues a push + flips sync_status
// to 'pending'.
//
// No-op when no destination is configured. Errors are logged, never
// returned — the local write already succeeded; an enqueue failure
// shouldn't fail the API call. Worst case: the queue row didn't get
// written and the destination stays out of sync until the next edit.
type Hook struct {
	store  storage.Storage
	lookup ConfigLookup
	log    *slog.Logger
	// userID is the single-user dogfood owner — "local" today, swapped
	// for the request user once auth lands.
	userID string
}

func NewHook(store storage.Storage, lookup ConfigLookup, log *slog.Logger) *Hook {
	if log == nil {
		log = slog.Default()
	}
	return &Hook{
		store:  store,
		lookup: lookup,
		log:    log.With("component", "mcpexport"),
		userID: "local",
	}
}

// CanExport reports whether this hook's user has a destination
// configured for the short item type ("todo" | "bug" | "kb" |
// "use_case") with the given op mapped to a destination tool. Lets the
// migrate endpoint fail fast with a useful message instead of silently
// enqueueing nothing (Notify treats both cases as a no-op).
func (h *Hook) CanExport(ctx context.Context, shortType, op string) (bool, error) {
	if h == nil {
		return false, nil
	}
	cfg, err := h.lookup.Get(ctx, h.userID, shortType)
	if err != nil || cfg == nil {
		return false, err
	}
	return cfg.Mapping[mcpmap.Operation(op)] != "", nil
}

// Notify is the entry point handlers call after a successful create /
// update / delete. ownerType is the polymorphic discriminator
// ("todo_item" / "bug_item" / "knowledge_entry" / "use_case_item").
//
// payload is whatever the handler wants serialized as the args for the
// destination tool. Convention:
//   - create / update: pass the *domain.TodoItem (or peer) the handler
//     just wrote — its JSON shape is the natural payload.
//   - delete: pass a map like {"id": ownerID, "remote_id": rid} that
//     the handler built from a pre-delete read of GetItemSyncState.
//     The destination's delete tool needs the remote id; reading it
//     after delete is too late.
func (h *Hook) Notify(ctx context.Context, ownerType, ownerID, op string, payload any) {
	if h == nil {
		return
	}
	short, err := OwnerTypeToShort(ownerType)
	if err != nil {
		h.log.Error("notify: unknown owner_type", "owner_type", ownerType, "err", err)
		return
	}
	cfg, err := h.lookup.Get(ctx, h.userID, short)
	if err != nil {
		h.log.Error("notify: lookup", "owner_type", ownerType, "owner_id", ownerID, "err", err)
		return
	}
	if cfg == nil {
		// No destination configured for this user/type. Common case for
		// un-wired item types — silent no-op.
		return
	}
	// Even when a destination is configured, we may not have a tool for
	// this op. Skip enqueue if the op is unmapped — saves a worker
	// round-trip just to permanent-fail.
	if cfg.Mapping[mcpmap.Operation(op)] == "" {
		return
	}

	payloadJSON, err := json.Marshal(payload)
	if err != nil {
		h.log.Error("notify: marshal payload", "owner_type", ownerType, "owner_id", ownerID, "err", err)
		return
	}

	if err := h.store.MarkItemSyncPending(ctx, ownerType, ownerID); err != nil {
		// For deletes the item is already gone — ErrNotFound is fine.
		if op != OpDelete && !errors.Is(err, storage.ErrNotFound) {
			h.log.Warn("notify: mark pending", "owner_type", ownerType, "owner_id", ownerID, "err", err)
		}
	}

	row := &domain.ExportQueueItem{
		ID:        id.New(),
		UserID:    h.userID,
		OwnerType: ownerType,
		OwnerID:   ownerID,
		Op:        op,
		Payload:   payloadJSON,
		// NextAttemptAt zero → EnqueueExportItem sets it to now → due
		// on the worker's next tick.
	}
	if err := h.store.EnqueueExportItem(ctx, row); err != nil {
		h.log.Error("notify: enqueue", "owner_type", ownerType, "owner_id", ownerID, "op", op, "err", err)
	}
}

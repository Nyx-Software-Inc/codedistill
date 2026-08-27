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

package sqlite

import (
	"context"
	"database/sql"

	"codedistill/internal/domain"
)

// item_events is an append-only lineage log (UC-5). No update/delete path
// — events are immutable history.

func (s *Store) RecordItemEvent(ctx context.Context, e *domain.ItemEvent) error {
	var actor any
	if e.ActorUserID != "" {
		actor = e.ActorUserID
	}
	_, err := s.DB.ExecContext(ctx,
		`INSERT INTO item_events (id, owner_type, owner_id, kind, summary, body, source, actor_user_id, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		e.ID, e.OwnerType, e.OwnerID, e.Kind, e.Summary, e.Body, e.Source, actor, e.CreatedAt,
	)
	return err
}

// LatestNotes returns the most recent `note`-kind event for every owner that has
// one — the canvas "pulse" (item-editing canvas rework, slice B). Portable
// correlated subquery (works on SQLite + Postgres); ties on created_at may yield
// more than one row per owner, which the caller collapses. Only owner_type,
// owner_id, summary, created_at are populated.
func (s *Store) LatestNotes(ctx context.Context) ([]*domain.ItemEvent, error) {
	rows, err := s.DB.QueryContext(ctx, `
		SELECT owner_type, owner_id, summary, created_at
		  FROM item_events e
		 WHERE kind = 'note'
		   AND created_at = (
		       SELECT MAX(created_at) FROM item_events e2
		        WHERE e2.kind = 'note' AND e2.owner_type = e.owner_type AND e2.owner_id = e.owner_id
		   )`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*domain.ItemEvent
	for rows.Next() {
		ev := &domain.ItemEvent{Kind: "note"}
		if err := rows.Scan(&ev.OwnerType, &ev.OwnerID, &ev.Summary, &ev.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, ev)
	}
	return out, rows.Err()
}

// ReparentItemEvents moves an owner's events to a new owner. Append-only stays
// the rule for content; this is a structural re-home used when a reclassify
// re-derives the work item (slice 4) so its log survives the swap.
func (s *Store) ReparentItemEvents(ctx context.Context, oldOwnerType, oldOwnerID, newOwnerType, newOwnerID string) error {
	_, err := s.DB.ExecContext(ctx,
		`UPDATE item_events SET owner_type = ?, owner_id = ? WHERE owner_type = ? AND owner_id = ?`,
		newOwnerType, newOwnerID, oldOwnerType, oldOwnerID,
	)
	return err
}

// ListItemEvents returns one owner's events oldest-first. The API merges
// multiple owners (a source item + its derived item) for the full lineage.
func (s *Store) ListItemEvents(ctx context.Context, ownerType, ownerID string) ([]*domain.ItemEvent, error) {
	rows, err := s.DB.QueryContext(ctx,
		`SELECT id, owner_type, owner_id, kind, summary, body, source, actor_user_id, created_at
		   FROM item_events
		  WHERE owner_type = ? AND owner_id = ?
		  ORDER BY created_at ASC, id ASC`,
		ownerType, ownerID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*domain.ItemEvent
	for rows.Next() {
		e := &domain.ItemEvent{}
		var actor sql.NullString
		if err := rows.Scan(&e.ID, &e.OwnerType, &e.OwnerID, &e.Kind, &e.Summary, &e.Body, &e.Source, &actor, &e.CreatedAt); err != nil {
			return nil, err
		}
		e.ActorUserID = actor.String
		out = append(out, e)
	}
	return out, rows.Err()
}

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
	"errors"
	"time"
)

// group_notes is the dedicated home for a group frame's note (canvas rework
// slice C2) — moved off scratchpad_items.annotations so that column can retire.

func (s *Store) SetGroupNote(ctx context.Context, groupID, note string, updatedAt time.Time) error {
	_, err := s.DB.ExecContext(ctx,
		`INSERT INTO group_notes (group_id, note, updated_at) VALUES (?, ?, ?)
		 ON CONFLICT(group_id) DO UPDATE SET note = excluded.note, updated_at = excluded.updated_at`,
		groupID, note, updatedAt,
	)
	return err
}

func (s *Store) GetGroupNote(ctx context.Context, groupID string) (string, error) {
	var note string
	err := s.DB.QueryRowContext(ctx, `SELECT note FROM group_notes WHERE group_id = ?`, groupID).Scan(&note)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	return note, err
}

// GroupNotesByScratchpad returns group_id -> note for every group in a
// scratchpad. Used to populate group items' notes once the annotations column
// is dropped.
func (s *Store) GroupNotesByScratchpad(ctx context.Context, scratchpadID string) (map[string]string, error) {
	rows, err := s.DB.QueryContext(ctx,
		`SELECT gn.group_id, gn.note
		   FROM group_notes gn
		   JOIN scratchpad_items si ON si.id = gn.group_id
		  WHERE si.scratchpad_id = ?`, scratchpadID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]string{}
	for rows.Next() {
		var id, note string
		if err := rows.Scan(&id, &note); err != nil {
			return nil, err
		}
		out[id] = note
	}
	return out, rows.Err()
}

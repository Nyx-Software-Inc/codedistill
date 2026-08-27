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
	"fmt"
	"time"

	"codedistill/internal/domain"
	"codedistill/internal/id"
	"codedistill/internal/storage"
)

// MCP export queue + per-item sync-state mutators. Schema lives in
// migration 0013; these are the read/write paths the mcpworker
// background loop and the API hooks (step 5) call.

func (s *Store) EnqueueExportItem(ctx context.Context, item *domain.ExportQueueItem) error {
	if item == nil {
		return fmt.Errorf("enqueue: nil item")
	}
	if item.UserID == "" || item.OwnerType == "" || item.OwnerID == "" || item.Op == "" {
		return fmt.Errorf("enqueue: user_id, owner_type, owner_id, op all required")
	}
	if len(item.Payload) == 0 {
		return fmt.Errorf("enqueue: payload required")
	}
	if item.ID == "" {
		item.ID = id.New()
	}
	now := time.Now().UTC()
	if item.CreatedAt.IsZero() {
		item.CreatedAt = now
	}
	if item.UpdatedAt.IsZero() {
		item.UpdatedAt = now
	}
	if item.NextAttemptAt.IsZero() {
		item.NextAttemptAt = now
	}
	_, err := s.DB.ExecContext(ctx, `
		INSERT INTO mcp_export_queue
			(id, user_id, owner_type, owner_id, op, payload,
			 attempts, next_attempt_at, last_error, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		item.ID, item.UserID, item.OwnerType, item.OwnerID, item.Op,
		string(item.Payload), item.Attempts, item.NextAttemptAt,
		item.LastError, item.CreatedAt, item.UpdatedAt,
	)
	if err != nil {
		return fmt.Errorf("enqueue export item: %w", err)
	}
	return nil
}

func (s *Store) ListDueExportItems(ctx context.Context, before time.Time, limit int) ([]*domain.ExportQueueItem, error) {
	if limit <= 0 {
		limit = 10
	}
	rows, err := s.DB.QueryContext(ctx, `
		SELECT id, user_id, owner_type, owner_id, op, payload,
		       attempts, next_attempt_at, last_error, created_at, updated_at
		FROM mcp_export_queue
		WHERE next_attempt_at <= ?
		ORDER BY next_attempt_at ASC, id ASC
		LIMIT ?`,
		before, limit,
	)
	if err != nil {
		return nil, fmt.Errorf("list due export items: %w", err)
	}
	defer rows.Close()

	var out []*domain.ExportQueueItem
	for rows.Next() {
		item := &domain.ExportQueueItem{}
		var payload string
		if err := rows.Scan(
			&item.ID, &item.UserID, &item.OwnerType, &item.OwnerID, &item.Op,
			&payload, &item.Attempts, &item.NextAttemptAt, &item.LastError,
			&item.CreatedAt, &item.UpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan export item: %w", err)
		}
		item.Payload = []byte(payload)
		out = append(out, item)
	}
	return out, rows.Err()
}

func (s *Store) DeleteExportQueueItem(ctx context.Context, queueID string) error {
	res, err := s.DB.ExecContext(ctx, `DELETE FROM mcp_export_queue WHERE id = ?`, queueID)
	if err != nil {
		return fmt.Errorf("delete export queue item: %w", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return fmt.Errorf("export queue item %q: %w", queueID, storage.ErrNotFound)
	}
	return nil
}

func (s *Store) RetryExportQueueItem(ctx context.Context, queueID string, attempts int, nextAt time.Time, lastError string) error {
	res, err := s.DB.ExecContext(ctx, `
		UPDATE mcp_export_queue
		SET attempts = ?, next_attempt_at = ?, last_error = ?, updated_at = ?
		WHERE id = ?`,
		attempts, nextAt, lastError, time.Now().UTC(), queueID,
	)
	if err != nil {
		return fmt.Errorf("retry export queue item: %w", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return fmt.Errorf("export queue item %q: %w", queueID, storage.ErrNotFound)
	}
	return nil
}

// itemTableFor returns the table name and updated_at column behavior for
// a polymorphic owner_type. The four item-type tables share the four
// sync columns (added by migration 0013) but live in distinct tables.
func itemTableFor(ownerType string) (string, error) {
	switch ownerType {
	case "todo_item":
		return "todo_items", nil
	case "bug_item":
		return "bug_items", nil
	case "knowledge_entry":
		return "knowledge_entries", nil
	case "use_case_item":
		return "use_case_items", nil
	default:
		return "", fmt.Errorf("unknown owner_type %q", ownerType)
	}
}

func (s *Store) MarkItemSynced(ctx context.Context, ownerType, ownerID, remoteID string, at time.Time) error {
	table, err := itemTableFor(ownerType)
	if err != nil {
		return err
	}
	// Two query variants: with and without remote_id update. Avoids
	// clobbering an existing remote_id when the op is update/delete.
	var res sql.Result
	if remoteID != "" {
		res, err = s.DB.ExecContext(ctx, `
			UPDATE `+table+`
			SET sync_status = 'synced',
			    last_sync_at = ?,
			    last_sync_error = '',
			    remote_id = ?
			WHERE id = ?`,
			at, remoteID, ownerID,
		)
	} else {
		res, err = s.DB.ExecContext(ctx, `
			UPDATE `+table+`
			SET sync_status = 'synced',
			    last_sync_at = ?,
			    last_sync_error = ''
			WHERE id = ?`,
			at, ownerID,
		)
	}
	if err != nil {
		return fmt.Errorf("mark synced %s/%s: %w", ownerType, ownerID, err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		// Item may have been deleted locally between enqueue and worker
		// run. Not an error from the worker's perspective — treat as
		// "nothing to do." Caller can log if it cares.
		return errors.Join(storage.ErrNotFound, fmt.Errorf("%s id=%s", ownerType, ownerID))
	}
	return nil
}

func (s *Store) MarkItemSyncFailed(ctx context.Context, ownerType, ownerID, errMsg string, at time.Time) error {
	table, err := itemTableFor(ownerType)
	if err != nil {
		return err
	}
	res, err := s.DB.ExecContext(ctx, `
		UPDATE `+table+`
		SET sync_status = 'failed',
		    last_sync_at = ?,
		    last_sync_error = ?
		WHERE id = ?`,
		at, errMsg, ownerID,
	)
	if err != nil {
		return fmt.Errorf("mark sync failed %s/%s: %w", ownerType, ownerID, err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return errors.Join(storage.ErrNotFound, fmt.Errorf("%s id=%s", ownerType, ownerID))
	}
	return nil
}

func (s *Store) GetItemSyncState(ctx context.Context, ownerType, ownerID string) (string, string, string, error) {
	table, err := itemTableFor(ownerType)
	if err != nil {
		return "", "", "", err
	}
	row := s.DB.QueryRowContext(ctx, `SELECT remote_id, sync_status, last_sync_error FROM `+table+` WHERE id = ?`, ownerID)
	var remoteID, status, lastErr string
	if err := row.Scan(&remoteID, &status, &lastErr); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return "", "", "", fmt.Errorf("%s id=%s: %w", ownerType, ownerID, storage.ErrNotFound)
		}
		return "", "", "", err
	}
	return remoteID, status, lastErr, nil
}

func (s *Store) MarkItemSyncPending(ctx context.Context, ownerType, ownerID string) error {
	table, err := itemTableFor(ownerType)
	if err != nil {
		return err
	}
	res, err := s.DB.ExecContext(ctx, `
		UPDATE `+table+`
		SET sync_status = 'pending',
		    last_sync_error = ''
		WHERE id = ?`,
		ownerID,
	)
	if err != nil {
		return fmt.Errorf("mark sync pending %s/%s: %w", ownerType, ownerID, err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return errors.Join(storage.ErrNotFound, fmt.Errorf("%s id=%s", ownerType, ownerID))
	}
	return nil
}

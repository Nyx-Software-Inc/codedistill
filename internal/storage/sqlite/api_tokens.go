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
	"fmt"
	"time"

	"codedistill/internal/domain"
	"codedistill/internal/storage"
)

// CreateAPIToken stores a per-user token. The caller has hashed the raw token.
func (s *Store) CreateAPIToken(ctx context.Context, t *domain.APIToken) error {
	_, err := s.DB.ExecContext(ctx,
		`INSERT INTO api_tokens (id, user_id, token_hash, name, created_at, last_used_at)
		 VALUES (?, ?, ?, ?, ?, ?)`,
		t.ID, t.UserID, t.TokenHash, t.Name, t.CreatedAt, t.LastUsedAt)
	if err != nil {
		return fmt.Errorf("create api token: %w", err)
	}
	return nil
}

// GetAPITokenUser resolves a token hash to its owning user id (auth path). Also
// returns last_used_at (nil if never) so the caller can throttle TouchAPIToken
// instead of writing on every request.
func (s *Store) GetAPITokenUser(ctx context.Context, tokenHash string) (string, *time.Time, error) {
	var userID string
	var lastUsed sql.NullTime
	err := s.DB.QueryRowContext(ctx,
		`SELECT user_id, last_used_at FROM api_tokens WHERE token_hash = ?`, tokenHash).Scan(&userID, &lastUsed)
	if err != nil {
		return "", nil, fmt.Errorf("api token: %w", storage.ErrNotFound)
	}
	var lu *time.Time
	if lastUsed.Valid {
		v := lastUsed.Time
		lu = &v
	}
	return userID, lu, nil
}

// ListAPITokens returns a user's tokens (no secrets), newest first.
func (s *Store) ListAPITokens(ctx context.Context, userID string) ([]*domain.APIToken, error) {
	rows, err := s.DB.QueryContext(ctx,
		`SELECT id, user_id, name, created_at, last_used_at
		   FROM api_tokens WHERE user_id = ? ORDER BY created_at DESC`, userID)
	if err != nil {
		return nil, fmt.Errorf("list api tokens: %w", err)
	}
	defer rows.Close()
	out := []*domain.APIToken{}
	for rows.Next() {
		t := &domain.APIToken{}
		var lastUsed sql.NullTime
		if err := rows.Scan(&t.ID, &t.UserID, &t.Name, &t.CreatedAt, &lastUsed); err != nil {
			return nil, err
		}
		if lastUsed.Valid {
			v := lastUsed.Time
			t.LastUsedAt = &v
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// DeleteAPIToken revokes a token, scoped to its owner (so a user can only delete
// their own). No-op (no error) if it doesn't exist or isn't theirs.
func (s *Store) DeleteAPIToken(ctx context.Context, id, userID string) error {
	_, err := s.DB.ExecContext(ctx, `DELETE FROM api_tokens WHERE id = ? AND user_id = ?`, id, userID)
	return err
}

// TouchAPIToken records last use. Best-effort.
func (s *Store) TouchAPIToken(ctx context.Context, tokenHash string, now time.Time) error {
	_, err := s.DB.ExecContext(ctx, `UPDATE api_tokens SET last_used_at = ? WHERE token_hash = ?`, now, tokenHash)
	return err
}

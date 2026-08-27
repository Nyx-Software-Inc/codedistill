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
	"fmt"
	"time"

	"codedistill/internal/domain"
	"codedistill/internal/storage"
)

// CreateSession persists a login session. The caller has already hashed the
// cookie token into ID.
func (s *Store) CreateSession(ctx context.Context, sess *domain.Session) error {
	_, err := s.DB.ExecContext(ctx,
		`INSERT INTO sessions (id, user_id, created_at, last_active_at, expires_at, user_agent)
		 VALUES (?, ?, ?, ?, ?, ?)`,
		sess.ID, sess.UserID, sess.CreatedAt, sess.LastActiveAt, sess.ExpiresAt, sess.UserAgent)
	if err != nil {
		return fmt.Errorf("create session: %w", err)
	}
	return nil
}

// GetValidSession returns a session by id only if it hasn't expired. Expired or
// absent sessions return ErrNotFound (the caller treats both as "not logged in").
func (s *Store) GetValidSession(ctx context.Context, id string, now time.Time) (*domain.Session, error) {
	sess := &domain.Session{}
	err := s.DB.QueryRowContext(ctx,
		`SELECT id, user_id, created_at, last_active_at, expires_at, user_agent
		   FROM sessions WHERE id = ? AND expires_at > ?`, id, now).
		Scan(&sess.ID, &sess.UserID, &sess.CreatedAt, &sess.LastActiveAt, &sess.ExpiresAt, &sess.UserAgent)
	if err != nil {
		return nil, fmt.Errorf("session %s: %w", id, storage.ErrNotFound)
	}
	return sess, nil
}

// TouchSession bumps last_active_at AND rolls expires_at forward — a session
// used within its lifetime slides (the sessionTTL comment's stated intent). Its
// one caller throttles this so it isn't a per-request write. Best-effort.
func (s *Store) TouchSession(ctx context.Context, id string, now, expiresAt time.Time) error {
	_, err := s.DB.ExecContext(ctx,
		`UPDATE sessions SET last_active_at = ?, expires_at = ? WHERE id = ?`, now, expiresAt, id)
	return err
}

// DeleteExpiredSessions prunes sessions past their expiry (already rejected by
// GetValidSession; this stops the rows accumulating). Returns how many were
// removed. Best-effort hygiene, run on a schedule.
func (s *Store) DeleteExpiredSessions(ctx context.Context, now time.Time) (int64, error) {
	res, err := s.DB.ExecContext(ctx, `DELETE FROM sessions WHERE expires_at <= ?`, now)
	if err != nil {
		return 0, err
	}
	n, _ := res.RowsAffected()
	return n, nil
}

// DeleteSession revokes one session (logout).
func (s *Store) DeleteSession(ctx context.Context, id string) error {
	_, err := s.DB.ExecContext(ctx, `DELETE FROM sessions WHERE id = ?`, id)
	return err
}

// DeleteUserSessions revokes all of a user's sessions ("sign out everywhere",
// and on deactivation).
func (s *Store) DeleteUserSessions(ctx context.Context, userID string) error {
	_, err := s.DB.ExecContext(ctx, `DELETE FROM sessions WHERE user_id = ?`, userID)
	return err
}

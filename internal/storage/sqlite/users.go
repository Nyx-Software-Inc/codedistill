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

	"codedistill/internal/domain"
	"codedistill/internal/storage"
)

const userColumns = `id, email, display_name, canonical_name, avatar, external_id, provider, created_at`

func scanUser(scan func(...any) error) (*domain.User, error) {
	u := &domain.User{}
	var (
		email, avatar, extID, provider sql.NullString
	)
	if err := scan(&u.ID, &email, &u.DisplayName, &u.CanonicalName, &avatar, &extID, &provider, &u.CreatedAt); err != nil {
		return nil, err
	}
	u.Email = stringOrEmpty(email)
	u.Avatar = stringOrEmpty(avatar)
	u.ExternalID = stringOrEmpty(extID)
	u.Provider = stringOrEmpty(provider)
	return u, nil
}

// GetUserByExternal looks up a user by their identity-provider subject —
// (provider, external_id) — the stable key for SSO logins (uses idx_users_external).
func (s *Store) GetUserByExternal(ctx context.Context, provider, externalID string) (*domain.User, error) {
	row := s.DB.QueryRowContext(ctx,
		`SELECT `+userColumns+` FROM users WHERE provider = ? AND external_id = ?`, provider, externalID)
	u, err := scanUser(row.Scan)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("user %s/%s: %w", provider, externalID, storage.ErrNotFound)
	}
	if err != nil {
		return nil, err
	}
	return u, nil
}

func (s *Store) CreateUser(ctx context.Context, u *domain.User) error {
	_, err := s.DB.ExecContext(ctx,
		`INSERT INTO users (`+userColumns+`) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		u.ID, nullString(u.Email), u.DisplayName, u.CanonicalName, nullString(u.Avatar),
		nullString(u.ExternalID), nullString(u.Provider), u.CreatedAt,
	)
	return err
}

func (s *Store) GetUser(ctx context.Context, id string) (*domain.User, error) {
	row := s.DB.QueryRowContext(ctx,
		`SELECT `+userColumns+` FROM users WHERE id = ?`, id,
	)
	u, err := scanUser(row.Scan)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("user %s: %w", id, storage.ErrNotFound)
	}
	if err != nil {
		return nil, err
	}
	return u, nil
}

func (s *Store) UpdateUser(ctx context.Context, u *domain.User) error {
	res, err := s.DB.ExecContext(ctx,
		`UPDATE users SET email = ?, display_name = ?, canonical_name = ?, avatar = ?, external_id = ?, provider = ? WHERE id = ?`,
		nullString(u.Email), u.DisplayName, u.CanonicalName, nullString(u.Avatar),
		nullString(u.ExternalID), nullString(u.Provider), u.ID,
	)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return fmt.Errorf("user %s: %w", u.ID, storage.ErrNotFound)
	}
	return nil
}

func (s *Store) DeleteUser(ctx context.Context, id string) error {
	res, err := s.DB.ExecContext(ctx, `DELETE FROM users WHERE id = ?`, id)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return fmt.Errorf("user %s: %w", id, storage.ErrNotFound)
	}
	return nil
}

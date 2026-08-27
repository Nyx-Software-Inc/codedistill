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
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"codedistill/internal/domain"
	"codedistill/internal/storage"
)

// Settings storage for user_settings and project_settings. Both tables share
// the same shape: (id_column, key, value TEXT, updated_at). Value is JSON-
// encoded text — primitives and structured values share one column. Set is
// upsert (INSERT ... ON CONFLICT DO UPDATE).

// --- user_settings ---

func (s *Store) GetUserSetting(ctx context.Context, userID, key string) (*domain.UserSetting, error) {
	row := s.DB.QueryRowContext(ctx,
		`SELECT user_id, key, value, updated_at FROM user_settings WHERE user_id = ? AND key = ?`,
		userID, key,
	)
	out := &domain.UserSetting{}
	var raw string
	if err := row.Scan(&out.UserID, &out.Key, &raw, &out.UpdatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("user_setting (%s, %s): %w", userID, key, storage.ErrNotFound)
		}
		return nil, err
	}
	out.Value = json.RawMessage(raw)
	return out, nil
}

func (s *Store) SetUserSetting(ctx context.Context, st *domain.UserSetting) error {
	if st.UpdatedAt.IsZero() {
		st.UpdatedAt = time.Now().UTC()
	}
	if len(st.Value) == 0 {
		return fmt.Errorf("user_setting %q: value required", st.Key)
	}
	_, err := s.DB.ExecContext(ctx,
		`INSERT INTO user_settings (user_id, key, value, updated_at)
		 VALUES (?, ?, ?, ?)
		 ON CONFLICT (user_id, key) DO UPDATE SET
			value = excluded.value, updated_at = excluded.updated_at`,
		st.UserID, st.Key, string(st.Value), st.UpdatedAt,
	)
	return err
}

func (s *Store) DeleteUserSetting(ctx context.Context, userID, key string) error {
	res, err := s.DB.ExecContext(ctx,
		`DELETE FROM user_settings WHERE user_id = ? AND key = ?`, userID, key,
	)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return fmt.Errorf("user_setting (%s, %s): %w", userID, key, storage.ErrNotFound)
	}
	return nil
}

func (s *Store) ListUserSettings(ctx context.Context, userID string) ([]*domain.UserSetting, error) {
	rows, err := s.DB.QueryContext(ctx,
		`SELECT user_id, key, value, updated_at FROM user_settings WHERE user_id = ? ORDER BY key`,
		userID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*domain.UserSetting
	for rows.Next() {
		st := &domain.UserSetting{}
		var raw string
		if err := rows.Scan(&st.UserID, &st.Key, &raw, &st.UpdatedAt); err != nil {
			return nil, err
		}
		st.Value = json.RawMessage(raw)
		out = append(out, st)
	}
	return out, rows.Err()
}

// --- project_settings ---

func (s *Store) GetProjectSetting(ctx context.Context, projectID, key string) (*domain.ProjectSetting, error) {
	row := s.DB.QueryRowContext(ctx,
		`SELECT project_id, key, value, updated_at FROM project_settings WHERE project_id = ? AND key = ?`,
		projectID, key,
	)
	out := &domain.ProjectSetting{}
	var raw string
	if err := row.Scan(&out.ProjectID, &out.Key, &raw, &out.UpdatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("project_setting (%s, %s): %w", projectID, key, storage.ErrNotFound)
		}
		return nil, err
	}
	out.Value = json.RawMessage(raw)
	return out, nil
}

func (s *Store) SetProjectSetting(ctx context.Context, st *domain.ProjectSetting) error {
	if st.UpdatedAt.IsZero() {
		st.UpdatedAt = time.Now().UTC()
	}
	if len(st.Value) == 0 {
		return fmt.Errorf("project_setting %q: value required", st.Key)
	}
	_, err := s.DB.ExecContext(ctx,
		`INSERT INTO project_settings (project_id, key, value, updated_at)
		 VALUES (?, ?, ?, ?)
		 ON CONFLICT (project_id, key) DO UPDATE SET
			value = excluded.value, updated_at = excluded.updated_at`,
		st.ProjectID, st.Key, string(st.Value), st.UpdatedAt,
	)
	return err
}

func (s *Store) DeleteProjectSetting(ctx context.Context, projectID, key string) error {
	res, err := s.DB.ExecContext(ctx,
		`DELETE FROM project_settings WHERE project_id = ? AND key = ?`, projectID, key,
	)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return fmt.Errorf("project_setting (%s, %s): %w", projectID, key, storage.ErrNotFound)
	}
	return nil
}

func (s *Store) ListProjectSettings(ctx context.Context, projectID string) ([]*domain.ProjectSetting, error) {
	rows, err := s.DB.QueryContext(ctx,
		`SELECT project_id, key, value, updated_at FROM project_settings WHERE project_id = ? ORDER BY key`,
		projectID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*domain.ProjectSetting
	for rows.Next() {
		st := &domain.ProjectSetting{}
		var raw string
		if err := rows.Scan(&st.ProjectID, &st.Key, &raw, &st.UpdatedAt); err != nil {
			return nil, err
		}
		st.Value = json.RawMessage(raw)
		out = append(out, st)
	}
	return out, rows.Err()
}

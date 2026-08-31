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
	"encoding/json"
	"fmt"

	"codedistill/internal/domain"
	"codedistill/internal/storage"
)

const customFieldDefColumns = `id, project_id, name, field_type, options, applies_to, position, created_at, updated_at`

func scanCustomFieldDef(scan func(...any) error) (*domain.CustomFieldDef, error) {
	d := &domain.CustomFieldDef{}
	var options, appliesTo string
	if err := scan(&d.ID, &d.ProjectID, &d.Name, &d.FieldType, &options, &appliesTo, &d.Position, &d.CreatedAt, &d.UpdatedAt); err != nil {
		return nil, err
	}
	_ = json.Unmarshal([]byte(options), &d.Options)
	_ = json.Unmarshal([]byte(appliesTo), &d.AppliesTo)
	if d.Options == nil {
		d.Options = []string{}
	}
	if d.AppliesTo == nil {
		d.AppliesTo = []string{}
	}
	return d, nil
}

// ListCustomFieldDefs returns a project's field definitions, ordered.
func (s *Store) ListCustomFieldDefs(ctx context.Context, projectID string) ([]*domain.CustomFieldDef, error) {
	rows, err := s.DB.QueryContext(ctx,
		`SELECT `+customFieldDefColumns+` FROM custom_field_defs WHERE project_id = ? ORDER BY position, created_at`, projectID)
	if err != nil {
		return nil, fmt.Errorf("list custom field defs: %w", err)
	}
	defer rows.Close()
	out := []*domain.CustomFieldDef{}
	for rows.Next() {
		d, err := scanCustomFieldDef(rows.Scan)
		if err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

// GetCustomFieldDef fetches one definition by id.
func (s *Store) GetCustomFieldDef(ctx context.Context, id string) (*domain.CustomFieldDef, error) {
	d, err := scanCustomFieldDef(s.DB.QueryRowContext(ctx,
		`SELECT `+customFieldDefColumns+` FROM custom_field_defs WHERE id = ?`, id).Scan)
	if err != nil {
		return nil, fmt.Errorf("custom field def %s: %w", id, storage.ErrNotFound)
	}
	return d, nil
}

func (s *Store) CreateCustomFieldDef(ctx context.Context, d *domain.CustomFieldDef) error {
	opts, _ := json.Marshal(orEmpty(d.Options))
	appl, _ := json.Marshal(orEmpty(d.AppliesTo))
	_, err := s.DB.ExecContext(ctx,
		`INSERT INTO custom_field_defs (`+customFieldDefColumns+`) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		d.ID, d.ProjectID, d.Name, d.FieldType, string(opts), string(appl), d.Position, d.CreatedAt, d.UpdatedAt)
	if err != nil {
		return fmt.Errorf("create custom field def: %w", err)
	}
	return nil
}

// UpdateCustomFieldDef writes the mutable fields (name/type/options/applies_to/
// position) + updated_at; project is immutable.
func (s *Store) UpdateCustomFieldDef(ctx context.Context, d *domain.CustomFieldDef) error {
	opts, _ := json.Marshal(orEmpty(d.Options))
	appl, _ := json.Marshal(orEmpty(d.AppliesTo))
	res, err := s.DB.ExecContext(ctx,
		`UPDATE custom_field_defs SET name = ?, field_type = ?, options = ?, applies_to = ?, position = ?, updated_at = ? WHERE id = ?`,
		d.Name, d.FieldType, string(opts), string(appl), d.Position, d.UpdatedAt, d.ID)
	if err != nil {
		return fmt.Errorf("update custom field def: %w", err)
	}
	if c, _ := res.RowsAffected(); c == 0 {
		return fmt.Errorf("custom field def %s: %w", d.ID, storage.ErrNotFound)
	}
	return nil
}

// DeleteCustomFieldDef removes a definition and all its values. Values are
// dropped explicitly (belt-and-braces alongside the FK cascade).
func (s *Store) DeleteCustomFieldDef(ctx context.Context, id string) error {
	// One transaction so a failure between the two can't strip every item's
	// values for a field definition that then survives — the field would still
	// be offered while the data behind it was silently gone.
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("delete custom field def: begin: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	if _, err := tx.ExecContext(ctx, `DELETE FROM custom_field_values WHERE field_id = ?`, id); err != nil {
		return fmt.Errorf("delete custom field values: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM custom_field_defs WHERE id = ?`, id); err != nil {
		return fmt.Errorf("delete custom field def: %w", err)
	}
	return tx.Commit()
}

// ListCustomFieldValues returns one item's custom-field values (field_id→value).
func (s *Store) ListCustomFieldValues(ctx context.Context, ownerType, ownerID string) ([]*domain.CustomFieldValue, error) {
	rows, err := s.DB.QueryContext(ctx,
		`SELECT field_id, owner_type, owner_id, value, updated_at FROM custom_field_values WHERE owner_type = ? AND owner_id = ?`,
		ownerType, ownerID)
	if err != nil {
		return nil, fmt.Errorf("list custom field values: %w", err)
	}
	defer rows.Close()
	out := []*domain.CustomFieldValue{}
	for rows.Next() {
		v := &domain.CustomFieldValue{}
		if err := rows.Scan(&v.FieldID, &v.OwnerType, &v.OwnerID, &v.Value, &v.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

// SetCustomFieldValue upserts a value; an empty value clears it (delete) so we
// don't accumulate blank rows.
func (s *Store) SetCustomFieldValue(ctx context.Context, v *domain.CustomFieldValue) error {
	if v.Value == "" {
		_, err := s.DB.ExecContext(ctx,
			`DELETE FROM custom_field_values WHERE field_id = ? AND owner_type = ? AND owner_id = ?`,
			v.FieldID, v.OwnerType, v.OwnerID)
		if err != nil {
			return fmt.Errorf("clear custom field value: %w", err)
		}
		return nil
	}
	_, err := s.DB.ExecContext(ctx,
		`INSERT INTO custom_field_values (field_id, owner_type, owner_id, value, updated_at)
		 VALUES (?, ?, ?, ?, ?)
		 ON CONFLICT(field_id, owner_type, owner_id) DO UPDATE SET value = excluded.value, updated_at = excluded.updated_at`,
		v.FieldID, v.OwnerType, v.OwnerID, v.Value, v.UpdatedAt)
	if err != nil {
		return fmt.Errorf("set custom field value: %w", err)
	}
	return nil
}

func orEmpty(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}

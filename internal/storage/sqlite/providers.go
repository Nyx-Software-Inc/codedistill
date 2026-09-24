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
)

const providerColumns = `id, name, protocol, endpoint, model, api_key,
	context_tokens, is_local, enabled, last_ok_at, last_error, created_at, updated_at`

// CreateModelProvider stores a connection. The caller encrypts api_key before
// it arrives here: this layer must not be the place a plaintext key can slip
// through by someone forgetting a step.
func (s *Store) CreateModelProvider(ctx context.Context, p *domain.ModelProvider) error {
	if !domain.ValidProtocol(p.Protocol) {
		return fmt.Errorf("create provider: unknown protocol %q", p.Protocol)
	}
	if p.Name == "" || p.Endpoint == "" || p.Model == "" {
		return fmt.Errorf("create provider: name, endpoint and model are required")
	}
	_, err := s.DB.ExecContext(ctx,
		`INSERT INTO model_providers (`+providerColumns+`)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		p.ID, p.Name, p.Protocol, p.Endpoint, p.Model, p.APIKey,
		p.ContextTokens, boolInt(p.IsLocal), boolInt(p.Enabled),
		nil, nil, p.CreatedAt, p.UpdatedAt)
	if err != nil {
		return fmt.Errorf("create provider: %w", err)
	}
	return nil
}

// UpdateModelProvider replaces the editable fields.
//
// An EMPTY api_key leaves the stored one alone. The UI sends back what it was
// shown, and it is never shown a key — so treating empty as "clear it" would
// silently wipe the credential on every unrelated edit.
func (s *Store) UpdateModelProvider(ctx context.Context, p *domain.ModelProvider) error {
	if !domain.ValidProtocol(p.Protocol) {
		return fmt.Errorf("update provider: unknown protocol %q", p.Protocol)
	}
	q := `UPDATE model_providers SET name = ?, protocol = ?, endpoint = ?,
	        model = ?, context_tokens = ?, is_local = ?, enabled = ?, updated_at = ?`
	args := []any{p.Name, p.Protocol, p.Endpoint, p.Model, p.ContextTokens,
		boolInt(p.IsLocal), boolInt(p.Enabled), p.UpdatedAt}
	if p.APIKey != "" {
		q += `, api_key = ?`
		args = append(args, p.APIKey)
	}
	q += ` WHERE id = ?`
	args = append(args, p.ID)

	if _, err := s.DB.ExecContext(ctx, q, args...); err != nil {
		return fmt.Errorf("update provider: %w", err)
	}
	return nil
}

// ClearProviderAPIKey removes a stored credential. Explicit, because an empty
// update deliberately means "leave it".
func (s *Store) ClearProviderAPIKey(ctx context.Context, id string, at time.Time) error {
	_, err := s.DB.ExecContext(ctx,
		`UPDATE model_providers SET api_key = '', updated_at = ? WHERE id = ?`, at, id)
	return err
}

func (s *Store) DeleteModelProvider(ctx context.Context, id string) error {
	_, err := s.DB.ExecContext(ctx, `DELETE FROM model_providers WHERE id = ?`, id)
	return err
}

// GetModelProvider returns one provider WITH its encrypted key, for the code
// that must build a client. Callers that serialise outward use ListModelProviders.
func (s *Store) GetModelProvider(ctx context.Context, id string) (*domain.ModelProvider, error) {
	row := s.DB.QueryRowContext(ctx, `SELECT `+providerColumns+` FROM model_providers WHERE id = ?`, id)
	p, err := scanProvider(row)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return p, err
}

// ListModelProviders returns every provider, newest first.
func (s *Store) ListModelProviders(ctx context.Context) ([]*domain.ModelProvider, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT `+providerColumns+` FROM model_providers ORDER BY created_at`)
	if err != nil {
		return nil, fmt.Errorf("list providers: %w", err)
	}
	defer rows.Close()
	var out []*domain.ModelProvider
	for rows.Next() {
		p, err := scanProvider(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// RecordProviderHealth stamps the outcome of a probe, so the UI can show which
// connections actually work rather than only which are configured.
func (s *Store) RecordProviderHealth(ctx context.Context, id string, ok bool, errMsg string, at time.Time) error {
	if ok {
		_, err := s.DB.ExecContext(ctx,
			`UPDATE model_providers SET last_ok_at = ?, last_error = '' WHERE id = ?`, at, id)
		return err
	}
	_, err := s.DB.ExecContext(ctx,
		`UPDATE model_providers SET last_error = ? WHERE id = ?`, errMsg, id)
	return err
}

// SetModelRole points a role at a provider. projectID empty sets the global
// default.
func (s *Store) SetWorkerModel(ctx context.Context, workerType, projectID, providerID string, at time.Time) error {
	if !domain.ValidWorkerType(workerType) {
		return fmt.Errorf("set worker model: unknown worker type %q", workerType)
	}
	// The unique indexes are partial (one for project scope, one for global),
	// so ON CONFLICT cannot name a single constraint. Delete-then-insert in a
	// transaction is the portable form and costs nothing at this cardinality.
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if projectID == "" {
		_, err = tx.ExecContext(ctx, `DELETE FROM workflow_workers WHERE worker_type = ? AND project_id IS NULL`, workerType)
	} else {
		_, err = tx.ExecContext(ctx, `DELETE FROM workflow_workers WHERE worker_type = ? AND project_id = ?`, workerType, projectID)
	}
	if err != nil {
		return fmt.Errorf("set worker model: %w", err)
	}
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO workflow_workers (worker_type, project_id, provider_id, updated_at) VALUES (?, ?, ?, ?)`,
		workerType, nullable(projectID), providerID, at); err != nil {
		return fmt.Errorf("set worker model: %w", err)
	}
	return tx.Commit()
}

// ClearModelRole removes a binding, falling the role back to the next scope.
func (s *Store) ClearWorkerModel(ctx context.Context, workerType, projectID string) error {
	if projectID == "" {
		_, err := s.DB.ExecContext(ctx, `DELETE FROM workflow_workers WHERE worker_type = ? AND project_id IS NULL`, workerType)
		return err
	}
	_, err := s.DB.ExecContext(ctx, `DELETE FROM workflow_workers WHERE worker_type = ? AND project_id = ?`, workerType, projectID)
	return err
}

// ResolveModelRole answers "which provider does this job, for this project".
//
// Project binding wins, then the global default, then nothing — which means the
// caller falls back to the app's configured model. That last case is what keeps
// an existing install working with nothing configured at all.
func (s *Store) ResolveWorkerModel(ctx context.Context, workerType, projectID string) (*domain.ModelProvider, error) {
	if projectID != "" {
		p, err := s.providerForWorker(ctx, `SELECT `+prefixed()+` FROM workflow_workers w
			JOIN model_providers p ON p.id = w.provider_id
			WHERE w.worker_type = ? AND w.project_id = ?`, workerType, projectID)
		if err != nil || p != nil {
			return p, err
		}
	}
	return s.providerForWorker(ctx, `SELECT `+prefixed()+` FROM workflow_workers w
		JOIN model_providers p ON p.id = w.provider_id
		WHERE w.worker_type = ? AND w.project_id IS NULL`, workerType)
}

// ListModelRoles returns the bindings in a scope, keyed by role.
func (s *Store) ListWorkerModels(ctx context.Context, projectID string) (map[string]string, error) {
	var rows *sql.Rows
	var err error
	if projectID == "" {
		rows, err = s.DB.QueryContext(ctx, `SELECT worker_type, provider_id FROM workflow_workers WHERE project_id IS NULL`)
	} else {
		rows, err = s.DB.QueryContext(ctx, `SELECT worker_type, provider_id FROM workflow_workers WHERE project_id = ?`, projectID)
	}
	if err != nil {
		return nil, fmt.Errorf("list worker models: %w", err)
	}
	defer rows.Close()
	out := map[string]string{}
	for rows.Next() {
		var wt, pid string
		if err := rows.Scan(&wt, &pid); err != nil {
			return nil, err
		}
		out[wt] = pid
	}
	return out, rows.Err()
}

func (s *Store) providerForWorker(ctx context.Context, q string, args ...any) (*domain.ModelProvider, error) {
	p, err := scanProvider(s.DB.QueryRowContext(ctx, q, args...))
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return p, err
}

// prefixed qualifies the provider columns for the join above.
func prefixed() string {
	return `p.id, p.name, p.protocol, p.endpoint, p.model, p.api_key,
		p.context_tokens, p.is_local, p.enabled, p.last_ok_at, p.last_error,
		p.created_at, p.updated_at`
}

func scanProvider(r rowScanner) (*domain.ModelProvider, error) {
	p := &domain.ModelProvider{}
	var lastErr sql.NullString
	var lastOK sql.NullTime
	var isLocal, enabled int
	if err := r.Scan(&p.ID, &p.Name, &p.Protocol, &p.Endpoint, &p.Model, &p.APIKey,
		&p.ContextTokens, &isLocal, &enabled, &lastOK, &lastErr,
		&p.CreatedAt, &p.UpdatedAt); err != nil {
		return nil, err
	}
	p.IsLocal, p.Enabled = isLocal != 0, enabled != 0
	p.LastError = lastErr.String
	// The wire form carries only WHETHER a key exists. The key itself never
	// leaves this process except to the provider it belongs to.
	p.HasAPIKey = p.APIKey != ""
	if lastOK.Valid {
		t := lastOK.Time
		p.LastOKAt = &t
	}
	return p, nil
}

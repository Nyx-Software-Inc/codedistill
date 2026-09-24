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
	"strings"
	"time"

	"codedistill/internal/domain"
)

// ListWorkflows returns every definition with its steps, and resolves each
// step's provider for display.
//
// One query per table rather than per workflow: the step list is small and a
// join would duplicate the workflow row per step, which the caller would then
// have to un-duplicate.
func (s *Store) ListWorkflows(ctx context.Context) ([]*domain.Workflow, error) {
	rows, err := s.DB.QueryContext(ctx,
		`SELECT id, name, description, builtin, resumable, enabled, created_at, updated_at
		 FROM workflows ORDER BY builtin DESC, name`)
	if err != nil {
		return nil, fmt.Errorf("list workflows: %w", err)
	}
	defer rows.Close()

	var out []*domain.Workflow
	byID := map[string]*domain.Workflow{}
	for rows.Next() {
		w := &domain.Workflow{}
		var builtin, resumable, enabled int
		var desc sql.NullString
		if err := rows.Scan(&w.ID, &w.Name, &desc, &builtin, &resumable, &enabled,
			&w.CreatedAt, &w.UpdatedAt); err != nil {
			return nil, err
		}
		w.Description = desc.String
		w.Builtin, w.Resumable, w.Enabled = builtin != 0, resumable != 0, enabled != 0
		w.Steps = []domain.WorkflowStep{}
		out = append(out, w)
		byID[w.ID] = w
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(out) == 0 {
		return out, nil
	}

	// Steps, with the provider resolved. LEFT JOIN because a step with no
	// provider is the normal starting state, not an error.
	srows, err := s.DB.QueryContext(ctx,
		`SELECT st.workflow_id, st.ordinal, st.worker_type, st.label,
		        st.needs_context, st.provider_id, p.name, p.is_local, p.context_tokens
		 FROM workflow_steps st
		 LEFT JOIN model_providers p ON p.id = st.provider_id
		 ORDER BY st.workflow_id, st.ordinal`)
	if err != nil {
		return nil, fmt.Errorf("list workflow steps: %w", err)
	}
	defer srows.Close()
	for srows.Next() {
		var wid string
		var st domain.WorkflowStep
		var label, provID, provName sql.NullString
		var isLocal, ctxTokens sql.NullInt64
		if err := srows.Scan(&wid, &st.Ordinal, &st.WorkerType, &label,
			&st.NeedsContext, &provID, &provName, &isLocal, &ctxTokens); err != nil {
			return nil, err
		}
		st.Label, st.ProviderID, st.ProviderName = label.String, provID.String, provName.String
		st.ProviderLocal, st.ContextTokens = isLocal.Int64 != 0, int(ctxTokens.Int64)
		if w := byID[wid]; w != nil {
			w.Steps = append(w.Steps, st)
		}
	}
	if err := srows.Err(); err != nil {
		return nil, err
	}

	prows, err := s.DB.QueryContext(ctx,
		`SELECT workflow_id, key, label, help, type, required, default_val, options, multiple
		 FROM workflow_params ORDER BY workflow_id, ordinal`)
	if err != nil {
		return nil, fmt.Errorf("list workflow params: %w", err)
	}
	defer prows.Close()
	for prows.Next() {
		var wid string
		var pm domain.WorkflowParam
		var req, multi int
		var opts string
		if err := prows.Scan(&wid, &pm.Key, &pm.Label, &pm.Help, &pm.Type,
			&req, &pm.Default, &opts, &multi); err != nil {
			return nil, err
		}
		pm.Required, pm.Multiple = req != 0, multi != 0
		pm.Options = parseParamOptions(opts)
		if w := byID[wid]; w != nil {
			w.Params = append(w.Params, pm)
		}
	}
	return out, prows.Err()
}

// parseParamOptions reads "value|label" lines. A line without a bar uses the
// value as its own label, so the simple case needs no ceremony.
func parseParamOptions(s string) []domain.ParamOption {
	var out []domain.ParamOption
	for _, line := range strings.Split(s, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		v, l, ok := strings.Cut(line, "|")
		if !ok {
			l = v
		}
		out = append(out, domain.ParamOption{Value: v, Label: l})
	}
	return out
}

// SaveJobParams records what a run was actually asked for.
func (s *Store) SaveJobParams(ctx context.Context, jobID string, params map[string]string) error {
	for k, v := range params {
		if _, err := s.DB.ExecContext(ctx,
			`INSERT INTO job_params (job_id, key, value) VALUES (?, ?, ?)
			 ON CONFLICT(job_id, key) DO UPDATE SET value = excluded.value`,
			jobID, k, v); err != nil {
			return fmt.Errorf("save job param %q: %w", k, err)
		}
	}
	return nil
}

// JobParams returns what a run was submitted with.
func (s *Store) JobParams(ctx context.Context, jobID string) (map[string]string, error) {
	rows, err := s.DB.QueryContext(ctx,
		`SELECT key, value FROM job_params WHERE job_id = ?`, jobID)
	if err != nil {
		return nil, fmt.Errorf("job params: %w", err)
	}
	defer rows.Close()
	out := map[string]string{}
	for rows.Next() {
		var k, v string
		if err := rows.Scan(&k, &v); err != nil {
			return nil, err
		}
		out[k] = v
	}
	return out, rows.Err()
}

// GetWorkflow returns one definition, or nil.
func (s *Store) GetWorkflow(ctx context.Context, id string) (*domain.Workflow, error) {
	all, err := s.ListWorkflows(ctx)
	if err != nil {
		return nil, err
	}
	for _, w := range all {
		if w.ID == id {
			return w, nil
		}
	}
	return nil, nil
}

// SetStepProvider points one step at a model.
//
// An empty provider CLEARS the binding, falling the step back to the global
// worker default and then to the app's model — rather than leaving it pointed
// at nothing, which would make a configured workflow fail at run time.
func (s *Store) SetStepProvider(ctx context.Context, workflowID string, ordinal int, providerID string, at time.Time) error {
	var prov any
	if providerID != "" {
		prov = providerID
	}
	res, err := s.DB.ExecContext(ctx,
		`UPDATE workflow_steps SET provider_id = ? WHERE workflow_id = ? AND ordinal = ?`,
		prov, workflowID, ordinal)
	if err != nil {
		return fmt.Errorf("set step provider: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("no step %d in workflow %q", ordinal, workflowID)
	}
	_, err = s.DB.ExecContext(ctx, `UPDATE workflows SET updated_at = ? WHERE id = ?`, at, workflowID)
	return err
}

// ResolveStepProvider answers "which model runs this step".
//
// Three levels, most specific first: the step's own provider, then the global
// worker-type binding, then nothing — which means the caller uses the model the
// app was started with. That last case is what keeps an install with no
// configuration working at all.
func (s *Store) ResolveStepProvider(ctx context.Context, workflowID string, ordinal int, projectID string) (*domain.ModelProvider, error) {
	var provID sql.NullString
	var workerType string
	err := s.DB.QueryRowContext(ctx,
		`SELECT provider_id, worker_type FROM workflow_steps WHERE workflow_id = ? AND ordinal = ?`,
		workflowID, ordinal).Scan(&provID, &workerType)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("resolve step provider: %w", err)
	}
	if provID.Valid && provID.String != "" {
		return s.GetModelProvider(ctx, provID.String)
	}
	return s.ResolveWorkerModel(ctx, workerType, projectID)
}

// CreateWorkflow stores a user-defined workflow and its steps.
func (s *Store) CreateWorkflow(ctx context.Context, w *domain.Workflow) error {
	if w.ID == "" || w.Name == "" {
		return fmt.Errorf("create workflow: id and name are required")
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if _, err := tx.ExecContext(ctx,
		`INSERT INTO workflows (id, name, description, builtin, resumable, enabled, created_at, updated_at)
		 VALUES (?, ?, ?, 0, ?, ?, ?, ?)`,
		w.ID, w.Name, w.Description, boolInt(w.Resumable), boolInt(w.Enabled),
		w.CreatedAt, w.UpdatedAt); err != nil {
		return fmt.Errorf("create workflow: %w", err)
	}
	for i, st := range w.Steps {
		var prov any
		if st.ProviderID != "" {
			prov = st.ProviderID
		}
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO workflow_steps (workflow_id, ordinal, worker_type, label, needs_context, provider_id)
			 VALUES (?, ?, ?, ?, ?, ?)`,
			w.ID, i, st.WorkerType, st.Label, st.NeedsContext, prov); err != nil {
			return fmt.Errorf("create workflow step %d: %w", i, err)
		}
	}
	return tx.Commit()
}

// DeleteWorkflow removes a user-defined workflow. Built-ins are refused: their
// steps are code, and a definition without an implementation is a broken row
// that fails only when someone tries to run it.
func (s *Store) DeleteWorkflow(ctx context.Context, id string) error {
	var builtin int
	err := s.DB.QueryRowContext(ctx, `SELECT builtin FROM workflows WHERE id = ?`, id).Scan(&builtin)
	if err == sql.ErrNoRows {
		return nil
	}
	if err != nil {
		return err
	}
	if builtin != 0 {
		return fmt.Errorf("%q is built in and cannot be deleted", id)
	}
	_, err = s.DB.ExecContext(ctx, `DELETE FROM workflows WHERE id = ?`, id)
	return err
}

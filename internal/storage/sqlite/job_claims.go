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
	"fmt"

	"codedistill/internal/domain"
)

// CreateJobClaim records something an agent asserted.
func (s *Store) CreateJobClaim(ctx context.Context, c *domain.JobClaim) error {
	_, err := s.DB.ExecContext(ctx,
		`INSERT INTO job_claims (id, job_id, job_item_id, round, author_role, kind, statement, evidence, falsifier, created_at, phase)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		c.ID, c.JobID, c.JobItemID, c.Round, c.AuthorRole, c.Kind, c.Statement, c.Evidence, c.Falsifier, c.CreatedAt,
		phaseOr(c.Phase))
	if err != nil {
		return fmt.Errorf("create claim: %w", err)
	}
	return nil
}

// ListJobClaims returns an item's claims for one round and phase, oldest
// first. Round 0 returns every round's; phase "" every phase's.
func (s *Store) ListJobClaims(ctx context.Context, jobItemID string, round int, phase string) ([]*domain.JobClaim, error) {
	q := `SELECT id, job_id, job_item_id, round, author_role, kind, statement, evidence, falsifier, created_at, phase
	      FROM job_claims WHERE job_item_id = ?`
	args := []any{jobItemID}
	if round > 0 {
		q += ` AND round = ?`
		args = append(args, round)
	}
	if phase != "" {
		q += ` AND phase = ?`
		args = append(args, phase)
	}
	rows, err := s.DB.QueryContext(ctx, q+` ORDER BY created_at, id`, args...)
	if err != nil {
		return nil, fmt.Errorf("list claims: %w", err)
	}
	defer rows.Close()
	var out []*domain.JobClaim
	for rows.Next() {
		c := &domain.JobClaim{}
		if err := rows.Scan(&c.ID, &c.JobID, &c.JobItemID, &c.Round, &c.AuthorRole, &c.Kind,
			&c.Statement, &c.Evidence, &c.Falsifier, &c.CreatedAt, &c.Phase); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// CreateClaimVerdict records a lens's ruling.
func (s *Store) CreateClaimVerdict(ctx context.Context, v *domain.ClaimVerdict) error {
	_, err := s.DB.ExecContext(ctx,
		`INSERT INTO claim_verdicts (id, job_id, job_item_id, round, lens, claim_id, verdict, checked, detail, created_at,
		   phase, severity, about, point_id)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		v.ID, v.JobID, v.JobItemID, v.Round, v.Lens, nullable(v.ClaimID), v.Verdict, v.Checked, v.Detail, v.CreatedAt,
		phaseOr(v.Phase), orDefault(v.Severity, domain.SeverityBlocking), orDefault(v.About, domain.AboutChange),
		nullable(v.PointID))
	if err != nil {
		return fmt.Errorf("create verdict: %w", err)
	}
	return nil
}

// ListClaimVerdicts returns an item's verdicts for one round (0 = every
// round), phase ("" = every phase) and lens ("" = every lens).
func (s *Store) ListClaimVerdicts(ctx context.Context, jobItemID string, round int, phase, lens string) ([]*domain.ClaimVerdict, error) {
	q := `SELECT id, job_id, job_item_id, round, lens, claim_id, verdict, checked, detail, created_at,
	        phase, severity, about, point_id
	      FROM claim_verdicts WHERE job_item_id = ?`
	args := []any{jobItemID}
	if round > 0 {
		q += ` AND round = ?`
		args = append(args, round)
	}
	if phase != "" {
		q += ` AND phase = ?`
		args = append(args, phase)
	}
	if lens != "" {
		q += ` AND lens = ?`
		args = append(args, lens)
	}
	rows, err := s.DB.QueryContext(ctx, q+` ORDER BY created_at, id`, args...)
	if err != nil {
		return nil, fmt.Errorf("list verdicts: %w", err)
	}
	defer rows.Close()
	var out []*domain.ClaimVerdict
	for rows.Next() {
		v := &domain.ClaimVerdict{}
		var claim, point sql.NullString
		if err := rows.Scan(&v.ID, &v.JobID, &v.JobItemID, &v.Round, &v.Lens, &claim, &v.Verdict,
			&v.Checked, &v.Detail, &v.CreatedAt, &v.Phase, &v.Severity, &v.About, &point); err != nil {
			return nil, err
		}
		v.ClaimID, v.PointID = claim.String, point.String
		out = append(out, v)
	}
	return out, rows.Err()
}

// SetVerdictPoint records which point a verdict raises.
func (s *Store) SetVerdictPoint(ctx context.Context, verdictID, pointID string) error {
	if _, err := s.DB.ExecContext(ctx, `UPDATE claim_verdicts SET point_id = ? WHERE id = ?`, pointID, verdictID); err != nil {
		return fmt.Errorf("set verdict point: %w", err)
	}
	return nil
}

// CreateJobPoint records a point raised for the first time.
func (s *Store) CreateJobPoint(ctx context.Context, p *domain.JobPoint) error {
	_, err := s.DB.ExecContext(ctx,
		`INSERT INTO job_points (id, job_id, job_item_id, kind, about, text, created_at) VALUES (?, ?, ?, ?, ?, ?, ?)`,
		p.ID, p.JobID, p.JobItemID, p.Kind, orDefault(p.About, domain.AboutChange), p.Text, p.CreatedAt)
	if err != nil {
		return fmt.Errorf("create point: %w", err)
	}
	return nil
}

// SetJobPointKind changes a point's kind: a note someone later calls
// blocking becomes an objection.
func (s *Store) SetJobPointKind(ctx context.Context, id, kind string) error {
	if _, err := s.DB.ExecContext(ctx, `UPDATE job_points SET kind = ? WHERE id = ?`, kind, id); err != nil {
		return fmt.Errorf("set point kind: %w", err)
	}
	return nil
}

// ListJobPoints returns an item's points, oldest first.
func (s *Store) ListJobPoints(ctx context.Context, jobItemID string) ([]*domain.JobPoint, error) {
	rows, err := s.DB.QueryContext(ctx,
		`SELECT id, job_id, job_item_id, kind, about, text, created_at
		 FROM job_points WHERE job_item_id = ? ORDER BY created_at, id`, jobItemID)
	if err != nil {
		return nil, fmt.Errorf("list points: %w", err)
	}
	defer rows.Close()
	var out []*domain.JobPoint
	for rows.Next() {
		p := &domain.JobPoint{}
		if err := rows.Scan(&p.ID, &p.JobID, &p.JobItemID, &p.Kind, &p.About, &p.Text, &p.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// CreateAgentRun records one agent run's stats.
func (s *Store) CreateAgentRun(ctx context.Context, r *domain.AgentRun) error {
	tools := ""
	if len(r.ToolCalls) > 0 {
		b, _ := json.Marshal(r.ToolCalls)
		tools = string(b)
	}
	_, err := s.DB.ExecContext(ctx,
		`INSERT INTO agent_runs (id, job_id, job_item_id, role, lens, round, agent, model, outcome, exit_code,
		   started_at, duration_ms, silent_ms, turns, tokens_in, tokens_out, cache_read, cache_write, cost_usd,
		   tool_calls, files_changed, lines_added, lines_deleted, claims_made, claims_refuted, phase)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		r.ID, r.JobID, nullable(r.JobItemID), r.Role, r.Lens, r.Round, r.Agent, nullable(r.Model), r.Outcome, r.ExitCode,
		r.StartedAt, r.DurationMS, r.SilentMS, r.Turns, r.TokensIn, r.TokensOut, r.CacheRead, r.CacheWrite, r.CostUSD,
		tools, r.FilesChanged, r.LinesAdded, r.LinesDeleted, r.ClaimsMade, r.ClaimsRefuted, phaseOr(r.Phase))
	if err != nil {
		return fmt.Errorf("create agent run: %w", err)
	}
	return nil
}

// ListAgentRuns returns a job's agent runs, in the order they ran.
func (s *Store) ListAgentRuns(ctx context.Context, jobID string) ([]*domain.AgentRun, error) {
	rows, err := s.DB.QueryContext(ctx,
		`SELECT id, job_id, job_item_id, role, lens, round, agent, model, outcome, exit_code,
		   started_at, duration_ms, silent_ms, turns, tokens_in, tokens_out, cache_read, cache_write, cost_usd,
		   tool_calls, files_changed, lines_added, lines_deleted, claims_made, claims_refuted, phase
		 FROM agent_runs WHERE job_id = ? ORDER BY started_at, id`, jobID)
	if err != nil {
		return nil, fmt.Errorf("list agent runs: %w", err)
	}
	defer rows.Close()
	var out []*domain.AgentRun
	for rows.Next() {
		r := &domain.AgentRun{}
		var item, model sql.NullString
		var tools string
		var exit, turns, files, added, deleted, made, refuted sql.NullInt64
		var tin, tout, cr, cw sql.NullInt64
		var cost sql.NullFloat64
		if err := rows.Scan(&r.ID, &r.JobID, &item, &r.Role, &r.Lens, &r.Round, &r.Agent, &model, &r.Outcome, &exit,
			&r.StartedAt, &r.DurationMS, &r.SilentMS, &turns, &tin, &tout, &cr, &cw, &cost,
			&tools, &files, &added, &deleted, &made, &refuted, &r.Phase); err != nil {
			return nil, err
		}
		r.JobItemID, r.Model = item.String, model.String
		r.ExitCode, r.Turns = intPtr(exit), intPtr(turns)
		r.FilesChanged, r.LinesAdded, r.LinesDeleted = intPtr(files), intPtr(added), intPtr(deleted)
		r.ClaimsMade, r.ClaimsRefuted = intPtr(made), intPtr(refuted)
		r.TokensIn, r.TokensOut, r.CacheRead, r.CacheWrite = int64Ptr(tin), int64Ptr(tout), int64Ptr(cr), int64Ptr(cw)
		if cost.Valid {
			v := cost.Float64
			r.CostUSD = &v
		}
		if tools != "" {
			_ = json.Unmarshal([]byte(tools), &r.ToolCalls)
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func orDefault(v, def string) string {
	if v == "" {
		return def
	}
	return v
}

func intPtr(n sql.NullInt64) *int {
	if !n.Valid {
		return nil
	}
	v := int(n.Int64)
	return &v
}

func int64Ptr(n sql.NullInt64) *int64 {
	if !n.Valid {
		return nil
	}
	v := n.Int64
	return &v
}

// CreateJobTest records a test the TestBuilder wrote or reviewed.
func (s *Store) CreateJobTest(ctx context.Context, t *domain.JobTest) error {
	var applies any
	if t.Applies != nil {
		applies = *t.Applies
	}
	_, err := s.DB.ExecContext(ctx,
		`INSERT INTO job_tests (id, job_id, job_item_id, origin, name, file, purpose, applies, criterion, reason, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		t.ID, t.JobID, t.JobItemID, t.Origin, t.Name, t.File, t.Purpose, applies, t.Criterion, t.Reason, t.CreatedAt)
	if err != nil {
		return fmt.Errorf("create job test: %w", err)
	}
	return nil
}

// ListJobTests returns an item's tests, in the order they were recorded.
func (s *Store) ListJobTests(ctx context.Context, jobItemID string) ([]*domain.JobTest, error) {
	rows, err := s.DB.QueryContext(ctx,
		`SELECT id, job_id, job_item_id, origin, name, file, purpose, applies, criterion, reason, created_at
		 FROM job_tests WHERE job_item_id = ? ORDER BY created_at, id`, jobItemID)
	if err != nil {
		return nil, fmt.Errorf("list job tests: %w", err)
	}
	defer rows.Close()
	var out []*domain.JobTest
	for rows.Next() {
		t := &domain.JobTest{}
		var applies sql.NullBool
		if err := rows.Scan(&t.ID, &t.JobID, &t.JobItemID, &t.Origin, &t.Name, &t.File, &t.Purpose, &applies,
			&t.Criterion, &t.Reason, &t.CreatedAt); err != nil {
			return nil, err
		}
		if applies.Valid {
			v := applies.Bool
			t.Applies = &v
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

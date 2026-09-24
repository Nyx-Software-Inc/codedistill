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
	"strings"
	"time"

	"codedistill/internal/domain"
)

const proposalColumns = `id, job_id, project_id, kind, subject, body, origin,
	corroborated, inferred_link, external_ref, priority, lines, status,
	created_item_id, linked_item_id, reject_reason, decided_at, decided_by, created_at`

// SaveDecomposeRun records what a decomposition learned about a document, along
// with everything it proposes. Written in one transaction: a run whose summary
// says "44 proposals" while only 12 rows landed would make the review surface
// lie about what it is showing.
func (s *Store) SaveDecomposeRun(ctx context.Context, run *domain.DecomposeRun, props []*domain.DecomposeProposal) error {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("save decompose run: %w", err)
	}
	defer tx.Rollback()

	if _, err := tx.ExecContext(ctx,
		`INSERT INTO decompose_runs
			(job_id, project_id, source_item_id, source_label, source_hash,
			 source_bytes, source_modified, sentences,
			 excluded_lines, tables_read, tables_declined, corroborated,
			 table_only, prose_only, declined_nodes, rejected_moves,
			 inferred_links, model, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		run.JobID, run.ProjectID, run.SourceItemID, nullable(run.SourceLabel),
		run.SourceHash, run.SourceBytes, timeOrNil(run.ModifiedAt),
		run.Sentences, run.ExcludedLines, run.TablesRead, run.TablesDeclined,
		run.Corroborated, run.TableOnly, run.ProseOnly, run.DeclinedNodes,
		run.RejectedMoves, run.InferredLinks, nullable(run.Model), run.CreatedAt,
	); err != nil {
		return fmt.Errorf("insert decompose run: %w", err)
	}

	for _, p := range props {
		if !domain.ValidProposalOrigin(p.Origin) {
			return fmt.Errorf("proposal %s: invalid origin %q", p.ID, p.Origin)
		}
		lines, err := json.Marshal(p.Lines)
		if err != nil {
			return fmt.Errorf("proposal %s: encode citation: %w", p.ID, err)
		}
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO decompose_proposals (`+proposalColumns+`)
			 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			p.ID, p.JobID, p.ProjectID, p.Kind, p.Subject, nullable(p.Body), p.Origin,
			boolInt(p.Corroborated), boolInt(p.InferredLink), nullable(p.ExternalRef),
			nullable(p.Priority), string(lines), domain.ProposalPending,
			nil, nil, nil, nil, nil, p.CreatedAt,
		); err != nil {
			return fmt.Errorf("insert proposal %s: %w", p.ID, err)
		}
	}
	return tx.Commit()
}

const runColumns = `job_id, project_id, source_item_id, source_label,
	source_hash, source_bytes, source_modified, sentences, excluded_lines,
	tables_read, tables_declined, corroborated, table_only, prose_only,
	declined_nodes, rejected_moves, inferred_links, model, created_at`

// GetDecomposeRun reads a run's summary, or nil.
func (s *Store) GetDecomposeRun(ctx context.Context, jobID string) (*domain.DecomposeRun, error) {
	row := s.DB.QueryRowContext(ctx, `SELECT `+runColumns+` FROM decompose_runs WHERE job_id = ?`, jobID)
	r, err := scanRun(row)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return r, err
}

// FindDecomposeRuns answers "have I read this document before?".
//
// The HASH is the identity: same bytes, same document, and re-reading it costs
// fifteen minutes for an answer already on disk. Passing a label as well finds
// EARLIER VERSIONS of the same file — a different hash under the same name is a
// revision, which is the case worth reading again, and the case where the
// proposals should be diffed against the items the first review already created
// rather than offered as if the document were new.
//
// Either argument may be empty. Newest first.
func (s *Store) FindDecomposeRuns(ctx context.Context, projectID, hash, label string) ([]*domain.DecomposeRun, error) {
	q := `SELECT ` + runColumns + ` FROM decompose_runs WHERE project_id = ?`
	args := []any{projectID}
	switch {
	case hash != "" && label != "":
		q += ` AND (source_hash = ? OR source_label = ?)`
		args = append(args, hash, label)
	case hash != "":
		q += ` AND source_hash = ?`
		args = append(args, hash)
	case label != "":
		q += ` AND source_label = ?`
		args = append(args, label)
	}
	q += ` ORDER BY created_at DESC`

	rows, err := s.DB.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("find decompose runs: %w", err)
	}
	defer rows.Close()

	var out []*domain.DecomposeRun
	for rows.Next() {
		r, err := scanRun(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func scanRun(r rowScanner) (*domain.DecomposeRun, error) {
	run := &domain.DecomposeRun{}
	var label, model sql.NullString
	var modified sql.NullTime
	if err := r.Scan(&run.JobID, &run.ProjectID, &run.SourceItemID, &label,
		&run.SourceHash, &run.SourceBytes, &modified, &run.Sentences,
		&run.ExcludedLines, &run.TablesRead, &run.TablesDeclined, &run.Corroborated,
		&run.TableOnly, &run.ProseOnly, &run.DeclinedNodes, &run.RejectedMoves,
		&run.InferredLinks, &model, &run.CreatedAt); err != nil {
		return nil, err
	}
	run.SourceLabel, run.Model = label.String, model.String
	if modified.Valid {
		m := modified.Time
		run.ModifiedAt = &m
	}
	return run, nil
}

func timeOrNil(t *time.Time) any {
	if t == nil {
		return nil
	}
	return *t
}

// ListProposals returns a job's proposals. statuses filters; empty means all.
// Ordering puts corroborated table rows first: they are the strongest evidence
// available — the author wrote the requirement down AND the prose agreed — so
// they are what a reviewer can accept in bulk without reading closely.
func (s *Store) ListProposals(ctx context.Context, jobID string, statuses []string) ([]*domain.DecomposeProposal, error) {
	q := `SELECT ` + proposalColumns + ` FROM decompose_proposals WHERE job_id = ?`
	args := []any{jobID}
	if len(statuses) > 0 {
		q += ` AND status IN (` + strings.TrimSuffix(strings.Repeat("?,", len(statuses)), ",") + `)`
		for _, st := range statuses {
			args = append(args, st)
		}
	}
	q += ` ORDER BY corroborated DESC, origin DESC, kind, subject`

	rows, err := s.DB.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("list proposals: %w", err)
	}
	defer rows.Close()

	var out []*domain.DecomposeProposal
	for rows.Next() {
		p, err := scanProposal(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// DecideProposal records a human's verdict. Only a pending proposal can be
// decided, so a double-submit from a slow review screen cannot create the same
// item twice — the second call is a no-op rather than a duplicate.
func (s *Store) DecideProposal(ctx context.Context, id, status, itemID, reason, userID string, at time.Time) error {
	if !domain.ValidProposalStatus(status) || status == domain.ProposalPending {
		return fmt.Errorf("decide proposal: invalid verdict %q", status)
	}
	var created, linked any
	switch status {
	case domain.ProposalAccepted:
		if itemID == "" {
			return fmt.Errorf("decide proposal: accepted needs the item it created")
		}
		created = itemID
	case domain.ProposalLinked:
		if itemID == "" {
			return fmt.Errorf("decide proposal: linked needs the item it links to")
		}
		linked = itemID
	case domain.ProposalRejected:
		// A reason is optional. It is what makes a suppressed proposal
		// reviewable later, not a way to police the user.
	}
	if status != domain.ProposalRejected {
		reason = ""
	}

	_, err := s.DB.ExecContext(ctx,
		`UPDATE decompose_proposals
		 SET status = ?, created_item_id = ?, linked_item_id = ?,
		     reject_reason = ?, decided_at = ?, decided_by = ?
		 WHERE id = ? AND status = ?`,
		status, created, linked, nullable(reason), at, nullable(userID),
		id, domain.ProposalPending)
	if err != nil {
		return fmt.Errorf("decide proposal: %w", err)
	}
	return nil
}

// ListDecomposeRuns returns a project's runs newest first, each with how many
// proposals are still waiting on a person.
//
// The pending count comes from the proposals rather than a column on the run: a
// stored count would be a second place the truth lives, and it would drift the
// first time a proposal was decided by any path that forgot to decrement it.
func (s *Store) ListDecomposeRuns(ctx context.Context, projectID string, limit int) ([]*domain.DecomposeRunSummary, error) {
	if limit <= 0 {
		limit = 50
	}
	rows, err := s.DB.QueryContext(ctx,
		`SELECT r.job_id, r.source_item_id, r.source_label, r.sentences, r.model, r.created_at,
		        COUNT(p.id)                                             AS total,
		        SUM(CASE WHEN p.status = 'pending'  THEN 1 ELSE 0 END)  AS pending,
		        SUM(CASE WHEN p.status = 'accepted' THEN 1 ELSE 0 END)  AS accepted,
		        SUM(CASE WHEN p.status = 'rejected' THEN 1 ELSE 0 END)  AS rejected,
		        SUM(CASE WHEN p.status = 'linked'   THEN 1 ELSE 0 END)  AS linked
		   FROM decompose_runs r
		   LEFT JOIN decompose_proposals p ON p.job_id = r.job_id
		  WHERE r.project_id = ?
		  GROUP BY r.job_id
		  ORDER BY r.created_at DESC
		  LIMIT ?`, projectID, limit)
	if err != nil {
		return nil, fmt.Errorf("list decompose runs: %w", err)
	}
	defer rows.Close()

	var out []*domain.DecomposeRunSummary
	for rows.Next() {
		r := &domain.DecomposeRunSummary{}
		var model sql.NullString
		if err := rows.Scan(&r.JobID, &r.SourceItemID, &r.SourceLabel, &r.Sentences,
			&model, &r.CreatedAt, &r.Total, &r.Pending, &r.Accepted,
			&r.Rejected, &r.Linked); err != nil {
			return nil, err
		}
		r.Model = model.String
		out = append(out, r)
	}
	return out, rows.Err()
}

// RejectedProposals returns a project's rejections, newest first.
//
// Scoped to the PROJECT, not the document: "SSO is out of scope for Q4" is a
// judgement about the work and should outlive the particular draft it was made
// against. A later decomposition shows these collapsed rather than proposing
// them again — suppressed, never hidden, because silently dropping one would be
// the product quietly deciding something on the user's behalf.
func (s *Store) RejectedProposals(ctx context.Context, projectID string, limit int) ([]*domain.DecomposeProposal, error) {
	q := `SELECT ` + proposalColumns + ` FROM decompose_proposals
	      WHERE project_id = ? AND status = ? ORDER BY decided_at DESC`
	args := []any{projectID, domain.ProposalRejected}
	if limit > 0 {
		q += ` LIMIT ?`
		args = append(args, limit)
	}
	rows, err := s.DB.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("rejected proposals: %w", err)
	}
	defer rows.Close()

	var out []*domain.DecomposeProposal
	for rows.Next() {
		p, err := scanProposal(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

func scanProposal(r rowScanner) (*domain.DecomposeProposal, error) {
	p := &domain.DecomposeProposal{}
	var body, ref, priority, createdItem, linkedItem, reason, by sql.NullString
	var linesJSON string
	var corroborated, inferred int
	var decidedAt sql.NullTime
	if err := r.Scan(&p.ID, &p.JobID, &p.ProjectID, &p.Kind, &p.Subject, &body,
		&p.Origin, &corroborated, &inferred, &ref, &priority, &linesJSON,
		&p.Status, &createdItem, &linkedItem, &reason, &decidedAt, &by,
		&p.CreatedAt); err != nil {
		return nil, err
	}
	p.Body, p.ExternalRef, p.Priority = body.String, ref.String, priority.String
	p.CreatedItemID, p.LinkedItemID, p.RejectReason = createdItem.String, linkedItem.String, reason.String
	p.DecidedBy = by.String
	p.Corroborated, p.InferredLink = corroborated != 0, inferred != 0
	if decidedAt.Valid {
		t := decidedAt.Time
		p.DecidedAt = &t
	}
	if linesJSON != "" {
		// A citation that fails to decode is a bug, not a reason to hide the
		// proposal — but it must not silently read as "no source".
		_ = json.Unmarshal([]byte(linesJSON), &p.Lines)
	}
	return p, nil
}

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

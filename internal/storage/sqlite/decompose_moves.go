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

// SaveDecomposePass records one completed reading pass and its moves, in a
// single transaction. The pass row is written even when there are no moves:
// "finished and found nothing" and "never ran" are different facts, and only
// one of them is worth five minutes to redo.
func (s *Store) SaveDecomposePass(ctx context.Context, p *domain.DecomposePass, moves []*domain.DecomposeMove) error {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("save decompose pass: %w", err)
	}
	defer tx.Rollback()

	// A re-run of the same pass replaces it rather than duplicating. This
	// happens when a pass is retried after a failure that left its rows behind.
	if _, err := tx.ExecContext(ctx,
		`DELETE FROM decompose_moves
		 WHERE project_id = ? AND source_hash = ? AND window_start = ?`,
		p.ProjectID, p.SourceHash, p.WindowStart); err != nil {
		return fmt.Errorf("clear prior pass: %w", err)
	}
	for _, m := range moves {
		var target any
		if m.Target != nil {
			target = *m.Target
		}
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO decompose_moves
				(id, project_id, source_hash, window_start, window_end,
				 sentence, role, label, target, created_at)
			 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			m.ID, p.ProjectID, p.SourceHash, p.WindowStart, p.WindowEnd,
			m.Sentence, m.Role, m.Label, target, p.CreatedAt); err != nil {
			return fmt.Errorf("insert move: %w", err)
		}
	}
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO decompose_passes
			(project_id, source_hash, window_start, window_end, moves, rejected, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?)
		 ON CONFLICT (project_id, source_hash, window_start) DO UPDATE SET
			window_end = excluded.window_end, moves = excluded.moves,
			rejected = excluded.rejected, created_at = excluded.created_at`,
		p.ProjectID, p.SourceHash, p.WindowStart, p.WindowEnd,
		len(moves), p.Rejected, p.CreatedAt); err != nil {
		return fmt.Errorf("insert pass: %w", err)
	}
	return tx.Commit()
}

// CompletedPasses returns the window starts already read for this document,
// with the moves each produced.
//
// Keyed on the content hash rather than a job id, so a retry after an
// interruption inherits the earlier run's work instead of starting over —
// which is the entire point.
func (s *Store) CompletedPasses(ctx context.Context, projectID, sourceHash string) (map[int][]*domain.DecomposeMove, error) {
	done := map[int][]*domain.DecomposeMove{}

	// Pass rows first, so a pass that legitimately produced nothing is still
	// known to be done.
	rows, err := s.DB.QueryContext(ctx,
		`SELECT window_start FROM decompose_passes WHERE project_id = ? AND source_hash = ?`,
		projectID, sourceHash)
	if err != nil {
		return nil, fmt.Errorf("completed passes: %w", err)
	}
	for rows.Next() {
		var start int
		if err := rows.Scan(&start); err != nil {
			rows.Close()
			return nil, err
		}
		done[start] = nil
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(done) == 0 {
		return done, nil
	}

	mrows, err := s.DB.QueryContext(ctx,
		`SELECT id, window_start, sentence, role, label, target
		 FROM decompose_moves WHERE project_id = ? AND source_hash = ?
		 ORDER BY window_start, sentence`,
		projectID, sourceHash)
	if err != nil {
		return nil, fmt.Errorf("completed moves: %w", err)
	}
	defer mrows.Close()
	for mrows.Next() {
		m := &domain.DecomposeMove{}
		var start int
		var target sql.NullInt64
		if err := mrows.Scan(&m.ID, &start, &m.Sentence, &m.Role, &m.Label, &target); err != nil {
			return nil, err
		}
		if target.Valid {
			t := int(target.Int64)
			m.Target = &t
		}
		done[start] = append(done[start], m)
	}
	return done, mrows.Err()
}

// ForgetDecomposePasses drops every recorded pass for a document, so an
// explicit re-read starts clean.
func (s *Store) ForgetDecomposePasses(ctx context.Context, projectID, sourceHash string) error {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, q := range []string{
		`DELETE FROM decompose_moves WHERE project_id = ? AND source_hash = ?`,
		`DELETE FROM decompose_passes WHERE project_id = ? AND source_hash = ?`,
	} {
		if _, err := tx.ExecContext(ctx, q, projectID, sourceHash); err != nil {
			return fmt.Errorf("forget passes: %w", err)
		}
	}
	return tx.Commit()
}

var _ = time.Time{}

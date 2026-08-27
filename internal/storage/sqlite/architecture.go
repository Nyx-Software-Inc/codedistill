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

const archNodeColumns = `id, project_id, name, kind, description, area, pos_x, pos_y, provenance, created_at, updated_at`

func scanArchNode(scan func(...any) error) (*domain.ArchitectureNode, error) {
	n := &domain.ArchitectureNode{}
	if err := scan(&n.ID, &n.ProjectID, &n.Name, &n.Kind, &n.Description, &n.Area,
		&n.PosX, &n.PosY, &n.Provenance, &n.CreatedAt, &n.UpdatedAt); err != nil {
		return nil, err
	}
	return n, nil
}

func (s *Store) ListArchitectureNodes(ctx context.Context, projectID string) ([]*domain.ArchitectureNode, error) {
	rows, err := s.DB.QueryContext(ctx,
		`SELECT `+archNodeColumns+` FROM architecture_nodes WHERE project_id = ? ORDER BY created_at`, projectID)
	if err != nil {
		return nil, fmt.Errorf("list architecture nodes: %w", err)
	}
	defer rows.Close()
	out := []*domain.ArchitectureNode{}
	for rows.Next() {
		n, err := scanArchNode(rows.Scan)
		if err != nil {
			return nil, err
		}
		out = append(out, n)
	}
	return out, rows.Err()
}

func (s *Store) GetArchitectureNode(ctx context.Context, id string) (*domain.ArchitectureNode, error) {
	n, err := scanArchNode(s.DB.QueryRowContext(ctx,
		`SELECT `+archNodeColumns+` FROM architecture_nodes WHERE id = ?`, id).Scan)
	if err != nil {
		return nil, fmt.Errorf("architecture node %s: %w", id, storage.ErrNotFound)
	}
	return n, nil
}

func (s *Store) CreateArchitectureNode(ctx context.Context, n *domain.ArchitectureNode) error {
	_, err := s.DB.ExecContext(ctx,
		`INSERT INTO architecture_nodes (`+archNodeColumns+`) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		n.ID, n.ProjectID, n.Name, n.Kind, n.Description, n.Area, n.PosX, n.PosY, n.Provenance, n.CreatedAt, n.UpdatedAt)
	if err != nil {
		return fmt.Errorf("create architecture node: %w", err)
	}
	return nil
}

// UpdateArchitectureNode writes the mutable fields (name, kind, description,
// area, position, provenance) + updated_at; project is immutable.
func (s *Store) UpdateArchitectureNode(ctx context.Context, n *domain.ArchitectureNode) error {
	res, err := s.DB.ExecContext(ctx,
		`UPDATE architecture_nodes SET name = ?, kind = ?, description = ?, area = ?,
		     pos_x = ?, pos_y = ?, provenance = ?, updated_at = ? WHERE id = ?`,
		n.Name, n.Kind, n.Description, n.Area, n.PosX, n.PosY, n.Provenance, n.UpdatedAt, n.ID)
	if err != nil {
		return fmt.Errorf("update architecture node: %w", err)
	}
	if c, _ := res.RowsAffected(); c == 0 {
		return fmt.Errorf("architecture node %s: not found", n.ID)
	}
	return nil
}

// SetArchitectureNodeStatus persists a node's last-computed as-built status
// ('matched' | 'missing' | 'unmapped'). Cached so the governance gate can read
// it without recomputing the git delta. Refreshed each time the architecture
// delta is computed (the diagram is viewed).
func (s *Store) SetArchitectureNodeStatus(ctx context.Context, id, status string) error {
	_, err := s.DB.ExecContext(ctx,
		`UPDATE architecture_nodes SET last_status = ? WHERE id = ?`, status, id)
	if err != nil {
		return fmt.Errorf("set architecture node status: %w", err)
	}
	return nil
}

// ArchitectureHasMissing reports whether the project has any ratified node whose
// last-computed status is 'missing' — the diagram claims a component the code
// doesn't have. Only ratified nodes count: an un-ratified draft shouldn't gate
// completion. Drives the architecture governance instance (Phase 6).
func (s *Store) ArchitectureHasMissing(ctx context.Context, projectID string) (bool, error) {
	var n int
	err := s.DB.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM architecture_nodes
		   WHERE project_id = ? AND provenance = 'ratified' AND last_status = 'missing'`,
		projectID).Scan(&n)
	if err != nil {
		return false, fmt.Errorf("architecture missing count: %w", err)
	}
	return n > 0, nil
}

// DeleteArchitectureNode removes a node and any edges touching it.
func (s *Store) DeleteArchitectureNode(ctx context.Context, id string) error {
	if _, err := s.DB.ExecContext(ctx,
		`DELETE FROM architecture_edges WHERE from_node = ? OR to_node = ?`, id, id); err != nil {
		return fmt.Errorf("delete node edges: %w", err)
	}
	if _, err := s.DB.ExecContext(ctx, `DELETE FROM architecture_nodes WHERE id = ?`, id); err != nil {
		return fmt.Errorf("delete architecture node: %w", err)
	}
	return nil
}

const archEdgeColumns = `id, project_id, from_node, to_node, label, provenance, created_at`

func scanArchEdge(scan func(...any) error) (*domain.ArchitectureEdge, error) {
	e := &domain.ArchitectureEdge{}
	if err := scan(&e.ID, &e.ProjectID, &e.FromNode, &e.ToNode, &e.Label, &e.Provenance, &e.CreatedAt); err != nil {
		return nil, err
	}
	return e, nil
}

func (s *Store) ListArchitectureEdges(ctx context.Context, projectID string) ([]*domain.ArchitectureEdge, error) {
	rows, err := s.DB.QueryContext(ctx,
		`SELECT `+archEdgeColumns+` FROM architecture_edges WHERE project_id = ? ORDER BY created_at`, projectID)
	if err != nil {
		return nil, fmt.Errorf("list architecture edges: %w", err)
	}
	defer rows.Close()
	out := []*domain.ArchitectureEdge{}
	for rows.Next() {
		e, err := scanArchEdge(rows.Scan)
		if err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

func (s *Store) CreateArchitectureEdge(ctx context.Context, e *domain.ArchitectureEdge) error {
	_, err := s.DB.ExecContext(ctx,
		`INSERT INTO architecture_edges (`+archEdgeColumns+`) VALUES (?, ?, ?, ?, ?, ?, ?)`,
		e.ID, e.ProjectID, e.FromNode, e.ToNode, e.Label, e.Provenance, e.CreatedAt)
	if err != nil {
		return fmt.Errorf("create architecture edge: %w", err)
	}
	return nil
}

func (s *Store) DeleteArchitectureEdge(ctx context.Context, id string) error {
	if _, err := s.DB.ExecContext(ctx, `DELETE FROM architecture_edges WHERE id = ?`, id); err != nil {
		return fmt.Errorf("delete architecture edge: %w", err)
	}
	return nil
}

// RatifyProposedArchitecture promotes every proposed node AND edge for a project
// to ratified, atomically. Edges become a plain provenance UPDATE — this
// replaces the handler-side delete-then-recreate that lost an edge for good if
// the recreate failed after the delete landed (bug 100). Either the whole graph
// ratifies or nothing changes.
func (s *Store) RatifyProposedArchitecture(ctx context.Context, projectID string, now time.Time) error {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("ratify architecture: begin: %w", err)
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx,
		`UPDATE architecture_nodes SET provenance = 'ratified', updated_at = ?
		   WHERE project_id = ? AND provenance = 'proposed'`, now, projectID); err != nil {
		return fmt.Errorf("ratify architecture nodes: %w", err)
	}
	if _, err := tx.ExecContext(ctx,
		`UPDATE architecture_edges SET provenance = 'ratified'
		   WHERE project_id = ? AND provenance = 'proposed'`, projectID); err != nil {
		return fmt.Errorf("ratify architecture edges: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("ratify architecture: commit: %w", err)
	}
	return nil
}

// DeleteProposedArchitecture clears every proposed (un-ratified) node + edge for
// a project, so a re-draft can replace the proposal while ratified structure survives.
func (s *Store) DeleteProposedArchitecture(ctx context.Context, projectID string) error {
	if _, err := s.DB.ExecContext(ctx,
		`DELETE FROM architecture_edges WHERE project_id = ? AND provenance = 'proposed'`, projectID); err != nil {
		return fmt.Errorf("clear proposed edges: %w", err)
	}
	if _, err := s.DB.ExecContext(ctx,
		`DELETE FROM architecture_nodes WHERE project_id = ? AND provenance = 'proposed'`, projectID); err != nil {
		return fmt.Errorf("clear proposed nodes: %w", err)
	}
	return nil
}

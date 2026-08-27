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
	"codedistill/internal/storage"
)

const findingColumns = `id, project_id, fingerprint, analyzer, rule_id, severity,
	file_path, line_start, line_end, title, detail, llm_summary, llm_severity,
	status, pushed_item_id, first_seen_at, last_seen_at, resolved_at,
	category, security_severity, source`

// severityRank orders findings high→info in list queries.
const severityRank = `CASE severity WHEN 'high' THEN 0 WHEN 'medium' THEN 1 WHEN 'low' THEN 2 ELSE 3 END`

func scanFinding(scan func(...any) error) (*domain.CodeFinding, error) {
	f := &domain.CodeFinding{}
	var pushed sql.NullString
	var resolved sql.NullTime
	if err := scan(&f.ID, &f.ProjectID, &f.Fingerprint, &f.Analyzer, &f.RuleID, &f.Severity,
		&f.FilePath, &f.LineStart, &f.LineEnd, &f.Title, &f.Detail, &f.LLMSummary, &f.LLMSeverity,
		&f.Status, &pushed, &f.FirstSeenAt, &f.LastSeenAt, &resolved,
		&f.Category, &f.SecuritySeverity, &f.Source); err != nil {
		return nil, err
	}
	f.PushedItemID = pushed.String
	if resolved.Valid {
		t := resolved.Time
		f.ResolvedAt = &t
	}
	return f, nil
}

// UpsertCodeFinding inserts a finding or, when one already exists for
// (project_id, fingerprint), refreshes its analyzer-derived fields and marks it
// seen again (clearing any resolved_at). It deliberately PRESERVES status, so a
// dismissed or pushed finding stays that way across re-scans. Returns whether a
// new row was inserted (for the scan's findings_new count).
func (s *Store) UpsertCodeFinding(ctx context.Context, f *domain.CodeFinding) (bool, error) {
	res, err := s.DB.ExecContext(ctx,
		`UPDATE code_findings SET severity = ?, category = ?, security_severity = ?, source = ?,
			file_path = ?, line_start = ?, line_end = ?,
			title = ?, detail = ?, last_seen_at = ?, resolved_at = NULL
		 WHERE project_id = ? AND fingerprint = ?`,
		f.Severity, f.Category, f.SecuritySeverity, f.Source, f.FilePath, f.LineStart, f.LineEnd,
		f.Title, f.Detail, f.LastSeenAt,
		f.ProjectID, f.Fingerprint)
	if err != nil {
		return false, fmt.Errorf("update finding: %w", err)
	}
	if n, _ := res.RowsAffected(); n > 0 {
		return false, nil
	}
	if _, err := s.DB.ExecContext(ctx,
		`INSERT INTO code_findings (`+findingColumns+`)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		f.ID, f.ProjectID, f.Fingerprint, f.Analyzer, f.RuleID, f.Severity,
		f.FilePath, f.LineStart, f.LineEnd, f.Title, f.Detail, f.LLMSummary, f.LLMSeverity,
		domain.FindingOpen, nil, f.FirstSeenAt, f.LastSeenAt, nil,
		f.Category, f.SecuritySeverity, f.Source); err != nil {
		return false, fmt.Errorf("insert finding: %w", err)
	}
	return true, nil
}

// ResolveStaleFindings stamps resolved_at on this project's OPEN findings FROM
// THE GIVEN SOURCE whose fingerprint wasn't seen in the latest full scan/ingest
// (fixed in code). Scoping by source keeps producers independent — a CI ingest
// resolves stale CI findings, not the built-in scan's, and vice versa. Pushed
// and dismissed findings are left alone. Returns the number resolved.
func (s *Store) ResolveStaleFindings(ctx context.Context, projectID, source string, seen map[string]bool, now time.Time) (int, error) {
	rows, err := s.DB.QueryContext(ctx,
		`SELECT id, fingerprint FROM code_findings
		 WHERE project_id = ? AND source = ? AND status = ? AND resolved_at IS NULL`,
		projectID, source, domain.FindingOpen)
	if err != nil {
		return 0, fmt.Errorf("scan open findings: %w", err)
	}
	var stale []string
	for rows.Next() {
		var id, fp string
		if err := rows.Scan(&id, &fp); err != nil {
			rows.Close()
			return 0, err
		}
		if !seen[fp] {
			stale = append(stale, id)
		}
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return 0, err
	}
	for _, id := range stale {
		if _, err := s.DB.ExecContext(ctx,
			`UPDATE code_findings SET resolved_at = ? WHERE id = ?`, now, id); err != nil {
			return 0, fmt.Errorf("resolve finding %s: %w", id, err)
		}
	}
	return len(stale), nil
}

// ListCodeFindings returns a project's actionable findings — open and not
// resolved — ordered by severity then location. (Pushed/dismissed/resolved
// findings have left the results page.)
func (s *Store) ListCodeFindings(ctx context.Context, projectID string) ([]*domain.CodeFinding, error) {
	rows, err := s.DB.QueryContext(ctx,
		`SELECT `+findingColumns+` FROM code_findings
		 WHERE project_id = ? AND status = ? AND resolved_at IS NULL
		 ORDER BY `+severityRank+`, file_path, line_start`,
		projectID, domain.FindingOpen)
	if err != nil {
		return nil, fmt.Errorf("list findings: %w", err)
	}
	defer rows.Close()
	out := []*domain.CodeFinding{}
	for rows.Next() {
		f, err := scanFinding(rows.Scan)
		if err != nil {
			return nil, err
		}
		out = append(out, f)
	}
	return out, rows.Err()
}

// HasOpenSecurityFinding reports whether the project has any LIVE finding with a
// security-severity at or above minScore — the governance security gate's query.
// "Live" = not resolved (still in the code) and not dismissed (not an accepted
// risk); a pushed finding still counts, because turning a vuln into a work item
// doesn't make the vuln go away. Fix it (resolved) or dismiss it (justified) to
// clear the gate.
func (s *Store) HasOpenSecurityFinding(ctx context.Context, projectID string, minScore float64) (bool, error) {
	var n int
	if err := s.DB.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM code_findings
		 WHERE project_id = ? AND resolved_at IS NULL AND status != ? AND security_severity >= ?`,
		projectID, domain.FindingDismissed, minScore).Scan(&n); err != nil {
		return false, fmt.Errorf("count security findings: %w", err)
	}
	return n > 0, nil
}

func (s *Store) GetCodeFinding(ctx context.Context, id string) (*domain.CodeFinding, error) {
	f, err := scanFinding(s.DB.QueryRowContext(ctx,
		`SELECT `+findingColumns+` FROM code_findings WHERE id = ?`, id).Scan)
	if err != nil {
		return nil, fmt.Errorf("finding %s: %w", id, storage.ErrNotFound)
	}
	return f, nil
}

// UpdateCodeFindingStatus moves a finding to pushed/dismissed (and records the
// scratchpad item it became, on push).
func (s *Store) UpdateCodeFindingStatus(ctx context.Context, id, status, pushedItemID string, now time.Time) error {
	var pushed any
	if pushedItemID != "" {
		pushed = pushedItemID
	}
	res, err := s.DB.ExecContext(ctx,
		`UPDATE code_findings SET status = ?, pushed_item_id = ?, last_seen_at = ? WHERE id = ?`,
		status, pushed, now, id)
	if err != nil {
		return fmt.Errorf("update finding status: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("finding %s: %w", id, storage.ErrNotFound)
	}
	return nil
}

// --- scans ---

func (s *Store) CreateAnalysisScan(ctx context.Context, sc *domain.AnalysisScan) error {
	var finished any
	if sc.FinishedAt != nil {
		finished = *sc.FinishedAt
	}
	_, err := s.DB.ExecContext(ctx,
		`INSERT INTO analysis_scans
			(id, project_id, trigger, started_at, finished_at, files_scanned,
			 findings_new, findings_resolved, skipped, error)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		sc.ID, sc.ProjectID, sc.Trigger, sc.StartedAt, finished, sc.FilesScanned,
		sc.FindingsNew, sc.FindingsResolved, sc.Skipped, sc.Error)
	if err != nil {
		return fmt.Errorf("create analysis scan: %w", err)
	}
	return nil
}

func (s *Store) LatestAnalysisScan(ctx context.Context, projectID string) (*domain.AnalysisScan, error) {
	sc := &domain.AnalysisScan{}
	var finished sql.NullTime
	err := s.DB.QueryRowContext(ctx,
		`SELECT id, project_id, trigger, started_at, finished_at, files_scanned,
			findings_new, findings_resolved, skipped, error
		 FROM analysis_scans WHERE project_id = ? ORDER BY started_at DESC LIMIT 1`, projectID).
		Scan(&sc.ID, &sc.ProjectID, &sc.Trigger, &sc.StartedAt, &finished, &sc.FilesScanned,
			&sc.FindingsNew, &sc.FindingsResolved, &sc.Skipped, &sc.Error)
	if err != nil {
		return nil, fmt.Errorf("latest scan for %s: %w", projectID, storage.ErrNotFound)
	}
	if finished.Valid {
		t := finished.Time
		sc.FinishedAt = &t
	}
	return sc, nil
}

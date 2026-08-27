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

package codeanalysis

// Ingest is the shared core behind every finding producer — the built-in scan,
// the HTTP ingest endpoint, the MCP tool, and the watched directory. Each parses
// or produces []Finding and calls Ingest; the upsert + source-scoped reconcile +
// scan-row bookkeeping lives here once.

import (
	"context"
	"strings"
	"time"

	"codedistill/internal/domain"
	"codedistill/internal/id"
)

// IngestStore is the narrow storage slice the ingest core needs (satisfied by
// the full storage.Storage).
type IngestStore interface {
	UpsertCodeFinding(ctx context.Context, f *domain.CodeFinding) (bool, error)
	ResolveStaleFindings(ctx context.Context, projectID, source string, seen map[string]bool, now time.Time) (int, error)
	CreateAnalysisScan(ctx context.Context, sc *domain.AnalysisScan) error
}

// Ingest upserts a producer's findings (tagged with source), reconciles the ones
// that resolved FOR THAT SOURCE, and records a scan row. Returns new + resolved
// counts. A per-finding upsert error is logged by the caller via the returned
// err (best-effort: the rest still persist).
func Ingest(ctx context.Context, store IngestStore, projectID, source, trigger string, started time.Time,
	findings []Finding, filesScanned int, skipped, notes []string) (newCount, resolved int, err error) {
	seen := make(map[string]bool, len(findings))
	for _, f := range findings {
		seen[f.Fingerprint] = true
		now := time.Now().UTC()
		df := &domain.CodeFinding{
			ID:               id.New(),
			ProjectID:        projectID,
			Fingerprint:      f.Fingerprint,
			Analyzer:         f.Analyzer,
			RuleID:           f.RuleID,
			Severity:         f.Severity,
			Category:         f.Category,
			SecuritySeverity: f.SecuritySeverity,
			Source:           source,
			FilePath:         f.FilePath,
			LineStart:        f.LineStart,
			LineEnd:          f.LineEnd,
			Title:            f.Title,
			Detail:           f.Detail,
			Status:           domain.FindingOpen,
			FirstSeenAt:      now,
			LastSeenAt:       now,
		}
		inserted, uerr := store.UpsertCodeFinding(ctx, df)
		if uerr != nil {
			err = uerr
			continue
		}
		if inserted {
			newCount++
		}
	}
	resolved, rerr := store.ResolveStaleFindings(ctx, projectID, source, seen, time.Now().UTC())
	if rerr != nil && err == nil {
		err = rerr
	}
	finished := time.Now().UTC()
	scan := &domain.AnalysisScan{
		ID:               id.New(),
		ProjectID:        projectID,
		Trigger:          trigger,
		StartedAt:        started,
		FinishedAt:       &finished,
		FilesScanned:     filesScanned,
		FindingsNew:      newCount,
		FindingsResolved: resolved,
		Skipped:          strings.Join(skipped, "\n"),
		Error:            strings.Join(notes, "\n"),
	}
	if cerr := store.CreateAnalysisScan(ctx, scan); cerr != nil && err == nil {
		err = cerr
	}
	return newCount, resolved, err
}

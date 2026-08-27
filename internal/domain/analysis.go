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

package domain

import "time"

// Code Analysis (UC-14). Static analyzers detect; the user triages findings on
// a results page — pushing them into a scratchpad (where the classifier makes
// them bugs/todos) or dismissing them. The LLM triage/review passes are later
// slices; the fields are here from the start so the schema is stable.

// Finding status values.
const (
	FindingOpen      = "open"
	FindingPushed    = "pushed"
	FindingDismissed = "dismissed"
)

// Normalized severity scale (analyzer-native severities map onto this).
const (
	SeverityHigh   = "high"
	SeverityMedium = "medium"
	SeverityLow    = "low"
	SeverityInfo   = "info"
)

// CodeFinding is one issue an analyzer reported, tracked across re-scans by
// Fingerprint (analyzer + rule + file + location hash). ResolvedAt is set when a
// finding disappears from a fresh full scan (fixed in code) rather than deleted.
type CodeFinding struct {
	ID           string     `json:"id"`
	ProjectID    string     `json:"project_id"`
	Fingerprint  string     `json:"-"`
	Analyzer     string     `json:"analyzer"`
	RuleID       string     `json:"rule_id"`
	Severity     string     `json:"severity"`
	// Category buckets the finding (security|correctness|quality|performance)
	// from SARIF rule tags; SecuritySeverity is the CVSS-style 0–10 score SARIF
	// carries (0 when absent — the governance gate blocks at ≥ 7.0). Source is
	// the producer that reported it: scan (built-in) | ci | watched | mcp.
	Category         string  `json:"category"`
	SecuritySeverity float64 `json:"security_severity"`
	Source           string  `json:"source"`
	FilePath         string  `json:"file_path"`
	LineStart    int        `json:"line_start"`
	LineEnd      int        `json:"line_end"`
	Title        string     `json:"title"`
	Detail       string     `json:"detail"`
	LLMSummary   string     `json:"llm_summary,omitempty"`
	LLMSeverity  string     `json:"llm_severity,omitempty"`
	Status       string     `json:"status"`
	PushedItemID string     `json:"pushed_item_id,omitempty"`
	FirstSeenAt  time.Time  `json:"first_seen_at"`
	LastSeenAt   time.Time  `json:"last_seen_at"`
	ResolvedAt   *time.Time `json:"resolved_at,omitempty"`
}

// AnalysisScan records one scan run for the dashboard + the results-page status
// line. Skipped carries the per-analyzer "not installed → here's the install
// hint" notes so a missing analyzer is a graceful skip, never a failed scan.
type AnalysisScan struct {
	ID               string     `json:"id"`
	ProjectID        string     `json:"project_id"`
	Trigger          string     `json:"trigger"`
	StartedAt        time.Time  `json:"started_at"`
	FinishedAt       *time.Time `json:"finished_at,omitempty"`
	FilesScanned     int        `json:"files_scanned"`
	FindingsNew      int        `json:"findings_new"`
	FindingsResolved int        `json:"findings_resolved"`
	Skipped          string     `json:"skipped,omitempty"`
	Error            string     `json:"error,omitempty"`
}

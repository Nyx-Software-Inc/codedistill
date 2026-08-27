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

package api

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"codedistill/internal/codeanalysis"
	"codedistill/internal/domain"
	"codedistill/internal/events"
	"codedistill/internal/features"
	codegit "codedistill/internal/git"
	"codedistill/internal/id"
)

// analysisRunning tracks in-flight scans per project so a second "Scan now"
// can't pile on (and so the results page can show a spinner). In-memory is fine
// for a single-process desktop app — a scan interrupted by restart just clears.
var analysisRunning sync.Map // projectID -> bool

// runAnalysisScan handles POST /api/v1/projects/{id}/analysis/scan. It kicks off
// a background scan (gosec + staticcheck) and returns immediately; the client
// polls the latest-scan endpoint for completion. On-request only in slice 1.
func (s *Server) runAnalysisScan(w http.ResponseWriter, r *http.Request) {
	pid := r.PathValue("id")
	p, err := s.store.GetProject(r.Context(), pid)
	if err != nil {
		writeErr(w, statusFor(err), err)
		return
	}
	if p.RepoRoot == "" {
		writeMsg(w, http.StatusBadRequest, "set the project's repo_root before scanning")
		return
	}
	if _, loaded := analysisRunning.LoadOrStore(pid, true); loaded {
		writeMsg(w, http.StatusConflict, "a scan is already running for this project")
		return
	}
	repoRoot := p.RepoRoot
	go func() {
		defer analysisRunning.Delete(pid)
		// Detached from the request; bound so a wedged analyzer can't run forever.
		ctx, cancel := context.WithTimeout(context.Background(), 12*time.Minute)
		defer cancel()
		s.performScan(ctx, pid, repoRoot, "manual")
	}()
	writeJSON(w, http.StatusAccepted, map[string]string{"status": "started"})
}

// performScan runs the analyzers, upserts findings, resolves stale ones, and
// records the scan row. Always records a scan row (even on error) so the results
// page has a status line.
func (s *Server) performScan(ctx context.Context, projectID, repoRoot, trigger string) {
	started := time.Now().UTC()
	result := codeanalysis.Scan(ctx, repoRoot)
	_, _, _ = s.ingestFindings(ctx, projectID, "scan", trigger, started,
		result.Findings, result.FilesScanned, result.Skipped, result.Notes)
}

// ingestFindings persists a producer's findings via the shared codeanalysis.Ingest
// core (upsert + source-scoped reconcile + scan row), then nudges SSE subscribers.
func (s *Server) ingestFindings(ctx context.Context, projectID, source, trigger string, started time.Time,
	findings []codeanalysis.Finding, filesScanned int, skipped, notes []string) (newCount, resolved int, err error) {
	newCount, resolved, err = codeanalysis.Ingest(ctx, s.store, projectID, source, trigger, started,
		findings, filesScanned, skipped, notes)
	if err != nil {
		// Propagated (audit C6): a CI pipeline must see its ingest fail and
		// retry — a 200 with counts over dropped findings lets the security
		// governance gate pass green on missing data.
		s.log.Warn("analysis ingest failed", "project", projectID, "source", source, "err", err)
		return 0, 0, err
	}
	s.autoRoute(ctx, projectID) // Enterprise: route qualifying findings to a scratchpad
	s.bus.Publish(events.ItemsChanged)
	return newCount, resolved, nil
}

// maxSARIFBytes caps an ingested SARIF document (they can be large, but not
// unbounded) so a POST can't exhaust memory.
const maxSARIFBytes = 32 << 20 // 32 MiB

// ingestSARIF accepts a SARIF document from an external producer (a CI job, a
// security pipeline, an agent) and folds its findings into the project — the
// Enterprise ingest surface (gated by features.Analysis at the route). It runs
// the exact same normalize + reconcile machinery as the built-in scan, tagged
// with the producer's source so reconciliation stays independent per producer.
func (s *Server) ingestSARIF(w http.ResponseWriter, r *http.Request) {
	pid := r.PathValue("id")
	if _, err := s.store.GetProject(r.Context(), pid); err != nil {
		writeErr(w, statusFor(err), err)
		return
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, maxSARIFBytes))
	if err != nil {
		writeMsg(w, http.StatusBadRequest, "read body: "+err.Error())
		return
	}
	// CI SARIF paths are already repo-relative, so there is no repoRoot to strip.
	findings, err := codeanalysis.ParseSARIF(body, "")
	if err != nil {
		writeMsg(w, http.StatusBadRequest, err.Error())
		return
	}
	source := strings.TrimSpace(r.URL.Query().Get("source"))
	if source == "" || source == "scan" {
		source = "ci" // never let an ingest masquerade as the built-in scan
	}
	newCount, resolved, err := s.ingestFindings(r.Context(), pid, source, "ingest", time.Now().UTC(),
		findings, 0, nil, nil)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]int{
		"findings": len(findings), "new": newCount, "resolved": resolved,
	})
}

// listFindings handles GET /api/v1/projects/{id}/analysis/findings.
func (s *Server) listFindings(w http.ResponseWriter, r *http.Request) {
	pid := r.PathValue("id")
	if _, err := s.store.GetProject(r.Context(), pid); err != nil {
		writeErr(w, statusFor(err), err)
		return
	}
	findings, err := s.store.ListCodeFindings(r.Context(), pid)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, findings)
}

// latestScan handles GET /api/v1/projects/{id}/analysis/scan/latest. Returns the
// most recent completed scan (or null) plus whether one is running now.
func (s *Server) latestScan(w http.ResponseWriter, r *http.Request) {
	pid := r.PathValue("id")
	if _, err := s.store.GetProject(r.Context(), pid); err != nil {
		writeErr(w, statusFor(err), err)
		return
	}
	_, running := analysisRunning.Load(pid)
	var scan *domain.AnalysisScan
	if sc, err := s.store.LatestAnalysisScan(r.Context(), pid); err == nil {
		scan = sc
	}
	writeJSON(w, http.StatusOK, map[string]any{"scan": scan, "running": running})
}

type pushFindingReq struct {
	ScratchpadID string `json:"scratchpad_id"`
}

// pushFinding handles POST /api/v1/analysis/findings/{id}/push. It creates a
// scratchpad item (carrying a code anchor to the offending file/line) on the
// target scratchpad and lets the classifier decide bug vs todo, then marks the
// finding pushed so it leaves the results page.
func (s *Server) pushFinding(w http.ResponseWriter, r *http.Request) {
	fid := r.PathValue("id")
	f, err := s.store.GetCodeFinding(r.Context(), fid)
	if err != nil {
		writeErr(w, statusFor(err), err)
		return
	}
	if f.Status != domain.FindingOpen {
		writeMsg(w, http.StatusConflict, "finding is not open")
		return
	}
	var req pushFindingReq
	if err := readJSON(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	if req.ScratchpadID == "" {
		writeMsg(w, http.StatusBadRequest, "scratchpad_id is required")
		return
	}
	if _, err := s.store.GetScratchpad(r.Context(), req.ScratchpadID); err != nil {
		writeErr(w, statusFor(err), err)
		return
	}

	itemID, err := s.pushFindingToScratchpad(r.Context(), f, req.ScratchpadID)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	s.recordEvent(r.Context(), "scratchpad_item", itemID, "created", "Pushed from code analysis", sourceUI)
	s.bus.Publish(events.ItemsChanged)
	writeJSON(w, http.StatusOK, map[string]string{"item_id": itemID})
}

// pushFindingToScratchpad creates a scratchpad item from a finding (content +
// a code anchor to the offending line), marks the finding pushed, and enqueues
// classification. Shared by the manual push handler and Enterprise auto-route.
func (s *Server) pushFindingToScratchpad(ctx context.Context, f *domain.CodeFinding, scratchpadID string) (string, error) {
	now := time.Now().UTC()
	content := fmt.Sprintf("[%s %s · %s] %s\n%s:%d",
		f.Analyzer, f.RuleID, f.Severity, f.Title, f.FilePath, f.LineStart)
	if f.Detail != "" {
		content += "\n\n" + f.Detail
	}
	nextRow, err := s.store.NextAvailableGridRow(ctx, scratchpadID)
	if err != nil {
		return "", err
	}
	gw, gh := naturalSize(content)
	item := &domain.ScratchpadItem{
		ID: id.New(), ScratchpadID: scratchpadID,
		ContentType: "text", Content: content,
		ClassificationState: "unprocessed",
		Tags:                normalizeTags(extractHashtags(content)),
		GridCol:             0, GridRow: nextRow, GridW: gw, GridH: gh,
		CreatedAt: now, UpdatedAt: now,
	}
	if err := s.store.CreateScratchpadItem(ctx, item); err != nil {
		return "", err
	}
	// Anchor to the offending code (best-effort: no repo/HEAD just means no pin).
	revision := ""
	if sha, err := codegit.HeadSHAForProject(ctx, s.store, f.ProjectID); err == nil {
		revision = sha
	}
	anchor := &domain.CodeAnchor{
		ID: id.New(), OwnerType: "scratchpad_item", OwnerID: item.ID,
		Kind: "file", Path: f.FilePath, LineStart: f.LineStart, LineEnd: f.LineEnd,
		Revision: revision, Provenance: "agent-suggested",
		Label:     fmt.Sprintf("%s %s", f.Analyzer, f.RuleID),
		CreatedAt: now, UpdatedAt: now,
	}
	if err := s.store.CreateCodeAnchor(ctx, anchor); err != nil {
		s.log.Warn("push finding: anchor create failed", "finding", f.ID, "err", err)
	}
	if err := s.store.UpdateCodeFindingStatus(ctx, f.ID, domain.FindingPushed, item.ID, now); err != nil {
		return "", err
	}
	if s.agent != nil {
		s.agent.Enqueue(item.ID)
	}
	return item.ID, nil
}

// Auto-route settings (Enterprise): the target scratchpad + the minimum severity
// that triggers routing a finding into it.
const (
	settingRouteTarget = "analysis.findings_target_scratchpad"
	settingRouteMinSev = "analysis.auto_route_min_severity"
)

// autoRoute creates a scratchpad item for each OPEN finding at or above the
// configured severity, into the configured target scratchpad. No-op unless a
// target is set and the paid Analysis feature is licensed. Idempotent: pushing
// flips a finding to 'pushed', so ListCodeFindings won't return it again.
func (s *Server) autoRoute(ctx context.Context, projectID string) {
	if !features.Enabled(features.Analysis) {
		return
	}
	targetName := s.projectSettingString(ctx, projectID, settingRouteTarget)
	if targetName == "" {
		return
	}
	pads, err := s.store.ListScratchpads(ctx, projectID)
	if err != nil {
		return
	}
	targetID := ""
	for _, p := range pads {
		if strings.EqualFold(p.Name, targetName) {
			targetID = p.ID
			break
		}
	}
	if targetID == "" {
		return // the configured scratchpad doesn't exist (yet)
	}
	minSev := s.projectSettingString(ctx, projectID, settingRouteMinSev)
	if minSev == "" {
		minSev = "high" // conservative default: only high routes
	}
	minRank := severityRank(minSev)
	findings, err := s.store.ListCodeFindings(ctx, projectID) // open, not-yet-pushed
	if err != nil {
		return
	}
	for _, f := range findings {
		if severityRank(f.Severity) < minRank {
			continue
		}
		if _, err := s.pushFindingToScratchpad(ctx, f, targetID); err != nil {
			s.log.Warn("auto-route push failed", "finding", f.ID, "err", err)
		}
	}
}

func severityRank(s string) int {
	switch s {
	case "high":
		return 3
	case "medium":
		return 2
	case "low":
		return 1
	default:
		return 0
	}
}

// projectSettingString reads a string-valued project setting ("" when unset).
func (s *Server) projectSettingString(ctx context.Context, projectID, key string) string {
	st, err := s.store.GetProjectSetting(ctx, projectID, key)
	if err != nil || st == nil {
		return ""
	}
	var v string
	if json.Unmarshal(st.Value, &v) != nil {
		return ""
	}
	return v
}

// dismissFinding handles POST /api/v1/analysis/findings/{id}/dismiss.
func (s *Server) dismissFinding(w http.ResponseWriter, r *http.Request) {
	fid := r.PathValue("id")
	if _, err := s.store.GetCodeFinding(r.Context(), fid); err != nil {
		writeErr(w, statusFor(err), err)
		return
	}
	if err := s.store.UpdateCodeFindingStatus(r.Context(), fid, domain.FindingDismissed, "", time.Now().UTC()); err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	s.bus.Publish(events.ItemsChanged)
	w.WriteHeader(http.StatusNoContent)
}


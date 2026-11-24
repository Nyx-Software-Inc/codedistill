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

//go:build oss

package api

import (
	"context"
	"net/http"
	"time"

	"codedistill/internal/jobqueue"
	"codedistill/internal/modelprobe"
)

// Fixing findings runs outside coding agents through the Conductor, which is
// part of CodeDistill Pro. The workflow row exists in every edition, so say so
// rather than leaving the start button to fail obscurely.
func (s *Server) submitFixFindings(w http.ResponseWriter, _ *http.Request, _ string, _ map[string]string, _ string) {
	writeMsg(w, http.StatusPaymentRequired, "fixing findings with an agent is part of CodeDistill Pro")
}

// registerFixRunner: no Conductor, so no fix jobs to run.
func (s *Server) registerFixRunner(*jobqueue.Scheduler) {}

// AgentMCPHandler: no Conductor, so no agents to serve.
func (s *Server) AgentMCPHandler() http.Handler { return nil }

// The job card's read side: no Conductor, so no fix jobs and nothing fixable.
func (s *Server) fixJobItems(w http.ResponseWriter, _ *http.Request) {
	writeMsg(w, http.StatusNotFound, "fixing findings with an agent is part of CodeDistill Pro")
}

func (s *Server) fixableItems(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{})
}

// The security knowledge base is part of the security agent: CodeDistill Pro.
func (s *Server) StartSecurityKnowledge(context.Context) {}

func (s *Server) securityKnowledge(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"refresh_enabled": false, "stale": []string{}, "sources": []any{}})
}

func (s *Server) refreshSecurityKnowledge(w http.ResponseWriter, _ *http.Request) {
	writeMsg(w, http.StatusPaymentRequired, "the security knowledge base is part of CodeDistill Pro")
}

// Agents run under the Conductor: CodeDistill Pro.
func (s *Server) agentsStatus(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"connectors": map[string]any{}, "roles": []any{}})
}

// Claude Code runs agents, which are part of CodeDistill Pro.
func probeClaudeCode(_ context.Context, model string, _ int) modelprobe.Result {
	return modelprobe.Result{Outcome: modelprobe.ModelUnusable, Model: model, CheckedAt: time.Now().UTC(),
		Detail: "running agents through Claude Code is part of CodeDistill Pro"}
}

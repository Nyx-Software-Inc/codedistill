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
	"net/http"
	"time"

	"codedistill/internal/domain"
	"codedistill/internal/id"
	"codedistill/internal/modelprobe"
	"codedistill/internal/secrets"
)

// Model providers and the roles that use them.
//
// The API never returns an API key. A provider carries HasAPIKey and nothing
// more, so a key cannot leave through a response body, a browser devtools
// panel, or a support bundle someone pastes into a ticket.

type providerRequest struct {
	Name          string `json:"name"`
	Protocol      string `json:"protocol"`
	Endpoint      string `json:"endpoint"`
	Model         string `json:"model"`
	APIKey        string `json:"api_key"` // inbound only; never echoed back
	ContextTokens int    `json:"context_tokens"`
	IsLocal       bool   `json:"is_local"`
	Enabled       bool   `json:"enabled"`
}

// localityOf clamps the caller's "nothing leaves this machine" claim.
//
// The flag drives a privacy statement the product repeats back — "this sends
// the document off this machine", suppressed when it is set — so it must not be
// settable by the person being reassured. Where the destination is provably
// remote the claim is overruled; where it is genuinely uncertain (a loopback
// endpoint, which an SSH tunnel imitates perfectly) the caller's answer stands,
// because nothing here knows better.
func localityOf(req providerRequest) bool {
	if domain.ProvenRemote(req.Protocol, req.Endpoint) {
		return false
	}
	return req.IsLocal
}

func (s *Server) listProviders(w http.ResponseWriter, r *http.Request) {
	rows, err := s.store.ListModelProviders(r.Context())
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	if rows == nil {
		rows = []*domain.ModelProvider{}
	}
	writeJSON(w, http.StatusOK, rows)
}

func (s *Server) createProvider(w http.ResponseWriter, r *http.Request) {
	var req providerRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	enc, err := s.encryptKey(req.APIKey)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	now := time.Now().UTC()
	p := &domain.ModelProvider{
		ID: id.New(), Name: req.Name, Protocol: req.Protocol,
		Endpoint: req.Endpoint, Model: req.Model, APIKey: enc,
		ContextTokens: req.ContextTokens, IsLocal: localityOf(req),
		Enabled: true, CreatedAt: now, UpdatedAt: now,
	}
	if p.ContextTokens <= 0 {
		// Ollama's own default, and the number that silently truncated every
		// oversized prompt in this product. Stored explicitly so it is visible
		// rather than implied.
		p.ContextTokens = 4096
	}
	if err := s.store.CreateModelProvider(r.Context(), p); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	p.APIKey, p.HasAPIKey = "", enc != ""
	writeJSON(w, http.StatusCreated, p)
}

func (s *Server) updateProvider(w http.ResponseWriter, r *http.Request) {
	var req providerRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	// An empty key means "leave the stored one": the UI is never shown a key,
	// so it always sends back empty on an unrelated edit.
	enc := ""
	if req.APIKey != "" {
		var err error
		if enc, err = s.encryptKey(req.APIKey); err != nil {
			writeErr(w, http.StatusInternalServerError, err)
			return
		}
	}
	p := &domain.ModelProvider{
		ID: r.PathValue("id"), Name: req.Name, Protocol: req.Protocol,
		Endpoint: req.Endpoint, Model: req.Model, APIKey: enc,
		ContextTokens: req.ContextTokens, IsLocal: localityOf(req),
		Enabled: req.Enabled, UpdatedAt: time.Now().UTC(),
	}
	if err := s.store.UpdateModelProvider(r.Context(), p); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) deleteProvider(w http.ResponseWriter, r *http.Request) {
	if err := s.store.DeleteModelProvider(r.Context(), r.PathValue("id")); err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// testProvider runs a real probe and records the outcome.
//
// This is the endpoint that makes configuration trustworthy: it generates, and
// when the provider claims a context window it proves it by sending an
// oversized prompt and reading back what the server says it actually read.
// Every number in the response is observed, not configured.
func (s *Server) testProvider(w http.ResponseWriter, r *http.Request) {
	p, err := s.store.GetModelProvider(r.Context(), r.PathValue("id"))
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	if p == nil {
		writeMsg(w, http.StatusNotFound, "no such provider")
		return
	}
	key, err := s.decryptKey(p.APIKey)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}

	// Bounded independently of the request: a wedged provider must not hold an
	// HTTP handler open, and the context proof needs room to actually run.
	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Minute)
	defer cancel()

	res := modelprobe.Probe(ctx, nil, modelprobe.Target{
		Protocol: p.Protocol, Endpoint: p.Endpoint, Model: p.Model,
		APIKey: key, NeedContext: p.ContextTokens,
	}, nil)

	detail := ""
	if !res.OK() {
		detail = res.Detail
	}
	if err := s.store.RecordProviderHealth(r.Context(), p.ID, res.OK(), detail, res.CheckedAt); err != nil {
		s.log.Warn("could not record provider health", "provider", p.ID, "err", err)
	}
	writeJSON(w, http.StatusOK, res)
}

// listWorkers returns which model serves each worker type in a scope, plus
// the warning that matters: an epistemic pair pointed at one provider is a
// challenger sharing the solutioner's blind spots.
func (s *Server) listWorkers(w http.ResponseWriter, r *http.Request) {
	projectID := r.URL.Query().Get("project_id")
	bindings, err := s.store.ListWorkerModels(r.Context(), projectID)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	if bindings == nil {
		bindings = map[string]string{}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"workers":               bindings,
		"known_types":           domain.WorkerTypes,
		"epistemic_pair_shared": domain.EpistemicPairShared(bindings),
	})
}

func (s *Server) setWorkerModel(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ProjectID  string `json:"project_id"`
		ProviderID string `json:"provider_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	workerType := r.PathValue("type")
	// An empty provider clears the binding, falling the worker back to the
	// next scope rather than leaving it pointed at nothing.
	if req.ProviderID == "" {
		if err := s.store.ClearWorkerModel(r.Context(), workerType, req.ProjectID); err != nil {
			writeErr(w, http.StatusBadRequest, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if err := s.store.SetWorkerModel(r.Context(), workerType, req.ProjectID, req.ProviderID, time.Now().UTC()); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// encryptKey opens (or creates) the keyring only when there is something to
// protect, so a machine that configures no credentials never grows a key file.
func (s *Server) encryptKey(plain string) (string, error) {
	if plain == "" {
		return "", nil
	}
	path, err := secrets.DefaultKeyPath()
	if err != nil {
		return "", err
	}
	kr, err := secrets.OpenOrCreate(path)
	if err != nil {
		return "", err
	}
	return kr.Encrypt(plain)
}

func (s *Server) decryptKey(stored string) (string, error) {
	if stored == "" {
		return "", nil
	}
	path, err := secrets.DefaultKeyPath()
	if err != nil {
		return "", err
	}
	kr, err := secrets.Open(path)
	if err != nil {
		return "", err
	}
	return kr.Decrypt(stored)
}

// discoverModels lists what a candidate provider offers, and describes one
// model when asked.
//
// This operates on an UNSAVED provider on purpose: a user configuring a
// connection should be able to see the real model list before committing to a
// row, and a model string that is merely mistyped should fail while they are
// looking at it rather than at call time with a 404.
//
// The request may carry a provider id instead of a key, so an existing
// provider can be re-queried without the UI ever holding its credential.
func (s *Server) discoverModels(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ProviderID string `json:"provider_id"`
		Protocol   string `json:"protocol"`
		Endpoint   string `json:"endpoint"`
		APIKey     string `json:"api_key"`
		Model      string `json:"model"` // when set, also describe this one
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}

	target := modelprobe.Target{
		Protocol: req.Protocol, Endpoint: req.Endpoint,
		Model: req.Model, APIKey: req.APIKey,
	}
	// An existing provider supplies its own stored credential, so the browser
	// never needs to hold one to re-query.
	if req.ProviderID != "" {
		p, err := s.store.GetModelProvider(r.Context(), req.ProviderID)
		if err != nil || p == nil {
			writeMsg(w, http.StatusNotFound, "no such provider")
			return
		}
		key, err := s.decryptKey(p.APIKey)
		if err != nil {
			writeErr(w, http.StatusInternalServerError, err)
			return
		}
		target = modelprobe.Target{
			Protocol: p.Protocol, Endpoint: p.Endpoint,
			Model: firstNonEmpty(req.Model, p.Model), APIKey: key,
		}
	}

	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()

	out := map[string]any{}
	models, err := modelprobe.ListModels(ctx, nil, target)
	if err != nil {
		// Reported rather than failed: describing a named model may still
		// work even when listing does not.
		out["error"] = err.Error()
	}
	if models == nil {
		models = []modelprobe.ModelSummary{}
	}
	out["models"] = models

	if target.Model != "" {
		if d, err := modelprobe.Describe(ctx, nil, target); err == nil {
			out["described"] = d
		} else {
			out["describe_error"] = err.Error()
		}
	}
	writeJSON(w, http.StatusOK, out)
}

func firstNonEmpty(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

// listVendors returns who you can connect to, including the ones this product
// cannot reach yet.
//
// Listing an unsupported vendor with a reason beats omitting it: someone
// looking for Anthropic should find out here, with a workaround, rather than
// concluding the product does not do cloud models at all.
func (s *Server) listVendors(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, modelprobe.KnownVendors)
}

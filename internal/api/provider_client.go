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
	"fmt"
	"net/http"
	"time"

	"codedistill/internal/domain"
	"codedistill/internal/ollama"
	"codedistill/internal/secrets"
	"codedistill/internal/storage"
)

// WorkerClient turns a worker type into something that can actually be called.
//
// Until this existed, providers could be configured, probed and bound to roles
// while nothing consumed them — the whole feature stopped one step short of
// being usable. This is that step.
//
// Returning (nil, nil) for an unbound worker is deliberate and load-bearing: it
// means "use whatever the app was started with", which is what keeps an
// existing install working with nothing configured.
func WorkerClient(ctx context.Context, store storage.Storage, workerType, projectID string, timeout time.Duration) (*ollama.Client, *domain.ModelProvider, error) {
	return workerClientFor(ctx, store, "", 0, workerType, projectID, timeout)
}

// StepClient resolves the model for one STEP of a workflow.
//
// This is what the workflow UI configures, and until now nothing called it:
// decompose resolved through the global worker binding and ignored the step
// entirely, so setting a model on a workflow persisted correctly and had no
// effect whatsoever. Configuration that silently does nothing is worse than
// configuration that is missing.
func StepClient(ctx context.Context, store storage.Storage, workflowID string, ordinal int, workerType, projectID string, timeout time.Duration) (*ollama.Client, *domain.ModelProvider, error) {
	return workerClientFor(ctx, store, workflowID, ordinal, workerType, projectID, timeout)
}

func workerClientFor(ctx context.Context, store storage.Storage, workflowID string, ordinal int, workerType, projectID string, timeout time.Duration) (*ollama.Client, *domain.ModelProvider, error) {
	var p *domain.ModelProvider
	var err error
	if workflowID != "" {
		// Step first: the most specific answer wins, and it already falls back
		// to the global worker binding inside ResolveStepProvider.
		p, err = store.ResolveStepProvider(ctx, workflowID, ordinal, projectID)
	} else {
		p, err = store.ResolveWorkerModel(ctx, workerType, projectID)
	}
	if err != nil || p == nil {
		return nil, nil, err
	}
	if !p.Enabled {
		return nil, nil, nil // disabled falls back rather than failing
	}

	key := ""
	if p.APIKey != "" {
		path, err := secrets.DefaultKeyPath()
		if err != nil {
			return nil, nil, err
		}
		kr, err := secrets.Open(path)
		if err != nil {
			// A provider with a key we cannot decrypt must not silently fall
			// back to the default model: the user configured something
			// specific, and quietly using a different one is worse than saying
			// the key is unreadable.
			return nil, nil, fmt.Errorf("provider %q has a stored key that cannot be read (is %s missing?): %w", p.Name, path, err)
		}
		if key, err = kr.Decrypt(p.APIKey); err != nil {
			return nil, nil, fmt.Errorf("provider %q: %w", p.Name, err)
		}
	}

	if timeout <= 0 {
		timeout = 10 * time.Minute
	}
	opts := []ollama.Option{
		ollama.WithEndpoint(p.Endpoint),
		ollama.WithModel(p.Model),
		ollama.WithProtocol(ollama.ParseProtocol(p.Protocol)),
		ollama.WithAPIKey(key),
		ollama.WithHTTPClient(&http.Client{Timeout: timeout}),
	}
	// Only meaningful for Ollama, and only correct to send when we know the
	// number: it is the setting whose absence silently truncated every
	// oversized prompt in this product for a year.
	if p.ContextTokens > 0 && p.Protocol == "ollama" {
		opts = append(opts, ollama.WithContextTokens(p.ContextTokens))
	}
	return ollama.New(opts...), p, nil
}

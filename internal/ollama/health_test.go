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

package ollama

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// The failure this exists for: after a laptop suspend, Ollama served /api/tags
// in a millisecond while /api/generate never returned. The old check asked only
// the first question, so the agent saw a healthy model, queued work, and every
// item hung. A health check that passes while the thing it guards is broken is
// worse than none.
func TestReachableCatchesAWedgedModel(t *testing.T) {
	// Released by the test, not by the request context: httptest's Close waits
	// for outstanding handlers, and a handler parked on r.Context() may never
	// be woken, which hangs the test rather than the server.
	release := make(chan struct{})
	hit := make(chan struct{}, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/tags":
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`{"models":[]}`))
		case "/api/generate":
			select {
			case hit <- struct{}{}:
			default:
			}
			<-release // never answers, exactly like the wedged case
		}
	}))
	defer func() { close(release); srv.Close() }()

	c := New(WithEndpoint(srv.URL), WithModel("m"),
		WithHTTPClient(&http.Client{Timeout: 2 * time.Second}))

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if c.Reachable(ctx) {
		t.Fatal("reported healthy while generation hangs — the exact bug this replaces")
	}
	select {
	case <-hit:
	default:
		t.Error("never exercised generation; /api/tags alone proves nothing")
	}
}

func TestReachableAcceptsAWorkingServer(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		if r.URL.Path == "/api/generate" {
			w.Write([]byte(`{"response":"o","done":true}`))
			return
		}
		w.Write([]byte(`{"models":[]}`))
	}))
	defer srv.Close()

	c := New(WithEndpoint(srv.URL), WithModel("m"))
	if !c.Reachable(context.Background()) {
		t.Fatal("a server that answers both endpoints was reported unhealthy")
	}
}

// A server that is genuinely down must fail fast on the cheap endpoint rather
// than waiting out a generation timeout.
func TestReachableFailsFastWhenTheServerIsDown(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	url := srv.URL
	srv.Close() // nothing is listening now

	c := New(WithEndpoint(url), WithModel("m"))
	t0 := time.Now()
	if c.Reachable(context.Background()) {
		t.Fatal("reported healthy with nothing listening")
	}
	if el := time.Since(t0); el > 5*time.Second {
		t.Errorf("took %s to notice a dead server; the cheap check should be first", el)
	}
}

// A 500 from generation is still a model that cannot do the job.
func TestReachableRejectsAGenerationError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/generate" {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"models":[]}`))
	}))
	defer srv.Close()

	c := New(WithEndpoint(srv.URL), WithModel("m"))
	if c.Reachable(context.Background()) {
		t.Fatal("a model returning 500 was reported healthy")
	}
}

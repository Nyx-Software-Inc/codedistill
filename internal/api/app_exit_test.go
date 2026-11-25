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
	"net/http/httptest"
	"testing"
	"time"

	"codedistill/internal/storage/sqlite"
)

func TestAppExit(t *testing.T) {
	store, err := sqlite.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })
	if err := store.Migrate(context.Background()); err != nil {
		t.Fatal(err)
	}
	s := NewServer(store, nil, BuildInfo{}, nil, nil, nil, nil, nil)
	t.Cleanup(s.Close)
	srv := httptest.NewServer(s.Handler())
	defer srv.Close()

	var info struct {
		DesktopApp bool `json:"desktop_app"`
	}
	doJSON(t, srv, "GET", "/api/v1/app", nil, 200, &info)
	if info.DesktopApp {
		t.Fatal("a server that opened no app window says it is the desktop app")
	}
	// A server people reach from their own browsers is not one person's to stop.
	doJSON(t, srv, "POST", "/api/v1/app/exit", nil, 404, nil)

	exited := make(chan struct{})
	s.SetExit(func() { close(exited) })
	doJSON(t, srv, "GET", "/api/v1/app", nil, 200, &info)
	if !info.DesktopApp {
		t.Fatal("the desktop app does not say so")
	}
	doJSON(t, srv, "POST", "/api/v1/app/exit", nil, 202, nil)
	select {
	case <-exited:
	case <-time.After(3 * time.Second):
		t.Fatal("exit was accepted but the app did not stop")
	}
}

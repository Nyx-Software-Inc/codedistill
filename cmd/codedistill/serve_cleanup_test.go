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

package main

import (
	"context"
	"path/filepath"
	"sync/atomic"
	"testing"

	"codedistill/internal/ollama"
)

// cmdServe starts Ollama as a child process, then has several exit paths that
// used to return WITHOUT stopping it: the S3-without-a-license refusal, two
// blobstore init failures, and the non-localhost bind guard. Each one left an
// orphaned `ollama serve` holding its model in RAM — and since the desktop
// package now provisions a RAM-sized model, that orphan is qwen2.5:14b (~9GB)
// on a 16GB+ machine.
//
// The bind guard is the one a user hits repeatedly: get the -addr flag wrong,
// correct it, retry — stacking an orphan each time (CE-review item 16).
//
// The supervisor is replaced via the ensureOllamaRunning seam so this never
// spawns a real Ollama.
func TestServe_StopsOllamaOnEveryExitPath(t *testing.T) {
	cases := []struct {
		name string
		env  map[string]string
		addr string
		// noAuth with a non-local addr trips the bind guard.
		noAuth bool
	}{
		{
			name: "s3 endpoint without an enterprise license",
			env:  map[string]string{"BLOB_S3_ENDPOINT": "https://s3.example.invalid"},
			addr: "127.0.0.1:0",
		},
		{
			name: "blobstore root cannot be created",
			env:  map[string]string{"CODEDISTILL_BLOBS": "/dev/null/not-a-directory"},
			addr: "127.0.0.1:0",
		},
		{
			// Previously an os.Exit(1) — deferred cleanup could never run, so
			// this path was not merely unstopped, it was unstoppable.
			name:   "non-localhost bind without write-auth",
			addr:   "0.0.0.0:0",
			noAuth: true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			for k, v := range tc.env {
				t.Setenv(k, v)
			}
			// Neutralise ambient config that would otherwise change the path taken.
			for _, k := range []string{"BLOB_S3_ENDPOINT", "CODEDISTILL_BLOBS", "CODEDISTILL_LICENSE"} {
				if _, set := tc.env[k]; !set {
					t.Setenv(k, "")
				}
			}

			var stops atomic.Int32
			orig := ensureOllamaRunning
			ensureOllamaRunning = func(_ context.Context, _ *ollama.Supervisor) (func(), error) {
				return func() { stops.Add(1) }, nil
			}
			t.Cleanup(func() { ensureOllamaRunning = orig })

			db := filepath.Join(t.TempDir(), "codedistill.db")
			// appWindow "off" so no browser window is ever launched from a test.
			err := cmdServe(db, ollama.DefaultModel, "http://127.0.0.1:11434", "",
				tc.addr, "off", true, "", "", tc.noAuth, false)

			if err == nil {
				t.Fatal("expected cmdServe to fail on this path; it returned nil")
			}
			if got := stops.Load(); got != 1 {
				t.Errorf("Ollama stop ran %d times, want exactly 1.\n"+
					"cmdServe returned %v while leaving the child it started running.", got, err)
			}
		})
	}
}

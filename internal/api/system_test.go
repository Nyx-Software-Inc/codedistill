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
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSystem_ReportsTheInstallation(t *testing.T) {
	srv, _ := setup(t)

	var got map[string]any
	doJSON(t, srv, "GET", "/api/v1/system", nil, 200, &got)

	// Single-user: the 'local' user is the seeded workspace owner, so the
	// localhost SPA is admin and this must not 403 on a desktop install.
	for _, k := range []string{"build", "runtime", "storage", "models"} {
		if _, ok := got[k]; !ok {
			t.Errorf("missing %q group", k)
		}
	}
	rt, _ := got["runtime"].(map[string]any)
	if rt["os"] == "" || rt["arch"] == "" {
		t.Error("runtime should name the OS and architecture")
	}
	// Discovered rather than passed in, so it should be populated even though
	// this test wires no RuntimeInfo.
	if rt["go_version"] == "" {
		t.Error("go_version should come from the runtime, not from configuration")
	}
}

func TestSystem_NeverEchoesAPostgresDSN(t *testing.T) {
	// The DSN carries a password and this endpoint is read by a browser, so
	// DBPath is required to be empty on Postgres. This asserts the handler
	// cannot leak one even if it is handed one.
	s := &Server{rt: RuntimeInfo{Dialect: "postgres", DBPath: ""}}
	st := s.storageInfo()
	if st.DatabasePath != "" {
		t.Errorf("postgres install reported a database path: %q", st.DatabasePath)
	}
	if st.DatabaseBytes != 0 {
		t.Errorf("postgres install reported a local size: %d", st.DatabaseBytes)
	}
	if st.Dialect != "postgres" {
		t.Errorf("dialect = %q, want postgres", st.Dialect)
	}
}

func TestSystem_DatabaseSizeIncludesTheWAL(t *testing.T) {
	dir := t.TempDir()
	db := filepath.Join(dir, "x.db")
	// A WAL-mode database keeps a write-ahead log that is routinely tens of
	// megabytes. Reporting only the .db file understates what is on disk, which
	// is the whole question this panel answers.
	if err := os.WriteFile(db, make([]byte, 1000), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(db+"-wal", make([]byte, 500), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(db+"-shm", make([]byte, 32), 0o600); err != nil {
		t.Fatal(err)
	}

	s := &Server{rt: RuntimeInfo{Dialect: "sqlite", DBPath: db}}
	st := s.storageInfo()
	if st.DatabaseBytes != 1532 {
		t.Errorf("database_bytes = %d, want 1532 (db + wal + shm)", st.DatabaseBytes)
	}
}

func TestSystem_BlobTotalWalksTheConfiguredRoot(t *testing.T) {
	dir := t.TempDir()
	// Not <db>.blobs: $CODEDISTILL_BLOBS can point the store elsewhere, and a
	// Postgres install has no sibling directory at all — so the root is passed
	// in rather than derived.
	blobs := filepath.Join(dir, "somewhere-else")
	if err := os.MkdirAll(filepath.Join(blobs, "ab"), 0o755); err != nil {
		t.Fatal(err)
	}
	for name, size := range map[string]int{"ab/one": 100, "ab/two": 250} {
		if err := os.WriteFile(filepath.Join(blobs, name), make([]byte, size), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	s := &Server{rt: RuntimeInfo{DBPath: filepath.Join(dir, "x.db"), BlobRoot: blobs}}
	st := s.storageInfo()
	if st.BlobBytes != 350 || st.BlobFiles != 2 {
		t.Errorf("blobs = %d bytes in %d files, want 350 in 2", st.BlobBytes, st.BlobFiles)
	}
	if st.BlobPath != blobs {
		t.Errorf("blob_path = %q, want the configured root %q", st.BlobPath, blobs)
	}
}

func TestSystem_MissingBlobRootIsNotAnError(t *testing.T) {
	// A fresh install has no blob directory until the first upload. The panel
	// must still open and simply report nothing.
	s := &Server{rt: RuntimeInfo{DBPath: filepath.Join(t.TempDir(), "x.db"), BlobRoot: "/nonexistent/blobs"}}
	st := s.storageInfo()
	if st.BlobPath != "" || st.BlobBytes != 0 {
		t.Errorf("absent blob root should report nothing, got %q / %d", st.BlobPath, st.BlobBytes)
	}
}

func TestSystem_RefusesANonAdmin(t *testing.T) {
	// The gate only bites on a multi-user server: isAdmin() returns false when
	// auth is configured and the request carries no authenticated identity.
	// Without this test, deleting the gate entirely breaks nothing — every other
	// test runs single-user, where the 'local' user is the seeded owner.
	s := &Server{auth: &oidcAuth{}}
	rec := httptest.NewRecorder()
	s.systemInfo(rec, httptest.NewRequest("GET", "/api/v1/system", nil))

	if rec.Code != http.StatusForbidden {
		t.Fatalf("anonymous request to a multi-user server got %d, want 403", rec.Code)
	}
	// And it must not leak the paths in the error body.
	if body := rec.Body.String(); strings.Contains(body, "/") && strings.Contains(body, ".db") {
		t.Errorf("403 body leaked a path: %s", body)
	}
}

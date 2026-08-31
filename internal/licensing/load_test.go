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

package licensing

import (
	"strings"
	"testing"
)

// A database DSN is not a filesystem path, and must never be treated as one.
//
// ResolvePath used to hand any -database value to filepath.Dir to find a
// sibling license file. filepath.Dir keeps the DSN's userinfo, so the derived
// "path" carried the database PASSWORD — and Load embeds that path in the
// not-found reason that `codedistill license status` prints. The credential
// therefore reached terminal scrollback, support tickets and CI logs on the
// commercial build, where Postgres is a supported configuration.
//
// This was reported as cosmetic ("a nonsensical path in one diagnostic").
// It is not: it is low-severity credential disclosure (CE-review item 7).
func TestResolvePath_DSNNeverLeaksCredentials(t *testing.T) {
	const password = "sup3rs3cr3t"
	dsns := []string{
		"postgres://dbuser:" + password + "@dbhost:5432/codedistill?sslmode=require",
		"postgresql://dbuser:" + password + "@dbhost:5432/codedistill",
	}

	for _, dsn := range dsns {
		t.Setenv("CODEDISTILL_LICENSE", "") // don't let a real env var mask the path under test

		path, _ := ResolvePath("", dsn)
		if strings.Contains(path, password) {
			t.Errorf("ResolvePath leaked the DSN password into the returned path:\n  %s", path)
		}
		if strings.Contains(path, "://") || strings.Contains(path, "@") {
			t.Errorf("ResolvePath returned something DSN-shaped rather than a file path:\n  %s", path)
		}

		// The reason string is the part a user actually sees.
		st := Load("", dsn, "1.0.0")
		if strings.Contains(st.Reason, password) {
			t.Errorf("license status reason leaked the DSN password:\n  %s", st.Reason)
		}
	}
}

// A plain file path must still resolve to its sibling, unchanged — the fix
// must not alter the normal single-user case.
func TestResolvePath_FilePathStillUsesSibling(t *testing.T) {
	t.Setenv("CODEDISTILL_LICENSE", "")
	dir := t.TempDir()
	path, found := ResolvePath("", dir+"/codedistill.db")
	if found {
		t.Fatalf("no license was written; found should be false (path %s)", path)
	}
	if want := dir + "/" + FileName; path != want {
		t.Errorf("path = %q, want the DB's sibling %q", path, want)
	}
}

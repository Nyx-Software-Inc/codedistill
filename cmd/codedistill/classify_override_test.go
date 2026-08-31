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
	"os"
	"path/filepath"
	"strings"
	"testing"

	"codedistill/internal/ollama"
)

// -override went unvalidated straight into the INSERT, so a typo surfaced as a
// raw two-line SQLite dump:
//
//	classify failed: constraint failed: CHECK constraint failed:
//	classification_override IN ('todo', 'bug', 'kb', 'skip', 'use_case')
//	           OR classification_override IS NULL (275)
//
// classify is the documented smoke-test command, so this lands on evaluating
// users — and -override is the one flag that lets them try it without Ollama
// running (CE-review item 24).
func TestClassify_RejectsUnknownOverride(t *testing.T) {
	dir := t.TempDir()
	db := filepath.Join(dir, "codedistill.db")

	err := cmdClassify(db, ollama.DefaultModel, "http://127.0.0.1:1", "", "some content", "banana")
	if err == nil {
		t.Fatal("expected an error for -override banana")
	}
	msg := err.Error()

	if strings.Contains(msg, "CHECK constraint") || strings.Contains(msg, "275") {
		t.Errorf("the SQLite constraint leaked to the user:\n  %s", msg)
	}
	if !strings.Contains(msg, "banana") {
		t.Errorf("error should name the bad value:\n  %s", msg)
	}
	for _, v := range []string{"todo", "bug", "kb", "use_case", "skip"} {
		if !strings.Contains(msg, v) {
			t.Errorf("error should list %q as a valid value:\n  %s", v, msg)
		}
	}
	// Validation must happen BEFORE the database is opened — a bad flag should
	// not create a database file as a side effect.
	if _, statErr := os.Stat(db); !os.IsNotExist(statErr) {
		t.Error("a rejected -override created the database anyway; validate before opening storage")
	}
}

// use_case has been valid in the schema since v0.7.0 but was missing from the
// flag help, so the fix must not codify the stale four-value list.
func TestClassify_AcceptsUseCaseOverride(t *testing.T) {
	db := filepath.Join(t.TempDir(), "codedistill.db")

	// Endpoint is deliberately dead: this asserts the override passed validation,
	// not that classification succeeded. A validation failure names the override;
	// anything else means it got through.
	err := cmdClassify(db, ollama.DefaultModel, "http://127.0.0.1:1", "", "some content", "use_case")
	if err != nil && strings.Contains(err.Error(), "use_case") {
		t.Errorf("use_case was rejected but is valid in the schema:\n  %s", err)
	}
}

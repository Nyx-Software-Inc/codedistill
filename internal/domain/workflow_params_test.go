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

package domain

import "testing"

// A required parameter left blank must stop the submission, not the run.
//
// The failure being guarded against: the dialog posts an empty file path, a job
// row is created, it starts, and it dies a second later. The monitor then shows
// a failed run and the user concludes the workflow is broken — when what
// happened is that a field was empty and nobody said so.
func TestBlankRequiredParamIsRefusedBeforeAJobExists(t *testing.T) {
	decl := []WorkflowParam{
		{Key: "file", Label: "Document", Type: ParamFile, Required: true},
		{Key: "again", Label: "Read again", Type: ParamBool},
	}
	_, err := ValidateParams(decl, map[string]string{"again": "true"})
	if err == nil {
		t.Fatal("a blank required parameter was accepted; the run would fail instead of the submission")
	}
	pe, ok := err.(*ParamError)
	if !ok {
		t.Fatalf("error is %T, so the dialog cannot mark a field", err)
	}
	if pe.Key != "file" {
		t.Fatalf("blamed %q; the empty field was %q", pe.Key, "file")
	}
}

// A default fills in for an answer nobody gave, and the result carries only
// what has a value — an empty string stored as a param would reach the runner
// as an argument that looks supplied.
func TestDefaultsFillInAndEmptiesAreNotStored(t *testing.T) {
	decl := []WorkflowParam{
		{Key: "scope", Label: "Scope", Type: ParamText, Default: "internal"},
		{Key: "note", Label: "Note", Type: ParamText},
	}
	got, err := ValidateParams(decl, map[string]string{})
	if err != nil {
		t.Fatalf("optional params refused: %v", err)
	}
	if got["scope"] != "internal" {
		t.Fatalf("default not applied: scope = %q", got["scope"])
	}
	if _, present := got["note"]; present {
		t.Fatalf("an unanswered optional param was stored as %q", got["note"])
	}
}

// A select whose value is not on the list is refused.
//
// This is the one that matters for user-defined workflows: the choices come
// from a row, so a stale dialog can post a value that was removed. Accepting it
// hands the runner an argument it has never seen.
func TestSelectRejectsAValueThatIsNotOffered(t *testing.T) {
	decl := []WorkflowParam{{
		Key: "depth", Label: "Depth", Type: ParamSelect, Required: true,
		Options: []ParamOption{{Value: "quick", Label: "Quick"}, {Value: "full", Label: "Full"}},
	}}
	if _, err := ValidateParams(decl, map[string]string{"depth": "exhaustive"}); err == nil {
		t.Fatal("a value outside the declared choices was accepted")
	}
	if _, err := ValidateParams(decl, map[string]string{"depth": "full"}); err != nil {
		t.Fatalf("a declared choice was refused: %v", err)
	}
}

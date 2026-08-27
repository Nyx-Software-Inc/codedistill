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

package codeanalysis

import "testing"

// A gosec-style SARIF: explicit result.level + security-severity on the result.
const gosecSARIF = `{
  "version": "2.1.0",
  "runs": [{
    "tool": {"driver": {"name": "gosec", "rules": [
      {"id": "G104", "shortDescription": {"text": "Errors unhandled"},
       "properties": {"tags": ["security"], "security-severity": "7.5"},
       "defaultConfiguration": {"level": "error"}}
    ]}},
    "results": [{
      "ruleId": "G104",
      "level": "error",
      "message": {"text": "Errors unhandled."},
      "properties": {"security-severity": "7.5"},
      "locations": [{"physicalLocation": {
        "artifactLocation": {"uri": "internal/foo.go"},
        "region": {"startLine": 42, "endLine": 42, "snippet": {"text": "x, _ := f()"}}
      }}]
    }]
  }]
}`

// A semgrep-style SARIF: ruleId resolves a rule whose defaultConfiguration
// supplies the level; severity comes from tags; identity from partialFingerprints.
const semgrepSARIF = `{
  "version": "2.1.0",
  "runs": [{
    "tool": {"driver": {"name": "semgrep", "rules": [
      {"id": "kotlin.lang.security.sqli",
       "properties": {"tags": ["security", "cwe-89"]},
       "defaultConfiguration": {"level": "warning"}}
    ]}},
    "results": [{
      "ruleId": "kotlin.lang.security.sqli",
      "message": {"text": "Possible SQL injection"},
      "partialFingerprints": {"primaryLocationLineHash": "deadbeef"},
      "locations": [{"physicalLocation": {
        "artifactLocation": {"uri": "app/Main.kt"},
        "region": {"startLine": 10, "endLine": 12}
      }}]
    }]
  }]
}`

func TestParseSARIF_Gosec(t *testing.T) {
	fs, err := ParseSARIF([]byte(gosecSARIF), "")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(fs) != 1 {
		t.Fatalf("want 1 finding, got %d", len(fs))
	}
	f := fs[0]
	if f.Analyzer != "gosec" || f.RuleID != "G104" {
		t.Errorf("tool/rule = %q/%q", f.Analyzer, f.RuleID)
	}
	if f.Severity != "high" { // security-severity 7.5 ⇒ high
		t.Errorf("severity = %q, want high", f.Severity)
	}
	if f.SecuritySeverity != 7.5 {
		t.Errorf("security_severity = %v, want 7.5", f.SecuritySeverity)
	}
	if f.Category != "security" {
		t.Errorf("category = %q, want security", f.Category)
	}
	if f.FilePath != "internal/foo.go" || f.LineStart != 42 {
		t.Errorf("loc = %q:%d", f.FilePath, f.LineStart)
	}
	if f.Title != "Errors unhandled." {
		t.Errorf("title = %q", f.Title)
	}
	if f.Fingerprint == "" {
		t.Error("empty fingerprint")
	}
}

func TestParseSARIF_Semgrep(t *testing.T) {
	fs, err := ParseSARIF([]byte(semgrepSARIF), "")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(fs) != 1 {
		t.Fatalf("want 1 finding, got %d", len(fs))
	}
	f := fs[0]
	if f.Analyzer != "semgrep" || f.RuleID != "kotlin.lang.security.sqli" {
		t.Errorf("tool/rule = %q/%q", f.Analyzer, f.RuleID)
	}
	if f.Severity != "medium" { // no security-severity; level=warning ⇒ medium
		t.Errorf("severity = %q, want medium", f.Severity)
	}
	if f.Category != "security" { // tag "security"
		t.Errorf("category = %q, want security", f.Category)
	}
	if f.FilePath != "app/Main.kt" || f.LineStart != 10 || f.LineEnd != 12 {
		t.Errorf("loc = %q:%d-%d", f.FilePath, f.LineStart, f.LineEnd)
	}
	if f.Fingerprint == "" {
		t.Error("empty fingerprint")
	}
}

func TestParseSARIF_EmptyIsNotAnError(t *testing.T) {
	fs, err := ParseSARIF([]byte(`{"version":"2.1.0","runs":[{"tool":{"driver":{"name":"x"}},"results":[]}]}`), "")
	if err != nil {
		t.Fatalf("empty runs should not error: %v", err)
	}
	if len(fs) != 0 {
		t.Fatalf("want 0 findings, got %d", len(fs))
	}
}

func TestParseSARIF_MalformedErrors(t *testing.T) {
	if _, err := ParseSARIF([]byte(`{not json`), ""); err == nil {
		t.Fatal("malformed SARIF should error")
	}
}

func TestParseSARIF_FingerprintsDiffer(t *testing.T) {
	g, _ := ParseSARIF([]byte(gosecSARIF), "")
	s, _ := ParseSARIF([]byte(semgrepSARIF), "")
	if g[0].Fingerprint == s[0].Fingerprint {
		t.Error("distinct findings produced the same fingerprint")
	}
}

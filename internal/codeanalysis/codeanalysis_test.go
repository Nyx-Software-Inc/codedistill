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

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// TestScanIntegration runs the real analyzers against this repo. Guarded by an
// env var because it needs gosec + staticcheck on PATH (and is slow); enable
// with CODEANALYSIS_INTEGRATION=1. Validates that real analyzer JSON parses into
// findings end-to-end.
func TestScanIntegration(t *testing.T) {
	if os.Getenv("CODEANALYSIS_INTEGRATION") != "1" {
		t.Skip("set CODEANALYSIS_INTEGRATION=1 (needs gosec + staticcheck on PATH)")
	}
	res := Scan(context.Background(), "../..") // repo root from this package
	t.Logf("findings=%d files=%d skipped=%v notes=%v", len(res.Findings), res.FilesScanned, res.Skipped, res.Notes)
	if res.FilesScanned == 0 {
		t.Errorf("expected to count Go files")
	}
	for i, f := range res.Findings {
		if f.Analyzer == "" || f.RuleID == "" || f.FilePath == "" || f.Fingerprint == "" {
			t.Errorf("finding %d incomplete: %+v", i, f)
		}
		if i < 3 {
			t.Logf("  [%s %s · %s] %s:%d %s", f.Analyzer, f.RuleID, f.Severity, f.FilePath, f.LineStart, f.Title)
		}
	}
}

// TestScanRoutesNonGoToSemgrep verifies a non-Go repo (Kotlin marker) dispatches
// to semgrep, never the Go analyzers. When semgrep isn't installed the routing
// is still observable via the skip hint — so the test needs no tools present.
func TestScanRoutesNonGoToSemgrep(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "build.gradle.kts"), []byte("plugins { kotlin(\"jvm\") }\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	res := Scan(context.Background(), dir)
	for _, f := range res.Findings {
		if f.Analyzer == "gosec" || f.Analyzer == "staticcheck" {
			t.Fatalf("Kotlin repo must not run Go analyzers, got %q", f.Analyzer)
		}
	}
	if _, ok := resolveBinary("semgrep"); !ok {
		if !strings.Contains(strings.Join(res.Skipped, " "), "semgrep") {
			t.Fatalf("expected a semgrep skip hint when semgrep is absent, got Skipped=%v", res.Skipped)
		}
	}
}

// TestScanRoutesGoToNativeAnalyzers verifies a Go repo stays on gosec/staticcheck
// and never invokes semgrep.
func TestScanRoutesGoToNativeAnalyzers(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module x\n\ngo 1.22\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	res := Scan(context.Background(), dir)
	for _, f := range res.Findings {
		if f.Analyzer == "semgrep" {
			t.Fatal("Go repo must not run semgrep")
		}
	}
	if strings.Contains(strings.Join(res.Skipped, " "), "semgrep") {
		t.Fatalf("Go repo must not mention semgrep, got Skipped=%v", res.Skipped)
	}
}

// TestSemgrepParsing drives runSemgrep against a fake `semgrep` on PATH that
// emits a representative SARIF payload, proving the runner → ParseSARIF wiring
// maps real semgrep output into findings without the heavy, network-bound tool.
func TestSemgrepParsing(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake-binary harness uses a /bin/sh script")
	}
	const payload = `{
      "version":"2.1.0",
      "runs":[{
        "tool":{"driver":{"name":"semgrep","rules":[
          {"id":"kotlin.lang.security.hardcoded-secret",
           "properties":{"tags":["security"]},
           "defaultConfiguration":{"level":"error"}}
        ]}},
        "results":[{
          "ruleId":"kotlin.lang.security.hardcoded-secret","level":"error",
          "message":{"text":"Hardcoded secret"},
          "locations":[{"physicalLocation":{
            "artifactLocation":{"uri":"src/Main.kt"},
            "region":{"startLine":12,"endLine":14}
          }}]
        }]
      }]
    }`
	binDir := t.TempDir()
	script := "#!/bin/sh\ncat <<'SARIF'\n" + payload + "\nSARIF\n"
	if err := os.WriteFile(filepath.Join(binDir, "semgrep"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))

	repo := t.TempDir()
	if err := os.WriteFile(filepath.Join(repo, "build.gradle.kts"), []byte("plugins { kotlin(\"jvm\") }\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	res := Scan(context.Background(), repo)
	if len(res.Findings) != 1 {
		t.Fatalf("want 1 finding, got %d (skipped=%v notes=%v)", len(res.Findings), res.Skipped, res.Notes)
	}
	f := res.Findings[0]
	if f.Analyzer != "semgrep" || f.Severity != "high" || f.FilePath != "src/Main.kt" ||
		f.LineStart != 12 || f.LineEnd != 14 || f.Category != "security" || f.RuleID == "" || f.Fingerprint == "" {
		t.Fatalf("finding parsed wrong: %+v", f)
	}
}

// TestDetektParsing drives runDetekt against a fake `detekt` on PATH that writes
// SARIF to the --report sarif:<file> path it's handed, proving the file-based
// runner (runSARIFFileTool) → ParseSARIF wiring maps detekt output into findings.
func TestDetektParsing(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake-binary harness uses a /bin/sh script")
	}
	const payload = `{
      "version":"2.1.0",
      "runs":[{
        "tool":{"driver":{"name":"detekt","rules":[
          {"id":"detekt.style.MagicNumber","defaultConfiguration":{"level":"warning"}}
        ]}},
        "results":[{
          "ruleId":"detekt.style.MagicNumber","level":"warning",
          "message":{"text":"This expression contains a magic number."},
          "locations":[{"physicalLocation":{
            "artifactLocation":{"uri":"src/Main.kt"},
            "region":{"startLine":7,"endLine":7}
          }}]
        }]
      }]
    }`
	binDir := t.TempDir()
	// detekt writes SARIF to the file named in its `--report sarif:<path>` arg,
	// not stdout — the fake mirrors that so runSARIFFileTool is exercised.
	script := "#!/bin/sh\nfor a in \"$@\"; do case \"$a\" in sarif:*) out=\"${a#sarif:}\";; esac; done\n" +
		"cat > \"$out\" <<'SARIF'\n" + payload + "\nSARIF\n"
	if err := os.WriteFile(filepath.Join(binDir, "detekt"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))

	repo := t.TempDir()
	if err := os.WriteFile(filepath.Join(repo, "build.gradle.kts"), []byte("plugins { kotlin(\"jvm\") }\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	res := Scan(context.Background(), repo)

	var got *Finding
	for i := range res.Findings {
		if res.Findings[i].Analyzer == "detekt" {
			got = &res.Findings[i]
			break
		}
	}
	if got == nil {
		t.Fatalf("expected a detekt finding, got findings=%+v notes=%v skipped=%v", res.Findings, res.Notes, res.Skipped)
	}
	if got.RuleID != "detekt.style.MagicNumber" || got.FilePath != "src/Main.kt" || got.LineStart != 7 || got.Fingerprint == "" {
		t.Fatalf("detekt finding parsed wrong: %+v", *got)
	}
}

// TestKotlinWithoutDetektIsHonest proves the reporting fix: on a Kotlin repo with
// no dedicated analyzer installed, the scan must SAY so — an empty semgrep pass
// can't masquerade as a clean bill of health.
func TestKotlinWithoutDetektIsHonest(t *testing.T) {
	if _, ok := resolveBinary("detekt"); ok {
		t.Skip("detekt is installed here — this test asserts the missing-detekt path")
	}
	repo := t.TempDir()
	if err := os.WriteFile(filepath.Join(repo, "build.gradle.kts"), []byte("plugins { kotlin(\"jvm\") }\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	res := Scan(context.Background(), repo)

	if !strings.Contains(strings.Join(res.Skipped, " "), "detekt") {
		t.Errorf("expected a detekt install hint in Skipped, got %v", res.Skipped)
	}
	notes := strings.Join(res.Notes, " ")
	if !strings.Contains(notes, "clean bill") && !strings.Contains(notes, "Install detekt") {
		t.Errorf("expected an honest 'not a clean bill / install detekt' note, got %v", res.Notes)
	}
}

func TestSeverityNormalization(t *testing.T) {
	if normalizeStaticcheckSeverity("error") != "medium" || normalizeStaticcheckSeverity("warning") != "low" {
		t.Errorf("staticcheck severity mapping wrong")
	}
}

// TestSemgrepTimeoutIsSurfaced proves the never-silent guarantee: a semgrep that
// hangs past the deadline yields a NOTE, not a phantom "0 findings" (Slavko's bug).
func TestSemgrepTimeoutIsSurfaced(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake-binary harness uses a /bin/sh script")
	}
	binDir := t.TempDir()
	// A fake semgrep that sleeps far past the (test-shortened) deadline.
	if err := os.WriteFile(filepath.Join(binDir, "semgrep"), []byte("#!/bin/sh\nsleep 30\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	repo := t.TempDir()
	if err := os.WriteFile(filepath.Join(repo, "build.gradle.kts"), []byte("plugins { kotlin(\"jvm\") }\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	res := Scan(ctx, repo)
	if len(res.Findings) != 0 {
		t.Fatalf("expected no findings on timeout, got %d", len(res.Findings))
	}
	if !strings.Contains(strings.Join(res.Notes, " "), "timed out") {
		t.Fatalf("timeout must surface a note, got Notes=%v", res.Notes)
	}
}

func TestFingerprintStableAndDistinct(t *testing.T) {
	a := Finding{Analyzer: "gosec", RuleID: "G101", FilePath: "x.go", LineStart: 10}
	b := Finding{Analyzer: "gosec", RuleID: "G101", FilePath: "x.go", LineStart: 10}
	c := Finding{Analyzer: "gosec", RuleID: "G101", FilePath: "x.go", LineStart: 11}
	if fingerprint(a) != fingerprint(b) {
		t.Errorf("same finding must fingerprint identically")
	}
	if fingerprint(a) == fingerprint(c) {
		t.Errorf("different line must fingerprint differently")
	}
}

func TestRelPath(t *testing.T) {
	if got := relPath("/repo", "/repo/internal/x.go"); got != "internal/x.go" {
		t.Errorf("relPath = %q, want internal/x.go", got)
	}
	// Outside the repo root falls back to the original.
	if got := relPath("/repo", "/other/x.go"); got != "/other/x.go" {
		t.Errorf("relPath outside = %q, want passthrough", got)
	}
}

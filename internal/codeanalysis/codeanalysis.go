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

// Package codeanalysis runs static analyzers over a project's repository and
// normalizes their output into findings. The tools emit SARIF (the OASIS
// interchange format), so the shared ParseSARIF serves every one — gosec (Go
// security), detekt (Kotlin), and semgrep (broad multi-language) all go through
// it; only staticcheck (no SARIF support) keeps its own small JSON adapter. This
// is the same ingest path the external-ingest surfaces use.
//
// It is a thin ORCHESTRATOR, not an engine: it detects the repo's language and
// runs the SARIF-emitting analyzers that fit and are installed — the right
// dedicated tool first (detekt for Kotlin), with semgrep as the broad security
// net. A missing analyzer is a graceful per-tool skip with an install hint; a
// timeout or unparseable run becomes a NOTE. And when a language has no
// dedicated analyzer installed (e.g. Kotlin without detekt), the scan says so —
// because semgrep's coverage there is minimal, a "0 findings" must never
// masquerade as a clean bill of health.
//
// Analyzers run against the working tree at repo_root (read-only; they never
// mutate code), with the module cache the user already has, so module
// resolution just works. Each is timeout-bounded; a non-zero exit is normal
// (both tools exit non-zero when they find issues) and never aborts the scan.
package codeanalysis

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"codedistill/internal/domain"
	"codedistill/internal/verifyprofile"
)

// scanTimeout bounds a single analyzer invocation.
const scanTimeout = 4 * time.Minute

// Finding is one normalized analyzer result, ready to upsert as a CodeFinding.
type Finding struct {
	Fingerprint string
	Analyzer    string
	RuleID      string
	Severity    string // normalized: high|medium|low|info
	FilePath    string // repo-relative
	LineStart   int
	LineEnd     int
	Title       string
	Detail      string
	// Category buckets the finding for the normalized taxonomy:
	// security|correctness|quality|performance. Derived from SARIF rule tags
	// (empty when the producer gives no signal).
	Category string
	// SecuritySeverity is the CVSS-style 0–10 score SARIF carries in the
	// `security-severity` property. 0 when absent. The governance gate keys on
	// this (block at ≥ 7.0); it is independent of the coarse Severity bucket.
	SecuritySeverity float64
}

// Result is the outcome of a full scan: the findings plus the bookkeeping the
// scan row records (files scanned, analyzers skipped with install hints, and
// non-fatal per-analyzer notes).
type Result struct {
	Findings     []Finding
	FilesScanned int
	Skipped      []string // "gosec not installed — install: …"
	Notes        []string // non-fatal analyzer errors (e.g. build failure)
}

// Scan runs the right analyzers for the repo's language and returns normalized
// findings. It never returns an error: tool absence and tool failure are folded
// into Skipped/Notes so the caller always has a result to persist.
//
// Dispatch is by detected language (the same detector the Verification settings
// use):
//   - Go             → gosec + staticcheck (dedicated Go tools)
//   - Kotlin(Gradle) → detekt (the real Kotlin analyzer) + semgrep (broad net)
//   - everything else→ semgrep (infers languages from the files present, so it
//     degrades gracefully rather than misfiring Go tools at a non-Go tree)
func Scan(ctx context.Context, repoRoot string) Result {
	// Resolve to absolute so relPath can make analyzer paths (which are
	// absolute) repo-relative regardless of how the caller passed the root.
	if abs, err := filepath.Abs(repoRoot); err == nil {
		repoRoot = abs
	}
	res := Result{}
	lang := verifyprofile.Detect(repoRoot).Language
	switch {
	case lang == "Go":
		runGosec(ctx, repoRoot, &res)
		runStaticcheck(ctx, repoRoot, &res)
		res.FilesScanned = countSourceFiles(repoRoot, ".go")

	case strings.HasPrefix(lang, "Kotlin"):
		// detekt is the dedicated Kotlin analyzer; semgrep is a broad security
		// net whose Kotlin rules are thin. Run both and merge. If detekt isn't
		// installed, say so loudly — otherwise semgrep's near-empty Kotlin pass
		// would read as a clean project when nothing really examined the Kotlin.
		ranDetekt := runDetekt(ctx, repoRoot, &res)
		runSemgrep(ctx, repoRoot, &res)
		res.FilesScanned = countSourceFiles(repoRoot, ".kt", ".kts")
		if !ranDetekt {
			res.Notes = append(res.Notes,
				"kotlin: no dedicated Kotlin analyzer ran — semgrep's Kotlin coverage is minimal, "+
					"so an empty result here is NOT a clean bill of health. Install detekt for real Kotlin analysis.")
		}

	default:
		runSemgrep(ctx, repoRoot, &res)
	}
	return res
}

func runGosec(ctx context.Context, repoRoot string, res *Result) {
	bin, ok := resolveBinary("gosec")
	if !ok {
		res.Skipped = append(res.Skipped,
			"gosec not found on PATH or in the Go bin dir — install: go install github.com/securego/gosec/v2/cmd/gosec@latest")
		return
	}
	// gosec emits SARIF to stdout; ParseSARIF normalizes it (security-severity,
	// tags → category, rule fingerprints) the same way every other producer is.
	runSARIFTool(ctx, repoRoot, "gosec", bin, []string{"-fmt=sarif", "-quiet", "./..."}, res)
}

func runStaticcheck(ctx context.Context, repoRoot string, res *Result) {
	bin, ok := resolveBinary("staticcheck")
	if !ok {
		res.Skipped = append(res.Skipped,
			"staticcheck not found on PATH or in the Go bin dir — install: go install honnef.co/go/tools/cmd/staticcheck@latest")
		return
	}
	ctx2, cancel := context.WithTimeout(ctx, scanTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx2, bin, "-f", "json", "./...")
	cmd.Dir = repoRoot
	var out, errb bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errb
	_ = cmd.Run() // non-zero exit is expected when issues are found

	// staticcheck emits newline-delimited JSON — one diagnostic per line.
	dec := json.NewDecoder(bytes.NewReader(out.Bytes()))
	any := false
	for dec.More() {
		var e struct {
			Code     string `json:"code"`
			Severity string `json:"severity"`
			Location struct {
				File string `json:"file"`
				Line int    `json:"line"`
			} `json:"location"`
			End struct {
				Line int `json:"line"`
			} `json:"end"`
			Message string `json:"message"`
		}
		if err := dec.Decode(&e); err != nil {
			break
		}
		any = true
		if e.Severity == "ignored" || e.Location.File == "" {
			continue
		}
		end := e.End.Line
		if end < e.Location.Line {
			end = e.Location.Line
		}
		f := Finding{
			Analyzer:  "staticcheck",
			RuleID:    e.Code,
			Severity:  normalizeStaticcheckSeverity(e.Severity),
			FilePath:  relPath(repoRoot, e.Location.File),
			LineStart: e.Location.Line,
			LineEnd:   end,
			Title:     e.Message,
		}
		f.Fingerprint = fingerprint(f)
		res.Findings = append(res.Findings, f)
	}
	if !any {
		if note := firstLine(errb.String()); note != "" {
			res.Notes = append(res.Notes, "staticcheck: "+note)
		}
	}
}

// runSemgrep runs semgrep over repoRoot for any non-Go language — one
// multi-language engine (Kotlin, Java, Ruby, C#, C/C++, TS/JS, Python, …)
// emitting SARIF, so the shared ParseSARIF covers every language. Missing binary
// → graceful skip; a timeout or unparseable run → a NOTE (never a silent empty).
//
// The rule source comes from resolveSemgrepConfig(): a local rules dir when
// available (offline), else the registry. The old `--config auto` could stall
// on the registry fetch and get killed at the timeout, producing a phantom "0
// findings" (the v0.16.0 Kotlin bug) — runSARIFTool now surfaces that timeout.
func runSemgrep(ctx context.Context, repoRoot string, res *Result) {
	bin, ok := resolveBinary("semgrep")
	if !ok {
		res.Skipped = append(res.Skipped,
			"semgrep not found on PATH — install: pipx install semgrep (or pip install --user semgrep)")
		return
	}
	// "." with Dir=repoRoot keeps result paths repo-relative; --metrics=off
	// honors the local-first stance (no phone-home).
	runSARIFTool(ctx, repoRoot, "semgrep", bin,
		[]string{"--config", resolveSemgrepConfig(), "--sarif", "--quiet", "--metrics=off", "."}, res)
}

// runDetekt runs detekt — the dedicated Kotlin static analyzer (quality,
// complexity, and potential-bug detection) — over repoRoot. detekt is a JVM tool
// that writes SARIF to a FILE (--report sarif:<path>), not stdout, so it goes
// through runSARIFFileTool. Returns true if detekt was present and ran, false if
// it was missing — so Scan can be honest that the Kotlin got no dedicated
// analysis. Non-zero exit is normal when detekt finds issues.
func runDetekt(ctx context.Context, repoRoot string, res *Result) bool {
	bin, ok := resolveBinary("detekt")
	if !ok {
		res.Skipped = append(res.Skipped,
			"detekt not found on PATH — the Kotlin analyzer; install: brew install detekt "+
				"(or see https://detekt.dev). Requires Java.")
		return false
	}
	f, err := os.CreateTemp("", "codedistill-detekt-*.sarif")
	if err != nil {
		res.Notes = append(res.Notes, "detekt: could not create a temp report file: "+err.Error())
		return true // detekt is present; we just couldn't stage the run
	}
	sarifPath := f.Name()
	_ = f.Close()
	defer os.Remove(sarifPath)

	// Default ruleset (no --config → detekt's bundled defaults). --input scopes
	// it to the repo; --report writes SARIF to our temp file.
	runSARIFFileTool(ctx, repoRoot, "detekt", bin,
		[]string{"--input", repoRoot, "--report", "sarif:" + sarifPath}, sarifPath, res)
	return true
}

// runSARIFFileTool runs an analyzer that writes its SARIF to a FILE (sarifPath)
// rather than stdout — e.g. detekt (--report sarif:<file>). Same never-silent
// contract as runSARIFTool: a timeout or an unreadable/unparseable report becomes
// a NOTE, never a silent empty result.
func runSARIFFileTool(ctx context.Context, repoRoot, tool, bin string, args []string, sarifPath string, res *Result) {
	ctx2, cancel := context.WithTimeout(ctx, scanTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx2, bin, args...)
	cmd.Dir = repoRoot
	var errb bytes.Buffer
	cmd.Stderr = &errb
	_ = cmd.Run() // a non-zero exit is normal when findings exist

	if ctx2.Err() == context.DeadlineExceeded {
		res.Notes = append(res.Notes, fmt.Sprintf(
			"%s: timed out after %s — the scan was killed, no findings recorded", tool, scanTimeout))
		return
	}
	data, err := os.ReadFile(sarifPath)
	if err != nil || len(bytes.TrimSpace(data)) == 0 {
		note := firstLine(errb.String())
		if note == "" {
			note = "produced no SARIF report"
		}
		res.Notes = append(res.Notes, tool+": "+note)
		return
	}
	findings, err := ParseSARIF(data, repoRoot)
	if err != nil {
		note := firstLine(errb.String())
		if note == "" {
			note = "produced an unparseable SARIF report"
		}
		res.Notes = append(res.Notes, tool+": "+note)
		return
	}
	res.Findings = append(res.Findings, findings...)
	if len(findings) == 0 {
		if note := firstLine(errb.String()); note != "" {
			res.Notes = append(res.Notes, tool+": "+note)
		}
	}
}

// runSARIFTool runs an analyzer that writes SARIF to stdout, parses it via the
// shared ParseSARIF, and appends the findings. It is deliberately NEVER silent:
// a timeout (the registry-fetch hang that produced phantom "0 findings") or an
// unparseable run is folded into res.Notes so the findings page shows what went
// wrong instead of a falsely-clean empty result.
func runSARIFTool(ctx context.Context, repoRoot, tool, bin string, args []string, res *Result) {
	ctx2, cancel := context.WithTimeout(ctx, scanTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx2, bin, args...)
	cmd.Dir = repoRoot
	var out, errb bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errb
	_ = cmd.Run() // a non-zero exit is normal when findings exist

	if ctx2.Err() == context.DeadlineExceeded {
		res.Notes = append(res.Notes, fmt.Sprintf(
			"%s: timed out after %s — the scan was killed, no findings recorded "+
				"(a registry rule fetch may need network; point at a local rules dir to scan offline)",
			tool, scanTimeout))
		return
	}
	findings, err := ParseSARIF(out.Bytes(), repoRoot)
	if err != nil {
		note := firstLine(errb.String())
		if note == "" {
			note = "produced no parseable SARIF output"
		}
		res.Notes = append(res.Notes, tool+": "+note)
		return
	}
	res.Findings = append(res.Findings, findings...)
	// Nothing found AND the tool wrote to stderr → surface it; a partial failure
	// reads as "clean" otherwise.
	if len(findings) == 0 {
		if note := firstLine(errb.String()); note != "" {
			res.Notes = append(res.Notes, tool+": "+note)
		}
	}
}

// resolveSemgrepConfig picks semgrep's rule source. Order: an explicit override
// (CODEDISTILL_SEMGREP_CONFIG — a local rules dir or registry id, for airgapped
// installs), then a CodeDistill-managed local rules dir if present (offline),
// then the registry default ("auto"). The managed-dir fetch/refresh flow is a
// later slice; this already lets an offline user point at local rules and stops
// a missing network from silently zeroing the scan.
func resolveSemgrepConfig() string {
	if c := strings.TrimSpace(os.Getenv("CODEDISTILL_SEMGREP_CONFIG")); c != "" {
		return c
	}
	if home, err := os.UserHomeDir(); err == nil {
		dir := filepath.Join(home, ".config", "codedistill", "semgrep-rules")
		if fi, err := os.Stat(dir); err == nil && fi.IsDir() {
			return dir
		}
	}
	return "auto"
}

// fingerprint identifies a finding across re-scans: analyzer + rule + file +
// a location hash. Line drift across edits can still mint a new fingerprint —
// acceptable for v1 (content-anchored fingerprints are a later refinement).
func fingerprint(f Finding) string {
	return shortHash(f.Analyzer + "\x00" + f.RuleID + "\x00" + f.FilePath + "\x00" + strconv.Itoa(f.LineStart))
}

// shortHash is the 16-hex-char SHA-256 prefix used for finding fingerprints.
func shortHash(s string) string {
	h := sha256.Sum256([]byte(s))
	return hex.EncodeToString(h[:])[:16]
}

func normalizeStaticcheckSeverity(s string) string {
	switch strings.ToLower(s) {
	case "error":
		return domain.SeverityMedium
	case "warning":
		return domain.SeverityLow
	default:
		return domain.SeverityInfo
	}
}

// relPath makes an analyzer's (usually absolute) path repo-relative. Falls back
// to the original string if it isn't under repoRoot.
func relPath(repoRoot, p string) string {
	if p == "" {
		return ""
	}
	if rel, err := filepath.Rel(repoRoot, p); err == nil && !strings.HasPrefix(rel, "..") {
		return rel
	}
	return p
}

// countSourceFiles is a cheap, consistent "files scanned" number for the given
// extensions — walking once, skipping the usual non-source dirs. Extensions
// include the dot (e.g. ".go", ".kt").
func countSourceFiles(repoRoot string, exts ...string) int {
	n := 0
	_ = filepath.WalkDir(repoRoot, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			switch d.Name() {
			case ".git", "vendor", "node_modules", "testdata", "build", ".gradle":
				return filepath.SkipDir
			}
			return nil
		}
		for _, ext := range exts {
			if strings.HasSuffix(d.Name(), ext) {
				n++
				break
			}
		}
		return nil
	})
	return n
}

// resolveBinary finds an analyzer executable. It checks PATH first, then the
// well-known install locations — because a server launched from a GUI or
// service manager often has a minimal PATH that excludes them: the Go bin dir
// where `go install` lands gosec/staticcheck, and the pip/pipx/brew dirs where
// semgrep lands. LookPath alone would miss both.
func resolveBinary(name string) (string, bool) {
	if p, err := exec.LookPath(name); err == nil {
		return p, true
	}
	var dirs []string
	if gobin := os.Getenv("GOBIN"); gobin != "" {
		dirs = append(dirs, gobin)
	}
	if gopath := os.Getenv("GOPATH"); gopath != "" {
		for _, g := range filepath.SplitList(gopath) {
			dirs = append(dirs, filepath.Join(g, "bin"))
		}
	}
	if home, err := os.UserHomeDir(); err == nil {
		// ~/go/bin: go install. ~/.local/bin: pip --user / pipx (semgrep).
		dirs = append(dirs, filepath.Join(home, "go", "bin"), filepath.Join(home, ".local", "bin"))
	}
	// Common system/brew locations for pip/pipx/Homebrew-installed tools.
	dirs = append(dirs, "/usr/local/bin", "/opt/homebrew/bin")
	for _, d := range dirs {
		cand := filepath.Join(d, name)
		if fi, err := os.Stat(cand); err == nil && !fi.IsDir() {
			return cand, true
		}
	}
	return "", false
}

func firstLine(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return strings.TrimSpace(s[:i])
	}
	return s
}

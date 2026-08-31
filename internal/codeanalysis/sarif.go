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

// SARIF (Static Analysis Results Interchange Format, OASIS 2.1.0) is the lingua
// franca every serious analyzer already emits — gosec, semgrep, detekt, ESLint,
// CodeQL, Snyk, SonarQube. Parsing it ONCE lets the built-in scanner and the
// (future) external-ingest surfaces share a single code path: SARIF in →
// normalized Finding. This replaces a per-tool JSON parser per analyzer.

import (
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"codedistill/internal/domain"
)

// --- minimal SARIF 2.1.0 shape (only the fields we consume) ---

type sarifLog struct {
	Runs []sarifRun `json:"runs"`
}

type sarifRun struct {
	Tool struct {
		Driver struct {
			Name  string      `json:"name"`
			Rules []sarifRule `json:"rules"`
		} `json:"driver"`
	} `json:"tool"`
	Results []sarifResult `json:"results"`
}

type sarifRule struct {
	ID               string `json:"id"`
	Name             string `json:"name"`
	ShortDescription struct {
		Text string `json:"text"`
	} `json:"shortDescription"`
	DefaultConfiguration struct {
		Level string `json:"level"`
	} `json:"defaultConfiguration"`
	Properties sarifProps `json:"properties"`
}

type sarifProps struct {
	Tags             []string `json:"tags"`
	SecuritySeverity string   `json:"security-severity"` // SARIF carries this as a string, e.g. "7.5"
}

type sarifResult struct {
	RuleID    string `json:"ruleId"`
	RuleIndex *int   `json:"ruleIndex"`
	Level     string `json:"level"`
	Message   struct {
		Text string `json:"text"`
	} `json:"message"`
	Locations           []sarifLocation   `json:"locations"`
	PartialFingerprints map[string]string `json:"partialFingerprints"`
	Properties          sarifProps        `json:"properties"`
}

type sarifLocation struct {
	PhysicalLocation struct {
		ArtifactLocation struct {
			URI string `json:"uri"`
		} `json:"artifactLocation"`
		Region struct {
			StartLine int `json:"startLine"`
			EndLine   int `json:"endLine"`
			Snippet   struct {
				Text string `json:"text"`
			} `json:"snippet"`
		} `json:"region"`
	} `json:"physicalLocation"`
}

// ParseSARIF turns a SARIF document into normalized findings. repoRoot makes
// artifact URIs repo-relative (SARIF emits a mix of relative paths and file://
// URIs). It returns an error only on malformed JSON — an empty `runs`/`results`
// is a valid "nothing found" document and yields zero findings, no error.
func ParseSARIF(data []byte, repoRoot string) ([]Finding, error) {
	var log sarifLog
	if err := json.Unmarshal(data, &log); err != nil {
		return nil, fmt.Errorf("parse sarif: %w", err)
	}
	var out []Finding
	for _, run := range log.Runs {
		tool := run.Tool.Driver.Name
		// Index rules by id (for ruleId lookups) and keep the slice (for ruleIndex).
		byID := make(map[string]sarifRule, len(run.Tool.Driver.Rules))
		for _, r := range run.Tool.Driver.Rules {
			byID[r.ID] = r
		}
		rules := run.Tool.Driver.Rules

		for _, r := range run.Results {
			ruleID := r.RuleID
			var rule sarifRule
			if ruleID != "" {
				rule = byID[ruleID]
			} else if r.RuleIndex != nil && *r.RuleIndex >= 0 && *r.RuleIndex < len(rules) {
				rule = rules[*r.RuleIndex]
				ruleID = rule.ID
			}

			level := r.Level
			if level == "" {
				level = rule.DefaultConfiguration.Level
			}
			secSev := parseSecuritySeverity(r.Properties.SecuritySeverity, rule.Properties.SecuritySeverity)
			tags := append(append([]string{}, r.Properties.Tags...), rule.Properties.Tags...)

			loc := firstLocation(r.Locations)
			title := strings.TrimSpace(r.Message.Text)
			if title == "" {
				title = strings.TrimSpace(rule.ShortDescription.Text)
			}

			f := Finding{
				Analyzer:         tool,
				RuleID:           ruleID,
				Severity:         severityFor(level, secSev),
				Category:         categoryFor(tags, secSev),
				SecuritySeverity: secSev,
				FilePath:         relPath(repoRoot, stripFileScheme(loc.PhysicalLocation.ArtifactLocation.URI)),
				LineStart:        loc.PhysicalLocation.Region.StartLine,
				LineEnd:          maxInt(loc.PhysicalLocation.Region.EndLine, loc.PhysicalLocation.Region.StartLine),
				Title:            title,
				Detail:           strings.TrimSpace(loc.PhysicalLocation.Region.Snippet.Text),
			}
			f.Fingerprint = sarifFingerprint(r.PartialFingerprints, f)
			out = append(out, f)
		}
	}
	return out, nil
}

// severityFor maps to our coarse bucket. A security-severity score (CVSS bands)
// wins when present — it's the more meaningful signal for security findings;
// otherwise we fall back to the SARIF level.
func severityFor(level string, secSev float64) string {
	if secSev > 0 {
		switch {
		case secSev >= 7.0:
			return domain.SeverityHigh
		case secSev >= 4.0:
			return domain.SeverityMedium
		default:
			return domain.SeverityLow
		}
	}
	switch strings.ToLower(level) {
	case "error":
		return domain.SeverityHigh
	case "warning":
		return domain.SeverityMedium
	case "note":
		return domain.SeverityLow
	default:
		return domain.SeverityInfo
	}
}

// categoryFor buckets a finding for the normalized taxonomy. A security-severity
// score or a security/cwe/owasp tag means security; otherwise we read the tags,
// defaulting to quality.
func categoryFor(tags []string, secSev float64) string {
	if secSev > 0 {
		return "security"
	}
	for _, t := range tags {
		switch lt := strings.ToLower(t); {
		case strings.Contains(lt, "security"), strings.Contains(lt, "cwe"), strings.Contains(lt, "owasp"), strings.Contains(lt, "vuln"):
			return "security"
		case strings.Contains(lt, "performance"), lt == "perf":
			return "performance"
		case strings.Contains(lt, "correctness"), lt == "bug":
			return "correctness"
		}
	}
	return "quality"
}

func parseSecuritySeverity(result, rule string) float64 {
	s := result
	if s == "" {
		s = rule
	}
	if s == "" {
		return 0
	}
	v, err := strconv.ParseFloat(strings.TrimSpace(s), 64)
	if err != nil || v < 0 {
		return 0
	}
	return v
}

// sarifFingerprint prefers the tool's own stable partialFingerprints (the whole
// point of finding identity surviving line drift); it falls back to our
// analyzer+rule+file+line hash when the tool doesn't supply one.
func sarifFingerprint(pf map[string]string, f Finding) string {
	if len(pf) > 0 {
		keys := make([]string, 0, len(pf))
		for k := range pf {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		var b strings.Builder
		b.WriteString(f.Analyzer)
		for _, k := range keys {
			b.WriteByte(0)
			b.WriteString(k)
			b.WriteByte('=')
			b.WriteString(pf[k])
		}
		return shortHash(b.String())
	}
	return fingerprint(f)
}

func firstLocation(locs []sarifLocation) sarifLocation {
	if len(locs) > 0 {
		return locs[0]
	}
	return sarifLocation{}
}

// stripFileScheme normalizes a SARIF artifact URI (which may be a bare relative
// path or a file:// URI) to a plain path that relPath can make repo-relative.
func stripFileScheme(uri string) string {
	return strings.TrimPrefix(uri, "file://")
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

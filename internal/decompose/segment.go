// =============================================================================
//
//	Copyright (c) 2026 Nyx Software, Inc.  All rights reserved.
//
//	CodeDistill
//
//	Property of Nyx Software, Inc., provided under a dual license: the GNU Affero General
//	Public License v3.0 (see the LICENSE file) and, separately, a commercial
//	license available from Nyx Software, Inc. Use outside the terms of one of those
//	licenses is prohibited.
//
//	SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-Nyx-Commercial
//
// =============================================================================
// Layer 1 — the document, as given.
//
// The document is never modified. This cuts it into sentences and records
// exactly which lines each came from. Every citation downstream traces to a
// span computed HERE, before any model has seen anything.
//
// Non-prose blocks — fenced code, tables — are excluded and counted. That is a
// known gap rather than a solved problem: a requirements table is often the
// richest content in a real specification, and cross-referencing it against the
// prose is the next thing this layer needs.
package decompose

import (
	"regexp"
	"strings"
)

var (
	fence       = regexp.MustCompile("^\\s*```")
	tableRow    = regexp.MustCompile(`^\s*\|`)
	heading     = regexp.MustCompile(`^\s*#{1,6}\s+(.*)`)
	listMarker  = regexp.MustCompile(`^\s*([-*+]|\d+\.)\s+`)
	sentenceEnd = regexp.MustCompile(`([.!?])(\s+|$)`)
	// Abbreviations that end in a period without ending a sentence. Kept
	// deliberately short: this is a known-imperfect segmenter and the honest
	// move is to bound the problem, not to pretend the list is complete.
	abbrev = map[string]bool{
		"e.g": true, "i.e": true, "etc": true, "vs": true, "cf": true,
		"Fig": true, "No": true, "Dr": true, "Mr": true, "Ms": true,
	}
)

// Segment cuts a markdown document into sentences with line provenance.
func Segment(lines []string) *Document {
	doc := &Document{Lines: len(lines)}
	section := ""
	para := 0
	inFence := false
	fenceStart := 0

	// A paragraph accumulates across lines; sentences are cut within it, and
	// each sentence remembers the first and last line it touched.
	// Each contribution records which line it came from and where it landed in
	// the accumulated text. Without this every sentence in a paragraph inherits
	// the WHOLE paragraph's span — a ten-line paragraph would cite nine lines
	// the sentence had nothing to do with, and precise provenance is the entire
	// point of doing segmentation in code.
	type pending struct {
		text  strings.Builder
		parts []contribution
	}
	var buf pending
	flush := func() {
		raw := buf.text.String()
		if strings.TrimSpace(raw) == "" {
			buf = pending{}
			return
		}
		para++
		for _, s := range splitSentences(raw) {
			l1, l2 := linesFor(buf.parts, s.from, s.to)
			doc.Sentences = append(doc.Sentences, Sentence{
				Idx:     len(doc.Sentences),
				Text:    s.text,
				Line1:   l1,
				Line2:   l2,
				Section: section,
				Para:    para,
			})
		}
		buf = pending{}
	}

	for i, ln := range lines {
		n := i + 1

		if fence.MatchString(ln) {
			if inFence {
				doc.Excluded = append(doc.Excluded, Excluded{"code", fenceStart, n})
				inFence = false
			} else {
				flush()
				inFence, fenceStart = true, n
			}
			continue
		}
		if inFence {
			continue
		}

		if tableRow.MatchString(ln) {
			flush()
			// Merge with the previous table region if adjacent, so a 12-row
			// table is one exclusion rather than twelve.
			if k := len(doc.Excluded) - 1; k >= 0 && doc.Excluded[k].Kind == "table" && doc.Excluded[k].Line2 >= n-1 {
				doc.Excluded[k].Line2 = n
			} else {
				doc.Excluded = append(doc.Excluded, Excluded{"table", n, n})
			}
			continue
		}

		if m := heading.FindStringSubmatch(ln); m != nil {
			flush()
			section = strings.TrimSpace(stripInline(m[1]))
			doc.Headings++
			continue
		}

		if strings.TrimSpace(ln) == "" {
			flush()
			continue
		}

		// A list marker starts a new prose unit; without this, six bullets
		// glue into one run-on "sentence".
		if listMarker.MatchString(ln) {
			flush()
			ln = listMarker.ReplaceAllString(ln, "")
		}
		start := buf.text.Len()
		buf.text.WriteString(strings.TrimSpace(ln))
		buf.parts = append(buf.parts, contribution{line: n, from: start, to: buf.text.Len()})
		buf.text.WriteString(" ")
	}
	flush()
	if inFence {
		doc.Excluded = append(doc.Excluded, Excluded{"code", fenceStart, len(lines)})
	}
	return doc
}

// splitSentences cuts on terminal punctuation, holding back on abbreviations.
// Deliberately simple; its errors show up as over-long or truncated sentences
// in the layer-1 report rather than as silent corruption downstream.
func splitSentences(p string) []span {
	var out []span
	start := 0
	for _, loc := range sentenceEnd.FindAllStringSubmatchIndex(p, -1) {
		end := loc[3] // just past the punctuation
		head := strings.TrimSpace(p[start:end])
		if word := lastWord(head); abbrev[strings.TrimRight(word, ".")] {
			continue
		}
		if len(head) > 1 {
			out = append(out, span{head, start, end})
		}
		start = loc[1]
	}
	if tail := strings.TrimSpace(p[start:]); len(tail) > 1 {
		out = append(out, span{tail, start, len(p)})
	}
	return out
}

// span is a sentence plus where it sat in the paragraph it was cut from.
type span struct {
	text     string
	from, to int
}

// contribution records which source line a stretch of paragraph text came
// from, so a sentence can be mapped back to the lines it actually occupies.
type contribution struct {
	line     int
	from, to int // byte range within the paragraph text
}

// linesFor maps a byte range in the paragraph back to the source lines it
// overlaps. This is the function the citation guarantee rests on.
func linesFor(parts []contribution, from, to int) (int, int) {
	lo, hi := 0, 0
	for _, c := range parts {
		if c.to <= from || c.from >= to {
			continue
		}
		if lo == 0 || c.line < lo {
			lo = c.line
		}
		if c.line > hi {
			hi = c.line
		}
	}
	return lo, hi
}

func lastWord(s string) string {
	f := strings.Fields(s)
	if len(f) == 0 {
		return ""
	}
	return f[len(f)-1]
}

var inlineMD = regexp.MustCompile("[*_`]+")

func stripInline(s string) string { return inlineMD.ReplaceAllString(s, "") }

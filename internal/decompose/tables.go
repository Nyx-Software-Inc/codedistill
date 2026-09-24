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
// Tables — the requirements a real specification already wrote down.
//
// Layer 1 excludes tables from prose segmentation because a table row has no
// contiguous sentence. That is true and it is not a reason to ignore them: on
// a real product description, 30 use cases sat in a four-column table with
// priorities attached while the prose pipeline independently re-derived 23 of
// them. Excluding the table meant excluding the best content in the document.
//
// A row is not defective prose. It is a RECORD, and a record is a better unit
// than a sentence because it already has fields. So a table with a header that
// maps onto item fields is read directly, no inference required, and its
// provenance is a cell reference — harder to fabricate than a line span.
//
// The point of reading both is not duplication, it is RECONCILIATION. Where the
// prose and the table agree, the proposal is corroborated. Where the table has
// something the prose missed, that is the honest answer to "what did it miss?",
// which coverage alone cannot give. Where the prose has something the table
// lacks, the table is incomplete or the prose is noise — and a human decides
// which.
//
// Not every table is a record table. A comparison matrix or a layout table has
// no rows worth importing, and turning one into thirty items is worse than
// ignoring it. Detection is deliberately conservative: a header must name at
// least one field we recognise, and a table that does not qualify stays
// excluded and is reported as such.
package decompose

import (
	"regexp"
	"strings"
)

// Table is a pipe-delimited table recovered from the document.
type Table struct {
	Header []string   `json:"header"`
	Rows   [][]string `json:"rows"`
	Line1  int        `json:"line1"`
	Line2  int        `json:"line2"`
	// RowLines[i] is the source line of Rows[i], so an imported row cites the
	// row it came from rather than the whole table.
	RowLines []int `json:"row_lines"`
}

// TableFields is the mapping from a record table's columns onto item fields.
// A column we do not recognise is left unmapped rather than guessed at.
type TableFields struct {
	Subject  int // required: the column carrying the requirement itself
	Body     int // -1 when absent
	Priority int // -1 when absent
	Ref      int // -1 when absent: an external id like UC-01
}

// IsRecordTable reports whether a header looks like requirement records.
func (t *Table) Fields() (TableFields, bool) {
	f := TableFields{Subject: -1, Body: -1, Priority: -1, Ref: -1}
	for i, h := range t.Header {
		// Ref is tested FIRST. A real header reads "Use Case # | Use Case
		// Statement", and matching the subject pattern by prefix would let the
		// id column swallow the one that carries the requirement.
		switch norm := strings.ToLower(strings.TrimSpace(h)); {
		case f.Ref < 0 && isRefHeader(norm):
			f.Ref = i
		case f.Subject < 0 && matchesAny(norm, subjectHeaders):
			f.Subject = i
		case f.Body < 0 && matchesAny(norm, bodyHeaders):
			f.Body = i
		case f.Priority < 0 && matchesAny(norm, priorityHeaders):
			f.Priority = i
		}
	}
	// Without a subject column there is nothing to propose. This is the guard
	// that keeps a comparison matrix from becoming thirty work items.
	return f, f.Subject >= 0
}

var (
	subjectHeaders  = []string{"use case statement", "requirement", "user story", "story", "statement", "need", "feature", "capability", "title", "summary", "use case"}
	bodyHeaders     = []string{"description", "detail", "details", "notes", "acceptance", "rationale"}
	priorityHeaders = []string{"priority", "severity", "importance", "moscow"}
)

// isRefHeader recognises an identifier column. Deliberately narrow: an exact
// name, or anything ending in "#" or "id" — "Use Case #", "Req ID", "Story
// Number". Anything looser starts eating the subject column.
func isRefHeader(h string) bool {
	switch h {
	case "id", "ref", "#", "key", "no", "no.", "number", "item":
		return true
	}
	return strings.HasSuffix(h, " #") || strings.HasSuffix(h, "#") ||
		strings.HasSuffix(h, " id") || strings.HasSuffix(h, " number")
}

func matchesAny(h string, opts []string) bool {
	for _, o := range opts {
		if h == o || strings.HasPrefix(h, o+" ") || strings.HasSuffix(h, " "+o) {
			return true
		}
	}
	return false
}

var tableLine = regexp.MustCompile(`^\s*\|`)
var dividerRow = regexp.MustCompile(`^[\s|:-]+$`)

// ExtractTables recovers pipe tables from the raw document. It runs over the
// same lines layer 1 excluded, so nothing is read twice as both prose and
// record.
func ExtractTables(lines []string) []Table {
	var out []Table
	i := 0
	for i < len(lines) {
		if !tableLine.MatchString(lines[i]) {
			i++
			continue
		}
		start := i
		var rows [][]string
		var rowLines []int
		for i < len(lines) && tableLine.MatchString(lines[i]) {
			if !dividerRow.MatchString(lines[i]) {
				rows = append(rows, splitRow(lines[i]))
				rowLines = append(rowLines, i+1)
			}
			i++
		}
		// A header plus at least one row. A single-row "table" is decoration.
		if len(rows) >= 2 {
			out = append(out, Table{
				Header: rows[0], Rows: rows[1:],
				RowLines: rowLines[1:], Line1: start + 1, Line2: i,
			})
		}
	}
	return out
}

func splitRow(line string) []string {
	s := strings.TrimSpace(line)
	s = strings.TrimPrefix(s, "|")
	s = strings.TrimSuffix(s, "|")
	parts := strings.Split(s, "|")
	for i := range parts {
		parts[i] = strings.TrimSpace(parts[i])
	}
	return parts
}

// RecordProposals reads a record table directly into proposals. No model is
// involved: the columns already say what each row is, and inference could only
// make it worse.
//
// Kind is not guessed per row. A table of "Use Case Statement" rows is a table
// of use cases, so the caller passes the kind implied by the subject header.
func (t *Table) RecordProposals(kind string) []Proposal {
	f, ok := t.Fields()
	if !ok {
		return nil
	}
	var out []Proposal
	for i, row := range t.Rows {
		if f.Subject >= len(row) {
			continue
		}
		subject := strings.TrimSpace(row[f.Subject])
		if subject == "" {
			continue
		}
		line := t.Line1
		if i < len(t.RowLines) {
			line = t.RowLines[i]
		}
		p := Proposal{
			Kind: kind, Subject: subject,
			Lines: [][2]int{{line, line}},
			// FromTable marks this as read rather than inferred, which is what
			// lets the review surface rank it ahead of a prose guess.
			FromTable: true,
		}
		if f.Ref >= 0 && f.Ref < len(row) {
			p.ExternalRef = strings.TrimSpace(row[f.Ref])
		}
		if f.Priority >= 0 && f.Priority < len(row) {
			p.Priority = strings.TrimSpace(row[f.Priority])
		}
		if f.Body >= 0 && f.Body < len(row) {
			p.Body = strings.TrimSpace(row[f.Body])
		}
		if p.ExternalRef != "" {
			p.NodeID = "t:" + p.ExternalRef
		} else {
			p.NodeID = "t:" + subject
		}
		out = append(out, p)
	}
	return out
}

// KindForHeader guesses the item kind a record table holds from its subject
// column. Only the unambiguous cases; everything else is a todo, which is the
// least presumptuous default for "work someone wrote down".
func KindForHeader(header string) string {
	h := strings.ToLower(header)
	switch {
	case strings.Contains(h, "use case"), strings.Contains(h, "user story"), strings.Contains(h, "story"):
		return "use_case"
	case strings.Contains(h, "bug"), strings.Contains(h, "defect"):
		return "bug"
	}
	return "todo"
}

// Reconcile reads the document's two accounts of itself against each other.
//
// Matching is by content-word overlap, NOT by embedding similarity. Embeddings
// were tried and failed on exactly this problem: on a product description where
// every sentence is about notes and meetings, cosine sat in a 0.6-0.78 band and
// stopped discriminating — "Delete meeting notes" came back as the best match
// for five unrelated use cases. Overlap is cruder and its mistakes are legible,
// which matters more here than precision, because a human reviews every bucket
// anyway and a wrong pairing must be obvious at a glance.
//
// A table row that no prose proposal matches is the honest answer to "what did
// the extraction miss?" — something coverage alone can never give.
func Reconcile(fromTable, fromProse []Proposal, threshold float64) Reconciliation {
	if threshold <= 0 {
		threshold = 0.5
	}
	var r Reconciliation
	claimed := make([]bool, len(fromProse))

	for _, t := range fromTable {
		tw := contentWords(t.Subject + " " + t.Body)
		best, bestScore := -1, 0.0
		for i, p := range fromProse {
			if claimed[i] {
				continue
			}
			if s := overlap(tw, contentWords(p.Subject)); s > bestScore {
				best, bestScore = i, s
			}
		}
		if best >= 0 && bestScore >= threshold {
			claimed[best] = true
			t.Corroborated = true
			r.Corroborated = append(r.Corroborated, t)
			continue
		}
		r.TableOnly = append(r.TableOnly, t)
	}
	for i, p := range fromProse {
		if !claimed[i] {
			r.ProseOnly = append(r.ProseOnly, p)
		}
	}
	return r
}

// overlap is the share of the SHORTER phrase's words present in the longer one.
// Jaccard would punish a table row whose Description column runs to forty words
// for matching a six-word prose subject, which is the common case here rather
// than an edge one.
func overlap(a, b map[string]bool) float64 {
	if len(a) == 0 || len(b) == 0 {
		return 0
	}
	short, long := a, b
	if len(b) < len(a) {
		short, long = b, a
	}
	hits := 0
	for w := range short {
		if long[w] {
			hits++
		}
	}
	return float64(hits) / float64(len(short))
}

// stopWords are carried by every requirement in every document ("as a user, I
// want to..."), so leaving them in would make any two rows look alike.
var stopWords = map[string]bool{
	"the": true, "and": true, "for": true, "that": true, "with": true, "this": true,
	"from": true, "want": true, "user": true, "should": true, "would": true, "able": true,
	"have": true, "when": true, "into": true, "they": true, "their": true, "them": true,
	"system": true, "must": true, "will": true, "shall": true, "can": true, "like": true,
}

func contentWords(s string) map[string]bool {
	out := map[string]bool{}
	for _, w := range strings.Fields(strings.ToLower(s)) {
		w = strings.Trim(w, ".,:;()\"'`*_-—–")
		if len(w) > 3 && !stopWords[w] {
			out[stem(w)] = true
		}
	}
	return out
}

// stem folds the plural/gerund variants that make "recording" and "recordings"
// and "record" look like three different requirements.
func stem(w string) string {
	for _, suf := range []string{"ings", "ing", "ies", "es", "s"} {
		if len(w) > len(suf)+3 && strings.HasSuffix(w, suf) {
			return strings.TrimSuffix(w, suf)
		}
	}
	return w
}

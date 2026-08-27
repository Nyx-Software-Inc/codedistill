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

package datamodel

import "strings"

// SQL-aware text scanners. All of them respect string literals ('…'), quoted
// identifiers ("…", `…`, [ … ]) and parenthesis nesting, so a ';' or ',' inside
// a string or a nested parens list never splits a statement or a column list.

// stripComments removes -- line comments and /* */ block comments, leaving
// string/identifier literals untouched.
func stripComments(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	var quote byte // 0 = outside any literal; else the closing char
	for i := 0; i < len(s); i++ {
		ch := s[i]
		if quote != 0 {
			b.WriteByte(ch)
			if ch == quote {
				quote = 0
			}
			continue
		}
		if ch == '-' && i+1 < len(s) && s[i+1] == '-' {
			for i < len(s) && s[i] != '\n' {
				i++
			}
			if i < len(s) {
				b.WriteByte('\n')
			}
			continue
		}
		if ch == '/' && i+1 < len(s) && s[i+1] == '*' {
			i += 2
			for i+1 < len(s) && !(s[i] == '*' && s[i+1] == '/') {
				i++
			}
			i++ // land on '/', loop's i++ steps past it
			continue
		}
		switch ch {
		case '\'', '"', '`':
			quote = ch
		case '[':
			quote = ']'
		}
		b.WriteByte(ch)
	}
	return b.String()
}

// splitOutsideGroups splits s on sep, but only where sep is at paren-depth 0 and
// outside any literal. The final segment is always emitted (mirrors strings.Split).
func splitOutsideGroups(s string, sep byte) []string {
	var out []string
	var b strings.Builder
	depth := 0
	var quote byte
	for i := 0; i < len(s); i++ {
		ch := s[i]
		if quote != 0 {
			b.WriteByte(ch)
			if ch == quote {
				quote = 0
			}
			continue
		}
		switch ch {
		case '\'', '"', '`':
			quote = ch
			b.WriteByte(ch)
			continue
		case '[':
			quote = ']'
			b.WriteByte(ch)
			continue
		case '(':
			depth++
		case ')':
			if depth > 0 {
				depth--
			}
		}
		if ch == sep && depth == 0 {
			out = append(out, b.String())
			b.Reset()
			continue
		}
		b.WriteByte(ch)
	}
	out = append(out, b.String())
	return out
}

// fieldsOutsideGroups splits s on whitespace at paren-depth 0 outside literals,
// keeping parenthesized/quoted groups as part of a single token (so
// "VARCHAR(255)" and "'a b'" each stay whole).
func fieldsOutsideGroups(s string) []string {
	var out []string
	var b strings.Builder
	depth := 0
	var quote byte
	flush := func() {
		if b.Len() > 0 {
			out = append(out, b.String())
			b.Reset()
		}
	}
	for i := 0; i < len(s); i++ {
		ch := s[i]
		if quote != 0 {
			b.WriteByte(ch)
			if ch == quote {
				quote = 0
			}
			continue
		}
		switch ch {
		case '\'', '"', '`':
			quote = ch
			b.WriteByte(ch)
			continue
		case '[':
			quote = ']'
			b.WriteByte(ch)
			continue
		case '(':
			depth++
			b.WriteByte(ch)
			continue
		case ')':
			if depth > 0 {
				depth--
			}
			b.WriteByte(ch)
			continue
		}
		if depth == 0 && (ch == ' ' || ch == '\t' || ch == '\n' || ch == '\r') {
			flush()
			continue
		}
		b.WriteByte(ch)
	}
	flush()
	return out
}

// parenBody returns the text inside the first balanced (…) group in s.
func parenBody(s string) (string, bool) {
	start := strings.IndexByte(s, '(')
	if start < 0 {
		return "", false
	}
	depth := 0
	var quote byte
	for i := start; i < len(s); i++ {
		ch := s[i]
		if quote != 0 {
			if ch == quote {
				quote = 0
			}
			continue
		}
		switch ch {
		case '\'', '"', '`':
			quote = ch
		case '[':
			quote = ']'
		case '(':
			depth++
		case ')':
			depth--
			if depth == 0 {
				return s[start+1 : i], true
			}
		}
	}
	return "", false
}

// unquoteIdent strips one layer of identifier quoting ("…", `…`, [ … ]) and, for
// an unquoted dotted name (schema.table), keeps the last segment.
func unquoteIdent(s string) string {
	s = strings.TrimSpace(s)
	if len(s) >= 2 {
		switch s[0] {
		case '"', '`', '\'':
			if s[len(s)-1] == s[0] {
				return s[1 : len(s)-1]
			}
		case '[':
			if s[len(s)-1] == ']' {
				return s[1 : len(s)-1]
			}
		}
	}
	if i := strings.LastIndexByte(s, '.'); i >= 0 && i < len(s)-1 {
		return unquoteIdent(s[i+1:])
	}
	return s
}

// firstWord returns the first whitespace/group-delimited token of s.
func firstWord(s string) string {
	f := fieldsOutsideGroups(s)
	if len(f) == 0 {
		return ""
	}
	return f[0]
}

// trimFoldPrefix strips a leading keyword phrase (case-insensitive, flexible
// inner whitespace) from s, enforcing a word boundary after it. Returns the
// remainder and whether the phrase matched.
func trimFoldPrefix(s, prefix string) (string, bool) {
	s = strings.TrimSpace(s)
	for _, w := range strings.Fields(prefix) {
		s = strings.TrimSpace(s)
		if len(s) < len(w) || !strings.EqualFold(s[:len(w)], w) {
			return "", false
		}
		rest := s[len(w):]
		if rest != "" {
			switch rest[0] {
			case ' ', '\t', '\n', '\r', '(':
			default:
				return "", false
			}
		}
		s = rest
	}
	return strings.TrimSpace(s), true
}

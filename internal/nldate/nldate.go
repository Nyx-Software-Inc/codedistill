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

// Package nldate extracts a natural-language DUE DATE from free text
// (item-editing redesign, slice 3 — capture-time extraction). It's deterministic
// and conservative: relative terms (today/tomorrow/in N days/eow/…) self-trigger,
// but a weekday or an explicit date must follow a deadline word (by/due/before/
// deadline/on) so we don't mistake "we met on Monday" for a deadline. A due date
// is a deadline, so results normalize to end-of-day (23:59:59) in ref's location.
//
// Not a full temporal grammar — it targets the phrasings real task captures use.
package nldate

import (
	"regexp"
	"strconv"
	"strings"
	"time"
)

var weekdays = map[string]time.Weekday{
	"sunday": time.Sunday, "sun": time.Sunday,
	"monday": time.Monday, "mon": time.Monday,
	"tuesday": time.Tuesday, "tue": time.Tuesday, "tues": time.Tuesday,
	"wednesday": time.Wednesday, "wed": time.Wednesday, "weds": time.Wednesday,
	"thursday": time.Thursday, "thu": time.Thursday, "thur": time.Thursday, "thurs": time.Thursday,
	"friday": time.Friday, "fri": time.Friday,
	"saturday": time.Saturday, "sat": time.Saturday,
}

var months = map[string]time.Month{
	"january": 1, "jan": 1, "february": 2, "feb": 2, "march": 3, "mar": 3,
	"april": 4, "apr": 4, "may": 5, "june": 6, "jun": 6, "july": 7, "jul": 7,
	"august": 8, "aug": 8, "september": 9, "sep": 9, "sept": 9, "october": 10, "oct": 10,
	"november": 11, "nov": 11, "december": 12, "dec": 12,
}

// trigger is the shared deadline-word prefix required before a weekday/date.
// Deliberately excludes "on" — too common ("we met on Monday") to be safe.
const trigger = `(?:by|due(?:\s+by|\s+date)?|before|deadline|no later than)\s+`

var (
	reToday    = regexp.MustCompile(`\b(today|tonight|eod)\b`)
	reTomorrow = regexp.MustCompile(`\btomorrow\b`)
	reNextWeek = regexp.MustCompile(`\bnext\s+week\b`)
	reInN      = regexp.MustCompile(`\bin\s+(\d{1,3})\s+(day|days|week|weeks)\b`)
	reEOW      = regexp.MustCompile(`\b(?:end\s+of\s+(?:the\s+)?week|eow)\b`)
	reEOM      = regexp.MustCompile(`\b(?:end\s+of\s+(?:the\s+)?month|eom)\b`)
	reISO      = regexp.MustCompile(`\b(\d{4})-(\d{2})-(\d{2})\b`)
	reWeekday  = regexp.MustCompile(trigger + `(next\s+)?(` + weekdayAlt() + `)\b`)
	reSlash    = regexp.MustCompile(trigger + `(\d{1,2})/(\d{1,2})(?:/(\d{2,4}))?\b`)
	reMonDay   = regexp.MustCompile(trigger + `(` + monthAlt() + `)\.?\s+(\d{1,2})(?:st|nd|rd|th)?\b`)
	reDayMon   = regexp.MustCompile(trigger + `(\d{1,2})(?:st|nd|rd|th)?\s+(?:of\s+)?(` + monthAlt() + `)\b`)
)

func weekdayAlt() string {
	// Longest-first so "tues" matches before "tue" etc.
	return `sunday|saturday|thursday|wednesday|tuesday|monday|friday|thurs|tues|weds|sun|mon|tue|wed|thu|thur|fri|sat`
}
func monthAlt() string {
	return `january|february|september|november|december|october|august|march|april|june|july|jan|feb|mar|apr|may|jun|jul|aug|sep|sept|oct|nov|dec`
}

// Parse returns the earliest due date expressed in text, anchored to ref, and
// ok=false when none is found.
func Parse(text string, ref time.Time) (time.Time, bool) {
	lower := strings.ToLower(text)
	bestPos := -1
	var best time.Time
	consider := func(loc []int, when time.Time, okDate bool) {
		if loc == nil || !okDate {
			return
		}
		if bestPos == -1 || loc[0] < bestPos {
			bestPos = loc[0]
			best = endOfDay(when)
		}
	}

	if m := reToday.FindStringIndex(lower); m != nil {
		consider(m, ref, true)
	}
	if m := reTomorrow.FindStringIndex(lower); m != nil {
		consider(m, ref.AddDate(0, 0, 1), true)
	}
	if m := reNextWeek.FindStringIndex(lower); m != nil {
		consider(m, ref.AddDate(0, 0, 7), true)
	}
	if m := reInN.FindStringSubmatchIndex(lower); m != nil {
		n, _ := strconv.Atoi(lower[m[2]:m[3]])
		unit := lower[m[4]:m[5]]
		days := n
		if strings.HasPrefix(unit, "week") {
			days = n * 7
		}
		consider(m[:2], ref.AddDate(0, 0, days), true)
	}
	if m := reEOW.FindStringIndex(lower); m != nil {
		consider(m, onOrAfterWeekday(ref, time.Friday), true)
	}
	if m := reEOM.FindStringIndex(lower); m != nil {
		consider(m, endOfMonth(ref), true)
	}
	if m := reISO.FindStringSubmatchIndex(lower); m != nil {
		if d, ok := isoDate(lower, m, ref); ok {
			consider(m[:2], d, true)
		}
	}
	if m := reWeekday.FindStringSubmatchIndex(lower); m != nil {
		next := m[2] != -1
		wd := weekdays[lower[m[4]:m[5]]]
		consider(m[:2], weekdayDate(ref, wd, next), true)
	}
	if m := reSlash.FindStringSubmatchIndex(lower); m != nil {
		if d, ok := slashDate(lower, m, ref); ok {
			consider(m[:2], d, true)
		}
	}
	if m := reMonDay.FindStringSubmatchIndex(lower); m != nil {
		mon := months[lower[m[2]:m[3]]]
		day, _ := strconv.Atoi(lower[m[4]:m[5]])
		consider(m[:2], monthDayDate(ref, mon, day), validDay(mon, day))
	}
	if m := reDayMon.FindStringSubmatchIndex(lower); m != nil {
		day, _ := strconv.Atoi(lower[m[2]:m[3]])
		mon := months[lower[m[4]:m[5]]]
		consider(m[:2], monthDayDate(ref, mon, day), validDay(mon, day))
	}

	if bestPos == -1 {
		return time.Time{}, false
	}
	return best, true
}

func endOfDay(t time.Time) time.Time {
	y, m, d := t.Date()
	return time.Date(y, m, d, 23, 59, 59, 0, t.Location())
}

// weekdayDate: bare weekday → nearest occurrence on/after ref (today if it is
// that weekday); "next" → the following week's occurrence.
func weekdayDate(ref time.Time, wd time.Weekday, next bool) time.Time {
	delta := (int(wd) - int(ref.Weekday()) + 7) % 7
	if next {
		delta += 7
	}
	return ref.AddDate(0, 0, delta)
}

// onOrAfterWeekday returns the nearest wd on/after ref (today if ref is wd).
func onOrAfterWeekday(ref time.Time, wd time.Weekday) time.Time {
	delta := (int(wd) - int(ref.Weekday()) + 7) % 7
	return ref.AddDate(0, 0, delta)
}

func endOfMonth(ref time.Time) time.Time {
	y, m, _ := ref.Date()
	// day 0 of next month == last day of this month.
	return time.Date(y, m+1, 0, 0, 0, 0, 0, ref.Location())
}

func isoDate(s string, m []int, ref time.Time) (time.Time, bool) {
	y, _ := strconv.Atoi(s[m[2]:m[3]])
	mo, _ := strconv.Atoi(s[m[4]:m[5]])
	d, _ := strconv.Atoi(s[m[6]:m[7]])
	if mo < 1 || mo > 12 || !validDay(time.Month(mo), d) {
		return time.Time{}, false
	}
	return time.Date(y, time.Month(mo), d, 0, 0, 0, 0, ref.Location()), true
}

func slashDate(s string, m []int, ref time.Time) (time.Time, bool) {
	mo, _ := strconv.Atoi(s[m[2]:m[3]])
	d, _ := strconv.Atoi(s[m[4]:m[5]])
	if mo < 1 || mo > 12 || !validDay(time.Month(mo), d) {
		return time.Time{}, false
	}
	if m[6] != -1 {
		y, _ := strconv.Atoi(s[m[6]:m[7]])
		if y < 100 {
			y += 2000
		}
		return time.Date(y, time.Month(mo), d, 0, 0, 0, 0, ref.Location()), true
	}
	return rollForward(ref, time.Month(mo), d), true
}

// monthDayDate builds a date in ref's year, rolling to next year if it's already past.
func monthDayDate(ref time.Time, mo time.Month, day int) time.Time {
	return rollForward(ref, mo, day)
}

// rollForward returns mo/day in ref's year, or next year if that date is before ref's date.
func rollForward(ref time.Time, mo time.Month, day int) time.Time {
	cand := time.Date(ref.Year(), mo, day, 0, 0, 0, 0, ref.Location())
	refDay := time.Date(ref.Year(), ref.Month(), ref.Day(), 0, 0, 0, 0, ref.Location())
	if cand.Before(refDay) {
		cand = cand.AddDate(1, 0, 0)
	}
	return cand
}

func validDay(mo time.Month, day int) bool {
	if day < 1 || day > 31 {
		return false
	}
	switch mo {
	case time.April, time.June, time.September, time.November:
		return day <= 30
	case time.February:
		return day <= 29
	default:
		return day <= 31
	}
}

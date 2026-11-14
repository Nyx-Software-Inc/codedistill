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

package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"codedistill/internal/seckb"
	"codedistill/internal/storage"
)

// The security knowledge base's storage (migration 0087). A source's entries
// are replaced in one transaction, so a reader sees the old version or the
// new one, never part of each.

const securitySourceCols = `id, last_attempt, last_success, last_error, failures, version, last_modified, entries`

func scanSecuritySource(sc interface{ Scan(...any) error }) (*seckb.SourceRow, error) {
	r := &seckb.SourceRow{}
	var attempt, success sql.NullTime
	if err := sc.Scan(&r.ID, &attempt, &success, &r.LastError, &r.Failures, &r.Version, &r.LastModified, &r.Entries); err != nil {
		return nil, err
	}
	if attempt.Valid {
		t := attempt.Time.UTC()
		r.LastAttempt = &t
	}
	if success.Valid {
		t := success.Time.UTC()
		r.LastSuccess = &t
	}
	return r, nil
}

// GetSecuritySource returns what is known about a source, or nil when it has
// never been refreshed.
func (s *Store) GetSecuritySource(ctx context.Context, id string) (*seckb.SourceRow, error) {
	r, err := scanSecuritySource(s.DB.QueryRowContext(ctx,
		`SELECT `+securitySourceCols+` FROM security_sources WHERE id = ?`, id))
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("security source %s: %w", id, err)
	}
	return r, nil
}

// ListSecuritySources returns every source that has been refreshed or tried.
func (s *Store) ListSecuritySources(ctx context.Context) ([]*seckb.SourceRow, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT `+securitySourceCols+` FROM security_sources ORDER BY id`)
	if err != nil {
		return nil, fmt.Errorf("list security sources: %w", err)
	}
	defer rows.Close()
	var out []*seckb.SourceRow
	for rows.Next() {
		r, err := scanSecuritySource(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// SaveSecuritySource records a refresh, successful or not.
func (s *Store) SaveSecuritySource(ctx context.Context, r *seckb.SourceRow) error {
	_, err := s.DB.ExecContext(ctx,
		`INSERT INTO security_sources (`+securitySourceCols+`) VALUES (?, ?, ?, ?, ?, ?, ?, ?)
		 ON CONFLICT (id) DO UPDATE SET last_attempt = excluded.last_attempt, last_success = excluded.last_success,
		   last_error = excluded.last_error, failures = excluded.failures, version = excluded.version,
		   last_modified = excluded.last_modified, entries = excluded.entries`,
		r.ID, nullTimePtr(r.LastAttempt), nullTimePtr(r.LastSuccess), r.LastError, r.Failures, r.Version, r.LastModified, r.Entries)
	if err != nil {
		return fmt.Errorf("save security source %s: %w", r.ID, err)
	}
	return nil
}

// ReplaceSecurityEntries swaps a source's entries for a new version.
func (s *Store) ReplaceSecurityEntries(ctx context.Context, source, version string, es []seckb.Entry) error {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `DELETE FROM security_entries WHERE source = ?`, source); err != nil {
		return fmt.Errorf("replace %s entries: %w", source, err)
	}
	for _, e := range es {
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO security_entries (key, source, title, body, url, version) VALUES (?, ?, ?, ?, ?, ?)
			 ON CONFLICT (key) DO UPDATE SET source = excluded.source, title = excluded.title, body = excluded.body,
			   url = excluded.url, version = excluded.version`,
			e.Key, source, e.Title, e.Body, e.URL, version); err != nil {
			return fmt.Errorf("store %s: %w", e.Key, err)
		}
	}
	return tx.Commit()
}

// ReplaceKEV swaps the known-exploited list for a new version.
func (s *Store) ReplaceKEV(ctx context.Context, version string, ks []seckb.KEV) error {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `DELETE FROM security_kev`); err != nil {
		return fmt.Errorf("replace KEV: %w", err)
	}
	for _, k := range ks {
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO security_kev (cve, vendor, product, name, date_added, description, required_action, due_date, ransomware, cwes, version)
			 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?) ON CONFLICT (cve) DO NOTHING`,
			k.CVE, k.Vendor, k.Product, k.Name, k.DateAdded, k.Description, k.RequiredAction, k.DueDate,
			k.Ransomware, strings.Join(k.CWEs, ","), version); err != nil {
			return fmt.Errorf("store %s: %w", k.CVE, err)
		}
	}
	return tx.Commit()
}

// GetSecurityEntry returns one entry by its key ("CWE-916").
func (s *Store) GetSecurityEntry(ctx context.Context, key string) (*seckb.Entry, error) {
	e := &seckb.Entry{}
	err := s.DB.QueryRowContext(ctx,
		`SELECT key, source, title, body, url, version FROM security_entries WHERE key = ?`, strings.ToUpper(strings.TrimSpace(key))).
		Scan(&e.Key, &e.Source, &e.Title, &e.Body, &e.URL, &e.Version)
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("security entry %s: %w", key, storage.ErrNotFound)
	}
	if err != nil {
		return nil, err
	}
	return e, nil
}

// GetKEV returns a known-exploited vulnerability by its CVE, or nil when the
// CVE is not on the list.
func (s *Store) GetKEV(ctx context.Context, cve string) (*seckb.KEV, error) {
	k := &seckb.KEV{}
	var cwes string
	err := s.DB.QueryRowContext(ctx,
		`SELECT cve, vendor, product, name, date_added, description, required_action, due_date, ransomware, cwes
		 FROM security_kev WHERE cve = ?`, strings.ToUpper(strings.TrimSpace(cve))).
		Scan(&k.CVE, &k.Vendor, &k.Product, &k.Name, &k.DateAdded, &k.Description, &k.RequiredAction, &k.DueDate, &k.Ransomware, &cwes)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if cwes != "" {
		k.CWEs = strings.Split(cwes, ",")
	}
	return k, nil
}

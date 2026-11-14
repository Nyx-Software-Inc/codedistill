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

package seckb

import (
	"archive/zip"
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const cweSample = `<?xml version="1.0" encoding="UTF-8"?><Weakness_Catalog Name="CWE" Version="5.0" Date="2026-10-28" xmlns="http://cwe.mitre.org/cwe-8" xmlns:xhtml="http://www.w3.org/1999/xhtml">
<Weaknesses>
<Weakness ID="916" Name="Use of Password Hash With Insufficient Computational Effort" Abstraction="Base" Status="Incomplete">
 <Description>The product generates a hash for a password, but it uses a scheme that does not provide a sufficient level of computational effort.</Description>
 <Extended_Description><xhtml:p>Many hash algorithms are designed to execute quickly &amp; cheaply.</xhtml:p><xhtml:ul><xhtml:li>stretching</xhtml:li><xhtml:li>salting</xhtml:li></xhtml:ul></Extended_Description>
 <Potential_Mitigations>
  <Mitigation><Phase>Architecture and Design</Phase><Description><xhtml:p>Use an adaptive hash function such as bcrypt, scrypt or PBKDF2.</xhtml:p></Description></Mitigation>
 </Potential_Mitigations>
</Weakness>
<Weakness ID="1" Name="Gone" Status="Deprecated"><Description>old</Description></Weakness>
</Weaknesses></Weakness_Catalog>`

func zipped(t *testing.T, name, body string) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	w, _ := zw.Create(name)
	w.Write([]byte(body))
	zw.Close()
	return buf.Bytes()
}

func TestParseCWE(t *testing.T) {
	es, v, err := ParseCWE(zipped(t, "cwec_v5.0.xml", cweSample))
	if err != nil {
		t.Fatal(err)
	}
	if v != "5.0, 2026-10-28" || len(es) != 1 {
		t.Fatalf("version %q, %d entries (deprecated ones are left out)", v, len(es))
	}
	e := es[0]
	if e.Key != "CWE-916" || !strings.HasPrefix(e.Title, "Use of Password Hash") || e.URL != "https://cwe.mitre.org/data/definitions/916.html" {
		t.Errorf("entry = %+v", e)
	}
	for _, want := range []string{"computational effort", "quickly & cheaply", "\nstretching\nsalting", "Mitigations:\n- Use an adaptive hash function such as bcrypt, scrypt or PBKDF2. (Architecture and Design)"} {
		if !strings.Contains(e.Body, want) {
			t.Errorf("body lacks %q:\n%s", want, e.Body)
		}
	}
	if strings.Contains(e.Body, "<") {
		t.Errorf("markup left in the body:\n%s", e.Body)
	}
}

func TestParseKEV(t *testing.T) {
	ks, v, err := ParseKEV([]byte(`{"catalogVersion":"2026.11.09","vulnerabilities":[{"cveID":"CVE-2026-1","vendorProject":"V","product":"P","cwes":["CWE-287"]}]}`))
	if err != nil || v != "2026.11.09" || len(ks) != 1 || ks[0].CVE != "CVE-2026-1" || ks[0].CWEs[0] != "CWE-287" {
		t.Fatalf("%v %q %+v", err, v, ks)
	}
	if _, _, err := ParseKEV([]byte(`{"vulnerabilities":[]}`)); err == nil {
		t.Error("an empty catalogue was accepted")
	}
}

// The real files, when they are at hand: SECKB_SAMPLES=<dir with cwe.zip, kev.json>.
func TestParseRealSources(t *testing.T) {
	dir := os.Getenv("SECKB_SAMPLES")
	if dir == "" {
		t.Skip("set SECKB_SAMPLES to a directory holding cwe.zip and kev.json")
	}
	b, _ := os.ReadFile(filepath.Join(dir, "cwe.zip"))
	es, v, err := ParseCWE(b)
	if err != nil || len(es) < 800 {
		t.Fatalf("CWE: %v, %d entries", err, len(es))
	}
	t.Logf("CWE %s: %d weaknesses", v, len(es))
	for _, e := range es {
		if e.Key == "CWE-916" {
			t.Logf("%s %s\n%s", e.Key, e.Title, e.Body)
		}
	}
	kb, _ := os.ReadFile(filepath.Join(dir, "kev.json"))
	ks, kv, err := ParseKEV(kb)
	if err != nil || len(ks) < 1000 {
		t.Fatalf("KEV: %v, %d entries", err, len(ks))
	}
	t.Logf("KEV %s: %d vulnerabilities", kv, len(ks))
}

func TestStateAndDue(t *testing.T) {
	now := time.Date(2026, 11, 9, 12, 0, 0, 0, time.UTC)
	kev, _ := SourceByID("kev")
	at := func(d time.Duration) *time.Time { x := now.Add(-d); return &x }
	cases := []struct {
		name string
		st   Status
		want string
	}{
		{"never", Status{}, StateNever},
		{"fresh", Status{LastSuccess: at(time.Hour), LastAttempt: at(time.Hour)}, StateCurrent},
		{"failing but usable", Status{LastSuccess: at(30 * time.Hour), LastAttempt: at(time.Hour), LastError: "timeout"}, StateRetrying},
		{"past schedule and grace", Status{LastSuccess: at(49 * time.Hour), LastAttempt: at(time.Hour), LastError: "timeout"}, StateStale},
	}
	for _, c := range cases {
		if got := State(kev, c.st, now); got != c.want {
			t.Errorf("%s: %s, want %s", c.name, got, c.want)
		}
	}
	if !Due(kev, Status{}, now, 0) {
		t.Error("a source never synced is due")
	}
	if Due(kev, Status{LastSuccess: at(2 * time.Hour), LastAttempt: at(2 * time.Hour)}, now, 0) {
		t.Error("a daily source refreshed two hours ago is not due")
	}
	failed := Status{LastSuccess: at(30 * time.Hour), LastAttempt: at(90 * time.Minute), LastError: "x"}
	if !Due(kev, failed, now, 0) || Due(kev, failed, now, 1) {
		t.Error("retries back off: an hour after the first failure, two after the second")
	}
}

func TestFetchHonoursNotModified(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("If-Modified-Since") == "Mon, 09 Nov 2026 19:54:37 GMT" {
			w.WriteHeader(http.StatusNotModified)
			return
		}
		w.Header().Set("Last-Modified", "Mon, 09 Nov 2026 19:54:37 GMT")
		w.Write([]byte("body"))
	}))
	defer srv.Close()
	s := Source{ID: "x", URL: srv.URL, MaxBytes: 100}
	b, lm, err := Fetch(context.Background(), srv.Client(), s, "")
	if err != nil || string(b) != "body" || lm == "" {
		t.Fatalf("%q %q %v", b, lm, err)
	}
	if _, _, err := Fetch(context.Background(), srv.Client(), s, lm); err != ErrNotModified {
		t.Errorf("unchanged source: %v", err)
	}
	s.MaxBytes = 2
	if _, _, err := Fetch(context.Background(), srv.Client(), s, ""); err == nil {
		t.Error("an oversized download was read")
	}
}

type memStore struct {
	rows    map[string]*SourceRow
	entries map[string][]Entry
	kev     []KEV
}

func (m *memStore) GetSecuritySource(_ context.Context, id string) (*SourceRow, error) {
	if r, ok := m.rows[id]; ok {
		c := *r
		return &c, nil
	}
	return nil, nil
}
func (m *memStore) SaveSecuritySource(_ context.Context, r *SourceRow) error {
	c := *r
	m.rows[r.ID] = &c
	return nil
}
func (m *memStore) ReplaceSecurityEntries(_ context.Context, src, _ string, es []Entry) error {
	m.entries[src] = es
	return nil
}
func (m *memStore) ReplaceKEV(_ context.Context, _ string, ks []KEV) error { m.kev = ks; return nil }

// A refresh stores what parses, records success, skips what is not due, and
// on failure keeps the old data and says why.
func TestRefresher(t *testing.T) {
	kevBody := `{"catalogVersion":"2026.11.09","vulnerabilities":[{"cveID":"CVE-1"}]}`
	cwe := zipped(t, "c.xml", cweSample)
	failing := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if failing {
			http.Error(w, "down", http.StatusBadGateway)
			return
		}
		if strings.HasSuffix(r.URL.Path, ".zip") {
			w.Write(cwe)
			return
		}
		w.Write([]byte(kevBody))
	}))
	defer srv.Close()
	saved := Sources
	t.Cleanup(func() { Sources = saved })
	Sources = []Source{
		{ID: "cwe", URL: srv.URL + "/cwe.zip", Every: week, Grace: 3 * day, MaxBytes: 1 << 20},
		{ID: "kev", URL: srv.URL + "/kev.json", Every: day, Grace: day, MaxBytes: 1 << 20},
	}
	now := time.Date(2026, 11, 9, 12, 0, 0, 0, time.UTC)
	st := &memStore{rows: map[string]*SourceRow{}, entries: map[string][]Entry{}}
	f := &Refresher{Store: st, Client: srv.Client(), Now: func() time.Time { return now }}

	if n := f.RunOnce(context.Background()); n != 2 || len(st.entries["cwe"]) != 1 || len(st.kev) != 1 {
		t.Fatalf("first run refreshed %d; cwe %d, kev %d", n, len(st.entries["cwe"]), len(st.kev))
	}
	if st.rows["kev"].Version != "2026.11.09" || st.rows["cwe"].Entries != 1 || st.rows["kev"].LastSuccess == nil {
		t.Errorf("rows = %+v %+v", st.rows["cwe"], st.rows["kev"])
	}
	if n := f.RunOnce(context.Background()); n != 0 {
		t.Errorf("nothing is due an instant later, refreshed %d", n)
	}

	// A day on, KEV is due; the source is down.
	now = now.Add(25 * time.Hour)
	failing = true
	f.RunOnce(context.Background())
	k := st.rows["kev"]
	if k.LastError == "" || k.Failures != 1 || len(st.kev) != 1 || State(Sources[1], k.Status(), now) != StateRetrying {
		t.Errorf("after a failure: %+v, kev %d, state %s", k, len(st.kev), State(Sources[1], k.Status(), now))
	}
	// Still down a day later: stale.
	now = now.Add(24 * time.Hour)
	f.RunOnce(context.Background())
	if s := State(Sources[1], st.rows["kev"].Status(), now); s != StateStale {
		t.Errorf("state = %s, want stale", s)
	}
	// Switched off (air-gapped): nothing is fetched.
	failing = false
	f.Enabled = func() bool { return false }
	if n := f.RunOnce(context.Background()); n != 0 {
		t.Errorf("refreshed %d while switched off", n)
	}
}

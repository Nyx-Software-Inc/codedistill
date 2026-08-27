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

package api

import (
	"crypto/tls"
	"net/http"
	"net/http/httptest"
	"testing"
)

// Auth/session cookies carry Secure only for HTTPS deployments: off for
// plain-http localhost (desktop), on when WithSecureCookies is set or the
// request arrived over TLS (G124/CWE-614). HttpOnly + SameSite are constant.
func TestAuthCookieSecureAttribute(t *testing.T) {
	// SetAuthCookie: Secure follows the passed flag; HttpOnly + SameSite=Strict always.
	for _, secure := range []bool{false, true} {
		rec := httptest.NewRecorder()
		SetAuthCookie(rec, "tok", secure)
		cs := rec.Result().Cookies()
		if len(cs) != 1 {
			t.Fatalf("secure=%v: got %d cookies, want 1", secure, len(cs))
		}
		c := cs[0]
		if c.Secure != secure {
			t.Errorf("secure=%v: cookie.Secure = %v, want %v", secure, c.Secure, secure)
		}
		if !c.HttpOnly {
			t.Errorf("secure=%v: HttpOnly not set", secure)
		}
		if c.SameSite != http.SameSiteStrictMode {
			t.Errorf("secure=%v: SameSite = %v, want Strict", secure, c.SameSite)
		}
	}

	// cookieSecure: true when configured OR the request arrived over TLS.
	plain := httptest.NewRequest(http.MethodGet, "http://localhost/", nil)
	if (&Server{}).cookieSecure(plain) {
		t.Error("cookieSecure = true for plain-http request with secureCookies off")
	}
	if !(&Server{secureCookies: true}).cookieSecure(plain) {
		t.Error("cookieSecure = false when WithSecureCookies set")
	}
	tlsReq := httptest.NewRequest(http.MethodGet, "https://example.com/", nil)
	tlsReq.TLS = &tls.ConnectionState{}
	if !(&Server{}).cookieSecure(tlsReq) {
		t.Error("cookieSecure = false for a TLS request")
	}
}

// requireWriteAuth: reads open; writes need the token via bearer header or
// the cd_auth cookie; wrong/absent credential is 401; empty token disables.
func TestRequireWriteAuth(t *testing.T) {
	const tok = "s3cr3t-token"
	s := &Server{authToken: tok}
	ok := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })
	ts := httptest.NewServer(s.requireWriteAuth(ok))
	defer ts.Close()

	do := func(method string, set func(*http.Request)) int {
		req, _ := http.NewRequest(method, ts.URL+"/api/v1/x", nil)
		if set != nil {
			set(req)
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("%s: %v", method, err)
		}
		resp.Body.Close()
		return resp.StatusCode
	}
	bearer := func(v string) func(*http.Request) {
		return func(r *http.Request) { r.Header.Set("Authorization", "Bearer "+v) }
	}
	cookie := func(v string) func(*http.Request) {
		return func(r *http.Request) { r.AddCookie(&http.Cookie{Name: authCookieName, Value: v}) }
	}

	cases := []struct {
		name   string
		method string
		set    func(*http.Request)
		want   int
	}{
		{"read open, no creds", "GET", nil, http.StatusOK},
		{"write, no creds", "POST", nil, http.StatusUnauthorized},
		{"write, bearer ok", "POST", bearer(tok), http.StatusOK},
		{"write, bearer wrong", "POST", bearer("nope"), http.StatusUnauthorized},
		{"write, cookie ok", "POST", cookie(tok), http.StatusOK},
		{"write, cookie wrong", "POST", cookie("nope"), http.StatusUnauthorized},
		{"delete, no creds", "DELETE", nil, http.StatusUnauthorized},
		{"patch, bearer ok", "PATCH", bearer(tok), http.StatusOK},
	}
	for _, c := range cases {
		if got := do(c.method, c.set); got != c.want {
			t.Errorf("%s: got %d, want %d", c.name, got, c.want)
		}
	}

	// Empty token disables the gate entirely (writes flow).
	off := httptest.NewServer((&Server{authToken: ""}).requireWriteAuth(ok))
	defer off.Close()
	req, _ := http.NewRequest("POST", off.URL+"/x", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("auth disabled: POST got %d, want 200", resp.StatusCode)
	}
}

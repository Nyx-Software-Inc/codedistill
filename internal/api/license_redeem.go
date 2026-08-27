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

// In-app license redemption (activation-service slice 2): the License area
// posts a claim code here; we call the activation service with THIS machine's
// fingerprint, receive a node-locked license, and install it. Deliberately
// part of every build including the CE — redeeming a purchase is the upgrade
// funnel, so the community binary must be able to do it.

import (
	"bytes"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"codedistill/internal/licensing"
)

// redeemConfig is wired by serve: where the installed license goes and which
// activation service to call.
type redeemConfig struct {
	dest        string // resolved license file path
	activateURL string // e.g. https://activate.codedistill.dev
	client      *http.Client
	pubkeys     []ed25519.PublicKey // trust list override (tests); nil = embedded keys
}

func (rc *redeemConfig) trustList() []ed25519.PublicKey {
	if rc.pubkeys != nil {
		return rc.pubkeys
	}
	return licensing.PublicKeys()
}

// WithLicenseRedeem enables POST /license/redeem + /license/refresh.
func (s *Server) WithLicenseRedeem(dest, activateURL string) *Server {
	s.redeem = &redeemConfig{dest: dest, activateURL: strings.TrimRight(activateURL, "/")}
	return s
}

// claimCodePath sits next to the license so "Refresh license" can re-redeem
// after a renewal without the user retyping the code.
func (rc *redeemConfig) claimCodePath() string {
	return filepath.Join(filepath.Dir(rc.dest), "claim-code")
}

func (s *Server) redeemLicense(w http.ResponseWriter, r *http.Request) {
	if s.redeem == nil {
		writeMsg(w, http.StatusServiceUnavailable, "license redemption is not configured on this server")
		return
	}
	var req struct {
		ClaimCode string `json:"claim_code"`
	}
	if err := readJSON(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	code := strings.TrimSpace(req.ClaimCode)
	if code == "" {
		writeMsg(w, http.StatusBadRequest, "claim_code is required")
		return
	}
	s.doRedeem(w, r, code)
}

// refreshLicense re-redeems with the stored claim code — how a renewed
// subscription's new expiry reaches this install.
func (s *Server) refreshLicense(w http.ResponseWriter, r *http.Request) {
	if s.redeem == nil {
		writeMsg(w, http.StatusServiceUnavailable, "license redemption is not configured on this server")
		return
	}
	raw, err := os.ReadFile(s.redeem.claimCodePath())
	if err != nil {
		writeMsg(w, http.StatusNotFound, "no stored claim code — redeem a purchase code first")
		return
	}
	s.doRedeem(w, r, strings.TrimSpace(string(raw)))
}

func (s *Server) doRedeem(w http.ResponseWriter, r *http.Request, code string) {
	fp, err := licensing.Fingerprint()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, fmt.Errorf("machine fingerprint: %w", err))
		return
	}
	body, _ := json.Marshal(map[string]string{"claim_code": code, "fingerprint": fp})
	httpReq, err := http.NewRequestWithContext(r.Context(), http.MethodPost,
		s.redeem.activateURL+"/redeem", bytes.NewReader(body))
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	httpReq.Header.Set("Content-Type", "application/json")
	client := s.redeem.client
	if client == nil {
		client = &http.Client{Timeout: 30 * time.Second}
	}
	resp, err := client.Do(httpReq)
	if err != nil {
		writeMsg(w, http.StatusBadGateway, "could not reach the activation service — check your network and try again")
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		// Surface the service's own message (seats exhausted / expired / unknown
		// code) with its status so the UI can show it verbatim.
		msg, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		writeMsg(w, resp.StatusCode, strings.TrimSpace(string(msg)))
		return
	}
	var out struct {
		License   string    `json:"license"`
		Customer  string    `json:"customer"`
		Edition   string    `json:"edition"`
		ExpiresAt time.Time `json:"expires_at"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		writeErr(w, http.StatusBadGateway, fmt.Errorf("activation service response: %w", err))
		return
	}
	lic, err := base64.StdEncoding.DecodeString(out.License)
	if err != nil {
		writeErr(w, http.StatusBadGateway, fmt.Errorf("license decode: %w", err))
		return
	}
	// Sanity-verify BEFORE installing: the returned file must be a trusted,
	// valid license for THIS machine. Refuse to write anything else.
	st := licensing.VerifyAny(lic, s.redeem.trustList(), time.Now().UTC(), fp, "")
	if st.State != licensing.StateValid && st.State != licensing.StateGrace {
		writeMsg(w, http.StatusBadGateway, "activation service returned an unusable license: "+st.Reason)
		return
	}
	if err := os.MkdirAll(filepath.Dir(s.redeem.dest), 0o700); err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	if err := os.WriteFile(s.redeem.dest, lic, 0o600); err != nil {
		writeErr(w, http.StatusInternalServerError, fmt.Errorf("install license: %w", err))
		return
	}
	if err := os.WriteFile(s.redeem.claimCodePath(), []byte(code+"\n"), 0o600); err != nil {
		s.log.Warn("store claim code failed (refresh will need the code re-entered)", "err", err)
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"customer":         out.Customer,
		"edition":          out.Edition,
		"expires_at":       out.ExpiresAt,
		"restart_required": true,
	})
}

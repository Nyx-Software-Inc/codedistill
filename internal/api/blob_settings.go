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
	"context"
	"encoding/json"
	"time"
)

// Settings keys consumed by the rich-canvas blob plumbing. Per-key
// cascade is project → user → installation default (set via
// WithBlobStore + DefaultBlobConfig). Workspace-scope settings are
// designed but not yet implemented; per-workspace quotas hook in
// here when the workspace scope lands.
const (
	settingBlobMaxBytes      = "blob.max_bytes_per_item"
	settingBlobAllowedMimes  = "blob.allowed_mimes"
	settingBlobURLTTLSeconds = "blob.url_ttl_seconds"
)

// LocalUserID is the implicit user id for single-user installs. Same
// constant the SPA uses in lib/userSettings.ts. Hosted SaaS will
// resolve the real user id from request auth and stop hardcoding
// this.
const LocalUserID = "local"

// resolveBlobConfig walks the settings cascade (project → user →
// installation floor) to produce the effective BlobConfig for a
// request against the given scratchpad. Cheap — three settings
// lookups against an indexed table — but worth caching if/when this
// becomes hot.
//
// The installation floor comes from whatever was passed to
// WithBlobStore (or DefaultBlobConfig() if WithBlobStore wasn't
// called). Each setting key, when present at a higher-priority scope,
// fully replaces the floor's value for that field — there's no
// per-field merging beyond "first present wins."
func (s *Server) resolveBlobConfig(ctx context.Context, scratchpadID string) BlobConfig {
	cfg := s.blobConfig
	if cfg.MaxBytesPerItem <= 0 {
		cfg = DefaultBlobConfig()
	}

	var projectID string
	if scratchpadID != "" {
		if sp, err := s.store.GetScratchpad(ctx, scratchpadID); err == nil {
			projectID = sp.ProjectID
		}
	}

	if v, ok := s.lookupSettingInt64(ctx, settingBlobMaxBytes, projectID, LocalUserID); ok && v > 0 {
		cfg.MaxBytesPerItem = v
	}
	if v, ok := s.lookupSettingStrings(ctx, settingBlobAllowedMimes, projectID, LocalUserID); ok && len(v) > 0 {
		cfg.AllowedMimes = v
	}
	if v, ok := s.lookupSettingInt64(ctx, settingBlobURLTTLSeconds, projectID, LocalUserID); ok && v > 0 {
		cfg.URLTTL = time.Duration(v) * time.Second
	}
	return cfg
}

// lookupSettingInt64 returns the first JSON-int64 value found in the
// scope chain. The cascade order is project, then user — first hit
// wins. Empty scope ids and unparseable JSON values are skipped
// silently; a bad-JSON value at one scope DOES NOT mask a valid
// value at a lower scope. The bool reports whether a value was
// successfully parsed, not whether a row exists.
func (s *Server) lookupSettingInt64(ctx context.Context, key, projectID, userID string) (int64, bool) {
	if projectID != "" {
		if st, err := s.store.GetProjectSetting(ctx, projectID, key); err == nil {
			var v int64
			if json.Unmarshal(st.Value, &v) == nil {
				return v, true
			}
		}
	}
	if userID != "" {
		if st, err := s.store.GetUserSetting(ctx, userID, key); err == nil {
			var v int64
			if json.Unmarshal(st.Value, &v) == nil {
				return v, true
			}
		}
	}
	return 0, false
}

// lookupSettingStrings returns the first JSON-[]string value found
// in the scope chain. Same cascade + parse-skip semantics as
// lookupSettingInt64.
func (s *Server) lookupSettingStrings(ctx context.Context, key, projectID, userID string) ([]string, bool) {
	if projectID != "" {
		if st, err := s.store.GetProjectSetting(ctx, projectID, key); err == nil {
			var v []string
			if json.Unmarshal(st.Value, &v) == nil {
				return v, true
			}
		}
	}
	if userID != "" {
		if st, err := s.store.GetUserSetting(ctx, userID, key); err == nil {
			var v []string
			if json.Unmarshal(st.Value, &v) == nil {
				return v, true
			}
		}
	}
	return nil, false
}

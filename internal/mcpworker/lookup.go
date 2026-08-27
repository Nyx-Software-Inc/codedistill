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

package mcpworker

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"codedistill/internal/storage"
)

// SettingsLookup reads MCP-export configuration from user_settings.
// Settings key shape: "mcp.export.<short>" where <short> is one of
// "todo" | "bug" | "kb" | "use_case" (per OwnerTypeToShort).
//
// Value shape (matches Config.MarshalJSON via the field tags):
//
//	{
//	  "endpoint":   { "url": "...", "credentials": {...}, "headers": {...} },
//	  "mapping":    { "create": "tool_name", "update": "...", ... }
//	}
//
// Returns (nil, nil) when the key is missing — that's "no destination
// configured for this user/type," not an error. Returns a wrapped error
// only when storage fails or the value can't be unmarshaled.
type SettingsLookup struct {
	store storage.Storage
}

func NewSettingsLookup(store storage.Storage) *SettingsLookup {
	return &SettingsLookup{store: store}
}

// settingsKey returns the user_settings key for a given short type.
// Centralized so the writer (settings UI in step 7) can use the same
// helper rather than hardcoding the format.
func settingsKey(shortType string) string {
	return "mcp.export." + shortType
}

// SettingsKey is the public accessor for the key format. Used by the
// hook (and eventually the settings UI) to write the same shape this
// lookup reads.
func SettingsKey(shortType string) string { return settingsKey(shortType) }

func (l *SettingsLookup) Get(ctx context.Context, userID, shortType string) (*Config, error) {
	if l == nil || l.store == nil {
		return nil, fmt.Errorf("settings lookup: nil store")
	}
	s, err := l.store.GetUserSetting(ctx, userID, settingsKey(shortType))
	if err != nil {
		if errors.Is(err, storage.ErrNotFound) {
			return nil, nil
		}
		return nil, fmt.Errorf("settings lookup: get %s/%s: %w", userID, shortType, err)
	}
	if len(s.Value) == 0 {
		return nil, nil
	}
	cfg := &Config{}
	if err := json.Unmarshal(s.Value, cfg); err != nil {
		return nil, fmt.Errorf("settings lookup: unmarshal %s/%s: %w", userID, shortType, err)
	}
	if cfg.Endpoint.URL == "" {
		// Treat malformed/empty config as "not configured" rather than
		// trying to push to nowhere. The UI should validate before save;
		// this is the belt-and-braces.
		return nil, nil
	}
	return cfg, nil
}

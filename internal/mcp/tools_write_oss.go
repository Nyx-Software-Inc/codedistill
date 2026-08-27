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

//go:build oss

package mcp

import "github.com/mark3labs/mcp-go/server"

// registerWriteTools is a no-op in the open-source build: MCP write tools
// are a paid feature, physically absent from the community edition. Read
// tools remain available, so "MCP read free" still holds.
func (t *Tools) registerWriteTools(_ *server.MCPServer) {}

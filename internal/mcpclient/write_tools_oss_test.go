//go:build oss

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

package mcpclient_test

// The community build registers no MCP write tools — "MCP read free, write
// paid" — so the catalog must advertise none of them. Asserting the empty set
// here turns that from an assumption into a tested property.
var expectedWriteTools []string

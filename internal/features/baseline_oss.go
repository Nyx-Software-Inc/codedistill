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

package features

// OSSBuild reports whether this binary was compiled with -tags oss.
const OSSBuild = true

// baselineAllows in the oss build pins every paid feature off — no
// license can turn them on. Today the paid packages are still compiled
// in (behavior gating); full compile-out happens when the public
// mirror is cut.
func baselineAllows(Feature) bool { return false }

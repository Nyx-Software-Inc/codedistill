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

package sqlite

import "testing"

// newTestStorePG: the CE has no Postgres backend, so the conformance path is
// skipped. Never reached in practice — CODEDISTILL_TEST_PG isn't set in CE
// runs — but keeps newTestStore compiling under -tags oss.
func newTestStorePG(t *testing.T, _ string) *Store {
	t.Skip("Postgres backend is not built in the Community Edition")
	return nil
}

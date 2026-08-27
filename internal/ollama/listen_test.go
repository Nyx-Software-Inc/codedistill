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

package ollama

import "net"

// listenLocal opens a local TCP socket on a kernel-chosen port. Callers
// immediately close it and reuse the port number — a simple way to pick a
// free port for a subprocess test fixture without racing.
func listenLocal() (net.Listener, error) {
	return net.Listen("tcp", "127.0.0.1:0")
}

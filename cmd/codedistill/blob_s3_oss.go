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

package main

import (
	"fmt"

	"codedistill/internal/blobstore"
)

// newS3Blob is the community-edition stub. The S3 blob backend is a paid
// feature stripped from the open-source build; serve already refuses
// BLOB_S3_ENDPOINT before reaching here (features.S3Blob is never enabled
// in this build), so this exists only to satisfy the linker.
func newS3Blob(_ string) (blobstore.BlobStore, error) {
	return nil, fmt.Errorf("the S3 blob backend is not available in the community edition")
}

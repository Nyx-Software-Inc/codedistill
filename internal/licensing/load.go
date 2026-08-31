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

// License file discovery + the one-call entry point main.go uses.
package licensing

import (
	"crypto/ed25519"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// FileName is the canonical license file name. The default search
// places it next to the DB file so backup/restore of an install
// naturally carries the license along.
const FileName = "codedistill.license"

// ResolvePath returns the license file path to use, in precedence
// order: the explicit -license flag value, $CODEDISTILL_LICENSE,
// then <db sibling>/codedistill.license, then the user config dir
// (~/.config/codedistill/codedistill.license). When no candidate
// exists on disk the sibling-of-DB path is returned with found=false
// (it's where `license install` will write).
func ResolvePath(flagPath, dbPath string) (path string, found bool) {
	if flagPath != "" {
		return flagPath, fileExists(flagPath)
	}
	if env := os.Getenv("CODEDISTILL_LICENSE"); env != "" {
		return env, fileExists(env)
	}
	// A DSN is not a filesystem path, and must not be run through filepath.Dir.
	// Doing so both invented a nonsensical location and — because Dir keeps the
	// userinfo — put the database PASSWORD into the path, which Load then embeds
	// in the not-found reason that `license status` prints to the terminal. Fall
	// back to the user config dir, which is where a server deployment's license
	// belongs anyway.
	if isDSN(dbPath) {
		if dir, err := os.UserConfigDir(); err == nil {
			cfg := filepath.Join(dir, "codedistill", FileName)
			return cfg, fileExists(cfg)
		}
		return FileName, fileExists(FileName)
	}

	sibling := filepath.Join(filepath.Dir(dbPath), FileName)
	if fileExists(sibling) {
		return sibling, true
	}
	if dir, err := os.UserConfigDir(); err == nil {
		cfg := filepath.Join(dir, "codedistill", FileName)
		if fileExists(cfg) {
			return cfg, true
		}
	}
	return sibling, false
}

// isDSN reports whether p is a database connection string rather than a file
// path. Deliberately scheme-agnostic — cmd/codedistill checks specifically for
// postgres:// and postgresql://, but anything with a scheme is not a path, and
// a backend added later must not silently reintroduce the credential leak.
func isDSN(p string) bool { return strings.Contains(p, "://") }

func fileExists(p string) bool {
	info, err := os.Stat(p)
	return err == nil && !info.IsDir()
}

// Load resolves, reads and verifies the license in one call. Returns
// StateNone when no file exists anywhere on the search path. version
// is the running binary version (for min/max range checks).
func Load(flagPath, dbPath, version string) *Status {
	path, found := ResolvePath(flagPath, dbPath)
	if !found {
		return None("no license file (searched -license, $CODEDISTILL_LICENSE, " + path + ", user config dir)")
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return None("no license file at " + path)
		}
		return invalid(fmt.Sprintf("read %s: %v", path, err))
	}
	fp, _ := Fingerprint() // empty fp only matters for locked licenses; Verify reports the mismatch
	return VerifyAny(raw, PublicKeys(), time.Now().UTC(), fp, version)
}

// PublicKey returns the embedded MASTER license-signing public key.
func PublicKey() ed25519.PublicKey {
	return ed25519.PublicKey(embeddedPublicKey[:])
}

// PublicKeys returns the full trust list: the offline master key plus the
// activation service's self-serve key (dual-key model — see pubkey.go).
func PublicKeys() []ed25519.PublicKey {
	return []ed25519.PublicKey{
		ed25519.PublicKey(embeddedPublicKey[:]),
		ed25519.PublicKey(embeddedSelfServeKey[:]),
	}
}

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

// Machine fingerprinting for node-locked licenses. The fingerprint is
// a SHA-256 over a per-OS stable machine identifier, prefixed with a
// scheme tag ("m1:") so the algorithm can evolve without invalidating
// old licenses (a future m2 verifier can accept both).
//
// This is honesty-enforcement, not DRM: anyone determined enough can
// recompile the OSS build. The goal is to make accidental seat
// sprawl visible, not to survive a motivated attacker.
package licensing

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"
)

// Fingerprint returns the local machine fingerprint, e.g.
// "m1:9f86d081...". The empty string with an error means no stable
// identifier could be found — verification of a locked license will
// fail with a clear reason in that case.
func Fingerprint() (string, error) {
	id, err := machineID()
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256([]byte("codedistill:" + strings.TrimSpace(id)))
	return "m1:" + hex.EncodeToString(sum[:]), nil
}

func machineID() (string, error) {
	switch runtime.GOOS {
	case "linux":
		// systemd machine-id, with the dbus path as the pre-systemd
		// fallback. Both survive reboots; neither survives a reinstall
		// (acceptable — customers ask for a re-issue).
		for _, p := range []string{"/etc/machine-id", "/var/lib/dbus/machine-id"} {
			if b, err := os.ReadFile(p); err == nil && len(strings.TrimSpace(string(b))) > 0 {
				return string(b), nil
			}
		}
		return "", fmt.Errorf("no machine-id found (/etc/machine-id missing)")
	case "darwin":
		out, err := exec.Command("ioreg", "-rd1", "-c", "IOPlatformExpertDevice").Output()
		if err != nil {
			return "", fmt.Errorf("ioreg: %w", err)
		}
		for _, line := range strings.Split(string(out), "\n") {
			if strings.Contains(line, "IOPlatformUUID") {
				if i := strings.Index(line, "= \""); i >= 0 {
					return strings.Trim(line[i+3:], "\" "), nil
				}
			}
		}
		return "", fmt.Errorf("IOPlatformUUID not found in ioreg output")
	case "windows":
		// MachineGuid is set at install time and stable thereafter.
		// `reg query` avoids a golang.org/x/sys/windows/registry dep.
		out, err := exec.Command("reg", "query",
			`HKLM\SOFTWARE\Microsoft\Cryptography`, "/v", "MachineGuid").Output()
		if err != nil {
			return "", fmt.Errorf("reg query MachineGuid: %w", err)
		}
		fields := strings.Fields(string(out))
		if len(fields) == 0 {
			return "", fmt.Errorf("MachineGuid not found")
		}
		return fields[len(fields)-1], nil
	default:
		return "", fmt.Errorf("unsupported platform %s", runtime.GOOS)
	}
}

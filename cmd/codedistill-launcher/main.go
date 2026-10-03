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

//go:build unix

// codedistill-launcher is the executable behind CodeDistill.app on macOS.
// It replaces the former shell-script launcher: notarisation and the
// hardened runtime want a Mach-O main executable, and a signed binary is
// what Gatekeeper checks. It does exactly what the script did: keep the
// database under ~/Library/Application Support/CodeDistill, open the UI in
// the default browser once the server has had a moment to start, and then
// become the server process, so macOS treats the app as running until it
// is quit from the Dock.
package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
)

const (
	addr = ":8080"
	url  = "http://localhost:8080"
)

func main() {
	exe, err := os.Executable()
	if err != nil {
		fail(err)
	}
	dir := filepath.Dir(exe)
	home, err := os.UserHomeDir()
	if err != nil {
		fail(err)
	}
	support := filepath.Join(home, "Library", "Application Support", "CodeDistill")
	if err := os.MkdirAll(support, 0o700); err != nil {
		fail(err)
	}
	db := filepath.Join(support, "codedistill.db")

	// The browser opener is a detached child, so it survives the exec below.
	_ = exec.Command("/bin/sh", "-c", "sleep 2 && open "+url).Start()

	server := filepath.Join(dir, "codedistill-bin")
	args := []string{"codedistill-bin", "-db", db, "-addr", addr, "serve"}
	fail(syscall.Exec(server, args, os.Environ())) // only returns on error
}

func fail(err error) {
	fmt.Fprintln(os.Stderr, "CodeDistill launcher:", err)
	os.Exit(1)
}

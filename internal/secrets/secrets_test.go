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

package secrets

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func newKeyring(t *testing.T) *Keyring {
	t.Helper()
	kr, err := OpenOrCreate(filepath.Join(t.TempDir(), "secret.key"))
	if err != nil {
		t.Fatal(err)
	}
	return kr
}

func TestRoundTrip(t *testing.T) {
	kr := newKeyring(t)
	const key = "sk-ant-api03-not-a-real-key"

	enc, err := kr.Encrypt(key)
	if err != nil {
		t.Fatal(err)
	}
	// The stored form must not contain the secret. This is the entire point:
	// the database gets copied, synced and attached to bug reports.
	if strings.Contains(enc, key) || strings.Contains(enc, "sk-ant") {
		t.Fatalf("plaintext survives in the stored value: %q", enc)
	}
	if !IsEncrypted(enc) {
		t.Error("stored value is not recognisable as encrypted")
	}
	got, err := kr.Decrypt(enc)
	if err != nil || got != key {
		t.Fatalf("Decrypt = %q, %v", got, err)
	}
}

// Empty must stay empty: "no secret" should not become an opaque blob that
// looks like one, or every provider without a key appears to have one.
func TestEmptyStaysEmpty(t *testing.T) {
	kr := newKeyring(t)
	enc, err := kr.Encrypt("")
	if err != nil || enc != "" {
		t.Fatalf("Encrypt(\"\") = %q, %v", enc, err)
	}
}

// Same input twice must not produce the same ciphertext, or the store leaks
// which providers share a key.
func TestNonceMakesCiphertextUnique(t *testing.T) {
	kr := newKeyring(t)
	a, _ := kr.Encrypt("same-key")
	b, _ := kr.Encrypt("same-key")
	if a == b {
		t.Fatal("identical plaintext produced identical ciphertext")
	}
}

// A database written before encryption existed holds plaintext. Refusing to
// read it would break the upgrade rather than protect anything.
func TestPlaintextPassesThroughForUpgrade(t *testing.T) {
	kr := newKeyring(t)
	got, err := kr.Decrypt("legacy-plaintext-key")
	if err != nil || got != "legacy-plaintext-key" {
		t.Fatalf("Decrypt of legacy value = %q, %v", got, err)
	}
	if IsEncrypted("legacy-plaintext-key") {
		t.Error("a plaintext value was reported as encrypted, so it would never be upgraded")
	}
}

// Authenticated encryption: a tampered value must fail loudly rather than
// decrypt to garbage that gets sent to a provider as an API key.
func TestTamperingIsDetected(t *testing.T) {
	kr := newKeyring(t)
	enc, _ := kr.Encrypt("secret")
	bad := enc[:len(enc)-2] + "AA"
	if _, err := kr.Decrypt(bad); err == nil {
		t.Fatal("a tampered ciphertext decrypted without error")
	}
}

// A different key must not silently produce wrong plaintext.
func TestWrongKeyFails(t *testing.T) {
	a, b := newKeyring(t), newKeyring(t)
	enc, _ := a.Encrypt("secret")
	if _, err := b.Decrypt(enc); err == nil {
		t.Fatal("decrypted with the wrong key")
	}
}

// Reading must never mint key material: a machine that stores no secret should
// end up with no key file.
func TestOpenDoesNotCreate(t *testing.T) {
	path := filepath.Join(t.TempDir(), "secret.key")
	if _, err := Open(path); !errors.Is(err, ErrNoKey) {
		t.Fatalf("Open on a missing key = %v, want ErrNoKey", err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("Open created a key file")
	}
}

func TestKeyFileIsNotWorldReadable(t *testing.T) {
	path := filepath.Join(t.TempDir(), "secret.key")
	if _, err := OpenOrCreate(path); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Fatalf("key mode is %o, want 600", perm)
	}
}

// Two starts must converge on ONE key, or secrets written under the first
// become unreadable when the second overwrites it.
func TestSecondOpenReusesTheSameKey(t *testing.T) {
	path := filepath.Join(t.TempDir(), "secret.key")
	a, err := OpenOrCreate(path)
	if err != nil {
		t.Fatal(err)
	}
	enc, _ := a.Encrypt("secret")

	b, err := OpenOrCreate(path)
	if err != nil {
		t.Fatal(err)
	}
	got, err := b.Decrypt(enc)
	if err != nil || got != "secret" {
		t.Fatalf("a second OpenOrCreate could not read the first's secret: %q, %v", got, err)
	}
}

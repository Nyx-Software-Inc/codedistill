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

// Package secrets encrypts values that must never sit in the database as
// plain text — provider API keys, first.
//
// WHAT THIS PROTECTS AGAINST, precisely, because a vague claim here is worse
// than none:
//
//   - The database file being copied, synced, backed up, or attached to a bug
//     report. CodeDistill's whole store is one SQLite file that users copy
//     around freely, and `backup` writes another one. A key in cleartext
//     travels with every one of those.
//   - A Postgres deployment where DBAs and backups see table contents.
//   - Casual inspection: opening the db and reading the settings table.
//
// WHAT IT DOES NOT PROTECT AGAINST: someone who can already read arbitrary
// files as the user running CodeDistill. They can read the key file too. That
// is also true of an OS keychain once the session is unlocked, and pretending
// otherwise would be security theatre. The threat being addressed is a
// travelling database, not a compromised account.
//
// The key lives beside the license signing key, in the user config dir, 0600.
// AES-256-GCM from the standard library: no dependency, authenticated, and a
// tampered ciphertext fails to open rather than decrypting to garbage.
package secrets

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// prefix marks a value as produced by this package, so a stored secret is
// recognisable and a plaintext value that predates encryption can be detected
// rather than fed to the decrypter.
const prefix = "cdenc1:"

// ErrNoKey means no key has been created yet. Callers that only READ secrets
// should treat this as "there are none", not as a failure.
var ErrNoKey = errors.New("no encryption key")

// Keyring holds the local encryption key.
type Keyring struct {
	mu   sync.RWMutex
	aead cipher.AEAD
	path string
}

// DefaultKeyPath is where the key lives: beside the license signing key.
func DefaultKeyPath() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "codedistill", "secret.key"), nil
}

// Open loads the key at path. It does NOT create one — a caller that merely
// reads settings should not have the side effect of minting key material.
func Open(path string) (*Keyring, error) {
	raw, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil, ErrNoKey
	}
	if err != nil {
		return nil, fmt.Errorf("read key: %w", err)
	}
	key, err := hex.DecodeString(strings.TrimSpace(string(raw)))
	if err != nil || len(key) != 32 {
		return nil, fmt.Errorf("key at %s is not a 32-byte hex key", path)
	}
	aead, err := newAEAD(key)
	if err != nil {
		return nil, err
	}
	return &Keyring{aead: aead, path: path}, nil
}

// OpenOrCreate loads the key, generating one on first use.
//
// Creation is deliberately separate from Open: writing a secret is the moment a
// key is needed, and a read path that silently creates one would produce a key
// file on machines that never store a secret at all.
func OpenOrCreate(path string) (*Keyring, error) {
	kr, err := Open(path)
	if err == nil {
		return kr, nil
	}
	if !errors.Is(err, ErrNoKey) {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, fmt.Errorf("create key dir: %w", err)
	}
	key := make([]byte, 32)
	if _, err := io.ReadFull(rand.Reader, key); err != nil {
		return nil, fmt.Errorf("generate key: %w", err)
	}
	// O_EXCL: two processes starting together must not both mint a key and
	// have one silently overwrite secrets encrypted under the other.
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		if os.IsExist(err) {
			return Open(path) // lost the race; the winner's key is authoritative
		}
		return nil, fmt.Errorf("create key: %w", err)
	}
	if _, err := f.WriteString(hex.EncodeToString(key) + "\n"); err != nil {
		f.Close()
		return nil, fmt.Errorf("write key: %w", err)
	}
	if err := f.Close(); err != nil {
		return nil, err
	}
	aead, err := newAEAD(key)
	if err != nil {
		return nil, err
	}
	return &Keyring{aead: aead, path: path}, nil
}

func newAEAD(key []byte) (cipher.AEAD, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(block)
}

// Encrypt returns a storable string. An empty input stays empty: "no secret"
// should not become an opaque blob that looks like one.
func (k *Keyring) Encrypt(plain string) (string, error) {
	if plain == "" {
		return "", nil
	}
	k.mu.RLock()
	defer k.mu.RUnlock()

	nonce := make([]byte, k.aead.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", fmt.Errorf("nonce: %w", err)
	}
	sealed := k.aead.Seal(nonce, nonce, []byte(plain), nil)
	return prefix + base64.StdEncoding.EncodeToString(sealed), nil
}

// Decrypt reverses Encrypt.
//
// A value without the marker is returned UNCHANGED. That is deliberate: a
// database written before encryption existed holds plaintext, and failing to
// read it would break the upgrade rather than protect anything. IsEncrypted
// lets a caller re-encrypt such values in place.
func (k *Keyring) Decrypt(stored string) (string, error) {
	if !IsEncrypted(stored) {
		return stored, nil
	}
	k.mu.RLock()
	defer k.mu.RUnlock()

	raw, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(stored, prefix))
	if err != nil {
		return "", fmt.Errorf("secret is not valid base64: %w", err)
	}
	n := k.aead.NonceSize()
	if len(raw) < n {
		return "", errors.New("secret is truncated")
	}
	out, err := k.aead.Open(nil, raw[:n], raw[n:], nil)
	if err != nil {
		// Authenticated: this means a wrong key or a tampered value, and both
		// deserve a clear error rather than silent garbage.
		return "", errors.New("cannot decrypt: wrong key, or the value was altered")
	}
	return string(out), nil
}

// IsEncrypted reports whether a stored value was produced by this package.
func IsEncrypted(s string) bool { return strings.HasPrefix(s, prefix) }

// Path is where the key lives, for diagnostics and for telling a user what to
// back up. Losing this file means every stored secret is unreadable.
func (k *Keyring) Path() string { return k.path }

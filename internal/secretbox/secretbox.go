// Package secretbox encrypts small secrets (TOTP seeds, CI secrets) at rest
// with AES-256-GCM under the server master key.
package secretbox

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Box encrypts and decrypts with a fixed key.
type Box struct {
	aead cipher.AEAD
}

// New creates a Box from a 32-byte key.
func New(key []byte) (*Box, error) {
	if len(key) != 32 {
		return nil, fmt.Errorf("master key must be 32 bytes, got %d", len(key))
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	return &Box{aead: aead}, nil
}

// LoadOrCreateKey reads the hex-encoded key at path, generating and saving
// a new one (mode 0600) if the file doesn't exist. created reports whether
// a new key was written.
func LoadOrCreateKey(path string) (key []byte, created bool, err error) {
	b, err := os.ReadFile(path)
	if err == nil {
		key, err = hex.DecodeString(strings.TrimSpace(string(b)))
		if err != nil {
			return nil, false, fmt.Errorf("master key %s is not valid hex: %w", path, err)
		}
		if len(key) != 32 {
			return nil, false, fmt.Errorf("master key %s must be 32 bytes (64 hex chars)", path)
		}
		return key, false, nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return nil, false, err
	}
	key = make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		return nil, false, err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return nil, false, err
	}
	// O_EXCL so two nodes starting at once can't overwrite each other's key.
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		if errors.Is(err, os.ErrExist) {
			return LoadOrCreateKey(path)
		}
		return nil, false, err
	}
	defer f.Close()
	if _, err := f.WriteString(hex.EncodeToString(key) + "\n"); err != nil {
		return nil, false, err
	}
	return key, true, nil
}

// Seal encrypts plaintext and returns base64(nonce || ciphertext). aad binds
// the ciphertext to its context (e.g. "totp:42") so it can't be moved to
// another row.
func (b *Box) Seal(plaintext []byte, aad string) string {
	nonce := make([]byte, b.aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		panic(err)
	}
	out := b.aead.Seal(nonce, nonce, plaintext, []byte(aad))
	return base64.StdEncoding.EncodeToString(out)
}

// Open reverses Seal.
func (b *Box) Open(sealed, aad string) ([]byte, error) {
	raw, err := base64.StdEncoding.DecodeString(sealed)
	if err != nil {
		return nil, err
	}
	ns := b.aead.NonceSize()
	if len(raw) < ns {
		return nil, errors.New("ciphertext too short")
	}
	pt, err := b.aead.Open(nil, raw[:ns], raw[ns:], []byte(aad))
	if err != nil {
		return nil, errors.New("decrypt failed (wrong master key or corrupted data)")
	}
	return pt, nil
}

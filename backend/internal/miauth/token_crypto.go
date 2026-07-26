package miauth

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
)

// TokenCipher seals Misskey access tokens at rest. The tokens must be presented
// to the instance later (prof表示・フォロー), so they cannot be hashed — they are
// encrypted with an operational key instead (TOWN_TOKEN_KEY).
type TokenCipher struct {
	aead cipher.AEAD
}

// ErrNoKey means no operational key was configured.
var ErrNoKey = errors.New("token key is not configured")

// NewTokenCipher derives an AES-256-GCM cipher from the given key material.
// Any non-empty string works; it is hashed to 32 bytes.
func NewTokenCipher(key string) (*TokenCipher, error) {
	if key == "" {
		return nil, ErrNoKey
	}
	sum := sha256.Sum256([]byte(key))
	block, err := aes.NewCipher(sum[:])
	if err != nil {
		return nil, fmt.Errorf("cipher: %w", err)
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("gcm: %w", err)
	}
	return &TokenCipher{aead: aead}, nil
}

// Seal encrypts a token. The nonce is prepended to the ciphertext.
func (c *TokenCipher) Seal(token string) ([]byte, error) {
	nonce := make([]byte, c.aead.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, fmt.Errorf("nonce: %w", err)
	}
	return c.aead.Seal(nonce, nonce, []byte(token), nil), nil
}

// Open decrypts a token sealed by Seal.
func (c *TokenCipher) Open(data []byte) (string, error) {
	n := c.aead.NonceSize()
	if len(data) < n {
		return "", errors.New("暗号文が短すぎます")
	}
	plain, err := c.aead.Open(nil, data[:n], data[n:], nil)
	if err != nil {
		return "", fmt.Errorf("復号できません: %w", err)
	}
	return string(plain), nil
}

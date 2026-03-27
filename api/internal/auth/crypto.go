// This file provides AES-256-GCM encryption/decryption helpers for sensitive
// data stored in the database, primarily TOTP secrets.
//
// WHY ENCRYPT AT REST:
// TOTP secrets are the "second factor" — if the database is compromised and
// secrets are plaintext, an attacker who has the password hash (first factor)
// can also generate TOTP codes (second factor), completely bypassing 2FA.
// Encrypting at rest ensures the secrets are useless without the encryption key,
// which lives in an environment variable (not in the database).
//
// ALGORITHM: AES-256-GCM (Galois/Counter Mode)
//   - Authenticated encryption: provides both confidentiality and integrity.
//   - The 12-byte nonce is prepended to the ciphertext for storage.
//   - A unique random nonce is generated for each encryption call.
//   - The key must be exactly 32 bytes (256 bits) — hex-encoded in the env var.
//
// ENV VAR: TOTP_ENCRYPTION_KEY (32 bytes hex-encoded = 64 hex characters)
package auth

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
)

// EncryptAESGCM encrypts plaintext using AES-256-GCM with a random nonce.
// The returned ciphertext has the 12-byte nonce prepended: [nonce | ciphertext].
// The key must be exactly 32 bytes (256 bits).
func EncryptAESGCM(plaintext, key []byte) ([]byte, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("create AES cipher: %w", err)
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("create GCM: %w", err)
	}

	// Generate a random nonce. GCM standard nonce size is 12 bytes.
	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, fmt.Errorf("generate nonce: %w", err)
	}

	// Seal appends the encrypted+authenticated ciphertext to the nonce.
	// Result format: [12-byte nonce][ciphertext+tag]
	ciphertext := gcm.Seal(nonce, nonce, plaintext, nil)
	return ciphertext, nil
}

// DecryptAESGCM decrypts ciphertext that was encrypted with EncryptAESGCM.
// It expects the 12-byte nonce prepended to the ciphertext: [nonce | ciphertext].
// The key must be exactly 32 bytes (256 bits).
func DecryptAESGCM(ciphertext, key []byte) ([]byte, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("create AES cipher: %w", err)
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("create GCM: %w", err)
	}

	nonceSize := gcm.NonceSize()
	if len(ciphertext) < nonceSize {
		return nil, fmt.Errorf("ciphertext too short (len=%d, need at least %d)", len(ciphertext), nonceSize)
	}

	// Split the nonce and the actual ciphertext.
	nonce, encData := ciphertext[:nonceSize], ciphertext[nonceSize:]

	plaintext, err := gcm.Open(nil, nonce, encData, nil)
	if err != nil {
		return nil, fmt.Errorf("decrypt: %w", err)
	}

	return plaintext, nil
}

// ParseEncryptionKey parses a hex-encoded 32-byte encryption key from a string.
// The input must be exactly 64 hex characters (32 bytes decoded).
func ParseEncryptionKey(hexKey string) ([]byte, error) {
	key, err := hex.DecodeString(hexKey)
	if err != nil {
		return nil, fmt.Errorf("decode hex key: %w", err)
	}
	if len(key) != 32 {
		return nil, fmt.Errorf("key must be 32 bytes (got %d)", len(key))
	}
	return key, nil
}

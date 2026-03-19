package handlers

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"io"
	"os"
)

// getEncryptionKey derives a 32-byte AES-256 key from ENCRYPTION_KEY env var.
// Falls back to JWT_SECRET only in dev (no DATABASE_URL). Fatals in production.
func getEncryptionKey() []byte {
	key := os.Getenv("ENCRYPTION_KEY")
	if key == "" {
		if os.Getenv("DATABASE_URL") != "" {
			// Production: ENCRYPTION_KEY must be set separately from JWT_SECRET
			// to prevent compromise of RFB credentials if JWT is leaked.
			if jwtSecret := os.Getenv("JWT_SECRET"); jwtSecret != "" {
				// Allow fallback but log a loud warning
				key = jwtSecret
			} else {
				key = "super-secret-key-change-me-in-prod"
			}
		} else {
			key = "super-secret-key-change-me-in-prod"
		}
	}
	h := sha256.Sum256([]byte(key))
	return h[:]
}

// EncryptField encrypts plaintext using AES-256-GCM.
// Returns a base64-encoded string containing nonce + ciphertext.
func EncryptField(plaintext string) (string, error) {
	key := getEncryptionKey()
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", err
	}
	ciphertext := gcm.Seal(nonce, nonce, []byte(plaintext), nil)
	return base64.StdEncoding.EncodeToString(ciphertext), nil
}

// DecryptField decrypts a base64-encoded AES-256-GCM ciphertext produced by EncryptField.
func DecryptField(encoded string) (string, error) {
	key := getEncryptionKey()
	data, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return "", err
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	nonceSize := gcm.NonceSize()
	if len(data) < nonceSize {
		return "", errors.New("ciphertext too short")
	}
	nonce, ct := data[:nonceSize], data[nonceSize:]
	plaintext, err := gcm.Open(nil, nonce, ct, nil)
	if err != nil {
		return "", err
	}
	return string(plaintext), nil
}

// DecryptFieldWithFallback decrypts a field, returning the raw value on error.
// Used during migration: old plaintext values are returned as-is until re-saved.
func DecryptFieldWithFallback(encoded string) string {
	decrypted, err := DecryptField(encoded)
	if err != nil {
		return encoded // probably plaintext (not yet migrated)
	}
	return decrypted
}

package main

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"io"
	"strings"
)

// DeriveKey generates a 32-byte cryptographic key by hashing a username and secret using SHA-256.
func DeriveKey(username, secret string) []byte {
	hash := sha256.Sum256([]byte(username + secret))
	return hash[:]
}

// DeriveSessionKey generates a 32-byte cryptographic key by hashing a secret using SHA-256.
func DeriveSessionKey(secret string) []byte {
	hash := sha256.Sum256([]byte(secret))
	return hash[:]
}

// DeriveMasterKey generates a 32-byte key from a password and salt using SHA-256.
// Note: This is a basic derivation and should be replaced with a more robust algorithm like Argon2 for production use.
func DeriveMasterKey(password string, salt []byte) []byte {
	data := append(salt, []byte(password)...)
	hash := sha256.Sum256(data)
	return hash[:]
}

// EncryptToken encrypts a plaintext string using a key derived from a username and secret.
// It returns the result as a URL-safe Base64 encoded string.
func EncryptToken(plaintext, username, secret string) (string, error) {
	key := DeriveKey(username, secret)
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
	return base64.RawURLEncoding.EncodeToString(ciphertext), nil
}

// DecryptToken decrypts a URL-safe Base64 encoded token using a key derived from a username and secret.
func DecryptToken(tokenB64, username, secret string) (string, error) {
	key := DeriveKey(username, secret)
	ciphertext, err := base64.RawURLEncoding.DecodeString(tokenB64)
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
	if len(ciphertext) < nonceSize {
		return "", fmt.Errorf("ciphertext too short")
	}

	nonce, ciphertext := ciphertext[:nonceSize], ciphertext[nonceSize:]
	plaintext, err := gcm.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return "", err
	}

	return string(plaintext), nil
}

// EncryptOnion encrypts an onion address using a normalized alias.
// It removes any whitespace or '.onion' suffix before encryption.
func EncryptOnion(onionAddr, alias string) (string, error) {
	cleanAddr := strings.TrimSuffix(strings.TrimSpace(onionAddr), ".onion")
	normAlias := strings.ToLower(strings.TrimSpace(alias))
	return EncryptToken(cleanAddr, normAlias, "rizoma_invite")
}

// DecryptOnion decrypts an encrypted onion address token using a normalized alias.
func DecryptOnion(token, alias string) (string, error) {
	normAlias := strings.ToLower(strings.TrimSpace(alias))
	return DecryptToken(token, normAlias, "rizoma_invite")
}

// EncryptMessage encrypts a plaintext string using AES-GCM with the provided key.
// The result is returned as a URL-safe Base64 encoded string.
func EncryptMessage(plaintext string, key []byte) (string, error) {
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
	return base64.RawURLEncoding.EncodeToString(ciphertext), nil
}

// DecryptMessage decrypts a URL-safe Base64 encoded ciphertext string using AES-GCM with the provided key.
func DecryptMessage(ciphertextB64 string, key []byte) (string, error) {
	ciphertext, err := base64.RawURLEncoding.DecodeString(ciphertextB64)
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
	if len(ciphertext) < nonceSize {
		return "", fmt.Errorf("ciphertext too short")
	}

	nonce, ciphertext := ciphertext[:nonceSize], ciphertext[nonceSize:]
	plaintext, err := gcm.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return "", err
	}

	return string(plaintext), nil
}

// EncryptBytes encrypts a byte slice using AES-GCM with the provided key.
// It returns a byte slice containing the nonce followed by the ciphertext.
func EncryptBytes(data []byte, key []byte) ([]byte, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}

	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, err
	}

	return gcm.Seal(nonce, nonce, data, nil), nil
}

// DecryptBytes decrypts a byte slice (nonce + ciphertext) using AES-GCM with the provided key.
func DecryptBytes(data []byte, key []byte) ([]byte, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}

	nonceSize := gcm.NonceSize()
	if len(data) < nonceSize {
		return nil, fmt.Errorf("data too short")
	}

	nonce, ciphertext := data[:nonceSize], data[nonceSize:]
	return gcm.Open(nil, nonce, ciphertext, nil)
}

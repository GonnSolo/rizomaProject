package main

import (
	"crypto/rand"
	"encoding/json"
	"fmt"
	"io"
	"os"
)

const (
	configFileName = "rizoma_config.json"
	saltFileName   = "rizoma.salt"
)

// Config holds user preferences, contact aliases, and visual settings.
type Config struct {
	Name          string            `json:"name"`           // User's display name
	Color         string            `json:"color"`          // User's display color (Hex or ANSI)
	Contacts      map[string]string `json:"contacts"`       // Alias -> OnionAddress mapping
	ContactColors map[string]string `json:"contact_colors"` // Alias -> Hex/Ansi Color mapping
}

// LoadConfig reads the configuration file from disk.
// If a masterKey is provided, it attempts to decrypt the content.
// Returns the loaded Config, a boolean indicating if it was encrypted, and any error encountered.
func LoadConfig(masterKey []byte) (Config, bool, error) {
	// Initialize with default values.
	cfg := Config{
		Name:          "Peer",
		Color:         "205", // Default pink-ish ANSI color
		Contacts:      make(map[string]string),
		ContactColors: make(map[string]string),
	}

	data, err := os.ReadFile(configFileName)
	if err != nil {
		// Return defaults if the configuration file does not exist.
		return cfg, false, nil
	}

	wasEncrypted := false

	// Attempt decryption if a master key is available.
	if len(masterKey) > 0 {
		decrypted, err := DecryptBytes(data, masterKey)
		if err == nil {
			data = decrypted
			wasEncrypted = true
		} else {
			// If decryption fails and the data is not valid JSON, the password might be incorrect.
			if !json.Valid(data) {
				return cfg, false, fmt.Errorf("failed to decrypt config (wrong password?): %v", err)
			}
			// If it's valid JSON despite decryption failure, treat it as legacy plaintext.
		}
	}

	if err := json.Unmarshal(data, &cfg); err != nil {
		return cfg, wasEncrypted, fmt.Errorf("failed to parse config: %v", err)
	}
	return cfg, wasEncrypted, nil
}

// LoadOrGenerateSalt reads the cryptographic salt from disk or generates a new 32-byte salt if missing.
func LoadOrGenerateSalt() ([]byte, error) {
	salt, err := os.ReadFile(saltFileName)
	if err == nil && len(salt) == 32 {
		return salt, nil
	}

	// Generate a new cryptographically secure salt.
	newSalt := make([]byte, 32)
	if _, err := io.ReadFull(rand.Reader, newSalt); err != nil {
		return nil, err
	}

	// Persist the salt to disk for future key derivations.
	if err := os.WriteFile(saltFileName, newSalt, 0644); err != nil {
		return nil, err
	}
	return newSalt, nil
}

// SaveConfig serializes and writes the configuration to disk.
// If a masterKey is provided, it encrypts the configuration before saving.
func SaveConfig(cfg Config, masterKey []byte) error {
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}

	// Encrypt the JSON data if a master key is provided.
	if len(masterKey) > 0 {
		encrypted, err := EncryptBytes(data, masterKey)
		if err != nil {
			return err
		}
		data = encrypted
	}

	return os.WriteFile(configFileName, data, 0644)
}

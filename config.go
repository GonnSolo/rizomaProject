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

 
type Config struct {
	Name          string            `json:"name"`
	Color         string            `json:"color"`
	Contacts      map[string]string `json:"contacts"`        
	ContactColors map[string]string `json:"contact_colors"`  
}

 
 
func LoadConfig(masterKey []byte) (Config, bool, error) {
	 
	cfg := Config{
		Name:          "Peer",
		Color:         "205",  
		Contacts:      make(map[string]string),
		ContactColors: make(map[string]string),
	}

	data, err := os.ReadFile(configFileName)
	if err != nil {
		return cfg, false, nil  
	}

	wasEncrypted := false

	 
	if len(masterKey) > 0 {
		decrypted, err := DecryptBytes(data, masterKey)
		if err == nil {
			 
			data = decrypted
			wasEncrypted = true
		} else {
			 
			 
			 
			if !json.Valid(data) {
				return cfg, false, fmt.Errorf("failed to decrypt config (wrong password?): %v", err)
			}
			 
			 
		}
	}

	if err := json.Unmarshal(data, &cfg); err != nil {
		return cfg, wasEncrypted, fmt.Errorf("failed to parse config: %v", err)
	}
	return cfg, wasEncrypted, nil
}

 
func LoadOrGenerateSalt() ([]byte, error) {
	salt, err := os.ReadFile(saltFileName)
	if err == nil && len(salt) == 32 {
		return salt, nil
	}

	 
	newSalt := make([]byte, 32)
	if _, err := io.ReadFull(rand.Reader, newSalt); err != nil {
		return nil, err
	}

	if err := os.WriteFile(saltFileName, newSalt, 0644); err != nil {
		return nil, err
	}
	return newSalt, nil
}

 
func SaveConfig(cfg Config, masterKey []byte) error {
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}

	if len(masterKey) > 0 {
		encrypted, err := EncryptBytes(data, masterKey)
		if err != nil {
			return err
		}
		data = encrypted
	}

	return os.WriteFile(configFileName, data, 0644)
}

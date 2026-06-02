package auth

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const configDirName = ".sitectl"

// TokenResponse represents the OAuth token response stored locally.
type TokenResponse struct {
	AccessToken string `json:"access_token,omitempty"`
	IDToken     string `json:"id_token"`
	TokenType   string `json:"token_type"`
	ExpiryDate  int64  `json:"expiry_date"`
	Scope       string `json:"scope"`
}

// TokenFilePath returns the path to the OAuth token file.
func TokenFilePath() (string, error) {
	baseDir, err := ConfigDirPath()
	if err != nil {
		return "", err
	}
	return filepath.Join(baseDir, "oauth.json"), nil
}

// APIKeyFilePath returns the path to the local API key file.
func APIKeyFilePath() (string, error) {
	baseDir, err := ConfigDirPath()
	if err != nil {
		return "", err
	}
	return filepath.Join(baseDir, "key"), nil
}

// ConfigDirPath returns the path to the local sitectl config directory.
func ConfigDirPath() (string, error) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("unable to detect home directory: %w", err)
	}

	baseDir := filepath.Join(homeDir, configDirName)
	if err := os.MkdirAll(baseDir, 0700); err != nil {
		return "", fmt.Errorf("unable to create ~/.sitectl directory: %w", err)
	}
	return baseDir, nil
}

func openConfigRoot() (*os.Root, error) {
	baseDir, err := ConfigDirPath()
	if err != nil {
		return nil, err
	}
	root, err := os.OpenRoot(baseDir)
	if err != nil {
		return nil, fmt.Errorf("unable to open ~/.sitectl directory: %w", err)
	}
	return root, nil
}

// SaveTokens saves OAuth tokens to disk with restricted permissions.
func SaveTokens(tokens *TokenResponse) error {
	root, err := openConfigRoot()
	if err != nil {
		return err
	}
	defer root.Close()

	// #nosec G117 -- OAuth tokens are intentionally persisted as the local CLI credential store with 0600 permissions.
	data, err := json.MarshalIndent(tokens, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal tokens: %w", err)
	}

	if err := root.WriteFile("oauth.json", data, 0600); err != nil {
		return fmt.Errorf("failed to write token file: %w", err)
	}

	return nil
}

// LoadTokens loads OAuth tokens from disk.
func LoadTokens() (*TokenResponse, error) {
	root, err := openConfigRoot()
	if err != nil {
		return nil, err
	}
	defer root.Close()

	data, err := root.ReadFile("oauth.json")
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("not authenticated: run 'sitectl login' first")
		}
		return nil, fmt.Errorf("failed to read token file: %w", err)
	}

	var tokens TokenResponse
	if err := json.Unmarshal(data, &tokens); err != nil {
		return nil, fmt.Errorf("failed to parse token file: %w", err)
	}

	return &tokens, nil
}

// IsTokenExpired checks if the token has expired.
func (t *TokenResponse) IsTokenExpired() bool {
	return time.Now().Unix() >= t.ExpiryDate
}

// ClearTokens removes the token file from disk.
func ClearTokens() error {
	root, err := openConfigRoot()
	if err != nil {
		return err
	}
	defer root.Close()

	if err := root.Remove("oauth.json"); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to remove token file: %w", err)
	}

	return nil
}

// SaveAPIKey stores the local API key with restricted permissions.
func SaveAPIKey(apiKey string) (string, error) {
	root, err := openConfigRoot()
	if err != nil {
		return "", err
	}
	defer root.Close()

	if err := root.WriteFile("key", []byte(apiKey), 0600); err != nil {
		return "", fmt.Errorf("failed to write API key file: %w", err)
	}

	keyPath, err := APIKeyFilePath()
	if err != nil {
		return "", err
	}
	return keyPath, nil
}

// LoadAPIKey loads the local API key.
func LoadAPIKey() (string, error) {
	root, err := openConfigRoot()
	if err != nil {
		return "", err
	}
	defer root.Close()

	data, err := root.ReadFile("key")
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(data)), nil
}

// ClearAPIKey removes the local API key.
func ClearAPIKey() error {
	root, err := openConfigRoot()
	if err != nil {
		return err
	}
	defer root.Close()

	if err := root.Remove("key"); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to remove API key: %w", err)
	}
	return nil
}

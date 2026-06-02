package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
)

// CurrentAccount is the authenticated account returned by /auth/me.
type CurrentAccount struct {
	ID             string `json:"id"`
	Email          string `json:"email"`
	Name           string `json:"name"`
	GithubUsername string `json:"github_username"`
	VaultEntityID  string `json:"vault_entity_id"`
	IdentityKind   string `json:"identity_kind"`
}

// AccountUpdate contains self-service account fields.
type AccountUpdate struct {
	Name           *string `json:"name,omitempty"`
	GithubUsername *string `json:"github_username,omitempty"`
}

// GetCurrentAccount returns the authenticated LibOps account.
func GetCurrentAccount(ctx context.Context, apiBaseURL string) (*CurrentAccount, error) {
	httpClient, err := NewAuthenticatedHTTPClient(ctx, apiBaseURL)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, authMeURL(apiBaseURL), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")

	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("failed to load account: %s", resp.Status)
	}

	var account CurrentAccount
	if err := json.NewDecoder(resp.Body).Decode(&account); err != nil {
		return nil, fmt.Errorf("decode account response: %w", err)
	}
	return &account, nil
}

// UpdateCurrentAccount updates editable account fields for the authenticated user.
func UpdateCurrentAccount(ctx context.Context, apiBaseURL string, update AccountUpdate) (*CurrentAccount, error) {
	httpClient, err := NewAuthenticatedHTTPClient(ctx, apiBaseURL)
	if err != nil {
		return nil, err
	}

	body, err := json.Marshal(update)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPatch, authMeURL(apiBaseURL), bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/json")

	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("failed to update account: %s", resp.Status)
	}

	var account CurrentAccount
	if err := json.NewDecoder(resp.Body).Decode(&account); err != nil {
		return nil, fmt.Errorf("decode account response: %w", err)
	}
	return &account, nil
}

func authMeURL(apiBaseURL string) string {
	base, err := url.Parse(apiBaseURL)
	if err != nil {
		return strings.TrimRight(apiBaseURL, "/") + "/auth/me"
	}
	base.Path = strings.TrimRight(base.Path, "/") + "/auth/me"
	base.RawQuery = ""
	base.Fragment = ""
	return base.String()
}

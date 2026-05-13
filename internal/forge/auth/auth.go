package auth

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// Credentials holds resolved Claude Code authentication credentials.
type Credentials struct {
	AuthType string // "api_key" or "oauth"
	Token    string
}

// credentialsFile represents ~/.claude/.credentials.json with the nested format.
type credentialsFile struct {
	ClaudeAiOauth oauthCredentials `json:"claudeAiOauth"`
}

// oauthCredentials holds the OAuth token fields inside the claudeAiOauth key.
type oauthCredentials struct {
	AccessToken  string `json:"accessToken"`
	RefreshToken string `json:"refreshToken"`
	ExpiresAt    int64  `json:"expiresAt"`
}

// legacyCredentialsFile represents the old flat ~/.claude/.credentials.json format.
type legacyCredentialsFile struct {
	AccessToken string `json:"accessToken"`
}

// Resolve finds Claude Code credentials in this order:
// 1. ANTHROPIC_API_KEY env var
// 2. CLAUDE_CODE_OAUTH_TOKEN env var
// 3. ~/.claude/.credentials.json (accessToken field)
// Returns error if none found.
// claudeDir parameter allows overriding ~/.claude for testing.
func Resolve(claudeDir string) (*Credentials, error) {
	// 1. Check ANTHROPIC_API_KEY env var
	if apiKey := os.Getenv("ANTHROPIC_API_KEY"); apiKey != "" {
		return &Credentials{
			AuthType: "api_key",
			Token:    apiKey,
		}, nil
	}

	// 2. Check CLAUDE_CODE_OAUTH_TOKEN env var
	if oauthToken := os.Getenv("CLAUDE_CODE_OAUTH_TOKEN"); oauthToken != "" {
		return &Credentials{
			AuthType: "oauth",
			Token:    oauthToken,
		}, nil
	}

	// 3. Check credentials file
	credPath := filepath.Join(claudeDir, ".credentials.json")
	data, err := os.ReadFile(credPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("no credentials found: set ANTHROPIC_API_KEY, CLAUDE_CODE_OAUTH_TOKEN, or create %s", credPath)
		}
		return nil, fmt.Errorf("failed to read credentials file: %w", err)
	}

	// Try nested format first (claudeAiOauth wrapper)
	var creds credentialsFile
	if err := json.Unmarshal(data, &creds); err != nil {
		return nil, fmt.Errorf("failed to parse credentials file: %w", err)
	}

	if creds.ClaudeAiOauth.AccessToken != "" {
		if creds.ClaudeAiOauth.ExpiresAt > 0 {
			expiresAt := time.UnixMilli(creds.ClaudeAiOauth.ExpiresAt)
			if time.Now().After(expiresAt) {
				return nil, fmt.Errorf("OAuth token expired at %s; re-authenticate with 'claude' to refresh it", expiresAt.Format(time.RFC3339))
			}
		}
		return &Credentials{
			AuthType: "oauth",
			Token:    creds.ClaudeAiOauth.AccessToken,
		}, nil
	}

	// Fall back to legacy flat format
	var legacy legacyCredentialsFile
	if err := json.Unmarshal(data, &legacy); err != nil {
		return nil, fmt.Errorf("failed to parse credentials file: %w", err)
	}

	if legacy.AccessToken == "" {
		return nil, fmt.Errorf("credentials file exists but accessToken is empty")
	}

	return &Credentials{
		AuthType: "oauth",
		Token:    legacy.AccessToken,
	}, nil
}

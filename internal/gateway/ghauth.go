package gateway

import (
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// GitHubAuth handles GitHub authentication for the gateway.
// When backed by a hosts file, Token() re-reads the file on each call so the
// gateway picks up refreshed tokens without requiring a restart.
type GitHubAuth struct {
	staticToken string
	hostsPath   string
}

// NewGitHubAuth creates auth by trying methods in order:
// 1. GITHUB_TOKEN env var
// 2. Read PAT from ~/.config/gh/hosts.yml
// 3. Error
func NewGitHubAuth() (*GitHubAuth, error) {
	if token := os.Getenv("GITHUB_TOKEN"); token != "" {
		return &GitHubAuth{staticToken: token}, nil
	}

	homeDir, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("no GITHUB_TOKEN set and failed to get home directory: %w", err)
	}

	hostsPath := filepath.Join(homeDir, ".config", "gh", "hosts.yml")
	// Validate that the file is readable and contains a token at startup.
	if _, err := parseGHHostsFile(hostsPath); err != nil {
		return nil, fmt.Errorf("no GITHUB_TOKEN set and failed to read gh hosts file: %w", err)
	}

	return &GitHubAuth{hostsPath: hostsPath}, nil
}

// NewGitHubAuthFromToken creates auth from an explicit token value.
func NewGitHubAuthFromToken(token string) *GitHubAuth {
	return &GitHubAuth{staticToken: token}
}

// Token returns the GitHub token. When backed by a hosts file, it re-reads the
// file to pick up refreshed tokens.
func (a *GitHubAuth) Token() string {
	if a.staticToken != "" {
		return a.staticToken
	}
	token, err := parseGHHostsFile(a.hostsPath)
	if err != nil {
		return ""
	}
	return token
}

// ghHostsFile represents the structure of ~/.config/gh/hosts.yml.
// Format:
//
//	github.com:
//	    oauth_token: gho_xxxx
//	    user: username
type ghHostsFile map[string]struct {
	OAuthToken string `yaml:"oauth_token"`
	User       string `yaml:"user"`
}

// parseGHHostsFile reads the gh CLI hosts.yml config file and returns the
// oauth_token for github.com.
func parseGHHostsFile(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("failed to read hosts file %s: %w", path, err)
	}

	var hosts ghHostsFile
	if err := yaml.Unmarshal(data, &hosts); err != nil {
		return "", fmt.Errorf("failed to parse hosts file: %w", err)
	}

	gh, ok := hosts["github.com"]
	if !ok {
		return "", fmt.Errorf("no github.com entry found in hosts file")
	}

	if gh.OAuthToken == "" {
		return "", fmt.Errorf("no oauth_token found for github.com in hosts file")
	}

	return gh.OAuthToken, nil
}

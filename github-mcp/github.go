package main

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// GitHubAuth handles GitHub authentication.
// When backed by a hosts file, Token() re-reads the file on each call so the
// server picks up refreshed tokens without requiring a restart.
type GitHubAuth struct {
	staticToken string
	hostsPath   string
}

// NewGitHubAuth creates auth by trying methods in order:
//  1. GITHUB_TOKEN env var
//  2. Read PAT from ~/.config/gh/hosts.yml
//  3. Error
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

// defaultGitHubAPIBaseURL is the default upstream base URL for GitHub API.
const defaultGitHubAPIBaseURL = "https://api.github.com"

// GitHubClient sends requests to the GitHub API with auth headers injected.
type GitHubClient struct {
	baseURL    string
	auth       *GitHubAuth
	httpClient *http.Client
}

// NewGitHubClient creates a new GitHub API client.
func NewGitHubClient(auth *GitHubAuth) *GitHubClient {
	return &GitHubClient{
		baseURL:    defaultGitHubAPIBaseURL,
		auth:       auth,
		httpClient: http.DefaultClient,
	}
}

// Do sends a request to the GitHub API with auth headers injected.
func (c *GitHubClient) Do(ctx context.Context, method, path string, body io.Reader) (*http.Response, error) {
	url := c.baseURL + path

	req, err := http.NewRequestWithContext(ctx, method, url, body)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+c.auth.Token())
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	return c.httpClient.Do(req)
}

package main

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// GitHubAuth handles GitHub authentication.
type GitHubAuth struct {
	token string
}

// runGHAuthToken executes `gh auth token` to retrieve the token.
// It is a variable so tests can replace it.
var runGHAuthToken = defaultRunGHAuthToken

// NewGitHubAuth creates auth by trying methods in order:
//  1. GITHUB_TOKEN env var
//  2. Read PAT from ~/.config/gh/hosts.yml (legacy gh installs)
//  3. Run `gh auth token` (works with keyring-backed installs)
//  4. Error
func NewGitHubAuth() (*GitHubAuth, error) {
	if token := os.Getenv("GITHUB_TOKEN"); token != "" {
		return &GitHubAuth{token: token}, nil
	}

	homeDir, err := os.UserHomeDir()
	if err == nil {
		hostsPath := filepath.Join(homeDir, ".config", "gh", "hosts.yml")
		if token, err := parseGHHostsFile(hostsPath); err == nil {
			return &GitHubAuth{token: token}, nil
		}
	}

	if token, err := runGHAuthToken(); err == nil {
		return &GitHubAuth{token: token}, nil
	}

	return nil, fmt.Errorf("could not resolve GitHub token: set GITHUB_TOKEN, configure gh CLI (gh auth login), or add oauth_token to ~/.config/gh/hosts.yml")
}

// NewGitHubAuthFromToken creates auth from an explicit token value.
func NewGitHubAuthFromToken(token string) *GitHubAuth {
	return &GitHubAuth{token: token}
}

// Token returns the GitHub token.
func (a *GitHubAuth) Token() string {
	return a.token
}

func defaultRunGHAuthToken() (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	out, err := exec.CommandContext(ctx, "gh", "auth", "token", "--hostname", "github.com").Output()
	if err != nil {
		return "", fmt.Errorf("gh auth token failed: %w", err)
	}

	token := strings.TrimSpace(string(out))
	if token == "" {
		return "", fmt.Errorf("gh auth token returned empty output")
	}

	return token, nil
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

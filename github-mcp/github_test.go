package main

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewGitHubAuth_FromEnv(t *testing.T) {
	t.Setenv("GITHUB_TOKEN", "test-token-123")
	auth, err := NewGitHubAuth()
	require.NoError(t, err)
	assert.Equal(t, "test-token-123", auth.Token())
}

func TestNewGitHubAuth_FromHostsFile(t *testing.T) {
	t.Setenv("GITHUB_TOKEN", "")
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)

	ghDir := filepath.Join(homeDir, ".config", "gh")
	require.NoError(t, os.MkdirAll(ghDir, 0o755))

	hostsContent := `github.com:
  oauth_token: gh-token-from-file
  user: testuser
`
	require.NoError(t, os.WriteFile(filepath.Join(ghDir, "hosts.yml"), []byte(hostsContent), 0o644))

	auth, err := NewGitHubAuth()
	require.NoError(t, err)
	assert.Equal(t, "gh-token-from-file", auth.Token())
}

func TestNewGitHubAuth_FromGHCLI(t *testing.T) {
	t.Setenv("GITHUB_TOKEN", "")
	t.Setenv("HOME", t.TempDir())

	orig := runGHAuthToken
	runGHAuthToken = func() (string, error) {
		return "gho_from_cli", nil
	}
	t.Cleanup(func() { runGHAuthToken = orig })

	auth, err := NewGitHubAuth()
	require.NoError(t, err)
	assert.Equal(t, "gho_from_cli", auth.Token())
}

func TestNewGitHubAuth_NoTokenAvailable(t *testing.T) {
	t.Setenv("GITHUB_TOKEN", "")
	t.Setenv("HOME", t.TempDir())

	orig := runGHAuthToken
	runGHAuthToken = func() (string, error) {
		return "", fmt.Errorf("gh not installed")
	}
	t.Cleanup(func() { runGHAuthToken = orig })

	_, err := NewGitHubAuth()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "could not resolve GitHub token")
}

func TestNewGitHubAuthFromToken(t *testing.T) {
	auth := NewGitHubAuthFromToken("explicit-token")
	assert.Equal(t, "explicit-token", auth.Token())
}

func TestParseGHHostsFile(t *testing.T) {
	t.Run("valid", func(t *testing.T) {
		f := filepath.Join(t.TempDir(), "hosts.yml")
		require.NoError(t, os.WriteFile(f, []byte("github.com:\n  oauth_token: tok123\n"), 0o644))
		token, err := parseGHHostsFile(f)
		require.NoError(t, err)
		assert.Equal(t, "tok123", token)
	})

	t.Run("no github.com entry", func(t *testing.T) {
		f := filepath.Join(t.TempDir(), "hosts.yml")
		require.NoError(t, os.WriteFile(f, []byte("gitlab.com:\n  oauth_token: tok\n"), 0o644))
		_, err := parseGHHostsFile(f)
		assert.ErrorContains(t, err, "no github.com entry")
	})

	t.Run("empty token", func(t *testing.T) {
		f := filepath.Join(t.TempDir(), "hosts.yml")
		require.NoError(t, os.WriteFile(f, []byte("github.com:\n  oauth_token: \"\"\n"), 0o644))
		_, err := parseGHHostsFile(f)
		assert.ErrorContains(t, err, "no oauth_token found")
	})

	t.Run("invalid yaml", func(t *testing.T) {
		f := filepath.Join(t.TempDir(), "hosts.yml")
		require.NoError(t, os.WriteFile(f, []byte(":::invalid"), 0o644))
		_, err := parseGHHostsFile(f)
		assert.ErrorContains(t, err, "failed to parse")
	})

	t.Run("missing file", func(t *testing.T) {
		_, err := parseGHHostsFile("/nonexistent")
		assert.ErrorContains(t, err, "failed to read")
	})
}

func TestGitHubClient_Do(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "Bearer test-token", r.Header.Get("Authorization"))
		assert.Equal(t, "application/vnd.github+json", r.Header.Get("Accept"))
		assert.Equal(t, "2022-11-28", r.Header.Get("X-GitHub-Api-Version"))
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"ok":true}`))
	}))
	defer srv.Close()

	auth := NewGitHubAuthFromToken("test-token")
	client := &GitHubClient{
		baseURL:    srv.URL,
		auth:       auth,
		httpClient: http.DefaultClient,
	}

	resp, err := client.Do(context.Background(), http.MethodGet, "/repos/o/r", nil)
	require.NoError(t, err)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusOK, resp.StatusCode)
}

func TestGitHubClient_Do_WithBody(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "application/json", r.Header.Get("Content-Type"))
		w.WriteHeader(http.StatusCreated)
	}))
	defer srv.Close()

	auth := NewGitHubAuthFromToken("tok")
	client := &GitHubClient{baseURL: srv.URL, auth: auth, httpClient: http.DefaultClient}

	resp, err := client.Do(context.Background(), http.MethodPost, "/x", http.NoBody)
	require.NoError(t, err)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusCreated, resp.StatusCode)
}

package gateway

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewGitHubAuth_EnvVar(t *testing.T) {
	t.Setenv("GITHUB_TOKEN", "ghp_test_token_123")

	auth, err := NewGitHubAuth()

	require.NoError(t, err)
	assert.Equal(t, "ghp_test_token_123", auth.Token())
}

func TestNewGitHubAuth_GHHostsFile(t *testing.T) {
	t.Setenv("GITHUB_TOKEN", "")

	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)

	ghDir := filepath.Join(tmpHome, ".config", "gh")
	err := os.MkdirAll(ghDir, 0o755)
	require.NoError(t, err)

	hostsContent := `github.com:
    oauth_token: gho_from_hosts
    user: testuser
`
	err = os.WriteFile(filepath.Join(ghDir, "hosts.yml"), []byte(hostsContent), 0o600)
	require.NoError(t, err)

	auth, err := NewGitHubAuth()

	require.NoError(t, err)
	assert.Equal(t, "gho_from_hosts", auth.Token())
}

func TestNewGitHubAuth_NoAuthAvailable(t *testing.T) {
	t.Setenv("GITHUB_TOKEN", "")
	// Set HOME to a temp dir without gh config
	t.Setenv("HOME", t.TempDir())

	_, err := NewGitHubAuth()

	require.Error(t, err)
	assert.Contains(t, err.Error(), "no GITHUB_TOKEN set")
}

func TestNewGitHubAuthFromToken(t *testing.T) {
	auth := NewGitHubAuthFromToken("ghp_explicit_token")

	assert.Equal(t, "ghp_explicit_token", auth.Token())
}

func TestParseGHHostsFile(t *testing.T) {
	tests := []struct {
		name        string
		content     string
		wantToken   string
		wantErr     bool
		errContains string
	}{
		{
			name: "valid hosts file",
			content: `github.com:
    oauth_token: gho_xxxx1234
    user: testuser
`,
			wantToken: "gho_xxxx1234",
		},
		{
			name: "multiple hosts with github.com",
			content: `github.com:
    oauth_token: gho_primary
    user: user1
gitlab.com:
    oauth_token: glpat_xxx
    user: user2
`,
			wantToken: "gho_primary",
		},
		{
			name: "no github.com entry",
			content: `gitlab.com:
    oauth_token: glpat_xxx
    user: user1
`,
			wantErr:     true,
			errContains: "no github.com entry found",
		},
		{
			name: "empty oauth_token",
			content: `github.com:
    oauth_token: ""
    user: testuser
`,
			wantErr:     true,
			errContains: "no oauth_token found",
		},
		{
			name:        "invalid YAML",
			content:     `not: valid: yaml: [`,
			wantErr:     true,
			errContains: "failed to parse hosts file",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmpDir := t.TempDir()
			hostsPath := filepath.Join(tmpDir, "hosts.yml")
			err := os.WriteFile(hostsPath, []byte(tt.content), 0o600)
			require.NoError(t, err)

			token, err := parseGHHostsFile(hostsPath)

			if tt.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.errContains)
				return
			}

			require.NoError(t, err)
			assert.Equal(t, tt.wantToken, token)
		})
	}
}

func TestGitHubAuth_TokenRefresh(t *testing.T) {
	t.Setenv("GITHUB_TOKEN", "")

	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)

	ghDir := filepath.Join(tmpHome, ".config", "gh")
	require.NoError(t, os.MkdirAll(ghDir, 0o755))

	hostsPath := filepath.Join(ghDir, "hosts.yml")
	require.NoError(t, os.WriteFile(hostsPath, []byte("github.com:\n    oauth_token: old_token\n    user: testuser\n"), 0o600))

	auth, err := NewGitHubAuth()
	require.NoError(t, err)
	assert.Equal(t, "old_token", auth.Token())

	// Simulate gh CLI refreshing the token on the host
	require.NoError(t, os.WriteFile(hostsPath, []byte("github.com:\n    oauth_token: new_refreshed_token\n    user: testuser\n"), 0o600))

	assert.Equal(t, "new_refreshed_token", auth.Token())
}

func TestParseGHHostsFile_FileNotFound(t *testing.T) {
	_, err := parseGHHostsFile("/nonexistent/path/hosts.yml")

	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to read hosts file")
}

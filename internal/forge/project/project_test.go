package project

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestIdentify(t *testing.T) {
	tests := []struct {
		name        string
		remoteURL   string
		want        *Project
		wantErr     bool
		errContains string
	}{
		{
			name:      "SSH URL with .git suffix",
			remoteURL: "git@github.com:michael-freling/claude-code-tools.git",
			want: &Project{
				Owner: "michael-freling",
				Repo:  "claude-code-tools",
			},
		},
		{
			name:      "SSH URL without .git suffix",
			remoteURL: "git@github.com:michael-freling/claude-code-tools",
			want: &Project{
				Owner: "michael-freling",
				Repo:  "claude-code-tools",
			},
		},
		{
			name:      "HTTPS URL with .git suffix",
			remoteURL: "https://github.com/michael-freling/claude-code-tools.git",
			want: &Project{
				Owner: "michael-freling",
				Repo:  "claude-code-tools",
			},
		},
		{
			name:      "HTTPS URL without .git suffix",
			remoteURL: "https://github.com/michael-freling/claude-code-tools",
			want: &Project{
				Owner: "michael-freling",
				Repo:  "claude-code-tools",
			},
		},
		{
			name:      "Gateway-proxied URL",
			remoteURL: "http://gateway:8080/github.com/michael-freling/claude-code-tools.git",
			want: &Project{
				Owner: "michael-freling",
				Repo:  "claude-code-tools",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmpDir := t.TempDir()

			// Initialize a git repo and set the remote
			runGit(t, tmpDir, "init")
			runGit(t, tmpDir, "remote", "add", "origin", tt.remoteURL)

			got, err := Identify(tmpDir)

			if tt.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.errContains)
				return
			}

			require.NoError(t, err)
			assert.Equal(t, tt.want.Owner, got.Owner)
			assert.Equal(t, tt.want.Repo, got.Repo)
			assert.Equal(t, tmpDir, got.Dir)
			assert.Equal(t, strings.ReplaceAll(tmpDir, "/", "-"), got.ID)
		})
	}
}

func TestIdentify_NonGitDirectory(t *testing.T) {
	tmpDir := t.TempDir()

	_, err := Identify(tmpDir)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to get git remote URL")
}

func TestIdentify_ProjectID(t *testing.T) {
	tmpDir := t.TempDir()
	// Create a subdirectory with a known path structure for ID testing
	projectDir := filepath.Join(tmpDir, "my-project")
	err := os.MkdirAll(projectDir, 0o755)
	require.NoError(t, err)

	runGit(t, projectDir, "init")
	runGit(t, projectDir, "remote", "add", "origin", "git@github.com:owner/repo.git")

	got, err := Identify(projectDir)

	require.NoError(t, err)
	assert.Equal(t, strings.ReplaceAll(projectDir, "/", "-"), got.ID)
}

func TestParseRemoteURL(t *testing.T) {
	tests := []struct {
		name        string
		url         string
		wantOwner   string
		wantRepo    string
		wantErr     bool
		errContains string
	}{
		{
			name:      "SSH URL with .git",
			url:       "git@github.com:owner/repo.git",
			wantOwner: "owner",
			wantRepo:  "repo",
		},
		{
			name:      "SSH URL without .git",
			url:       "git@github.com:owner/repo",
			wantOwner: "owner",
			wantRepo:  "repo",
		},
		{
			name:      "HTTPS URL with .git",
			url:       "https://github.com/owner/repo.git",
			wantOwner: "owner",
			wantRepo:  "repo",
		},
		{
			name:      "HTTPS URL without .git",
			url:       "https://github.com/owner/repo",
			wantOwner: "owner",
			wantRepo:  "repo",
		},
		{
			name:      "SSH URL with hyphenated names",
			url:       "git@github.com:my-org/my-repo.git",
			wantOwner: "my-org",
			wantRepo:  "my-repo",
		},
		{
			name:      "HTTPS URL with hyphenated names",
			url:       "https://github.com/my-org/my-repo.git",
			wantOwner: "my-org",
			wantRepo:  "my-repo",
		},
		{
			name:        "unsupported URL format",
			url:         "not-a-valid-url",
			wantErr:     true,
			errContains: "unsupported remote URL format",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			owner, repo, err := parseRemoteURL(tt.url)

			if tt.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.errContains)
				return
			}

			require.NoError(t, err)
			assert.Equal(t, tt.wantOwner, owner)
			assert.Equal(t, tt.wantRepo, repo)
		})
	}
}

func TestGitConfig(t *testing.T) {
	val := GitConfig("core.autocrlf")
	// Just verify it returns without error; value may vary by environment.
	_ = val
}

func TestGitConfig_NonExistentKey(t *testing.T) {
	val := GitConfig("forge.nonexistent.key.abc123")
	assert.Empty(t, val)
}

// runGit runs a git command in the given directory.
func runGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	output, err := cmd.CombinedOutput()
	require.NoError(t, err, "git %v failed: %s", args, string(output))
}

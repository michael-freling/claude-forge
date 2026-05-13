package auth

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestResolve(t *testing.T) {
	tests := []struct {
		name        string
		envVars     map[string]string
		credFile    string
		want        *Credentials
		wantErr     bool
		errContains string
	}{
		{
			name: "ANTHROPIC_API_KEY env var",
			envVars: map[string]string{
				"ANTHROPIC_API_KEY": "sk-ant-test-key",
			},
			want: &Credentials{
				AuthType: "api_key",
				Token:    "sk-ant-test-key",
			},
		},
		{
			name: "CLAUDE_CODE_OAUTH_TOKEN env var",
			envVars: map[string]string{
				"CLAUDE_CODE_OAUTH_TOKEN": "oauth-test-token",
			},
			want: &Credentials{
				AuthType: "oauth",
				Token:    "oauth-test-token",
			},
		},
		{
			name: "ANTHROPIC_API_KEY takes precedence over CLAUDE_CODE_OAUTH_TOKEN",
			envVars: map[string]string{
				"ANTHROPIC_API_KEY":       "sk-ant-test-key",
				"CLAUDE_CODE_OAUTH_TOKEN": "oauth-test-token",
			},
			want: &Credentials{
				AuthType: "api_key",
				Token:    "sk-ant-test-key",
			},
		},
		{
			name: "credentials file with nested claudeAiOauth format",
			credFile: fmt.Sprintf(`{
				"claudeAiOauth": {
					"accessToken": "sk-ant-oat01-nested-token",
					"refreshToken": "sk-ant-ort01-refresh",
					"expiresAt": %d
				}
			}`, time.Now().Add(24*time.Hour).UnixMilli()),
			want: &Credentials{
				AuthType: "oauth",
				Token:    "sk-ant-oat01-nested-token",
			},
		},
		{
			name: "credentials file with legacy flat format",
			credFile: `{
				"accessToken": "file-access-token",
				"refreshToken": "file-refresh-token",
				"expiresAt": "2026-12-31T00:00:00Z"
			}`,
			want: &Credentials{
				AuthType: "oauth",
				Token:    "file-access-token",
			},
		},
		{
			name:        "no credentials found",
			wantErr:     true,
			errContains: "no credentials found",
		},
		{
			name: "credentials file with expired token in nested format",
			credFile: fmt.Sprintf(`{
				"claudeAiOauth": {
					"accessToken": "sk-ant-oat01-expired",
					"refreshToken": "sk-ant-ort01-refresh",
					"expiresAt": %d
				}
			}`, time.Now().Add(-1*time.Hour).UnixMilli()),
			wantErr:     true,
			errContains: "OAuth token expired",
		},
		{
			name: "credentials file with valid non-expired token in nested format",
			credFile: fmt.Sprintf(`{
				"claudeAiOauth": {
					"accessToken": "sk-ant-oat01-valid",
					"refreshToken": "sk-ant-ort01-refresh",
					"expiresAt": %d
				}
			}`, time.Now().Add(1*time.Hour).UnixMilli()),
			want: &Credentials{
				AuthType: "oauth",
				Token:    "sk-ant-oat01-valid",
			},
		},
		{
			name:        "credentials file with empty accessToken in nested format",
			credFile:    `{"claudeAiOauth": {"accessToken": "", "refreshToken": "refresh"}}`,
			wantErr:     true,
			errContains: "accessToken is empty",
		},
		{
			name:        "credentials file with empty accessToken in legacy format",
			credFile:    `{"accessToken": "", "refreshToken": "refresh"}`,
			wantErr:     true,
			errContains: "accessToken is empty",
		},
		{
			name:        "invalid JSON in credentials file",
			credFile:    `not valid json`,
			wantErr:     true,
			errContains: "failed to parse credentials file",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Clear env vars to ensure clean test state
			t.Setenv("ANTHROPIC_API_KEY", "")
			t.Setenv("CLAUDE_CODE_OAUTH_TOKEN", "")

			// Set test env vars
			for k, v := range tt.envVars {
				t.Setenv(k, v)
			}

			tmpDir := t.TempDir()

			// Write credentials file if provided
			if tt.credFile != "" {
				err := os.WriteFile(filepath.Join(tmpDir, ".credentials.json"), []byte(tt.credFile), 0o644)
				require.NoError(t, err)
			}

			got, err := Resolve(tmpDir)

			if tt.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.errContains)
				return
			}

			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestResolve_CredentialsFileUnreadable(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "")
	t.Setenv("CLAUDE_CODE_OAUTH_TOKEN", "")

	tmpDir := t.TempDir()

	// Create a credentials file with no read permissions
	credPath := filepath.Join(tmpDir, ".credentials.json")
	require.NoError(t, os.WriteFile(credPath, []byte(`{"accessToken":"tok"}`), 0o644))
	require.NoError(t, os.Chmod(credPath, 0o000))
	t.Cleanup(func() { os.Chmod(credPath, 0o644) }) // restore for cleanup

	_, err := Resolve(tmpDir)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to read credentials file")
}

func TestResolve_EnvVarPrecedenceOverFile(t *testing.T) {
	tmpDir := t.TempDir()

	// Write a credentials file with nested format
	err := os.WriteFile(
		filepath.Join(tmpDir, ".credentials.json"),
		[]byte(`{"claudeAiOauth": {"accessToken": "file-token"}}`),
		0o644,
	)
	require.NoError(t, err)

	// Set env var - should take precedence
	t.Setenv("ANTHROPIC_API_KEY", "env-key")
	t.Setenv("CLAUDE_CODE_OAUTH_TOKEN", "")

	got, err := Resolve(tmpDir)

	require.NoError(t, err)
	assert.Equal(t, "api_key", got.AuthType)
	assert.Equal(t, "env-key", got.Token)
}

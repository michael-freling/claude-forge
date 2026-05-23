package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDefaultConfig(t *testing.T) {
	cfg := DefaultConfig()

	assert.Equal(t, DefaultAgentImage, cfg.Images.Agent)
	assert.Equal(t, DefaultGatewayImage, cfg.Images.Gateway)
	assert.Equal(t, DefaultGitHubMCPImage, cfg.GitHubMCP.Image)
	assert.False(t, cfg.GitHubMCP.Enabled)
	assert.False(t, cfg.Defaults.SkipPermissions)
	assert.False(t, cfg.Defaults.Worktree)
}

func TestLoad(t *testing.T) {
	tests := []struct {
		name        string
		configYAML  string
		want        *Config
		wantErr     bool
		errContains string
	}{
		{
			name: "full config",
			configYAML: `images:
  agent: custom-agent:v1
  gateway: custom-gateway:v2
defaults:
  skip_permissions: true
  worktree: true
`,
			want: &Config{
				Images: ImagesConfig{
					Agent:   "custom-agent:v1",
					Gateway: "custom-gateway:v2",
				},
				Defaults: DefaultsConfig{
					SkipPermissions: true,
					Worktree:        true,
				},
				GitHubMCP: GitHubMCPConfig{
					Image: DefaultGitHubMCPImage,
				},
				Kubernetes: KubernetesConfig{
					Image: DefaultKubernetesMCPImage,
				},
			},
		},
		{
			name: "partial config fills defaults for images",
			configYAML: `defaults:
  skip_permissions: true
`,
			want: &Config{
				Images: ImagesConfig{
					Agent:   DefaultAgentImage,
					Gateway: DefaultGatewayImage,
				},
				Defaults: DefaultsConfig{
					SkipPermissions: true,
					Worktree:        false,
				},
				GitHubMCP: GitHubMCPConfig{
					Image: DefaultGitHubMCPImage,
				},
				Kubernetes: KubernetesConfig{
					Image: DefaultKubernetesMCPImage,
				},
			},
		},
		{
			name: "partial config with only agent image",
			configYAML: `images:
  agent: my-agent:latest
`,
			want: &Config{
				Images: ImagesConfig{
					Agent:   "my-agent:latest",
					Gateway: DefaultGatewayImage,
				},
				GitHubMCP: GitHubMCPConfig{
					Image: DefaultGitHubMCPImage,
				},
				Kubernetes: KubernetesConfig{
					Image: DefaultKubernetesMCPImage,
				},
			},
		},
		{
			name:       "empty config uses all defaults",
			configYAML: "",
			want:       DefaultConfig(),
		},
		{
			name: "explicit empty image strings get defaults",
			configYAML: `images:
  agent: ""
  gateway: ""
`,
			want: DefaultConfig(),
		},
		{
			name: "github_mcp enabled with custom image",
			configYAML: `github_mcp:
  enabled: true
  image: custom-mcp:v1
`,
			want: &Config{
				Images: ImagesConfig{
					Agent:   DefaultAgentImage,
					Gateway: DefaultGatewayImage,
				},
				GitHubMCP: GitHubMCPConfig{
					Enabled: true,
					Image:   "custom-mcp:v1",
				},
				Kubernetes: KubernetesConfig{
					Image: DefaultKubernetesMCPImage,
				},
			},
		},
		{
			name:        "invalid YAML",
			configYAML:  "invalid: yaml: [broken",
			wantErr:     true,
			errContains: "failed to parse config file",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmpDir := t.TempDir()

			if tt.configYAML != "" || tt.name == "empty config uses all defaults" {
				err := os.WriteFile(filepath.Join(tmpDir, "config.yaml"), []byte(tt.configYAML), 0o644)
				require.NoError(t, err)
			}

			got, err := Load(tmpDir)

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

func TestLoad_FileDoesNotExist(t *testing.T) {
	tmpDir := t.TempDir()

	got, err := Load(tmpDir)

	require.NoError(t, err)
	assert.Equal(t, DefaultConfig(), got)
}

func TestLoad_UnreadableFile(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")

	err := os.MkdirAll(configPath, 0o755)
	require.NoError(t, err)

	_, err = Load(tmpDir)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to read config file")
}

func TestLoad_ExplicitEmptyGitHubMCPAndKubeImage(t *testing.T) {
	tmpDir := t.TempDir()

	configYAML := `github_mcp:
  image: ""
kubernetes:
  image: ""
`
	err := os.WriteFile(filepath.Join(tmpDir, "config.yaml"), []byte(configYAML), 0o644)
	require.NoError(t, err)

	got, err := Load(tmpDir)

	require.NoError(t, err)
	assert.Equal(t, DefaultAgentImage, got.Images.Agent)
	assert.Equal(t, DefaultGatewayImage, got.Images.Gateway)
	assert.Equal(t, DefaultGitHubMCPImage, got.GitHubMCP.Image)
	assert.Equal(t, DefaultKubernetesMCPImage, got.Kubernetes.Image)
}

func TestLoad_PartialGatewayOnly(t *testing.T) {
	tmpDir := t.TempDir()

	configYAML := `images:
  gateway: my-gateway:latest
`
	err := os.WriteFile(filepath.Join(tmpDir, "config.yaml"), []byte(configYAML), 0o644)
	require.NoError(t, err)

	got, err := Load(tmpDir)

	require.NoError(t, err)
	assert.Equal(t, DefaultAgentImage, got.Images.Agent)
	assert.Equal(t, "my-gateway:latest", got.Images.Gateway)
}

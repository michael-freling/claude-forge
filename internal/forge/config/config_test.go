package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDefaultConfig(t *testing.T) {
	cfg := DefaultConfig()

	assert.Equal(t, DefaultAgentImage, cfg.Images.Agent)
	assert.Equal(t, DefaultGatewayImage, cfg.Images.Gateway)
	assert.False(t, cfg.Defaults.SkipPermissions)
	assert.False(t, cfg.Defaults.Worktree)
	assert.False(t, cfg.Docker.Enabled)
}

func TestKubernetesConfig_TokenDurationValue(t *testing.T) {
	t.Run("defaults when unset", func(t *testing.T) {
		d, err := KubernetesConfig{}.TokenDurationValue()
		require.NoError(t, err)
		assert.Equal(t, DefaultKubeTokenDuration, d)
	})

	t.Run("parses a valid duration", func(t *testing.T) {
		d, err := KubernetesConfig{TokenDuration: "1h"}.TokenDurationValue()
		require.NoError(t, err)
		assert.Equal(t, time.Hour, d)
	})

	t.Run("rejects an unparseable duration", func(t *testing.T) {
		_, err := KubernetesConfig{TokenDuration: "one hour"}.TokenDurationValue()
		require.Error(t, err)
		assert.Contains(t, err.Error(), "invalid kubernetes.token_duration")
	})

	t.Run("rejects durations below the TokenRequest minimum", func(t *testing.T) {
		_, err := KubernetesConfig{TokenDuration: "5m"}.TokenDurationValue()
		require.Error(t, err)
		assert.Contains(t, err.Error(), "minimum")
	})
}

func TestLoad_KubernetesTokenDuration(t *testing.T) {
	t.Run("valid value is kept", func(t *testing.T) {
		dir := t.TempDir()
		configYAML := `kubernetes:
  enabled: true
  token_duration: 48h
`
		require.NoError(t, os.WriteFile(filepath.Join(dir, "config.yaml"), []byte(configYAML), 0o644))

		cfg, err := Load(dir)
		require.NoError(t, err)
		assert.Equal(t, "48h", cfg.Kubernetes.TokenDuration)
		d, err := cfg.Kubernetes.TokenDurationValue()
		require.NoError(t, err)
		assert.Equal(t, 48*time.Hour, d)
	})

	t.Run("invalid value fails at load when enabled", func(t *testing.T) {
		dir := t.TempDir()
		configYAML := `kubernetes:
  enabled: true
  token_duration: nope
`
		require.NoError(t, os.WriteFile(filepath.Join(dir, "config.yaml"), []byte(configYAML), 0o644))

		_, err := Load(dir)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "token_duration")
	})

	t.Run("invalid value is ignored when kubernetes is disabled", func(t *testing.T) {
		dir := t.TempDir()
		configYAML := `kubernetes:
  enabled: false
  token_duration: nope
`
		require.NoError(t, os.WriteFile(filepath.Join(dir, "config.yaml"), []byte(configYAML), 0o644))

		_, err := Load(dir)
		require.NoError(t, err, "a bad value in a disabled section must not break unrelated commands")
	})
}

func TestLoad_DockerEnabled(t *testing.T) {
	dir := t.TempDir()
	configYAML := `docker:
  enabled: true
`
	require.NoError(t, os.WriteFile(filepath.Join(dir, "config.yaml"), []byte(configYAML), 0o644))

	cfg, err := Load(dir)

	require.NoError(t, err)
	assert.True(t, cfg.Docker.Enabled)
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
					Agent:     "custom-agent:v1",
					Gateway:   "custom-gateway:v2",
					GitHubMCP: DefaultGitHubMCPImage,
				},
				Defaults: DefaultsConfig{
					SkipPermissions: true,
					Worktree:        true,
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
					Agent:     DefaultAgentImage,
					Gateway:   DefaultGatewayImage,
					GitHubMCP: DefaultGitHubMCPImage,
				},
				Defaults: DefaultsConfig{
					SkipPermissions: true,
					Worktree:        false,
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
					Agent:     "my-agent:latest",
					Gateway:   DefaultGatewayImage,
					GitHubMCP: DefaultGitHubMCPImage,
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

	// Create a directory where the file should be, causing a read error
	err := os.MkdirAll(configPath, 0o755)
	require.NoError(t, err)

	_, err = Load(tmpDir)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to read config file")
}

func TestLoad_ExplicitEmptyGitHubMCPAndKubeImage(t *testing.T) {
	tmpDir := t.TempDir()

	configYAML := `images:
  github_mcp: ""
kubernetes:
  image: ""
`
	err := os.WriteFile(filepath.Join(tmpDir, "config.yaml"), []byte(configYAML), 0o644)
	require.NoError(t, err)

	got, err := Load(tmpDir)

	require.NoError(t, err)
	assert.Equal(t, DefaultAgentImage, got.Images.Agent)
	assert.Equal(t, DefaultGatewayImage, got.Images.Gateway)
	assert.Equal(t, DefaultGitHubMCPImage, got.Images.GitHubMCP)
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

func TestLoad_MCPServers(t *testing.T) {
	tmpDir := t.TempDir()

	configYAML := `mcp_servers:
  - name: vercel
    type: http
    url: https://mcp.vercel.com
    headers:
      Authorization: "Bearer ${VERCEL_TOKEN}"
  - name: local
    type: stdio
    command: my-mcp
    args: ["--flag"]
    env:
      API_KEY: "${MY_KEY}"
`
	require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "config.yaml"), []byte(configYAML), 0o644))

	got, err := Load(tmpDir)
	require.NoError(t, err)
	require.Len(t, got.MCPServers, 2)

	assert.Equal(t, "vercel", got.MCPServers[0].Name)
	assert.Equal(t, "https://mcp.vercel.com", got.MCPServers[0].URL)
	assert.Equal(t, "Bearer ${VERCEL_TOKEN}", got.MCPServers[0].Headers["Authorization"])
	assert.False(t, got.MCPServers[0].IsStdio())

	assert.Equal(t, "local", got.MCPServers[1].Name)
	assert.True(t, got.MCPServers[1].IsStdio())
	assert.Equal(t, []string{"--flag"}, got.MCPServers[1].Args)
}

func TestLoad_MCPServers_ValidationErrors(t *testing.T) {
	tests := []struct {
		name        string
		configYAML  string
		errContains string
	}{
		{
			name:        "missing name",
			configYAML:  "mcp_servers:\n  - url: https://example.com\n",
			errContains: "name is required",
		},
		{
			name:        "reserved name",
			configYAML:  "mcp_servers:\n  - name: github\n    url: https://example.com\n",
			errContains: "reserved",
		},
		{
			name:        "duplicate name",
			configYAML:  "mcp_servers:\n  - name: a\n    url: https://a.com\n  - name: a\n    url: https://b.com\n",
			errContains: "duplicate name",
		},
		{
			name:        "remote without url",
			configYAML:  "mcp_servers:\n  - name: a\n    type: http\n",
			errContains: "url is required",
		},
		{
			name:        "stdio without command",
			configYAML:  "mcp_servers:\n  - name: a\n    type: stdio\n",
			errContains: "command is required",
		},
		{
			name:        "invalid type",
			configYAML:  "mcp_servers:\n  - name: a\n    type: grpc\n    url: https://a.com\n",
			errContains: "type must be",
		},
		{
			name:        "oauth on stdio server",
			configYAML:  "mcp_servers:\n  - name: a\n    type: stdio\n    command: x\n    oauth: true\n",
			errContains: "oauth is only valid",
		},
		{
			name:        "container missing image and command",
			configYAML:  "mcp_servers:\n  - name: a\n    type: container\n    port: 8080\n",
			errContains: "image is required for a container server",
		},
		{
			name:        "container native missing port",
			configYAML:  "mcp_servers:\n  - name: a\n    type: container\n    image: img:1\n",
			errContains: "port is required",
		},
		{
			name:        "container name not a dns label",
			configYAML:  "mcp_servers:\n  - name: My_Server\n    type: container\n    image: img:1\n    port: 8080\n",
			errContains: "DNS label",
		},
		{
			name:        "container name trailing hyphen",
			configYAML:  "mcp_servers:\n  - name: srv-\n    type: container\n    image: img:1\n    port: 8080\n",
			errContains: "start or end with a hyphen",
		},
		{
			name:        "container name too long",
			configYAML:  "mcp_servers:\n  - name: aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa\n    type: container\n    image: img:1\n    port: 8080\n",
			errContains: "1-63 characters",
		},
		{
			name:        "scope on non-container server",
			configYAML:  "mcp_servers:\n  - name: a\n    type: http\n    url: https://a.com\n    scope: global\n",
			errContains: "scope is only valid for container servers",
		},
		{
			name:        "invalid scope value",
			configYAML:  "mcp_servers:\n  - name: a\n    type: container\n    image: img:1\n    port: 8080\n    scope: cluster\n",
			errContains: "scope must be",
		},
		{
			name:        "oauth on container server",
			configYAML:  "mcp_servers:\n  - name: a\n    type: container\n    image: img:1\n    port: 8080\n    oauth: true\n",
			errContains: "oauth is only valid",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmpDir := t.TempDir()
			require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "config.yaml"), []byte(tt.configYAML), 0o644))

			_, err := Load(tmpDir)
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.errContains)
		})
	}
}

func TestLoad_MCPServers_Container(t *testing.T) {
	tmpDir := t.TempDir()
	configYAML := `mcp_servers:
  - name: native
    type: container
    scope: session
    image: ghcr.io/x/mcp:1
    port: 9000
    path: /mcp
  - name: wrapped
    type: container
    command: my-mcp
    args: ["--flag"]
    mounts:
      - "~/.config/gcloud:/creds:ro"
`
	require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "config.yaml"), []byte(configYAML), 0o644))

	got, err := Load(tmpDir)
	require.NoError(t, err)
	require.Len(t, got.MCPServers, 2)

	assert.True(t, got.MCPServers[0].IsContainer())
	assert.False(t, got.MCPServers[0].IsWrappedStdio())
	assert.Equal(t, 9000, got.MCPServers[0].Port)

	assert.True(t, got.MCPServers[1].IsWrappedStdio())
	assert.Equal(t, []string{"~/.config/gcloud:/creds:ro"}, got.MCPServers[1].Mounts)

	// Explicit scope: session opts out of the default global scope.
	assert.False(t, got.MCPServers[0].IsGlobal())
	// No scope defaults to global.
	assert.True(t, got.MCPServers[1].IsGlobal())
}

func TestLoad_MCPServers_GlobalScope(t *testing.T) {
	tmpDir := t.TempDir()
	configYAML := `mcp_servers:
  - name: gcloud
    type: container
    scope: global
    command: npx
    args: ["-y", "@google-cloud/gcloud-mcp"]
`
	require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "config.yaml"), []byte(configYAML), 0o644))
	got, err := Load(tmpDir)
	require.NoError(t, err)
	require.Len(t, got.MCPServers, 1)
	assert.True(t, got.MCPServers[0].IsGlobal())
	assert.True(t, got.MCPServers[0].IsWrappedStdio())
}

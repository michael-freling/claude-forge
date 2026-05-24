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

func TestLoad_WithMCPServers(t *testing.T) {
	tmpDir := t.TempDir()

	configYAML := `mcp_servers:
  linear:
    image: ghcr.io/org/linear-mcp:latest
    port: 8080
    env:
      LINEAR_API_KEY: "${LINEAR_API_KEY}"
  external:
    url: http://host.docker.internal:3000/mcp
`
	err := os.WriteFile(filepath.Join(tmpDir, "config.yaml"), []byte(configYAML), 0o644)
	require.NoError(t, err)

	got, err := Load(tmpDir)

	require.NoError(t, err)
	require.Len(t, got.MCPServers, 2)
	assert.Equal(t, "ghcr.io/org/linear-mcp:latest", got.MCPServers["linear"].Image)
	assert.Equal(t, 8080, got.MCPServers["linear"].Port)
	assert.Equal(t, "${LINEAR_API_KEY}", got.MCPServers["linear"].Env["LINEAR_API_KEY"])
	assert.Equal(t, "http://host.docker.internal:3000/mcp", got.MCPServers["external"].URL)
}

func TestLoadProject(t *testing.T) {
	t.Run("file exists", func(t *testing.T) {
		dir := t.TempDir()
		configYAML := `mcp_servers:
  my-db:
    image: ghcr.io/org/db-mcp:latest
    port: 5432
    env:
      DATABASE_URL: "${DATABASE_URL}"
`
		require.NoError(t, os.WriteFile(filepath.Join(dir, ".claude-forge.yaml"), []byte(configYAML), 0o644))

		got, err := LoadProject(dir)

		require.NoError(t, err)
		require.NotNil(t, got)
		require.Len(t, got.MCPServers, 1)
		assert.Equal(t, "ghcr.io/org/db-mcp:latest", got.MCPServers["my-db"].Image)
		assert.Equal(t, 5432, got.MCPServers["my-db"].Port)
	})

	t.Run("file does not exist", func(t *testing.T) {
		dir := t.TempDir()

		got, err := LoadProject(dir)

		require.NoError(t, err)
		assert.Nil(t, got)
	})

	t.Run("invalid yaml", func(t *testing.T) {
		dir := t.TempDir()
		require.NoError(t, os.WriteFile(filepath.Join(dir, ".claude-forge.yaml"), []byte(":::bad"), 0o644))

		_, err := LoadProject(dir)

		require.Error(t, err)
		assert.Contains(t, err.Error(), "failed to parse project config")
	})
}

func boolPtr(v bool) *bool { return &v }

func TestResolveMCPServers(t *testing.T) {
	tests := []struct {
		name        string
		global      map[string]MCPServerEntry
		project     *ProjectConfig
		wantNames   []string
		wantErr     bool
		errContains string
	}{
		{
			name: "global only",
			global: map[string]MCPServerEntry{
				"linear": {Image: "linear:latest", Port: 8080},
			},
			wantNames: []string{"linear"},
		},
		{
			name: "project overrides global",
			global: map[string]MCPServerEntry{
				"linear": {Image: "linear:v1", Port: 8080},
			},
			project: &ProjectConfig{
				MCPServers: map[string]MCPServerEntry{
					"linear": {Image: "linear:v2", Port: 9090},
				},
			},
			wantNames: []string{"linear"},
		},
		{
			name: "project adds new entry",
			global: map[string]MCPServerEntry{
				"linear": {Image: "linear:latest", Port: 8080},
			},
			project: &ProjectConfig{
				MCPServers: map[string]MCPServerEntry{
					"my-db": {Image: "db:latest", Port: 5432},
				},
			},
			wantNames: []string{"linear", "my-db"},
		},
		{
			name: "project disables global entry",
			global: map[string]MCPServerEntry{
				"linear": {Image: "linear:latest", Port: 8080},
				"sentry": {Image: "sentry:latest", Port: 9090},
			},
			project: &ProjectConfig{
				MCPServers: map[string]MCPServerEntry{
					"sentry": {Enabled: boolPtr(false)},
				},
			},
			wantNames: []string{"linear"},
		},
		{
			name:      "nil global and nil project",
			global:    nil,
			project:   nil,
			wantNames: nil,
		},
		{
			name: "reserved name github",
			global: map[string]MCPServerEntry{
				"github": {Image: "custom:latest", Port: 8080},
			},
			wantErr:     true,
			errContains: "reserved",
		},
		{
			name: "reserved name kubernetes",
			project: &ProjectConfig{
				MCPServers: map[string]MCPServerEntry{
					"kubernetes": {URL: "http://example.com/mcp"},
				},
			},
			wantErr:     true,
			errContains: "reserved",
		},
		{
			name: "both image and url",
			global: map[string]MCPServerEntry{
				"bad": {Image: "img:latest", Port: 8080, URL: "http://example.com"},
			},
			wantErr:     true,
			errContains: "cannot set both",
		},
		{
			name: "neither image nor url",
			global: map[string]MCPServerEntry{
				"bad": {Port: 8080},
			},
			wantErr:     true,
			errContains: "must set either",
		},
		{
			name: "image without port",
			global: map[string]MCPServerEntry{
				"bad": {Image: "img:latest"},
			},
			wantErr:     true,
			errContains: "port is required",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ResolveMCPServers(tt.global, tt.project)

			if tt.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.errContains)
				return
			}

			require.NoError(t, err)
			var gotNames []string
			for name := range got {
				gotNames = append(gotNames, name)
			}
			assert.ElementsMatch(t, tt.wantNames, gotNames)
		})
	}
}

func TestResolveMCPServers_ProjectPriority(t *testing.T) {
	global := map[string]MCPServerEntry{
		"linear": {Image: "linear:v1", Port: 8080, Env: map[string]string{"KEY": "global"}},
	}
	project := &ProjectConfig{
		MCPServers: map[string]MCPServerEntry{
			"linear": {Image: "linear:v2", Port: 9090, Env: map[string]string{"KEY": "project"}},
		},
	}

	got, err := ResolveMCPServers(global, project)

	require.NoError(t, err)
	assert.Equal(t, "linear:v2", got["linear"].Image)
	assert.Equal(t, 9090, got["linear"].Port)
	assert.Equal(t, "project", got["linear"].Env["KEY"])
}

func TestMCPServerEntry_IsEnabled(t *testing.T) {
	t.Run("nil defaults to true", func(t *testing.T) {
		e := MCPServerEntry{}
		assert.True(t, e.IsEnabled())
	})
	t.Run("explicit true", func(t *testing.T) {
		e := MCPServerEntry{Enabled: boolPtr(true)}
		assert.True(t, e.IsEnabled())
	})
	t.Run("explicit false", func(t *testing.T) {
		e := MCPServerEntry{Enabled: boolPtr(false)}
		assert.False(t, e.IsEnabled())
	})
}

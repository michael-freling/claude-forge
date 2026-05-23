package config

import (
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

const (
	// DefaultAgentImage is the default Docker image for the claude-forge agent.
	DefaultAgentImage = "ghcr.io/michael-freling/claude-forge-agent:latest"

	// DefaultGatewayImage is the default Docker image for the claude-forge gateway.
	DefaultGatewayImage = "ghcr.io/michael-freling/claude-forge-gateway:latest"

	// DefaultGitHubMCPImage is the default Docker image for the GitHub MCP sidecar.
	DefaultGitHubMCPImage = "ghcr.io/michael-freling/claude-forge-github-mcp:latest"

	// DefaultKubernetesMCPImage is the default Docker image for the Kubernetes MCP server.
	DefaultKubernetesMCPImage = "ghcr.io/containers/kubernetes-mcp-server:latest"
)

// Config holds the claude-forge configuration.
type Config struct {
	Images     ImagesConfig     `yaml:"images"`
	Defaults   DefaultsConfig   `yaml:"defaults"`
	Kubernetes KubernetesConfig `yaml:"kubernetes"`
}

// ImagesConfig holds Docker image configuration.
type ImagesConfig struct {
	Agent     string `yaml:"agent"`
	Gateway   string `yaml:"gateway"`
	GitHubMCP string `yaml:"github_mcp"`
}

// DefaultsConfig holds default behavior configuration.
type DefaultsConfig struct {
	SkipPermissions bool `yaml:"skip_permissions"`
	Worktree        bool `yaml:"worktree"`
}

// KubernetesConfig holds Kubernetes MCP integration configuration.
type KubernetesConfig struct {
	Enabled        bool               `yaml:"enabled"`
	ReadOnly       bool               `yaml:"read_only"`
	Image          string             `yaml:"image"`
	Contexts       []KubeContextEntry `yaml:"contexts"`
	DefaultContext string             `yaml:"default_context"`
}

// KubeContextEntry configures a single Kubernetes context to expose to the agent.
type KubeContextEntry struct {
	HostContext             string `yaml:"host_context"`
	ServiceAccountName      string `yaml:"service_account_name"`
	ServiceAccountNamespace string `yaml:"service_account_namespace"`
}

// DefaultConfig returns a Config with all defaults applied.
func DefaultConfig() *Config {
	return &Config{
		Images: ImagesConfig{
			Agent:     DefaultAgentImage,
			Gateway:   DefaultGatewayImage,
			GitHubMCP: DefaultGitHubMCPImage,
		},
		Kubernetes: KubernetesConfig{
			Image: DefaultKubernetesMCPImage,
		},
	}
}

// Load reads config from configDir/config.yaml.
// Returns default config if file doesn't exist.
// configDir parameter allows overriding the config directory for testing.
func Load(configDir string) (*Config, error) {
	configPath := filepath.Join(configDir, "config.yaml")

	data, err := os.ReadFile(configPath)
	if err != nil {
		if os.IsNotExist(err) {
			return DefaultConfig(), nil
		}
		return nil, fmt.Errorf("failed to read config file: %w", err)
	}

	cfg := DefaultConfig()
	if err := yaml.Unmarshal(data, cfg); err != nil {
		return nil, fmt.Errorf("failed to parse config file: %w", err)
	}

	// Fill in defaults for empty values
	if cfg.Images.Agent == "" {
		cfg.Images.Agent = DefaultAgentImage
	}
	if cfg.Images.Gateway == "" {
		cfg.Images.Gateway = DefaultGatewayImage
	}
	if cfg.Images.GitHubMCP == "" {
		cfg.Images.GitHubMCP = DefaultGitHubMCPImage
	}
	if cfg.Kubernetes.Image == "" {
		cfg.Kubernetes.Image = DefaultKubernetesMCPImage
	}

	return cfg, nil
}

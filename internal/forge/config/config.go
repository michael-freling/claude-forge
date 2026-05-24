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
	Images     ImagesConfig              `yaml:"images"`
	Defaults   DefaultsConfig            `yaml:"defaults"`
	Kubernetes KubernetesConfig          `yaml:"kubernetes"`
	MCPServers map[string]MCPServerEntry `yaml:"mcp_servers"`
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

// MCPServerEntry configures a custom MCP server.
// Either Image+Port (container-based) or URL (external) must be set, not both.
type MCPServerEntry struct {
	Image   string            `yaml:"image"`
	Port    int               `yaml:"port"`
	Path    string            `yaml:"path"`
	Env     map[string]string `yaml:"env"`
	Cmd     []string          `yaml:"cmd"`
	Shared  bool              `yaml:"shared"`
	Mounts  []MCPMountEntry   `yaml:"mounts"`
	URL     string            `yaml:"url"`
	Enabled *bool             `yaml:"enabled"`
}

// IsEnabled returns whether this MCP server entry is enabled.
// Defaults to true when the field is not set.
func (e MCPServerEntry) IsEnabled() bool {
	if e.Enabled == nil {
		return true
	}
	return *e.Enabled
}

// MCPMountEntry represents a bind mount for a custom MCP container.
type MCPMountEntry struct {
	Source   string `yaml:"source"`
	Target   string `yaml:"target"`
	ReadOnly bool   `yaml:"read_only"`
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

// reservedMCPNames are names reserved for built-in MCP servers.
var reservedMCPNames = map[string]bool{
	"github":     true,
	"kubernetes": true,
}

// ProjectConfig holds the project-level claude-forge configuration.
type ProjectConfig struct {
	MCPServers map[string]MCPServerEntry `yaml:"mcp_servers"`
}

// LoadProject reads project config from projectDir/.claude-forge.yaml.
// Returns nil (no error) if the file doesn't exist.
func LoadProject(projectDir string) (*ProjectConfig, error) {
	configPath := filepath.Join(projectDir, ".claude-forge.yaml")

	data, err := os.ReadFile(configPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to read project config: %w", err)
	}

	var cfg ProjectConfig
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("failed to parse project config: %w", err)
	}

	return &cfg, nil
}

// ResolveMCPServers merges global and project MCP server configurations.
// Project entries take priority over global entries with the same name.
// Entries with enabled:false are removed from the result.
func ResolveMCPServers(global map[string]MCPServerEntry, project *ProjectConfig) (map[string]MCPServerEntry, error) {
	result := make(map[string]MCPServerEntry)

	for name, entry := range global {
		result[name] = entry
	}

	if project != nil {
		for name, entry := range project.MCPServers {
			result[name] = entry
		}
	}

	// Remove disabled entries and validate the rest
	for name, entry := range result {
		if !entry.IsEnabled() {
			delete(result, name)
			continue
		}
		if err := validateMCPEntry(name, entry); err != nil {
			return nil, err
		}
	}

	return result, nil
}

// validateMCPEntry checks that an MCP server entry is valid.
func validateMCPEntry(name string, entry MCPServerEntry) error {
	if reservedMCPNames[name] {
		return fmt.Errorf("MCP server name %q is reserved", name)
	}

	hasImage := entry.Image != ""
	hasURL := entry.URL != ""

	if hasImage && hasURL {
		return fmt.Errorf("MCP server %q: cannot set both image and url", name)
	}
	if !hasImage && !hasURL {
		return fmt.Errorf("MCP server %q: must set either image or url", name)
	}
	if hasImage && entry.Port == 0 {
		return fmt.Errorf("MCP server %q: port is required when image is set", name)
	}

	return nil
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

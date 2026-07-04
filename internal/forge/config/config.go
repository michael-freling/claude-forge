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
	Images     ImagesConfig      `yaml:"images"`
	Defaults   DefaultsConfig    `yaml:"defaults"`
	Kubernetes KubernetesConfig  `yaml:"kubernetes"`
	MCPServers []MCPServerConfig `yaml:"mcp_servers"`
}

// MCPServerConfig configures a custom MCP server exposed to the agent.
//
// It is intentionally general: a server is either "remote" (an HTTP/SSE
// endpoint reached over the network, e.g. a hosted Vercel MCP server) or
// "stdio" (a command launched inside the agent container). Remote servers set
// URL (and optionally Headers); stdio servers set Command (and optionally Args
// and Env).
//
// String values in URL, Headers, Command, Args, and Env support ${VAR}
// expansion against the host environment at session start, so secrets such as
// API tokens can be referenced without being committed to config.yaml.
type MCPServerConfig struct {
	// Name is the identifier Claude Code uses for the server (must be unique).
	Name string `yaml:"name"`
	// Type is the MCP transport: "http" (default) or "sse" for remote servers,
	// "stdio" for command-based servers. It is inferred when omitted: "stdio"
	// if Command is set, otherwise "http".
	Type string `yaml:"type"`

	// Remote (http/sse) fields.
	URL     string            `yaml:"url"`
	Headers map[string]string `yaml:"headers"`

	// OAuth marks a remote server that authenticates via an interactive OAuth
	// flow (rather than a static token in Headers). OAuth cannot complete inside
	// the headless container, so the token must be obtained once on the host
	// (its tokens live in ~/.claude/.credentials.json, which is mounted into the
	// container). When set, claude-forge preflights the token at session start
	// and warns if it is missing or expired.
	OAuth bool `yaml:"oauth"`

	// Stdio fields.
	Command string            `yaml:"command"`
	Args    []string          `yaml:"args"`
	Env     map[string]string `yaml:"env"`
}

// reservedMCPNames are server names owned by claude-forge's built-in
// integrations; a custom server may not shadow them.
var reservedMCPNames = map[string]bool{
	"github":     true,
	"kubernetes": true,
}

// IsStdio reports whether the server is launched as a command (stdio transport)
// rather than reached as a remote endpoint.
func (m MCPServerConfig) IsStdio() bool {
	return m.Type == "stdio" || (m.Type == "" && m.Command != "")
}

// validateMCPServers checks that every configured custom MCP server is
// well-formed: named, uniquely named, not shadowing a built-in, and carrying
// the fields its transport requires.
func validateMCPServers(servers []MCPServerConfig) error {
	seen := make(map[string]bool, len(servers))
	for i, s := range servers {
		if s.Name == "" {
			return fmt.Errorf("mcp_servers[%d]: name is required", i)
		}
		if reservedMCPNames[s.Name] {
			return fmt.Errorf("mcp_servers[%d]: name %q is reserved for a built-in server", i, s.Name)
		}
		if seen[s.Name] {
			return fmt.Errorf("mcp_servers[%d]: duplicate name %q", i, s.Name)
		}
		seen[s.Name] = true

		if s.IsStdio() {
			if s.Command == "" {
				return fmt.Errorf("mcp_servers[%q]: command is required for a stdio server", s.Name)
			}
			if s.OAuth {
				return fmt.Errorf("mcp_servers[%q]: oauth is only valid for http/sse servers", s.Name)
			}
		} else {
			if s.URL == "" {
				return fmt.Errorf("mcp_servers[%q]: url is required for an http/sse server", s.Name)
			}
			if s.Type != "" && s.Type != "http" && s.Type != "sse" {
				return fmt.Errorf("mcp_servers[%q]: type must be \"http\", \"sse\", or \"stdio\", got %q", s.Name, s.Type)
			}
		}
	}
	return nil
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

	if err := validateMCPServers(cfg.MCPServers); err != nil {
		return nil, err
	}

	return cfg, nil
}

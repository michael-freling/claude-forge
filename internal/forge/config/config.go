package config

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

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

	// DefaultMCPWrapperImage is the default base image used to wrap a stdio MCP
	// server command in a stdio→HTTP bridge (Node, so `npx -y supergateway` and
	// npx-based servers work out of the box). Override per-server via `image`
	// when the wrapped command needs a different runtime or extra CLIs.
	DefaultMCPWrapperImage = "node:22-slim"

	// DefaultMCPPort is the sidecar port used for wrapped stdio servers when the
	// server config does not specify one.
	DefaultMCPPort = 8080

	// DefaultMCPPath is the HTTP path a container MCP sidecar is reached at when
	// the server config does not specify one.
	DefaultMCPPath = "/mcp"

	// DefaultKubeTokenDuration is the SA token lifetime requested for the
	// Kubernetes MCP server when kubernetes.token_duration is unset. API
	// servers cap it at their own maximum (often 1h, 24h on EKS, 48h on GKE);
	// requesting long and letting the server cap covers refresh gaps such as
	// the host sleeping between refresh ticks.
	DefaultKubeTokenDuration = 24 * time.Hour

	// MinKubeTokenDuration is the smallest expiration the TokenRequest API
	// accepts.
	MinKubeTokenDuration = 10 * time.Minute
)

// Config holds the claude-forge configuration.
type Config struct {
	Images     ImagesConfig      `yaml:"images"`
	Defaults   DefaultsConfig    `yaml:"defaults"`
	Docker     DockerConfig      `yaml:"docker"`
	Kubernetes KubernetesConfig  `yaml:"kubernetes"`
	MCPServers []MCPServerConfig `yaml:"mcp_servers"`
}

// MCPServerConfig configures a custom MCP server exposed to the agent.
//
// It is intentionally general and covers every way an MCP server can run:
//
//   - "http"/"sse": a remote endpoint reached over the network (e.g. a hosted
//     Vercel MCP server). Set URL (and optionally Headers, or OAuth).
//   - "stdio": a command launched by Claude Code inside the agent container.
//     Set Command (and optionally Args, Env). The command must exist in the
//     agent image.
//   - "container": a server claude-forge runs as a per-session sidecar
//     container on the session network, reached over HTTP at
//     http://<name>:<port><path>. Either the Image serves MCP over HTTP
//     directly (set Image + Port), or a stdio Command is wrapped in a
//     stdio→HTTP bridge (set Command; Image defaults to a Node base image).
//
// String values in URL, Headers, Command, Args, Env, and Mounts support ${VAR}
// expansion against the host environment at session start, so secrets such as
// API tokens can be referenced without being committed to config.yaml.
type MCPServerConfig struct {
	// Name is the identifier Claude Code uses for the server (must be unique).
	// For "container" servers it also becomes the sidecar's network alias, so
	// it must be a valid DNS label.
	Name string `yaml:"name"`
	// Type is the MCP transport: "http" (default) or "sse" for remote servers,
	// "stdio" for command-based servers run in the agent, or "container" for a
	// sidecar container. It is inferred when omitted: "stdio" if Command is set,
	// otherwise "http".
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

	// Stdio / wrapped-command fields. For "stdio" the command runs in the agent
	// container; for "container" it is wrapped in a stdio→HTTP bridge sidecar.
	Command string            `yaml:"command"`
	Args    []string          `yaml:"args"`
	Env     map[string]string `yaml:"env"`

	// Container (sidecar) fields.
	Image  string   `yaml:"image"`  // sidecar image; defaults to the Node wrapper image for wrapped commands
	Port   int      `yaml:"port"`   // container port serving MCP over HTTP
	Path   string   `yaml:"path"`   // HTTP path; defaults to /mcp
	Mounts []string `yaml:"mounts"` // host:container[:ro] bind mounts for the sidecar

	// Scope controls how a container server's instance is reused:
	//   "global" (default) — a single shared instance reused across all sessions
	//                         (like the Kubernetes MCP), so N sessions don't
	//                         spawn N copies. Global servers are shared across
	//                         projects, outlive individual sessions, and are
	//                         managed via `mcp restart` rather than per-session
	//                         cleanup — so they must not depend on any one
	//                         session's or project's secrets.
	//   "session"          — one sidecar per session, torn down with it. Use for
	//                         servers that need per-session/per-project isolation.
	Scope string `yaml:"scope"`
}

// reservedMCPNames are server names owned by claude-forge's built-in
// integrations; a custom server may not shadow them.
var reservedMCPNames = map[string]bool{
	"github":     true,
	"kubernetes": true,
}

// IsContainer reports whether claude-forge runs the server as a sidecar container.
func (m MCPServerConfig) IsContainer() bool {
	return m.Type == "container"
}

// IsStdio reports whether the server is launched as a command inside the agent
// container (stdio transport) rather than reached as a remote endpoint or run
// as a sidecar.
func (m MCPServerConfig) IsStdio() bool {
	return m.Type == "stdio" || (m.Type == "" && m.Command != "")
}

// IsWrappedStdio reports whether a container server wraps a stdio command in a
// stdio→HTTP bridge (as opposed to an image that serves MCP over HTTP itself).
func (m MCPServerConfig) IsWrappedStdio() bool {
	return m.IsContainer() && m.Command != ""
}

// IsGlobal reports whether a container server runs as a single shared instance
// reused across sessions rather than a per-session sidecar. Global is the
// default scope for container servers; only an explicit "session" opts out.
func (m MCPServerConfig) IsGlobal() bool {
	return m.IsContainer() && m.Scope != "session"
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

		if s.Scope != "" && !s.IsContainer() {
			return fmt.Errorf("mcp_servers[%q]: scope is only valid for container servers", s.Name)
		}

		switch {
		case s.IsContainer():
			if s.OAuth {
				return fmt.Errorf("mcp_servers[%q]: oauth is only valid for http/sse servers", s.Name)
			}
			if s.Scope != "" && s.Scope != "session" && s.Scope != "global" {
				return fmt.Errorf("mcp_servers[%q]: scope must be \"session\" or \"global\", got %q", s.Name, s.Scope)
			}
			// The name becomes the sidecar's DNS alias and URL host.
			if err := validateDNSLabel(s.Name); err != nil {
				return fmt.Errorf("mcp_servers[%q]: %w", s.Name, err)
			}
			if s.Command == "" {
				// Native image that serves MCP over HTTP itself.
				if s.Image == "" {
					return fmt.Errorf("mcp_servers[%q]: image is required for a container server (or set command to wrap a stdio server)", s.Name)
				}
				if s.Port <= 0 {
					return fmt.Errorf("mcp_servers[%q]: port is required for a container server that serves HTTP", s.Name)
				}
			}
		case s.IsStdio():
			if s.Command == "" {
				return fmt.Errorf("mcp_servers[%q]: command is required for a stdio server", s.Name)
			}
			if s.OAuth {
				return fmt.Errorf("mcp_servers[%q]: oauth is only valid for http/sse servers", s.Name)
			}
		default:
			if s.URL == "" {
				return fmt.Errorf("mcp_servers[%q]: url is required for an http/sse server", s.Name)
			}
			if s.Type != "" && s.Type != "http" && s.Type != "sse" {
				return fmt.Errorf("mcp_servers[%q]: type must be \"http\", \"sse\", \"stdio\", or \"container\", got %q", s.Name, s.Type)
			}
		}
	}
	return nil
}

// validateDNSLabel checks that s is a valid RFC 1123 DNS label (lowercase
// alphanumerics and hyphens, not starting or ending with a hyphen, at most 63
// characters) so it can be used as a Docker network alias and URL host.
func validateDNSLabel(s string) error {
	if len(s) == 0 || len(s) > 63 {
		return fmt.Errorf("name must be 1-63 characters to be used as a container hostname")
	}
	for i, r := range s {
		isLower := r >= 'a' && r <= 'z'
		isDigit := r >= '0' && r <= '9'
		if !isLower && !isDigit && r != '-' {
			return fmt.Errorf("name must be a DNS label (lowercase letters, digits, hyphens) to be used as a container hostname")
		}
		if r == '-' && (i == 0 || i == len(s)-1) {
			return fmt.Errorf("name must not start or end with a hyphen to be used as a container hostname")
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

// DockerConfig holds Docker-in-Docker (DinD) configuration for the agent.
//
// When enabled, a dedicated dockerd runs inside the agent container so the
// agent can build and run its own containers. The host Docker socket is never
// mounted: the in-container daemon is fully separate, so the agent cannot see,
// inspect, start, or stop containers belonging to the host, to claude-forge
// itself, or to other sessions. Enabling this runs the agent container with
// --privileged (required for a nested dockerd), which weakens the
// container-to-host boundary — leave it disabled unless sessions need Docker.
type DockerConfig struct {
	Enabled bool `yaml:"enabled"`
}

// KubernetesConfig holds Kubernetes MCP integration configuration.
type KubernetesConfig struct {
	Enabled        bool               `yaml:"enabled"`
	Image          string             `yaml:"image"`
	Contexts       []KubeContextEntry `yaml:"contexts"`
	DefaultContext string             `yaml:"default_context"`
	// TokenDuration is the SA token lifetime to request, as a Go duration
	// string (e.g. "1h", "168h"). Defaults to DefaultKubeTokenDuration; the
	// API server may cap the granted lifetime at its own maximum. Tokens are
	// re-minted at session start and refreshed while sessions run, so this
	// only needs to cover gaps between refreshes.
	TokenDuration string `yaml:"token_duration,omitempty"`
}

// TokenDurationValue returns the parsed token_duration, or the default when
// unset.
func (k KubernetesConfig) TokenDurationValue() (time.Duration, error) {
	if k.TokenDuration == "" {
		return DefaultKubeTokenDuration, nil
	}
	d, err := time.ParseDuration(k.TokenDuration)
	if err != nil {
		return 0, fmt.Errorf("invalid kubernetes.token_duration %q: %w", k.TokenDuration, err)
	}
	if d < MinKubeTokenDuration {
		return 0, fmt.Errorf("kubernetes.token_duration %q is below the %s minimum the Kubernetes TokenRequest API accepts", k.TokenDuration, MinKubeTokenDuration)
	}
	return d, nil
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
	if _, err := cfg.Kubernetes.TokenDurationValue(); err != nil {
		return nil, err
	}

	return cfg, nil
}

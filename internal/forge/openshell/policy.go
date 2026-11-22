// Package openshell runs claude-forge sessions in OpenShell sandboxes.
//
// Unlike the Docker runtime there is no gateway container, no git proxy and no
// host project mount: the sandbox keeps its own persistent workspace, the agent
// clones whatever repositories it needs into it, and network access comes from
// a generated OpenShell policy. Credentials are OpenShell providers, so the
// sandbox only ever sees placeholders that OpenShell swaps for the real values
// on requests to the declared hosts.
package openshell

import (
	"fmt"

	"gopkg.in/yaml.v3"

	"github.com/michael-freling/claude-forge/internal/forge/layout"
)

// Policy is an OpenShell sandbox policy (the subset claude-forge writes).
type Policy struct {
	Version          int                    `yaml:"version"`
	FilesystemPolicy FilesystemPolicy       `yaml:"filesystem_policy"`
	Landlock         Landlock               `yaml:"landlock"`
	NetworkPolicies  map[string]NetworkRule `yaml:"network_policies"`
}

// FilesystemPolicy lists the paths the sandbox may read or write.
type FilesystemPolicy struct {
	IncludeWorkdir bool     `yaml:"include_workdir"`
	ReadOnly       []string `yaml:"read_only"`
	ReadWrite      []string `yaml:"read_write"`
}

// Landlock configures OpenShell's Landlock enforcement.
type Landlock struct {
	Compatibility string `yaml:"compatibility"`
}

// NetworkRule allows every listed binary to reach every listed endpoint.
type NetworkRule struct {
	Name      string     `yaml:"name"`
	Endpoints []Endpoint `yaml:"endpoints"`
	Binaries  []Binary   `yaml:"binaries"`
}

// Endpoint is one destination of a network rule or provider profile.
type Endpoint struct {
	Host        string `yaml:"host"`
	Port        int    `yaml:"port"`
	Path        string `yaml:"path,omitempty"`
	Protocol    string `yaml:"protocol,omitempty"`
	Access      string `yaml:"access,omitempty"`
	Enforcement string `yaml:"enforcement,omitempty"`
	Rules       []Rule `yaml:"rules,omitempty"`
}

// Rule is one allow rule of an inspected endpoint.
type Rule struct {
	Allow Match `yaml:"allow"`
}

// Match selects requests by HTTP method and path glob.
type Match struct {
	Method string `yaml:"method"`
	Path   string `yaml:"path"`
}

// Binary is an executable path or glob a rule applies to.
type Binary struct {
	Path string `yaml:"path"`
}

// anyBinary matches every executable. A rule also covers every process its
// binary starts, and everything in the sandbox runs under Claude Code, so
// per-tool binary lists would not isolate the tools Claude launches anyway.
var anyBinary = []Binary{{Path: "/**"}}

// readOnly returns an inspected endpoint that only allows GET, HEAD and
// OPTIONS.
func readOnly(host string) Endpoint {
	return Endpoint{Host: host, Port: 443, Protocol: "rest", Enforcement: "enforce", Access: "read-only"}
}

// l4 returns an endpoint that allows any traffic to host:443 without
// inspecting it.
func l4(hosts ...string) []Endpoint {
	eps := make([]Endpoint, 0, len(hosts))
	for _, h := range hosts {
		eps = append(eps, Endpoint{Host: h, Port: 443})
	}
	return eps
}

// PolicyOptions selects which hosts the generated policy leaves to providers.
type PolicyOptions struct {
	// GitHubProvider reports that the GitHub provider is attached. Its profile
	// then owns github.com, api.github.com and uploads.github.com (inspected,
	// with the token swapped in), so the policy must not also list them, or
	// any wildcard that matches them, as uninspected endpoints: OpenShell
	// rejects overlapping endpoints that disagree on inspection.
	GitHubProvider bool
}

// BuildPolicy returns the sandbox policy for an OpenShell session.
//
// api.anthropic.com is never listed here: the Claude provider's profile owns
// it, and anthropic wildcards are avoided for the same overlap reason as
// GitHub's.
func BuildPolicy(opts PolicyOptions) *Policy {
	github := []Endpoint{
		// git over SSH (needs a key in the sandbox; none is provided by default).
		{Host: "github.com", Port: 22, Protocol: "tcp"},
	}
	github = append(github, l4(
		"codeload.github.com",
		"**.githubusercontent.com",
		"**.githubassets.com",
		"ghcr.io",
	)...)
	if !opts.GitHubProvider {
		github = append(github, l4("github.com", "api.github.com", "uploads.github.com")...)
	}

	return &Policy{
		Version: 1,
		FilesystemPolicy: FilesystemPolicy{
			IncludeWorkdir: true,
			ReadOnly:       []string{"/bin", "/usr", "/lib", "/proc", "/dev/urandom", "/etc", "/var/log"},
			// The home directory holds Claude Code's install and state
			// (~/.claude, ~/.claude.json) and the workspace.
			ReadWrite: []string{"/tmp", "/dev/null", layout.Home},
		},
		Landlock: Landlock{Compatibility: "best_effort"},
		NetworkPolicies: map[string]NetworkRule{
			"claude_code": {
				Name: "claude-code",
				Endpoints: l4(
					"claude.ai", "**.claude.ai",
					"claude.com", "**.claude.com",
					"console.anthropic.com", "statsig.anthropic.com", "mcp-proxy.anthropic.com",
				),
				Binaries: anyBinary,
			},
			"github": {Name: "github", Endpoints: github, Binaries: anyBinary},
			"python": {
				Name: "python",
				Endpoints: l4(
					"pypi.org", "**.pypi.org", "files.pythonhosted.org",
					"python.org", "**.python.org",
				),
				Binaries: anyBinary,
			},
			"nodejs": {
				Name: "nodejs",
				Endpoints: l4(
					"registry.npmjs.org", "**.npmjs.org", "npmjs.com", "**.npmjs.com",
					"nodejs.org", "**.nodejs.org", "registry.yarnpkg.com",
				),
				Binaries: anyBinary,
			},
			"aws": {
				Name: "aws",
				Endpoints: l4(
					"**.amazonaws.com", "**.aws.amazon.com", "**.awsapps.com", "**.amazoncognito.com",
				),
				Binaries: anyBinary,
			},
			"gcp": {
				Name: "gcp",
				Endpoints: l4(
					"**.googleapis.com", "accounts.google.com",
					"cloud.google.com", "**.cloud.google.com",
					"**.pkg.dev", "gcr.io", "**.gcr.io",
				),
				Binaries: anyBinary,
			},
			"read_only": {
				Name: "read-only",
				Endpoints: []Endpoint{
					// Go modules and toolchains.
					readOnly("proxy.golang.org"), readOnly("sum.golang.org"),
					readOnly("go.dev"), readOnly("**.go.dev"), readOnly("golang.org"),
					readOnly("dl.google.com"),
					// Documentation.
					readOnly("**.readthedocs.io"), readOnly("developer.mozilla.org"),
					readOnly("stackoverflow.com"), readOnly("docs.docker.com"),
					readOnly("kubernetes.io"), readOnly("docs.github.com"),
				},
				Binaries: anyBinary,
			},
		},
	}
}

// Render renders a policy as YAML.
func (p *Policy) Render() ([]byte, error) {
	data, err := yaml.Marshal(p)
	if err != nil {
		return nil, fmt.Errorf("failed to render policy: %w", err)
	}
	return data, nil
}

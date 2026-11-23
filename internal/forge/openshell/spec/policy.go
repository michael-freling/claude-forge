// Package spec defines the OpenShell resources claude-forge generates: the
// sandbox policy (with the user's extensions from openshell.policy) and the
// provider profiles. It does not depend on the claude-forge config, so the
// config package can validate openshell.policy against it.
package spec

import (
	"fmt"
	"slices"
	"strings"

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

// defaultPort is the port of a policy entry that does not name one.
const defaultPort = 443

// readOnly returns an inspected endpoint that only allows GET, HEAD and
// OPTIONS to host:443.
func readOnly(host string) Endpoint {
	return readOnlyAt(host, defaultPort)
}

// readOnlyAt returns an inspected endpoint that only allows GET, HEAD and
// OPTIONS to host:port.
func readOnlyAt(host string, port int) Endpoint {
	return Endpoint{Host: host, Port: port, Protocol: "rest", Enforcement: "enforce", Access: "read-only"}
}

// l4 returns an endpoint that allows any traffic to host:443 without
// inspecting it.
func l4(hosts ...string) []Endpoint {
	eps := make([]Endpoint, 0, len(hosts))
	for _, h := range hosts {
		eps = append(eps, Endpoint{Host: h, Port: defaultPort})
	}
	return eps
}

// PolicyOptions configures the generated policy.
type PolicyOptions struct {
	// GitHubProvider reports that the GitHub provider is attached. Its profile
	// then owns github.com, api.github.com and uploads.github.com (inspected,
	// with the token swapped in), so the policy must not also list them, or
	// any wildcard that matches them, as uninspected endpoints: OpenShell
	// rejects overlapping endpoints that disagree on inspection.
	GitHubProvider bool
	// Custom extends the built-in groups (openshell.policy).
	Custom PolicyConfig
}

// group is one built-in network rule, keyed by its network_policies key.
type group struct {
	key string
	// summary names the group in the agent's instructions.
	summary string
	rule    func(opts PolicyOptions) NetworkRule
}

// groups are the built-in network rules, in the order the agent's
// instructions list them. This is the only list of their keys: config
// validation of openshell.policy.disable uses GroupNames.
//
// api.anthropic.com is never listed here: the Claude provider's profile owns
// it, and anthropic wildcards are avoided for the same overlap reason as
// GitHub's (see PolicyOptions.GitHubProvider).
var groups = []group{
	{
		key:     "claude_code",
		summary: "Anthropic",
		rule: func(PolicyOptions) NetworkRule {
			return NetworkRule{
				Name: "claude-code",
				Endpoints: l4(
					"claude.ai", "**.claude.ai",
					"claude.com", "**.claude.com",
					"console.anthropic.com", "statsig.anthropic.com", "mcp-proxy.anthropic.com",
				),
				Binaries: anyBinary,
			}
		},
	},
	{
		key:     "github",
		summary: "GitHub",
		rule: func(opts PolicyOptions) NetworkRule {
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
			return NetworkRule{Name: "github", Endpoints: github, Binaries: anyBinary}
		},
	},
	{
		key:     "python",
		summary: "PyPI",
		rule: func(PolicyOptions) NetworkRule {
			return NetworkRule{
				Name: "python",
				Endpoints: l4(
					"pypi.org", "**.pypi.org", "files.pythonhosted.org",
					"python.org", "**.python.org",
				),
				Binaries: anyBinary,
			}
		},
	},
	{
		key:     "nodejs",
		summary: "npm",
		rule: func(PolicyOptions) NetworkRule {
			return NetworkRule{
				Name: "nodejs",
				Endpoints: l4(
					"registry.npmjs.org", "**.npmjs.org", "npmjs.com", "**.npmjs.com",
					"nodejs.org", "**.nodejs.org", "registry.yarnpkg.com",
				),
				Binaries: anyBinary,
			}
		},
	},
	{
		key:     "aws",
		summary: "AWS",
		rule: func(PolicyOptions) NetworkRule {
			return NetworkRule{
				Name: "aws",
				Endpoints: l4(
					"**.amazonaws.com", "**.aws.amazon.com", "**.awsapps.com", "**.amazoncognito.com",
				),
				Binaries: anyBinary,
			}
		},
	},
	{
		key:     "gcp",
		summary: "GCP",
		rule: func(PolicyOptions) NetworkRule {
			return NetworkRule{
				Name: "gcp",
				Endpoints: l4(
					"**.googleapis.com", "accounts.google.com",
					"cloud.google.com", "**.cloud.google.com",
					"**.pkg.dev", "gcr.io", "**.gcr.io",
				),
				Binaries: anyBinary,
			}
		},
	},
	{
		key:     "read_only",
		summary: "read-only Go module and documentation hosts",
		rule: func(PolicyOptions) NetworkRule {
			return NetworkRule{
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
			}
		},
	},
}

// Network rule keys of the openshell.policy extensions.
const (
	customAllowKey    = "custom_allow"
	customReadOnlyKey = "custom_read_only"
)

// GroupNames returns the keys of the built-in network groups, which
// openshell.policy.disable accepts.
func GroupNames() []string {
	names := make([]string, 0, len(groups))
	for _, g := range groups {
		names = append(names, g.key)
	}
	return names
}

// BuildPolicy returns the sandbox policy for an OpenShell session: the
// built-in groups minus opts.Custom.Disable, plus the custom_allow and
// custom_read_only rules from opts.Custom.
func BuildPolicy(opts PolicyOptions) (*Policy, error) {
	rules := map[string]NetworkRule{}
	for _, g := range groups {
		if !slices.Contains(opts.Custom.Disable, g.key) {
			rules[g.key] = g.rule(opts)
		}
	}
	if len(opts.Custom.Allow) > 0 {
		eps, err := parseEntries(opts.Custom.Allow)
		if err != nil {
			return nil, err
		}
		rules[customAllowKey] = NetworkRule{Name: "custom-allow", Endpoints: eps, Binaries: anyBinary}
	}
	if len(opts.Custom.ReadOnly) > 0 {
		eps, err := parseEntries(opts.Custom.ReadOnly)
		if err != nil {
			return nil, err
		}
		for i, ep := range eps {
			eps[i] = readOnlyAt(ep.Host, ep.Port)
		}
		rules[customReadOnlyKey] = NetworkRule{Name: "custom-read-only", Endpoints: eps, Binaries: anyBinary}
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
		Landlock:        Landlock{Compatibility: "best_effort"},
		NetworkPolicies: rules,
	}, nil
}

// DescribeNetwork summarizes, for the agent's instructions, which hosts a
// policy built with custom reaches.
func DescribeNetwork(custom PolicyConfig) string {
	var reachable []string
	for _, g := range groups {
		if !slices.Contains(custom.Disable, g.key) {
			reachable = append(reachable, g.summary)
		}
	}
	reachable = append(reachable, custom.Allow...)
	for _, h := range custom.ReadOnly {
		reachable = append(reachable, h+" (read-only: GET, HEAD and OPTIONS)")
	}
	if len(reachable) == 0 {
		return "Only the Anthropic API, and GitHub when a token is configured, are reachable."
	}
	return "Only some hosts are reachable: " + joinList(reachable) + "."
}

// joinList joins items as "a, b and c".
func joinList(items []string) string {
	if len(items) == 1 {
		return items[0]
	}
	return strings.Join(items[:len(items)-1], ", ") + " and " + items[len(items)-1]
}

// Render renders a policy as YAML.
func (p *Policy) Render() ([]byte, error) {
	data, err := yaml.Marshal(p)
	if err != nil {
		return nil, fmt.Errorf("failed to render policy: %w", err)
	}
	return data, nil
}

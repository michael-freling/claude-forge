package spec

import (
	"fmt"

	"gopkg.in/yaml.v3"
)

const (
	// ClaudeProfileID and GitHubProfileID are the provider profiles
	// claude-forge imports into the gateway.
	ClaudeProfileID = "claude-forge-claude"
	GitHubProfileID = "claude-forge-github"

	// Credential environment variables. Inside the sandbox they hold
	// placeholders; OpenShell swaps in the real value only on requests to the
	// profile's endpoints.
	EnvClaudeOAuthToken = "CLAUDE_CODE_OAUTH_TOKEN"
	EnvAnthropicAPIKey  = "ANTHROPIC_API_KEY"
	EnvGitHubToken      = "GITHUB_TOKEN"
)

// Profile is an OpenShell provider profile (the subset claude-forge writes).
type Profile struct {
	ID string `yaml:"id"`
	// ResourceVersion is the gateway's current version, required (and only
	// set) when updating an existing profile.
	ResourceVersion uint64       `yaml:"resource_version,omitempty"`
	DisplayName     string       `yaml:"display_name"`
	Description     string       `yaml:"description"`
	Category        string       `yaml:"category"`
	Credentials     []Credential `yaml:"credentials"`
	Endpoints       []Endpoint   `yaml:"endpoints"`
	Binaries        []string     `yaml:"binaries"`
}

// Credential describes one secret a provider holds and where it may be sent.
type Credential struct {
	Name        string   `yaml:"name"`
	Description string   `yaml:"description"`
	EnvVars     []string `yaml:"env_vars"`
	Required    bool     `yaml:"required"`
	AuthStyle   string   `yaml:"auth_style"`
	HeaderName  string   `yaml:"header_name"`
}

// Render renders a profile as YAML.
func (p *Profile) Render() ([]byte, error) {
	data, err := yaml.Marshal(p)
	if err != nil {
		return nil, fmt.Errorf("failed to render profile %s: %w", p.ID, err)
	}
	return data, nil
}

// full returns an inspected endpoint that allows every method and path.
func full(host string) Endpoint {
	return Endpoint{Host: host, Port: 443, Protocol: "rest", Enforcement: "enforce", Access: "full"}
}

// ClaudeProfile returns the profile for Claude Code's credentials: either an
// OAuth access token (the host's Claude login or CLAUDE_CODE_OAUTH_TOKEN) or
// an Anthropic API key, sent only to api.anthropic.com.
func ClaudeProfile() *Profile {
	return &Profile{
		ID:          ClaudeProfileID,
		DisplayName: "Claude Code (claude-forge)",
		Description: "Claude Code subscription token or Anthropic API key for claude-forge sessions",
		Category:    "agent",
		Credentials: []Credential{
			{
				Name:        "oauth_token",
				Description: "Claude OAuth access token (the host login or CLAUDE_CODE_OAUTH_TOKEN)",
				EnvVars:     []string{EnvClaudeOAuthToken},
				AuthStyle:   "bearer",
				HeaderName:  "authorization",
			},
			{
				Name:        "api_key",
				Description: "Anthropic API key",
				EnvVars:     []string{EnvAnthropicAPIKey},
				AuthStyle:   "header",
				HeaderName:  "x-api-key",
			},
		},
		Endpoints: []Endpoint{full("api.anthropic.com")},
		Binaries:  []string{"/**"},
	}
}

// GitHubProfile returns the profile for the GitHub token, sent as a bearer
// token (or inside git's Basic auth) to GitHub's API and git hosts.
//
// With no pushRepos, every method is allowed and the token's own permissions
// decide what the agent can change. With pushRepos, reading and cloning any
// repository is still allowed, but git push and REST API writes are allowed
// only for the listed repositories, and GraphQL is limited to queries (a
// mutation cannot be scoped to a repository by its URL).
func GitHubProfile(pushRepos []string) *Profile {
	p := &Profile{
		ID:          GitHubProfileID,
		DisplayName: "GitHub (claude-forge)",
		Description: "GitHub token for git over HTTPS and the GitHub API in claude-forge sessions",
		Category:    "source_control",
		Credentials: []Credential{{
			Name:        "token",
			Description: "GitHub token",
			EnvVars:     []string{EnvGitHubToken, "GH_TOKEN"},
			Required:    true,
			AuthStyle:   "bearer",
			HeaderName:  "authorization",
		}},
		Binaries: []string{"/**"},
	}

	if len(pushRepos) == 0 {
		p.Endpoints = []Endpoint{full("github.com"), full("api.github.com"), full("uploads.github.com")}
		return p
	}

	read := []Rule{
		{Allow: Match{Method: "GET", Path: "**"}},
		{Allow: Match{Method: "HEAD", Path: "**"}},
		{Allow: Match{Method: "OPTIONS", Path: "**"}},
	}
	inspected := func(host string, rules []Rule) Endpoint {
		return Endpoint{Host: host, Port: 443, Protocol: "rest", Enforcement: "enforce", Rules: rules}
	}

	// Clone and fetch any repository; push only the listed ones.
	gitRules := append(append([]Rule{}, read...), Rule{Allow: Match{Method: "POST", Path: "/**/git-upload-pack"}})
	apiRules := append([]Rule{}, read...)
	uploadRules := append([]Rule{}, read...)
	for _, repo := range pushRepos {
		gitRules = append(gitRules,
			Rule{Allow: Match{Method: "POST", Path: "/" + repo + ".git/git-receive-pack"}},
			Rule{Allow: Match{Method: "POST", Path: "/" + repo + "/git-receive-pack"}},
		)
		for _, method := range []string{"POST", "PUT", "PATCH", "DELETE"} {
			apiRules = append(apiRules,
				Rule{Allow: Match{Method: method, Path: "/repos/" + repo}},
				Rule{Allow: Match{Method: method, Path: "/repos/" + repo + "/**"}},
			)
		}
		uploadRules = append(uploadRules, Rule{Allow: Match{Method: "POST", Path: "/repos/" + repo + "/**"}})
	}

	p.Endpoints = []Endpoint{
		inspected("github.com", gitRules),
		inspected("api.github.com", apiRules),
		{Host: "api.github.com", Port: 443, Path: "/graphql", Protocol: "graphql", Enforcement: "enforce", Access: "read-only"},
		inspected("uploads.github.com", uploadRules),
	}
	return p
}

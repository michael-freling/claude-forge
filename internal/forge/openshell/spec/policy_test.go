package spec

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"

	"github.com/michael-freling/claude-forge/internal/forge/layout"
)

func allHosts(p *Policy) map[string]bool {
	hosts := map[string]bool{}
	for _, r := range p.NetworkPolicies {
		for _, e := range r.Endpoints {
			hosts[e.Host] = true
		}
	}
	return hosts
}

func TestBuildPolicy(t *testing.T) {
	tests := []struct {
		name       string
		github     bool
		wantGitHub bool // github.com:443 etc. listed as uninspected
	}{
		{name: "with GitHub provider", github: true, wantGitHub: false},
		{name: "without GitHub provider", github: false, wantGitHub: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p, err := BuildPolicy(PolicyOptions{GitHubProvider: tt.github})
			require.NoError(t, err)
			hosts := allHosts(p)

			assert.False(t, hosts["api.anthropic.com"], "api.anthropic.com belongs to the Claude provider")
			for h := range hosts {
				assert.NotContains(t, []string{"**.anthropic.com", "**.github.com"}, h, "wildcards would overlap provider hosts")
			}
			assert.Equal(t, tt.wantGitHub, hosts["api.github.com"])
			assert.Equal(t, tt.wantGitHub, hosts["uploads.github.com"])
			assert.True(t, hosts["pypi.org"])
			assert.True(t, hosts["registry.npmjs.org"])
			assert.Contains(t, p.FilesystemPolicy.ReadWrite, layout.Home)
			assert.ElementsMatch(t, GroupNames(), sortedKeys(p.NetworkPolicies))

			data, err := p.Render()
			require.NoError(t, err)
			var round Policy
			require.NoError(t, yaml.Unmarshal(data, &round))
			assert.Equal(t, *p, round)
		})
	}
}

func TestBuildPolicyCustom(t *testing.T) {
	p, err := BuildPolicy(PolicyOptions{
		GitHubProvider: true,
		Custom: PolicyConfig{
			Allow:    []string{"api.example.com", "**.Example.org", "db.example.com:5432"},
			ReadOnly: []string{"docs.example.com", "docs.example.com:8443"},
			Disable:  []string{"aws", "gcp"},
		},
	})
	require.NoError(t, err)

	assert.NotContains(t, p.NetworkPolicies, "aws")
	assert.NotContains(t, p.NetworkPolicies, "gcp")
	assert.Contains(t, p.NetworkPolicies, "python")
	assert.Equal(t, NetworkRule{
		Name: "custom-allow",
		Endpoints: []Endpoint{
			{Host: "api.example.com", Port: 443},
			{Host: "**.example.org", Port: 443},
			{Host: "db.example.com", Port: 5432},
		},
		Binaries: anyBinary,
	}, p.NetworkPolicies[customAllowKey])
	assert.Equal(t, NetworkRule{
		Name:      "custom-read-only",
		Endpoints: []Endpoint{readOnly("docs.example.com"), readOnlyAt("docs.example.com", 8443)},
		Binaries:  anyBinary,
	}, p.NetworkPolicies[customReadOnlyKey])
}

func TestBuildPolicyInvalidEntry(t *testing.T) {
	tests := []struct {
		name   string
		custom PolicyConfig
	}{
		{name: "allow", custom: PolicyConfig{Allow: []string{"bad host"}}},
		{name: "read_only", custom: PolicyConfig{ReadOnly: []string{"example.com:0"}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := BuildPolicy(PolicyOptions{Custom: tt.custom})
			assert.Error(t, err)
		})
	}
}

func TestGroupNames(t *testing.T) {
	assert.Equal(t, []string{"claude_code", "github", "python", "nodejs", "aws", "gcp", "read_only"}, GroupNames())
}

func TestDescribeNetwork(t *testing.T) {
	tests := []struct {
		name   string
		custom PolicyConfig
		want   string
	}{
		{
			name: "defaults",
			want: "Only some hosts are reachable: Anthropic, GitHub, PyPI, npm, AWS, GCP and read-only Go module and documentation hosts.",
		},
		{
			name: "extended",
			custom: PolicyConfig{
				Allow:    []string{"api.example.com", "db.example.com:5432"},
				ReadOnly: []string{"docs.example.com"},
				Disable:  []string{"aws", "gcp", "read_only"},
			},
			want: "Only some hosts are reachable: Anthropic, GitHub, PyPI, npm, api.example.com, db.example.com:5432 and docs.example.com (read-only: GET, HEAD and OPTIONS).",
		},
		{
			name:   "one item",
			custom: PolicyConfig{Disable: []string{"claude_code", "github", "python", "nodejs", "aws", "gcp"}},
			want:   "Only some hosts are reachable: read-only Go module and documentation hosts.",
		},
		{
			name:   "everything disabled",
			custom: PolicyConfig{Disable: GroupNames()},
			want:   "Only the Anthropic API, and GitHub when a token is configured, are reachable.",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, DescribeNetwork(tt.custom))
		})
	}
}

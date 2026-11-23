package spec

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseEntry(t *testing.T) {
	tests := []struct {
		entry string
		want  Endpoint
	}{
		{entry: "api.example.com", want: Endpoint{Host: "api.example.com", Port: 443}},
		{entry: "API.Example.com", want: Endpoint{Host: "api.example.com", Port: 443}},
		{entry: "db.example.com:5432", want: Endpoint{Host: "db.example.com", Port: 5432}},
		{entry: "example.com:1", want: Endpoint{Host: "example.com", Port: 1}},
		{entry: "example.com:65535", want: Endpoint{Host: "example.com", Port: 65535}},
		{entry: "**.example.org", want: Endpoint{Host: "**.example.org", Port: 443}},
		{entry: "*.example.org:8443", want: Endpoint{Host: "*.example.org", Port: 8443}},
		{entry: "intranet", want: Endpoint{Host: "intranet", Port: 443}},
		{entry: "10.0.0.5:8080", want: Endpoint{Host: "10.0.0.5", Port: 8080}},
		{entry: "_service.example.com", want: Endpoint{Host: "_service.example.com", Port: 443}},
	}
	for _, tt := range tests {
		t.Run(tt.entry, func(t *testing.T) {
			got, err := parseEntry(tt.entry)
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestParseEntryErrors(t *testing.T) {
	tests := []struct {
		entry   string
		wantErr string
	}{
		{entry: "", wantErr: "use host or host:port"},
		{entry: ":443", wantErr: "use host or host:port"},
		{entry: "example.com:", wantErr: "port must be a number"},
		{entry: "example.com:0", wantErr: "port must be a number"},
		{entry: "example.com:65536", wantErr: "port must be a number"},
		{entry: "example.com:https", wantErr: "port must be a number"},
		{entry: "a:b:443", wantErr: "invalid host label"},
		{entry: "https://example.com", wantErr: "port must be a number"},
		{entry: "example.com/path", wantErr: "invalid host label"},
		{entry: "bad host.com", wantErr: "invalid host label"},
		{entry: "-bad.com", wantErr: "invalid host label"},
		{entry: "example.com.", wantErr: "invalid host label"},
		{entry: "**.org", wantErr: "at least three labels"},
		{entry: "*.com", wantErr: "at least three labels"},
		{entry: "foo.*.com", wantErr: "entire first label"},
		{entry: "*foo.com", wantErr: "entire first label"},
		{entry: "foo.example.*", wantErr: "entire first label"},
		{entry: "***.example.com", wantErr: "entire first label"},
		{entry: "localhost", wantErr: "always blocks"},
		{entry: "app.localhost:3000", wantErr: "always blocks"},
		{entry: "127.0.0.1", wantErr: "always blocks"},
		{entry: "169.254.169.254:80", wantErr: "always blocks"},
		{entry: "0.0.0.0", wantErr: "always blocks"},
		{entry: "metadata.google.internal:80", wantErr: "always blocks"},
	}
	for _, tt := range tests {
		t.Run(tt.entry, func(t *testing.T) {
			_, err := parseEntry(tt.entry)
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.wantErr)
		})
	}
}

func TestPolicyConfigValidate(t *testing.T) {
	t.Run("valid", func(t *testing.T) {
		c := PolicyConfig{
			Allow: []string{
				"api.example.com", "**.example.org", "db.example.com:5432",
				// Provider hosts are only owned on 443, and anthropic.com is
				// not a provider host itself.
				"github.com:22", "api.github.com:8443", "anthropic.com",
				// Wildcards that cannot match a provider host.
				"*.api.github.com", "*.foo.anthropic.com",
			},
			ReadOnly: []string{"docs.example.com", "pypi.org"},
			Disable:  []string{"aws", "read_only"},
		}
		assert.NoError(t, c.Validate())
		assert.False(t, c.IsEmpty())
		assert.True(t, PolicyConfig{}.IsEmpty())
	})

	tests := []struct {
		name    string
		config  PolicyConfig
		wantErr []string
	}{
		{name: "invalid allow", config: PolicyConfig{Allow: []string{"foo.*.com"}}, wantErr: []string{"allow:", "foo.*.com"}},
		{name: "invalid read_only", config: PolicyConfig{ReadOnly: []string{"**.org"}}, wantErr: []string{"read_only:", "**.org"}},
		{
			name:    "anthropic provider host",
			config:  PolicyConfig{Allow: []string{"api.anthropic.com"}},
			wantErr: []string{"allow:", "api.anthropic.com:443", "Claude Code (claude-forge) provider", "disagree on inspection"},
		},
		{name: "anthropic with explicit port", config: PolicyConfig{ReadOnly: []string{"api.anthropic.com:443"}}, wantErr: []string{"read_only:", "api.anthropic.com:443"}},
		{name: "anthropic double wildcard", config: PolicyConfig{Allow: []string{"**.anthropic.com"}}, wantErr: []string{"api.anthropic.com:443"}},
		{name: "anthropic single wildcard", config: PolicyConfig{ReadOnly: []string{"*.anthropic.com"}}, wantErr: []string{"api.anthropic.com:443"}},
		{name: "github", config: PolicyConfig{Allow: []string{"github.com"}}, wantErr: []string{"github.com:443", "GitHub (claude-forge) provider"}},
		{name: "github api", config: PolicyConfig{Allow: []string{"api.github.com:443"}}, wantErr: []string{"api.github.com:443"}},
		{name: "github uploads", config: PolicyConfig{ReadOnly: []string{"uploads.github.com"}}, wantErr: []string{"uploads.github.com:443"}},
		{name: "github double wildcard", config: PolicyConfig{Allow: []string{"**.github.com"}}, wantErr: []string{"github.com:443"}},
		{name: "github single wildcard", config: PolicyConfig{Allow: []string{"*.github.com"}}, wantErr: []string{"github.com:443"}},
		{name: "unknown group", config: PolicyConfig{Disable: []string{"azure"}}, wantErr: []string{"disable:", `"azure"`, "claude_code, github"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.config.Validate()
			require.Error(t, err)
			for _, want := range tt.wantErr {
				assert.Contains(t, err.Error(), want)
			}
		})
	}
}

func TestHostsOverlap(t *testing.T) {
	tests := []struct {
		a, b string
		want bool
	}{
		{a: "api.github.com", b: "api.github.com", want: true},
		{a: "api.github.com", b: "github.com", want: false},
		{a: "*.github.com", b: "api.github.com", want: true},
		{a: "api.github.com", b: "*.github.com", want: true},
		{a: "**.github.com", b: "api.github.com", want: true},
		{a: "*.github.com", b: "github.com", want: false},
		{a: "**.github.com", b: "github.com", want: false},
		{a: "**.com", b: "api.github.com", want: true},
		{a: "*.com", b: "api.github.com", want: false},
		{a: "*.api.github.com", b: "api.github.com", want: false},
		{a: "*.example.com", b: "**.example.com", want: true},
		{a: "**.example.com", b: "*.a.example.com", want: true},
		{a: "*.example.com", b: "*.a.example.com", want: false},
		{a: "*.a.example.com", b: "*.b.example.com", want: false},
		{a: "**.example.com", b: "**.example.org", want: false},
		{a: "**.example.com", b: "api.example.org", want: false},
	}
	for _, tt := range tests {
		t.Run(tt.a+" "+tt.b, func(t *testing.T) {
			assert.Equal(t, tt.want, hostsOverlap(tt.a, tt.b))
			assert.Equal(t, tt.want, hostsOverlap(tt.b, tt.a), "symmetric")
		})
	}
}

func TestProviderOverlaps(t *testing.T) {
	t.Run("lists overlapping endpoints", func(t *testing.T) {
		policy := []byte(`version: 1
network_policies:
  mine:
    name: mine
    endpoints:
      - host: "**.GitHub.com"
        port: 443
      - host: api.anthropic.com
      - host: example.com
        port: 443
      - host: github.com
        port: 22
`)
		got, err := ProviderOverlaps(policy)
		require.NoError(t, err)
		require.Len(t, got, 2)
		assert.Contains(t, got[0], "network_policies.mine: **.GitHub.com:443 overlaps api.github.com:443")
		assert.Contains(t, got[1], "network_policies.mine: api.anthropic.com:443 overlaps api.anthropic.com:443")
	})

	t.Run("generated policy has none", func(t *testing.T) {
		for _, github := range []bool{true, false} {
			p, err := BuildPolicy(PolicyOptions{GitHubProvider: github})
			require.NoError(t, err)
			data, err := p.Render()
			require.NoError(t, err)
			got, err := ProviderOverlaps(data)
			require.NoError(t, err)
			if github {
				assert.Empty(t, got)
			} else {
				// Without the provider, GitHub's hosts are listed uninspected.
				assert.Len(t, got, 3)
			}
		}
	})

	t.Run("invalid policy", func(t *testing.T) {
		_, err := ProviderOverlaps([]byte("network_policies: [1"))
		assert.Error(t, err)
	})
}

package spec

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestClaudeProfile(t *testing.T) {
	p := ClaudeProfile()
	assert.Equal(t, ClaudeProfileID, p.ID)
	require.Len(t, p.Endpoints, 1)
	assert.Equal(t, "api.anthropic.com", p.Endpoints[0].Host)
	var envs []string
	for _, c := range p.Credentials {
		envs = append(envs, c.EnvVars...)
	}
	assert.ElementsMatch(t, []string{EnvClaudeOAuthToken, EnvAnthropicAPIKey}, envs)
	_, err := p.Render()
	require.NoError(t, err)
}

func TestGitHubProfile(t *testing.T) {
	t.Run("no push repos allows everything", func(t *testing.T) {
		p := GitHubProfile(nil)
		for _, e := range p.Endpoints {
			assert.Equal(t, "full", e.Access, e.Host)
			assert.Empty(t, e.Rules)
		}
		assert.Len(t, p.Endpoints, 3)
	})

	t.Run("push repos restrict writes", func(t *testing.T) {
		p := GitHubProfile([]string{"octo/demo"})
		byHost := map[string][]Endpoint{}
		for _, e := range p.Endpoints {
			byHost[e.Host] = append(byHost[e.Host], e)
		}
		has := func(eps []Endpoint, method, path string) bool {
			for _, e := range eps {
				for _, r := range e.Rules {
					if r.Allow.Method == method && r.Allow.Path == path {
						return true
					}
				}
			}
			return false
		}
		assert.True(t, has(byHost["github.com"], "POST", "/octo/demo.git/git-receive-pack"))
		assert.True(t, has(byHost["github.com"], "POST", "/**/git-upload-pack"))
		assert.False(t, has(byHost["github.com"], "POST", "/**/git-receive-pack"))
		assert.True(t, has(byHost["api.github.com"], "PATCH", "/repos/octo/demo/**"))
		assert.True(t, has(byHost["api.github.com"], "GET", "**"))
		assert.True(t, has(byHost["uploads.github.com"], "POST", "/repos/octo/demo/**"))

		var graphql *Endpoint
		for i, e := range byHost["api.github.com"] {
			if e.Path == "/graphql" {
				graphql = &byHost["api.github.com"][i]
			}
		}
		require.NotNil(t, graphql)
		assert.Equal(t, "read-only", graphql.Access)
	})
}

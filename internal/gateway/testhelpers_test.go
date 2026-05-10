package gateway

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewTestProxy(t *testing.T) {
	config := ProxyConfig{
		AllowedOwner: "test-owner",
		AllowedRepo:  "test-repo",
	}
	ghAuth := NewGitHubAuthFromToken("test-token")

	proxy := NewTestProxy(config, ghAuth, "http://example.com")

	require.NotNil(t, proxy)
	assert.Equal(t, "test-owner", proxy.config.AllowedOwner)
	assert.Equal(t, "test-repo", proxy.config.AllowedRepo)
	assert.Equal(t, "http://example.com", proxy.upstreamURL)
	assert.NotNil(t, proxy.httpClient)
	assert.NotNil(t, proxy.ghAuth)
}

func TestNewTestAPIServer(t *testing.T) {
	config := ProxyConfig{
		AllowedOwner: "test-owner",
		AllowedRepo:  "test-repo",
	}
	ghAuth := NewGitHubAuthFromToken("test-token")

	server := NewTestAPIServer(config, ghAuth, "http://example.com")

	require.NotNil(t, server)
	assert.Equal(t, "test-owner", server.config.AllowedOwner)
	assert.Equal(t, "test-repo", server.config.AllowedRepo)
	assert.Equal(t, "http://example.com", server.upstreamURL)
	assert.NotNil(t, server.httpClient)
	assert.NotNil(t, server.ghAuth)
}

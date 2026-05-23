package gateway

import "net/http"

// NewTestProxy creates a Proxy with a custom upstream URL for testing.
func NewTestProxy(config ProxyConfig, ghAuth *GitHubAuth, upstreamURL string) *Proxy {
	return &Proxy{
		config:      config,
		ghAuth:      ghAuth,
		upstreamURL: upstreamURL,
		httpClient:  http.DefaultClient,
	}
}

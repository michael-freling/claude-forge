package kube

import (
	"context"
	"time"
)

// DefaultTokenRefreshInterval is how often a running session re-mints SA
// tokens. It must stay comfortably below the smallest token lifetime an API
// server will grant (1h on most clusters) so a single missed refresh never
// leaves the MCP server with expired credentials.
const DefaultTokenRefreshInterval = 15 * time.Minute

// TokenRefresher periodically re-mints the SA tokens used by the shared
// Kubernetes MCP server. Tokens expire after ~1h on most clusters while
// sessions can stay open for days, so the CLI keeps refreshing them for as
// long as it is attached to a session.
type TokenRefresher struct {
	Contexts       []ContextConfig
	KubeconfigPath string        // host kubeconfig used to mint tokens
	OutputDir      string        // generated-kubeconfig dir mounted into the container
	TokenDuration  time.Duration // lifetime requested for each minted token
	Interval       time.Duration // defaults to DefaultTokenRefreshInterval
	Log            func(format string, args ...any)
}

// Run blocks, refreshing tokens every Interval until ctx is cancelled.
// Failures are logged and retried on the next tick: a transient kubectl or
// network error must not end refreshes for a session that runs for days.
func (r *TokenRefresher) Run(ctx context.Context) {
	interval := r.Interval
	if interval <= 0 {
		interval = DefaultTokenRefreshInterval
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := RefreshTokens(r.Contexts, r.KubeconfigPath, r.OutputDir, r.TokenDuration); err != nil && r.Log != nil {
				r.Log("Warning: failed to refresh Kubernetes MCP tokens: %v", err)
			}
		}
	}
}

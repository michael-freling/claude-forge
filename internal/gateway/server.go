package gateway

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
)

// Server is the main gateway server that runs the git proxy.
type Server struct {
	proxy *Proxy
}

// NewServer creates a new gateway server with the given config.
// It resolves GitHub authentication automatically.
func NewServer(config ProxyConfig) (*Server, error) {
	ghAuth, err := NewGitHubAuth()
	if err != nil {
		return nil, fmt.Errorf("failed to initialize GitHub auth: %w", err)
	}

	return &Server{
		proxy: NewProxy(config, ghAuth),
	}, nil
}

// NewServerWithAuth creates a new gateway server with explicit auth.
func NewServerWithAuth(config ProxyConfig, ghAuth *GitHubAuth) *Server {
	return &Server{
		proxy: NewProxy(config, ghAuth),
	}
}

// Run starts the proxy server on proxyAddr. It blocks until an OS interrupt
// signal is received, then shuts down gracefully.
func (s *Server) Run(proxyAddr string) error {
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()
	return s.RunWithContext(ctx, proxyAddr)
}

// RunWithContext starts the proxy server and blocks until the context is
// cancelled or a server error occurs, then shuts down gracefully.
func (s *Server) RunWithContext(ctx context.Context, proxyAddr string) error {
	proxyServer := &http.Server{
		Addr:    proxyAddr,
		Handler: s.proxy,
	}

	errCh := make(chan error, 1)

	go func() {
		if err := proxyServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			errCh <- fmt.Errorf("proxy server error: %w", err)
		}
	}()

	select {
	case <-ctx.Done():
		fmt.Fprintf(os.Stderr, "shutting down\n")
	case err := <-errCh:
		return err
	}

	proxyServer.Shutdown(context.Background())
	return nil
}

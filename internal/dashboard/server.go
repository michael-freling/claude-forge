package dashboard

import (
	"context"
	"embed"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
)

// assetsFS holds the embedded single-page dashboard UI. The index.html here is
// authored separately (the UI build); the backend only embeds and serves it.
//
//go:embed assets
var assetsFS embed.FS

// Server serves the dashboard UI and its JSON API.
type Server struct {
	provider Provider
}

// NewServer creates a dashboard server backed by the given Provider.
func NewServer(provider Provider) *Server {
	return &Server{provider: provider}
}

// Run serves the dashboard on addr until an OS interrupt signal is received,
// then shuts down gracefully.
func (s *Server) Run(addr string) error {
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()
	return s.RunWithContext(ctx, addr)
}

// RunWithContext serves the dashboard and blocks until the context is cancelled
// or a server error occurs, then shuts down gracefully.
func (s *Server) RunWithContext(ctx context.Context, addr string) error {
	mux := http.NewServeMux()
	mux.HandleFunc("/", s.handleIndex)
	mux.HandleFunc("/api/dashboard", s.handleAPI)

	httpServer := &http.Server{
		Addr:    addr,
		Handler: mux,
	}

	errCh := make(chan error, 1)
	go func() {
		if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			errCh <- fmt.Errorf("dashboard server error: %w", err)
		}
	}()

	select {
	case <-ctx.Done():
		fmt.Fprintf(os.Stderr, "shutting down\n")
	case err := <-errCh:
		return err
	}

	httpServer.Shutdown(context.Background())
	return nil
}

// handleIndex serves the embedded single-page UI at "/".
func (s *Server) handleIndex(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	data, err := assetsFS.ReadFile("assets/index.html")
	if err != nil {
		http.Error(w, "dashboard UI not found", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write(data)
}

// handleAPI serves the dashboard snapshot as JSON at "/api/dashboard".
func (s *Server) handleAPI(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	dash, err := s.provider.Build(r.Context())
	if err != nil {
		http.Error(w, fmt.Sprintf("failed to build dashboard: %v", err), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(dash); err != nil {
		fmt.Fprintf(os.Stderr, "failed to encode dashboard: %v\n", err)
	}
}

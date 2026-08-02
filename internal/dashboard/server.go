// Package dashboard implements the local web dashboard for claude-forge: a
// read-only ConnectRPC service (and an embedded single-page UI) that surfaces
// each project's sessions, their pull requests, and the status of the MCP
// server containers backing running sessions and the shared global scope.
package dashboard

import (
	"context"
	"embed"
	"fmt"
	"io/fs"
	"net/http"
	"os"
	"os/signal"
	"path"
	"strings"
	"syscall"

	"connectrpc.com/connect"

	dashboardv1 "github.com/michael-freling/claude-forge/internal/gen/dashboard/v1"
	"github.com/michael-freling/claude-forge/internal/gen/dashboard/v1/dashboardv1connect"
)

// webFS holds the embedded single-page dashboard UI. The index.html committed
// here is a placeholder so the Go build always compiles; the frontend build
// (Vite, output dir internal/dashboard/webdist) overwrites it with the real SPA.
//
//go:embed webdist
var webFS embed.FS

// Server serves the dashboard: a ConnectRPC DashboardService plus the embedded
// single-page UI. It implements dashboardv1connect.DashboardServiceHandler.
type Server struct {
	provider Provider
}

var _ dashboardv1connect.DashboardServiceHandler = (*Server)(nil)

// NewServer creates a dashboard server backed by the given Provider.
func NewServer(provider Provider) *Server {
	return &Server{provider: provider}
}

// GetDashboard implements the DashboardService RPC: it builds the current
// snapshot via the Provider and maps it to the generated proto response.
func (s *Server) GetDashboard(
	ctx context.Context,
	_ *connect.Request[dashboardv1.GetDashboardRequest],
) (*connect.Response[dashboardv1.GetDashboardResponse], error) {
	dash, err := s.provider.Build(ctx)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to build dashboard: %w", err))
	}
	return connect.NewResponse(toProtoResponse(dash)), nil
}

// newHandler wires the ConnectRPC service at its generated path and serves the
// embedded SPA (with client-side-routing fallback) for everything else.
func (s *Server) newHandler() http.Handler {
	mux := http.NewServeMux()

	rpcPath, rpcHandler := dashboardv1connect.NewDashboardServiceHandler(s)
	mux.Handle(rpcPath, rpcHandler)

	// fs.Sub over an embed.FS with a present subdir never errors.
	sub, _ := fs.Sub(webFS, "webdist")
	mux.Handle("/", spaHandler(sub))

	return mux
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
	httpServer := &http.Server{
		Addr:    addr,
		Handler: s.newHandler(),
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

// spaHandler serves static files from fsys and falls back to index.html for any
// path that does not resolve to a file, so the SPA can handle client-side
// routing. The root path is served from index.html directly.
func spaHandler(fsys fs.FS) http.Handler {
	fileServer := http.FileServerFS(fsys)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		name := strings.TrimPrefix(path.Clean(r.URL.Path), "/")
		if name == "" {
			serveIndex(w, fsys)
			return
		}
		if _, err := fs.Stat(fsys, name); err != nil {
			// Unknown non-API path: let the SPA's client-side router handle it.
			serveIndex(w, fsys)
			return
		}
		fileServer.ServeHTTP(w, r)
	})
}

// serveIndex writes the embedded index.html, the SPA entrypoint.
func serveIndex(w http.ResponseWriter, fsys fs.FS) {
	data, err := fs.ReadFile(fsys, "index.html")
	if err != nil {
		http.Error(w, "dashboard UI not built", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write(data)
}

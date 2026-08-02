package dashboard

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"syscall"
	"testing"
	"testing/fstest"
	"time"

	"connectrpc.com/connect"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	dashboardv1 "github.com/michael-freling/claude-forge/internal/gen/dashboard/v1"
	"github.com/michael-freling/claude-forge/internal/gen/dashboard/v1/dashboardv1connect"
)

type stubProvider struct {
	dash *Dashboard
	err  error
}

func (s stubProvider) Build(_ context.Context) (*Dashboard, error) {
	return s.dash, s.err
}

// --- ConnectRPC handler ---

func TestServer_GetDashboard_Direct(t *testing.T) {
	dash := &Dashboard{
		GeneratedAt: "2025-01-15T10:00:00Z",
		Warnings:    []string{"heads up"},
		Global:      GlobalMCP{Servers: []MCPServer{{Name: "kubernetes", Scope: "global", Kind: "container"}}},
		Projects:    []Project{{ID: "-x", Resolved: true}},
	}
	srv := NewServer(stubProvider{dash: dash})

	resp, err := srv.GetDashboard(context.Background(), connect.NewRequest(&dashboardv1.GetDashboardRequest{}))
	require.NoError(t, err)

	pd := resp.Msg.GetDashboard()
	require.NotNil(t, pd)
	require.NotNil(t, pd.GetGeneratedAt())
	assert.Equal(t, []string{"heads up"}, pd.GetWarnings())
	require.Len(t, pd.GetGlobal().GetServers(), 1)
	assert.Equal(t, "kubernetes", pd.GetGlobal().GetServers()[0].GetName())
	require.Len(t, pd.GetProjects(), 1)
	assert.Equal(t, "-x", pd.GetProjects()[0].GetId())
}

func TestServer_GetDashboard_BuildError(t *testing.T) {
	srv := NewServer(stubProvider{err: fmt.Errorf("build boom")})

	_, err := srv.GetDashboard(context.Background(), connect.NewRequest(&dashboardv1.GetDashboardRequest{}))
	require.Error(t, err)
	assert.Equal(t, connect.CodeInternal, connect.CodeOf(err))
	assert.Contains(t, err.Error(), "build boom")
}

func TestServer_GetDashboard_OverHTTP(t *testing.T) {
	dash := &Dashboard{
		GeneratedAt: "2025-01-15T10:00:00Z",
		Global:      GlobalMCP{Servers: []MCPServer{{Name: "github", Scope: "session", Kind: "container"}}},
		Projects:    []Project{{ID: "-y"}},
	}
	srv := NewServer(stubProvider{dash: dash})

	ts := httptest.NewServer(srv.newHandler())
	defer ts.Close()

	client := dashboardv1connect.NewDashboardServiceClient(http.DefaultClient, ts.URL)
	resp, err := client.GetDashboard(context.Background(), connect.NewRequest(&dashboardv1.GetDashboardRequest{}))
	require.NoError(t, err)

	pd := resp.Msg.GetDashboard()
	require.NotNil(t, pd)
	require.Len(t, pd.GetProjects(), 1)
	assert.Equal(t, "-y", pd.GetProjects()[0].GetId())
	require.Len(t, pd.GetGlobal().GetServers(), 1)
	assert.Equal(t, dashboardv1.McpScope_MCP_SCOPE_SESSION, pd.GetGlobal().GetServers()[0].GetScope())
}

func TestServer_GetDashboard_OverHTTP_Error(t *testing.T) {
	srv := NewServer(stubProvider{err: fmt.Errorf("nope")})

	ts := httptest.NewServer(srv.newHandler())
	defer ts.Close()

	client := dashboardv1connect.NewDashboardServiceClient(http.DefaultClient, ts.URL)
	_, err := client.GetDashboard(context.Background(), connect.NewRequest(&dashboardv1.GetDashboardRequest{}))
	require.Error(t, err)
	assert.Equal(t, connect.CodeInternal, connect.CodeOf(err))
}

// --- SPA serving ---

func TestServer_SPA_Root(t *testing.T) {
	srv := NewServer(stubProvider{dash: &Dashboard{}})
	ts := httptest.NewServer(srv.newHandler())
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/")
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Contains(t, resp.Header.Get("Content-Type"), "text/html")
	body := readAll(t, resp)
	assert.Contains(t, body, "claude-forge dashboard")
}

func TestServer_SPA_FallbackUnknownPath(t *testing.T) {
	srv := NewServer(stubProvider{dash: &Dashboard{}})
	ts := httptest.NewServer(srv.newHandler())
	defer ts.Close()

	// A client-side route with no matching file falls back to index.html.
	resp, err := http.Get(ts.URL + "/projects/some/deep/route")
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Contains(t, readAll(t, resp), "claude-forge dashboard")
}

func TestSPAHandler_ServesExistingFile(t *testing.T) {
	fsys := fstest.MapFS{
		"index.html":   {Data: []byte("<title>root</title>")},
		"app.js":       {Data: []byte("console.log(1)")},
		"static/x.css": {Data: []byte("body{}")},
	}
	h := spaHandler(fsys)

	// Existing static file served directly.
	req := httptest.NewRequest(http.MethodGet, "/app.js", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	assert.Equal(t, http.StatusOK, w.Code)
	assert.Contains(t, w.Body.String(), "console.log(1)")

	// Nested existing file served directly.
	req = httptest.NewRequest(http.MethodGet, "/static/x.css", nil)
	w = httptest.NewRecorder()
	h.ServeHTTP(w, req)
	assert.Equal(t, http.StatusOK, w.Code)
	assert.Contains(t, w.Body.String(), "body{}")

	// Unknown path falls back to index.html.
	req = httptest.NewRequest(http.MethodGet, "/unknown", nil)
	w = httptest.NewRecorder()
	h.ServeHTTP(w, req)
	assert.Equal(t, http.StatusOK, w.Code)
	assert.Contains(t, w.Body.String(), "<title>root</title>")
}

func TestSPAHandler_MissingIndex(t *testing.T) {
	// An FS with no index.html surfaces a 500 on the fallback.
	fsys := fstest.MapFS{"app.js": {Data: []byte("x")}}
	h := spaHandler(fsys)

	req := httptest.NewRequest(http.MethodGet, "/whatever", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	assert.Equal(t, http.StatusInternalServerError, w.Code)
	assert.Contains(t, w.Body.String(), "dashboard UI not built")
}

// --- lifecycle ---

func TestServer_RunWithContext_Shutdown(t *testing.T) {
	srv := NewServer(stubProvider{dash: &Dashboard{}})

	ctx, cancel := context.WithCancel(context.Background())
	errCh := make(chan error, 1)
	go func() {
		errCh <- srv.RunWithContext(ctx, "127.0.0.1:0")
	}()

	time.Sleep(50 * time.Millisecond)
	cancel()

	select {
	case err := <-errCh:
		assert.NoError(t, err)
	case <-time.After(5 * time.Second):
		t.Fatal("RunWithContext did not return after context cancellation")
	}
}

func TestServer_RunWithContext_BadAddress(t *testing.T) {
	srv := NewServer(stubProvider{dash: &Dashboard{}})

	err := srv.RunWithContext(t.Context(), "invalid-address-::::")
	require.Error(t, err)
}

func TestServer_Run_Signal(t *testing.T) {
	srv := NewServer(stubProvider{dash: &Dashboard{}})

	errCh := make(chan error, 1)
	go func() {
		errCh <- srv.Run("127.0.0.1:0")
	}()

	time.Sleep(50 * time.Millisecond)

	p, err := os.FindProcess(os.Getpid())
	require.NoError(t, err)
	require.NoError(t, p.Signal(syscall.SIGTERM))

	select {
	case err := <-errCh:
		assert.NoError(t, err)
	case <-time.After(5 * time.Second):
		t.Fatal("Run did not return after SIGTERM")
	}
}

func readAll(t *testing.T, resp *http.Response) string {
	t.Helper()
	data, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	return string(data)
}

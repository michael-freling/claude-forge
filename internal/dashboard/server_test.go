package dashboard

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type stubProvider struct {
	dash *Dashboard
	err  error
}

func (s stubProvider) Build(_ context.Context) (*Dashboard, error) {
	return s.dash, s.err
}

func TestServer_HandleIndex(t *testing.T) {
	srv := NewServer(stubProvider{dash: &Dashboard{}})

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	w := httptest.NewRecorder()
	srv.handleIndex(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Contains(t, w.Header().Get("Content-Type"), "text/html")
	assert.Contains(t, w.Body.String(), "claude-forge dashboard")
}

func TestServer_HandleIndex_NotFound(t *testing.T) {
	srv := NewServer(stubProvider{})

	req := httptest.NewRequest(http.MethodGet, "/nope", nil)
	w := httptest.NewRecorder()
	srv.handleIndex(w, req)

	assert.Equal(t, http.StatusNotFound, w.Code)
}

func TestServer_HandleIndex_MethodNotAllowed(t *testing.T) {
	srv := NewServer(stubProvider{})

	req := httptest.NewRequest(http.MethodPost, "/", nil)
	w := httptest.NewRecorder()
	srv.handleIndex(w, req)

	assert.Equal(t, http.StatusMethodNotAllowed, w.Code)
}

func TestServer_HandleAPI(t *testing.T) {
	dash := &Dashboard{
		GeneratedAt: "2025-01-15T10:00:00Z",
		Warnings:    []string{},
		Global:      GlobalMCP{Servers: []MCPServer{{Name: "kubernetes", Scope: "global"}}},
		Projects:    []Project{{ID: "-x", Resolved: true}},
	}
	srv := NewServer(stubProvider{dash: dash})

	req := httptest.NewRequest(http.MethodGet, "/api/dashboard", nil)
	w := httptest.NewRecorder()
	srv.handleAPI(w, req)

	require.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "application/json", w.Header().Get("Content-Type"))

	var got Dashboard
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &got))
	assert.Equal(t, "2025-01-15T10:00:00Z", got.GeneratedAt)
	require.Len(t, got.Global.Servers, 1)
	assert.Equal(t, "kubernetes", got.Global.Servers[0].Name)
	require.Len(t, got.Projects, 1)
	assert.Equal(t, "-x", got.Projects[0].ID)
}

func TestServer_HandleAPI_MethodNotAllowed(t *testing.T) {
	srv := NewServer(stubProvider{dash: &Dashboard{}})

	req := httptest.NewRequest(http.MethodPost, "/api/dashboard", nil)
	w := httptest.NewRecorder()
	srv.handleAPI(w, req)

	assert.Equal(t, http.StatusMethodNotAllowed, w.Code)
}

func TestServer_HandleAPI_BuildError(t *testing.T) {
	srv := NewServer(stubProvider{err: fmt.Errorf("build boom")})

	req := httptest.NewRequest(http.MethodGet, "/api/dashboard", nil)
	w := httptest.NewRecorder()
	srv.handleAPI(w, req)

	assert.Equal(t, http.StatusInternalServerError, w.Code)
	assert.Contains(t, w.Body.String(), "build boom")
}

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

package gateway

import (
	"context"
	"os"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewServerWithAuth(t *testing.T) {
	config := ProxyConfig{
		AllowedOwner: "test-owner",
		AllowedRepo:  "test-repo",
	}
	ghAuth := NewGitHubAuthFromToken("test-token")

	server := NewServerWithAuth(config, ghAuth)

	require.NotNil(t, server)
	assert.NotNil(t, server.proxy)
}

func TestNewServer_NoAuth(t *testing.T) {
	t.Setenv("GITHUB_TOKEN", "")
	t.Setenv("HOME", t.TempDir())

	config := ProxyConfig{
		AllowedOwner: "test-owner",
		AllowedRepo:  "test-repo",
	}

	_, err := NewServer(config)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to initialize GitHub auth")
}

func TestNewServer_WithEnvToken(t *testing.T) {
	t.Setenv("GITHUB_TOKEN", "ghp_test_server_token")

	config := ProxyConfig{
		AllowedOwner: "test-owner",
		AllowedRepo:  "test-repo",
	}

	server, err := NewServer(config)

	require.NoError(t, err)
	require.NotNil(t, server)
	assert.NotNil(t, server.proxy)
}

func TestServer_RunWithContext_Shutdown(t *testing.T) {
	ghAuth := NewGitHubAuthFromToken("test-token")
	srv := NewServerWithAuth(ProxyConfig{
		AllowedOwner: "test-owner",
		AllowedRepo:  "test-repo",
	}, ghAuth)

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

func TestServer_Run_Signal(t *testing.T) {
	ghAuth := NewGitHubAuthFromToken("test-token")
	srv := NewServerWithAuth(ProxyConfig{
		AllowedOwner: "test-owner",
		AllowedRepo:  "test-repo",
	}, ghAuth)

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

func TestServer_RunWithContext_BadAddress(t *testing.T) {
	ghAuth := NewGitHubAuthFromToken("test-token")
	srv := NewServerWithAuth(ProxyConfig{
		AllowedOwner: "test-owner",
		AllowedRepo:  "test-repo",
	}, ghAuth)

	err := srv.RunWithContext(t.Context(), "invalid-address-::::")
	require.Error(t, err)
}

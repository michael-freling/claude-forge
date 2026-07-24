package kube

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTokenRefresher_RefreshesUntilCancelled(t *testing.T) {
	var calls atomic.Int32
	stubResolveToken(t, func(ctx ContextConfig, kubeconfigPath string, duration time.Duration) (string, error) {
		calls.Add(1)
		return fmt.Sprintf("token-%d", calls.Load()), nil
	})

	outDir := filepath.Join(t.TempDir(), "out")
	contexts := []ContextConfig{
		{HostContext: "ctx-a", ServiceAccountName: "sa-a", ServiceAccountNamespace: "ns-a"},
	}

	r := &TokenRefresher{
		Contexts:       contexts,
		KubeconfigPath: "unused",
		OutputDir:      outDir,
		TokenDuration:  time.Hour,
		Interval:       time.Millisecond,
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		r.Run(ctx)
		close(done)
	}()

	require.Eventually(t, func() bool { return calls.Load() >= 2 }, 5*time.Second, time.Millisecond)
	cancel()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("refresher did not stop after cancellation")
	}

	data, err := os.ReadFile(filepath.Join(outDir, TokensDirName, TokenFileName("ctx-a")))
	require.NoError(t, err)
	assert.Contains(t, string(data), "token-")
}

func TestTokenRefresher_IntervalCappedByTokenDuration(t *testing.T) {
	var calls atomic.Int32
	stubResolveToken(t, func(ctx ContextConfig, kubeconfigPath string, duration time.Duration) (string, error) {
		calls.Add(1)
		return "token", nil
	})

	// A long explicit Interval must be capped to half the token lifetime —
	// otherwise a short-lived token would expire between ticks.
	r := &TokenRefresher{
		Contexts: []ContextConfig{
			{HostContext: "ctx-a", ServiceAccountName: "sa-a", ServiceAccountNamespace: "ns-a"},
		},
		KubeconfigPath: "unused",
		OutputDir:      filepath.Join(t.TempDir(), "out"),
		TokenDuration:  10 * time.Millisecond,
		Interval:       time.Hour,
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go r.Run(ctx)

	require.Eventually(t, func() bool { return calls.Load() >= 2 }, 5*time.Second, time.Millisecond)
}

func TestTokenRefresher_LogsAndKeepsGoingOnError(t *testing.T) {
	var calls atomic.Int32
	stubResolveToken(t, func(ctx ContextConfig, kubeconfigPath string, duration time.Duration) (string, error) {
		calls.Add(1)
		return "", fmt.Errorf("transient failure")
	})

	var logged atomic.Int32
	r := &TokenRefresher{
		Contexts: []ContextConfig{
			{HostContext: "ctx-a", ServiceAccountName: "sa-a", ServiceAccountNamespace: "ns-a"},
		},
		KubeconfigPath: "unused",
		OutputDir:      filepath.Join(t.TempDir(), "out"),
		Interval:       time.Millisecond,
		Log: func(format string, args ...any) {
			logged.Add(1)
			assert.Contains(t, fmt.Sprintf(format, args...), "transient failure")
		},
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go r.Run(ctx)

	// Multiple failing ticks: the loop must survive errors and keep retrying.
	require.Eventually(t, func() bool { return calls.Load() >= 2 && logged.Load() >= 2 }, 5*time.Second, time.Millisecond)
}

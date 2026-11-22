package main

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/michael-freling/claude-forge/internal/forge/auth"
	"github.com/michael-freling/claude-forge/internal/forge/config"
	"github.com/michael-freling/claude-forge/internal/forge/layout"
	"github.com/michael-freling/claude-forge/internal/forge/openshell"
)

// recordingRunner records openshell invocations and answers sandbox list.
type recordingRunner struct {
	calls []string
}

func (r *recordingRunner) Output(_ context.Context, _ []string, args ...string) ([]byte, error) {
	joined := strings.Join(args, " ")
	r.calls = append(r.calls, joined)
	if strings.HasPrefix(joined, "sandbox list") {
		return []byte(`{"sandboxes":[{"name":"demo","phase":"Ready","labels":{"claude-forge":"true"}}]}`), nil
	}
	if strings.HasPrefix(joined, "profile export") {
		return []byte("resource_version: 1\n"), nil
	}
	return nil, nil
}

func (r *recordingRunner) Interactive(_ context.Context, args ...string) error {
	r.calls = append(r.calls, strings.Join(args, " "))
	return nil
}

func withFakeOpenShell(t *testing.T) *recordingRunner {
	t.Helper()
	runner := &recordingRunner{}
	host := t.TempDir()
	orig := newOpenShellManager
	t.Cleanup(func() { newOpenShellManager = orig })
	newOpenShellManager = func() (*openshell.Manager, error) {
		return &openshell.Manager{
			Client:    &openshell.Client{Runner: runner, TempDir: t.TempDir()},
			Config:    config.DefaultConfig(),
			ClaudeDir: host, ConfigDir: host, HomeDir: host,
			LookupEnv: func(string) (string, bool) { return "", false },
			ResolveClaude: func() (*auth.Credentials, error) {
				return &auth.Credentials{AuthType: "oauth", Token: "t"}, nil
			},
		}, nil
	}
	return runner
}

func runOpenShell(t *testing.T, args ...string) (string, error) {
	t.Helper()
	cmd := newOpenShellCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs(args)
	err := cmd.ExecuteContext(context.Background())
	return out.String(), err
}

func TestOpenShellCmd(t *testing.T) {
	tests := []struct {
		args     []string
		wantCall string
		wantOut  string
	}{
		{args: []string{"start", "new1", "-p", "hello", "--agent", "a1"}, wantCall: "-- claude --dangerously-skip-permissions --agent a1 --name new1 hello"},
		{args: []string{"start", "new1", "--no-skip-permissions"}, wantCall: "-- claude --name new1"},
		{args: []string{"connect", "demo", "--agent", "a1"}, wantCall: "claude --dangerously-skip-permissions --agent a1"},
		{args: []string{"list"}, wantOut: "demo"},
		{args: []string{"stop", "demo"}, wantCall: "sandbox stop demo"},
		{args: []string{"delete", "demo"}, wantCall: "sandbox delete demo"},
		{args: []string{"auth"}, wantCall: "provider create --name " + openshell.ClaudeProviderName},
		{args: []string{"policy"}, wantOut: "network_policies"},
		{args: []string{"policy", "--no-github-token"}, wantOut: "api.github.com"},
	}
	for _, tt := range tests {
		t.Run(strings.Join(tt.args, " "), func(t *testing.T) {
			runner := withFakeOpenShell(t)
			out, err := runOpenShell(t, tt.args...)
			require.NoError(t, err)
			if tt.wantOut != "" {
				assert.Contains(t, out, tt.wantOut)
			}
			if tt.wantCall != "" {
				assert.True(t, strings.Contains(strings.Join(runner.calls, "\n"), tt.wantCall), "calls: %v", runner.calls)
			}
		})
	}
}

func TestOpenShellStartRepoFlag(t *testing.T) {
	t.Run("repeatable", func(t *testing.T) {
		runner := withFakeOpenShell(t)
		_, err := runOpenShell(t, "start", "new1", "--repo", "octo/a", "--repo", "other/b")
		require.NoError(t, err)
		calls := strings.Join(runner.calls, "\n")
		assert.Contains(t, calls, "-- test -e "+layout.Workspace+"/a")
		assert.Contains(t, calls, "-- test -e "+layout.Workspace+"/b")
	})

	t.Run("invalid", func(t *testing.T) {
		runner := withFakeOpenShell(t)
		_, err := runOpenShell(t, "start", "new1", "--repo", "not-a-repo")
		require.Error(t, err)
		assert.Contains(t, err.Error(), `invalid repository "not-a-repo"`)
		assert.NotContains(t, strings.Join(runner.calls, "\n"), "sandbox create")
	})
}

func TestOpenShellCmd_ManagerError(t *testing.T) {
	orig := newOpenShellManager
	t.Cleanup(func() { newOpenShellManager = orig })
	errBoom := errors.New("boom")
	newOpenShellManager = func() (*openshell.Manager, error) { return nil, errBoom }

	for _, args := range [][]string{
		{"start", "x"}, {"connect", "x"}, {"list"}, {"stop", "x"},
		{"delete", "x"}, {"auth"}, {"policy"},
	} {
		_, err := runOpenShell(t, args...)
		assert.ErrorIs(t, err, errBoom, args)
	}
}

func TestNewOpenShellManager(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	t.Run("openshell missing", func(t *testing.T) {
		t.Setenv("PATH", t.TempDir())
		_, err := newOpenShellManager()
		require.Error(t, err)
		assert.Contains(t, err.Error(), "openshell CLI not found")
	})

	t.Run("builds manager", func(t *testing.T) {
		bin := t.TempDir()
		require.NoError(t, os.WriteFile(filepath.Join(bin, "openshell"), []byte("#!/bin/sh\n"), 0o755))
		require.NoError(t, os.WriteFile(filepath.Join(bin, "gh"), []byte("#!/bin/sh\necho gh-token\n"), 0o755))
		t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))

		m, err := newOpenShellManager()
		require.NoError(t, err)
		assert.Equal(t, filepath.Join(home, ".claude"), m.ClaudeDir)
		token, err := m.GitHubCLIToken(context.Background())
		require.NoError(t, err)
		assert.Equal(t, "gh-token\n", token)
		m.GitIdentity()
		assert.Equal(t, "", m.ClaudeModel())
	})

	t.Run("gh missing", func(t *testing.T) {
		bin := t.TempDir()
		require.NoError(t, os.WriteFile(filepath.Join(bin, "openshell"), []byte("#!/bin/sh\n"), 0o755))
		t.Setenv("PATH", bin)
		m, err := newOpenShellManager()
		require.NoError(t, err)
		_, err = m.GitHubCLIToken(context.Background())
		assert.Error(t, err)
	})

	t.Run("bad config", func(t *testing.T) {
		dir := filepath.Join(home, ".config", "claude-forge")
		require.NoError(t, os.MkdirAll(dir, 0o755))
		require.NoError(t, os.WriteFile(filepath.Join(dir, "config.yaml"), []byte("openshell:\n  github:\n    push_repos: [bad]\n"), 0o644))
		t.Cleanup(func() { os.Remove(filepath.Join(dir, "config.yaml")) })
		_, err := newOpenShellManager()
		assert.Error(t, err)
	})
}

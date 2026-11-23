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
	"github.com/michael-freling/claude-forge/internal/forge/openshell/spec"
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
	return withFakeOpenShellConfig(t, config.DefaultConfig())
}

func withFakeOpenShellConfig(t *testing.T, cfg *config.Config) *recordingRunner {
	t.Helper()
	runner := &recordingRunner{}
	host := t.TempDir()
	orig := newOpenShellManager
	t.Cleanup(func() { newOpenShellManager = orig })
	newOpenShellManager = func() (*openshell.Manager, error) {
		return &openshell.Manager{
			Client:    &openshell.Client{Runner: runner, TempDir: t.TempDir()},
			Config:    cfg,
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
		{args: []string{"start", "new1", "-p", "hello", "--agent", "a1"}, wantCall: "-- env -u CLAUDE_CODE_OAUTH_TOKEN claude --dangerously-skip-permissions --agent a1 --name new1 hello"},
		{args: []string{"start", "new1", "--no-skip-permissions"}, wantCall: "-- env -u CLAUDE_CODE_OAUTH_TOKEN claude --name new1"},
		{args: []string{"start", "new1", "--remote-control"}, wantCall: "-- env -u CLAUDE_CODE_OAUTH_TOKEN claude --remote-control new1 --dangerously-skip-permissions --name new1"},
		{args: []string{"connect", "demo", "--agent", "a1"}, wantCall: "claude --dangerously-skip-permissions --agent a1"},
		{args: []string{"connect", "demo", "--remote-control"}, wantCall: "claude --remote-control demo --dangerously-skip-permissions"},
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

func TestOpenShellRemoteControlNeedsLogin(t *testing.T) {
	for _, args := range [][]string{{"start", "new1", "--remote-control"}, {"connect", "demo", "--remote-control"}} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			runner := withFakeOpenShell(t)
			orig := newOpenShellManager
			newOpenShellManager = func() (*openshell.Manager, error) {
				m, err := orig()
				require.NoError(t, err)
				m.ResolveClaude = func() (*auth.Credentials, error) {
					return &auth.Credentials{AuthType: "oauth", Token: "t", FromEnv: true}, nil
				}
				return m, nil
			}
			_, err := runOpenShell(t, args...)
			require.ErrorIs(t, err, openshell.ErrRemoteControlNeedsLogin)
			assert.NotContains(t, strings.Join(runner.calls, "\n"), "sandbox exec")
		})
	}
}

func TestOpenShellPolicyCmd(t *testing.T) {
	policyFile := filepath.Join(t.TempDir(), "policy.yaml")
	const policy = "version: 1\nnetwork_policies: {}\n"
	require.NoError(t, os.WriteFile(policyFile, []byte(policy), 0o644))

	tests := []struct {
		name      string
		openshell config.OpenShellConfig
		want      func(t *testing.T, out string)
		wantErr   bool
	}{
		{
			name: "generated with extensions",
			openshell: config.OpenShellConfig{Policy: spec.PolicyConfig{
				Allow:    []string{"db.example.com:5432"},
				ReadOnly: []string{"docs.example.com"},
				Disable:  []string{"aws"},
			}},
			want: func(t *testing.T, out string) {
				assert.Contains(t, out, "custom_allow:")
				assert.Contains(t, out, "port: 5432")
				assert.Contains(t, out, "custom_read_only:")
				assert.NotContains(t, out, "amazonaws.com")
			},
		},
		{
			name:      "policy file",
			openshell: config.OpenShellConfig{PolicyFile: policyFile},
			want:      func(t *testing.T, out string) { assert.Equal(t, policy, out) },
		},
		{
			name:      "unreadable policy file",
			openshell: config.OpenShellConfig{PolicyFile: filepath.Join(t.TempDir(), "missing.yaml")},
			wantErr:   true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := config.DefaultConfig()
			cfg.OpenShell = tt.openshell
			withFakeOpenShellConfig(t, cfg)
			out, err := runOpenShell(t, "policy")
			if tt.wantErr {
				assert.Error(t, err)
				return
			}
			require.NoError(t, err)
			tt.want(t, out)
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

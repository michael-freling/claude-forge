package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/michael-freling/claude-forge/internal/forge/container"
	"github.com/michael-freling/claude-forge/internal/forge/layout"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// cmdCapturingContainerManager is a fakeContainerManager that records the
// agent container's command, so tests can check which Claude Code flags the
// CLI plumbed through to the orchestrator.
type cmdCapturingContainerManager struct {
	fakeContainerManager
	agentCmd []string
}

func (c *cmdCapturingContainerManager) StartAgent(_ context.Context, opts container.AgentOptions) (string, error) {
	c.agentCmd = opts.Cmd
	return "agent-id", nil
}

// agentArg returns the value following --agent in cmd, or "" when absent.
func agentArg(cmd []string) string {
	for i, a := range cmd {
		if a == "--agent" && i+1 < len(cmd) {
			return cmd[i+1]
		}
	}
	return ""
}

// setupAgentTest prepares a git project as the working directory, a fake
// orchestrator that captures the agent command, and optionally a config.yaml
// with defaults.agent set. It returns the capturing container manager.
func setupAgentTest(t *testing.T, configDefault string) *cmdCapturingContainerManager {
	t.Helper()
	cm := &cmdCapturingContainerManager{}
	homeDir := setupTestOrchestrator(t, cm)
	if configDefault != "" {
		configYAML := "defaults:\n  agent: " + configDefault + "\n"
		require.NoError(t, os.WriteFile(filepath.Join(homeDir, ".config", "claude-forge", "config.yaml"), []byte(configYAML), 0o644))
	}

	// Keep the session directory resume reads (~/.claude-forge/...) out of
	// the real home directory.
	t.Setenv("HOME", t.TempDir())
	t.Setenv("ANTHROPIC_API_KEY", "sk-ant-test-key")

	originalSync := syncHostPlugins
	syncHostPlugins = func(string, bool) error { return nil }
	t.Cleanup(func() { syncHostPlugins = originalSync })

	repoDir := setupTestGitRepo(t)
	origDir, err := os.Getwd()
	require.NoError(t, err)
	require.NoError(t, os.Chdir(repoDir))
	t.Cleanup(func() { _ = os.Chdir(origDir) })
	return cm
}

func TestStartCmd_AgentFlag(t *testing.T) {
	tests := []struct {
		name          string
		configDefault string
		args          []string
		want          string
	}{
		{
			name: "no agent",
			args: []string{"-p", "task", "sess"},
			want: "",
		},
		{
			name:          "config default",
			configDefault: "my-plugin:orchestrator",
			args:          []string{"-p", "task", "sess"},
			want:          "my-plugin:orchestrator",
		},
		{
			name:          "flag overrides config default",
			configDefault: "my-plugin:orchestrator",
			args:          []string{"--agent", "reviewer", "-p", "task", "sess"},
			want:          "reviewer",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cm := setupAgentTest(t, tt.configDefault)

			cmd := newStartCmd()
			cmd.SetArgs(tt.args)
			require.NoError(t, cmd.Execute())

			require.NotNil(t, cm.agentCmd, "agent container was not started")
			assert.Equal(t, tt.want, agentArg(cm.agentCmd))
		})
	}
}

func TestStartCmd_InvalidAgentFlag(t *testing.T) {
	cm := setupAgentTest(t, "")

	cmd := newStartCmd()
	cmd.SetArgs([]string{"--agent", "-p", "-p", "task", "sess"})
	err := cmd.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "must not start with")
	assert.Nil(t, cm.agentCmd, "no agent container should start for an invalid agent")
}

func TestResumeCmd_AgentFlag(t *testing.T) {
	tests := []struct {
		name          string
		configDefault string
		args          []string
		want          string
	}{
		{
			name:          "config default on continue",
			configDefault: "my-plugin:orchestrator",
			args:          nil,
			want:          "my-plugin:orchestrator",
		},
		{
			name:          "flag overrides config default on resume by id",
			configDefault: "my-plugin:orchestrator",
			args:          []string{"--agent", "reviewer", "resume-session"},
			want:          "reviewer",
		},
		{
			name: "flag without config default on continue",
			args: []string{"--agent", "reviewer"},
			want: "reviewer",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cm := setupAgentTest(t, tt.configDefault)

			sessionDir, err := projectSessionDir()
			require.NoError(t, err)
			bucket := filepath.Join(sessionDir, layout.SessionSubdir)
			require.NoError(t, os.MkdirAll(bucket, 0o755))
			writeSessionFile(t, filepath.Join(bucket, "resume-session.jsonl"),
				"2025-01-15T10:30:00Z", "earlier work")

			cmd := newResumeCmd()
			cmd.SetArgs(tt.args)
			// resume is interactive: the docker attach to the fake agent
			// container fails harmlessly once the session has started.
			captureStdout(t, func() { err = cmd.Execute() })
			require.NoError(t, err)

			require.NotNil(t, cm.agentCmd, "agent container was not started")
			assert.Contains(t, strings.Join(cm.agentCmd, " "), "--resume")
			assert.Equal(t, tt.want, agentArg(cm.agentCmd))
		})
	}
}

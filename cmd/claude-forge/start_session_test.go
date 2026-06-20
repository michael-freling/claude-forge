package main

import (
	"context"
	"os"
	"os/exec"
	"testing"
	"time"

	"github.com/michael-freling/claude-forge/internal/forge"
	"github.com/michael-freling/claude-forge/internal/forge/container"
	"github.com/stretchr/testify/require"
)

// fakeContainerManager implements container.ContainerManager with success
// defaults, so an Orchestrator built on it can run Start/Cleanup without a real
// Docker daemon. It lets us cover startSession end to end.
type fakeContainerManager struct{}

func (fakeContainerManager) CreateNetwork(context.Context, string) (string, error) {
	return "net-id", nil
}
func (fakeContainerManager) RemoveNetwork(context.Context, string) error { return nil }
func (fakeContainerManager) EnsureSharedNetwork(context.Context, string) (string, error) {
	return "shared-net", nil
}
func (fakeContainerManager) ConnectNetwork(context.Context, string, string, []string) error {
	return nil
}
func (fakeContainerManager) StartAgent(context.Context, container.AgentOptions) (string, error) {
	return "agent-id", nil
}
func (fakeContainerManager) StartGateway(context.Context, container.GatewayOptions) (string, error) {
	return "gw-id", nil
}
func (fakeContainerManager) StartGitHubMCP(context.Context, container.GitHubMCPOptions) (string, error) {
	return "mcp-id", nil
}
func (fakeContainerManager) StartSharedService(context.Context, container.SharedServiceOptions) (string, error) {
	return "shared-id", nil
}
func (fakeContainerManager) IsContainerRunning(context.Context, string) (bool, error) {
	return false, nil
}
func (fakeContainerManager) WaitForReady(context.Context, string, time.Duration) error { return nil }
func (fakeContainerManager) StopContainer(context.Context, string) error               { return nil }
func (fakeContainerManager) RemoveContainer(context.Context, string) error             { return nil }
func (fakeContainerManager) ListForgeContainers(context.Context) ([]container.ContainerInfo, error) {
	return nil, nil
}
func (fakeContainerManager) PullImage(context.Context, string) error          { return nil }
func (fakeContainerManager) ImageExists(context.Context, string) (bool, error) { return true, nil }
func (fakeContainerManager) ContainerLogs(context.Context, string) (string, error) {
	return "", nil
}
func (fakeContainerManager) Close() error { return nil }

// TestStartSession_NonInteractive runs startSession against a fake container
// manager from inside a temporary git project. The agent container does not
// really exist, so the docker wait/logs calls fail harmlessly; the function
// should still complete the session and clean up without error.
func TestStartSession_NonInteractive(t *testing.T) {
	homeDir := t.TempDir()
	require.NoError(t, os.MkdirAll(homeDir+"/.claude", 0o755))
	require.NoError(t, os.MkdirAll(homeDir+"/.config/claude-forge", 0o755))

	// A git project in the working directory, since startSession lets the
	// orchestrator resolve the project from the current directory.
	projectDir := t.TempDir()
	runGitIn(t, projectDir, "init")
	runGitIn(t, projectDir, "remote", "add", "origin", "git@github.com:test-owner/test-repo.git")

	origWD, err := os.Getwd()
	require.NoError(t, err)
	require.NoError(t, os.Chdir(projectDir))
	t.Cleanup(func() { _ = os.Chdir(origWD) })

	t.Setenv("ANTHROPIC_API_KEY", "sk-ant-test-key")

	original := createOrchestrator
	createOrchestrator = func() (*forge.Orchestrator, func(), error) {
		orch := forge.NewOrchestrator(fakeContainerManager{}, homeDir)
		orch.Log = func(string, ...any) {}
		return orch, func() {}, nil
	}
	t.Cleanup(func() { createOrchestrator = original })

	// Non-interactive (prompt set) so we don't try to attach to a TTY.
	err = startSession(true, false, "do a task", "", "", false, nil, "", "")
	require.NoError(t, err)
}

func runGitIn(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, "git %v: %s", args, string(out))
}

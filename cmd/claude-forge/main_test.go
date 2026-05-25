package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/michael-freling/claude-code-tools/internal/forge"
	"github.com/michael-freling/claude-code-tools/internal/forge/container"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewRootCmd(t *testing.T) {
	cmd := newRootCmd()

	assert.Equal(t, "claude-forge", cmd.Use)

	expectedSubcommands := []string{
		"init", "start", "resume", "stop", "status",
		"build", "auth", "plugins", "version", "gateway", "kube", "mcp",
	}

	subcommandNames := make(map[string]bool)
	for _, sub := range cmd.Commands() {
		subcommandNames[sub.Use] = true
	}

	for _, expected := range expectedSubcommands {
		found := false
		for _, sub := range cmd.Commands() {
			// cmd.Use may include argument specs like "resume [session-id]",
			// so match on the first word.
			name := sub.Name()
			if name == expected {
				found = true
				break
			}
		}
		assert.True(t, found, "expected subcommand %q not found", expected)
	}
}

func TestNewStartCmd(t *testing.T) {
	cmd := newStartCmd()

	assert.Equal(t, "start", cmd.Use)

	worktreeFlag := cmd.Flags().Lookup("worktree")
	require.NotNil(t, worktreeFlag)
	assert.Equal(t, "false", worktreeFlag.DefValue)

	noSkipFlag := cmd.Flags().Lookup("no-skip-permissions")
	require.NotNil(t, noSkipFlag)
	assert.Equal(t, "false", noSkipFlag.DefValue)

	promptFlag := cmd.Flags().Lookup("prompt")
	require.NotNil(t, promptFlag)
	assert.Equal(t, "", promptFlag.DefValue)
	assert.Equal(t, "p", promptFlag.Shorthand)
}

func TestNewResumeCmd(t *testing.T) {
	cmd := newResumeCmd()

	assert.Equal(t, "resume [session-id]", cmd.Use)

	listFlag := cmd.Flags().Lookup("list")
	require.NotNil(t, listFlag)
	assert.Equal(t, "false", listFlag.DefValue)

	// Verify MaximumNArgs(1) is set by checking the Args validator.
	require.NotNil(t, cmd.Args)
}

func TestNewGatewayCmd(t *testing.T) {
	cmd := newGatewayCmd()

	assert.Equal(t, "gateway", cmd.Use)

	ownerFlag := cmd.Flags().Lookup("owner")
	require.NotNil(t, ownerFlag)
	assert.Equal(t, "", ownerFlag.DefValue)

	repoFlag := cmd.Flags().Lookup("repo")
	require.NotNil(t, repoFlag)
	assert.Equal(t, "", repoFlag.DefValue)

	proxyAddrFlag := cmd.Flags().Lookup("proxy-addr")
	require.NotNil(t, proxyAddrFlag)
	assert.Equal(t, ":8080", proxyAddrFlag.DefValue)
}

func TestNewVersionCmd(t *testing.T) {
	cmd := newVersionCmd()

	assert.Equal(t, "version", cmd.Use)

	// The version command uses fmt.Printf (writes to os.Stdout), so we
	// capture it by redirecting stdout via a pipe.
	oldStdout := os.Stdout
	r, w, err := os.Pipe()
	require.NoError(t, err)

	os.Stdout = w
	execErr := cmd.Execute()
	w.Close()
	os.Stdout = oldStdout

	require.NoError(t, execErr)

	var buf bytes.Buffer
	_, err = io.Copy(&buf, r)
	require.NoError(t, err)

	assert.Contains(t, buf.String(), "claude-forge version")
}

func TestNewStopCmd(t *testing.T) {
	cmd := newStopCmd()
	assert.Equal(t, "stop", cmd.Use)
}

func TestNewStatusCmd(t *testing.T) {
	cmd := newStatusCmd()
	assert.Equal(t, "status", cmd.Use)
}

func TestNewBuildCmd(t *testing.T) {
	cmd := newBuildCmd()
	assert.Equal(t, "build", cmd.Use)
}

func TestNewAuthCmd(t *testing.T) {
	cmd := newAuthCmd()
	assert.Equal(t, "auth", cmd.Use)
}

// --- stubContainerManager implements container.ContainerManager for testing ---

type stubContainerManager struct {
	containers []container.ContainerInfo
}

func (s *stubContainerManager) CreateNetwork(_ context.Context, _ string) (string, error) {
	return "net-id", nil
}
func (s *stubContainerManager) RemoveNetwork(_ context.Context, _ string) error { return nil }
func (s *stubContainerManager) StartAgent(_ context.Context, _ container.AgentOptions) (string, error) {
	return "agent-id", nil
}
func (s *stubContainerManager) StartGateway(_ context.Context, _ container.GatewayOptions) (string, error) {
	return "gw-id", nil
}
func (s *stubContainerManager) WaitForReady(_ context.Context, _ string, _ time.Duration) error {
	return nil
}
func (s *stubContainerManager) StopContainer(_ context.Context, _ string) error   { return nil }
func (s *stubContainerManager) RemoveContainer(_ context.Context, _ string) error { return nil }
func (s *stubContainerManager) ListForgeContainers(_ context.Context) ([]container.ContainerInfo, error) {
	return s.containers, nil
}
func (s *stubContainerManager) PullImage(_ context.Context, _ string) error { return nil }
func (s *stubContainerManager) ImageExists(_ context.Context, _ string) (bool, error) {
	return true, nil
}
func (s *stubContainerManager) ContainerLogs(_ context.Context, _ string) (string, error) {
	return "", nil
}
func (s *stubContainerManager) StartGitHubMCP(_ context.Context, _ container.GitHubMCPOptions) (string, error) {
	return "mcp-id", nil
}
func (s *stubContainerManager) StartDockerMCP(_ context.Context, _ container.DockerMCPOptions) (string, error) {
	return "docker-mcp-id", nil
}
func (s *stubContainerManager) EnsureSharedNetwork(_ context.Context, _ string) (string, error) {
	return "shared-net-id", nil
}
func (s *stubContainerManager) ConnectNetwork(_ context.Context, _, _ string, _ []string) error {
	return nil
}
func (s *stubContainerManager) IsContainerRunning(_ context.Context, _ string) (bool, error) {
	return false, nil
}
func (s *stubContainerManager) StartSharedService(_ context.Context, _ container.SharedServiceOptions) (string, error) {
	return "shared-id", nil
}
func (s *stubContainerManager) Close() error { return nil }

// errorContainerManager returns an error for ListForgeContainers and PullImage.
type errorContainerManager struct {
	stubContainerManager
	err error
}

func (e *errorContainerManager) ListForgeContainers(_ context.Context) ([]container.ContainerInfo, error) {
	return nil, e.err
}
func (e *errorContainerManager) PullImage(_ context.Context, _ string) error {
	return e.err
}

// setupTestOrchestrator overrides createOrchestrator with a stub and restores
// it when the test finishes. Returns the home directory used.
func setupTestOrchestrator(t *testing.T, cm container.ContainerManager) string {
	t.Helper()
	homeDir := t.TempDir()

	// Create directories the orchestrator expects
	configDir := filepath.Join(homeDir, ".config", "claude-forge")
	require.NoError(t, os.MkdirAll(configDir, 0o755))
	claudeDir := filepath.Join(homeDir, ".claude")
	require.NoError(t, os.MkdirAll(claudeDir, 0o755))

	original := createOrchestrator
	createOrchestrator = func() (*forge.Orchestrator, func(), error) {
		orch := forge.NewOrchestrator(cm, homeDir)
		return orch, func() {}, nil
	}
	t.Cleanup(func() { createOrchestrator = original })

	return homeDir
}

// setupTestGitRepo creates a temporary git repo with a remote origin.
func setupTestGitRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()

	runGitCmd(t, dir, "init")
	runGitCmd(t, dir, "config", "user.email", "test@test.com")
	runGitCmd(t, dir, "config", "user.name", "Test User")
	runGitCmd(t, dir, "remote", "add", "origin", "git@github.com:test-owner/test-repo.git")

	return dir
}

func runGitCmd(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	output, err := cmd.CombinedOutput()
	require.NoError(t, err, "git %v failed: %s", args, string(output))
}

// captureStdout captures os.Stdout during fn execution.
func captureStdout(t *testing.T, fn func()) string {
	t.Helper()

	oldStdout := os.Stdout
	r, w, err := os.Pipe()
	require.NoError(t, err)

	os.Stdout = w
	fn()
	w.Close()
	os.Stdout = oldStdout

	var buf bytes.Buffer
	_, err = io.Copy(&buf, r)
	require.NoError(t, err)

	return buf.String()
}

func TestStopCmd_Execute(t *testing.T) {
	setupTestOrchestrator(t, &stubContainerManager{})
	repoDir := setupTestGitRepo(t)

	origDir, err := os.Getwd()
	require.NoError(t, err)
	require.NoError(t, os.Chdir(repoDir))
	t.Cleanup(func() { os.Chdir(origDir) })

	cmd := newStopCmd()
	cmd.SetArgs([]string{})

	output := captureStdout(t, func() {
		err = cmd.Execute()
	})
	require.NoError(t, err)
	assert.Contains(t, output, "No running containers found")
}

func TestStatusCmd_Execute_Empty(t *testing.T) {
	setupTestOrchestrator(t, &stubContainerManager{})

	cmd := newStatusCmd()
	cmd.SetArgs([]string{})

	output := captureStdout(t, func() {
		err := cmd.Execute()
		require.NoError(t, err)
	})
	assert.Contains(t, output, "No running forge containers.")
}

func TestStatusCmd_Execute_WithContainers(t *testing.T) {
	created := time.Date(2025, 1, 15, 10, 30, 0, 0, time.UTC)
	cm := &stubContainerManager{
		containers: []container.ContainerInfo{
			{
				Name:    "forge-agent-testproj-abc12345",
				ID:      "abc123",
				Image:   "ghcr.io/test/agent:latest",
				Status:  "running",
				Created: created,
			},
			{
				Name:    "forge-gateway-testproj-abc12345",
				ID:      "def456",
				Image:   "ghcr.io/test/gateway:latest",
				Status:  "running",
				Created: created,
			},
		},
	}
	setupTestOrchestrator(t, cm)

	cmd := newStatusCmd()
	cmd.SetArgs([]string{})

	output := captureStdout(t, func() {
		err := cmd.Execute()
		require.NoError(t, err)
	})
	assert.Contains(t, output, "forge-agent-testproj-abc12345")
	assert.Contains(t, output, "forge-gateway-testproj-abc12345")
	assert.Contains(t, output, "NAME")
	assert.Contains(t, output, "IMAGE")
	assert.Contains(t, output, "STATUS")
}

func TestBuildCmd_Execute(t *testing.T) {
	setupTestOrchestrator(t, &stubContainerManager{})

	cmd := newBuildCmd()
	cmd.SetArgs([]string{})

	output := captureStdout(t, func() {
		err := cmd.Execute()
		require.NoError(t, err)
	})
	assert.Contains(t, output, "Pulling image")
	assert.Contains(t, output, "All images up to date")
}

func TestGatewayCmd_MissingFlags(t *testing.T) {
	cmd := newGatewayCmd()
	cmd.SetArgs([]string{})

	err := cmd.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "--owner and --repo are required")
}

func TestGatewayCmd_MissingOwner(t *testing.T) {
	cmd := newGatewayCmd()
	cmd.SetArgs([]string{"--repo=test-repo"})

	err := cmd.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "--owner and --repo are required")
}

func TestGatewayCmd_MissingRepo(t *testing.T) {
	cmd := newGatewayCmd()
	cmd.SetArgs([]string{"--owner=test-owner"})

	err := cmd.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "--owner and --repo are required")
}

func TestResumeCmd_List_NoSessions(t *testing.T) {
	setupTestOrchestrator(t, &stubContainerManager{})
	repoDir := setupTestGitRepo(t)

	origDir, err := os.Getwd()
	require.NoError(t, err)
	require.NoError(t, os.Chdir(repoDir))
	t.Cleanup(func() { os.Chdir(origDir) })

	cmd := newResumeCmd()
	cmd.SetArgs([]string{"--list"})

	output := captureStdout(t, func() {
		err = cmd.Execute()
	})
	require.NoError(t, err)
	assert.Contains(t, output, "No sessions found.")
}

func TestResumeCmd_List_WithSessions(t *testing.T) {
	setupTestOrchestrator(t, &stubContainerManager{})
	repoDir := setupTestGitRepo(t)

	origDir, err := os.Getwd()
	require.NoError(t, err)
	require.NoError(t, os.Chdir(repoDir))
	t.Cleanup(func() { os.Chdir(origDir) })

	// project.Identify derives ID from the directory path: strings.ReplaceAll(dir, "/", "-")
	projID := strings.ReplaceAll(repoDir, "/", "-")

	homeDir, err := os.UserHomeDir()
	require.NoError(t, err)

	sessionDir := filepath.Join(homeDir, ".claude-forge", projID)
	require.NoError(t, os.MkdirAll(sessionDir, 0o755))
	t.Cleanup(func() { os.RemoveAll(sessionDir) })

	// Write a minimal JSONL session file (real Claude Code format)
	sessionFile := filepath.Join(sessionDir, "abc12345.jsonl")
	writeSessionFile(t, sessionFile, "2025-01-15T10:30:01Z", "Hello Claude")

	cmd := newResumeCmd()
	cmd.SetArgs([]string{"--list"})

	output := captureStdout(t, func() {
		err = cmd.Execute()
	})
	require.NoError(t, err)
	assert.Contains(t, output, "SESSION ID")
	assert.Contains(t, output, "WORKTREE")
	assert.Contains(t, output, "abc12345")
	assert.Contains(t, output, "Hello Claude")
}

func TestResumeCmd_List_WithLongMessage(t *testing.T) {
	setupTestOrchestrator(t, &stubContainerManager{})
	repoDir := setupTestGitRepo(t)

	origDir, err := os.Getwd()
	require.NoError(t, err)
	require.NoError(t, os.Chdir(repoDir))
	t.Cleanup(func() { os.Chdir(origDir) })

	projID := strings.ReplaceAll(repoDir, "/", "-")

	homeDir, err := os.UserHomeDir()
	require.NoError(t, err)

	sessionDir := filepath.Join(homeDir, ".claude-forge", projID)
	require.NoError(t, os.MkdirAll(sessionDir, 0o755))
	t.Cleanup(func() { os.RemoveAll(sessionDir) })

	// Write a session file with a message longer than 60 characters
	longMsg := "This is a very long first message that should be truncated because it exceeds sixty characters in total"
	sessionFile := filepath.Join(sessionDir, "def67890.jsonl")
	writeSessionFile(t, sessionFile, "2025-01-15T10:30:01Z", longMsg)

	cmd := newResumeCmd()
	cmd.SetArgs([]string{"--list"})

	output := captureStdout(t, func() {
		err = cmd.Execute()
	})
	require.NoError(t, err)
	assert.Contains(t, output, "...")
	assert.NotContains(t, output, longMsg) // Should be truncated
}

func TestStatusCmd_ListError(t *testing.T) {
	// Create a stub that returns an error from ListForgeContainers
	errCM := &errorContainerManager{err: fmt.Errorf("list containers error")}
	setupTestOrchestrator(t, errCM)

	cmd := newStatusCmd()
	cmd.SetArgs([]string{})

	err := cmd.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "list containers error")
}

func TestResumeCmd_NotInGitRepo(t *testing.T) {
	setupTestOrchestrator(t, &stubContainerManager{})

	// Use a temp dir that is NOT a git repo
	dir := t.TempDir()
	origDir, err := os.Getwd()
	require.NoError(t, err)
	require.NoError(t, os.Chdir(dir))
	t.Cleanup(func() { os.Chdir(origDir) })

	cmd := newResumeCmd()
	cmd.SetArgs([]string{"--list"})

	err = cmd.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to identify project")
}

func TestResumeCmd_List_ShowsWorktreeName(t *testing.T) {
	setupTestOrchestrator(t, &stubContainerManager{})
	repoDir := setupTestGitRepo(t)

	origDir, err := os.Getwd()
	require.NoError(t, err)
	require.NoError(t, os.Chdir(repoDir))
	t.Cleanup(func() { os.Chdir(origDir) })

	projID := strings.ReplaceAll(repoDir, "/", "-")

	homeDir, err := os.UserHomeDir()
	require.NoError(t, err)

	sessionDir := filepath.Join(homeDir, ".claude-forge", projID)
	t.Cleanup(func() { os.RemoveAll(sessionDir) })

	// Create a regular session in -work/
	workDir := filepath.Join(sessionDir, "-work")
	require.NoError(t, os.MkdirAll(workDir, 0o755))
	writeSessionFile(t, filepath.Join(workDir, "regular-session.jsonl"),
		"2025-01-15T10:30:00Z", "regular work")

	// Create a worktree session in -work--claude-worktrees-feature/
	wtDir := filepath.Join(sessionDir, "-work--claude-worktrees-feature")
	require.NoError(t, os.MkdirAll(wtDir, 0o755))
	writeSessionFile(t, filepath.Join(wtDir, "wt-session.jsonl"),
		"2025-01-15T11:00:00Z", "worktree work")

	cmd := newResumeCmd()
	cmd.SetArgs([]string{"--list"})

	output := captureStdout(t, func() {
		err = cmd.Execute()
	})
	require.NoError(t, err)

	assert.Contains(t, output, "WORKTREE")
	assert.Contains(t, output, "wt-session")
	assert.Contains(t, output, "feature")
	assert.Contains(t, output, "regular-session")
}

func TestResumeCmd_FindSessionNotFound(t *testing.T) {
	setupTestOrchestrator(t, &stubContainerManager{})
	repoDir := setupTestGitRepo(t)

	origDir, err := os.Getwd()
	require.NoError(t, err)
	require.NoError(t, os.Chdir(repoDir))
	t.Cleanup(func() { os.Chdir(origDir) })

	projID := strings.ReplaceAll(repoDir, "/", "-")

	homeDir, err := os.UserHomeDir()
	require.NoError(t, err)

	sessionDir := filepath.Join(homeDir, ".claude-forge", projID)
	require.NoError(t, os.MkdirAll(sessionDir, 0o755))
	t.Cleanup(func() { os.RemoveAll(sessionDir) })

	cmd := newResumeCmd()
	cmd.SetArgs([]string{"nonexistent-session-id"})

	err = cmd.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

func TestResumeCmd_ContinueNoSessions(t *testing.T) {
	setupTestOrchestrator(t, &stubContainerManager{})
	repoDir := setupTestGitRepo(t)

	origDir, err := os.Getwd()
	require.NoError(t, err)
	require.NoError(t, os.Chdir(repoDir))
	t.Cleanup(func() { os.Chdir(origDir) })

	projID := strings.ReplaceAll(repoDir, "/", "-")

	homeDir, err := os.UserHomeDir()
	require.NoError(t, err)

	sessionDir := filepath.Join(homeDir, ".claude-forge", projID)
	require.NoError(t, os.MkdirAll(sessionDir, 0o755))
	t.Cleanup(func() { os.RemoveAll(sessionDir) })

	cmd := newResumeCmd()
	cmd.SetArgs([]string{}) // no args, no --list → continue most recent

	err = cmd.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no sessions found")
}

func writeSessionFile(t *testing.T, path, timestamp, message string) {
	t.Helper()
	lines := []map[string]any{
		{"type": "permission-mode", "permissionMode": "bypassPermissions"},
		{"type": "user", "message": map[string]string{"role": "user", "content": message}, "timestamp": timestamp},
	}
	var content []byte
	for _, line := range lines {
		b, err := json.Marshal(line)
		require.NoError(t, err)
		content = append(content, b...)
		content = append(content, '\n')
	}
	require.NoError(t, os.WriteFile(path, content, 0o644))
}

func TestMcpRestartCmd_NoSharedMCP(t *testing.T) {
	setupTestOrchestrator(t, &stubContainerManager{})

	cmd := newMcpCmd()
	cmd.SetArgs([]string{"restart"})

	output := captureStdout(t, func() {
		err := cmd.Execute()
		require.NoError(t, err)
	})
	assert.Contains(t, output, "No shared MCP servers configured")
}

func TestMcpRestartCmd_OrchestratorError(t *testing.T) {
	original := createOrchestrator
	createOrchestrator = func() (*forge.Orchestrator, func(), error) {
		return nil, nil, fmt.Errorf("test orchestrator error")
	}
	t.Cleanup(func() { createOrchestrator = original })

	cmd := newMcpCmd()
	cmd.SetArgs([]string{"restart"})

	err := cmd.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "test orchestrator error")
}

func TestAuthCmd_WithAPIKey(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "sk-ant-api-test1234567890abcdef")

	cmd := newAuthCmd()
	cmd.SetArgs([]string{})

	output := captureStdout(t, func() {
		err := cmd.Execute()
		require.NoError(t, err)
	})
	assert.Contains(t, output, "Auth type: api_key")
	assert.Contains(t, output, "Token: sk-ant-a...cdef")
}

func TestAuthCmd_WithOAuthToken(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "")
	t.Setenv("CLAUDE_CODE_OAUTH_TOKEN", "oauth-tok-12345")

	cmd := newAuthCmd()
	cmd.SetArgs([]string{})

	output := captureStdout(t, func() {
		err := cmd.Execute()
		require.NoError(t, err)
	})
	assert.Contains(t, output, "Auth type: oauth")
	assert.Contains(t, output, "Token: oauth-to...2345")
}

func TestAuthCmd_WithShortToken(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "short-tok")

	cmd := newAuthCmd()
	cmd.SetArgs([]string{})

	output := captureStdout(t, func() {
		err := cmd.Execute()
		require.NoError(t, err)
	})
	assert.Contains(t, output, "Auth type: api_key")
	assert.Contains(t, output, "Token: [present]")
}

func TestBuildCmd_PullError(t *testing.T) {
	errCM := &errorContainerManager{err: fmt.Errorf("pull image error")}
	setupTestOrchestrator(t, errCM)

	cmd := newBuildCmd()
	cmd.SetArgs([]string{})

	err := cmd.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "pull image error")
}

func TestGatewayCmd_AuthFailure(t *testing.T) {
	// Clear any auth tokens so NewServer will fail
	t.Setenv("GITHUB_TOKEN", "")
	t.Setenv("GH_TOKEN", "")
	t.Setenv("HOME", t.TempDir())

	cmd := newGatewayCmd()
	cmd.SetArgs([]string{"--owner=test-owner", "--repo=test-repo"})

	err := cmd.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to create gateway server")
}

func TestGatewayCmd_ServerError(t *testing.T) {
	// Set a GITHUB_TOKEN so NewServer succeeds
	t.Setenv("GITHUB_TOKEN", "ghp_test_gateway_token")

	cmd := newGatewayCmd()
	// Use an invalid address to cause an immediate server error
	cmd.SetArgs([]string{
		"--owner=test-owner",
		"--repo=test-repo",
		"--proxy-addr=invalid-address-:::::",
	})

	output := captureStdout(t, func() {
		err := cmd.Execute()
		require.Error(t, err)
	})
	assert.Contains(t, output, "Gateway starting")
}

func TestStatusCmd_OrchestratorError(t *testing.T) {
	original := createOrchestrator
	createOrchestrator = func() (*forge.Orchestrator, func(), error) {
		return nil, nil, fmt.Errorf("test orchestrator error")
	}
	t.Cleanup(func() { createOrchestrator = original })

	cmd := newStatusCmd()
	cmd.SetArgs([]string{})

	err := cmd.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "test orchestrator error")
}

func TestBuildCmd_OrchestratorError(t *testing.T) {
	original := createOrchestrator
	createOrchestrator = func() (*forge.Orchestrator, func(), error) {
		return nil, nil, fmt.Errorf("test orchestrator error")
	}
	t.Cleanup(func() { createOrchestrator = original })

	cmd := newBuildCmd()
	cmd.SetArgs([]string{})

	err := cmd.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "test orchestrator error")
}

func TestStopCmd_OrchestratorError(t *testing.T) {
	original := createOrchestrator
	createOrchestrator = func() (*forge.Orchestrator, func(), error) {
		return nil, nil, fmt.Errorf("test orchestrator error")
	}
	t.Cleanup(func() { createOrchestrator = original })

	cmd := newStopCmd()
	cmd.SetArgs([]string{})

	err := cmd.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "test orchestrator error")
}

func TestStopCmd_NotInGitRepo(t *testing.T) {
	setupTestOrchestrator(t, &stubContainerManager{})

	dir := t.TempDir()
	origDir, err := os.Getwd()
	require.NoError(t, err)
	require.NoError(t, os.Chdir(dir))
	t.Cleanup(func() { os.Chdir(origDir) })

	cmd := newStopCmd()
	cmd.SetArgs([]string{})

	err = cmd.Execute()
	require.Error(t, err)
}

func TestReadHostPlugins(t *testing.T) {
	t.Run("valid plugins file", func(t *testing.T) {
		homeDir := t.TempDir()
		pluginsDir := filepath.Join(homeDir, ".claude", "plugins")
		require.NoError(t, os.MkdirAll(pluginsDir, 0o755))

		content := `{
			"version": 2,
			"plugins": {
				"gopls-lsp@claude-plugins-official": [{"version": "1.0.0"}],
				"my-plugin@my-marketplace": [{"version": "0.1.0"}]
			}
		}`
		require.NoError(t, os.WriteFile(filepath.Join(pluginsDir, "installed_plugins.json"), []byte(content), 0o644))

		plugins, err := readHostPlugins(homeDir)
		require.NoError(t, err)
		assert.Len(t, plugins, 2)
		assert.Contains(t, plugins, "gopls-lsp@claude-plugins-official")
		assert.Contains(t, plugins, "my-plugin@my-marketplace")
	})

	t.Run("no plugins file", func(t *testing.T) {
		homeDir := t.TempDir()
		plugins, err := readHostPlugins(homeDir)
		require.NoError(t, err)
		assert.Empty(t, plugins)
	})

	t.Run("empty plugins map", func(t *testing.T) {
		homeDir := t.TempDir()
		pluginsDir := filepath.Join(homeDir, ".claude", "plugins")
		require.NoError(t, os.MkdirAll(pluginsDir, 0o755))

		content := `{"version": 2, "plugins": {}}`
		require.NoError(t, os.WriteFile(filepath.Join(pluginsDir, "installed_plugins.json"), []byte(content), 0o644))

		plugins, err := readHostPlugins(homeDir)
		require.NoError(t, err)
		assert.Empty(t, plugins)
	})

	t.Run("invalid json", func(t *testing.T) {
		homeDir := t.TempDir()
		pluginsDir := filepath.Join(homeDir, ".claude", "plugins")
		require.NoError(t, os.MkdirAll(pluginsDir, 0o755))

		require.NoError(t, os.WriteFile(filepath.Join(pluginsDir, "installed_plugins.json"), []byte("not json"), 0o644))

		_, err := readHostPlugins(homeDir)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "failed to parse")
	})
}

func TestPluginsSyncCmd_NoPlugins(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)

	cmd := newPluginsSyncCmd()
	err := cmd.Execute()
	require.NoError(t, err)
}

func TestPluginsSyncCmd_WithPlugins(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)

	pluginsDir := filepath.Join(homeDir, ".claude", "plugins")
	require.NoError(t, os.MkdirAll(pluginsDir, 0o755))
	content := `{"version": 2, "plugins": {"gopls-lsp@claude-plugins-official": [{}]}}`
	require.NoError(t, os.WriteFile(filepath.Join(pluginsDir, "installed_plugins.json"), []byte(content), 0o644))

	// Create config dir so config.Load succeeds
	configDir := filepath.Join(homeDir, ".config", "claude-forge")
	require.NoError(t, os.MkdirAll(configDir, 0o755))

	original := pluginsSyncRun
	var capturedPlugins []string
	pluginsSyncRun = func(cmd *cobra.Command, args []string) error {
		// Call the real readHostPlugins to verify it works, but skip Docker
		plugins, err := readHostPlugins(homeDir)
		if err != nil {
			return err
		}
		capturedPlugins = plugins
		return nil
	}
	t.Cleanup(func() { pluginsSyncRun = original })

	cmd := newPluginsSyncCmd()
	err := cmd.Execute()
	require.NoError(t, err)
	assert.Equal(t, []string{"gopls-lsp@claude-plugins-official"}, capturedPlugins)
}

func TestReadHostMarketplaces(t *testing.T) {
	t.Run("reads github sources and names", func(t *testing.T) {
		dir := t.TempDir()
		content := `{
			"claude-code-plugins": {"source": {"source": "github", "repo": "anthropics/claude-code"}},
			"custom": {"source": {"source": "github", "repo": "org/custom-plugins"}}
		}`
		require.NoError(t, os.WriteFile(filepath.Join(dir, "known_marketplaces.json"), []byte(content), 0o644))

		info := readHostMarketplaces(dir)
		assert.Len(t, info.Sources, 2)
		assert.Contains(t, info.Sources, "anthropics/claude-code")
		assert.Contains(t, info.Sources, "org/custom-plugins")
		assert.True(t, info.Names["claude-code-plugins"])
		assert.True(t, info.Names["custom"])
	})

	t.Run("extracts repo from directory path with github.com", func(t *testing.T) {
		dir := t.TempDir()
		content := `{
			"michael-freling": {"source": {"source": "directory", "path": "/home/user/src/github.com/michael-freling/claude-code-config/.claude/worktrees/main"}},
			"remote": {"source": {"source": "github", "repo": "anthropics/claude-code"}}
		}`
		require.NoError(t, os.WriteFile(filepath.Join(dir, "known_marketplaces.json"), []byte(content), 0o644))

		info := readHostMarketplaces(dir)
		assert.Len(t, info.Sources, 2)
		assert.Contains(t, info.Sources, "michael-freling/claude-code-config")
		assert.Contains(t, info.Sources, "anthropics/claude-code")
		assert.True(t, info.Names["michael-freling"])
		assert.True(t, info.Names["remote"])
	})

	t.Run("skips directory sources without github.com in path", func(t *testing.T) {
		dir := t.TempDir()
		content := `{
			"local": {"source": {"source": "directory", "path": "/some/local/path"}},
			"remote": {"source": {"source": "github", "repo": "anthropics/claude-code"}}
		}`
		require.NoError(t, os.WriteFile(filepath.Join(dir, "known_marketplaces.json"), []byte(content), 0o644))

		info := readHostMarketplaces(dir)
		assert.Equal(t, []string{"anthropics/claude-code"}, info.Sources)
		assert.True(t, info.Names["remote"])
		assert.False(t, info.Names["local"])
	})

	t.Run("returns empty when file missing", func(t *testing.T) {
		info := readHostMarketplaces(t.TempDir())
		assert.Empty(t, info.Sources)
		assert.Empty(t, info.Names)
	})
}

func TestEnablePluginsInSettings(t *testing.T) {
	t.Run("adds enabledPlugins to existing settings", func(t *testing.T) {
		dir := t.TempDir()
		initial := `{
  "model": "opus",
  "autoUpdaterStatus": "disabled"
}`
		require.NoError(t, os.WriteFile(filepath.Join(dir, "settings.json"), []byte(initial), 0o644))

		err := enablePluginsInSettings(dir, []string{"foo@bar", "baz@qux"})
		require.NoError(t, err)

		data, err := os.ReadFile(filepath.Join(dir, "settings.json"))
		require.NoError(t, err)

		var settings map[string]any
		require.NoError(t, json.Unmarshal(data, &settings))
		enabled, ok := settings["enabledPlugins"].(map[string]any)
		require.True(t, ok)
		assert.Equal(t, true, enabled["foo@bar"])
		assert.Equal(t, true, enabled["baz@qux"])
		assert.Equal(t, "opus", settings["model"])
	})

	t.Run("returns error when file missing", func(t *testing.T) {
		err := enablePluginsInSettings(t.TempDir(), []string{"foo@bar"})
		assert.Error(t, err)
	})
}

func TestBuildConfigTemplate(t *testing.T) {
	t.Run("without kubeconfig", func(t *testing.T) {
		homeDir := t.TempDir()
		content := buildConfigTemplate(homeDir)

		assert.Contains(t, content, "images:")
		assert.Contains(t, content, "defaults:")
		assert.Contains(t, content, "# kubernetes:")
		assert.Contains(t, content, "#   default_context: my-cluster")
	})

	t.Run("with kubeconfig", func(t *testing.T) {
		homeDir := t.TempDir()
		kubeDir := filepath.Join(homeDir, ".kube")
		require.NoError(t, os.MkdirAll(kubeDir, 0o755))
		kubeconfig := `apiVersion: v1
kind: Config
clusters:
  - name: prod-cluster
    cluster:
      server: https://prod.example.com
  - name: dev-cluster
    cluster:
      server: https://dev.example.com
contexts:
  - name: prod
    context:
      cluster: prod-cluster
      user: admin
  - name: dev
    context:
      cluster: dev-cluster
      user: admin
current-context: prod
users:
  - name: admin
    user:
      token: fake
`
		require.NoError(t, os.WriteFile(filepath.Join(kubeDir, "config"), []byte(kubeconfig), 0o644))

		content := buildConfigTemplate(homeDir)

		assert.Contains(t, content, "#   default_context: prod")
		assert.Contains(t, content, "#     - host_context: prod")
		assert.Contains(t, content, "#     - host_context: dev")
		assert.Contains(t, content, "#       service_account_name: claude-forge-agent")
	})

	t.Run("respects KUBECONFIG env", func(t *testing.T) {
		homeDir := t.TempDir()
		customPath := filepath.Join(homeDir, "custom-kubeconfig")
		kubeconfig := `apiVersion: v1
kind: Config
contexts:
  - name: custom-ctx
    context:
      cluster: c
      user: u
clusters:
  - name: c
    cluster:
      server: https://custom.example.com
users:
  - name: u
    user:
      token: t
`
		require.NoError(t, os.WriteFile(customPath, []byte(kubeconfig), 0o644))
		t.Setenv("KUBECONFIG", customPath)

		content := buildConfigTemplate(homeDir)
		assert.Contains(t, content, "#     - host_context: custom-ctx")
	})
}

func TestInitCmd(t *testing.T) {
	t.Run("creates config file", func(t *testing.T) {
		homeDir := t.TempDir()
		t.Setenv("HOME", homeDir)

		cmd := newInitCmd()
		cmd.SetArgs([]string{})
		require.NoError(t, cmd.Execute())

		configPath := filepath.Join(homeDir, ".config", "claude-forge", "config.yaml")
		data, err := os.ReadFile(configPath)
		require.NoError(t, err)
		assert.Contains(t, string(data), "images:")
	})

	t.Run("refuses to overwrite without force", func(t *testing.T) {
		homeDir := t.TempDir()
		t.Setenv("HOME", homeDir)

		cmd := newInitCmd()
		cmd.SetArgs([]string{})
		require.NoError(t, cmd.Execute())

		cmd2 := newInitCmd()
		cmd2.SetArgs([]string{})
		err := cmd2.Execute()
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "use --force to overwrite")
	})

	t.Run("overwrites with force", func(t *testing.T) {
		homeDir := t.TempDir()
		t.Setenv("HOME", homeDir)

		cmd := newInitCmd()
		cmd.SetArgs([]string{})
		require.NoError(t, cmd.Execute())

		cmd2 := newInitCmd()
		cmd2.SetArgs([]string{"--force"})
		require.NoError(t, cmd2.Execute())
	})
}

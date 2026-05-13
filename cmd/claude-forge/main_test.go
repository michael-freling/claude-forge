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
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewRootCmd(t *testing.T) {
	cmd := newRootCmd()

	assert.Equal(t, "claude-forge", cmd.Use)

	expectedSubcommands := []string{
		"start", "resume", "stop", "status",
		"build", "auth", "version", "gateway", "forge-gh",
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

	apiAddrFlag := cmd.Flags().Lookup("api-addr")
	require.NotNil(t, apiAddrFlag)
	assert.Equal(t, ":8083", apiAddrFlag.DefValue)
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

func TestNewForgeGHCmd(t *testing.T) {
	cmd := newForgeGHCmd()

	assert.Equal(t, "forge-gh", cmd.Use)
	assert.True(t, cmd.DisableFlagParsing)
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

	// Write a minimal JSONL session file
	sessionFile := filepath.Join(sessionDir, "abc12345.jsonl")
	lines := []map[string]string{
		{"type": "system", "timestamp": "2025-01-15T10:30:00Z", "message": "Session started"},
		{"type": "human", "timestamp": "2025-01-15T10:30:01Z", "message": "Hello Claude"},
	}
	var content []byte
	for _, line := range lines {
		b, err := json.Marshal(line)
		require.NoError(t, err)
		content = append(content, b...)
		content = append(content, '\n')
	}
	require.NoError(t, os.WriteFile(sessionFile, content, 0o644))

	cmd := newResumeCmd()
	cmd.SetArgs([]string{"--list"})

	output := captureStdout(t, func() {
		err = cmd.Execute()
	})
	require.NoError(t, err)
	assert.Contains(t, output, "SESSION ID")
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
	lines := []map[string]string{
		{"type": "system", "timestamp": "2025-01-15T10:30:00Z", "message": "Session started"},
		{"type": "human", "timestamp": "2025-01-15T10:30:01Z", "message": longMsg},
	}
	var content []byte
	for _, line := range lines {
		b, err := json.Marshal(line)
		require.NoError(t, err)
		content = append(content, b...)
		content = append(content, '\n')
	}
	require.NoError(t, os.WriteFile(sessionFile, content, 0o644))

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

func TestForgeGHCmd_Execute_NoGateway(t *testing.T) {
	original := forgeGHGatewayURL
	forgeGHGatewayURL = "http://127.0.0.1:1" // port 1 is always refused
	t.Cleanup(func() { forgeGHGatewayURL = original })

	cmd := newForgeGHCmd()
	cmd.SetArgs([]string{"pr", "list"})

	err := cmd.Execute()
	require.Error(t, err)
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
		"--api-addr=invalid-address-:::::",
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

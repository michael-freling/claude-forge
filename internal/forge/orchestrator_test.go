package forge

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/michael-freling/claude-forge/internal/forge/claudecode"
	"github.com/michael-freling/claude-forge/internal/forge/config"
	"github.com/michael-freling/claude-forge/internal/forge/container"
	"github.com/michael-freling/claude-forge/internal/forge/session"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
)

// setupGitProject creates a temp directory with a git remote configured.
func setupGitProject(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	runGit(t, dir, "init")
	runGit(t, dir, "remote", "add", "origin", "git@github.com:test-owner/test-repo.git")
	return dir
}

// runGit runs a git command in the given directory.
func runGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	output, err := cmd.CombinedOutput()
	require.NoError(t, err, "git %v failed: %s", args, string(output))
}

// setupOrchestrator creates an Orchestrator with temp directories and a silent logger.
func setupOrchestrator(t *testing.T, mock *MockContainerManager) (*Orchestrator, string) {
	t.Helper()
	homeDir := t.TempDir()

	// Create required directories
	require.NoError(t, os.MkdirAll(filepath.Join(homeDir, ".claude"), 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(homeDir, ".config", "claude-forge"), 0o755))

	orch := NewOrchestrator(mock, homeDir)
	orch.Log = func(format string, args ...any) {} // silent in tests
	return orch, homeDir
}

func TestStart_Success(t *testing.T) {
	ctrl := gomock.NewController(t)

	mockCM := NewMockContainerManager(ctrl)
	orch, _ := setupOrchestrator(t, mockCM)

	projectDir := setupGitProject(t)
	t.Setenv("ANTHROPIC_API_KEY", "sk-ant-test-key-123")

	// Mock Docker calls
	mockCM.EXPECT().ImageExists(gomock.Any(), gomock.Any()).Return(true, nil).Times(3)
	mockCM.EXPECT().CreateNetwork(gomock.Any(), gomock.Any()).Return("net-id", nil)
	mockCM.EXPECT().StartGateway(gomock.Any(), gomock.Any()).Return("gw-id", nil)
	mockCM.EXPECT().WaitForReady(gomock.Any(), "gw-id", gomock.Any()).Return(nil)
	mockCM.EXPECT().StartGitHubMCP(gomock.Any(), gomock.Any()).Return("mcp-id", nil)
	mockCM.EXPECT().WaitForReady(gomock.Any(), "mcp-id", gomock.Any()).Return(nil)
	mockCM.EXPECT().StartAgent(gomock.Any(), gomock.Any()).
		DoAndReturn(func(ctx context.Context, opts container.AgentOptions) (string, error) {
			assert.Equal(t, projectDir, opts.ProjectDir)
			assert.Contains(t, opts.Env, "ANTHROPIC_API_KEY")
			assert.Equal(t, "sk-ant-test-key-123", opts.Env["ANTHROPIC_API_KEY"])
			assert.Contains(t, opts.Cmd, "--dangerously-skip-permissions")
			return "agent-id", nil
		})

	sess, err := orch.Start(context.Background(), StartOptions{
		SkipPermissions: true,
		ProjectDir:      projectDir,
		UID:             1000,
		GID:             1000,
	})

	require.NoError(t, err)
	assert.NotEmpty(t, sess.AgentName)
	assert.NotEmpty(t, sess.GatewayName)
	assert.NotEmpty(t, sess.NetworkName)
	assert.NotEmpty(t, sess.SessionID)
	assert.Contains(t, sess.AgentName, "forge-agent-")
	assert.Contains(t, sess.GatewayName, "forge-gateway-")
	assert.Contains(t, sess.NetworkName, "forge_net_")
}

func TestStart_ImagePull(t *testing.T) {
	ctrl := gomock.NewController(t)

	mockCM := NewMockContainerManager(ctrl)
	orch, _ := setupOrchestrator(t, mockCM)

	projectDir := setupGitProject(t)
	t.Setenv("ANTHROPIC_API_KEY", "sk-ant-test-key")

	// Images don't exist -- expect pull
	mockCM.EXPECT().ImageExists(gomock.Any(), gomock.Any()).Return(false, nil).Times(3)
	mockCM.EXPECT().PullImage(gomock.Any(), gomock.Any()).Return(nil).Times(3)
	mockCM.EXPECT().CreateNetwork(gomock.Any(), gomock.Any()).Return("net-id", nil)
	mockCM.EXPECT().StartGateway(gomock.Any(), gomock.Any()).Return("gw-id", nil)
	mockCM.EXPECT().WaitForReady(gomock.Any(), "gw-id", gomock.Any()).Return(nil)
	mockCM.EXPECT().StartGitHubMCP(gomock.Any(), gomock.Any()).Return("mcp-id", nil)
	mockCM.EXPECT().WaitForReady(gomock.Any(), "mcp-id", gomock.Any()).Return(nil)
	mockCM.EXPECT().StartAgent(gomock.Any(), gomock.Any()).Return("agent-id", nil)

	sess, err := orch.Start(context.Background(), StartOptions{
		ProjectDir: projectDir,
	})

	require.NoError(t, err)
	assert.NotNil(t, sess)
}

func TestStart_FailsOnProjectIdentify(t *testing.T) {
	ctrl := gomock.NewController(t)

	mockCM := NewMockContainerManager(ctrl)
	orch, _ := setupOrchestrator(t, mockCM)

	// Use a non-git directory
	nonGitDir := t.TempDir()
	t.Setenv("ANTHROPIC_API_KEY", "sk-ant-test-key")

	_, err := orch.Start(context.Background(), StartOptions{
		ProjectDir: nonGitDir,
	})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to identify project")
}

func TestStart_FailsOnNetworkCreate(t *testing.T) {
	ctrl := gomock.NewController(t)

	mockCM := NewMockContainerManager(ctrl)
	orch, _ := setupOrchestrator(t, mockCM)

	projectDir := setupGitProject(t)
	t.Setenv("ANTHROPIC_API_KEY", "sk-ant-test-key")

	mockCM.EXPECT().ImageExists(gomock.Any(), gomock.Any()).Return(true, nil).Times(3)
	mockCM.EXPECT().CreateNetwork(gomock.Any(), gomock.Any()).Return("", fmt.Errorf("network create failed"))

	_, err := orch.Start(context.Background(), StartOptions{
		ProjectDir: projectDir,
	})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to create network")
}

func TestStart_FailsOnGatewayStart(t *testing.T) {
	ctrl := gomock.NewController(t)

	mockCM := NewMockContainerManager(ctrl)
	orch, _ := setupOrchestrator(t, mockCM)

	projectDir := setupGitProject(t)
	t.Setenv("ANTHROPIC_API_KEY", "sk-ant-test-key")

	mockCM.EXPECT().ImageExists(gomock.Any(), gomock.Any()).Return(true, nil).Times(3)
	mockCM.EXPECT().CreateNetwork(gomock.Any(), gomock.Any()).Return("net-id", nil)
	mockCM.EXPECT().StartGateway(gomock.Any(), gomock.Any()).Return("", fmt.Errorf("gateway start failed"))

	// Expect cleanup calls (agent, github-mcp, gateway)
	mockCM.EXPECT().StopContainer(gomock.Any(), gomock.Any()).Return(nil).Times(3)
	mockCM.EXPECT().RemoveContainer(gomock.Any(), gomock.Any()).Return(nil).Times(3)
	mockCM.EXPECT().RemoveNetwork(gomock.Any(), gomock.Any()).Return(nil).Times(1)

	_, err := orch.Start(context.Background(), StartOptions{
		ProjectDir: projectDir,
	})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to start gateway")
}

func TestStart_FailsOnGatewayCrash(t *testing.T) {
	ctrl := gomock.NewController(t)

	mockCM := NewMockContainerManager(ctrl)
	orch, _ := setupOrchestrator(t, mockCM)

	projectDir := setupGitProject(t)
	t.Setenv("ANTHROPIC_API_KEY", "sk-ant-test-key")

	mockCM.EXPECT().ImageExists(gomock.Any(), gomock.Any()).Return(true, nil).Times(3)
	mockCM.EXPECT().CreateNetwork(gomock.Any(), gomock.Any()).Return("net-id", nil)
	mockCM.EXPECT().StartGateway(gomock.Any(), gomock.Any()).Return("gw-id", nil)
	mockCM.EXPECT().WaitForReady(gomock.Any(), "gw-id", gomock.Any()).Return(fmt.Errorf("container gw-id exited with code 1"))
	mockCM.EXPECT().ContainerLogs(gomock.Any(), "gw-id").Return("Error: no GITHUB_TOKEN set", nil)

	// Expect cleanup calls (agent, github-mcp, gateway)
	mockCM.EXPECT().StopContainer(gomock.Any(), gomock.Any()).Return(nil).Times(3)
	mockCM.EXPECT().RemoveContainer(gomock.Any(), gomock.Any()).Return(nil).Times(3)
	mockCM.EXPECT().RemoveNetwork(gomock.Any(), gomock.Any()).Return(nil).Times(1)

	_, err := orch.Start(context.Background(), StartOptions{
		ProjectDir: projectDir,
	})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "gateway container failed to start")
	assert.Contains(t, err.Error(), "no GITHUB_TOKEN set")
}

func TestStart_FailsOnAgentStart(t *testing.T) {
	ctrl := gomock.NewController(t)

	mockCM := NewMockContainerManager(ctrl)
	orch, _ := setupOrchestrator(t, mockCM)

	projectDir := setupGitProject(t)
	t.Setenv("ANTHROPIC_API_KEY", "sk-ant-test-key")

	mockCM.EXPECT().ImageExists(gomock.Any(), gomock.Any()).Return(true, nil).Times(3)
	mockCM.EXPECT().CreateNetwork(gomock.Any(), gomock.Any()).Return("net-id", nil)
	mockCM.EXPECT().StartGateway(gomock.Any(), gomock.Any()).Return("gw-id", nil)
	mockCM.EXPECT().WaitForReady(gomock.Any(), "gw-id", gomock.Any()).Return(nil)
	mockCM.EXPECT().StartGitHubMCP(gomock.Any(), gomock.Any()).Return("mcp-id", nil)
	mockCM.EXPECT().WaitForReady(gomock.Any(), "mcp-id", gomock.Any()).Return(nil)
	mockCM.EXPECT().StartAgent(gomock.Any(), gomock.Any()).Return("", fmt.Errorf("agent start failed"))

	// Expect cleanup calls (agent, github-mcp, gateway)
	mockCM.EXPECT().StopContainer(gomock.Any(), gomock.Any()).Return(nil).Times(3)
	mockCM.EXPECT().RemoveContainer(gomock.Any(), gomock.Any()).Return(nil).Times(3)
	mockCM.EXPECT().RemoveNetwork(gomock.Any(), gomock.Any()).Return(nil).Times(1)

	_, err := orch.Start(context.Background(), StartOptions{
		ProjectDir: projectDir,
	})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to start agent")
}

func TestStop_Success(t *testing.T) {
	ctrl := gomock.NewController(t)

	mockCM := NewMockContainerManager(ctrl)
	orch, _ := setupOrchestrator(t, mockCM)

	projectDir := setupGitProject(t)
	projectID := strings.ReplaceAll(projectDir, "/", "-")

	mockCM.EXPECT().ListForgeContainers(gomock.Any()).Return([]container.ContainerInfo{
		{Name: fmt.Sprintf("forge-agent-%s-abcd1234", projectID)},
		{Name: fmt.Sprintf("forge-gateway-%s-abcd1234", projectID)},
		{Name: fmt.Sprintf("forge-github-mcp-%s-abcd1234", projectID)},
	}, nil)

	// Expect stop + remove for each container
	mockCM.EXPECT().StopContainer(gomock.Any(), gomock.Any()).Return(nil).Times(3)
	mockCM.EXPECT().RemoveContainer(gomock.Any(), gomock.Any()).Return(nil).Times(3)
	// Expect network removal
	mockCM.EXPECT().RemoveNetwork(gomock.Any(), gomock.Any()).Return(nil).Times(1)

	err := orch.Stop(context.Background(), projectDir)
	require.NoError(t, err)
}

func TestStop_NoContainers(t *testing.T) {
	ctrl := gomock.NewController(t)

	mockCM := NewMockContainerManager(ctrl)
	orch, _ := setupOrchestrator(t, mockCM)

	projectDir := setupGitProject(t)

	mockCM.EXPECT().ListForgeContainers(gomock.Any()).Return([]container.ContainerInfo{}, nil)

	err := orch.Stop(context.Background(), projectDir)
	require.NoError(t, err)
}

func TestStatus(t *testing.T) {
	ctrl := gomock.NewController(t)

	mockCM := NewMockContainerManager(ctrl)
	orch, _ := setupOrchestrator(t, mockCM)

	expected := []container.ContainerInfo{
		{
			Name:    "forge-agent-proj-abc12345",
			ID:      "c-1",
			Image:   "agent:latest",
			Status:  "Up 5 minutes",
			Created: time.Now(),
		},
	}

	mockCM.EXPECT().ListForgeContainers(gomock.Any()).Return(expected, nil)

	result, err := orch.Status(context.Background())
	require.NoError(t, err)
	assert.Len(t, result, 1)
	assert.Equal(t, "forge-agent-proj-abc12345", result[0].Name)
}

func TestBuild(t *testing.T) {
	ctrl := gomock.NewController(t)

	mockCM := NewMockContainerManager(ctrl)
	orch, _ := setupOrchestrator(t, mockCM)

	mockCM.EXPECT().PullImage(gomock.Any(), gomock.Any()).Return(nil).Times(3)

	err := orch.Build(context.Background())
	require.NoError(t, err)
}

func TestCleanup(t *testing.T) {
	ctrl := gomock.NewController(t)

	mockCM := NewMockContainerManager(ctrl)
	orch, _ := setupOrchestrator(t, mockCM)

	sess := &Session{
		AgentName:     "forge-agent-proj-sess1234",
		GatewayName:   "forge-gateway-proj-sess1234",
		GitHubMCPName: "forge-github-mcp-proj-sess1234",
		NetworkName:   "forge_net_proj_sess1234",
	}

	mockCM.EXPECT().StopContainer(gomock.Any(), "forge-agent-proj-sess1234").Return(nil)
	mockCM.EXPECT().RemoveContainer(gomock.Any(), "forge-agent-proj-sess1234").Return(nil)
	mockCM.EXPECT().StopContainer(gomock.Any(), "forge-github-mcp-proj-sess1234").Return(nil)
	mockCM.EXPECT().RemoveContainer(gomock.Any(), "forge-github-mcp-proj-sess1234").Return(nil)
	mockCM.EXPECT().StopContainer(gomock.Any(), "forge-gateway-proj-sess1234").Return(nil)
	mockCM.EXPECT().RemoveContainer(gomock.Any(), "forge-gateway-proj-sess1234").Return(nil)
	mockCM.EXPECT().RemoveNetwork(gomock.Any(), "forge_net_proj_sess1234").Return(nil)

	orch.Cleanup(context.Background(), sess)
}

func TestNewOrchestrator(t *testing.T) {
	ctrl := gomock.NewController(t)
	mockCM := NewMockContainerManager(ctrl)

	orch := NewOrchestrator(mockCM, "/home/testuser")

	assert.Equal(t, "/home/testuser", orch.HomeDir)
	assert.Equal(t, "/home/testuser/.config/claude-forge", orch.ConfigDir)
	assert.Equal(t, "/home/testuser/.claude", orch.ClaudeDir)
	assert.NotNil(t, orch.Log)
	assert.Equal(t, mockCM, orch.Containers)

	// Verify the default Log function executes without panic
	orch.Log("test message %s", "arg")
}

func TestStart_Interactive(t *testing.T) {
	tests := []struct {
		name            string
		interactive     bool
		wantInteractive bool
	}{
		{
			name:            "interactive mode passes through to agent options",
			interactive:     true,
			wantInteractive: true,
		},
		{
			name:            "non-interactive mode passes through to agent options",
			interactive:     false,
			wantInteractive: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctrl := gomock.NewController(t)

			mockCM := NewMockContainerManager(ctrl)
			orch, _ := setupOrchestrator(t, mockCM)

			projectDir := setupGitProject(t)
			t.Setenv("ANTHROPIC_API_KEY", "sk-ant-test-key")

			mockCM.EXPECT().ImageExists(gomock.Any(), gomock.Any()).Return(true, nil).Times(3)
			mockCM.EXPECT().CreateNetwork(gomock.Any(), gomock.Any()).Return("net-id", nil)
			mockCM.EXPECT().StartGateway(gomock.Any(), gomock.Any()).Return("gw-id", nil)
			mockCM.EXPECT().WaitForReady(gomock.Any(), "gw-id", gomock.Any()).Return(nil)
			mockCM.EXPECT().StartGitHubMCP(gomock.Any(), gomock.Any()).Return("mcp-id", nil)
			mockCM.EXPECT().WaitForReady(gomock.Any(), "mcp-id", gomock.Any()).Return(nil)
			mockCM.EXPECT().StartAgent(gomock.Any(), gomock.Any()).
				DoAndReturn(func(ctx context.Context, opts container.AgentOptions) (string, error) {
					assert.Equal(t, tt.wantInteractive, opts.Interactive)
					return "agent-id", nil
				})

			sess, err := orch.Start(context.Background(), StartOptions{
				Interactive: tt.interactive,
				ProjectDir:  projectDir,
			})

			require.NoError(t, err)
			assert.NotNil(t, sess)
		})
	}
}

func TestStart_OAuthCredentials(t *testing.T) {
	t.Run("from env var without credentials file", func(t *testing.T) {
		ctrl := gomock.NewController(t)
		mockCM := NewMockContainerManager(ctrl)
		orch, _ := setupOrchestrator(t, mockCM)

		projectDir := setupGitProject(t)
		t.Setenv("ANTHROPIC_API_KEY", "")
		t.Setenv("CLAUDE_CODE_OAUTH_TOKEN", "oauth-token-xyz")

		mockCM.EXPECT().ImageExists(gomock.Any(), gomock.Any()).Return(true, nil).Times(3)
		mockCM.EXPECT().CreateNetwork(gomock.Any(), gomock.Any()).Return("net-id", nil)
		mockCM.EXPECT().StartGateway(gomock.Any(), gomock.Any()).Return("gw-id", nil)
		mockCM.EXPECT().WaitForReady(gomock.Any(), "gw-id", gomock.Any()).Return(nil)
		mockCM.EXPECT().StartGitHubMCP(gomock.Any(), gomock.Any()).Return("mcp-id", nil)
		mockCM.EXPECT().WaitForReady(gomock.Any(), "mcp-id", gomock.Any()).Return(nil)
		mockCM.EXPECT().StartAgent(gomock.Any(), gomock.Any()).
			DoAndReturn(func(ctx context.Context, opts container.AgentOptions) (string, error) {
				assert.Equal(t, "oauth-token-xyz", opts.Env["CLAUDE_CODE_OAUTH_TOKEN"])
				_, hasAPIKey := opts.Env["ANTHROPIC_API_KEY"]
				assert.False(t, hasAPIKey)
				return "agent-id", nil
			})

		sess, err := orch.Start(context.Background(), StartOptions{
			ProjectDir: projectDir,
		})
		require.NoError(t, err)
		assert.NotNil(t, sess)
	})

	t.Run("from credentials file", func(t *testing.T) {
		ctrl := gomock.NewController(t)
		mockCM := NewMockContainerManager(ctrl)
		orch, homeDir := setupOrchestrator(t, mockCM)

		// Write credentials file so the orchestrator sees it
		credsJSON := `{"claudeAiOauth":{"accessToken":"file-token","expiresAt":9999999999999}}`
		require.NoError(t, os.WriteFile(filepath.Join(homeDir, ".claude", ".credentials.json"), []byte(credsJSON), 0o600))

		projectDir := setupGitProject(t)
		t.Setenv("ANTHROPIC_API_KEY", "")
		t.Setenv("CLAUDE_CODE_OAUTH_TOKEN", "")

		mockCM.EXPECT().ImageExists(gomock.Any(), gomock.Any()).Return(true, nil).Times(3)
		mockCM.EXPECT().CreateNetwork(gomock.Any(), gomock.Any()).Return("net-id", nil)
		mockCM.EXPECT().StartGateway(gomock.Any(), gomock.Any()).Return("gw-id", nil)
		mockCM.EXPECT().WaitForReady(gomock.Any(), "gw-id", gomock.Any()).Return(nil)
		mockCM.EXPECT().StartGitHubMCP(gomock.Any(), gomock.Any()).Return("mcp-id", nil)
		mockCM.EXPECT().WaitForReady(gomock.Any(), "mcp-id", gomock.Any()).Return(nil)
		mockCM.EXPECT().StartAgent(gomock.Any(), gomock.Any()).
			DoAndReturn(func(ctx context.Context, opts container.AgentOptions) (string, error) {
				_, hasOAuth := opts.Env["CLAUDE_CODE_OAUTH_TOKEN"]
				assert.False(t, hasOAuth, "OAuth token should not be passed as env var when credentials file exists")
				_, hasAPIKey := opts.Env["ANTHROPIC_API_KEY"]
				assert.False(t, hasAPIKey)
				return "agent-id", nil
			})

		sess, err := orch.Start(context.Background(), StartOptions{
			ProjectDir: projectDir,
		})
		require.NoError(t, err)
		assert.NotNil(t, sess)
	})
}

func TestStart_CommandArgs(t *testing.T) {
	ctrl := gomock.NewController(t)

	mockCM := NewMockContainerManager(ctrl)
	orch, _ := setupOrchestrator(t, mockCM)

	projectDir := setupGitProject(t)
	t.Setenv("ANTHROPIC_API_KEY", "sk-ant-test-key")

	mockCM.EXPECT().ImageExists(gomock.Any(), gomock.Any()).Return(true, nil).Times(3)
	mockCM.EXPECT().CreateNetwork(gomock.Any(), gomock.Any()).Return("net-id", nil)
	mockCM.EXPECT().StartGateway(gomock.Any(), gomock.Any()).Return("gw-id", nil)
	mockCM.EXPECT().WaitForReady(gomock.Any(), "gw-id", gomock.Any()).Return(nil)
	mockCM.EXPECT().StartGitHubMCP(gomock.Any(), gomock.Any()).Return("mcp-id", nil)
	mockCM.EXPECT().WaitForReady(gomock.Any(), "mcp-id", gomock.Any()).Return(nil)
	mockCM.EXPECT().StartAgent(gomock.Any(), gomock.Any()).
		DoAndReturn(func(ctx context.Context, opts container.AgentOptions) (string, error) {
			assert.Contains(t, opts.Cmd, "--dangerously-skip-permissions")
			assert.Contains(t, opts.Cmd, "--worktree")
			assert.Contains(t, opts.Cmd, "-p")
			assert.Contains(t, opts.Cmd, "hello world")
			return "agent-id", nil
		})

	sess, err := orch.Start(context.Background(), StartOptions{
		SkipPermissions: true,
		Worktree:        true,
		Prompt:          "hello world",
		ProjectDir:      projectDir,
	})

	require.NoError(t, err)
	assert.NotNil(t, sess)
}

func TestStart_WithName_PinsSessionIDAndWritesSidecar(t *testing.T) {
	ctrl := gomock.NewController(t)

	mockCM := NewMockContainerManager(ctrl)
	orch, homeDir := setupOrchestrator(t, mockCM)

	projectDir := setupGitProject(t)
	t.Setenv("ANTHROPIC_API_KEY", "sk-ant-test-key")

	var capturedSessionID string
	mockCM.EXPECT().ImageExists(gomock.Any(), gomock.Any()).Return(true, nil).Times(3)
	mockCM.EXPECT().CreateNetwork(gomock.Any(), gomock.Any()).Return("net-id", nil)
	mockCM.EXPECT().StartGateway(gomock.Any(), gomock.Any()).Return("gw-id", nil)
	mockCM.EXPECT().WaitForReady(gomock.Any(), "gw-id", gomock.Any()).Return(nil)
	mockCM.EXPECT().StartGitHubMCP(gomock.Any(), gomock.Any()).Return("mcp-id", nil)
	mockCM.EXPECT().WaitForReady(gomock.Any(), "mcp-id", gomock.Any()).Return(nil)
	mockCM.EXPECT().StartAgent(gomock.Any(), gomock.Any()).
		DoAndReturn(func(ctx context.Context, opts container.AgentOptions) (string, error) {
			assert.Contains(t, opts.Cmd, "--session-id")
			assert.Contains(t, opts.Cmd, "--name")
			assert.Contains(t, opts.Cmd, "claude")
			assert.NotContains(t, opts.Cmd, "--resume")
			assert.NotContains(t, opts.Cmd, "--continue")
			// Capture the value following --session-id.
			for i, a := range opts.Cmd {
				if a == "--session-id" && i+1 < len(opts.Cmd) {
					capturedSessionID = opts.Cmd[i+1]
				}
			}
			return "agent-id", nil
		})

	sess, err := orch.Start(context.Background(), StartOptions{
		SkipPermissions: true,
		Name:            "claude",
		ProjectDir:      projectDir,
	})
	require.NoError(t, err)
	require.NotNil(t, sess)

	require.NotEmpty(t, capturedSessionID)
	sidecar := filepath.Join(homeDir, ".claude-forge", sess.ProjectID, capturedSessionID+".json")
	data, err := os.ReadFile(sidecar)
	require.NoError(t, err)
	var got session.Metadata
	require.NoError(t, json.Unmarshal(data, &got))
	assert.Equal(t, "claude", got.Name)
}

func TestStart_RejectsInvalidName(t *testing.T) {
	ctrl := gomock.NewController(t)

	mockCM := NewMockContainerManager(ctrl)
	orch, _ := setupOrchestrator(t, mockCM)

	// No container methods should be called: validation fails first.
	_, err := orch.Start(context.Background(), StartOptions{
		Name:       "bad\tname",
		ProjectDir: setupGitProject(t),
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "tab or newline")
}

func TestStart_Resume_WithName_NoSessionID(t *testing.T) {
	ctrl := gomock.NewController(t)

	mockCM := NewMockContainerManager(ctrl)
	orch, _ := setupOrchestrator(t, mockCM)

	projectDir := setupGitProject(t)
	t.Setenv("ANTHROPIC_API_KEY", "sk-ant-test-key")

	mockCM.EXPECT().ImageExists(gomock.Any(), gomock.Any()).Return(true, nil).Times(3)
	mockCM.EXPECT().CreateNetwork(gomock.Any(), gomock.Any()).Return("net-id", nil)
	mockCM.EXPECT().StartGateway(gomock.Any(), gomock.Any()).Return("gw-id", nil)
	mockCM.EXPECT().WaitForReady(gomock.Any(), "gw-id", gomock.Any()).Return(nil)
	mockCM.EXPECT().StartGitHubMCP(gomock.Any(), gomock.Any()).Return("mcp-id", nil)
	mockCM.EXPECT().WaitForReady(gomock.Any(), "mcp-id", gomock.Any()).Return(nil)
	mockCM.EXPECT().StartAgent(gomock.Any(), gomock.Any()).
		DoAndReturn(func(ctx context.Context, opts container.AgentOptions) (string, error) {
			assert.Contains(t, opts.Cmd, "--resume")
			assert.Contains(t, opts.Cmd, "--name")
			assert.Contains(t, opts.Cmd, "claude")
			assert.NotContains(t, opts.Cmd, "--session-id")
			return "agent-id", nil
		})

	_, err := orch.Start(context.Background(), StartOptions{
		ResumeID:     "abc12345",
		ResumeSubdir: "-work",
		Name:         "claude",
		ProjectDir:   projectDir,
	})
	require.NoError(t, err)
}

func TestStart_ResumeSession(t *testing.T) {
	ctrl := gomock.NewController(t)

	mockCM := NewMockContainerManager(ctrl)
	orch, _ := setupOrchestrator(t, mockCM)

	projectDir := setupGitProject(t)
	t.Setenv("ANTHROPIC_API_KEY", "sk-ant-test-key")

	mockCM.EXPECT().ImageExists(gomock.Any(), gomock.Any()).Return(true, nil).Times(3)
	mockCM.EXPECT().CreateNetwork(gomock.Any(), gomock.Any()).Return("net-id", nil)
	mockCM.EXPECT().StartGateway(gomock.Any(), gomock.Any()).Return("gw-id", nil)
	mockCM.EXPECT().WaitForReady(gomock.Any(), "gw-id", gomock.Any()).Return(nil)
	mockCM.EXPECT().StartGitHubMCP(gomock.Any(), gomock.Any()).Return("mcp-id", nil)
	mockCM.EXPECT().WaitForReady(gomock.Any(), "mcp-id", gomock.Any()).Return(nil)
	mockCM.EXPECT().StartAgent(gomock.Any(), gomock.Any()).
		DoAndReturn(func(ctx context.Context, opts container.AgentOptions) (string, error) {
			assert.Contains(t, opts.Cmd, "--resume")
			assert.Contains(t, opts.Cmd, "/home/user/.claude/projects/-work/abc12345.jsonl")
			assert.NotContains(t, opts.Cmd, "--continue")
			return "agent-id", nil
		})

	sess, err := orch.Start(context.Background(), StartOptions{
		ResumeID:     "abc12345",
		ResumeSubdir: "-work",
		ProjectDir:   projectDir,
	})

	require.NoError(t, err)
	assert.NotNil(t, sess)
}

func TestStart_ResumeSession_WithoutSubdir(t *testing.T) {
	ctrl := gomock.NewController(t)

	mockCM := NewMockContainerManager(ctrl)
	orch, _ := setupOrchestrator(t, mockCM)

	projectDir := setupGitProject(t)
	t.Setenv("ANTHROPIC_API_KEY", "sk-ant-test-key")

	mockCM.EXPECT().ImageExists(gomock.Any(), gomock.Any()).Return(true, nil).Times(3)
	mockCM.EXPECT().CreateNetwork(gomock.Any(), gomock.Any()).Return("net-id", nil)
	mockCM.EXPECT().StartGateway(gomock.Any(), gomock.Any()).Return("gw-id", nil)
	mockCM.EXPECT().WaitForReady(gomock.Any(), "gw-id", gomock.Any()).Return(nil)
	mockCM.EXPECT().StartGitHubMCP(gomock.Any(), gomock.Any()).Return("mcp-id", nil)
	mockCM.EXPECT().WaitForReady(gomock.Any(), "mcp-id", gomock.Any()).Return(nil)
	mockCM.EXPECT().StartAgent(gomock.Any(), gomock.Any()).
		DoAndReturn(func(ctx context.Context, opts container.AgentOptions) (string, error) {
			assert.Contains(t, opts.Cmd, "--resume")
			assert.Contains(t, opts.Cmd, "abc12345")
			assert.NotContains(t, opts.Cmd, ".jsonl")
			return "agent-id", nil
		})

	sess, err := orch.Start(context.Background(), StartOptions{
		ResumeID:   "abc12345",
		ProjectDir: projectDir,
	})

	require.NoError(t, err)
	assert.NotNil(t, sess)
}

func TestStart_ResumeWorktreeSession(t *testing.T) {
	ctrl := gomock.NewController(t)

	mockCM := NewMockContainerManager(ctrl)
	orch, _ := setupOrchestrator(t, mockCM)

	projectDir := setupGitProject(t)
	t.Setenv("ANTHROPIC_API_KEY", "sk-ant-test-key")

	mockCM.EXPECT().ImageExists(gomock.Any(), gomock.Any()).Return(true, nil).Times(3)
	mockCM.EXPECT().CreateNetwork(gomock.Any(), gomock.Any()).Return("net-id", nil)
	mockCM.EXPECT().StartGateway(gomock.Any(), gomock.Any()).Return("gw-id", nil)
	mockCM.EXPECT().WaitForReady(gomock.Any(), "gw-id", gomock.Any()).Return(nil)
	mockCM.EXPECT().StartGitHubMCP(gomock.Any(), gomock.Any()).Return("mcp-id", nil)
	mockCM.EXPECT().WaitForReady(gomock.Any(), "mcp-id", gomock.Any()).Return(nil)
	mockCM.EXPECT().StartAgent(gomock.Any(), gomock.Any()).
		DoAndReturn(func(ctx context.Context, opts container.AgentOptions) (string, error) {
			assert.Contains(t, opts.Cmd, "--worktree")
			assert.Contains(t, opts.Cmd, "--resume")
			assert.Contains(t, opts.Cmd, "/home/user/.claude/projects/-work--claude-worktrees-my-feature/wt123456.jsonl")
			return "agent-id", nil
		})

	sess, err := orch.Start(context.Background(), StartOptions{
		Worktree:           true,
		ResumeID:           "wt123456",
		ResumeSubdir:       "-work--claude-worktrees-my-feature",
		ResumeWorktreeName: "my-feature",
		ProjectDir:         projectDir,
	})

	require.NoError(t, err)
	assert.NotNil(t, sess)
}

func TestStart_ResumeNonWorktreeSession_NoWorktreeFlag(t *testing.T) {
	ctrl := gomock.NewController(t)

	mockCM := NewMockContainerManager(ctrl)
	orch, _ := setupOrchestrator(t, mockCM)

	projectDir := setupGitProject(t)
	t.Setenv("ANTHROPIC_API_KEY", "sk-ant-test-key")

	mockCM.EXPECT().ImageExists(gomock.Any(), gomock.Any()).Return(true, nil).Times(3)
	mockCM.EXPECT().CreateNetwork(gomock.Any(), gomock.Any()).Return("net-id", nil)
	mockCM.EXPECT().StartGateway(gomock.Any(), gomock.Any()).Return("gw-id", nil)
	mockCM.EXPECT().WaitForReady(gomock.Any(), "gw-id", gomock.Any()).Return(nil)
	mockCM.EXPECT().StartGitHubMCP(gomock.Any(), gomock.Any()).Return("mcp-id", nil)
	mockCM.EXPECT().WaitForReady(gomock.Any(), "mcp-id", gomock.Any()).Return(nil)
	mockCM.EXPECT().StartAgent(gomock.Any(), gomock.Any()).
		DoAndReturn(func(ctx context.Context, opts container.AgentOptions) (string, error) {
			assert.NotContains(t, opts.Cmd, "--worktree")
			assert.Contains(t, opts.Cmd, "--resume")
			return "agent-id", nil
		})

	sess, err := orch.Start(context.Background(), StartOptions{
		Worktree:     false,
		ResumeID:     "abc12345",
		ResumeSubdir: "-work",
		ProjectDir:   projectDir,
	})

	require.NoError(t, err)
	assert.NotNil(t, sess)
}

func TestStart_ContinueSession(t *testing.T) {
	ctrl := gomock.NewController(t)

	mockCM := NewMockContainerManager(ctrl)
	orch, _ := setupOrchestrator(t, mockCM)

	projectDir := setupGitProject(t)
	t.Setenv("ANTHROPIC_API_KEY", "sk-ant-test-key")

	mockCM.EXPECT().ImageExists(gomock.Any(), gomock.Any()).Return(true, nil).Times(3)
	mockCM.EXPECT().CreateNetwork(gomock.Any(), gomock.Any()).Return("net-id", nil)
	mockCM.EXPECT().StartGateway(gomock.Any(), gomock.Any()).Return("gw-id", nil)
	mockCM.EXPECT().WaitForReady(gomock.Any(), "gw-id", gomock.Any()).Return(nil)
	mockCM.EXPECT().StartGitHubMCP(gomock.Any(), gomock.Any()).Return("mcp-id", nil)
	mockCM.EXPECT().WaitForReady(gomock.Any(), "mcp-id", gomock.Any()).Return(nil)
	mockCM.EXPECT().StartAgent(gomock.Any(), gomock.Any()).
		DoAndReturn(func(ctx context.Context, opts container.AgentOptions) (string, error) {
			assert.Contains(t, opts.Cmd, "--continue")
			assert.NotContains(t, opts.Cmd, "--resume")
			return "agent-id", nil
		})

	sess, err := orch.Start(context.Background(), StartOptions{
		Continue:   true,
		ProjectDir: projectDir,
	})

	require.NoError(t, err)
	assert.NotNil(t, sess)
}

func TestBuild_ConfigLoadFails(t *testing.T) {
	ctrl := gomock.NewController(t)

	mockCM := NewMockContainerManager(ctrl)
	homeDir := t.TempDir()

	// Create config dir with an invalid config file
	configDir := filepath.Join(homeDir, ".config", "claude-forge")
	require.NoError(t, os.MkdirAll(configDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(configDir, "config.yaml"), []byte(":::invalid yaml"), 0o644))

	orch := NewOrchestrator(mockCM, homeDir)
	orch.Log = func(format string, args ...any) {}

	err := orch.Build(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to load config")
}

func TestBuild_PullImageFails(t *testing.T) {
	ctrl := gomock.NewController(t)

	mockCM := NewMockContainerManager(ctrl)
	orch, _ := setupOrchestrator(t, mockCM)

	// First two image pulls succeed, third fails
	gomock.InOrder(
		mockCM.EXPECT().PullImage(gomock.Any(), gomock.Any()).Return(nil),
		mockCM.EXPECT().PullImage(gomock.Any(), gomock.Any()).Return(nil),
		mockCM.EXPECT().PullImage(gomock.Any(), gomock.Any()).Return(fmt.Errorf("network timeout")),
	)

	err := orch.Build(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to pull image")
	assert.Contains(t, err.Error(), "network timeout")
}

func TestStart_ImageExistsError(t *testing.T) {
	ctrl := gomock.NewController(t)

	mockCM := NewMockContainerManager(ctrl)
	orch, _ := setupOrchestrator(t, mockCM)

	projectDir := setupGitProject(t)
	t.Setenv("ANTHROPIC_API_KEY", "sk-ant-test-key")

	mockCM.EXPECT().ImageExists(gomock.Any(), gomock.Any()).Return(false, fmt.Errorf("docker daemon error"))

	_, err := orch.Start(context.Background(), StartOptions{
		ProjectDir: projectDir,
	})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to check image")
}

func TestStart_HostModelPropagation(t *testing.T) {
	t.Run("sets ANTHROPIC_MODEL when host has model configured", func(t *testing.T) {
		ctrl := gomock.NewController(t)

		mockCM := NewMockContainerManager(ctrl)
		orch, homeDir := setupOrchestrator(t, mockCM)

		projectDir := setupGitProject(t)
		t.Setenv("ANTHROPIC_API_KEY", "sk-ant-test-key")

		// Write model to host's ~/.claude/settings.json
		settings := `{"model": "claude-opus-4-6"}`
		require.NoError(t, os.WriteFile(filepath.Join(homeDir, ".claude", "settings.json"), []byte(settings), 0o644))

		mockCM.EXPECT().ImageExists(gomock.Any(), gomock.Any()).Return(true, nil).Times(3)
		mockCM.EXPECT().CreateNetwork(gomock.Any(), gomock.Any()).Return("net-id", nil)
		mockCM.EXPECT().StartGateway(gomock.Any(), gomock.Any()).Return("gw-id", nil)
		mockCM.EXPECT().WaitForReady(gomock.Any(), "gw-id", gomock.Any()).Return(nil)
		mockCM.EXPECT().StartGitHubMCP(gomock.Any(), gomock.Any()).Return("mcp-id", nil)
		mockCM.EXPECT().WaitForReady(gomock.Any(), "mcp-id", gomock.Any()).Return(nil)
		mockCM.EXPECT().StartAgent(gomock.Any(), gomock.Any()).
			DoAndReturn(func(ctx context.Context, opts container.AgentOptions) (string, error) {
				assert.Equal(t, "claude-opus-4-6", opts.Env["ANTHROPIC_MODEL"])
				return "agent-id", nil
			})

		sess, err := orch.Start(context.Background(), StartOptions{
			ProjectDir: projectDir,
		})

		require.NoError(t, err)
		assert.NotNil(t, sess)
	})

	t.Run("does not set ANTHROPIC_MODEL when host has no model", func(t *testing.T) {
		ctrl := gomock.NewController(t)

		mockCM := NewMockContainerManager(ctrl)
		orch, _ := setupOrchestrator(t, mockCM)

		projectDir := setupGitProject(t)
		t.Setenv("ANTHROPIC_API_KEY", "sk-ant-test-key")

		mockCM.EXPECT().ImageExists(gomock.Any(), gomock.Any()).Return(true, nil).Times(3)
		mockCM.EXPECT().CreateNetwork(gomock.Any(), gomock.Any()).Return("net-id", nil)
		mockCM.EXPECT().StartGateway(gomock.Any(), gomock.Any()).Return("gw-id", nil)
		mockCM.EXPECT().WaitForReady(gomock.Any(), "gw-id", gomock.Any()).Return(nil)
		mockCM.EXPECT().StartGitHubMCP(gomock.Any(), gomock.Any()).Return("mcp-id", nil)
		mockCM.EXPECT().WaitForReady(gomock.Any(), "mcp-id", gomock.Any()).Return(nil)
		mockCM.EXPECT().StartAgent(gomock.Any(), gomock.Any()).
			DoAndReturn(func(ctx context.Context, opts container.AgentOptions) (string, error) {
				_, exists := opts.Env["ANTHROPIC_MODEL"]
				assert.False(t, exists, "ANTHROPIC_MODEL should not be set when host has no model")
				return "agent-id", nil
			})

		sess, err := orch.Start(context.Background(), StartOptions{
			ProjectDir: projectDir,
		})

		require.NoError(t, err)
		assert.NotNil(t, sess)
	})
}

func TestStop_ListContainersError(t *testing.T) {
	ctrl := gomock.NewController(t)

	mockCM := NewMockContainerManager(ctrl)
	orch, _ := setupOrchestrator(t, mockCM)

	projectDir := setupGitProject(t)

	mockCM.EXPECT().ListForgeContainers(gomock.Any()).Return(nil, fmt.Errorf("list error"))

	err := orch.Stop(context.Background(), projectDir)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to list containers")
}

func TestStop_NonGitDirectory(t *testing.T) {
	ctrl := gomock.NewController(t)

	mockCM := NewMockContainerManager(ctrl)
	orch, _ := setupOrchestrator(t, mockCM)

	nonGitDir := t.TempDir()

	err := orch.Stop(context.Background(), nonGitDir)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to identify project")
}

func TestStart_ExtraMounts(t *testing.T) {
	ctrl := gomock.NewController(t)

	mockCM := NewMockContainerManager(ctrl)
	orch, _ := setupOrchestrator(t, mockCM)

	projectDir := setupGitProject(t)
	mountSource := t.TempDir()
	t.Setenv("ANTHROPIC_API_KEY", "sk-ant-test-key")

	mockCM.EXPECT().ImageExists(gomock.Any(), gomock.Any()).Return(true, nil).Times(3)
	mockCM.EXPECT().CreateNetwork(gomock.Any(), gomock.Any()).Return("net-id", nil)
	mockCM.EXPECT().StartGateway(gomock.Any(), gomock.Any()).Return("gw-id", nil)
	mockCM.EXPECT().WaitForReady(gomock.Any(), "gw-id", gomock.Any()).Return(nil)
	mockCM.EXPECT().StartGitHubMCP(gomock.Any(), gomock.Any()).Return("mcp-id", nil)
	mockCM.EXPECT().WaitForReady(gomock.Any(), "mcp-id", gomock.Any()).Return(nil)
	mockCM.EXPECT().StartAgent(gomock.Any(), gomock.Any()).
		DoAndReturn(func(ctx context.Context, opts container.AgentOptions) (string, error) {
			require.Len(t, opts.ExtraMounts, 1)
			assert.Equal(t, mountSource, opts.ExtraMounts[0].Source)
			assert.Equal(t, "/data/shared", opts.ExtraMounts[0].Target)
			return "agent-id", nil
		})

	sess, err := orch.Start(context.Background(), StartOptions{
		SkipPermissions: true,
		ProjectDir:      projectDir,
		UID:             1000,
		GID:             1000,
		Mounts:          []string{mountSource + ":/data/shared"},
	})

	require.NoError(t, err)
	assert.NotEmpty(t, sess.AgentName)
}

func TestStart_ExtraMounts_InvalidFormat(t *testing.T) {
	ctrl := gomock.NewController(t)

	mockCM := NewMockContainerManager(ctrl)
	orch, _ := setupOrchestrator(t, mockCM)

	projectDir := setupGitProject(t)
	t.Setenv("ANTHROPIC_API_KEY", "sk-ant-test-key")

	mockCM.EXPECT().ImageExists(gomock.Any(), gomock.Any()).Return(true, nil).Times(3)
	mockCM.EXPECT().CreateNetwork(gomock.Any(), gomock.Any()).Return("net-id", nil)
	mockCM.EXPECT().StartGateway(gomock.Any(), gomock.Any()).Return("gw-id", nil)
	mockCM.EXPECT().WaitForReady(gomock.Any(), "gw-id", gomock.Any()).Return(nil)
	mockCM.EXPECT().StartGitHubMCP(gomock.Any(), gomock.Any()).Return("mcp-id", nil)
	mockCM.EXPECT().WaitForReady(gomock.Any(), "mcp-id", gomock.Any()).Return(nil)
	mockCM.EXPECT().ContainerLogs(gomock.Any(), gomock.Any()).Return("", nil).AnyTimes()
	mockCM.EXPECT().RemoveContainer(gomock.Any(), gomock.Any()).Return(nil).AnyTimes()
	mockCM.EXPECT().RemoveNetwork(gomock.Any(), gomock.Any()).Return(nil).AnyTimes()

	_, err := orch.Start(context.Background(), StartOptions{
		SkipPermissions: true,
		ProjectDir:      projectDir,
		UID:             1000,
		GID:             1000,
		Mounts:          []string{"no-colon-separator"},
	})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid mount format")
}

func TestStart_ExtraMounts_NonexistentSource(t *testing.T) {
	ctrl := gomock.NewController(t)

	mockCM := NewMockContainerManager(ctrl)
	orch, _ := setupOrchestrator(t, mockCM)

	projectDir := setupGitProject(t)
	t.Setenv("ANTHROPIC_API_KEY", "sk-ant-test-key")

	mockCM.EXPECT().ImageExists(gomock.Any(), gomock.Any()).Return(true, nil).Times(3)
	mockCM.EXPECT().CreateNetwork(gomock.Any(), gomock.Any()).Return("net-id", nil)
	mockCM.EXPECT().StartGateway(gomock.Any(), gomock.Any()).Return("gw-id", nil)
	mockCM.EXPECT().WaitForReady(gomock.Any(), "gw-id", gomock.Any()).Return(nil)
	mockCM.EXPECT().StartGitHubMCP(gomock.Any(), gomock.Any()).Return("mcp-id", nil)
	mockCM.EXPECT().WaitForReady(gomock.Any(), "mcp-id", gomock.Any()).Return(nil)
	mockCM.EXPECT().ContainerLogs(gomock.Any(), gomock.Any()).Return("", nil).AnyTimes()
	mockCM.EXPECT().RemoveContainer(gomock.Any(), gomock.Any()).Return(nil).AnyTimes()
	mockCM.EXPECT().RemoveNetwork(gomock.Any(), gomock.Any()).Return(nil).AnyTimes()

	_, err := orch.Start(context.Background(), StartOptions{
		SkipPermissions: true,
		ProjectDir:      projectDir,
		UID:             1000,
		GID:             1000,
		Mounts:          []string{"/nonexistent/path/abc123:/container/path"},
	})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "mount source path does not exist")
}

func TestStart_GHTokenFromHostsFile(t *testing.T) {
	ctrl := gomock.NewController(t)

	mockCM := NewMockContainerManager(ctrl)
	orch, homeDir := setupOrchestrator(t, mockCM)

	projectDir := setupGitProject(t)
	t.Setenv("ANTHROPIC_API_KEY", "sk-ant-test-key")
	t.Setenv("GITHUB_TOKEN", "") // explicitly unset

	// Write gh hosts.yml
	ghConfigDir := filepath.Join(homeDir, ".config", "gh")
	require.NoError(t, os.MkdirAll(ghConfigDir, 0o755))
	hostsContent := "github.com:\n  oauth_token: gho_from_hosts_file\n  user: testuser\n"
	require.NoError(t, os.WriteFile(filepath.Join(ghConfigDir, "hosts.yml"), []byte(hostsContent), 0o644))

	mockCM.EXPECT().ImageExists(gomock.Any(), gomock.Any()).Return(true, nil).Times(3)
	mockCM.EXPECT().CreateNetwork(gomock.Any(), gomock.Any()).Return("net-id", nil)
	mockCM.EXPECT().StartGateway(gomock.Any(), gomock.Any()).
		DoAndReturn(func(ctx context.Context, opts container.GatewayOptions) (string, error) {
			assert.Equal(t, "gho_from_hosts_file", opts.Env["GITHUB_TOKEN"])
			return "gw-id", nil
		})
	mockCM.EXPECT().WaitForReady(gomock.Any(), "gw-id", gomock.Any()).Return(nil)
	mockCM.EXPECT().StartGitHubMCP(gomock.Any(), gomock.Any()).Return("mcp-id", nil)
	mockCM.EXPECT().WaitForReady(gomock.Any(), "mcp-id", gomock.Any()).Return(nil)
	mockCM.EXPECT().StartAgent(gomock.Any(), gomock.Any()).Return("agent-id", nil)

	sess, err := orch.Start(context.Background(), StartOptions{
		SkipPermissions: true,
		ProjectDir:      projectDir,
		UID:             1000,
		GID:             1000,
	})

	require.NoError(t, err)
	assert.NotEmpty(t, sess.AgentName)
}

func TestStart_WithKubernetesEnabled(t *testing.T) {
	ctrl := gomock.NewController(t)

	mockCM := NewMockContainerManager(ctrl)
	orch, homeDir := setupOrchestrator(t, mockCM)

	projectDir := setupGitProject(t)
	t.Setenv("ANTHROPIC_API_KEY", "sk-ant-test-key")

	// Write config with Kubernetes enabled
	configDir := filepath.Join(homeDir, ".config", "claude-forge")
	configContent := `images:
  agent: agent:latest
  gateway: gateway:latest
  github_mcp: mcp:latest
kubernetes:
  enabled: true
  image: k8s-mcp:latest
  contexts:
    - host_context: my-cluster
      service_account_name: forge-sa
      service_account_namespace: default
`
	require.NoError(t, os.WriteFile(filepath.Join(configDir, "config.yaml"), []byte(configContent), 0o644))

	mockCM.EXPECT().ImageExists(gomock.Any(), gomock.Any()).Return(true, nil).Times(4)
	mockCM.EXPECT().CreateNetwork(gomock.Any(), gomock.Any()).Return("net-id", nil)
	mockCM.EXPECT().StartGateway(gomock.Any(), gomock.Any()).Return("gw-id", nil)
	mockCM.EXPECT().WaitForReady(gomock.Any(), "gw-id", gomock.Any()).Return(nil)
	mockCM.EXPECT().StartGitHubMCP(gomock.Any(), gomock.Any()).Return("mcp-id", nil)
	mockCM.EXPECT().WaitForReady(gomock.Any(), "mcp-id", gomock.Any()).Return(nil)
	// startKubernetesMCP will be called but will fail at GenerateKubeconfig (no kubeconfig file)
	// Start logs a warning and continues without adding kubernetes to settings.json
	mockCM.EXPECT().EnsureSharedNetwork(gomock.Any(), "forge-shared").Return("shared-net-id", nil)
	mockCM.EXPECT().IsContainerRunning(gomock.Any(), "forge-k8s-mcp").Return(false, nil)
	mockCM.EXPECT().RemoveContainer(gomock.Any(), "forge-k8s-mcp").Return(nil)
	mockCM.EXPECT().StartAgent(gomock.Any(), gomock.Any()).Return("agent-id", nil)
	// ExtraNetworks is NOT set because startKubernetesMCP failed

	sess, err := orch.Start(context.Background(), StartOptions{
		ProjectDir: projectDir,
	})

	require.NoError(t, err)
	assert.NotNil(t, sess)
}

func TestStart_WithKubernetesEnabled_MCPFailsGracefully(t *testing.T) {
	ctrl := gomock.NewController(t)

	mockCM := NewMockContainerManager(ctrl)
	orch, homeDir := setupOrchestrator(t, mockCM)

	projectDir := setupGitProject(t)
	t.Setenv("ANTHROPIC_API_KEY", "sk-ant-test-key")

	// Write config with Kubernetes enabled
	configDir := filepath.Join(homeDir, ".config", "claude-forge")
	configContent := `images:
  agent: agent:latest
  gateway: gateway:latest
  github_mcp: mcp:latest
kubernetes:
  enabled: true
  image: k8s-mcp:latest
  contexts:
    - host_context: my-cluster
      service_account_name: forge-sa
      service_account_namespace: default
`
	require.NoError(t, os.WriteFile(filepath.Join(configDir, "config.yaml"), []byte(configContent), 0o644))

	mockCM.EXPECT().ImageExists(gomock.Any(), gomock.Any()).Return(true, nil).Times(4)
	mockCM.EXPECT().CreateNetwork(gomock.Any(), gomock.Any()).Return("net-id", nil)
	mockCM.EXPECT().StartGateway(gomock.Any(), gomock.Any()).Return("gw-id", nil)
	mockCM.EXPECT().WaitForReady(gomock.Any(), "gw-id", gomock.Any()).Return(nil)
	mockCM.EXPECT().StartGitHubMCP(gomock.Any(), gomock.Any()).Return("mcp-id", nil)
	mockCM.EXPECT().WaitForReady(gomock.Any(), "mcp-id", gomock.Any()).Return(nil)
	// startKubernetesMCP fails (no kubeconfig) — Start continues without
	// kubernetes in settings.json
	mockCM.EXPECT().EnsureSharedNetwork(gomock.Any(), "forge-shared").Return("shared-net-id", nil)
	mockCM.EXPECT().IsContainerRunning(gomock.Any(), "forge-k8s-mcp").Return(false, nil)
	mockCM.EXPECT().RemoveContainer(gomock.Any(), "forge-k8s-mcp").Return(nil)
	mockCM.EXPECT().StartAgent(gomock.Any(), gomock.Any()).Return("agent-id", nil)

	sess, err := orch.Start(context.Background(), StartOptions{
		ProjectDir: projectDir,
	})

	require.NoError(t, err)
	assert.NotNil(t, sess)
}

func TestStartKubernetesMCP_StartsNewContainer(t *testing.T) {
	ctrl := gomock.NewController(t)

	mockCM := NewMockContainerManager(ctrl)
	orch, homeDir := setupOrchestrator(t, mockCM)

	// Create a fake kubeconfig
	kubeDir := filepath.Join(homeDir, ".kube")
	require.NoError(t, os.MkdirAll(kubeDir, 0o755))
	kubeconfig := `apiVersion: v1
kind: Config
clusters:
- name: my-cluster
  cluster:
    server: https://k8s.example.com:6443
    certificate-authority-data: ZmFrZS1jYQ==
contexts:
- name: my-cluster
  context:
    cluster: my-cluster
    user: admin
users:
- name: admin
  user:
    token: admin-token
current-context: my-cluster
`
	require.NoError(t, os.WriteFile(filepath.Join(kubeDir, "config"), []byte(kubeconfig), 0o644))

	cfg := &config.Config{
		Kubernetes: config.KubernetesConfig{
			Enabled: true,
			Image:   "k8s-mcp:latest",
			Contexts: []config.KubeContextEntry{
				{HostContext: "my-cluster", ServiceAccountName: "forge-sa", ServiceAccountNamespace: "default"},
			},
		},
	}

	mockCM.EXPECT().EnsureSharedNetwork(gomock.Any(), "forge-shared").Return("net-id", nil)
	mockCM.EXPECT().IsContainerRunning(gomock.Any(), "forge-k8s-mcp").Return(false, nil)
	mockCM.EXPECT().RemoveContainer(gomock.Any(), "forge-k8s-mcp").Return(nil)
	// GenerateKubeconfig will fail because kubectl isn't available,
	// but this test verifies the code path up to that point

	err := orch.startKubernetesMCP(context.Background(), cfg)
	// Will fail at GenerateKubeconfig (no kubectl), but exercises the setup code
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to generate kubeconfig")
}

func TestStartKubernetesMCP_KUBECONFIGEnvVar(t *testing.T) {
	ctrl := gomock.NewController(t)

	mockCM := NewMockContainerManager(ctrl)
	orch, _ := setupOrchestrator(t, mockCM)

	// Set KUBECONFIG to a non-existent path to test env var handling
	t.Setenv("KUBECONFIG", "/tmp/nonexistent-kubeconfig-test")

	cfg := &config.Config{
		Kubernetes: config.KubernetesConfig{
			Enabled: true,
			Image:   "k8s-mcp:latest",
			Contexts: []config.KubeContextEntry{
				{HostContext: "ctx-1", ServiceAccountName: "sa", ServiceAccountNamespace: "ns"},
			},
		},
	}

	mockCM.EXPECT().EnsureSharedNetwork(gomock.Any(), "forge-shared").Return("net-id", nil)
	mockCM.EXPECT().IsContainerRunning(gomock.Any(), "forge-k8s-mcp").Return(false, nil)
	mockCM.EXPECT().RemoveContainer(gomock.Any(), "forge-k8s-mcp").Return(nil)

	err := orch.startKubernetesMCP(context.Background(), cfg)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to generate kubeconfig")
}

func TestStartKubernetesMCP_DefaultContextFallback(t *testing.T) {
	ctrl := gomock.NewController(t)

	mockCM := NewMockContainerManager(ctrl)
	orch, _ := setupOrchestrator(t, mockCM)

	cfg := &config.Config{
		Kubernetes: config.KubernetesConfig{
			Enabled:        true,
			Image:          "k8s-mcp:latest",
			DefaultContext: "explicit-default",
			Contexts: []config.KubeContextEntry{
				{HostContext: "ctx-1", ServiceAccountName: "sa", ServiceAccountNamespace: "ns"},
			},
		},
	}

	mockCM.EXPECT().EnsureSharedNetwork(gomock.Any(), "forge-shared").Return("net-id", nil)
	mockCM.EXPECT().IsContainerRunning(gomock.Any(), "forge-k8s-mcp").Return(false, nil)
	mockCM.EXPECT().RemoveContainer(gomock.Any(), "forge-k8s-mcp").Return(nil)

	err := orch.startKubernetesMCP(context.Background(), cfg)
	// Will fail at GenerateKubeconfig, but exercises the DefaultContext path
	require.Error(t, err)
}

func TestStartKubernetesMCP_AlreadyRunning(t *testing.T) {
	ctrl := gomock.NewController(t)

	mockCM := NewMockContainerManager(ctrl)
	orch, _ := setupOrchestrator(t, mockCM)

	cfg := &config.Config{
		Kubernetes: config.KubernetesConfig{
			Enabled: true,
			Image:   "k8s-mcp:latest",
			Contexts: []config.KubeContextEntry{
				{HostContext: "ctx-1", ServiceAccountName: "sa", ServiceAccountNamespace: "ns"},
			},
		},
	}

	mockCM.EXPECT().EnsureSharedNetwork(gomock.Any(), "forge-shared").Return("net-id", nil)
	mockCM.EXPECT().IsContainerRunning(gomock.Any(), "forge-k8s-mcp").Return(true, nil)

	err := orch.startKubernetesMCP(context.Background(), cfg)
	require.NoError(t, err)
}

func TestStartKubernetesMCP_EnsureNetworkFails(t *testing.T) {
	ctrl := gomock.NewController(t)

	mockCM := NewMockContainerManager(ctrl)
	orch, _ := setupOrchestrator(t, mockCM)

	cfg := &config.Config{
		Kubernetes: config.KubernetesConfig{
			Enabled: true,
			Image:   "k8s-mcp:latest",
			Contexts: []config.KubeContextEntry{
				{HostContext: "ctx-1", ServiceAccountName: "sa", ServiceAccountNamespace: "ns"},
			},
		},
	}

	mockCM.EXPECT().EnsureSharedNetwork(gomock.Any(), "forge-shared").Return("", fmt.Errorf("docker error"))

	err := orch.startKubernetesMCP(context.Background(), cfg)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to ensure shared network")
}

func TestStartKubernetesMCP_CheckRunningFails(t *testing.T) {
	ctrl := gomock.NewController(t)

	mockCM := NewMockContainerManager(ctrl)
	orch, _ := setupOrchestrator(t, mockCM)

	cfg := &config.Config{
		Kubernetes: config.KubernetesConfig{
			Enabled: true,
			Image:   "k8s-mcp:latest",
			Contexts: []config.KubeContextEntry{
				{HostContext: "ctx-1", ServiceAccountName: "sa", ServiceAccountNamespace: "ns"},
			},
		},
	}

	mockCM.EXPECT().EnsureSharedNetwork(gomock.Any(), "forge-shared").Return("net-id", nil)
	mockCM.EXPECT().IsContainerRunning(gomock.Any(), "forge-k8s-mcp").Return(false, fmt.Errorf("inspect error"))

	err := orch.startKubernetesMCP(context.Background(), cfg)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to check k8s-mcp status")
}

func TestRestartSharedMCP_StopsAndRestarts(t *testing.T) {
	ctrl := gomock.NewController(t)

	mockCM := NewMockContainerManager(ctrl)
	orch, homeDir := setupOrchestrator(t, mockCM)

	configDir := filepath.Join(homeDir, ".config", "claude-forge")
	configContent := `images:
  agent: agent:latest
  gateway: gateway:latest
  github_mcp: mcp:latest
kubernetes:
  enabled: true
  image: k8s-mcp:latest
  contexts:
    - host_context: my-cluster
      service_account_name: forge-sa
      service_account_namespace: default
`
	require.NoError(t, os.WriteFile(filepath.Join(configDir, "config.yaml"), []byte(configContent), 0o644))

	// Container is currently running
	mockCM.EXPECT().IsContainerRunning(gomock.Any(), "forge-k8s-mcp").Return(true, nil)
	mockCM.EXPECT().StopContainer(gomock.Any(), "forge-k8s-mcp").Return(nil)
	mockCM.EXPECT().RemoveContainer(gomock.Any(), "forge-k8s-mcp").Return(nil)
	// startKubernetesMCP is called to restart
	mockCM.EXPECT().EnsureSharedNetwork(gomock.Any(), "forge-shared").Return("net-id", nil)
	mockCM.EXPECT().IsContainerRunning(gomock.Any(), "forge-k8s-mcp").Return(false, nil)
	mockCM.EXPECT().RemoveContainer(gomock.Any(), "forge-k8s-mcp").Return(nil)

	err := orch.RestartSharedMCP(context.Background())
	// Will fail at GenerateKubeconfig (no real kubectl), but exercises stop+start path
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to restart Kubernetes MCP")
}

func TestRestartSharedMCP_NotRunning(t *testing.T) {
	ctrl := gomock.NewController(t)

	mockCM := NewMockContainerManager(ctrl)
	orch, homeDir := setupOrchestrator(t, mockCM)

	configDir := filepath.Join(homeDir, ".config", "claude-forge")
	configContent := `images:
  agent: agent:latest
  gateway: gateway:latest
  github_mcp: mcp:latest
kubernetes:
  enabled: true
  image: k8s-mcp:latest
  contexts:
    - host_context: my-cluster
      service_account_name: forge-sa
      service_account_namespace: default
`
	require.NoError(t, os.WriteFile(filepath.Join(configDir, "config.yaml"), []byte(configContent), 0o644))

	// Container is NOT running — skip stop, go straight to start
	mockCM.EXPECT().IsContainerRunning(gomock.Any(), "forge-k8s-mcp").Return(false, nil)
	mockCM.EXPECT().EnsureSharedNetwork(gomock.Any(), "forge-shared").Return("net-id", nil)
	mockCM.EXPECT().IsContainerRunning(gomock.Any(), "forge-k8s-mcp").Return(false, nil)
	mockCM.EXPECT().RemoveContainer(gomock.Any(), "forge-k8s-mcp").Return(nil)

	err := orch.RestartSharedMCP(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to restart Kubernetes MCP")
}

func TestRestartSharedMCP_KubernetesDisabled(t *testing.T) {
	ctrl := gomock.NewController(t)

	mockCM := NewMockContainerManager(ctrl)
	orch, _ := setupOrchestrator(t, mockCM)

	err := orch.RestartSharedMCP(context.Background())
	require.NoError(t, err)
}

func TestBuild_WithKubernetesEnabled(t *testing.T) {
	ctrl := gomock.NewController(t)

	mockCM := NewMockContainerManager(ctrl)
	orch, homeDir := setupOrchestrator(t, mockCM)

	// Write config with Kubernetes enabled
	configDir := filepath.Join(homeDir, ".config", "claude-forge")
	configContent := `images:
  agent: agent:latest
  gateway: gateway:latest
  github_mcp: mcp:latest
kubernetes:
  enabled: true
  image: k8s-mcp:latest
  contexts:
    - host_context: my-cluster
      service_account_name: sa
      service_account_namespace: ns
`
	require.NoError(t, os.WriteFile(filepath.Join(configDir, "config.yaml"), []byte(configContent), 0o644))

	mockCM.EXPECT().PullImage(gomock.Any(), gomock.Any()).Return(nil).Times(4)

	err := orch.Build(context.Background())
	require.NoError(t, err)
}

func TestStart_FailsOnGitHubMCPStart(t *testing.T) {
	ctrl := gomock.NewController(t)

	mockCM := NewMockContainerManager(ctrl)
	orch, _ := setupOrchestrator(t, mockCM)

	projectDir := setupGitProject(t)
	t.Setenv("ANTHROPIC_API_KEY", "sk-ant-test-key")

	mockCM.EXPECT().ImageExists(gomock.Any(), gomock.Any()).Return(true, nil).Times(3)
	mockCM.EXPECT().CreateNetwork(gomock.Any(), gomock.Any()).Return("net-id", nil)
	mockCM.EXPECT().StartGateway(gomock.Any(), gomock.Any()).Return("gw-id", nil)
	mockCM.EXPECT().WaitForReady(gomock.Any(), "gw-id", gomock.Any()).Return(nil)
	mockCM.EXPECT().StartGitHubMCP(gomock.Any(), gomock.Any()).Return("", fmt.Errorf("mcp start failed"))

	// Expect cleanup
	mockCM.EXPECT().StopContainer(gomock.Any(), gomock.Any()).Return(nil).Times(3)
	mockCM.EXPECT().RemoveContainer(gomock.Any(), gomock.Any()).Return(nil).Times(3)
	mockCM.EXPECT().RemoveNetwork(gomock.Any(), gomock.Any()).Return(nil).Times(1)

	_, err := orch.Start(context.Background(), StartOptions{
		ProjectDir: projectDir,
	})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to start github-mcp")
}

func TestStart_FailsOnGitHubMCPReady(t *testing.T) {
	ctrl := gomock.NewController(t)

	mockCM := NewMockContainerManager(ctrl)
	orch, _ := setupOrchestrator(t, mockCM)

	projectDir := setupGitProject(t)
	t.Setenv("ANTHROPIC_API_KEY", "sk-ant-test-key")

	mockCM.EXPECT().ImageExists(gomock.Any(), gomock.Any()).Return(true, nil).Times(3)
	mockCM.EXPECT().CreateNetwork(gomock.Any(), gomock.Any()).Return("net-id", nil)
	mockCM.EXPECT().StartGateway(gomock.Any(), gomock.Any()).Return("gw-id", nil)
	mockCM.EXPECT().WaitForReady(gomock.Any(), "gw-id", gomock.Any()).Return(nil)
	mockCM.EXPECT().StartGitHubMCP(gomock.Any(), gomock.Any()).Return("mcp-id", nil)
	mockCM.EXPECT().WaitForReady(gomock.Any(), "mcp-id", gomock.Any()).Return(fmt.Errorf("mcp exited with code 1"))
	mockCM.EXPECT().ContainerLogs(gomock.Any(), "mcp-id").Return("connection refused", nil)

	// Expect cleanup
	mockCM.EXPECT().StopContainer(gomock.Any(), gomock.Any()).Return(nil).Times(3)
	mockCM.EXPECT().RemoveContainer(gomock.Any(), gomock.Any()).Return(nil).Times(3)
	mockCM.EXPECT().RemoveNetwork(gomock.Any(), gomock.Any()).Return(nil).Times(1)

	_, err := orch.Start(context.Background(), StartOptions{
		ProjectDir: projectDir,
	})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "github-mcp failed to start")
}

func TestReadGHToken(t *testing.T) {
	t.Run("valid hosts.yml", func(t *testing.T) {
		dir := t.TempDir()
		hostsContent := `github.com:
  oauth_token: gho_test_token_123
  user: testuser
`
		require.NoError(t, os.WriteFile(filepath.Join(dir, "hosts.yml"), []byte(hostsContent), 0o644))

		token := readGHToken(dir)
		assert.Equal(t, "gho_test_token_123", token)
	})

	t.Run("file does not exist", func(t *testing.T) {
		dir := t.TempDir()
		token := readGHToken(dir)
		assert.Empty(t, token)
	})

	t.Run("invalid yaml", func(t *testing.T) {
		dir := t.TempDir()
		require.NoError(t, os.WriteFile(filepath.Join(dir, "hosts.yml"), []byte(":::invalid"), 0o644))

		token := readGHToken(dir)
		assert.Empty(t, token)
	})

	t.Run("no github.com entry", func(t *testing.T) {
		dir := t.TempDir()
		hostsContent := `gitlab.com:
  oauth_token: glpat_something
`
		require.NoError(t, os.WriteFile(filepath.Join(dir, "hosts.yml"), []byte(hostsContent), 0o644))

		token := readGHToken(dir)
		assert.Empty(t, token)
	})
}

func TestCustomMCPServer(t *testing.T) {
	t.Run("remote server expands url and header vars", func(t *testing.T) {
		t.Setenv("VERCEL_TOKEN", "secret-token")
		out := customMCPServer(config.MCPServerConfig{
			Name:    "vercel",
			Type:    "http",
			URL:     "https://mcp.vercel.com/${VERCEL_TOKEN}",
			Headers: map[string]string{"Authorization": "Bearer ${VERCEL_TOKEN}"},
		})
		assert.Equal(t, "http", out.Type)
		assert.Equal(t, "https://mcp.vercel.com/secret-token", out.URL)
		assert.Equal(t, "Bearer secret-token", out.Headers["Authorization"])
		assert.Empty(t, out.Command)
	})

	t.Run("remote server defaults type to http", func(t *testing.T) {
		out := customMCPServer(config.MCPServerConfig{Name: "x", URL: "https://x.com"})
		assert.Equal(t, "http", out.Type)
	})

	t.Run("stdio server expands command args env", func(t *testing.T) {
		t.Setenv("MY_KEY", "k123")
		out := customMCPServer(config.MCPServerConfig{
			Name:    "local",
			Type:    "stdio",
			Command: "my-mcp",
			Args:    []string{"--token", "${MY_KEY}"},
			Env:     map[string]string{"API_KEY": "${MY_KEY}"},
		})
		assert.Equal(t, "stdio", out.Type)
		assert.Equal(t, "my-mcp", out.Command)
		assert.Equal(t, []string{"--token", "k123"}, out.Args)
		assert.Equal(t, "k123", out.Env["API_KEY"])
		assert.Empty(t, out.URL)
	})

	t.Run("empty maps become nil", func(t *testing.T) {
		out := customMCPServer(config.MCPServerConfig{Name: "x", URL: "https://x.com"})
		assert.Nil(t, out.Headers)
	})
}

func TestWarnUnauthenticatedMCP(t *testing.T) {
	newOrch := func(t *testing.T) (*Orchestrator, *[]string) {
		t.Helper()
		claudeDir := t.TempDir()
		var logs []string
		o := &Orchestrator{
			ClaudeDir: claudeDir,
			Log:       func(format string, args ...any) { logs = append(logs, fmt.Sprintf(format, args...)) },
		}
		return o, &logs
	}

	writeCreds := func(t *testing.T, dir, body string) {
		t.Helper()
		require.NoError(t, os.WriteFile(filepath.Join(dir, ".credentials.json"), []byte(body), 0o600))
	}

	joined := func(logs []string) string { return strings.Join(logs, "\n") }

	t.Run("non-oauth servers are never checked", func(t *testing.T) {
		o, logs := newOrch(t)
		o.warnUnauthenticatedMCP(&config.Config{MCPServers: []config.MCPServerConfig{
			{Name: "public", Type: "http", URL: "https://x.com"},
			{Name: "tool", Type: "stdio", Command: "x"},
		}})
		assert.Empty(t, *logs)
	})

	t.Run("warns when oauth token missing", func(t *testing.T) {
		o, logs := newOrch(t)
		o.warnUnauthenticatedMCP(&config.Config{MCPServers: []config.MCPServerConfig{
			{Name: "vercel", Type: "http", URL: "https://mcp.vercel.com", OAuth: true},
		}})
		assert.Contains(t, joined(*logs), `MCP server "vercel" needs OAuth`)
		assert.Contains(t, joined(*logs), "claude mcp add --transport http vercel https://mcp.vercel.com")
	})

	t.Run("silent when a valid token is present", func(t *testing.T) {
		o, logs := newOrch(t)
		key := claudecode.MCPOAuthKey("vercel", "http", "https://mcp.vercel.com", nil)
		writeCreds(t, o.ClaudeDir, fmt.Sprintf(`{"mcpOAuth":{%q:{"accessToken":"a","refreshToken":"r"}}}`, key))
		o.warnUnauthenticatedMCP(&config.Config{MCPServers: []config.MCPServerConfig{
			{Name: "vercel", Type: "http", URL: "https://mcp.vercel.com", OAuth: true},
		}})
		assert.Empty(t, *logs)
	})

	t.Run("warns when token expired", func(t *testing.T) {
		o, logs := newOrch(t)
		key := claudecode.MCPOAuthKey("vercel", "http", "https://mcp.vercel.com", nil)
		writeCreds(t, o.ClaudeDir, fmt.Sprintf(`{"mcpOAuth":{%q:{"accessToken":"a","expiresAt":1}}}`, key))
		o.warnUnauthenticatedMCP(&config.Config{MCPServers: []config.MCPServerConfig{
			{Name: "vercel", Type: "http", URL: "https://mcp.vercel.com", OAuth: true},
		}})
		assert.Contains(t, joined(*logs), "expired")
	})

	t.Run("expands env vars in url", func(t *testing.T) {
		o, logs := newOrch(t)
		t.Setenv("MCP_HOST", "mcp.vercel.com")
		o.warnUnauthenticatedMCP(&config.Config{MCPServers: []config.MCPServerConfig{
			{Name: "vercel", Type: "http", URL: "https://${MCP_HOST}", OAuth: true},
		}})
		assert.Contains(t, joined(*logs), "https://mcp.vercel.com")
	})
}

func TestWarnUnauthenticatedMCP_ReadError(t *testing.T) {
	claudeDir := t.TempDir()
	// Make .credentials.json a directory so ReadFile fails (not IsNotExist).
	require.NoError(t, os.MkdirAll(filepath.Join(claudeDir, ".credentials.json"), 0o755))

	var logs []string
	o := &Orchestrator{
		ClaudeDir: claudeDir,
		Log:       func(format string, args ...any) { logs = append(logs, fmt.Sprintf(format, args...)) },
	}
	o.warnUnauthenticatedMCP(&config.Config{MCPServers: []config.MCPServerConfig{
		{Name: "vercel", Type: "http", URL: "https://mcp.vercel.com", OAuth: true},
	}})
	assert.Contains(t, strings.Join(logs, "\n"), "could not check OAuth token")
}

func TestSupergatewayCmd(t *testing.T) {
	t.Setenv("TOK", "secret")
	cmd := supergatewayCmd(config.MCPServerConfig{
		Command: "npx",
		Args:    []string{"-y", "@google-cloud/gcloud-mcp", "--token", "${TOK}"},
	}, 8080, "/mcp")
	assert.Equal(t, []string{
		"npx", "-y", "supergateway",
		"--stdio", "npx -y @google-cloud/gcloud-mcp --token secret",
		"--outputTransport", "streamableHttp",
		"--port", "8080",
		"--streamableHttpPath", "/mcp",
		"--healthEndpoint", "/healthz",
	}, cmd)
}

func TestParseSidecarMounts(t *testing.T) {
	home := t.TempDir()
	o := &Orchestrator{HomeDir: home}

	t.Run("tilde and ro", func(t *testing.T) {
		m, err := o.parseSidecarMounts([]string{"~/.config/gcloud:/creds:ro"})
		require.NoError(t, err)
		require.Len(t, m, 1)
		assert.Equal(t, filepath.Join(home, ".config/gcloud"), m[0].Source)
		assert.Equal(t, "/creds", m[0].Target)
		assert.True(t, m[0].ReadOnly)
	})

	t.Run("env expansion, read-write", func(t *testing.T) {
		t.Setenv("SRC", "/tmp/data")
		m, err := o.parseSidecarMounts([]string{"${SRC}:/data"})
		require.NoError(t, err)
		assert.Equal(t, "/tmp/data", m[0].Source)
		assert.False(t, m[0].ReadOnly)
	})

	t.Run("invalid spec", func(t *testing.T) {
		_, err := o.parseSidecarMounts([]string{"justonepart"})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "expected host:container")
	})
}

func TestCustomSidecarImages(t *testing.T) {
	cfg := &config.Config{MCPServers: []config.MCPServerConfig{
		{Name: "remote", Type: "http", URL: "https://x.com"},
		{Name: "native", Type: "container", Scope: "session", Image: "ghcr.io/x/mcp:1", Port: 8080},
		{Name: "wrapped", Type: "container", Scope: "session", Command: "npx"}, // default wrapper image
		{Name: "wrapped2", Type: "container", Scope: "session", Image: "custom:1", Command: "x"},
	}}
	assert.Equal(t, []string{"ghcr.io/x/mcp:1", config.DefaultMCPWrapperImage, "custom:1"}, customSidecarImages(cfg))
}

func TestStartCustomSidecars(t *testing.T) {
	newOrch := func(t *testing.T, mockCM *MockContainerManager) *Orchestrator {
		return &Orchestrator{Containers: mockCM, HomeDir: t.TempDir(), Log: func(string, ...any) {}}
	}
	sess := func() *Session { return &Session{NetworkName: "net", ProjectID: "proj", SessionID: "sess"} }

	t.Run("native container registers http url", func(t *testing.T) {
		ctrl := gomock.NewController(t)
		mockCM := NewMockContainerManager(ctrl)
		var got container.SharedServiceOptions
		mockCM.EXPECT().ImageExists(gomock.Any(), "ghcr.io/x/mcp:1").Return(true, nil)
		mockCM.EXPECT().StartSharedService(gomock.Any(), gomock.Any()).
			DoAndReturn(func(_ any, opts container.SharedServiceOptions) (string, error) { got = opts; return "id1", nil })
		mockCM.EXPECT().WaitForReady(gomock.Any(), "id1", gomock.Any()).Return(nil)

		o := newOrch(t, mockCM)
		s := sess()
		regs, _ := o.startCustomSidecars(context.Background(), &config.Config{MCPServers: []config.MCPServerConfig{
			{Name: "gcp", Type: "container", Scope: "session", Image: "ghcr.io/x/mcp:1", Port: 9000, Path: "/mcp"},
		}}, s)

		assert.Equal(t, "http://gcp:9000/mcp", regs["gcp"].URL)
		assert.Equal(t, "http", regs["gcp"].Type)
		assert.Equal(t, []string{"forge-mcp-gcp-proj-sess"}, s.SidecarNames)
		assert.Equal(t, "net", got.NetworkName)
		assert.Equal(t, "gcp", got.Alias)
	})

	t.Run("wrapped stdio uses default image and bridge cmd", func(t *testing.T) {
		ctrl := gomock.NewController(t)
		mockCM := NewMockContainerManager(ctrl)
		var got container.SharedServiceOptions
		mockCM.EXPECT().ImageExists(gomock.Any(), config.DefaultMCPWrapperImage).Return(true, nil)
		mockCM.EXPECT().StartSharedService(gomock.Any(), gomock.Any()).
			DoAndReturn(func(_ any, opts container.SharedServiceOptions) (string, error) { got = opts; return "id2", nil })
		mockCM.EXPECT().WaitForReady(gomock.Any(), "id2", gomock.Any()).Return(nil)

		o := newOrch(t, mockCM)
		s := sess()
		regs, _ := o.startCustomSidecars(context.Background(), &config.Config{MCPServers: []config.MCPServerConfig{
			{Name: "tool", Type: "container", Scope: "session", Command: "my-mcp", Args: []string{"--flag"}},
		}}, s)

		assert.Equal(t, "http://tool:8080/mcp", regs["tool"].URL)
		assert.Equal(t, config.DefaultMCPWrapperImage, got.Image)
		assert.Contains(t, got.Cmd, "supergateway")
		assert.Contains(t, got.Cmd, "my-mcp --flag")
	})

	t.Run("pulls image when missing", func(t *testing.T) {
		ctrl := gomock.NewController(t)
		mockCM := NewMockContainerManager(ctrl)
		mockCM.EXPECT().ImageExists(gomock.Any(), "img:1").Return(false, nil)
		mockCM.EXPECT().PullImage(gomock.Any(), "img:1").Return(nil)
		mockCM.EXPECT().StartSharedService(gomock.Any(), gomock.Any()).Return("id3", nil)
		mockCM.EXPECT().WaitForReady(gomock.Any(), "id3", gomock.Any()).Return(nil)

		o := newOrch(t, mockCM)
		s := sess()
		regs, _ := o.startCustomSidecars(context.Background(), &config.Config{MCPServers: []config.MCPServerConfig{
			{Name: "svc", Type: "container", Scope: "session", Image: "img:1", Port: 8080},
		}}, s)
		assert.Contains(t, regs, "svc")
	})

	t.Run("start failure is non-fatal and unregistered", func(t *testing.T) {
		ctrl := gomock.NewController(t)
		mockCM := NewMockContainerManager(ctrl)
		mockCM.EXPECT().ImageExists(gomock.Any(), gomock.Any()).Return(true, nil)
		mockCM.EXPECT().StartSharedService(gomock.Any(), gomock.Any()).Return("", assert.AnError)

		o := newOrch(t, mockCM)
		s := sess()
		regs, _ := o.startCustomSidecars(context.Background(), &config.Config{MCPServers: []config.MCPServerConfig{
			{Name: "bad", Type: "container", Scope: "session", Image: "img:1", Port: 8080},
		}}, s)
		assert.Empty(t, regs)
		assert.Empty(t, s.SidecarNames)
	})

	t.Run("not-ready is skipped but recorded for cleanup", func(t *testing.T) {
		ctrl := gomock.NewController(t)
		mockCM := NewMockContainerManager(ctrl)
		mockCM.EXPECT().ImageExists(gomock.Any(), gomock.Any()).Return(true, nil)
		mockCM.EXPECT().StartSharedService(gomock.Any(), gomock.Any()).Return("id4", nil)
		mockCM.EXPECT().WaitForReady(gomock.Any(), "id4", gomock.Any()).Return(assert.AnError)
		mockCM.EXPECT().ContainerLogs(gomock.Any(), "id4").Return("boom", nil)

		o := newOrch(t, mockCM)
		s := sess()
		regs, _ := o.startCustomSidecars(context.Background(), &config.Config{MCPServers: []config.MCPServerConfig{
			{Name: "slow", Type: "container", Scope: "session", Image: "img:1", Port: 8080},
		}}, s)
		assert.Empty(t, regs)
		assert.Equal(t, []string{"forge-mcp-slow-proj-sess"}, s.SidecarNames)
	})

	t.Run("non-container servers are ignored", func(t *testing.T) {
		ctrl := gomock.NewController(t)
		mockCM := NewMockContainerManager(ctrl)
		o := newOrch(t, mockCM)
		s := sess()
		regs, _ := o.startCustomSidecars(context.Background(), &config.Config{MCPServers: []config.MCPServerConfig{
			{Name: "remote", Type: "http", URL: "https://x.com"},
			{Name: "local", Type: "stdio", Command: "x"},
		}}, s)
		assert.Empty(t, regs)
		assert.Empty(t, s.SidecarNames)
	})
}

func TestStartCustomSidecars_EdgeBranches(t *testing.T) {
	t.Run("invalid mount skips server", func(t *testing.T) {
		ctrl := gomock.NewController(t)
		mockCM := NewMockContainerManager(ctrl)
		// No Start/ImageExists expected — bails at mount parsing.
		o := &Orchestrator{Containers: mockCM, HomeDir: t.TempDir(), Log: func(string, ...any) {}}
		s := &Session{NetworkName: "net", ProjectID: "p", SessionID: "s"}
		regs, _ := o.startCustomSidecars(context.Background(), &config.Config{MCPServers: []config.MCPServerConfig{
			{Name: "svc", Type: "container", Scope: "session", Image: "img:1", Port: 8080, Mounts: []string{"bad"}},
		}}, s)
		assert.Empty(t, regs)
		assert.Empty(t, s.SidecarNames)
	})

	t.Run("image check error still attempts start", func(t *testing.T) {
		ctrl := gomock.NewController(t)
		mockCM := NewMockContainerManager(ctrl)
		mockCM.EXPECT().ImageExists(gomock.Any(), "img:1").Return(false, assert.AnError)
		mockCM.EXPECT().StartSharedService(gomock.Any(), gomock.Any()).Return("id", nil)
		mockCM.EXPECT().WaitForReady(gomock.Any(), "id", gomock.Any()).Return(nil)
		o := &Orchestrator{Containers: mockCM, HomeDir: t.TempDir(), Log: func(string, ...any) {}}
		s := &Session{NetworkName: "net", ProjectID: "p", SessionID: "s"}
		regs, _ := o.startCustomSidecars(context.Background(), &config.Config{MCPServers: []config.MCPServerConfig{
			{Name: "svc", Type: "container", Scope: "session", Image: "img:1", Port: 8080},
		}}, s)
		assert.Contains(t, regs, "svc")
	})
}

func TestStartCustomSidecars_GlobalScope(t *testing.T) {
	t.Run("global sidecar runs as shared singleton and signals shared network", func(t *testing.T) {
		ctrl := gomock.NewController(t)
		mockCM := NewMockContainerManager(ctrl)
		var got container.SharedServiceOptions
		mockCM.EXPECT().EnsureSharedNetwork(gomock.Any(), "forge-shared").Return("net", nil)
		mockCM.EXPECT().IsContainerRunning(gomock.Any(), "forge-mcp-global-gcloud").Return(false, nil)
		mockCM.EXPECT().RemoveContainer(gomock.Any(), "forge-mcp-global-gcloud").Return(nil)
		mockCM.EXPECT().ImageExists(gomock.Any(), config.DefaultMCPWrapperImage).Return(true, nil)
		mockCM.EXPECT().StartSharedService(gomock.Any(), gomock.Any()).
			DoAndReturn(func(_ any, opts container.SharedServiceOptions) (string, error) { got = opts; return "gid", nil })
		mockCM.EXPECT().WaitForReady(gomock.Any(), "gid", gomock.Any()).Return(nil)

		o := &Orchestrator{Containers: mockCM, HomeDir: t.TempDir(), Log: func(string, ...any) {}}
		s := &Session{NetworkName: "net", ProjectID: "p", SessionID: "s"}
		regs, shared := o.startCustomSidecars(context.Background(), &config.Config{MCPServers: []config.MCPServerConfig{
			{Name: "gcloud", Type: "container", Scope: "global", Command: "npx"},
		}}, s)

		assert.True(t, shared, "global sidecar should signal shared-network use")
		assert.Equal(t, "http://gcloud:8080/mcp", regs["gcloud"].URL)
		assert.Equal(t, "forge-shared", got.NetworkName)
		assert.Equal(t, "forge-mcp-global-gcloud", got.Name)
		// Global sidecars are not tracked for per-session cleanup.
		assert.Empty(t, s.SidecarNames)
	})

	t.Run("no scope defaults to global", func(t *testing.T) {
		ctrl := gomock.NewController(t)
		mockCM := NewMockContainerManager(ctrl)
		mockCM.EXPECT().EnsureSharedNetwork(gomock.Any(), "forge-shared").Return("net", nil)
		mockCM.EXPECT().IsContainerRunning(gomock.Any(), "forge-mcp-global-svc").Return(false, nil)
		mockCM.EXPECT().RemoveContainer(gomock.Any(), "forge-mcp-global-svc").Return(nil)
		mockCM.EXPECT().ImageExists(gomock.Any(), "img:1").Return(true, nil)
		mockCM.EXPECT().StartSharedService(gomock.Any(), gomock.Any()).Return("id", nil)
		mockCM.EXPECT().WaitForReady(gomock.Any(), "id", gomock.Any()).Return(nil)

		o := &Orchestrator{Containers: mockCM, HomeDir: t.TempDir(), Log: func(string, ...any) {}}
		s := &Session{NetworkName: "net", ProjectID: "p", SessionID: "s"}
		regs, shared := o.startCustomSidecars(context.Background(), &config.Config{MCPServers: []config.MCPServerConfig{
			{Name: "svc", Type: "container", Image: "img:1", Port: 8080}, // no scope
		}}, s)

		assert.True(t, shared)
		assert.Contains(t, regs, "svc")
		assert.Empty(t, s.SidecarNames, "global sidecars are not tracked for per-session cleanup")
	})

	t.Run("global sidecar reused when already running", func(t *testing.T) {
		ctrl := gomock.NewController(t)
		mockCM := NewMockContainerManager(ctrl)
		mockCM.EXPECT().EnsureSharedNetwork(gomock.Any(), "forge-shared").Return("net", nil)
		mockCM.EXPECT().IsContainerRunning(gomock.Any(), "forge-mcp-global-svc").Return(true, nil)
		// No RemoveContainer/StartSharedService/WaitForReady — it's reused.

		o := &Orchestrator{Containers: mockCM, HomeDir: t.TempDir(), Log: func(string, ...any) {}}
		s := &Session{NetworkName: "net", ProjectID: "p", SessionID: "s"}
		regs, shared := o.startCustomSidecars(context.Background(), &config.Config{MCPServers: []config.MCPServerConfig{
			{Name: "svc", Type: "container", Scope: "global", Image: "img:1", Port: 9000},
		}}, s)

		assert.True(t, shared)
		assert.Equal(t, "http://svc:9000/mcp", regs["svc"].URL)
	})
}

func TestRestartSharedMCP_GlobalSidecar(t *testing.T) {
	ctrl := gomock.NewController(t)
	mockCM := NewMockContainerManager(ctrl)
	orch, homeDir := setupOrchestrator(t, mockCM)

	configDir := filepath.Join(homeDir, ".config", "claude-forge")
	configContent := `mcp_servers:
  - name: gcloud
    type: container
    scope: global
    command: npx
`
	require.NoError(t, os.WriteFile(filepath.Join(configDir, "config.yaml"), []byte(configContent), 0o644))

	name := "forge-mcp-global-gcloud"
	gomock.InOrder(
		mockCM.EXPECT().IsContainerRunning(gomock.Any(), name).Return(true, nil),  // RestartSharedMCP
		mockCM.EXPECT().IsContainerRunning(gomock.Any(), name).Return(false, nil), // ensureGlobalSidecar
	)
	mockCM.EXPECT().StopContainer(gomock.Any(), name).Return(nil)
	mockCM.EXPECT().RemoveContainer(gomock.Any(), name).Return(nil).Times(2)
	mockCM.EXPECT().EnsureSharedNetwork(gomock.Any(), "forge-shared").Return("net", nil)
	mockCM.EXPECT().ImageExists(gomock.Any(), config.DefaultMCPWrapperImage).Return(true, nil)
	mockCM.EXPECT().StartSharedService(gomock.Any(), gomock.Any()).Return("gid", nil)
	mockCM.EXPECT().WaitForReady(gomock.Any(), "gid", gomock.Any()).Return(nil)

	require.NoError(t, orch.RestartSharedMCP(context.Background()))
}

func TestEnsureGlobalSidecar_Failures(t *testing.T) {
	newOrch := func(mockCM *MockContainerManager) *Orchestrator {
		return &Orchestrator{Containers: mockCM, HomeDir: t.TempDir(), Log: func(string, ...any) {}}
	}

	t.Run("mount error skips before touching containers", func(t *testing.T) {
		ctrl := gomock.NewController(t)
		mockCM := NewMockContainerManager(ctrl)
		// No container calls expected.
		_, ok := newOrch(mockCM).ensureGlobalSidecar(context.Background(),
			config.MCPServerConfig{Name: "g", Type: "container", Scope: "global", Image: "i:1", Port: 8080, Mounts: []string{"bad"}})
		assert.False(t, ok)
	})

	t.Run("shared network error", func(t *testing.T) {
		ctrl := gomock.NewController(t)
		mockCM := NewMockContainerManager(ctrl)
		mockCM.EXPECT().EnsureSharedNetwork(gomock.Any(), "forge-shared").Return("", assert.AnError)
		_, ok := newOrch(mockCM).ensureGlobalSidecar(context.Background(),
			config.MCPServerConfig{Name: "g", Type: "container", Scope: "global", Image: "i:1", Port: 8080})
		assert.False(t, ok)
	})

	t.Run("start failure", func(t *testing.T) {
		ctrl := gomock.NewController(t)
		mockCM := NewMockContainerManager(ctrl)
		mockCM.EXPECT().EnsureSharedNetwork(gomock.Any(), "forge-shared").Return("net", nil)
		mockCM.EXPECT().IsContainerRunning(gomock.Any(), "forge-mcp-global-g").Return(false, nil)
		mockCM.EXPECT().RemoveContainer(gomock.Any(), "forge-mcp-global-g").Return(nil)
		mockCM.EXPECT().ImageExists(gomock.Any(), "i:1").Return(true, nil)
		mockCM.EXPECT().StartSharedService(gomock.Any(), gomock.Any()).Return("", assert.AnError)
		_, ok := newOrch(mockCM).ensureGlobalSidecar(context.Background(),
			config.MCPServerConfig{Name: "g", Type: "container", Scope: "global", Image: "i:1", Port: 8080})
		assert.False(t, ok)
	})

	t.Run("not ready", func(t *testing.T) {
		ctrl := gomock.NewController(t)
		mockCM := NewMockContainerManager(ctrl)
		mockCM.EXPECT().EnsureSharedNetwork(gomock.Any(), "forge-shared").Return("net", nil)
		mockCM.EXPECT().IsContainerRunning(gomock.Any(), "forge-mcp-global-g").Return(false, nil)
		mockCM.EXPECT().RemoveContainer(gomock.Any(), "forge-mcp-global-g").Return(nil)
		mockCM.EXPECT().ImageExists(gomock.Any(), "i:1").Return(true, nil)
		mockCM.EXPECT().StartSharedService(gomock.Any(), gomock.Any()).Return("id", nil)
		mockCM.EXPECT().WaitForReady(gomock.Any(), "id", gomock.Any()).Return(assert.AnError)
		mockCM.EXPECT().ContainerLogs(gomock.Any(), "id").Return("boom", nil)
		_, ok := newOrch(mockCM).ensureGlobalSidecar(context.Background(),
			config.MCPServerConfig{Name: "g", Type: "container", Scope: "global", Image: "i:1", Port: 8080})
		assert.False(t, ok)
	})
}

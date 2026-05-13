package forge

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/michael-freling/claude-code-tools/internal/forge/container"
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
	mockCM.EXPECT().ImageExists(gomock.Any(), gomock.Any()).Return(true, nil).Times(2)
	mockCM.EXPECT().CreateNetwork(gomock.Any(), gomock.Any()).Return("net-id", nil)
	mockCM.EXPECT().StartGateway(gomock.Any(), gomock.Any()).Return("gw-id", nil)
	mockCM.EXPECT().WaitForReady(gomock.Any(), "gw-id", gomock.Any()).Return(nil)
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
	mockCM.EXPECT().ImageExists(gomock.Any(), gomock.Any()).Return(false, nil).Times(2)
	mockCM.EXPECT().PullImage(gomock.Any(), gomock.Any()).Return(nil).Times(2)
	mockCM.EXPECT().CreateNetwork(gomock.Any(), gomock.Any()).Return("net-id", nil)
	mockCM.EXPECT().StartGateway(gomock.Any(), gomock.Any()).Return("gw-id", nil)
	mockCM.EXPECT().WaitForReady(gomock.Any(), "gw-id", gomock.Any()).Return(nil)
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

	mockCM.EXPECT().ImageExists(gomock.Any(), gomock.Any()).Return(true, nil).Times(2)
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

	mockCM.EXPECT().ImageExists(gomock.Any(), gomock.Any()).Return(true, nil).Times(2)
	mockCM.EXPECT().CreateNetwork(gomock.Any(), gomock.Any()).Return("net-id", nil)
	mockCM.EXPECT().StartGateway(gomock.Any(), gomock.Any()).Return("", fmt.Errorf("gateway start failed"))

	// Expect cleanup calls
	mockCM.EXPECT().StopContainer(gomock.Any(), gomock.Any()).Return(nil).Times(2)
	mockCM.EXPECT().RemoveContainer(gomock.Any(), gomock.Any()).Return(nil).Times(2)
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

	mockCM.EXPECT().ImageExists(gomock.Any(), gomock.Any()).Return(true, nil).Times(2)
	mockCM.EXPECT().CreateNetwork(gomock.Any(), gomock.Any()).Return("net-id", nil)
	mockCM.EXPECT().StartGateway(gomock.Any(), gomock.Any()).Return("gw-id", nil)
	mockCM.EXPECT().WaitForReady(gomock.Any(), "gw-id", gomock.Any()).Return(fmt.Errorf("container gw-id exited with code 1"))
	mockCM.EXPECT().ContainerLogs(gomock.Any(), "gw-id").Return("Error: no GITHUB_TOKEN set", nil)

	// Expect cleanup calls
	mockCM.EXPECT().StopContainer(gomock.Any(), gomock.Any()).Return(nil).Times(2)
	mockCM.EXPECT().RemoveContainer(gomock.Any(), gomock.Any()).Return(nil).Times(2)
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

	mockCM.EXPECT().ImageExists(gomock.Any(), gomock.Any()).Return(true, nil).Times(2)
	mockCM.EXPECT().CreateNetwork(gomock.Any(), gomock.Any()).Return("net-id", nil)
	mockCM.EXPECT().StartGateway(gomock.Any(), gomock.Any()).Return("gw-id", nil)
	mockCM.EXPECT().WaitForReady(gomock.Any(), "gw-id", gomock.Any()).Return(nil)
	mockCM.EXPECT().StartAgent(gomock.Any(), gomock.Any()).Return("", fmt.Errorf("agent start failed"))

	// Expect cleanup calls
	mockCM.EXPECT().StopContainer(gomock.Any(), gomock.Any()).Return(nil).Times(2)
	mockCM.EXPECT().RemoveContainer(gomock.Any(), gomock.Any()).Return(nil).Times(2)
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
	}, nil)

	// Expect stop + remove for each container
	mockCM.EXPECT().StopContainer(gomock.Any(), gomock.Any()).Return(nil).Times(2)
	mockCM.EXPECT().RemoveContainer(gomock.Any(), gomock.Any()).Return(nil).Times(2)
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

	mockCM.EXPECT().PullImage(gomock.Any(), gomock.Any()).Return(nil).Times(2)

	err := orch.Build(context.Background())
	require.NoError(t, err)
}

func TestCleanup(t *testing.T) {
	ctrl := gomock.NewController(t)

	mockCM := NewMockContainerManager(ctrl)
	orch, _ := setupOrchestrator(t, mockCM)

	sess := &Session{
		AgentName:   "forge-agent-proj-sess1234",
		GatewayName: "forge-gateway-proj-sess1234",
		NetworkName: "forge_net_proj_sess1234",
	}

	mockCM.EXPECT().StopContainer(gomock.Any(), "forge-agent-proj-sess1234").Return(nil)
	mockCM.EXPECT().RemoveContainer(gomock.Any(), "forge-agent-proj-sess1234").Return(nil)
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

			mockCM.EXPECT().ImageExists(gomock.Any(), gomock.Any()).Return(true, nil).Times(2)
			mockCM.EXPECT().CreateNetwork(gomock.Any(), gomock.Any()).Return("net-id", nil)
			mockCM.EXPECT().StartGateway(gomock.Any(), gomock.Any()).Return("gw-id", nil)
			mockCM.EXPECT().WaitForReady(gomock.Any(), "gw-id", gomock.Any()).Return(nil)
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
	ctrl := gomock.NewController(t)

	mockCM := NewMockContainerManager(ctrl)
	orch, _ := setupOrchestrator(t, mockCM)

	projectDir := setupGitProject(t)
	t.Setenv("ANTHROPIC_API_KEY", "")
	t.Setenv("CLAUDE_CODE_OAUTH_TOKEN", "oauth-token-xyz")

	mockCM.EXPECT().ImageExists(gomock.Any(), gomock.Any()).Return(true, nil).Times(2)
	mockCM.EXPECT().CreateNetwork(gomock.Any(), gomock.Any()).Return("net-id", nil)
	mockCM.EXPECT().StartGateway(gomock.Any(), gomock.Any()).Return("gw-id", nil)
	mockCM.EXPECT().WaitForReady(gomock.Any(), "gw-id", gomock.Any()).Return(nil)
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
}

func TestStart_CommandArgs(t *testing.T) {
	ctrl := gomock.NewController(t)

	mockCM := NewMockContainerManager(ctrl)
	orch, _ := setupOrchestrator(t, mockCM)

	projectDir := setupGitProject(t)
	t.Setenv("ANTHROPIC_API_KEY", "sk-ant-test-key")

	mockCM.EXPECT().ImageExists(gomock.Any(), gomock.Any()).Return(true, nil).Times(2)
	mockCM.EXPECT().CreateNetwork(gomock.Any(), gomock.Any()).Return("net-id", nil)
	mockCM.EXPECT().StartGateway(gomock.Any(), gomock.Any()).Return("gw-id", nil)
	mockCM.EXPECT().WaitForReady(gomock.Any(), "gw-id", gomock.Any()).Return(nil)
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

func TestStart_ResumeSession(t *testing.T) {
	ctrl := gomock.NewController(t)

	mockCM := NewMockContainerManager(ctrl)
	orch, _ := setupOrchestrator(t, mockCM)

	projectDir := setupGitProject(t)
	t.Setenv("ANTHROPIC_API_KEY", "sk-ant-test-key")

	mockCM.EXPECT().ImageExists(gomock.Any(), gomock.Any()).Return(true, nil).Times(2)
	mockCM.EXPECT().CreateNetwork(gomock.Any(), gomock.Any()).Return("net-id", nil)
	mockCM.EXPECT().StartGateway(gomock.Any(), gomock.Any()).Return("gw-id", nil)
	mockCM.EXPECT().WaitForReady(gomock.Any(), "gw-id", gomock.Any()).Return(nil)
	mockCM.EXPECT().StartAgent(gomock.Any(), gomock.Any()).
		DoAndReturn(func(ctx context.Context, opts container.AgentOptions) (string, error) {
			assert.Contains(t, opts.Cmd, "--resume")
			assert.Contains(t, opts.Cmd, "abc12345")
			// --continue should NOT be set when ResumeID is provided
			assert.NotContains(t, opts.Cmd, "--continue")
			return "agent-id", nil
		})

	sess, err := orch.Start(context.Background(), StartOptions{
		ResumeID:   "abc12345",
		ProjectDir: projectDir,
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

	mockCM.EXPECT().ImageExists(gomock.Any(), gomock.Any()).Return(true, nil).Times(2)
	mockCM.EXPECT().CreateNetwork(gomock.Any(), gomock.Any()).Return("net-id", nil)
	mockCM.EXPECT().StartGateway(gomock.Any(), gomock.Any()).Return("gw-id", nil)
	mockCM.EXPECT().WaitForReady(gomock.Any(), "gw-id", gomock.Any()).Return(nil)
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

	// First image pull succeeds, second fails
	gomock.InOrder(
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

		mockCM.EXPECT().ImageExists(gomock.Any(), gomock.Any()).Return(true, nil).Times(2)
		mockCM.EXPECT().CreateNetwork(gomock.Any(), gomock.Any()).Return("net-id", nil)
		mockCM.EXPECT().StartGateway(gomock.Any(), gomock.Any()).Return("gw-id", nil)
		mockCM.EXPECT().WaitForReady(gomock.Any(), "gw-id", gomock.Any()).Return(nil)
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

		mockCM.EXPECT().ImageExists(gomock.Any(), gomock.Any()).Return(true, nil).Times(2)
		mockCM.EXPECT().CreateNetwork(gomock.Any(), gomock.Any()).Return("net-id", nil)
		mockCM.EXPECT().StartGateway(gomock.Any(), gomock.Any()).Return("gw-id", nil)
		mockCM.EXPECT().WaitForReady(gomock.Any(), "gw-id", gomock.Any()).Return(nil)
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

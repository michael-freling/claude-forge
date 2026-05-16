package container

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/api/types/image"
	"github.com/docker/docker/api/types/network"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
)

func TestCreateNetwork(t *testing.T) {
	tests := []struct {
		name        string
		networkName string
		setupMock   func(*MockDockerAPI)
		wantID      string
		wantErr     bool
		errContains string
	}{
		{
			name:        "creates network successfully",
			networkName: "forge_net",
			setupMock: func(m *MockDockerAPI) {
				m.EXPECT().
					NetworkCreate(gomock.Any(), "forge_net", network.CreateOptions{Driver: "bridge"}).
					Return(network.CreateResponse{ID: "net-123"}, nil)
			},
			wantID: "net-123",
		},
		{
			name:        "fails when Docker returns error",
			networkName: "forge_net",
			setupMock: func(m *MockDockerAPI) {
				m.EXPECT().
					NetworkCreate(gomock.Any(), "forge_net", network.CreateOptions{Driver: "bridge"}).
					Return(network.CreateResponse{}, fmt.Errorf("network already exists"))
			},
			wantErr:     true,
			errContains: "failed to create network",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctrl := gomock.NewController(t)
			defer ctrl.Finish()

			mockAPI := NewMockDockerAPI(ctrl)
			tt.setupMock(mockAPI)

			client := newClientWithAPI(mockAPI)
			ctx := context.Background()

			id, err := client.CreateNetwork(ctx, tt.networkName)

			if tt.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.errContains)
				return
			}

			require.NoError(t, err)
			assert.Equal(t, tt.wantID, id)
		})
	}
}

func TestRemoveNetwork(t *testing.T) {
	tests := []struct {
		name        string
		networkName string
		setupMock   func(*MockDockerAPI)
		wantErr     bool
		errContains string
	}{
		{
			name:        "removes network successfully",
			networkName: "forge_net",
			setupMock: func(m *MockDockerAPI) {
				m.EXPECT().
					NetworkRemove(gomock.Any(), "forge_net").
					Return(nil)
			},
		},
		{
			name:        "fails when Docker returns error",
			networkName: "forge_net",
			setupMock: func(m *MockDockerAPI) {
				m.EXPECT().
					NetworkRemove(gomock.Any(), "forge_net").
					Return(fmt.Errorf("network not found"))
			},
			wantErr:     true,
			errContains: "failed to remove network",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctrl := gomock.NewController(t)
			defer ctrl.Finish()

			mockAPI := NewMockDockerAPI(ctrl)
			tt.setupMock(mockAPI)

			client := newClientWithAPI(mockAPI)
			ctx := context.Background()

			err := client.RemoveNetwork(ctx, tt.networkName)

			if tt.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.errContains)
				return
			}

			require.NoError(t, err)
		})
	}
}

func TestStartAgent(t *testing.T) {
	tests := []struct {
		name        string
		opts        AgentOptions
		setupMock   func(*MockDockerAPI)
		wantID      string
		wantErr     bool
		errContains string
	}{
		{
			name: "creates and starts agent container",
			opts: AgentOptions{
				Name:        "forge-agent-test-project-session1",
				Image:       "agent:latest",
				NetworkName: "forge_net",
				ProjectDir:  "/home/user/my-project",
				Env:         map[string]string{"ANTHROPIC_API_KEY": "sk-test"},
				Privileged:  true,
				Interactive: true,
				Cmd:         []string{"--dangerously-skip-permissions"},
			},
			setupMock: func(m *MockDockerAPI) {
				m.EXPECT().
					ContainerCreate(
						gomock.Any(),
						gomock.Any(),
						gomock.Any(),
						gomock.Any(),
						"forge-agent-test-project-session1",
					).
					DoAndReturn(func(ctx context.Context, config *container.Config, hostConfig *container.HostConfig, netConfig *network.NetworkingConfig, name string) (container.CreateResponse, error) {
						assert.Equal(t, "agent:latest", config.Image)
						assert.Empty(t, config.Entrypoint)
						assert.Equal(t, []string{"claude", "--dangerously-skip-permissions"}, []string(config.Cmd))
						assert.Equal(t, "/work", config.WorkingDir)
						assert.Contains(t, config.Env, "ANTHROPIC_API_KEY=sk-test")
						assert.True(t, config.Tty, "Tty should be true when Interactive is true")
						assert.True(t, config.OpenStdin, "OpenStdin should be true when Interactive is true")
						assert.True(t, hostConfig.Privileged)
						assert.Contains(t, netConfig.EndpointsConfig, "forge_net")

						// Check project dir mount
						foundProjectMount := false
						for _, m := range hostConfig.Mounts {
							if m.Target == "/work" && m.Source == "/home/user/my-project" {
								foundProjectMount = true
							}
						}
						assert.True(t, foundProjectMount, "project dir mount not found")

						return container.CreateResponse{ID: "container-123"}, nil
					})
				m.EXPECT().
					ContainerStart(gomock.Any(), "container-123", container.StartOptions{}).
					Return(nil)
			},
			wantID: "container-123",
		},
		{
			name: "fails when container create fails",
			opts: AgentOptions{
				Name:        "forge-agent-test",
				Image:       "agent:latest",
				NetworkName: "forge_net",
				ProjectDir:  "/home/user/project",
			},
			setupMock: func(m *MockDockerAPI) {
				m.EXPECT().
					ContainerCreate(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
					Return(container.CreateResponse{}, fmt.Errorf("image not found"))
			},
			wantErr:     true,
			errContains: "failed to create agent container",
		},
		{
			name: "fails when container start fails",
			opts: AgentOptions{
				Name:        "forge-agent-test",
				Image:       "agent:latest",
				NetworkName: "forge_net",
				ProjectDir:  "/home/user/project",
			},
			setupMock: func(m *MockDockerAPI) {
				m.EXPECT().
					ContainerCreate(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
					Return(container.CreateResponse{ID: "container-123"}, nil)
				m.EXPECT().
					ContainerStart(gomock.Any(), "container-123", container.StartOptions{}).
					Return(fmt.Errorf("start failed"))
			},
			wantErr:     true,
			errContains: "failed to start agent container",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctrl := gomock.NewController(t)
			defer ctrl.Finish()

			mockAPI := NewMockDockerAPI(ctrl)
			tt.setupMock(mockAPI)

			client := newClientWithAPI(mockAPI)
			ctx := context.Background()

			id, err := client.StartAgent(ctx, tt.opts)

			if tt.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.errContains)
				return
			}

			require.NoError(t, err)
			assert.Equal(t, tt.wantID, id)
		})
	}
}

func TestStartAgent_Mounts(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockAPI := NewMockDockerAPI(ctrl)

	// Create real temp directories so EvalSymlinks succeeds
	homeDir := t.TempDir()
	claudeDir := filepath.Join(homeDir, ".claude")
	configDir := filepath.Join(homeDir, ".config", "claude-forge")
	sessionDir := filepath.Join(homeDir, ".claude-forge", "project-id")
	projectDir := filepath.Join(homeDir, "project")

	for _, dir := range []string{
		filepath.Join(claudeDir, "rules"),
		filepath.Join(claudeDir, "agents"),
		filepath.Join(claudeDir, "commands"),
		filepath.Join(claudeDir, "skills"),
		configDir,
		sessionDir,
		projectDir,
	} {
		require.NoError(t, os.MkdirAll(dir, 0o755))
	}
	// Create required config files and CLAUDE.md
	for _, f := range []string{
		filepath.Join(configDir, "settings.json"),
		filepath.Join(configDir, ".claude.json"),
		filepath.Join(configDir, "gitconfig"),
		filepath.Join(homeDir, "CLAUDE.md"),
	} {
		require.NoError(t, os.WriteFile(f, []byte("{}"), 0o644))
	}

	opts := AgentOptions{
		Name:        "forge-agent-project-session",
		Image:       "agent:latest",
		NetworkName: "forge_net",
		ProjectDir:  projectDir,
		SessionDir:  sessionDir,
		ClaudeDir:   claudeDir,
		ConfigDir:   configDir,
		HomeDir:     homeDir,
	}

	mockAPI.EXPECT().
		ContainerCreate(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
		DoAndReturn(func(ctx context.Context, config *container.Config, hostConfig *container.HostConfig, netConfig *network.NetworkingConfig, name string) (container.CreateResponse, error) {
			mountsByTarget := make(map[string]string)
			for _, m := range hostConfig.Mounts {
				mountsByTarget[m.Target] = m.Source
			}

			assert.Contains(t, mountsByTarget, "/work", "project mount missing")
			assert.Contains(t, mountsByTarget, "/home/user/.claude/rules", "claude rules mount missing")
			assert.Contains(t, mountsByTarget, "/home/user/.claude/agents", "claude agents mount missing")
			assert.Contains(t, mountsByTarget, "/home/user/.claude/commands", "claude commands mount missing")
			assert.Contains(t, mountsByTarget, "/home/user/.claude/skills", "claude skills mount missing")
			assert.Contains(t, mountsByTarget, "/home/user/.claude/settings.json", "settings.json mount missing")
			assert.Contains(t, mountsByTarget, "/home/user/.gitconfig", "gitconfig mount missing")
			assert.Contains(t, mountsByTarget, "/home/user/CLAUDE.md", "home CLAUDE.md mount missing")

			// Session dir mounts at /home/user/.claude/projects (parent) so Claude Code's
			// writes under -work/ and -work-.claude-worktrees-*/ both reach the host.
			assert.Equal(t, sessionDir, mountsByTarget["/home/user/.claude/projects"],
				"session dir mount missing or pointed at wrong source")

			return container.CreateResponse{ID: "c-123"}, nil
		})

	mockAPI.EXPECT().
		ContainerStart(gomock.Any(), "c-123", container.StartOptions{}).
		Return(nil)

	client := newClientWithAPI(mockAPI)
	ctx := context.Background()

	_, err := client.StartAgent(ctx, opts)
	require.NoError(t, err)
}

func TestStartAgent_SymlinkMounts(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockAPI := NewMockDockerAPI(ctrl)

	homeDir := t.TempDir()
	claudeDir := filepath.Join(homeDir, ".claude")
	require.NoError(t, os.MkdirAll(claudeDir, 0o755))

	// Create real target directories outside claudeDir
	realAgentsDir := filepath.Join(homeDir, "shared-agents")
	realCommandsDir := filepath.Join(homeDir, "shared-commands")
	require.NoError(t, os.MkdirAll(realAgentsDir, 0o755))
	require.NoError(t, os.MkdirAll(realCommandsDir, 0o755))

	// Create symlinks: ~/.claude/agents -> shared-agents, ~/.claude/commands -> shared-commands
	require.NoError(t, os.Symlink(realAgentsDir, filepath.Join(claudeDir, "agents")))
	require.NoError(t, os.Symlink(realCommandsDir, filepath.Join(claudeDir, "commands")))
	// Create regular directories for rules and skills
	require.NoError(t, os.MkdirAll(filepath.Join(claudeDir, "rules"), 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(claudeDir, "skills"), 0o755))

	// Create symlinked CLAUDE.md
	realCLAUDEmd := filepath.Join(homeDir, "real-claude.md")
	require.NoError(t, os.WriteFile(realCLAUDEmd, []byte("# CLAUDE"), 0o644))
	require.NoError(t, os.Symlink(realCLAUDEmd, filepath.Join(homeDir, "CLAUDE.md")))

	opts := AgentOptions{
		Name:        "forge-agent-project-session",
		Image:       "agent:latest",
		NetworkName: "forge_net",
		ProjectDir:  homeDir,
		ClaudeDir:   claudeDir,
		HomeDir:     homeDir,
	}

	mockAPI.EXPECT().
		ContainerCreate(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
		DoAndReturn(func(ctx context.Context, config *container.Config, hostConfig *container.HostConfig, netConfig *network.NetworkingConfig, name string) (container.CreateResponse, error) {
			mountsByTarget := make(map[string]string)
			for _, m := range hostConfig.Mounts {
				mountsByTarget[m.Target] = m.Source
			}

			// Symlinked dirs must resolve to real paths
			assert.Equal(t, realAgentsDir, mountsByTarget["/home/user/.claude/agents"],
				"agents symlink should resolve to real path")
			assert.Equal(t, realCommandsDir, mountsByTarget["/home/user/.claude/commands"],
				"commands symlink should resolve to real path")

			// Regular dirs should still be mounted
			assert.Contains(t, mountsByTarget, "/home/user/.claude/rules")
			assert.Contains(t, mountsByTarget, "/home/user/.claude/skills")

			// Symlinked CLAUDE.md should resolve to real path
			assert.Equal(t, realCLAUDEmd, mountsByTarget["/home/user/CLAUDE.md"],
				"CLAUDE.md symlink should resolve to real path")

			return container.CreateResponse{ID: "c-123"}, nil
		})

	mockAPI.EXPECT().
		ContainerStart(gomock.Any(), "c-123", container.StartOptions{}).
		Return(nil)

	client := newClientWithAPI(mockAPI)
	ctx := context.Background()

	_, err := client.StartAgent(ctx, opts)
	require.NoError(t, err)
}

func TestStartAgent_NonExistentClaudeDirs(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockAPI := NewMockDockerAPI(ctrl)

	homeDir := t.TempDir()
	claudeDir := filepath.Join(homeDir, ".claude")
	require.NoError(t, os.MkdirAll(claudeDir, 0o755))

	// Only create "rules" — agents, commands, skills don't exist
	require.NoError(t, os.MkdirAll(filepath.Join(claudeDir, "rules"), 0o755))
	// No CLAUDE.md either

	opts := AgentOptions{
		Name:        "forge-agent-project-session",
		Image:       "agent:latest",
		NetworkName: "forge_net",
		ProjectDir:  homeDir,
		ClaudeDir:   claudeDir,
		HomeDir:     homeDir,
	}

	mockAPI.EXPECT().
		ContainerCreate(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
		DoAndReturn(func(ctx context.Context, config *container.Config, hostConfig *container.HostConfig, netConfig *network.NetworkingConfig, name string) (container.CreateResponse, error) {
			mountTargets := make(map[string]bool)
			for _, m := range hostConfig.Mounts {
				mountTargets[m.Target] = true
			}

			// Only rules should be mounted (it's the only one that exists)
			assert.True(t, mountTargets["/home/user/.claude/rules"], "rules mount should exist")
			assert.False(t, mountTargets["/home/user/.claude/agents"], "agents mount should be skipped")
			assert.False(t, mountTargets["/home/user/.claude/commands"], "commands mount should be skipped")
			assert.False(t, mountTargets["/home/user/.claude/skills"], "skills mount should be skipped")
			assert.False(t, mountTargets["/home/user/CLAUDE.md"], "CLAUDE.md mount should be skipped")

			return container.CreateResponse{ID: "c-123"}, nil
		})

	mockAPI.EXPECT().
		ContainerStart(gomock.Any(), "c-123", container.StartOptions{}).
		Return(nil)

	client := newClientWithAPI(mockAPI)
	ctx := context.Background()

	_, err := client.StartAgent(ctx, opts)
	require.NoError(t, err)
}

func TestStartAgent_BrokenSymlinkMounts(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockAPI := NewMockDockerAPI(ctrl)

	homeDir := t.TempDir()
	claudeDir := filepath.Join(homeDir, ".claude")
	require.NoError(t, os.MkdirAll(claudeDir, 0o755))

	// Create a broken symlink: agents -> non-existent target
	require.NoError(t, os.Symlink("/non/existent/path", filepath.Join(claudeDir, "agents")))
	// Create a working regular directory for rules
	require.NoError(t, os.MkdirAll(filepath.Join(claudeDir, "rules"), 0o755))

	// Create a broken symlink for CLAUDE.md
	require.NoError(t, os.Symlink("/non/existent/claude.md", filepath.Join(homeDir, "CLAUDE.md")))

	opts := AgentOptions{
		Name:        "forge-agent-project-session",
		Image:       "agent:latest",
		NetworkName: "forge_net",
		ProjectDir:  homeDir,
		ClaudeDir:   claudeDir,
		HomeDir:     homeDir,
	}

	mockAPI.EXPECT().
		ContainerCreate(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
		DoAndReturn(func(ctx context.Context, config *container.Config, hostConfig *container.HostConfig, netConfig *network.NetworkingConfig, name string) (container.CreateResponse, error) {
			mountTargets := make(map[string]bool)
			for _, m := range hostConfig.Mounts {
				mountTargets[m.Target] = true
			}

			// Broken symlinks should be skipped
			assert.False(t, mountTargets["/home/user/.claude/agents"], "broken agents symlink should be skipped")
			assert.False(t, mountTargets["/home/user/CLAUDE.md"], "broken CLAUDE.md symlink should be skipped")
			// Regular dir should still be mounted
			assert.True(t, mountTargets["/home/user/.claude/rules"], "rules mount should exist")

			return container.CreateResponse{ID: "c-123"}, nil
		})

	mockAPI.EXPECT().
		ContainerStart(gomock.Any(), "c-123", container.StartOptions{}).
		Return(nil)

	client := newClientWithAPI(mockAPI)
	ctx := context.Background()

	_, err := client.StartAgent(ctx, opts)
	require.NoError(t, err)
}

func TestStartAgent_Interactive(t *testing.T) {
	tests := []struct {
		name        string
		interactive bool
		wantTty     bool
		wantStdin   bool
	}{
		{
			name:        "interactive mode sets Tty and OpenStdin",
			interactive: true,
			wantTty:     true,
			wantStdin:   true,
		},
		{
			name:        "non-interactive mode does not set Tty or OpenStdin",
			interactive: false,
			wantTty:     false,
			wantStdin:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctrl := gomock.NewController(t)
			defer ctrl.Finish()

			mockAPI := NewMockDockerAPI(ctrl)

			opts := AgentOptions{
				Name:        "forge-agent-test-session",
				Image:       "agent:latest",
				NetworkName: "forge_net",
				ProjectDir:  "/home/user/project",
				Interactive: tt.interactive,
			}

			mockAPI.EXPECT().
				ContainerCreate(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
				DoAndReturn(func(ctx context.Context, config *container.Config, hostConfig *container.HostConfig, netConfig *network.NetworkingConfig, name string) (container.CreateResponse, error) {
					assert.Equal(t, tt.wantTty, config.Tty, "Tty mismatch")
					assert.Equal(t, tt.wantStdin, config.OpenStdin, "OpenStdin mismatch")
					return container.CreateResponse{ID: "c-123"}, nil
				})

			mockAPI.EXPECT().
				ContainerStart(gomock.Any(), "c-123", container.StartOptions{}).
				Return(nil)

			client := newClientWithAPI(mockAPI)
			ctx := context.Background()

			_, err := client.StartAgent(ctx, opts)
			require.NoError(t, err)
		})
	}
}

func TestStartAgent_UIDGIDEnvVars(t *testing.T) {
	tests := []struct {
		name       string
		uid        int
		gid        int
		wantUID    bool
		wantGID    bool
		wantUIDVal string
		wantGIDVal string
	}{
		{
			name:       "sets FORGE_UID and FORGE_GID when both positive",
			uid:        1000,
			gid:        1000,
			wantUID:    true,
			wantGID:    true,
			wantUIDVal: "FORGE_UID=1000",
			wantGIDVal: "FORGE_GID=1000",
		},
		{
			name:    "does not set FORGE_UID or FORGE_GID when both zero",
			uid:     0,
			gid:     0,
			wantUID: false,
			wantGID: false,
		},
		{
			name:       "sets only FORGE_UID when GID is zero",
			uid:        501,
			gid:        0,
			wantUID:    true,
			wantGID:    false,
			wantUIDVal: "FORGE_UID=501",
		},
		{
			name:       "sets only FORGE_GID when UID is zero",
			uid:        0,
			gid:        20,
			wantUID:    false,
			wantGID:    true,
			wantGIDVal: "FORGE_GID=20",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctrl := gomock.NewController(t)
			defer ctrl.Finish()

			mockAPI := NewMockDockerAPI(ctrl)

			opts := AgentOptions{
				Name:        "forge-agent-test-session",
				Image:       "agent:latest",
				NetworkName: "forge_net",
				ProjectDir:  "/home/user/project",
				UID:         tt.uid,
				GID:         tt.gid,
			}

			mockAPI.EXPECT().
				ContainerCreate(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
				DoAndReturn(func(ctx context.Context, config *container.Config, hostConfig *container.HostConfig, netConfig *network.NetworkingConfig, name string) (container.CreateResponse, error) {
					if tt.wantUID {
						assert.Contains(t, config.Env, tt.wantUIDVal)
					} else {
						for _, e := range config.Env {
							assert.NotContains(t, e, "FORGE_UID=")
						}
					}
					if tt.wantGID {
						assert.Contains(t, config.Env, tt.wantGIDVal)
					} else {
						for _, e := range config.Env {
							assert.NotContains(t, e, "FORGE_GID=")
						}
					}
					return container.CreateResponse{ID: "c-123"}, nil
				})

			mockAPI.EXPECT().
				ContainerStart(gomock.Any(), "c-123", container.StartOptions{}).
				Return(nil)

			client := newClientWithAPI(mockAPI)
			ctx := context.Background()

			_, err := client.StartAgent(ctx, opts)
			require.NoError(t, err)
		})
	}
}

func TestStartAgent_CacheDirMounts(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockAPI := NewMockDockerAPI(ctrl)

	opts := AgentOptions{
		Name:        "forge-agent-test-session",
		Image:       "agent:latest",
		NetworkName: "forge_net",
		ProjectDir:  "/home/user/project",
		CacheDirs: []CacheDir{
			{Source: "/home/user/go/pkg/mod", Target: "/home/user/go/pkg/mod"},
			{Source: "/home/user/.npm", Target: "/home/user/.npm"},
		},
	}

	mockAPI.EXPECT().
		ContainerCreate(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
		DoAndReturn(func(ctx context.Context, config *container.Config, hostConfig *container.HostConfig, netConfig *network.NetworkingConfig, name string) (container.CreateResponse, error) {
			// Verify cache dir mounts exist and are read-write
			foundGoMod := false
			foundNpm := false
			for _, m := range hostConfig.Mounts {
				if m.Source == "/home/user/go/pkg/mod" && m.Target == "/home/user/go/pkg/mod" {
					foundGoMod = true
					assert.False(t, m.ReadOnly, "go mod cache should be read-write")
				}
				if m.Source == "/home/user/.npm" && m.Target == "/home/user/.npm" {
					foundNpm = true
					assert.False(t, m.ReadOnly, "npm cache should be read-write")
				}
			}
			assert.True(t, foundGoMod, "go mod cache mount not found")
			assert.True(t, foundNpm, "npm cache mount not found")

			return container.CreateResponse{ID: "c-123"}, nil
		})

	mockAPI.EXPECT().
		ContainerStart(gomock.Any(), "c-123", container.StartOptions{}).
		Return(nil)

	client := newClientWithAPI(mockAPI)
	ctx := context.Background()

	_, err := client.StartAgent(ctx, opts)
	require.NoError(t, err)
}

func TestStartAgent_NoCacheDirs(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockAPI := NewMockDockerAPI(ctrl)

	opts := AgentOptions{
		Name:        "forge-agent-test-session",
		Image:       "agent:latest",
		NetworkName: "forge_net",
		ProjectDir:  "/home/user/project",
		// No CacheDirs
	}

	mockAPI.EXPECT().
		ContainerCreate(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
		DoAndReturn(func(ctx context.Context, config *container.Config, hostConfig *container.HostConfig, netConfig *network.NetworkingConfig, name string) (container.CreateResponse, error) {
			// Only the project dir mount should exist
			assert.Len(t, hostConfig.Mounts, 1)
			assert.Equal(t, "/work", hostConfig.Mounts[0].Target)
			return container.CreateResponse{ID: "c-123"}, nil
		})

	mockAPI.EXPECT().
		ContainerStart(gomock.Any(), "c-123", container.StartOptions{}).
		Return(nil)

	client := newClientWithAPI(mockAPI)
	ctx := context.Background()

	_, err := client.StartAgent(ctx, opts)
	require.NoError(t, err)
}

func TestStartGateway(t *testing.T) {
	tests := []struct {
		name        string
		opts        GatewayOptions
		setupMock   func(*MockDockerAPI)
		wantID      string
		wantErr     bool
		errContains string
	}{
		{
			name: "creates and starts gateway container",
			opts: GatewayOptions{
				Name:        "forge-gateway-test-session",
				Image:       "gateway:latest",
				NetworkName: "forge_net",
				SSHDir:      "/home/user/.ssh",
				GHConfigDir: "/home/user/.config/gh",
				Owner:       "michael-freling",
				Repo:        "claude-code-tools",
			},
			setupMock: func(m *MockDockerAPI) {
				m.EXPECT().
					ContainerCreate(
						gomock.Any(),
						gomock.Any(),
						gomock.Any(),
						gomock.Any(),
						"forge-gateway-test-session",
					).
					DoAndReturn(func(ctx context.Context, config *container.Config, hostConfig *container.HostConfig, netConfig *network.NetworkingConfig, name string) (container.CreateResponse, error) {
						assert.Equal(t, "gateway:latest", config.Image)
						assert.Equal(t, []string{"gateway", "--owner=michael-freling", "--repo=claude-code-tools"}, []string(config.Cmd))
						assert.Contains(t, netConfig.EndpointsConfig, "forge_net")

						// Check SSH mount
						foundSSH := false
						foundGH := false
						for _, m := range hostConfig.Mounts {
							if m.Target == "/home/user/.ssh" && m.ReadOnly {
								foundSSH = true
							}
							if m.Target == "/home/user/.config/gh" && m.ReadOnly {
								foundGH = true
							}
						}
						assert.True(t, foundSSH, "SSH mount not found")
						assert.True(t, foundGH, "GH config mount not found")

						return container.CreateResponse{ID: "gw-123"}, nil
					})
				m.EXPECT().
					ContainerStart(gomock.Any(), "gw-123", container.StartOptions{}).
					Return(nil)
			},
			wantID: "gw-123",
		},
		{
			name: "fails when container create fails",
			opts: GatewayOptions{
				Name:        "forge-gateway-test",
				Image:       "gateway:latest",
				NetworkName: "forge_net",
				Owner:       "owner",
				Repo:        "repo",
			},
			setupMock: func(m *MockDockerAPI) {
				m.EXPECT().
					ContainerCreate(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
					Return(container.CreateResponse{}, fmt.Errorf("create failed"))
			},
			wantErr:     true,
			errContains: "failed to create gateway container",
		},
		{
			name: "fails when container start fails",
			opts: GatewayOptions{
				Name:        "forge-gateway-test",
				Image:       "gateway:latest",
				NetworkName: "forge_net",
				Owner:       "owner",
				Repo:        "repo",
			},
			setupMock: func(m *MockDockerAPI) {
				m.EXPECT().
					ContainerCreate(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
					Return(container.CreateResponse{ID: "gw-123"}, nil)
				m.EXPECT().
					ContainerStart(gomock.Any(), "gw-123", container.StartOptions{}).
					Return(fmt.Errorf("start failed"))
			},
			wantErr:     true,
			errContains: "failed to start gateway container",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctrl := gomock.NewController(t)
			defer ctrl.Finish()

			mockAPI := NewMockDockerAPI(ctrl)
			tt.setupMock(mockAPI)

			client := newClientWithAPI(mockAPI)
			ctx := context.Background()

			id, err := client.StartGateway(ctx, tt.opts)

			if tt.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.errContains)
				return
			}

			require.NoError(t, err)
			assert.Equal(t, tt.wantID, id)
		})
	}
}

func TestStopContainer(t *testing.T) {
	tests := []struct {
		name        string
		containerID string
		setupMock   func(*MockDockerAPI)
		wantErr     bool
		errContains string
	}{
		{
			name:        "stops container successfully",
			containerID: "forge-agent-test",
			setupMock: func(m *MockDockerAPI) {
				m.EXPECT().
					ContainerStop(gomock.Any(), "forge-agent-test", container.StopOptions{}).
					Return(nil)
			},
		},
		{
			name:        "fails when stop fails",
			containerID: "forge-agent-test",
			setupMock: func(m *MockDockerAPI) {
				m.EXPECT().
					ContainerStop(gomock.Any(), "forge-agent-test", container.StopOptions{}).
					Return(fmt.Errorf("container not found"))
			},
			wantErr:     true,
			errContains: "failed to stop container",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctrl := gomock.NewController(t)
			defer ctrl.Finish()

			mockAPI := NewMockDockerAPI(ctrl)
			tt.setupMock(mockAPI)

			client := newClientWithAPI(mockAPI)
			ctx := context.Background()

			err := client.StopContainer(ctx, tt.containerID)

			if tt.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.errContains)
				return
			}

			require.NoError(t, err)
		})
	}
}

func TestRemoveContainer(t *testing.T) {
	tests := []struct {
		name        string
		containerID string
		setupMock   func(*MockDockerAPI)
		wantErr     bool
		errContains string
	}{
		{
			name:        "removes container successfully",
			containerID: "forge-agent-test",
			setupMock: func(m *MockDockerAPI) {
				m.EXPECT().
					ContainerRemove(gomock.Any(), "forge-agent-test", container.RemoveOptions{Force: true}).
					Return(nil)
			},
		},
		{
			name:        "fails when remove fails",
			containerID: "forge-agent-test",
			setupMock: func(m *MockDockerAPI) {
				m.EXPECT().
					ContainerRemove(gomock.Any(), "forge-agent-test", container.RemoveOptions{Force: true}).
					Return(fmt.Errorf("container not found"))
			},
			wantErr:     true,
			errContains: "failed to remove container",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctrl := gomock.NewController(t)
			defer ctrl.Finish()

			mockAPI := NewMockDockerAPI(ctrl)
			tt.setupMock(mockAPI)

			client := newClientWithAPI(mockAPI)
			ctx := context.Background()

			err := client.RemoveContainer(ctx, tt.containerID)

			if tt.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.errContains)
				return
			}

			require.NoError(t, err)
		})
	}
}

func TestListForgeContainers(t *testing.T) {
	tests := []struct {
		name        string
		setupMock   func(*MockDockerAPI)
		want        []ContainerInfo
		wantErr     bool
		errContains string
	}{
		{
			name: "lists forge containers",
			setupMock: func(m *MockDockerAPI) {
				m.EXPECT().
					ContainerList(gomock.Any(), gomock.Any()).
					DoAndReturn(func(ctx context.Context, opts container.ListOptions) ([]container.Summary, error) {
						assert.True(t, opts.All)
						return []container.Summary{
							{
								ID:      "c-1",
								Names:   []string{"/forge-agent-project-session1"},
								Image:   "agent:latest",
								Status:  "Up 5 minutes",
								Created: 1715270400,
							},
							{
								ID:      "c-2",
								Names:   []string{"/forge-gateway-project-session1"},
								Image:   "gateway:latest",
								Status:  "Up 5 minutes",
								Created: 1715270400,
							},
						}, nil
					})
			},
			want: []ContainerInfo{
				{
					Name:   "forge-agent-project-session1",
					ID:     "c-1",
					Image:  "agent:latest",
					Status: "Up 5 minutes",
				},
				{
					Name:   "forge-gateway-project-session1",
					ID:     "c-2",
					Image:  "gateway:latest",
					Status: "Up 5 minutes",
				},
			},
		},
		{
			name: "returns empty list when no forge containers",
			setupMock: func(m *MockDockerAPI) {
				m.EXPECT().
					ContainerList(gomock.Any(), gomock.Any()).
					Return([]container.Summary{}, nil)
			},
			want: []ContainerInfo{},
		},
		{
			name: "fails when Docker returns error",
			setupMock: func(m *MockDockerAPI) {
				m.EXPECT().
					ContainerList(gomock.Any(), gomock.Any()).
					Return(nil, fmt.Errorf("daemon not running"))
			},
			wantErr:     true,
			errContains: "failed to list forge containers",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctrl := gomock.NewController(t)
			defer ctrl.Finish()

			mockAPI := NewMockDockerAPI(ctrl)
			tt.setupMock(mockAPI)

			client := newClientWithAPI(mockAPI)
			ctx := context.Background()

			got, err := client.ListForgeContainers(ctx)

			if tt.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.errContains)
				return
			}

			require.NoError(t, err)
			require.Len(t, got, len(tt.want))
			for i, want := range tt.want {
				assert.Equal(t, want.Name, got[i].Name)
				assert.Equal(t, want.ID, got[i].ID)
				assert.Equal(t, want.Image, got[i].Image)
				assert.Equal(t, want.Status, got[i].Status)
			}
		})
	}
}

func TestListForgeContainers_Filters(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockAPI := NewMockDockerAPI(ctrl)

	mockAPI.EXPECT().
		ContainerList(gomock.Any(), gomock.Any()).
		DoAndReturn(func(ctx context.Context, opts container.ListOptions) ([]container.Summary, error) {
			assert.True(t, opts.All)

			// Verify filter arguments
			expectedFilters := filters.NewArgs()
			expectedFilters.Add("name", "forge-agent-")
			expectedFilters.Add("name", "forge-gateway-")
			assert.Equal(t, expectedFilters, opts.Filters)

			return []container.Summary{}, nil
		})

	client := newClientWithAPI(mockAPI)
	ctx := context.Background()

	_, err := client.ListForgeContainers(ctx)
	require.NoError(t, err)
}

func TestPullImage(t *testing.T) {
	tests := []struct {
		name        string
		imageName   string
		setupMock   func(*MockDockerAPI)
		wantErr     bool
		errContains string
	}{
		{
			name:      "pulls image successfully",
			imageName: "agent:latest",
			setupMock: func(m *MockDockerAPI) {
				m.EXPECT().
					ImagePull(gomock.Any(), "agent:latest", image.PullOptions{}).
					Return(io.NopCloser(strings.NewReader("")), nil)
			},
		},
		{
			name:      "fails when pull fails",
			imageName: "agent:latest",
			setupMock: func(m *MockDockerAPI) {
				m.EXPECT().
					ImagePull(gomock.Any(), "agent:latest", image.PullOptions{}).
					Return(nil, fmt.Errorf("unauthorized"))
			},
			wantErr:     true,
			errContains: "failed to pull image",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctrl := gomock.NewController(t)
			defer ctrl.Finish()

			mockAPI := NewMockDockerAPI(ctrl)
			tt.setupMock(mockAPI)

			client := newClientWithAPI(mockAPI)
			ctx := context.Background()

			err := client.PullImage(ctx, tt.imageName)

			if tt.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.errContains)
				return
			}

			require.NoError(t, err)
		})
	}
}

func TestImageExists(t *testing.T) {
	tests := []struct {
		name        string
		imageName   string
		setupMock   func(*MockDockerAPI)
		want        bool
		wantErr     bool
		errContains string
	}{
		{
			name:      "image exists",
			imageName: "agent:latest",
			setupMock: func(m *MockDockerAPI) {
				m.EXPECT().
					ImageList(gomock.Any(), gomock.Any()).
					Return([]image.Summary{{ID: "sha256:abc123"}}, nil)
			},
			want: true,
		},
		{
			name:      "image does not exist",
			imageName: "agent:latest",
			setupMock: func(m *MockDockerAPI) {
				m.EXPECT().
					ImageList(gomock.Any(), gomock.Any()).
					Return([]image.Summary{}, nil)
			},
			want: false,
		},
		{
			name:      "fails when list fails",
			imageName: "agent:latest",
			setupMock: func(m *MockDockerAPI) {
				m.EXPECT().
					ImageList(gomock.Any(), gomock.Any()).
					Return(nil, fmt.Errorf("daemon error"))
			},
			wantErr:     true,
			errContains: "failed to list images",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctrl := gomock.NewController(t)
			defer ctrl.Finish()

			mockAPI := NewMockDockerAPI(ctrl)
			tt.setupMock(mockAPI)

			client := newClientWithAPI(mockAPI)
			ctx := context.Background()

			got, err := client.ImageExists(ctx, tt.imageName)

			if tt.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.errContains)
				return
			}

			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestClose(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockAPI := NewMockDockerAPI(ctrl)
	mockAPI.EXPECT().Close().Return(nil)

	client := newClientWithAPI(mockAPI)

	err := client.Close()
	require.NoError(t, err)
}

func TestWaitForReady(t *testing.T) {
	tests := []struct {
		name        string
		setupMock   func(*MockDockerAPI)
		wantErr     bool
		errContains string
	}{
		{
			name: "returns nil when container is running",
			setupMock: func(m *MockDockerAPI) {
				m.EXPECT().
					ContainerInspect(gomock.Any(), "c-123").
					Return(container.InspectResponse{
						ContainerJSONBase: &container.ContainerJSONBase{
							State: &container.State{Running: true},
						},
					}, nil).
					Times(2)
			},
		},
		{
			name: "returns error when container crashes after starting",
			setupMock: func(m *MockDockerAPI) {
				first := m.EXPECT().
					ContainerInspect(gomock.Any(), "c-123").
					Return(container.InspectResponse{
						ContainerJSONBase: &container.ContainerJSONBase{
							State: &container.State{Running: true},
						},
					}, nil)
				m.EXPECT().
					ContainerInspect(gomock.Any(), "c-123").
					After(first).
					Return(container.InspectResponse{
						ContainerJSONBase: &container.ContainerJSONBase{
							State: &container.State{Status: "exited", ExitCode: 1},
						},
					}, nil)
			},
			wantErr:     true,
			errContains: "exited with code 1",
		},
		{
			name: "returns error when container exited",
			setupMock: func(m *MockDockerAPI) {
				m.EXPECT().
					ContainerInspect(gomock.Any(), "c-123").
					Return(container.InspectResponse{
						ContainerJSONBase: &container.ContainerJSONBase{
							State: &container.State{Status: "exited", ExitCode: 1},
						},
					}, nil)
			},
			wantErr:     true,
			errContains: "exited with code 1",
		},
		{
			name: "returns error when container is dead",
			setupMock: func(m *MockDockerAPI) {
				m.EXPECT().
					ContainerInspect(gomock.Any(), "c-123").
					Return(container.InspectResponse{
						ContainerJSONBase: &container.ContainerJSONBase{
							State: &container.State{Status: "dead", ExitCode: 137},
						},
					}, nil)
			},
			wantErr:     true,
			errContains: "exited with code 137",
		},
		{
			name: "returns error when inspect fails",
			setupMock: func(m *MockDockerAPI) {
				m.EXPECT().
					ContainerInspect(gomock.Any(), "c-123").
					Return(container.InspectResponse{}, fmt.Errorf("no such container"))
			},
			wantErr:     true,
			errContains: "failed to inspect container",
		},
		{
			name: "times out when container stays in created state",
			setupMock: func(m *MockDockerAPI) {
				m.EXPECT().
					ContainerInspect(gomock.Any(), "c-123").
					Return(container.InspectResponse{
						ContainerJSONBase: &container.ContainerJSONBase{
							State: &container.State{Status: "created"},
						},
					}, nil).
					AnyTimes()
			},
			wantErr:     true,
			errContains: "timed out waiting",
		},
		{
			name: "returns error when context is cancelled",
			setupMock: func(m *MockDockerAPI) {
				m.EXPECT().
					ContainerInspect(gomock.Any(), "c-123").
					Return(container.InspectResponse{
						ContainerJSONBase: &container.ContainerJSONBase{
							State: &container.State{Status: "created"},
						},
					}, nil).
					AnyTimes()
			},
			wantErr:     true,
			errContains: "context canceled",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctrl := gomock.NewController(t)
			defer ctrl.Finish()

			mockAPI := NewMockDockerAPI(ctrl)
			tt.setupMock(mockAPI)

			client := newClientWithAPI(mockAPI)
			ctx := context.Background()
			if tt.name == "returns error when context is cancelled" {
				var cancel context.CancelFunc
				ctx, cancel = context.WithCancel(ctx)
				cancel()
			}

			err := client.WaitForReady(ctx, "c-123", 1*time.Second)

			if tt.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.errContains)
				return
			}

			require.NoError(t, err)
		})
	}
}

func TestContainerLogs(t *testing.T) {
	tests := []struct {
		name        string
		setupMock   func(*MockDockerAPI)
		wantLogs    string
		wantErr     bool
		errContains string
	}{
		{
			name: "returns logs from container",
			setupMock: func(m *MockDockerAPI) {
				m.EXPECT().
					ContainerLogs(gomock.Any(), "c-123", gomock.Any()).
					Return(io.NopCloser(strings.NewReader("Error: no GITHUB_TOKEN set")), nil)
			},
			wantLogs: "Error: no GITHUB_TOKEN set",
		},
		{
			name: "returns empty string for empty logs",
			setupMock: func(m *MockDockerAPI) {
				m.EXPECT().
					ContainerLogs(gomock.Any(), "c-123", gomock.Any()).
					Return(io.NopCloser(strings.NewReader("")), nil)
			},
			wantLogs: "",
		},
		{
			name: "returns error when logs fail",
			setupMock: func(m *MockDockerAPI) {
				m.EXPECT().
					ContainerLogs(gomock.Any(), "c-123", gomock.Any()).
					Return(nil, fmt.Errorf("container not found"))
			},
			wantErr:     true,
			errContains: "failed to get logs",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctrl := gomock.NewController(t)
			defer ctrl.Finish()

			mockAPI := NewMockDockerAPI(ctrl)
			tt.setupMock(mockAPI)

			client := newClientWithAPI(mockAPI)
			ctx := context.Background()

			logs, err := client.ContainerLogs(ctx, "c-123")

			if tt.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.errContains)
				return
			}

			require.NoError(t, err)
			assert.Equal(t, tt.wantLogs, logs)
		})
	}
}

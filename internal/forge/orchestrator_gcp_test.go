package forge

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/michael-freling/claude-forge/internal/forge/config"
	"github.com/michael-freling/claude-forge/internal/forge/container"
	"github.com/michael-freling/claude-forge/internal/forge/gcpmcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
)

// writeADC writes a dummy Application Default Credentials file into the default
// host gcloud dir (homeDir/.config/gcloud) so startGCPMCP's preflight passes.
func writeADC(t *testing.T, homeDir string) string {
	t.Helper()
	dir := filepath.Join(homeDir, ".config", "gcloud")
	require.NoError(t, os.MkdirAll(dir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "application_default_credentials.json"),
		[]byte(`{"type":"authorized_user"}`), 0o644))
	return dir
}

func TestStartGCPMCP_StartsNewContainer(t *testing.T) {
	ctrl := gomock.NewController(t)
	mockCM := NewMockContainerManager(ctrl)
	orch, homeDir := setupOrchestrator(t, mockCM)
	gcloudDir := writeADC(t, homeDir)

	cfg := &config.Config{GCP: config.GCPConfig{
		Enabled:           true,
		Image:             "gcp-mcp:latest",
		Project:           "my-proj",
		AllowSecretAccess: true,
	}}

	mockCM.EXPECT().EnsureSharedNetwork(gomock.Any(), "forge-shared").Return("net-id", nil)
	mockCM.EXPECT().IsContainerRunning(gomock.Any(), "forge-gcp-mcp").Return(false, nil)
	mockCM.EXPECT().RemoveContainer(gomock.Any(), "forge-gcp-mcp").Return(nil)
	mockCM.EXPECT().StartSharedService(gomock.Any(), gomock.Any()).
		DoAndReturn(func(ctx context.Context, opts container.SharedServiceOptions) (string, error) {
			assert.Equal(t, "gcp-mcp:latest", opts.Image)
			assert.Equal(t, "gcp-mcp", opts.Alias)
			assert.Equal(t, "forge-shared", opts.NetworkName)
			// HOME must be the parent of the mounted gcloud dir for ADC lookup.
			assert.Equal(t, gcpmcp.MCPServerHome, opts.Env["HOME"])
			assert.Equal(t, gcpmcp.MCPServerGcloudDir, opts.Env["CLOUDSDK_CONFIG"])
			// Read-only mount of the host ADC dir.
			require.Len(t, opts.Mounts, 1)
			assert.Equal(t, gcloudDir, opts.Mounts[0].Source)
			assert.Equal(t, gcpmcp.MCPServerGcloudDir, opts.Mounts[0].Target)
			assert.True(t, opts.Mounts[0].ReadOnly)
			// Config flows into the launch flags; the secret gate is opt-in.
			assert.Contains(t, opts.Cmd, "--project")
			assert.Contains(t, opts.Cmd, "my-proj")
			assert.Contains(t, opts.Cmd, "--allow-secret-access")
			assert.NotContains(t, opts.Cmd, "--allow-writes")
			return "gcp-id", nil
		})
	mockCM.EXPECT().WaitForReady(gomock.Any(), "gcp-id", gomock.Any()).Return(nil)

	require.NoError(t, orch.startGCPMCP(context.Background(), cfg))
}

func TestStartGCPMCP_AlreadyRunning(t *testing.T) {
	ctrl := gomock.NewController(t)
	mockCM := NewMockContainerManager(ctrl)
	orch, homeDir := setupOrchestrator(t, mockCM)
	writeADC(t, homeDir)

	cfg := &config.Config{GCP: config.GCPConfig{Enabled: true, Image: "gcp-mcp:latest"}}

	mockCM.EXPECT().EnsureSharedNetwork(gomock.Any(), "forge-shared").Return("net-id", nil)
	mockCM.EXPECT().IsContainerRunning(gomock.Any(), "forge-gcp-mcp").Return(true, nil)
	// No RemoveContainer / StartSharedService when already running.

	require.NoError(t, orch.startGCPMCP(context.Background(), cfg))
}

func TestStartGCPMCP_MissingADC(t *testing.T) {
	ctrl := gomock.NewController(t)
	mockCM := NewMockContainerManager(ctrl)
	orch, _ := setupOrchestrator(t, mockCM)

	cfg := &config.Config{GCP: config.GCPConfig{Enabled: true, Image: "gcp-mcp:latest"}}

	// Preflight fails before any container call, so no mock expectations.
	err := orch.startGCPMCP(context.Background(), cfg)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no Application Default Credentials found")
}

func TestStartGCPMCP_StartFails(t *testing.T) {
	ctrl := gomock.NewController(t)
	mockCM := NewMockContainerManager(ctrl)
	orch, homeDir := setupOrchestrator(t, mockCM)
	writeADC(t, homeDir)

	cfg := &config.Config{GCP: config.GCPConfig{Enabled: true, Image: "gcp-mcp:latest"}}

	mockCM.EXPECT().EnsureSharedNetwork(gomock.Any(), "forge-shared").Return("net-id", nil)
	mockCM.EXPECT().IsContainerRunning(gomock.Any(), "forge-gcp-mcp").Return(false, nil)
	mockCM.EXPECT().RemoveContainer(gomock.Any(), "forge-gcp-mcp").Return(nil)
	mockCM.EXPECT().StartSharedService(gomock.Any(), gomock.Any()).Return("", assert.AnError)

	err := orch.startGCPMCP(context.Background(), cfg)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to start gcp-mcp")
}

func TestGcpGcloudDir(t *testing.T) {
	orch := &Orchestrator{HomeDir: "/home/tester"}

	// Default: ~/.config/gcloud under the host home.
	assert.Equal(t, filepath.Join("/home/tester", ".config", "gcloud"),
		orch.gcpGcloudDir(&config.Config{}))

	// Leading ~/ is expanded against the host home.
	assert.Equal(t, filepath.Join("/home/tester", "custom", "gcloud"),
		orch.gcpGcloudDir(&config.Config{GCP: config.GCPConfig{GcloudConfigDir: "~/custom/gcloud"}}))

	// Absolute path is used verbatim.
	assert.Equal(t, "/etc/gcloud",
		orch.gcpGcloudDir(&config.Config{GCP: config.GCPConfig{GcloudConfigDir: "/etc/gcloud"}}))
}

func TestStart_WithGCPEnabled_Success(t *testing.T) {
	ctrl := gomock.NewController(t)
	mockCM := NewMockContainerManager(ctrl)
	orch, homeDir := setupOrchestrator(t, mockCM)
	writeADC(t, homeDir)

	projectDir := setupGitProject(t)
	t.Setenv("ANTHROPIC_API_KEY", "sk-ant-test-key")

	configDir := filepath.Join(homeDir, ".config", "claude-forge")
	configContent := `images:
  agent: agent:latest
  gateway: gateway:latest
  github_mcp: mcp:latest
gcp:
  enabled: true
  image: gcp-mcp:latest
  project: my-proj
`
	require.NoError(t, os.WriteFile(filepath.Join(configDir, "config.yaml"), []byte(configContent), 0o644))

	mockCM.EXPECT().ImageExists(gomock.Any(), gomock.Any()).Return(true, nil).Times(3)
	mockCM.EXPECT().CreateNetwork(gomock.Any(), gomock.Any()).Return("net-id", nil)
	mockCM.EXPECT().StartGateway(gomock.Any(), gomock.Any()).Return("gw-id", nil)
	mockCM.EXPECT().WaitForReady(gomock.Any(), "gw-id", gomock.Any()).Return(nil)
	mockCM.EXPECT().StartGitHubMCP(gomock.Any(), gomock.Any()).Return("mcp-id", nil)
	mockCM.EXPECT().WaitForReady(gomock.Any(), "mcp-id", gomock.Any()).Return(nil)
	// GCP MCP starts as a shared singleton.
	mockCM.EXPECT().EnsureSharedNetwork(gomock.Any(), "forge-shared").Return("shared-net", nil)
	mockCM.EXPECT().IsContainerRunning(gomock.Any(), "forge-gcp-mcp").Return(false, nil)
	mockCM.EXPECT().RemoveContainer(gomock.Any(), "forge-gcp-mcp").Return(nil)
	mockCM.EXPECT().StartSharedService(gomock.Any(), gomock.Any()).Return("gcp-id", nil)
	mockCM.EXPECT().WaitForReady(gomock.Any(), "gcp-id", gomock.Any()).Return(nil)
	// The agent joins forge-shared because the GCP MCP is running there.
	mockCM.EXPECT().StartAgent(gomock.Any(), gomock.Any()).
		DoAndReturn(func(ctx context.Context, opts container.AgentOptions) (string, error) {
			require.Len(t, opts.ExtraNetworks, 1)
			assert.Equal(t, "forge-shared", opts.ExtraNetworks[0].NetworkName)
			return "agent-id", nil
		})

	sess, err := orch.Start(context.Background(), StartOptions{ProjectDir: projectDir})
	require.NoError(t, err)
	require.NotNil(t, sess)

	// The gcp server is advertised in settings only because it started.
	settings := readSettingsMCPServers(t, configDir)
	assert.Contains(t, settings, "gcp")
	assert.Contains(t, settings, "http://gcp-mcp:"+gcpmcp.MCPServerPort+"/mcp")
}

func TestStart_WithGCPEnabled_MissingADCGraceful(t *testing.T) {
	ctrl := gomock.NewController(t)
	mockCM := NewMockContainerManager(ctrl)
	orch, homeDir := setupOrchestrator(t, mockCM)
	// No ADC written: the preflight fails and the session continues without GCP.

	projectDir := setupGitProject(t)
	t.Setenv("ANTHROPIC_API_KEY", "sk-ant-test-key")

	configDir := filepath.Join(homeDir, ".config", "claude-forge")
	configContent := `gcp:
  enabled: true
  image: gcp-mcp:latest
`
	require.NoError(t, os.WriteFile(filepath.Join(configDir, "config.yaml"), []byte(configContent), 0o644))

	mockCM.EXPECT().ImageExists(gomock.Any(), gomock.Any()).Return(true, nil).Times(3)
	mockCM.EXPECT().CreateNetwork(gomock.Any(), gomock.Any()).Return("net-id", nil)
	mockCM.EXPECT().StartGateway(gomock.Any(), gomock.Any()).Return("gw-id", nil)
	mockCM.EXPECT().WaitForReady(gomock.Any(), "gw-id", gomock.Any()).Return(nil)
	mockCM.EXPECT().StartGitHubMCP(gomock.Any(), gomock.Any()).Return("mcp-id", nil)
	mockCM.EXPECT().WaitForReady(gomock.Any(), "mcp-id", gomock.Any()).Return(nil)
	// startGCPMCP fails at the ADC preflight before any container call.
	mockCM.EXPECT().StartAgent(gomock.Any(), gomock.Any()).
		DoAndReturn(func(ctx context.Context, opts container.AgentOptions) (string, error) {
			assert.Empty(t, opts.ExtraNetworks) // not joined to forge-shared
			return "agent-id", nil
		})

	sess, err := orch.Start(context.Background(), StartOptions{ProjectDir: projectDir})
	require.NoError(t, err)
	require.NotNil(t, sess)

	settings := readSettingsMCPServers(t, configDir)
	assert.NotContains(t, settings, "gcp-mcp")
}

// readSettingsMCPServers returns the raw settings.json content written by the
// orchestrator, for asserting which MCP servers were advertised.
func readSettingsMCPServers(t *testing.T, configDir string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(configDir, "settings.json"))
	require.NoError(t, err)
	return string(data)
}

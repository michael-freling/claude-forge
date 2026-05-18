package forge

//go:generate mockgen -destination=mock_container_manager_test.go -package=forge github.com/michael-freling/claude-code-tools/internal/forge/container ContainerManager

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/docker/docker/api/types/mount"
	"github.com/michael-freling/claude-code-tools/internal/forge/auth"
	"github.com/michael-freling/claude-code-tools/internal/forge/claudecode"
	"github.com/michael-freling/claude-code-tools/internal/forge/config"
	"github.com/michael-freling/claude-code-tools/internal/forge/container"
	"github.com/michael-freling/claude-code-tools/internal/forge/kube"
	"github.com/michael-freling/claude-code-tools/internal/forge/project"
	"github.com/michael-freling/claude-code-tools/internal/forge/session"
	"gopkg.in/yaml.v3"
)

// Orchestrator manages the lifecycle of claude-forge sessions.
type Orchestrator struct {
	Containers container.ContainerManager
	HomeDir    string
	ConfigDir  string
	ClaudeDir  string
	Log        func(format string, args ...any)
}

// NewOrchestrator creates an Orchestrator with default paths derived from homeDir.
func NewOrchestrator(containers container.ContainerManager, homeDir string) *Orchestrator {
	return &Orchestrator{
		Containers: containers,
		HomeDir:    homeDir,
		ConfigDir:  filepath.Join(homeDir, ".config", "claude-forge"),
		ClaudeDir:  filepath.Join(homeDir, ".claude"),
		Log:        func(format string, args ...any) { fmt.Printf(format+"\n", args...) },
	}
}

// StartOptions holds options for starting a session.
type StartOptions struct {
	SkipPermissions bool
	Worktree        bool
	Prompt          string
	ResumeID        string
	Continue        bool
	Interactive     bool     // allocate TTY for docker attach (false for prompt mode)
	ProjectDir      string   // working directory (defaults to cwd if empty)
	UID             int      // host user UID
	GID             int      // host user GID
	Mounts          []string // additional host:container bind mounts
}

// Session holds information about a running session.
type Session struct {
	AgentName     string
	GatewayName   string
	GitHubMCPName string
	NetworkName   string
	SessionID     string
	ProjectID     string
}

// Start creates a new claude-forge session: loads config, identifies the project,
// resolves credentials, creates containers, and returns the session info.
// The caller is responsible for attaching to the agent and calling Cleanup.
func (o *Orchestrator) Start(ctx context.Context, opts StartOptions) (*Session, error) {
	// Resolve project directory
	projectDir := opts.ProjectDir
	if projectDir == "" {
		var err error
		projectDir, err = os.Getwd()
		if err != nil {
			return nil, fmt.Errorf("failed to get working directory: %w", err)
		}
	}

	// Load config
	cfg, err := config.Load(o.ConfigDir)
	if err != nil {
		return nil, fmt.Errorf("failed to load config: %w", err)
	}

	// Identify project
	proj, err := project.Identify(projectDir)
	if err != nil {
		return nil, fmt.Errorf("failed to identify project: %w", err)
	}
	o.Log("Project: %s/%s (%s)", proj.Owner, proj.Repo, proj.Dir)

	// Resolve credentials
	creds, err := auth.Resolve(o.ClaudeDir)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve credentials: %w", err)
	}
	o.Log("Auth: %s", creds.AuthType)

	// Detect dependency cache directories
	cacheDirs := claudecode.DetectCacheDirs(o.HomeDir)
	if len(cacheDirs) > 0 {
		o.Log("Cache mounts: %d directories", len(cacheDirs))
	}

	// Generate session ID
	sessionID, err := session.GenerateID()
	if err != nil {
		return nil, err
	}

	// Construct names
	sess := &Session{
		NetworkName:   fmt.Sprintf("forge_net_%s_%s", proj.ID, sessionID),
		AgentName:     fmt.Sprintf("forge-agent-%s-%s", proj.ID, sessionID),
		GatewayName:   fmt.Sprintf("forge-gateway-%s-%s", proj.ID, sessionID),
		GitHubMCPName: fmt.Sprintf("forge-github-mcp-%s-%s", proj.ID, sessionID),
		SessionID:     sessionID,
		ProjectID:     proj.ID,
	}

	// Create session directory
	sessionDir := filepath.Join(o.HomeDir, ".claude-forge", proj.ID)
	if err := os.MkdirAll(sessionDir, 0o755); err != nil {
		return nil, fmt.Errorf("failed to create session directory: %w", err)
	}

	// Create plugins directory (persists across sessions, managed from inside the container)
	pluginsDir := filepath.Join(o.HomeDir, ".claude-forge", "plugins")
	if err := os.MkdirAll(pluginsDir, 0o755); err != nil {
		return nil, fmt.Errorf("failed to create plugins directory: %w", err)
	}

	// Write/update gitconfig
	gitUserName := project.GitConfig("user.name")
	gitUserEmail := project.GitConfig("user.email")
	ccOpts := claudecode.Options{
		GitUserName:  gitUserName,
		GitUserEmail: gitUserEmail,
	}
	if err := claudecode.WriteGitconfig(o.ConfigDir, ccOpts); err != nil {
		return nil, fmt.Errorf("failed to write gitconfig: %w", err)
	}

	// Ensure settings.json exists
	if err := claudecode.EnsureSettings(o.ConfigDir); err != nil {
		return nil, fmt.Errorf("failed to ensure settings.json: %w", err)
	}

	// Ensure .claude.json exists (skips onboarding in container)
	if err := claudecode.EnsureUserConfig(o.ConfigDir, o.HomeDir); err != nil {
		return nil, fmt.Errorf("failed to ensure .claude.json: %w", err)
	}

	// Pull images if not present
	imagesToPull := []string{cfg.Images.Agent, cfg.Images.Gateway, cfg.Images.GitHubMCP}
	if cfg.Kubernetes.Enabled && len(cfg.Kubernetes.Contexts) > 0 {
		imagesToPull = append(imagesToPull, cfg.Kubernetes.Image)
	}
	for _, img := range imagesToPull {
		exists, err := o.Containers.ImageExists(ctx, img)
		if err != nil {
			return nil, fmt.Errorf("failed to check image %s: %w", img, err)
		}
		if !exists {
			o.Log("Pulling image: %s", img)
			if err := o.Containers.PullImage(ctx, img); err != nil {
				return nil, fmt.Errorf("failed to pull image %s: %w", img, err)
			}
		}
	}

	// Create Docker network
	o.Log("Creating network: %s", sess.NetworkName)
	if _, err := o.Containers.CreateNetwork(ctx, sess.NetworkName); err != nil {
		return nil, fmt.Errorf("failed to create network: %w", err)
	}

	// Start gateway
	o.Log("Starting gateway: %s", sess.GatewayName)
	sshDir := filepath.Join(o.HomeDir, ".ssh")
	ghConfigDir := filepath.Join(o.HomeDir, ".config", "gh")
	gatewayEnv := map[string]string{}
	if ghToken := os.Getenv("GITHUB_TOKEN"); ghToken != "" {
		gatewayEnv["GITHUB_TOKEN"] = ghToken
	} else if token := readGHToken(ghConfigDir); token != "" {
		gatewayEnv["GITHUB_TOKEN"] = token
	}
	gatewayID, err := o.Containers.StartGateway(ctx, container.GatewayOptions{
		Name:        sess.GatewayName,
		Image:       cfg.Images.Gateway,
		NetworkName: sess.NetworkName,
		SSHDir:      sshDir,
		GHConfigDir: ghConfigDir,
		Owner:       proj.Owner,
		Repo:        proj.Repo,
		Env:         gatewayEnv,
	})
	if err != nil {
		o.Cleanup(ctx, sess)
		return nil, fmt.Errorf("failed to start gateway: %w", err)
	}

	if err := o.Containers.WaitForReady(ctx, gatewayID, 5*time.Second); err != nil {
		logs, _ := o.Containers.ContainerLogs(ctx, gatewayID)
		o.Cleanup(ctx, sess)
		if logs != "" {
			return nil, fmt.Errorf("gateway container failed to start: %w\nGateway logs:\n%s", err, logs)
		}
		return nil, fmt.Errorf("gateway container failed to start: %w", err)
	}

	// Start GitHub MCP sidecar
	o.Log("Starting GitHub MCP: %s", sess.GitHubMCPName)
	mcpID, err := o.Containers.StartGitHubMCP(ctx, container.GitHubMCPOptions{
		Name:        sess.GitHubMCPName,
		Image:       cfg.Images.GitHubMCP,
		NetworkName: sess.NetworkName,
		Owner:       proj.Owner,
		Repo:        proj.Repo,
		Env:         gatewayEnv,
	})
	if err != nil {
		o.Cleanup(ctx, sess)
		return nil, fmt.Errorf("failed to start github-mcp: %w", err)
	}

	if err := o.Containers.WaitForReady(ctx, mcpID, 5*time.Second); err != nil {
		logs, _ := o.Containers.ContainerLogs(ctx, mcpID)
		o.Cleanup(ctx, sess)
		if logs != "" {
			return nil, fmt.Errorf("github-mcp failed to start: %w\nLogs:\n%s", err, logs)
		}
		return nil, fmt.Errorf("github-mcp failed to start: %w", err)
	}

	// Write MCP server config to settings.json for the agent
	mcpServers := map[string]claudecode.MCPServerConfig{
		"github": {Type: "url", URL: "http://github-mcp:8083/mcp"},
	}

	// Add Kubernetes MCP if enabled
	if cfg.Kubernetes.Enabled && len(cfg.Kubernetes.Contexts) > 0 {
		mcpServers["kubernetes"] = claudecode.MCPServerConfig{Type: "url", URL: "http://k8s-mcp:8090/mcp"}
	}

	if err := claudecode.UpdateMCPServers(o.ConfigDir, mcpServers); err != nil {
		o.Cleanup(ctx, sess)
		return nil, fmt.Errorf("failed to update MCP settings: %w", err)
	}

	// Build agent command args
	var agentCmd []string
	if opts.SkipPermissions {
		agentCmd = append(agentCmd, "--dangerously-skip-permissions")
	}
	if opts.Worktree {
		agentCmd = append(agentCmd, "--worktree")
	}
	if opts.ResumeID != "" {
		agentCmd = append(agentCmd, "--resume", opts.ResumeID)
	} else if opts.Continue {
		agentCmd = append(agentCmd, "--continue")
	}
	if opts.Prompt != "" {
		agentCmd = append(agentCmd, "-p", opts.Prompt)
	}

	// Build environment variables
	agentEnv := map[string]string{
		"HOME":                "/home/user",
		"GIT_TERMINAL_PROMPT": "0",
		"FORGE_PROJECT_OWNER": proj.Owner,
		"FORGE_PROJECT_REPO":  proj.Repo,
	}
	switch creds.AuthType {
	case "api_key":
		agentEnv["ANTHROPIC_API_KEY"] = creds.Token
	case "oauth":
		credentialsPath := filepath.Join(o.ClaudeDir, ".credentials.json")
		if _, err := os.Stat(credentialsPath); err != nil {
			agentEnv["CLAUDE_CODE_OAUTH_TOKEN"] = creds.Token
		}
	}
	if opts.UID > 0 {
		agentEnv["FORGE_UID"] = fmt.Sprintf("%d", opts.UID)
	}
	if opts.GID > 0 {
		agentEnv["FORGE_GID"] = fmt.Sprintf("%d", opts.GID)
	}

	// Read host model preference and propagate to container
	if model := claudecode.ReadHostModel(o.ClaudeDir); model != "" {
		agentEnv["ANTHROPIC_MODEL"] = model
	}

	// Convert cache dirs
	var containerCacheDirs []container.CacheDir
	for _, cd := range cacheDirs {
		containerCacheDirs = append(containerCacheDirs, container.CacheDir{
			Source: cd.Source,
			Target: cd.Target,
		})
	}

	// Parse extra mounts (host:container format)
	var extraMounts []container.CacheDir
	for _, m := range opts.Mounts {
		parts := strings.SplitN(m, ":", 2)
		if len(parts) != 2 {
			return nil, fmt.Errorf("invalid mount format %q: expected host_path:container_path", m)
		}
		source, err := filepath.Abs(parts[0])
		if err != nil {
			return nil, fmt.Errorf("invalid mount source path %q: %w", parts[0], err)
		}
		if _, err := os.Stat(source); err != nil {
			return nil, fmt.Errorf("mount source path does not exist: %s", source)
		}
		extraMounts = append(extraMounts, container.CacheDir{
			Source: source,
			Target: parts[1],
		})
	}

	// Start Kubernetes MCP shared service if enabled
	if cfg.Kubernetes.Enabled && len(cfg.Kubernetes.Contexts) > 0 {
		if err := o.startKubernetesMCP(ctx, cfg); err != nil {
			o.Log("Warning: failed to start Kubernetes MCP: %v", err)
		}
	}

	// Start agent
	o.Log("Starting agent: %s", sess.AgentName)
	if _, err := o.Containers.StartAgent(ctx, container.AgentOptions{
		Name:        sess.AgentName,
		Image:       cfg.Images.Agent,
		NetworkName: sess.NetworkName,
		ProjectDir:  proj.Dir,
		SessionDir:  sessionDir,
		ClaudeDir:   o.ClaudeDir,
		ConfigDir:   o.ConfigDir,
		HomeDir:     o.HomeDir,
		PluginsDir:  pluginsDir,
		Env:         agentEnv,
		Interactive: opts.Interactive,
		Cmd:         agentCmd,
		UID:         opts.UID,
		GID:         opts.GID,
		CacheDirs:   containerCacheDirs,
		ExtraMounts: extraMounts,
	}); err != nil {
		o.Cleanup(ctx, sess)
		return nil, fmt.Errorf("failed to start agent: %w", err)
	}

	// Connect agent to shared network for Kubernetes MCP access
	if cfg.Kubernetes.Enabled && len(cfg.Kubernetes.Contexts) > 0 {
		if err := o.Containers.ConnectNetwork(ctx, "forge-shared", sess.AgentName, nil); err != nil {
			o.Log("Warning: failed to connect agent to shared network: %v", err)
		}
	}

	return sess, nil
}

// Cleanup stops and removes all containers and network for a session.
func (o *Orchestrator) Cleanup(ctx context.Context, sess *Session) {
	o.Log("Cleaning up...")
	_ = o.Containers.StopContainer(ctx, sess.AgentName)
	_ = o.Containers.RemoveContainer(ctx, sess.AgentName)
	_ = o.Containers.StopContainer(ctx, sess.GitHubMCPName)
	_ = o.Containers.RemoveContainer(ctx, sess.GitHubMCPName)
	_ = o.Containers.StopContainer(ctx, sess.GatewayName)
	_ = o.Containers.RemoveContainer(ctx, sess.GatewayName)
	_ = o.Containers.RemoveNetwork(ctx, sess.NetworkName)
	o.Log("Cleanup complete.")
}

// Stop stops all running containers for the project in the given directory.
func (o *Orchestrator) Stop(ctx context.Context, projectDir string) error {
	proj, err := project.Identify(projectDir)
	if err != nil {
		return fmt.Errorf("failed to identify project: %w", err)
	}

	containers, err := o.Containers.ListForgeContainers(ctx)
	if err != nil {
		return fmt.Errorf("failed to list containers: %w", err)
	}

	var matched []container.ContainerInfo
	for _, c := range containers {
		if strings.Contains(c.Name, proj.ID) {
			matched = append(matched, c)
		}
	}

	if len(matched) == 0 {
		o.Log("No running containers found for this project.")
		return nil
	}

	for _, c := range matched {
		o.Log("Stopping: %s", c.Name)
		_ = o.Containers.StopContainer(ctx, c.Name)
		_ = o.Containers.RemoveContainer(ctx, c.Name)
	}

	// Extract session IDs and remove networks.
	// Container names follow the pattern: forge-<type>-<project-id>-<session-id>
	// where session-id is the last 8 hex characters.
	sessionIDs := make(map[string]bool)
	prefixes := []string{"forge-agent-", "forge-gateway-", "forge-github-mcp-"}
	for _, c := range matched {
		name := c.Name
		for _, prefix := range prefixes {
			name = strings.TrimPrefix(name, prefix)
		}
		if len(name) >= 8 {
			sid := name[len(name)-8:]
			sessionIDs[sid] = true
		}
	}

	for sid := range sessionIDs {
		netName := fmt.Sprintf("forge_net_%s_%s", proj.ID, sid)
		o.Log("Removing network: %s", netName)
		_ = o.Containers.RemoveNetwork(ctx, netName)
	}

	o.Log("Stopped.")
	return nil
}

// StatusEntry holds info about a running forge container.
type StatusEntry = container.ContainerInfo

// Status returns all running forge containers.
func (o *Orchestrator) Status(ctx context.Context) ([]StatusEntry, error) {
	return o.Containers.ListForgeContainers(ctx)
}

// readGHToken reads the GitHub OAuth token from gh CLI's hosts.yml file.
// Returns empty string if the file doesn't exist or can't be read.
func readGHToken(ghConfigDir string) string {
	hostsPath := filepath.Join(ghConfigDir, "hosts.yml")
	data, err := os.ReadFile(hostsPath)
	if err != nil {
		return ""
	}

	var hosts map[string]struct {
		OAuthToken string `yaml:"oauth_token"`
	}
	if err := yaml.Unmarshal(data, &hosts); err != nil {
		return ""
	}

	if gh, ok := hosts["github.com"]; ok {
		return gh.OAuthToken
	}
	return ""
}

// startKubernetesMCP ensures the shared Kubernetes MCP service is running.
func (o *Orchestrator) startKubernetesMCP(ctx context.Context, cfg *config.Config) error {
	// Ensure shared network exists
	if _, err := o.Containers.EnsureSharedNetwork(ctx, "forge-shared"); err != nil {
		return fmt.Errorf("failed to ensure shared network: %w", err)
	}

	k8sMCPName := "forge-k8s-mcp"
	running, err := o.Containers.IsContainerRunning(ctx, k8sMCPName)
	if err != nil {
		return fmt.Errorf("failed to check k8s-mcp status: %w", err)
	}
	if running {
		o.Log("Kubernetes MCP already running")
		return nil
	}

	// Generate kubeconfig with SA tokens
	kubeconfigDir := filepath.Join(o.ConfigDir, "k8s-mcp")
	if err := os.MkdirAll(kubeconfigDir, 0o700); err != nil {
		return fmt.Errorf("failed to create k8s-mcp config dir: %w", err)
	}
	kubeconfigOutput := filepath.Join(kubeconfigDir, "kubeconfig")

	homeKubeconfig := filepath.Join(o.HomeDir, ".kube", "config")
	if kc := os.Getenv("KUBECONFIG"); kc != "" {
		homeKubeconfig = kc
	}

	var contexts []kube.ContextConfig
	for _, c := range cfg.Kubernetes.Contexts {
		contexts = append(contexts, kube.ContextConfig{
			HostContext:             c.HostContext,
			ServiceAccountName:      c.ServiceAccountName,
			ServiceAccountNamespace: c.ServiceAccountNamespace,
		})
	}

	defaultCtx := cfg.Kubernetes.DefaultContext
	if defaultCtx == "" && len(cfg.Kubernetes.Contexts) > 0 {
		defaultCtx = cfg.Kubernetes.Contexts[0].HostContext
	}

	if err := kube.GenerateKubeconfig(contexts, homeKubeconfig, defaultCtx, kubeconfigOutput); err != nil {
		return fmt.Errorf("failed to generate kubeconfig: %w", err)
	}

	// Build command for the MCP server
	cmd := []string{"--transport", "http", "--port", "8090"}
	if cfg.Kubernetes.ReadOnly {
		cmd = append(cmd, "--read-only")
	}

	o.Log("Starting Kubernetes MCP: %s", k8sMCPName)
	k8sID, err := o.Containers.StartSharedService(ctx, container.SharedServiceOptions{
		Name:        k8sMCPName,
		Image:       cfg.Kubernetes.Image,
		NetworkName: "forge-shared",
		Alias:       "k8s-mcp",
		Cmd:         cmd,
		Mounts: []mount.Mount{
			{
				Type:     mount.TypeBind,
				Source:   kubeconfigOutput,
				Target:   "/home/user/.kube/config",
				ReadOnly: true,
			},
		},
	})
	if err != nil {
		return fmt.Errorf("failed to start k8s-mcp: %w", err)
	}

	if err := o.Containers.WaitForReady(ctx, k8sID, 10*time.Second); err != nil {
		return fmt.Errorf("k8s-mcp failed to start: %w", err)
	}

	return nil
}

// Build pulls the latest agent and gateway images.
func (o *Orchestrator) Build(ctx context.Context) error {
	cfg, err := config.Load(o.ConfigDir)
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	images := []string{cfg.Images.Agent, cfg.Images.Gateway, cfg.Images.GitHubMCP}
	if cfg.Kubernetes.Enabled {
		images = append(images, cfg.Kubernetes.Image)
	}
	for _, img := range images {
		o.Log("Pulling image: %s", img)
		if err := o.Containers.PullImage(ctx, img); err != nil {
			return fmt.Errorf("failed to pull image %s: %w", img, err)
		}
	}
	o.Log("All images up to date.")
	return nil
}

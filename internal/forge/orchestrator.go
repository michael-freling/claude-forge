package forge

//go:generate mockgen -destination=mock_container_manager_test.go -package=forge github.com/michael-freling/claude-forge/internal/forge/container ContainerManager

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/docker/docker/api/types/mount"
	"github.com/michael-freling/claude-forge/internal/forge/auth"
	"github.com/michael-freling/claude-forge/internal/forge/claudecode"
	"github.com/michael-freling/claude-forge/internal/forge/config"
	"github.com/michael-freling/claude-forge/internal/forge/container"
	"github.com/michael-freling/claude-forge/internal/forge/kube"
	"github.com/michael-freling/claude-forge/internal/forge/project"
	"github.com/michael-freling/claude-forge/internal/forge/session"
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
	SkipPermissions    bool
	Worktree           bool
	Prompt             string
	ResumeID           string
	ResumeSubdir       string // session subdir (e.g., "-work") for constructing the resume file path
	Continue           bool
	Interactive        bool     // allocate TTY for docker attach (false for prompt mode)
	ProjectDir         string   // working directory (defaults to cwd if empty)
	UID                int      // host user UID
	GID                int      // host user GID
	Mounts             []string // additional host:container bind mounts
	ResumeWorktreeName string   // worktree name when resuming a worktree session
	Name               string   // human-readable session name (passed to Claude Code via --name)
}

// Session holds information about a running session.
type Session struct {
	AgentName     string
	GatewayName   string
	GitHubMCPName string
	NetworkName   string
	SessionID     string
	ProjectID     string
	SidecarNames  []string // custom MCP sidecar container names, for cleanup
}

// Start creates a new claude-forge session: loads config, identifies the project,
// resolves credentials, creates containers, and returns the session info.
// The caller is responsible for attaching to the agent and calling Cleanup.
func (o *Orchestrator) Start(ctx context.Context, opts StartOptions) (*Session, error) {
	// Fail fast on an invalid session name before doing any container work.
	if opts.Name != "" {
		if err := session.ValidateName(opts.Name); err != nil {
			return nil, err
		}
	}

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

	if cfg.Kubernetes.Enabled && len(cfg.Kubernetes.Contexts) == 0 {
		return nil, fmt.Errorf("kubernetes is enabled but no contexts are configured; run 'claude-forge init' and configure kubernetes.contexts in %s/config.yaml", o.ConfigDir)
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
	if cfg.Kubernetes.Enabled {
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

	// Start Kubernetes MCP shared service if enabled (before writing settings
	// so we only advertise it when the server is actually running)
	k8sRunning := false
	if cfg.Kubernetes.Enabled {
		if err := o.startKubernetesMCP(ctx, cfg); err != nil {
			o.Log("Warning: failed to start Kubernetes MCP: %v", err)
		} else {
			k8sRunning = true
		}
	}

	// Start custom "container" MCP servers (per-session or global) before
	// writing settings, so we only advertise the ones that actually came up.
	sidecarRegs, usedSharedMCP := o.startCustomSidecars(ctx, cfg, sess)

	// Write MCP server config to settings.json for the agent
	mcpServers := map[string]claudecode.MCPServerConfig{
		"github": {Type: "url", URL: "http://github-mcp:8083/mcp"},
	}
	if k8sRunning {
		mcpServers["kubernetes"] = claudecode.MCPServerConfig{Type: "url", URL: "http://k8s-mcp:" + kube.MCPServerPort + "/mcp"}
	}

	// Merge user-configured custom MCP servers (e.g. a hosted Vercel MCP).
	// ${VAR} references in URLs, headers, commands, args, and env are expanded
	// against the host environment now so secrets stay out of config.yaml.
	// Container servers are handled as sidecars above.
	for _, s := range cfg.MCPServers {
		if s.IsContainer() {
			continue
		}
		mcpServers[s.Name] = customMCPServer(s)
		o.Log("Custom MCP: %s", s.Name)
	}
	for name, reg := range sidecarRegs {
		mcpServers[name] = reg
	}

	if err := claudecode.UpdateMCPServers(o.ConfigDir, mcpServers); err != nil {
		o.Cleanup(ctx, sess)
		return nil, fmt.Errorf("failed to update MCP settings: %w", err)
	}

	if err := claudecode.RegisterProjectMCPServers(o.ConfigDir, mcpServers); err != nil {
		o.Cleanup(ctx, sess)
		return nil, fmt.Errorf("failed to register MCP servers in .claude.json: %w", err)
	}

	// Preflight OAuth-based MCP servers: the interactive OAuth flow cannot run
	// inside the headless container, so warn (non-fatally) when a required token
	// is missing or expired rather than letting the server silently fail to
	// connect mid-session. Tokens are looked up in the same credentials file
	// that is mounted into the container.
	o.warnUnauthenticatedMCP(cfg)

	// For fresh sessions, pin the Claude session ID so the resulting JSONL and
	// the sidecar metadata file share a known identifier.
	fresh := opts.ResumeID == "" && !opts.Continue
	var claudeSessionID string
	if fresh {
		claudeSessionID, err = session.GenerateUUID()
		if err != nil {
			return nil, err
		}
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
		if opts.ResumeSubdir != "" {
			resumePath := fmt.Sprintf("/home/user/.claude/projects/%s/%s.jsonl", opts.ResumeSubdir, opts.ResumeID)
			agentCmd = append(agentCmd, "--resume", resumePath)
		} else {
			agentCmd = append(agentCmd, "--resume", opts.ResumeID)
		}
	} else if opts.Continue {
		agentCmd = append(agentCmd, "--continue")
	} else {
		// --session-id is a Claude Code CLI flag; re-verify it still exists
		// against `claude --help` when bumping the agent image.
		agentCmd = append(agentCmd, "--session-id", claudeSessionID)
	}
	if opts.Name != "" {
		// --name is a Claude Code CLI flag (passed on fresh and resume/continue
		// paths); re-verify it still exists against `claude --help` when bumping
		// the agent image.
		agentCmd = append(agentCmd, "--name", opts.Name)
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
		// The image pins the Claude Code version and the npm prefix is not
		// writable by the container user; without this the auto-updater runs
		// anyway (the legacy autoUpdaterStatus settings key is ignored by
		// newer versions) and reports "no write permission to npm prefix".
		"DISABLE_AUTOUPDATER": "1",
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

	// Build extra networks for the agent (connect before start so DNS is ready).
	// The agent joins forge-shared when any shared service is running there:
	// the Kubernetes MCP and/or global custom MCP sidecars.
	var extraNetworks []container.NetworkAttachment
	if k8sRunning || usedSharedMCP {
		extraNetworks = append(extraNetworks, container.NetworkAttachment{
			NetworkName: "forge-shared",
		})
	}

	// Start agent
	o.Log("Starting agent: %s", sess.AgentName)
	if _, err := o.Containers.StartAgent(ctx, container.AgentOptions{
		Name:               sess.AgentName,
		Image:              cfg.Images.Agent,
		NetworkName:        sess.NetworkName,
		ProjectDir:         proj.Dir,
		SessionDir:         sessionDir,
		ClaudeDir:          o.ClaudeDir,
		ConfigDir:          o.ConfigDir,
		HomeDir:            o.HomeDir,
		PluginsDir:         pluginsDir,
		Env:                agentEnv,
		Interactive:        opts.Interactive,
		Cmd:                agentCmd,
		UID:                opts.UID,
		GID:                opts.GID,
		CacheDirs:          containerCacheDirs,
		ExtraMounts:        extraMounts,
		ResumeWorktreeName: opts.ResumeWorktreeName,
		ExtraNetworks:      extraNetworks,
	}); err != nil {
		o.Cleanup(ctx, sess)
		return nil, fmt.Errorf("failed to start agent: %w", err)
	}

	// Persist the session name in a sidecar file so `resume --list` and
	// `resume` can surface and reuse it later. Written only after the agent
	// starts so a failed start leaves no orphaned sidecar. A write failure is
	// non-fatal: the session runs, the name just isn't recorded.
	if fresh && opts.Name != "" {
		if err := session.WriteMetadata(sessionDir, claudeSessionID, session.Metadata{Name: opts.Name}); err != nil {
			o.Log("Warning: failed to write session metadata: %v", err)
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
	for _, name := range sess.SidecarNames {
		_ = o.Containers.StopContainer(ctx, name)
		_ = o.Containers.RemoveContainer(ctx, name)
	}
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

// customMCPServer converts a user-configured MCP server into the settings
// shape written for Claude Code, expanding ${VAR} references in every string
// field against the host environment.
func customMCPServer(s config.MCPServerConfig) claudecode.MCPServerConfig {
	out := claudecode.MCPServerConfig{Type: s.Type}
	if s.IsStdio() {
		out.Type = "stdio"
		out.Command = os.ExpandEnv(s.Command)
		for _, a := range s.Args {
			out.Args = append(out.Args, os.ExpandEnv(a))
		}
		out.Env = expandEnvMap(s.Env)
		return out
	}

	if out.Type == "" {
		out.Type = "http"
	}
	out.URL = os.ExpandEnv(s.URL)
	out.Headers = expandEnvMap(s.Headers)
	return out
}

// expandEnvMap returns a copy of m with ${VAR} references in each value
// expanded against the host environment. Returns nil for an empty map so the
// omitempty JSON tags drop the field entirely.
func expandEnvMap(m map[string]string) map[string]string {
	if len(m) == 0 {
		return nil
	}
	out := make(map[string]string, len(m))
	for k, v := range m {
		out[k] = os.ExpandEnv(v)
	}
	return out
}

// startCustomSidecars starts each type:"container" MCP server as a per-session
// sidecar on the session network and returns the HTTP registrations for the
// ones that came up. Failures are logged and skipped so a single bad server
// doesn't abort the session (matching the Kubernetes MCP behavior).
func (o *Orchestrator) startCustomSidecars(ctx context.Context, cfg *config.Config, sess *Session) (map[string]claudecode.MCPServerConfig, bool) {
	regs := map[string]claudecode.MCPServerConfig{}
	usedShared := false
	for _, s := range cfg.MCPServers {
		if !s.IsContainer() {
			continue
		}
		var reg claudecode.MCPServerConfig
		var ok bool
		if s.IsGlobal() {
			reg, ok = o.ensureGlobalSidecar(ctx, s)
			if ok {
				usedShared = true
			}
		} else {
			reg, ok = o.startSessionSidecar(ctx, s, sess)
		}
		if ok {
			regs[s.Name] = reg
		}
	}
	return regs, usedShared
}

// sidecarRuntime resolves the image, container command, port, path, and mounts
// for a container MCP server, applying defaults and ${VAR} expansion.
func (o *Orchestrator) sidecarRuntime(s config.MCPServerConfig) (image string, cmd []string, port int, path string, mounts []mount.Mount, err error) {
	port = s.Port
	if port == 0 {
		port = config.DefaultMCPPort
	}
	path = s.Path
	if path == "" {
		path = config.DefaultMCPPath
	}
	image = s.Image
	if s.IsWrappedStdio() {
		if image == "" {
			image = config.DefaultMCPWrapperImage
		}
		cmd = supergatewayCmd(s, port, path)
	} else {
		// Native image that serves MCP over HTTP itself; pass through any args.
		for _, a := range s.Args {
			cmd = append(cmd, os.ExpandEnv(a))
		}
	}
	mounts, err = o.parseSidecarMounts(s.Mounts)
	return
}

// ensureImage pulls image if it isn't present locally. Returns false (with a
// warning) only when a needed pull fails.
func (o *Orchestrator) ensureImage(ctx context.Context, name, image string) bool {
	if exists, err := o.Containers.ImageExists(ctx, image); err == nil && !exists {
		o.Log("Pulling image: %s", image)
		if err := o.Containers.PullImage(ctx, image); err != nil {
			o.Log("Warning: MCP sidecar %q: failed to pull image %s: %v; skipping", name, image, err)
			return false
		}
	}
	return true
}

// sidecarReg builds the HTTP registration for a sidecar reached at its alias.
func sidecarReg(name string, port int, path string) claudecode.MCPServerConfig {
	return claudecode.MCPServerConfig{
		Type: "http",
		URL:  fmt.Sprintf("http://%s:%d%s", name, port, path),
	}
}

// startSessionSidecar starts a per-session container MCP server on the session
// network and records it for cleanup. Returns the registration and whether it
// came up.
func (o *Orchestrator) startSessionSidecar(ctx context.Context, s config.MCPServerConfig, sess *Session) (claudecode.MCPServerConfig, bool) {
	image, cmd, port, path, mounts, err := o.sidecarRuntime(s)
	if err != nil {
		o.Log("Warning: MCP sidecar %q: %v; skipping", s.Name, err)
		return claudecode.MCPServerConfig{}, false
	}
	if !o.ensureImage(ctx, s.Name, image) {
		return claudecode.MCPServerConfig{}, false
	}

	containerName := fmt.Sprintf("forge-mcp-%s-%s-%s", s.Name, sess.ProjectID, sess.SessionID)
	o.Log("Starting MCP sidecar: %s", s.Name)
	id, err := o.Containers.StartSharedService(ctx, container.SharedServiceOptions{
		Name:        containerName,
		Image:       image,
		NetworkName: sess.NetworkName,
		Alias:       s.Name,
		Env:         expandEnvMap(s.Env),
		Mounts:      mounts,
		Cmd:         cmd,
	})
	if err != nil {
		o.Log("Warning: MCP sidecar %q failed to start: %v; skipping", s.Name, err)
		return claudecode.MCPServerConfig{}, false
	}
	// Record for cleanup as soon as it exists, even if readiness fails.
	sess.SidecarNames = append(sess.SidecarNames, containerName)

	if err := o.Containers.WaitForReady(ctx, id, 60*time.Second); err != nil {
		logs, _ := o.Containers.ContainerLogs(ctx, id)
		o.Log("Warning: MCP sidecar %q not ready: %v; skipping\n%s", s.Name, err, logs)
		return claudecode.MCPServerConfig{}, false
	}
	return sidecarReg(s.Name, port, path), true
}

// globalSidecarName is the stable container name for a global (shared) MCP
// server, independent of any session.
func globalSidecarName(serverName string) string {
	return "forge-mcp-global-" + serverName
}

// ensureGlobalSidecar starts a global container MCP server as a shared singleton
// on the forge-shared network, reusing it if already running. Global servers are
// not tracked for per-session cleanup; they persist across sessions and are
// managed via RestartSharedMCP.
func (o *Orchestrator) ensureGlobalSidecar(ctx context.Context, s config.MCPServerConfig) (claudecode.MCPServerConfig, bool) {
	image, cmd, port, path, mounts, err := o.sidecarRuntime(s)
	if err != nil {
		o.Log("Warning: MCP sidecar %q: %v; skipping", s.Name, err)
		return claudecode.MCPServerConfig{}, false
	}

	if _, err := o.Containers.EnsureSharedNetwork(ctx, "forge-shared"); err != nil {
		o.Log("Warning: MCP sidecar %q: failed to ensure shared network: %v; skipping", s.Name, err)
		return claudecode.MCPServerConfig{}, false
	}

	name := globalSidecarName(s.Name)
	if running, err := o.Containers.IsContainerRunning(ctx, name); err == nil && running {
		o.Log("Global MCP already running: %s", s.Name)
		return sidecarReg(s.Name, port, path), true
	}
	// Remove a stale (crashed/exited) instance so we can recreate it.
	_ = o.Containers.RemoveContainer(ctx, name)

	if !o.ensureImage(ctx, s.Name, image) {
		return claudecode.MCPServerConfig{}, false
	}

	o.Log("Starting global MCP: %s", s.Name)
	id, err := o.Containers.StartSharedService(ctx, container.SharedServiceOptions{
		Name:        name,
		Image:       image,
		NetworkName: "forge-shared",
		Alias:       s.Name,
		Env:         expandEnvMap(s.Env),
		Mounts:      mounts,
		Cmd:         cmd,
	})
	if err != nil {
		o.Log("Warning: global MCP %q failed to start: %v; skipping", s.Name, err)
		return claudecode.MCPServerConfig{}, false
	}
	if err := o.Containers.WaitForReady(ctx, id, 60*time.Second); err != nil {
		logs, _ := o.Containers.ContainerLogs(ctx, id)
		o.Log("Warning: global MCP %q not ready: %v; skipping\n%s", s.Name, err, logs)
		return claudecode.MCPServerConfig{}, false
	}
	return sidecarReg(s.Name, port, path), true
}

// supergatewayCmd builds the container command that wraps a stdio MCP server in
// supergateway's stdio→streamable-HTTP bridge. The child command inherits the
// sidecar's environment, so secrets are passed via Env rather than a flag.
func supergatewayCmd(s config.MCPServerConfig, port int, path string) []string {
	child := os.ExpandEnv(s.Command)
	for _, a := range s.Args {
		child += " " + os.ExpandEnv(a)
	}
	return []string{
		"npx", "-y", "supergateway",
		"--stdio", child,
		"--outputTransport", "streamableHttp",
		"--port", strconv.Itoa(port),
		"--streamableHttpPath", path,
		"--healthEndpoint", "/healthz",
	}
}

// parseSidecarMounts converts "host:container[:ro]" specs into bind mounts,
// expanding ${VAR} and a leading ~/ against the host environment.
func (o *Orchestrator) parseSidecarMounts(specs []string) ([]mount.Mount, error) {
	var out []mount.Mount
	for _, spec := range specs {
		expanded := os.ExpandEnv(spec)
		parts := strings.Split(expanded, ":")
		if len(parts) < 2 || len(parts) > 3 {
			return nil, fmt.Errorf("invalid mount %q: expected host:container[:ro]", spec)
		}
		src := parts[0]
		if strings.HasPrefix(src, "~/") {
			src = filepath.Join(o.HomeDir, src[2:])
		}
		src, err := filepath.Abs(src)
		if err != nil {
			return nil, fmt.Errorf("invalid mount source %q: %w", parts[0], err)
		}
		readOnly := len(parts) == 3 && parts[2] == "ro"
		out = append(out, mount.Mount{
			Type:     mount.TypeBind,
			Source:   src,
			Target:   parts[1],
			ReadOnly: readOnly,
		})
	}
	return out, nil
}

// warnUnauthenticatedMCP checks each OAuth-based custom MCP server for a valid
// stored token and logs a warning for any that is missing or expired. It never
// fails the session — an unauthenticated server simply won't connect until the
// user authenticates on the host.
func (o *Orchestrator) warnUnauthenticatedMCP(cfg *config.Config) {
	nowMs := time.Now().UnixMilli()
	for _, s := range cfg.MCPServers {
		if !s.OAuth {
			continue
		}
		typ := s.Type
		if typ == "" {
			typ = "http"
		}
		url := os.ExpandEnv(s.URL)
		// OAuth servers carry no static headers, so the lookup uses an empty set.
		status, err := claudecode.CheckMCPOAuth(o.ClaudeDir, s.Name, typ, url, nil, nowMs)
		if err != nil {
			o.Log("Warning: could not check OAuth token for MCP %q: %v", s.Name, err)
			continue
		}
		switch status {
		case claudecode.MCPOAuthMissing:
			o.Log("Warning: MCP server %q needs OAuth but no token was found; it will be unavailable this session.", s.Name)
			o.Log("         Authenticate once on the host, then restart the session:")
			o.Log("           claude mcp add --transport %s %s %s", typ, s.Name, url)
			o.Log("           claude   # then run: /mcp -> %s -> Authenticate", s.Name)
		case claudecode.MCPOAuthExpired:
			o.Log("Warning: MCP server %q OAuth token is expired and not refreshable; re-authenticate on the host via 'claude' -> /mcp.", s.Name)
		}
	}
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

	// Remove stale container if it exists but isn't running (e.g. crashed/exited)
	// so we can create a fresh one with the same name.
	_ = o.Containers.RemoveContainer(ctx, k8sMCPName)

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

	cmd := kube.MCPServerArgs()

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
				Target:   kube.MCPServerKubeconfigPath,
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

// RestartSharedMCP stops all shared MCP containers and starts them again.
func (o *Orchestrator) RestartSharedMCP(ctx context.Context) error {
	cfg, err := config.Load(o.ConfigDir)
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	var globals []config.MCPServerConfig
	for _, s := range cfg.MCPServers {
		if s.IsGlobal() {
			globals = append(globals, s)
		}
	}

	if !cfg.Kubernetes.Enabled && len(globals) == 0 {
		o.Log("No shared MCP servers configured.")
		return nil
	}

	if cfg.Kubernetes.Enabled {
		k8sMCPName := "forge-k8s-mcp"
		running, err := o.Containers.IsContainerRunning(ctx, k8sMCPName)
		if err != nil {
			return fmt.Errorf("failed to check k8s-mcp status: %w", err)
		}
		if running {
			o.Log("Stopping: %s", k8sMCPName)
			_ = o.Containers.StopContainer(ctx, k8sMCPName)
			_ = o.Containers.RemoveContainer(ctx, k8sMCPName)
		}
		if err := o.startKubernetesMCP(ctx, cfg); err != nil {
			return fmt.Errorf("failed to restart Kubernetes MCP: %w", err)
		}
	}

	// Restart global custom MCP sidecars: stop the existing instance so
	// ensureGlobalSidecar recreates it fresh.
	for _, s := range globals {
		name := globalSidecarName(s.Name)
		if running, _ := o.Containers.IsContainerRunning(ctx, name); running {
			o.Log("Stopping: %s", name)
			_ = o.Containers.StopContainer(ctx, name)
		}
		_ = o.Containers.RemoveContainer(ctx, name)
		if _, ok := o.ensureGlobalSidecar(ctx, s); !ok {
			o.Log("Warning: failed to restart global MCP %q", s.Name)
		}
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
	images = append(images, customSidecarImages(cfg)...)
	seen := make(map[string]bool, len(images))
	for _, img := range images {
		if seen[img] {
			continue
		}
		seen[img] = true
		o.Log("Pulling image: %s", img)
		if err := o.Containers.PullImage(ctx, img); err != nil {
			return fmt.Errorf("failed to pull image %s: %w", img, err)
		}
	}
	o.Log("All images up to date.")
	return nil
}

// customSidecarImages returns the images needed by type:"container" MCP servers,
// substituting the default wrapper image for wrapped stdio servers without an
// explicit image.
func customSidecarImages(cfg *config.Config) []string {
	var images []string
	for _, s := range cfg.MCPServers {
		if !s.IsContainer() {
			continue
		}
		img := s.Image
		if img == "" && s.IsWrappedStdio() {
			img = config.DefaultMCPWrapperImage
		}
		if img != "" {
			images = append(images, img)
		}
	}
	return images
}

package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/michael-freling/claude-forge/internal/forge"
	"github.com/michael-freling/claude-forge/internal/forge/auth"
	forgeconfig "github.com/michael-freling/claude-forge/internal/forge/config"
	"github.com/michael-freling/claude-forge/internal/forge/container"
	"github.com/michael-freling/claude-forge/internal/forge/kube"
	"github.com/michael-freling/claude-forge/internal/forge/project"
	"github.com/michael-freling/claude-forge/internal/forge/session"
	"github.com/michael-freling/claude-forge/internal/gateway"
	"github.com/spf13/cobra"
)

// version is set at build time via ldflags.
var version = "dev"

// orchestratorFactoryFunc creates an Orchestrator and returns a cleanup function.
type orchestratorFactoryFunc func() (*forge.Orchestrator, func(), error)

// createOrchestrator is the factory function for creating an Orchestrator.
// Tests override this to inject a mock.
var createOrchestrator orchestratorFactoryFunc = newOrchestrator

func main() {
	rootCmd := newRootCmd()
	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}

func newRootCmd() *cobra.Command {
	rootCmd := &cobra.Command{
		Use:   "claude-forge",
		Short: "Launch and manage Claude Code sessions in Docker containers",
		Long: `claude-forge orchestrates Claude Code inside Docker containers with a
secure gateway proxy for GitHub access. It manages agent and gateway
containers, Docker networks, and session state.`,
		SilenceUsage: true,
	}

	rootCmd.AddCommand(
		newInitCmd(),
		newStartCmd(),
		newResumeCmd(),
		newListCmd(),
		newPruneCmd(),
		newStopCmd(),
		newStatusCmd(),
		newBuildCmd(),
		newAuthCmd(),
		newPluginsCmd(),
		newVersionCmd(),
		newGatewayCmd(),
		newKubeCmd(),
		newMcpCmd(),
	)

	return rootCmd
}

// newOrchestrator creates an Orchestrator backed by a real Docker client.
func newOrchestrator() (*forge.Orchestrator, func(), error) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return nil, nil, fmt.Errorf("failed to get home directory: %w", err)
	}
	dockerClient, err := container.NewClient()
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create Docker client: %w", err)
	}
	orch := forge.NewOrchestrator(dockerClient, homeDir)
	cleanup := func() { dockerClient.Close() }
	return orch, cleanup, nil
}

// startSession runs the common logic for the start and resume commands.
func startSession(skipPermissions, worktree bool, prompt, resumeID, resumeSubdir string, continueSession bool, mounts []string, resumeWorktreeName, name string) error {
	orch, cleanup, err := createOrchestrator()
	if err != nil {
		return err
	}
	defer cleanup()

	// Sync the host's locally installed Claude Code plugins into forge's plugin
	// directory so they are available in the session without a manual sync.
	// Best-effort: a failure here should not block starting the session.
	if err := syncHostPlugins(orch.HomeDir); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: plugin sync failed: %v\n", err)
	}

	ctx := context.Background()
	hostUID := os.Getuid()
	hostGID := os.Getgid()
	interactive := prompt == ""

	sess, err := orch.Start(ctx, forge.StartOptions{
		SkipPermissions:    skipPermissions,
		Worktree:           worktree,
		Prompt:             prompt,
		ResumeID:           resumeID,
		ResumeSubdir:       resumeSubdir,
		Continue:           continueSession,
		Interactive:        interactive,
		UID:                hostUID,
		GID:                hostGID,
		Mounts:             mounts,
		ResumeWorktreeName: resumeWorktreeName,
		Name:               name,
	})
	if err != nil {
		return err
	}

	if interactive {
		// Attach to the agent container's TTY using docker attach.
		fmt.Println("Claude Code is ready. Attaching to session...")
		attachCmd := exec.Command("docker", "attach", sess.AgentName)
		attachCmd.Stdin = os.Stdin
		attachCmd.Stdout = os.Stdout
		attachCmd.Stderr = os.Stderr
		// docker attach returns an error when the container exits, which is expected.
		_ = attachCmd.Run()
	} else {
		// Non-interactive mode: wait for the container to exit, then print its logs.
		fmt.Println("Claude Code is running in non-interactive mode...")
		waitCmd := exec.Command("docker", "wait", sess.AgentName)
		waitOutput, err := waitCmd.Output()
		if err != nil {
			fmt.Fprintf(os.Stderr, "Warning: docker wait failed: %v\n", err)
		}

		logsCmd := exec.Command("docker", "logs", sess.AgentName)
		logsCmd.Stdout = os.Stdout
		logsCmd.Stderr = os.Stderr
		_ = logsCmd.Run()

		// Check exit code from docker wait
		exitCode := strings.TrimSpace(string(waitOutput))
		if exitCode != "" && exitCode != "0" {
			orch.Cleanup(context.Background(), sess)
			return fmt.Errorf("agent exited with code %s", exitCode)
		}
	}

	// Clean up after the container finishes.
	orch.Cleanup(context.Background(), sess)
	return nil
}

// newInitCmd creates the "init" subcommand.
func newInitCmd() *cobra.Command {
	var force bool

	cmd := &cobra.Command{
		Use:   "init",
		Short: "Create a default config file",
		Long: `Write a config.yaml to ~/.config/claude-forge/ with detected settings.
Kubernetes contexts are auto-detected from your kubeconfig.
If the file already exists, use --force to overwrite it.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			homeDir, err := os.UserHomeDir()
			if err != nil {
				return fmt.Errorf("failed to get home directory: %w", err)
			}
			configDir := filepath.Join(homeDir, ".config", "claude-forge")
			configPath := filepath.Join(configDir, "config.yaml")

			if !force {
				if _, err := os.Stat(configPath); err == nil {
					return fmt.Errorf("config already exists at %s (use --force to overwrite)", configPath)
				}
			}

			content := buildConfigTemplate(homeDir)

			if err := os.MkdirAll(configDir, 0o755); err != nil {
				return fmt.Errorf("failed to create config directory: %w", err)
			}
			if err := os.WriteFile(configPath, []byte(content), 0o644); err != nil {
				return fmt.Errorf("failed to write config: %w", err)
			}

			fmt.Printf("Config written to %s\n", configPath)
			return nil
		},
	}

	cmd.Flags().BoolVar(&force, "force", false, "Overwrite existing config file")
	return cmd
}

// buildConfigTemplate generates the config.yaml content, auto-detecting
// Kubernetes contexts from the user's kubeconfig.
func buildConfigTemplate(homeDir string) string {
	var b strings.Builder

	b.WriteString(`# claude-forge configuration
# Location: ~/.config/claude-forge/config.yaml

# Docker images used for each container role.
images:
  agent: ` + forgeconfig.DefaultAgentImage + `
  gateway: ` + forgeconfig.DefaultGatewayImage + `
  github_mcp: ` + forgeconfig.DefaultGitHubMCPImage + `

# Default flags applied to every "start" invocation.
defaults:
  skip_permissions: false
  worktree: false

# Kubernetes MCP server integration.
# When enabled, a shared MCP server container gives agents read-only
# access to your clusters via short-lived ServiceAccount tokens.
#
# Prerequisites:
#   1. Create RBAC resources:  claude-forge kube render --context <ctx> | kubectl apply -f -
#   2. Uncomment the section below.
`)

	kubeconfigPath := os.Getenv("KUBECONFIG")
	if kubeconfigPath == "" {
		kubeconfigPath = filepath.Join(homeDir, ".kube", "config")
	}

	contexts, err := kube.ListContexts(kubeconfigPath)
	if err != nil || len(contexts) == 0 {
		b.WriteString(`#
# kubernetes:
#   enabled: true
#   image: ` + forgeconfig.DefaultKubernetesMCPImage + `
#   default_context: my-cluster
#   contexts:
#     - host_context: my-cluster
#       service_account_name: claude-forge-agent
#       service_account_namespace: default
`)
		return b.String()
	}

	b.WriteString("# kubernetes:\n")
	b.WriteString("#   enabled: true\n")
	b.WriteString("#   image: " + forgeconfig.DefaultKubernetesMCPImage + "\n")
	b.WriteString("#   default_context: " + contexts[0] + "\n")
	b.WriteString("#   contexts:\n")
	for _, ctx := range contexts {
		b.WriteString("#     - host_context: " + ctx + "\n")
		b.WriteString("#       service_account_name: claude-forge-agent\n")
		b.WriteString("#       service_account_namespace: default\n")
	}

	return b.String()
}

// newStartCmd creates the "start" subcommand.
func newStartCmd() *cobra.Command {
	var (
		worktree          bool
		noSkipPermissions bool
		prompt            string
		mounts            []string
	)

	cmd := &cobra.Command{
		Use:   "start <name>",
		Short: "Start a new Claude Code session in a Docker container",
		Long: `Start launches a new Claude Code agent and gateway in Docker containers.
The required <name> argument is a human-readable session name; it is passed to
Claude Code and shown in 'resume --list'. By default,
--dangerously-skip-permissions is enabled; use --no-skip-permissions to disable
it.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			skipPermissions := !noSkipPermissions
			return startSession(skipPermissions, worktree, prompt, "", "", false, mounts, "", args[0])
		},
	}

	cmd.Flags().BoolVar(&worktree, "worktree", false, "Enable worktree mode for Claude Code")
	cmd.Flags().BoolVar(&noSkipPermissions, "no-skip-permissions", false, "Disable --dangerously-skip-permissions")
	cmd.Flags().StringVarP(&prompt, "prompt", "p", "", "Initial prompt to send to Claude Code")
	cmd.Flags().StringArrayVar(&mounts, "mount", nil, "Additional host directories to mount (format: host_path:container_path)")

	return cmd
}

// projectSessionDir resolves the host session directory for the project in the
// current working directory.
func projectSessionDir() (string, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("failed to get working directory: %w", err)
	}
	proj, err := project.Identify(cwd)
	if err != nil {
		return "", fmt.Errorf("failed to identify project: %w", err)
	}
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("failed to get home directory: %w", err)
	}
	return filepath.Join(homeDir, ".claude-forge", proj.ID), nil
}

// listSessions writes the session table for sessionDir to w.
func listSessions(w io.Writer, sessionDir string) error {
	sessions, err := session.List(sessionDir)
	if err != nil {
		return fmt.Errorf("failed to list sessions: %w", err)
	}
	if len(sessions) == 0 {
		fmt.Fprintln(w, "No sessions found.")
		return nil
	}

	tw := tabwriter.NewWriter(w, 0, 4, 2, ' ', 0)
	fmt.Fprintln(tw, "SESSION ID\tNAME\tCREATED\tWORKTREE\tFIRST MESSAGE")
	for _, s := range sessions {
		firstMsg := s.FirstMsg
		if len(firstMsg) > 60 {
			firstMsg = firstMsg[:57] + "..."
		}
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\n", s.ID, s.Name, s.CreatedAt.Format(time.RFC3339), s.WorktreeName(), firstMsg)
	}
	return tw.Flush()
}

// newListCmd creates the "list" subcommand.
func newListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List past Claude Code sessions for the current project",
		RunE: func(cmd *cobra.Command, args []string) error {
			sessionDir, err := projectSessionDir()
			if err != nil {
				return err
			}
			return listSessions(cmd.OutOrStdout(), sessionDir)
		},
	}
}

// parseAge parses a session age. It accepts standard Go durations (e.g. "720h")
// plus a "<N>d" day form (e.g. "30d").
func parseAge(s string) (time.Duration, error) {
	if rest, ok := strings.CutSuffix(s, "d"); ok {
		days, err := strconv.Atoi(rest)
		if err != nil {
			return 0, fmt.Errorf("invalid duration %q", s)
		}
		return time.Duration(days) * 24 * time.Hour, nil
	}
	return time.ParseDuration(s)
}

// newPruneCmd creates the "prune" subcommand.
func newPruneCmd() *cobra.Command {
	var (
		olderThan string
		keep      int
		dryRun    bool
	)

	cmd := &cobra.Command{
		Use:   "prune",
		Short: "Delete old Claude Code sessions for the current project",
		Long: `Prune removes session transcripts (and their name sidecars) for the
current project. By default it deletes sessions older than 30 days. Narrow
or widen the window with --older-than, and protect the N most recent
sessions with --keep. Use --dry-run to preview.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			var maxAge time.Duration
			if olderThan != "" {
				var err error
				maxAge, err = parseAge(olderThan)
				if err != nil {
					return fmt.Errorf("invalid --older-than: %w", err)
				}
			}

			sessionDir, err := projectSessionDir()
			if err != nil {
				return err
			}
			sessions, err := session.List(sessionDir)
			if err != nil {
				return fmt.Errorf("failed to list sessions: %w", err)
			}

			// sessions are sorted most-recent-first.
			now := time.Now()
			var toPrune []session.Session
			for i, s := range sessions {
				if keep >= 0 && i < keep {
					continue // protected: among the N newest
				}
				if olderThan != "" && now.Sub(s.CreatedAt) < maxAge {
					continue // not old enough
				}
				toPrune = append(toPrune, s)
			}

			w := cmd.OutOrStdout()
			if len(toPrune) == 0 {
				fmt.Fprintln(w, "No sessions to prune.")
				return nil
			}

			for _, s := range toPrune {
				if dryRun {
					fmt.Fprintf(w, "would delete  %s  %s  %s\n", s.ID, s.CreatedAt.Format(time.RFC3339), s.Name)
					continue
				}
				if err := session.Delete(sessionDir, s); err != nil {
					return err
				}
				fmt.Fprintf(w, "deleted  %s  %s  %s\n", s.ID, s.CreatedAt.Format(time.RFC3339), s.Name)
			}

			verb := "Deleted"
			if dryRun {
				verb = "Would delete"
			}
			fmt.Fprintf(w, "%s %d session(s).\n", verb, len(toPrune))
			return nil
		},
	}

	cmd.Flags().StringVar(&olderThan, "older-than", "30d", "Delete sessions older than this age (e.g. 30d, 720h)")
	cmd.Flags().IntVar(&keep, "keep", -1, "Keep the N most recent sessions, delete the rest")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "Show what would be deleted without deleting")

	return cmd
}

// newResumeCmd creates the "resume" subcommand.
func newResumeCmd() *cobra.Command {
	var (
		mounts []string
		name   string
	)

	cmd := &cobra.Command{
		Use:   "resume [session-id|name]",
		Short: "Resume a past Claude Code session",
		Long: `Resume a previous session by ID or by name (the most recent session
with that name). If no argument is given, the most recent session is
continued. Run 'claude-forge list' to see available sessions. The session
name is reused unless overridden with --name.`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			sessionDir, err := projectSessionDir()
			if err != nil {
				return err
			}

			if len(args) == 1 {
				sess, err := session.Find(sessionDir, args[0])
				if err != nil {
					return err
				}
				// Use the resolved session ID (args[0] may be a name).
				return startSession(true, sess.IsWorktree(), "", sess.ID, sess.Subdir, false, mounts, sess.WorktreeName(), resumeName(name, sess.Name))
			}

			// Continue most recent session, detecting worktree if needed
			sessions, err := session.List(sessionDir)
			if err != nil {
				return fmt.Errorf("failed to list sessions: %w", err)
			}
			if len(sessions) == 0 {
				return fmt.Errorf("no sessions found to continue")
			}
			mostRecent := sessions[0]
			return startSession(true, mostRecent.IsWorktree(), "", mostRecent.ID, mostRecent.Subdir, false, mounts, mostRecent.WorktreeName(), resumeName(name, mostRecent.Name))
		},
	}

	cmd.Flags().StringArrayVar(&mounts, "mount", nil, "Additional host directories to mount (format: host_path:container_path)")
	cmd.Flags().StringVarP(&name, "name", "n", "", "Override the session name passed to Claude Code (defaults to the stored name)")

	return cmd
}

// resumeName returns the override name if provided, otherwise the stored name.
func resumeName(override, stored string) string {
	if override != "" {
		return override
	}
	return stored
}

// newStopCmd creates the "stop" subcommand.
func newStopCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "stop",
		Short: "Stop running Claude Code containers for the current project",
		RunE: func(cmd *cobra.Command, args []string) error {
			orch, cleanup, err := createOrchestrator()
			if err != nil {
				return err
			}
			defer cleanup()

			cwd, err := os.Getwd()
			if err != nil {
				return fmt.Errorf("failed to get working directory: %w", err)
			}

			return orch.Stop(context.Background(), cwd)
		},
	}
}

// newStatusCmd creates the "status" subcommand.
func newStatusCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Show running Claude Code containers",
		RunE: func(cmd *cobra.Command, args []string) error {
			orch, cleanup, err := createOrchestrator()
			if err != nil {
				return err
			}
			defer cleanup()

			containers, err := orch.Status(context.Background())
			if err != nil {
				return err
			}

			if len(containers) == 0 {
				fmt.Println("No running forge containers.")
				return nil
			}

			w := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
			fmt.Fprintln(w, "NAME\tIMAGE\tSTATUS\tCREATED")
			for _, c := range containers {
				fmt.Fprintf(w, "%s\t%s\t%s\t%s\n",
					c.Name,
					c.Image,
					c.Status,
					c.Created.Format(time.RFC3339),
				)
			}
			return w.Flush()
		},
	}
}

// newBuildCmd creates the "build" subcommand.
func newBuildCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "build",
		Short: "Pull or rebuild Claude Code Docker images",
		RunE: func(cmd *cobra.Command, args []string) error {
			orch, cleanup, err := createOrchestrator()
			if err != nil {
				return err
			}
			defer cleanup()

			return orch.Build(context.Background())
		},
	}
}

// newAuthCmd creates the "auth" subcommand.
func newAuthCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "auth",
		Short: "Verify Claude Code authentication credentials",
		RunE: func(cmd *cobra.Command, args []string) error {
			homeDir, err := os.UserHomeDir()
			if err != nil {
				return fmt.Errorf("failed to get home directory: %w", err)
			}
			claudeDir := filepath.Join(homeDir, ".claude")

			creds, err := auth.Resolve(claudeDir)
			if err != nil {
				fmt.Fprintf(os.Stderr, "No credentials found: %v\n", err)
				os.Exit(1)
			}

			fmt.Printf("Auth type: %s\n", creds.AuthType)
			token := creds.Token
			if len(token) > 12 {
				fmt.Printf("Token: %s...%s\n", token[:8], token[len(token)-4:])
			} else {
				fmt.Println("Token: [present]")
			}
			return nil
		},
	}
}

// newVersionCmd creates the "version" subcommand.
func newVersionCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print the claude-forge version",
		Run: func(cmd *cobra.Command, args []string) {
			fmt.Printf("claude-forge version %s\n", version)
		},
	}
}

// newGatewayCmd creates the "gateway" subcommand for running inside the
// gateway container.
func newGatewayCmd() *cobra.Command {
	var (
		owner     string
		repo      string
		proxyAddr string
	)

	cmd := &cobra.Command{
		Use:   "gateway",
		Short: "Start the gateway server (for container use)",
		Long: `Start the gateway proxy server. This is typically invoked as the
entrypoint of the gateway container, not by end users directly.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if owner == "" || repo == "" {
				return fmt.Errorf("--owner and --repo are required")
			}

			srv, err := gateway.NewServer(gateway.ProxyConfig{
				AllowedOwner: owner,
				AllowedRepo:  repo,
			})
			if err != nil {
				return fmt.Errorf("failed to create gateway server: %w", err)
			}

			fmt.Printf("Gateway starting: proxy=%s owner=%s repo=%s\n", proxyAddr, owner, repo)
			return srv.Run(proxyAddr)
		},
	}

	cmd.Flags().StringVar(&owner, "owner", "", "Allowed GitHub repository owner")
	cmd.Flags().StringVar(&repo, "repo", "", "Allowed GitHub repository name")
	cmd.Flags().StringVar(&proxyAddr, "proxy-addr", ":8080", "Address for the git proxy server")

	return cmd
}

// newPluginsCmd creates the "plugins" subcommand with sync subcommand.
func newPluginsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "plugins",
		Short: "Manage forge plugins",
	}
	cmd.AddCommand(newPluginsSyncCmd())
	return cmd
}

// newPluginsSyncCmd creates the "plugins sync" subcommand that installs host
// plugins into the running agent container.
func newPluginsSyncCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "sync",
		Short: "Sync host plugins into forge's plugin directory",
		Long: `Reads ~/.claude/plugins/installed_plugins.json from the host, starts a
temporary container, and runs "claude plugins install" for each plugin.
Plugins persist in ~/.claude-forge/plugins/ across sessions.`,
		RunE: pluginsSyncRun,
	}
}

var pluginsSyncRun = func(cmd *cobra.Command, args []string) error {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("failed to get home directory: %w", err)
	}
	return syncHostPlugins(homeDir)
}

// syncHostPlugins reinstalls the host's locally installed Claude Code plugins
// into forge's persistent plugins directory (~/.claude-forge/plugins) by running
// "claude plugins install" inside a temporary container. It is a no-op when the
// host has no plugins. Used by "plugins sync" and automatically at session start.
var syncHostPlugins = func(homeDir string) error {
	plugins, err := readHostPlugins(homeDir)
	if err != nil {
		return err
	}
	if len(plugins) == 0 {
		fmt.Println("No plugins found on host.")
		return nil
	}

	configDir := filepath.Join(homeDir, ".config", "claude-forge")
	cfg, err := forgeconfig.Load(configDir)
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	pluginsDir := filepath.Join(homeDir, ".claude-forge", "plugins")
	if err := os.MkdirAll(pluginsDir, 0o755); err != nil {
		return fmt.Errorf("failed to create plugins directory: %w", err)
	}

	// Read host marketplace sources to add them inside the container
	hostPluginsDir := filepath.Join(homeDir, ".claude", "plugins")
	mpInfo := readHostMarketplaces(hostPluginsDir)

	// Filter plugins: skip those from unavailable marketplaces, deduplicate by name
	var syncPlugins []string
	seenNames := make(map[string]bool)
	for _, plugin := range plugins {
		parts := strings.SplitN(plugin, "@", 2)
		name := parts[0]
		marketplace := ""
		if len(parts) == 2 {
			marketplace = parts[1]
		}
		if marketplace != "" && !mpInfo.Names[marketplace] {
			fmt.Printf("Skipping %s (marketplace %q not available remotely)\n", plugin, marketplace)
			continue
		}
		if seenNames[name] {
			continue
		}
		seenNames[name] = true
		syncPlugins = append(syncPlugins, plugin)
	}

	if len(syncPlugins) == 0 {
		fmt.Println("No syncable plugins found.")
		return nil
	}

	containerName := "forge-plugins-sync"
	fmt.Printf("Syncing %d plugins...\n", len(syncPlugins))

	// Start a temporary container with the plugins dir mounted
	uid := os.Getuid()
	gid := os.Getgid()
	runArgs := []string{
		"run", "--rm", "--name", containerName,
		"-e", fmt.Sprintf("FORGE_UID=%d", uid),
		"-e", fmt.Sprintf("FORGE_GID=%d", gid),
		"-v", pluginsDir + ":/home/user/.claude/plugins",
	}

	// Build commands: add marketplaces, update, then install each plugin
	var installCmds []string
	for _, src := range mpInfo.Sources {
		installCmds = append(installCmds, fmt.Sprintf("claude plugins marketplace add %s || true", src))
	}
	installCmds = append(installCmds, "claude plugins marketplace update")
	for _, plugin := range syncPlugins {
		installCmds = append(installCmds, fmt.Sprintf("claude plugins install %s || true", plugin))
	}
	shellCmd := strings.Join(installCmds, " && ")

	runArgs = append(runArgs, cfg.Images.Agent, "bash", "-c", shellCmd)

	dockerCmd := exec.Command("docker", runArgs...)
	dockerCmd.Stdout = os.Stdout
	dockerCmd.Stderr = os.Stderr

	if err := dockerCmd.Run(); err != nil {
		return fmt.Errorf("plugin sync failed: %w", err)
	}

	// Write enabledPlugins to settings.json so the agent container picks them up
	if err := enablePluginsInSettings(configDir, syncPlugins); err != nil {
		return fmt.Errorf("failed to update settings: %w", err)
	}

	fmt.Println("Plugin sync complete.")
	return nil
}

// readHostPlugins reads the host's installed_plugins.json and returns plugin
// identifiers in "name@marketplace" format.
func readHostPlugins(homeDir string) ([]string, error) {
	pluginsPath := filepath.Join(homeDir, ".claude", "plugins", "installed_plugins.json")
	data, err := os.ReadFile(pluginsPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to read installed_plugins.json: %w", err)
	}

	var file struct {
		Plugins map[string]any `json:"plugins"`
	}
	if err := json.Unmarshal(data, &file); err != nil {
		return nil, fmt.Errorf("failed to parse installed_plugins.json: %w", err)
	}

	var plugins []string
	for key := range file.Plugins {
		plugins = append(plugins, key)
	}
	return plugins, nil
}

// enablePluginsInSettings reads settings.json from configDir, adds an
// enabledPlugins map for all synced plugins, and writes it back.
func enablePluginsInSettings(configDir string, plugins []string) error {
	settingsPath := filepath.Join(configDir, "settings.json")
	data, err := os.ReadFile(settingsPath)
	if err != nil {
		return err
	}

	var settings map[string]any
	if err := json.Unmarshal(data, &settings); err != nil {
		return err
	}

	enabled := make(map[string]bool)
	for _, p := range plugins {
		enabled[p] = true
	}
	settings["enabledPlugins"] = enabled

	out, err := json.MarshalIndent(settings, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(settingsPath, append(out, '\n'), 0o644)
}

// marketplaceInfo holds parsed marketplace data.
type marketplaceInfo struct {
	Sources []string        // GitHub repos to add
	Names   map[string]bool // names of available (GitHub-sourced) marketplaces
}

// readHostMarketplaces reads known_marketplaces.json and returns marketplace info.
func readHostMarketplaces(hostPluginsDir string) marketplaceInfo {
	info := marketplaceInfo{Names: make(map[string]bool)}

	data, err := os.ReadFile(filepath.Join(hostPluginsDir, "known_marketplaces.json"))
	if err != nil {
		return info
	}

	var marketplaces map[string]struct {
		Source struct {
			Source string `json:"source"`
			Repo   string `json:"repo"`
			Path   string `json:"path"`
		} `json:"source"`
	}
	if err := json.Unmarshal(data, &marketplaces); err != nil {
		return info
	}

	for name, m := range marketplaces {
		switch m.Source.Source {
		case "github":
			if m.Source.Repo != "" {
				info.Sources = append(info.Sources, m.Source.Repo)
				info.Names[name] = true
			}
		case "directory":
			if repo := extractGitHubRepo(m.Source.Path); repo != "" {
				info.Sources = append(info.Sources, repo)
				info.Names[name] = true
			}
		}
	}
	return info
}

// extractGitHubRepo parses "owner/repo" from a path containing "github.com/{owner}/{repo}".
func extractGitHubRepo(path string) string {
	const marker = "github.com/"
	idx := strings.Index(path, marker)
	if idx < 0 {
		return ""
	}
	rest := path[idx+len(marker):]
	parts := strings.SplitN(rest, "/", 3)
	if len(parts) < 2 || parts[0] == "" || parts[1] == "" {
		return ""
	}
	return parts[0] + "/" + parts[1]
}

// newKubeCmd creates the "kube" subcommand group for Kubernetes integration.
func newKubeCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "kube",
		Short: "Kubernetes integration commands",
	}
	cmd.AddCommand(newKubeRenderCmd())
	return cmd
}

// newKubeRenderCmd creates the "kube render" subcommand that generates RBAC
// manifests for the Kubernetes MCP server's service account.
func newKubeRenderCmd() *cobra.Command {
	var (
		clusterRoleName         string
		serviceAccountName      string
		serviceAccountNamespace string
		kubeconfig              string
		kubeContext             string
	)

	cmd := &cobra.Command{
		Use:   "render",
		Short: "Generate RBAC manifests for Kubernetes MCP access",
		Long: `Discovers API resources from the target cluster and generates
ServiceAccount, ClusterRole, and ClusterRoleBinding YAML manifests with
safe carveouts (no secrets, no exec, no RBAC tampering).

The output can be piped directly to kubectl apply:

  claude-forge kube render --context dev | kubectl apply -f -`,
		RunE: func(cmd *cobra.Command, args []string) error {
			output, err := kube.Render(kube.RenderOptions{
				ClusterRoleName:         clusterRoleName,
				ServiceAccountName:      serviceAccountName,
				ServiceAccountNamespace: serviceAccountNamespace,
				Kubeconfig:              kubeconfig,
				Context:                 kubeContext,
			})
			if err != nil {
				return err
			}
			fmt.Print(output)
			return nil
		},
	}

	cmd.Flags().StringVar(&clusterRoleName, "cluster-role-name", "claude-forge-agent",
		"Name for the generated ClusterRole and ClusterRoleBinding")
	cmd.Flags().StringVar(&serviceAccountName, "service-account-name", "claude-forge-agent", "Name for the generated ServiceAccount")
	cmd.Flags().StringVar(&serviceAccountNamespace, "service-account-namespace", "default", "Namespace for the ServiceAccount")
	cmd.Flags().StringVar(&kubeconfig, "kubeconfig", "", "Path to kubeconfig (defaults to $KUBECONFIG or ~/.kube/config)")
	cmd.Flags().StringVar(&kubeContext, "context", "", "Kubeconfig context to use for API discovery")

	return cmd
}

func newMcpCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "mcp",
		Short: "Manage shared MCP servers",
	}
	cmd.AddCommand(newMcpRestartCmd())
	return cmd
}

func newMcpRestartCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "restart",
		Short: "Restart all shared MCP server containers",
		RunE: func(cmd *cobra.Command, args []string) error {
			orch, cleanup, err := createOrchestrator()
			if err != nil {
				return err
			}
			defer cleanup()

			return orch.RestartSharedMCP(context.Background())
		},
	}
}

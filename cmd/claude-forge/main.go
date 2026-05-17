package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/michael-freling/claude-code-tools/internal/forge"
	"github.com/michael-freling/claude-code-tools/internal/forge/auth"
	forgeconfig "github.com/michael-freling/claude-code-tools/internal/forge/config"
	"github.com/michael-freling/claude-code-tools/internal/forge/container"
	"github.com/michael-freling/claude-code-tools/internal/forge/project"
	"github.com/michael-freling/claude-code-tools/internal/forge/session"
	"github.com/michael-freling/claude-code-tools/internal/forgegh"
	"github.com/michael-freling/claude-code-tools/internal/gateway"
	"github.com/spf13/cobra"
)

// version is set at build time via ldflags.
var version = "dev"

// orchestratorFactoryFunc creates an Orchestrator and returns a cleanup function.
type orchestratorFactoryFunc func() (*forge.Orchestrator, func(), error)

// createOrchestrator is the factory function for creating an Orchestrator.
// Tests override this to inject a mock.
var createOrchestrator orchestratorFactoryFunc = newOrchestrator

// forgeGHGatewayURL is the gateway URL used by the forge-gh client.
// Tests override this to inject a non-routable address.
var forgeGHGatewayURL = "http://gateway:8083"

func main() {
	// Busybox-style multi-call binary: if invoked as "gh" or "forge-gh",
	// act as the forge-gh GitHub CLI wrapper.
	basename := filepath.Base(os.Args[0])
	if basename == "gh" || basename == "forge-gh" {
		client := forgegh.NewClient(forgeGHGatewayURL)
		if err := client.Run(os.Args[1:]); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
		return
	}

	// Normal CLI mode.
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
		newStartCmd(),
		newResumeCmd(),
		newStopCmd(),
		newStatusCmd(),
		newBuildCmd(),
		newAuthCmd(),
		newPluginsCmd(),
		newVersionCmd(),
		newGatewayCmd(),
		newForgeGHCmd(),
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
func startSession(skipPermissions, worktree bool, prompt, resumeID string, continueSession bool, mounts []string) error {
	orch, cleanup, err := createOrchestrator()
	if err != nil {
		return err
	}
	defer cleanup()

	ctx := context.Background()
	hostUID := os.Getuid()
	hostGID := os.Getgid()
	interactive := prompt == ""

	sess, err := orch.Start(ctx, forge.StartOptions{
		SkipPermissions: skipPermissions,
		Worktree:        worktree,
		Prompt:          prompt,
		ResumeID:        resumeID,
		Continue:        continueSession,
		Interactive:     interactive,
		UID:             hostUID,
		GID:             hostGID,
		Mounts:          mounts,
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

// newStartCmd creates the "start" subcommand.
func newStartCmd() *cobra.Command {
	var (
		worktree          bool
		noSkipPermissions bool
		prompt            string
		mounts            []string
	)

	cmd := &cobra.Command{
		Use:   "start",
		Short: "Start a new Claude Code session in a Docker container",
		Long: `Start launches a new Claude Code agent and gateway in Docker containers.
By default, --dangerously-skip-permissions is enabled. Use --no-skip-permissions
to disable it.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			skipPermissions := !noSkipPermissions
			return startSession(skipPermissions, worktree, prompt, "", false, mounts)
		},
	}

	cmd.Flags().BoolVar(&worktree, "worktree", false, "Enable worktree mode for Claude Code")
	cmd.Flags().BoolVar(&noSkipPermissions, "no-skip-permissions", false, "Disable --dangerously-skip-permissions")
	cmd.Flags().StringVarP(&prompt, "prompt", "p", "", "Initial prompt to send to Claude Code")
	cmd.Flags().StringArrayVar(&mounts, "mount", nil, "Additional host directories to mount (format: host_path:container_path)")

	return cmd
}

// newResumeCmd creates the "resume" subcommand.
func newResumeCmd() *cobra.Command {
	var list bool

	cmd := &cobra.Command{
		Use:   "resume [session-id]",
		Short: "Resume a past Claude Code session",
		Long: `Resume a previous session by ID, or use --list to see available sessions.
If no session ID is given and --list is not set, the most recent session
is continued.`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cwd, err := os.Getwd()
			if err != nil {
				return fmt.Errorf("failed to get working directory: %w", err)
			}

			proj, err := project.Identify(cwd)
			if err != nil {
				return fmt.Errorf("failed to identify project: %w", err)
			}

			homeDir, err := os.UserHomeDir()
			if err != nil {
				return fmt.Errorf("failed to get home directory: %w", err)
			}
			sessionDir := filepath.Join(homeDir, ".claude-forge", proj.ID)

			if list {
				sessions, err := session.List(sessionDir)
				if err != nil {
					return fmt.Errorf("failed to list sessions: %w", err)
				}
				if len(sessions) == 0 {
					fmt.Println("No sessions found.")
					return nil
				}

				w := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
				fmt.Fprintln(w, "SESSION ID\tCREATED\tFIRST MESSAGE")
				for _, s := range sessions {
					firstMsg := s.FirstMsg
					if len(firstMsg) > 60 {
						firstMsg = firstMsg[:57] + "..."
					}
					fmt.Fprintf(w, "%s\t%s\t%s\n", s.ID, s.CreatedAt.Format(time.RFC3339), firstMsg)
				}
				return w.Flush()
			}

			if len(args) == 1 {
				return startSession(true, false, "", args[0], false, nil)
			}

			return startSession(true, false, "", "", true, nil)
		},
	}

	cmd.Flags().BoolVar(&list, "list", false, "List available sessions")

	return cmd
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
		apiAddr   string
	)

	cmd := &cobra.Command{
		Use:   "gateway",
		Short: "Start the gateway server (for container use)",
		Long: `Start the gateway proxy and API server. This is typically invoked as the
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

			fmt.Printf("Gateway starting: proxy=%s api=%s owner=%s repo=%s\n", proxyAddr, apiAddr, owner, repo)
			return srv.Run(proxyAddr, apiAddr)
		},
	}

	cmd.Flags().StringVar(&owner, "owner", "", "Allowed GitHub repository owner")
	cmd.Flags().StringVar(&repo, "repo", "", "Allowed GitHub repository name")
	cmd.Flags().StringVar(&proxyAddr, "proxy-addr", ":8080", "Address for the git proxy server")
	cmd.Flags().StringVar(&apiAddr, "api-addr", ":8083", "Address for the API server")

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

	containerName := "forge-plugins-sync"
	fmt.Printf("Syncing %d plugins...\n", len(plugins))

	// Start a temporary container with the plugins dir mounted
	runArgs := []string{
		"run", "--rm", "--name", containerName,
		"-v", pluginsDir + ":/home/user/.claude/plugins",
	}

	// Build install commands: update marketplaces first, then install each plugin
	var installCmds []string
	installCmds = append(installCmds, "claude plugins marketplace update")
	for _, plugin := range plugins {
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

// newForgeGHCmd creates the "forge-gh" subcommand as an explicit alternative
// to the os.Args[0] detection for running as the GitHub CLI wrapper.
func newForgeGHCmd() *cobra.Command {
	return &cobra.Command{
		Use:                "forge-gh",
		Short:              "Act as GitHub CLI wrapper (for container use)",
		Long:               `Proxy GitHub CLI commands through the gateway API server. This is used inside the agent container as an alternative to the busybox-style os.Args[0] detection.`,
		DisableFlagParsing: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			client := forgegh.NewClient(forgeGHGatewayURL)
			return client.Run(args)
		},
	}
}

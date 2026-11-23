package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/michael-freling/claude-forge/internal/forge/claudecode"
	forgeconfig "github.com/michael-freling/claude-forge/internal/forge/config"
	"github.com/michael-freling/claude-forge/internal/forge/openshell"
	"github.com/michael-freling/claude-forge/internal/forge/project"
)

// newOpenShellManager builds an OpenShell session manager from the host's
// claude-forge config.
var newOpenShellManager = func() (*openshell.Manager, error) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("failed to get home directory: %w", err)
	}
	configDir := filepath.Join(homeDir, ".config", "claude-forge")
	cfg, err := forgeconfig.Load(configDir)
	if err != nil {
		return nil, fmt.Errorf("failed to load config: %w", err)
	}
	if _, err := exec.LookPath("openshell"); err != nil {
		return nil, fmt.Errorf("openshell CLI not found in PATH: install OpenShell first")
	}
	claudeDir := filepath.Join(homeDir, ".claude")

	return &openshell.Manager{
		Client: &openshell.Client{
			Runner: openshell.ExecRunner{Gateway: cfg.OpenShell.Gateway},
		},
		Config:    cfg,
		ClaudeDir: claudeDir,
		ConfigDir: configDir,
		HomeDir:   homeDir,
		Out:       os.Stderr,
		GitHubCLIToken: func(ctx context.Context) (string, error) {
			if _, err := exec.LookPath("gh"); err != nil {
				return "", err
			}
			out, err := exec.CommandContext(ctx, "gh", "auth", "token").Output()
			return string(out), err
		},
		GitIdentity: func() (string, string) {
			return project.GitConfig("user.name"), project.GitConfig("user.email")
		},
		ClaudeModel: func() string { return claudecode.ReadHostModel(claudeDir) },
	}, nil
}

func newOpenShellCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "openshell",
		Short: "Run Claude Code sessions in OpenShell sandboxes",
		Long: `Run Claude Code in OpenShell sandboxes instead of Docker containers.

An OpenShell session is not tied to a project directory or git repository:
each sandbox has its own persistent workspace (~/work). 'start --repo' and
openshell.repositories clone repositories into it when the sandbox is
created, and the agent can clone more whenever it needs them. There is
no gateway container or git proxy. Network access comes from a generated
sandbox policy that openshell.policy can extend and openshell.policy_file
replace (see 'claude-forge openshell policy'), and the Claude and
GitHub credentials are OpenShell providers, so the sandbox only ever sees
placeholders for them.

Sandboxes use the same Claude login as 'claude-forge start' (ANTHROPIC_API_KEY,
CLAUDE_CODE_OAUTH_TOKEN, or your 'claude' login in ~/.claude), so there is no
login per sandbox. Only the current access token is handed to OpenShell, never
the refresh token, and it is re-synced on every start and connect.`,
	}
	cmd.AddCommand(
		newOpenShellStartCmd(),
		newOpenShellConnectCmd(),
		newOpenShellListCmd(),
		newOpenShellStopCmd(),
		newOpenShellDeleteCmd(),
		newOpenShellAuthCmd(),
		newOpenShellPolicyCmd(),
	)
	return cmd
}

func newOpenShellStartCmd() *cobra.Command {
	var (
		noSkipPermissions bool
		prompt            string
		agent             string
		repos             []string
	)
	cmd := &cobra.Command{
		Use:   "start <name>",
		Short: "Create a sandbox and start Claude Code in it",
		Long: `Create an OpenShell sandbox named <name> and start Claude Code in its
workspace. Exiting Claude Code leaves the sandbox running; reconnect with
'claude-forge openshell connect <name>'.

Before Claude Code starts, the new sandbox is set up:

  - The repositories in openshell.repositories and --repo are cloned into
    ~/work/<name> (~/work/<owner>-<name> when two share a name).
  - Claude Code plugins are installed: openshell.plugins, or the plugins
    enabled on the host when that key is not set. Each plugin's marketplace
    is added from the host's marketplace list; one added from a local
    directory is added from that directory's GitHub origin.

A repository or plugin that fails only prints a warning.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			m, err := newOpenShellManager()
			if err != nil {
				return err
			}
			return m.Start(cmd.Context(), openshell.StartOptions{
				Name:            args[0],
				Prompt:          prompt,
				SkipPermissions: !noSkipPermissions,
				Agent:           agent,
				Repos:           repos,
			})
		},
	}
	cmd.Flags().BoolVar(&noSkipPermissions, "no-skip-permissions", false, "Disable --dangerously-skip-permissions")
	cmd.Flags().StringVarP(&prompt, "prompt", "p", "", "Initial prompt to send to Claude Code")
	cmd.Flags().StringVar(&agent, "agent", "", agentFlagUsage)
	cmd.Flags().StringArrayVar(&repos, "repo", nil, "GitHub repository (owner/name) to clone into the workspace, in addition to openshell.repositories (repeatable)")
	return cmd
}

func newOpenShellConnectCmd() *cobra.Command {
	var (
		noSkipPermissions bool
		agent             string
	)
	cmd := &cobra.Command{
		Use:   "connect <name>",
		Short: "Continue Claude Code in an existing sandbox",
		Long: `Run Claude Code again in sandbox <name>, continuing its most recent
conversation. A stopped sandbox is started first.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			m, err := newOpenShellManager()
			if err != nil {
				return err
			}
			return m.Connect(cmd.Context(), args[0], openshell.StartOptions{
				SkipPermissions: !noSkipPermissions,
				Agent:           agent,
			})
		},
	}
	cmd.Flags().BoolVar(&noSkipPermissions, "no-skip-permissions", false, "Disable --dangerously-skip-permissions")
	cmd.Flags().StringVar(&agent, "agent", "", agentFlagUsage)
	return cmd
}

func newOpenShellListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List claude-forge sandboxes",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			m, err := newOpenShellManager()
			if err != nil {
				return err
			}
			return m.List(cmd.Context(), cmd.OutOrStdout())
		},
	}
}

func newOpenShellStopCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "stop <name>",
		Short: "Stop a sandbox, keeping its workspace",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			m, err := newOpenShellManager()
			if err != nil {
				return err
			}
			return m.Stop(cmd.Context(), args[0])
		},
	}
}

func newOpenShellDeleteCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "delete <name>",
		Short: "Delete a sandbox and its workspace",
		Long:  "Delete sandbox <name>. Its workspace is removed too, so push any work first.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			m, err := newOpenShellManager()
			if err != nil {
				return err
			}
			return m.Delete(cmd.Context(), args[0])
		},
	}
}

func newOpenShellAuthCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "auth",
		Short: "Re-sync the Claude and GitHub credentials to OpenShell",
		Long: `Copy the current Claude and GitHub credentials to the OpenShell providers
sandboxes use. Running sandboxes pick up the new values within seconds.

start and connect already do this for Claude, so run it only to refresh
sandboxes you are attached to, e.g. after the host's Claude login refreshed
an expired access token, or after rotating the GitHub token.

  Claude: ANTHROPIC_API_KEY, CLAUDE_CODE_OAUTH_TOKEN, or ~/.claude/.credentials.json
  GitHub: GITHUB_TOKEN, GH_TOKEN, or 'gh auth token'

Sandboxes only see placeholders; OpenShell swaps in the real values on
requests to Anthropic and GitHub.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			m, err := newOpenShellManager()
			if err != nil {
				return err
			}
			return m.Auth(cmd.Context())
		},
	}
}

func newOpenShellPolicyCmd() *cobra.Command {
	var noGitHub bool
	cmd := &cobra.Command{
		Use:   "policy",
		Short: "Print the sandbox policy new sandboxes get",
		Long: `Print the OpenShell policy new sandboxes are created with.

By default this is the generated allowlist (Claude Code, GitHub, PyPI, npm,
AWS, GCP, Go modules and some documentation sites), extended by
openshell.policy in the claude-forge config: extra 'allow' and 'read_only'
hosts, and built-in groups to 'disable'. With openshell.policy_file set, that
file is printed and used verbatim instead.

A policy change only applies to sandboxes created afterwards. To open a host
in a running sandbox, run
'openshell policy update <sandbox> --add-endpoint <host>:443 --binary "/**" --wait'.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			m, err := newOpenShellManager()
			if err != nil {
				return err
			}
			data, err := m.PolicyYAML(!noGitHub)
			if err != nil {
				return err
			}
			_, err = cmd.OutOrStdout().Write(data)
			return err
		},
	}
	cmd.Flags().BoolVar(&noGitHub, "no-github-token", false, "Print the policy for sandboxes without a GitHub provider")
	return cmd
}

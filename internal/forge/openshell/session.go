// Package openshell runs claude-forge sessions in OpenShell sandboxes.
//
// Unlike the Docker runtime there is no gateway container, no git proxy and no
// host project mount: the sandbox keeps its own persistent workspace, the agent
// clones whatever repositories it needs into it, and network access comes from
// a generated OpenShell policy. Credentials are OpenShell providers, so the
// sandbox only ever sees placeholders that OpenShell swaps for the real values
// on requests to the declared hosts.
package openshell

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"regexp"
	"strconv"
	"strings"

	"github.com/michael-freling/claude-forge/internal/forge/auth"
	"github.com/michael-freling/claude-forge/internal/forge/config"
	"github.com/michael-freling/claude-forge/internal/forge/layout"
	"github.com/michael-freling/claude-forge/internal/forge/openshell/spec"
)

// ClaudeProviderName and GitHubProviderName are the provider instances
// (holding the credentials) every claude-forge sandbox attaches.
const (
	ClaudeProviderName = "claude-forge-claude"
	GitHubProviderName = "claude-forge-github"
)

// Manager runs claude-forge sessions in OpenShell sandboxes.
type Manager struct {
	Client *Client
	Config *config.Config

	ClaudeDir string // host ~/.claude
	ConfigDir string // ~/.config/claude-forge
	HomeDir   string // host home directory

	Out io.Writer

	// ResolveClaude resolves the host's Claude credentials (auth.Resolve on
	// ClaudeDir by default).
	ResolveClaude func() (*auth.Credentials, error)
	// LookupEnv reads the host environment (os.LookupEnv by default).
	LookupEnv func(string) (string, bool)
	// GitHubCLIToken returns `gh auth token`, used when no GitHub token is in
	// the environment. Nil disables the fallback.
	GitHubCLIToken func(context.Context) (string, error)
	// GitIdentity returns the host's git user.name and user.email.
	GitIdentity func() (name, email string)
	// ClaudeModel returns the host's Claude Code model setting ("" for none).
	ClaudeModel func() string
	// GitOrigin returns the origin remote URL of a host git checkout (git
	// remote get-url by default). It resolves plugin marketplaces the host
	// added from a local directory.
	GitOrigin func(dir string) (string, error)
}

func (m *Manager) logf(format string, args ...any) {
	if m.Out != nil {
		fmt.Fprintf(m.Out, format+"\n", args...)
	}
}

func (m *Manager) env(key string) string {
	lookup := m.LookupEnv
	if lookup == nil {
		lookup = os.LookupEnv
	}
	v, _ := lookup(key)
	return strings.TrimSpace(v)
}

// sandboxNamePattern is a DNS label: OpenShell names sandboxes after it.
var sandboxNamePattern = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?$`)

// ValidateName checks a sandbox name.
func ValidateName(name string) error {
	if !sandboxNamePattern.MatchString(name) {
		return fmt.Errorf("invalid sandbox name %q: use lowercase letters, digits and '-' (max 63 characters)", name)
	}
	return nil
}

// resolveClaude resolves the Claude login claude-forge already uses for
// Docker sessions: ANTHROPIC_API_KEY, CLAUDE_CODE_OAUTH_TOKEN, or the host's
// ~/.claude/.credentials.json.
func (m *Manager) resolveClaude() (*auth.Credentials, error) {
	resolve := m.ResolveClaude
	if resolve == nil {
		resolve = func() (*auth.Credentials, error) { return auth.Resolve(m.ClaudeDir) }
	}
	creds, err := resolve()
	if err != nil {
		return nil, fmt.Errorf("failed to resolve Claude credentials (log in with 'claude' on the host): %w", err)
	}
	return creds, nil
}

// ErrRemoteControlNeedsLogin is returned for --remote-control when the host's
// Claude credential is not a full 'claude' login.
var ErrRemoteControlNeedsLogin = errors.New("--remote-control needs the host's 'claude' login (~/.claude/.credentials.json): " +
	"ANTHROPIC_API_KEY and CLAUDE_CODE_OAUTH_TOKEN ('claude setup-token') are inference-only; unset them and log in with 'claude'")

// checkRemoteControl fails when Remote Control is requested without a full
// login, before anything is created or started.
func checkRemoteControl(opts StartOptions, creds *auth.Credentials) error {
	if opts.RemoteControl && !creds.FullLogin() {
		return ErrRemoteControlNeedsLogin
	}
	return nil
}

// providerCredential returns the Claude provider's env key and value.
func providerCredential(creds *auth.Credentials) (key, value string) {
	if creds.AuthType == "api_key" {
		return spec.EnvAnthropicAPIKey, creds.Token
	}
	return spec.EnvClaudeOAuthToken, creds.Token
}

// syncClaude copies the host's current Claude credential into the Claude
// provider. It runs on every start and connect: a login from
// ~/.claude/.credentials.json is a short-lived access token that the host's
// Claude Code refreshes, and only the access token (never the refresh token)
// is handed to OpenShell.
func (m *Manager) syncClaude(ctx context.Context, creds *auth.Credentials) error {
	if err := m.Client.EnsureProfile(ctx, spec.ClaudeProfile()); err != nil {
		return err
	}
	key, value := providerCredential(creds)
	return m.Client.SetProvider(ctx, ClaudeProviderName, spec.ClaudeProfileID, key, value)
}

// githubToken returns a GitHub token from the environment or the gh CLI.
func (m *Manager) githubToken(ctx context.Context) string {
	for _, k := range []string{spec.EnvGitHubToken, "GH_TOKEN"} {
		if v := m.env(k); v != "" {
			return v
		}
	}
	if m.GitHubCLIToken != nil {
		if v, err := m.GitHubCLIToken(ctx); err == nil {
			return strings.TrimSpace(v)
		}
	}
	return ""
}

// syncGitHub makes sure the GitHub provider exists and reports whether it
// does. With refresh, the GitHub token is re-read from the host even when the
// provider already exists.
func (m *Manager) syncGitHub(ctx context.Context, refresh bool) (bool, error) {
	if err := m.Client.EnsureProfile(ctx, spec.GitHubProfile(m.Config.OpenShell.GitHub.PushRepos)); err != nil {
		return false, err
	}
	if !refresh && m.Client.ProviderExists(ctx, GitHubProviderName) {
		return true, nil
	}
	token := m.githubToken(ctx)
	if token == "" {
		m.logf("No GitHub token (GITHUB_TOKEN, GH_TOKEN or 'gh auth token'): only public repositories can be cloned")
		return false, nil
	}
	if err := m.Client.SetProvider(ctx, GitHubProviderName, spec.GitHubProfileID, spec.EnvGitHubToken, token); err != nil {
		return false, err
	}
	return true, nil
}

// Auth re-syncs the Claude and GitHub providers from the host's credentials.
func (m *Manager) Auth(ctx context.Context) error {
	creds, err := m.resolveClaude()
	if err != nil {
		return err
	}
	if err := m.syncClaude(ctx, creds); err != nil {
		return err
	}
	if _, err := m.syncGitHub(ctx, true); err != nil {
		return err
	}
	m.logf("Synced Claude and GitHub credentials to OpenShell providers %s and %s", ClaudeProviderName, GitHubProviderName)
	return nil
}

// PolicyYAML returns the policy a new sandbox gets: openshell.policy_file
// verbatim, or the generated policy with the openshell.policy extensions.
//
// A policy file is only checked for provider-owned hosts on a best-effort
// basis, with warnings: OpenShell itself rejects a sandbox whose policy
// disagrees with a provider on inspecting a host.
func (m *Manager) PolicyYAML(github bool) ([]byte, error) {
	cfg := m.Config.OpenShell
	if cfg.PolicyFile == "" {
		p, err := spec.BuildPolicy(spec.PolicyOptions{GitHubProvider: github, Custom: cfg.Policy})
		if err != nil {
			return nil, err
		}
		return p.Render()
	}

	data, err := os.ReadFile(cfg.PolicyFile)
	if err != nil {
		return nil, fmt.Errorf("failed to read openshell.policy_file: %w", err)
	}
	warnings, err := spec.ProviderOverlaps(data)
	if err != nil {
		m.logf("Warning: could not check openshell.policy_file %s for provider-owned hosts: %v", cfg.PolicyFile, err)
	}
	for _, w := range warnings {
		m.logf("Warning: openshell.policy_file %s: %s", cfg.PolicyFile, w)
	}
	return data, nil
}

// networkSummary describes the sandbox's network access for the agent.
func (m *Manager) networkSummary() string {
	if m.Config.OpenShell.PolicyFile != "" {
		return "The user's own sandbox policy decides which hosts are reachable; most are not."
	}
	return spec.DescribeNetwork(m.Config.OpenShell.Policy)
}

// StartOptions configures a new session.
type StartOptions struct {
	Name            string
	Prompt          string
	SkipPermissions bool
	Agent           string
	// Repos (owner/name) are cloned in addition to openshell.repositories.
	Repos []string
	// RemoteControl starts Claude Code with Remote Control, named after the
	// sandbox. It needs a full 'claude' login on the host.
	RemoteControl bool
}

// Start creates a sandbox, clones the repositories and installs the plugins
// it is set up with, and runs Claude Code in it.
func (m *Manager) Start(ctx context.Context, opts StartOptions) error {
	if err := ValidateName(opts.Name); err != nil {
		return err
	}
	repos, err := m.repositories(opts.Repos)
	if err != nil {
		return err
	}
	if _, err := m.Client.GetSandbox(ctx, opts.Name); err == nil {
		return fmt.Errorf("sandbox %q already exists: run 'claude-forge openshell connect %s'", opts.Name, opts.Name)
	} else if !errors.Is(err, ErrNotFound) {
		return err
	}

	creds, err := m.resolveClaude()
	if err != nil {
		return err
	}
	if err := checkRemoteControl(opts, creds); err != nil {
		return err
	}
	if err := m.syncClaude(ctx, creds); err != nil {
		return err
	}
	providers := []string{ClaudeProviderName}
	github, err := m.syncGitHub(ctx, false)
	if err != nil {
		return err
	}
	if github {
		providers = append(providers, GitHubProviderName)
	}
	policy, err := m.PolicyYAML(github)
	if err != nil {
		return err
	}

	image := m.Config.OpenShell.Image
	if image == "" {
		image = m.Config.Images.Agent
	}
	m.logf("Creating sandbox %s from %s", opts.Name, image)
	if err := m.Client.CreateSandbox(ctx, CreateOptions{
		Name:      opts.Name,
		Image:     image,
		Policy:    policy,
		Providers: providers,
	}); err != nil {
		return err
	}

	if err := m.uploadHome(ctx, opts.Name, true, github, creds); err != nil {
		return err
	}
	m.cloneRepos(ctx, opts.Name, repos)
	m.installPlugins(ctx, opts.Name)
	return m.runClaude(ctx, opts.Name, false, creds, opts)
}

// Connect runs Claude Code again in an existing sandbox, continuing its most
// recent conversation, and starts the sandbox first if it is stopped.
func (m *Manager) Connect(ctx context.Context, name string, opts StartOptions) error {
	sb, err := m.Client.GetSandbox(ctx, name)
	if err != nil {
		return err
	}
	creds, err := m.resolveClaude()
	if err != nil {
		return err
	}
	if err := checkRemoteControl(opts, creds); err != nil {
		return err
	}
	switch sb.Phase {
	case "Ready":
	case "Stopped":
		m.logf("Starting sandbox %s", name)
		if err := m.Client.Start(ctx, name); err != nil {
			return err
		}
	default:
		return fmt.Errorf("sandbox %q is %s and cannot be reconnected: delete it and start a new one", name, sb.Phase)
	}

	if err := m.syncClaude(ctx, creds); err != nil {
		return err
	}
	github := m.Client.ProviderExists(ctx, GitHubProviderName)
	if err := m.uploadHome(ctx, name, false, github, creds); err != nil {
		return err
	}
	return m.runClaude(ctx, name, true, creds, opts)
}

// uploadHome refreshes the sandbox's home directory files. With a full login
// it also writes the sandbox's Claude login (see loginScript).
func (m *Manager) uploadHome(ctx context.Context, name string, firstRun, github bool, creds *auth.Credentials) error {
	stage, err := os.MkdirTemp(m.Client.TempDir, "claude-forge-home-*")
	if err != nil {
		return fmt.Errorf("failed to create staging directory: %w", err)
	}
	defer os.RemoveAll(stage)

	opts := HomeOptions{
		ClaudeDir:   m.ClaudeDir,
		ConfigDir:   m.ConfigDir,
		HomeDir:     m.HomeDir,
		FirstRun:    firstRun,
		GitHubToken: github,
		PushRepos:   m.Config.OpenShell.GitHub.PushRepos,
		Network:     m.networkSummary(),
		FullLogin:   creds.FullLogin(),
	}
	if m.GitIdentity != nil {
		opts.GitUserName, opts.GitUserEmail = m.GitIdentity()
	}
	if err := StageHome(stage, opts); err != nil {
		return err
	}
	if err := m.Client.uploadHome(ctx, name, stage, layout.Home); err != nil {
		return err
	}
	claudeDir := path.Join(layout.Home, ".claude")
	if _, err := m.Client.Run(ctx, name, "", nil, "bash", "-c", mergeSettingsScript, "bash",
		path.Join(claudeDir, stagedSettingsFile), path.Join(claudeDir, "settings.json")); err != nil {
		return fmt.Errorf("failed to update settings.json in sandbox %s: %w", name, err)
	}
	if !creds.FullLogin() {
		return nil
	}
	return m.writeLogin(ctx, name, creds)
}

// writeLogin writes the sandbox's Claude login, which Remote Control needs:
// a token from CLAUDE_CODE_OAUTH_TOKEN is inference-only, but the same token
// in ~/.claude/.credentials.json is a full login. It runs on every start and
// connect, since the provider's placeholder can change.
func (m *Manager) writeLogin(ctx context.Context, name string, creds *auth.Credentials) error {
	scopes, err := json.Marshal(creds.Scopes)
	if err != nil {
		return fmt.Errorf("failed to marshal the Claude login scopes: %w", err)
	}
	if _, err := m.Client.Run(ctx, name, "", nil, "bash", "-c", loginScript, "bash",
		path.Join(layout.Home, ".claude", stagedOAuthAccountFile),
		path.Join(layout.Home, ".claude.json"),
		path.Join(layout.Home, ".claude", ".credentials.json"),
		string(scopes), creds.SubscriptionType); err != nil {
		return fmt.Errorf("failed to write the Claude login in sandbox %s: %w", name, err)
	}
	return nil
}

// loginScript merges the staged oauthAccount ($1, when there is one) into
// ~/.claude.json ($2) and writes ~/.claude/.credentials.json ($3) with the
// host login's scopes ($4, JSON) and subscription type ($5).
//
// The access token is the Claude provider's placeholder, read from the
// sandbox's own environment, so the real token never leaves the host and
// never appears in a command line. There is no refresh token, and the far
// future expiry (2100-01-01) keeps Claude Code from trying to refresh: the
// host's claude-forge re-syncs the provider when the real token changes.
const loginScript = `set -e
if [ -f "$1" ]; then
  if [ -f "$2" ]; then
    jq --slurpfile account "$1" '.oauthAccount = $account[0]' "$2" > "$2.claude-forge"
  else
    jq -n --slurpfile account "$1" '{oauthAccount: $account[0]}' > "$2.claude-forge"
  fi
  mv "$2.claude-forge" "$2"
  rm "$1"
fi
if [ -z "$CLAUDE_CODE_OAUTH_TOKEN" ]; then
  echo "CLAUDE_CODE_OAUTH_TOKEN is not set in the sandbox" >&2
  exit 1
fi
umask 077
mkdir -p "$(dirname "$3")"
jq -n --arg token "$CLAUDE_CODE_OAUTH_TOKEN" --argjson scopes "$4" --arg subscription "$5" \
  '{claudeAiOauth: {accessToken: $token, refreshToken: null, expiresAt: 4102444800000, scopes: $scopes,
    subscriptionType: (if $subscription == "" then null else $subscription end)}}' > "$3.claude-forge"
mv "$3.claude-forge" "$3"`

// mergeSettingsScript replaces the sandbox's settings.json ($2) with the
// staged forge settings ($1), keeping the keys `claude plugin` manages in
// the sandbox, so reconnecting does not disable the installed plugins.
const mergeSettingsScript = `set -e
if [ -f "$2" ]; then
  jq -s '.[0] + (.[1] | {enabledPlugins, extraKnownMarketplaces} | with_entries(select(.value != null)))' "$1" "$2" > "$1.merged"
  mv "$1.merged" "$2"
  rm "$1"
else
  mv "$1" "$2"
fi`

// claudeArgs builds the Claude Code command line.
func (m *Manager) claudeArgs(opts StartOptions) []string {
	var args []string
	if opts.SkipPermissions {
		args = append(args, "--dangerously-skip-permissions")
	}
	agent := opts.Agent
	if agent == "" {
		agent = m.Config.Defaults.Agent
	}
	if agent != "" {
		args = append(args, "--agent", agent)
	}
	if opts.Name != "" {
		args = append(args, "--name", opts.Name)
	}
	if opts.Prompt != "" {
		args = append(args, opts.Prompt)
	}
	return args
}

// continueFallbackSeconds is how soon claude --continue must fail for connect
// to start a new conversation instead. With nothing to resume it exits 1
// within about a second; the margin covers a slow sandbox, and a resumed
// conversation that ends with an error after this long is real work that must
// not be replaced by a new session.
const continueFallbackSeconds = 10

// continueScript continues the most recent conversation in the workspace, or
// starts a new one when there is none to continue.
var continueScript = continueScriptWithin(continueFallbackSeconds)

// ContinueScript returns the bash script connect runs in a sandbox: its
// positional parameters are the claude arguments.
func ContinueScript() string {
	return continueScript
}

// continueScriptWithin returns the continue script with a fallback window of
// fallbackSeconds. Transcripts alone do not mean there is a conversation to
// continue: claude -p, claude remote-control, and sessions quit before the
// first message leave ones that claude --continue cannot resume, and it exits
// with "No conversation found to continue". So a --continue that fails within
// the window falls back to a new conversation, in the same shell so the
// terminal stays attached.
func continueScriptWithin(fallbackSeconds int) string {
	return `if ls ~/.claude/projects/` + layout.SessionSubdir + `/*.jsonl >/dev/null 2>&1; then ` +
		`start=$SECONDS; claude --continue "$@"; status=$?; ` +
		`if [ "$status" -eq 0 ] || [ $((SECONDS - start)) -ge ` + strconv.Itoa(fallbackSeconds) + ` ]; then exit "$status"; fi; ` +
		`fi; exec claude "$@"`
}

// sandboxEnv is the environment of the commands claude-forge runs in a
// sandbox. Values show up in the host's process list: never put secrets here.
func sandboxEnv() map[string]string {
	return map[string]string{
		// sandbox exec sets HOME to the working directory; Claude Code's
		// config, the uploaded files and .gitconfig live in the real home.
		"HOME":                    layout.Home,
		"DISABLE_AUTOUPDATER":     "1",
		"DISABLE_TELEMETRY":       "1",
		"DISABLE_ERROR_REPORTING": "1",
		"GIT_TERMINAL_PROMPT":     "0",
	}
}

func (m *Manager) runClaude(ctx context.Context, name string, resume bool, creds *auth.Credentials, opts StartOptions) error {
	env := sandboxEnv()
	if m.ClaudeModel != nil {
		if model := m.ClaudeModel(); model != "" {
			env["ANTHROPIC_MODEL"] = model
		}
	}

	args := m.claudeArgs(opts)
	if opts.RemoteControl {
		args = append([]string{"--remote-control", name}, args...)
	}
	command := append([]string{"claude"}, args...)
	if resume {
		command = append([]string{"bash", "-c", continueScript, "claude"}, args...)
	}
	if creds.FullLogin() {
		// Claude Code treats a token in CLAUDE_CODE_OAUTH_TOKEN as
		// inference-only; without it, the full login writeLogin wrote is used.
		command = append([]string{"env", "-u", spec.EnvClaudeOAuthToken}, command...)
	}
	err := m.Client.Exec(ctx, name, layout.Workspace, env, command...)
	m.logf("\nSandbox %s keeps running. Reconnect: claude-forge openshell connect %s | stop: claude-forge openshell stop %s", name, name, name)
	return err
}

// List writes the claude-forge sandboxes to w.
func (m *Manager) List(ctx context.Context, w io.Writer) error {
	sandboxes, err := m.Client.ListSandboxes(ctx)
	if err != nil {
		return err
	}
	if len(sandboxes) == 0 {
		fmt.Fprintln(w, "No claude-forge sandboxes.")
		return nil
	}
	fmt.Fprintf(w, "%-30s %-12s %s\n", "NAME", "PHASE", "CREATED")
	for _, s := range sandboxes {
		fmt.Fprintf(w, "%-30s %-12s %s\n", s.Name, s.Phase, s.CreatedAt)
	}
	return nil
}

// Stop stops a claude-forge sandbox, keeping its workspace.
func (m *Manager) Stop(ctx context.Context, name string) error {
	if _, err := m.Client.GetSandbox(ctx, name); err != nil {
		return err
	}
	return m.Client.Stop(ctx, name)
}

// Delete deletes a claude-forge sandbox and its workspace.
func (m *Manager) Delete(ctx context.Context, name string) error {
	if _, err := m.Client.GetSandbox(ctx, name); err != nil {
		return err
	}
	return m.Client.Delete(ctx, name)
}

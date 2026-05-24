package claudecode

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// ContainerConfig holds all Claude Code configuration needed for the agent container.
type ContainerConfig struct {
	Env       map[string]string // Environment variables for the container
	Cmd       []string          // Arguments to pass to the claude entrypoint
	Mounts    []MountConfig     // Volume mounts for Claude Code files
	Gitconfig string            // Generated gitconfig content
}

// MountConfig represents a bind mount for the container.
type MountConfig struct {
	Source   string
	Target   string
	ReadOnly bool
}

// Options holds user-provided options for configuring Claude Code.
type Options struct {
	SkipPermissions bool   // Pass --dangerously-skip-permissions (default true)
	Worktree        bool   // Pass --worktree to Claude Code
	Prompt          string // Pass -p "<prompt>" to Claude Code
	Resume          string // Pass --resume <session-id> to Claude Code
	Continue        bool   // Pass --continue to Claude Code (resume most recent)
	Model           string // Claude Code model override (e.g. "claude-opus-4-6")

	// Paths
	ProjectDir string // Host path to the project directory
	HomeDir    string // Host home directory (for ~/CLAUDE.md, ~/.claude/, etc.)
	ConfigDir  string // Path to ~/.config/claude-forge/
	SessionDir string // Path to session storage dir (e.g. ~/.claude-forge/<project-id>/)

	// Auth
	AuthToken string // Claude Code auth token (ANTHROPIC_API_KEY or CLAUDE_CODE_OAUTH_TOKEN)
	AuthType  string // "api_key" or "oauth"

	// Project
	ProjectID string // Project identifier (e.g. "-work")
	Owner     string // GitHub repo owner
	Repo      string // GitHub repo name

	// Git identity (from host git config)
	GitUserName  string
	GitUserEmail string

	// Host user identity (for file ownership mapping)
	UID int // host user UID
	GID int // host user GID
}

// BuildContainerConfig constructs the full container configuration from options.
// This is the main entry point -- it generates env vars, command args, volume mounts,
// and gitconfig content that the Docker client needs to start the agent container.
func BuildContainerConfig(opts Options) (*ContainerConfig, error) {
	if err := validate(opts); err != nil {
		return nil, err
	}

	env := buildEnv(opts)
	cmd := buildCmd(opts)
	mounts := buildMounts(opts)
	gitconfig := generateGitconfig(opts)

	return &ContainerConfig{
		Env:       env,
		Cmd:       cmd,
		Mounts:    mounts,
		Gitconfig: gitconfig,
	}, nil
}

// validate checks that required fields are set and values are valid.
func validate(opts Options) error {
	if opts.ProjectDir == "" {
		return fmt.Errorf("project directory is required")
	}
	if opts.AuthToken == "" {
		return fmt.Errorf("auth token is required")
	}
	if opts.AuthType != "api_key" && opts.AuthType != "oauth" {
		return fmt.Errorf("auth type must be \"api_key\" or \"oauth\", got %q", opts.AuthType)
	}
	return nil
}

// buildEnv constructs the environment variable map for the container.
// OAuth tokens are not set here — Claude Code reads ~/.claude/.credentials.json
// (bind-mounted into the container) so it can refresh tokens autonomously.
func buildEnv(opts Options) map[string]string {
	env := map[string]string{
		"HOME":                "/home/user",
		"GIT_TERMINAL_PROMPT": "0",
	}
	if opts.AuthType == "api_key" {
		env["ANTHROPIC_API_KEY"] = opts.AuthToken
	}
	if opts.UID > 0 {
		env["FORGE_UID"] = fmt.Sprintf("%d", opts.UID)
	}
	if opts.GID > 0 {
		env["FORGE_GID"] = fmt.Sprintf("%d", opts.GID)
	}
	if opts.Model != "" {
		env["ANTHROPIC_MODEL"] = opts.Model
	}
	return env
}

// buildCmd constructs the command arguments for the claude entrypoint.
func buildCmd(opts Options) []string {
	var cmd []string
	if opts.SkipPermissions {
		cmd = append(cmd, "--dangerously-skip-permissions")
	}
	if opts.Worktree {
		cmd = append(cmd, "--worktree")
	}
	if opts.Resume != "" {
		cmd = append(cmd, "--resume", opts.Resume)
	} else if opts.Continue {
		cmd = append(cmd, "--continue")
	}
	if opts.Prompt != "" {
		cmd = append(cmd, "-p", opts.Prompt)
	}
	return cmd
}

// buildMounts constructs the list of volume mounts for the container.
// Conditional mounts are only included if the source path exists on the host.
func buildMounts(opts Options) []MountConfig {
	mounts := []MountConfig{
		{
			Source: opts.ProjectDir,
			Target: "/work",
		},
	}

	// Session directory
	if opts.SessionDir != "" && pathExists(opts.SessionDir) {
		mounts = append(mounts, MountConfig{
			Source: opts.SessionDir,
			Target: "/home/user/.claude/projects/" + opts.ProjectID + "/",
		})
	}

	// Home CLAUDE.md (read-only)
	if opts.HomeDir != "" {
		homeClaude := filepath.Join(opts.HomeDir, "CLAUDE.md")
		if pathExists(homeClaude) {
			mounts = append(mounts, MountConfig{
				Source:   homeClaude,
				Target:   "/home/user/CLAUDE.md",
				ReadOnly: true,
			})
		}
	}

	// ~/.claude/CLAUDE.md (read-only)
	if opts.HomeDir != "" {
		dotClaudeMD := filepath.Join(opts.HomeDir, ".claude", "CLAUDE.md")
		if pathExists(dotClaudeMD) {
			mounts = append(mounts, MountConfig{
				Source:   dotClaudeMD,
				Target:   "/home/user/.claude/CLAUDE.md",
				ReadOnly: true,
			})
		}
	}

	// ~/.claude/ subdirectories (read-only)
	claudeSubdirs := []string{"rules", "agents", "commands", "skills", "plugins"}
	if opts.HomeDir != "" {
		for _, subdir := range claudeSubdirs {
			source := filepath.Join(opts.HomeDir, ".claude", subdir)
			if pathExists(source) {
				mounts = append(mounts, MountConfig{
					Source:   source,
					Target:   "/home/user/.claude/" + subdir + "/",
					ReadOnly: true,
				})
			}
		}
	}

	// ~/.claude/.credentials.json (read-write so Claude Code can refresh OAuth tokens)
	if opts.HomeDir != "" {
		credPath := filepath.Join(opts.HomeDir, ".claude", ".credentials.json")
		if pathExists(credPath) {
			mounts = append(mounts, MountConfig{
				Source: credPath,
				Target: "/home/user/.claude/.credentials.json",
			})
		}
	}

	// Config dir settings.json (read-only)
	if opts.ConfigDir != "" {
		settingsPath := filepath.Join(opts.ConfigDir, "settings.json")
		if pathExists(settingsPath) {
			mounts = append(mounts, MountConfig{
				Source:   settingsPath,
				Target:   "/home/user/.claude/settings.json",
				ReadOnly: true,
			})
		}
	}

	// Config dir gitconfig (read-only)
	if opts.ConfigDir != "" {
		gitconfigPath := filepath.Join(opts.ConfigDir, "gitconfig")
		if pathExists(gitconfigPath) {
			mounts = append(mounts, MountConfig{
				Source:   gitconfigPath,
				Target:   "/home/user/.gitconfig",
				ReadOnly: true,
			})
		}
	}

	return mounts
}

// generateGitconfig produces gitconfig content that routes GitHub traffic
// through the gateway reverse proxy and sets the git user identity.
// Uses url.insteadOf to rewrite https://github.com/ URLs to plain HTTP
// requests to the gateway, avoiding CONNECT tunneling.
//
// worktree.useRelativePaths makes git worktree add (including the one Claude
// Code runs for --worktree) emit a .git file whose gitdir is relative — so the
// worktree resolves correctly both at /work in the container and at the host
// project path. Requires git 2.48+ in the agent image.
func generateGitconfig(opts Options) string {
	return fmt.Sprintf(`[url "http://gateway:8080/github.com/"]
    insteadOf = https://github.com/

[user]
    name = %s
    email = %s

[worktree]
    useRelativePaths = true
`, opts.GitUserName, opts.GitUserEmail)
}

// WriteGitconfig writes the generated gitconfig to the config directory.
// Creates the config directory if it doesn't exist.
func WriteGitconfig(configDir string, opts Options) error {
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		return fmt.Errorf("failed to create config directory: %w", err)
	}

	content := generateGitconfig(opts)
	gitconfigPath := filepath.Join(configDir, "gitconfig")
	if err := os.WriteFile(gitconfigPath, []byte(content), 0o644); err != nil {
		return fmt.Errorf("failed to write gitconfig: %w", err)
	}

	return nil
}

// DefaultSettings returns the default Claude Code settings JSON content.
func DefaultSettings() string {
	return `{
  "autoUpdaterStatus": "disabled",
  "skipDangerousModePermissionPrompt": true
}`
}

// EnsureSettings writes settings.json to the config directory if it doesn't exist.
// Creates the config directory if it doesn't exist.
func EnsureSettings(configDir string) error {
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		return fmt.Errorf("failed to create config directory: %w", err)
	}

	settingsPath := filepath.Join(configDir, "settings.json")
	if pathExists(settingsPath) {
		return nil
	}

	if err := os.WriteFile(settingsPath, []byte(DefaultSettings()), 0o644); err != nil {
		return fmt.Errorf("failed to write settings.json: %w", err)
	}

	return nil
}

// EnsureUserConfig writes .claude.json to the config directory if it doesn't exist.
// It reads the theme from the host's ~/.claude.json so the container matches the user's preference.
// This is mounted into the container at ~/.claude.json to skip onboarding.
func EnsureUserConfig(configDir, homeDir string) error {
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		return fmt.Errorf("failed to create config directory: %w", err)
	}

	configPath := filepath.Join(configDir, ".claude.json")
	if pathExists(configPath) {
		return nil
	}

	theme := readHostTheme(homeDir)

	config := map[string]any{
		"hasCompletedOnboarding": true,
		"theme":                  theme,
		"projects": map[string]any{
			"/work": map[string]any{
				"hasTrustDialogAccepted": true,
			},
		},
	}
	data, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal .claude.json: %w", err)
	}

	if err := os.WriteFile(configPath, append(data, '\n'), 0o644); err != nil {
		return fmt.Errorf("failed to write .claude.json: %w", err)
	}

	return nil
}

// readHostTheme reads the theme from the host's ~/.claude.json.
// Returns "dark" as the default if the file doesn't exist or can't be parsed.
func readHostTheme(homeDir string) string {
	hostConfig := filepath.Join(homeDir, ".claude.json")
	data, err := os.ReadFile(hostConfig)
	if err != nil {
		return "dark"
	}

	var parsed map[string]any
	if err := json.Unmarshal(data, &parsed); err != nil {
		return "dark"
	}

	if theme, ok := parsed["theme"].(string); ok && theme != "" {
		return theme
	}
	return "dark"
}

// ReadHostModel reads the model from the host's ~/.claude/settings.json.
// Returns an empty string if the file doesn't exist, can't be parsed, or the model isn't set.
func ReadHostModel(claudeDir string) string {
	settingsPath := filepath.Join(claudeDir, "settings.json")
	data, err := os.ReadFile(settingsPath)
	if err != nil {
		return ""
	}

	var parsed map[string]any
	if err := json.Unmarshal(data, &parsed); err != nil {
		return ""
	}

	if model, ok := parsed["model"].(string); ok && model != "" {
		return model
	}
	return ""
}

// CacheDir represents a host dependency cache directory to mount into the container.
type CacheDir struct {
	Source string // host path
	Target string // container path
}

// DetectCacheDirs finds dependency cache directories that should be mounted
// into the container for faster package installation.
//
// Go caches are shared from the host (Go handles version differences safely).
// npm/pnpm/pip caches use per-forge directories under ~/.claude-forge/caches/
// to avoid version conflicts with host tools.
func DetectCacheDirs(homeDir string) []CacheDir {
	var result []CacheDir

	// Go caches: safe to share with host (version-independent)
	hostCaches := []CacheDir{
		{filepath.Join(homeDir, "go", "pkg", "mod"), "/home/user/go/pkg/mod"},
		{filepath.Join(homeDir, ".cache", "go-build"), "/home/user/.cache/go-build"},
	}
	for _, c := range hostCaches {
		if pathExists(c.Source) {
			result = append(result, c)
		}
	}

	// npm/pnpm/pip: use per-forge caches to avoid version conflicts with host.
	// These dirs are always created (not conditional on host existence).
	forgeCacheBase := filepath.Join(homeDir, ".claude-forge", "caches")
	forgeCaches := []CacheDir{
		{filepath.Join(forgeCacheBase, "npm"), "/home/user/.npm"},
		{filepath.Join(forgeCacheBase, "pnpm"), "/home/user/.local/share/pnpm/store"},
		{filepath.Join(forgeCacheBase, "pip"), "/home/user/.cache/pip"},
	}
	for _, c := range forgeCaches {
		_ = os.MkdirAll(c.Source, 0o755)
		result = append(result, c)
	}

	return result
}

// MCPServerConfig represents an MCP server entry in settings.json.
type MCPServerConfig struct {
	Type string `json:"type"`
	URL  string `json:"url"`
}

// UpdateMCPServers reads settings.json from configDir, merges the given
// mcpServers map, and writes back. Creates the file with defaults if missing.
func UpdateMCPServers(configDir string, servers map[string]MCPServerConfig) error {
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		return fmt.Errorf("failed to create config directory: %w", err)
	}

	settingsPath := filepath.Join(configDir, "settings.json")
	data, err := os.ReadFile(settingsPath)
	if err != nil {
		if !os.IsNotExist(err) {
			return fmt.Errorf("failed to read settings.json: %w", err)
		}
		data = []byte(DefaultSettings())
	}

	var settings map[string]any
	if err := json.Unmarshal(data, &settings); err != nil {
		return fmt.Errorf("failed to parse settings.json: %w", err)
	}

	mcpServers := make(map[string]any)
	if existing, ok := settings["mcpServers"].(map[string]any); ok {
		mcpServers = existing
	}
	for name, cfg := range servers {
		mcpServers[name] = map[string]any{
			"type": cfg.Type,
			"url":  cfg.URL,
		}
	}
	settings["mcpServers"] = mcpServers

	out, err := json.MarshalIndent(settings, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal settings.json: %w", err)
	}
	return os.WriteFile(settingsPath, append(out, '\n'), 0o644)
}

// pathExists returns true if the given path exists on the filesystem.
func pathExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

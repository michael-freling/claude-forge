package openshell

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"unicode"

	"github.com/michael-freling/claude-forge/internal/forge/claudecode"
	"github.com/michael-freling/claude-forge/internal/forge/layout"
)

// HomeOptions configures the files claude-forge copies into a sandbox's home.
type HomeOptions struct {
	ClaudeDir string // host ~/.claude: CLAUDE.md, rules, agents, commands, skills
	ConfigDir string // ~/.config/claude-forge: settings.json
	HomeDir   string // host home: theme from ~/.claude.json

	// FirstRun also writes the files Claude Code and git own at runtime
	// (~/.claude.json, ~/.gitconfig). They are only seeded when the sandbox is
	// created, so reconnecting does not discard the session's own changes.
	FirstRun bool

	GitUserName  string
	GitUserEmail string
	// GitHubToken configures git to send the GitHub provider's token
	// placeholder, which OpenShell swaps for the real token.
	GitHubToken bool
	// PushRepos is shown to the agent in its instructions.
	PushRepos []string
	// Network summarizes the reachable hosts in the agent's instructions.
	Network string
	// FullLogin stages the account and organization IDs of the host's
	// 'claude' login (see stagedOAuthAccountFile), which Remote Control needs.
	FullLogin bool
}

// claudeSubdirs are copied from the host's ~/.claude into the sandbox.
var claudeSubdirs = []string{"rules", "agents", "commands", "skills"}

// settingsDroppedKeys are claude-forge settings that only make sense for the
// Docker runtime: MCP servers reached on the session network, and plugins
// installed into the Docker plugins directory. A sandbox installs its own
// plugins instead (see Manager.installPlugins).
var settingsDroppedKeys = []string{"mcpServers", "enabledPlugins"}

// stagedSettingsFile is where StageHome writes the forge settings.json in
// .claude. The sandbox's own settings.json is never overwritten directly:
// mergeSettingsScript folds the staged file into it, so the plugins Claude Code
// installed in the sandbox stay enabled.
const stagedSettingsFile = "settings.claude-forge.json"

// stagedOAuthAccountFile is where StageHome writes, in .claude, the host's
// ~/.claude.json oauthAccount reduced to oauthAccountKeys. loginScript merges
// it into the sandbox's ~/.claude.json.
const stagedOAuthAccountFile = "oauth-account.claude-forge.json"

// oauthAccountKeys are the oauthAccount fields copied into a sandbox: the
// account and organization IDs and plan tiers Claude Code needs to check
// Remote Control eligibility. Personal fields (email address, names) are
// never copied.
var oauthAccountKeys = []string{
	"accountUuid",
	"organizationUuid",
	"organizationRole",
	"organizationType",
	"billingType",
	"workspaceRole",
	"organizationRateLimitTier",
	"userRateLimitTier",
	"seatTier",
}

// StageHome writes the home-directory files for a sandbox into stageDir,
// laid out as they should appear under the sandbox's home.
func StageHome(stageDir string, opts HomeOptions) error {
	claudeDir := filepath.Join(stageDir, ".claude")
	if err := os.MkdirAll(claudeDir, 0o755); err != nil {
		return fmt.Errorf("failed to create %s: %w", claudeDir, err)
	}

	settings, err := sandboxSettings(opts.ConfigDir)
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(claudeDir, stagedSettingsFile), settings, 0o644); err != nil {
		return fmt.Errorf("failed to write %s: %w", stagedSettingsFile, err)
	}

	if opts.FullLogin {
		if err := stageOAuthAccount(claudeDir, opts.HomeDir); err != nil {
			return err
		}
	}

	memory := Memory(opts.Network, opts.GitHubToken, opts.PushRepos)
	if hostMemory, err := os.ReadFile(filepath.Join(opts.ClaudeDir, "CLAUDE.md")); err == nil {
		memory += "\n" + string(hostMemory)
	}
	if err := os.WriteFile(filepath.Join(claudeDir, "CLAUDE.md"), []byte(memory), 0o644); err != nil {
		return fmt.Errorf("failed to write CLAUDE.md: %w", err)
	}

	for _, sub := range claudeSubdirs {
		src := filepath.Join(opts.ClaudeDir, sub)
		if _, err := os.Stat(src); err != nil {
			continue
		}
		if err := copyTree(src, filepath.Join(claudeDir, sub)); err != nil {
			return err
		}
	}

	if !opts.FirstRun {
		return nil
	}

	userConfig, err := json.MarshalIndent(map[string]any{
		"hasCompletedOnboarding": true,
		"theme":                  hostTheme(opts.HomeDir),
		"projects": map[string]any{
			layout.Workspace: map[string]any{"hasTrustDialogAccepted": true},
		},
	}, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal .claude.json: %w", err)
	}
	if err := os.WriteFile(filepath.Join(stageDir, ".claude.json"), append(userConfig, '\n'), 0o644); err != nil {
		return fmt.Errorf("failed to write .claude.json: %w", err)
	}

	if err := os.WriteFile(filepath.Join(stageDir, ".gitconfig"), []byte(Gitconfig(opts)), 0o644); err != nil {
		return fmt.Errorf("failed to write .gitconfig: %w", err)
	}
	return nil
}

// sandboxSettings returns the claude-forge settings.json without the
// Docker-only keys, or the defaults when there is none.
func sandboxSettings(configDir string) ([]byte, error) {
	settings := map[string]any{}
	data, err := os.ReadFile(filepath.Join(configDir, "settings.json"))
	if err != nil || json.Unmarshal(data, &settings) != nil || settings == nil {
		settings = map[string]any{}
		if err := json.Unmarshal([]byte(claudecode.DefaultSettings()), &settings); err != nil {
			return nil, fmt.Errorf("failed to parse default settings: %w", err)
		}
	}
	for _, k := range settingsDroppedKeys {
		delete(settings, k)
	}
	// Telemetry exporters configured for the Docker runtime point at a
	// collector on the session network, which a sandbox cannot reach.
	if env, ok := settings["env"].(map[string]any); ok {
		for k := range env {
			if strings.HasPrefix(k, "OTEL_") || strings.HasPrefix(k, "CLAUDE_CODE_ENABLE_TELEMETRY") || strings.HasPrefix(k, "CLAUDE_CODE_ENHANCED_TELEMETRY") {
				delete(env, k)
			}
		}
		if len(env) == 0 {
			delete(settings, "env")
		}
	}
	out, err := json.MarshalIndent(settings, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("failed to marshal settings.json: %w", err)
	}
	return append(out, '\n'), nil
}

// hostTheme reads the theme from the host's ~/.claude.json ("dark" if unset).
func hostTheme(homeDir string) string {
	var parsed map[string]any
	data, err := os.ReadFile(filepath.Join(homeDir, ".claude.json"))
	if err == nil && json.Unmarshal(data, &parsed) == nil {
		if theme, ok := parsed["theme"].(string); ok && theme != "" {
			return theme
		}
	}
	return "dark"
}

// hostOAuthAccount returns the oauthAccountKeys fields of the host's
// ~/.claude.json oauthAccount, or nil when there are none.
func hostOAuthAccount(homeDir string) map[string]any {
	var parsed struct {
		OAuthAccount map[string]any `json:"oauthAccount"`
	}
	data, err := os.ReadFile(filepath.Join(homeDir, ".claude.json"))
	if err != nil || json.Unmarshal(data, &parsed) != nil {
		return nil
	}
	account := map[string]any{}
	for _, k := range oauthAccountKeys {
		if v, ok := parsed.OAuthAccount[k]; ok {
			account[k] = v
		}
	}
	if len(account) == 0 {
		return nil
	}
	return account
}

// stageOAuthAccount writes the host's filtered oauthAccount to
// stagedOAuthAccountFile in claudeDir. Without one nothing is written, and
// the sandbox's ~/.claude.json keeps its own oauthAccount.
func stageOAuthAccount(claudeDir, homeDir string) error {
	account := hostOAuthAccount(homeDir)
	if account == nil {
		return nil
	}
	data, err := json.MarshalIndent(account, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal %s: %w", stagedOAuthAccountFile, err)
	}
	if err := os.WriteFile(filepath.Join(claudeDir, stagedOAuthAccountFile), append(data, '\n'), 0o644); err != nil {
		return fmt.Errorf("failed to write %s: %w", stagedOAuthAccountFile, err)
	}
	return nil
}

// Gitconfig returns the sandbox's ~/.gitconfig.
func Gitconfig(opts HomeOptions) string {
	var b strings.Builder
	if opts.GitUserName != "" || opts.GitUserEmail != "" {
		b.WriteString("[user]\n")
		if opts.GitUserName != "" {
			fmt.Fprintf(&b, "    name = %s\n", gitconfigQuote(opts.GitUserName))
		}
		if opts.GitUserEmail != "" {
			fmt.Fprintf(&b, "    email = %s\n", gitconfigQuote(opts.GitUserEmail))
		}
		b.WriteString("\n")
	}
	if opts.GitHubToken {
		// $GITHUB_TOKEN holds the provider's placeholder; git sends it inside
		// Basic auth and OpenShell swaps in the real token for github.com.
		b.WriteString(`[credential "https://github.com"]
    helper = "!f() { echo username=x-access-token; echo password=$GITHUB_TOKEN; }; f"

`)
	}
	b.WriteString("[init]\n    defaultBranch = main\n")
	return b.String()
}

// gitconfigQuote returns v as a git-config quoted string. Control characters
// are dropped, so a value cannot end the line and start a new section (such as
// [core] sshCommand), and backslashes and double quotes are escaped.
func gitconfigQuote(v string) string {
	var b strings.Builder
	b.WriteByte('"')
	for _, r := range v {
		switch {
		case unicode.IsControl(r):
			continue
		case r == '\\' || r == '"':
			b.WriteByte('\\')
		}
		b.WriteRune(r)
	}
	b.WriteByte('"')
	return b.String()
}

// copyTree copies a directory, following symlinks so the sandbox gets real
// files (rules and skills are often symlinked into a dotfiles repository).
// A symlink may point anywhere, including outside ~/.claude, and its target is
// copied. A symlinked directory that would re-enter a directory already being
// copied, such as a link to an ancestor, is skipped so a cycle cannot recurse
// forever.
func copyTree(src, dst string) error {
	root, err := filepath.EvalSymlinks(src)
	if err != nil {
		return fmt.Errorf("failed to resolve %s: %w", src, err)
	}
	return copyTreeFrom(root, dst, nil)
}

// copyTreeFrom copies the real directory root to dst. active holds the real
// roots of the copies that led here through symlinks.
func copyTreeFrom(root, dst string, active []string) error {
	active = append(active[:len(active):len(active)], root)
	return filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)

		if info.Mode()&os.ModeSymlink != 0 {
			resolved, err := filepath.EvalSymlinks(path)
			if err != nil {
				return nil // broken link: skip
			}
			ri, err := os.Stat(resolved)
			if err != nil {
				return nil
			}
			if !ri.IsDir() {
				return copyFile(resolved, target)
			}
			// Walk does not follow symlinks and root is a real path, so the
			// link's own directory is a real path too.
			reenters := func(dir string) bool { return containsPath(resolved, dir) }
			if reenters(filepath.Dir(path)) || slices.ContainsFunc(active, reenters) {
				return nil // cycle
			}
			return copyTreeFrom(resolved, target, active)
		}
		if info.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		if !info.Mode().IsRegular() {
			return nil
		}
		return copyFile(path, target)
	})
}

// containsPath reports whether dir is path or one of its ancestors.
func containsPath(dir, path string) bool {
	rel, err := filepath.Rel(dir, path)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

func copyFile(src, dst string) error {
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return fmt.Errorf("failed to create %s: %w", filepath.Dir(dst), err)
	}
	in, err := os.Open(src)
	if err != nil {
		return fmt.Errorf("failed to open %s: %w", src, err)
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("failed to create %s: %w", dst, err)
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return fmt.Errorf("failed to copy %s: %w", src, err)
	}
	return out.Close()
}

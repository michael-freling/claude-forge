package openshell

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

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

	memory := Memory(opts.GitHubToken, opts.PushRepos)
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

// Gitconfig returns the sandbox's ~/.gitconfig.
func Gitconfig(opts HomeOptions) string {
	var b strings.Builder
	if opts.GitUserName != "" || opts.GitUserEmail != "" {
		b.WriteString("[user]\n")
		if opts.GitUserName != "" {
			fmt.Fprintf(&b, "    name = %s\n", opts.GitUserName)
		}
		if opts.GitUserEmail != "" {
			fmt.Fprintf(&b, "    email = %s\n", opts.GitUserEmail)
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

// copyTree copies a directory, following symlinks so the sandbox gets real
// files (rules and skills are often symlinked into a dotfiles repository).
func copyTree(src, dst string) error {
	return filepath.Walk(src, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
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
			if ri.IsDir() {
				return copyTree(resolved, target)
			}
			return copyFile(resolved, target)
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

package openshell

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/michael-freling/claude-forge/internal/forge/config"
	"github.com/michael-freling/claude-forge/internal/forge/layout"
)

// repositories returns the repositories to clone into a new sandbox: the
// configured ones, then extra (from --repo), without duplicates. GitHub
// repository names are case-insensitive, so duplicates are too.
func (m *Manager) repositories(extra []string) ([]string, error) {
	var repos []string
	seen := map[string]bool{}
	for _, list := range [][]string{m.Config.OpenShell.Repositories, extra} {
		for _, r := range list {
			if err := config.ValidateGitHubRepo(r); err != nil {
				return nil, err
			}
			key := strings.ToLower(r)
			if seen[key] {
				continue
			}
			seen[key] = true
			repos = append(repos, r)
		}
	}
	return repos, nil
}

// cloneTarget is a repository and the workspace directory it is cloned into.
type cloneTarget struct {
	Repo string // owner/name
	Dir  string // absolute path under layout.Workspace
}

// cloneTargets puts each repository in a workspace directory named after it,
// or <owner>-<name> when an earlier repository already took the name.
func cloneTargets(repos []string) []cloneTarget {
	taken := map[string]bool{}
	targets := make([]cloneTarget, 0, len(repos))
	for _, r := range repos {
		owner, name, _ := strings.Cut(r, "/")
		dir := name
		if taken[strings.ToLower(dir)] {
			dir = owner + "-" + name
		}
		taken[strings.ToLower(dir)] = true
		targets = append(targets, cloneTarget{Repo: r, Dir: path.Join(layout.Workspace, dir)})
	}
	return targets
}

// cloneRepos clones repos into the sandbox workspace. A repository that
// cannot be cloned (a typo, or no access) only produces a warning.
func (m *Manager) cloneRepos(ctx context.Context, name string, repos []string) {
	for _, t := range cloneTargets(repos) {
		if _, err := m.Client.Run(ctx, name, "", nil, "test", "-e", t.Dir); err == nil {
			m.logf("Skipping %s: %s already exists", t.Repo, t.Dir)
			continue
		}
		m.logf("Cloning %s into %s", t.Repo, t.Dir)
		url := "https://github.com/" + t.Repo + ".git"
		if _, err := m.Client.Run(ctx, name, layout.Workspace, sandboxEnv(), "git", "clone", "--", url, t.Dir); err != nil {
			m.logf("Warning: failed to clone %s: %v", t.Repo, err)
		}
	}
}

// pluginIDs returns the plugins to install in a new sandbox: the configured
// list, or the plugins enabled on the host when none is configured.
func (m *Manager) pluginIDs() []string {
	if m.Config.OpenShell.Plugins != nil {
		return m.Config.OpenShell.Plugins
	}
	var settings struct {
		EnabledPlugins map[string]any `json:"enabledPlugins"`
	}
	settingsPath := filepath.Join(m.ClaudeDir, "settings.json")
	if err := readJSON(settingsPath, &settings); err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			m.logf("Warning: cannot read the host's plugins: %v", err)
		}
		return nil
	}
	var ids []string
	for id, enabled := range settings.EnabledPlugins {
		if on, ok := enabled.(bool); ok && on {
			ids = append(ids, id)
		}
	}
	sort.Strings(ids)
	return ids
}

// marketplaceSource is a marketplace's source in the host's
// ~/.claude/plugins/known_marketplaces.json.
type marketplaceSource struct {
	Source string `json:"source"`
	Repo   string `json:"repo"` // github
	URL    string `json:"url"`  // git, url
	Path   string `json:"path"` // directory
}

// knownMarketplaces reads the host's marketplaces by name. A missing file
// means none: every plugin's marketplace is then unresolvable.
func (m *Manager) knownMarketplaces() map[string]marketplaceSource {
	var known map[string]struct {
		Source marketplaceSource `json:"source"`
	}
	knownPath := filepath.Join(m.ClaudeDir, "plugins", "known_marketplaces.json")
	if err := readJSON(knownPath, &known); err != nil && !errors.Is(err, os.ErrNotExist) {
		m.logf("Warning: %v", err)
	}
	sources := make(map[string]marketplaceSource, len(known))
	for name, k := range known {
		sources[name] = k.Source
	}
	return sources
}

// githubRemotePattern matches the GitHub remote URLs git uses (SSH, ssh:// or
// HTTPS) and captures owner/name.
var githubRemotePattern = regexp.MustCompile(`^(?:git@github\.com:|ssh://git@github\.com/|https://github\.com/)([^/]+/[^/]+?)(?:\.git)?/?$`)

// gitOrigin returns the origin remote URL of a host git checkout.
func gitOrigin(dir string) (string, error) {
	out, err := exec.Command("git", "-C", dir, "remote", "get-url", "origin").Output()
	if err != nil {
		return "", fmt.Errorf("read the origin remote of %s: %w", dir, err)
	}
	return strings.TrimSpace(string(out)), nil
}

// marketplaceAddSource maps a host marketplace to the source
// `claude plugin marketplace add` takes in the sandbox. A marketplace added
// from a host directory has no meaning in the sandbox, so it is added from
// the GitHub repository the directory was cloned from.
func (m *Manager) marketplaceAddSource(s marketplaceSource) (string, error) {
	switch {
	case s.Source == "github" && s.Repo != "":
		return s.Repo, nil
	case (s.Source == "git" || s.Source == "url") && s.URL != "":
		return s.URL, nil
	case s.Source == "directory" && s.Path != "":
		origin := m.GitOrigin
		if origin == nil {
			origin = gitOrigin
		}
		remote, err := origin(s.Path)
		if err != nil {
			return "", err
		}
		match := githubRemotePattern.FindStringSubmatch(remote)
		if match == nil {
			return "", fmt.Errorf("the origin remote of %s (%s) is not a GitHub repository", s.Path, remote)
		}
		return match[1], nil
	}
	return "", fmt.Errorf("unsupported marketplace source %q", s.Source)
}

// installPlugins installs Claude Code plugins in the sandbox, adding each
// plugin's marketplace first. A plugin that cannot be installed only
// produces a warning.
func (m *Manager) installPlugins(ctx context.Context, name string) {
	ids := m.pluginIDs()
	if len(ids) == 0 {
		return
	}
	known := m.knownMarketplaces()
	// marketplaceReady records, per marketplace, whether it was added.
	marketplaceReady := map[string]bool{}
	for _, id := range ids {
		at := strings.LastIndex(id, "@")
		if at <= 0 || at == len(id)-1 {
			m.logf("Warning: skipping plugin %s: use name@marketplace", id)
			continue
		}
		market := id[at+1:]
		ready, tried := marketplaceReady[market]
		if !tried {
			ready = m.addMarketplace(ctx, name, market, known)
			marketplaceReady[market] = ready
		}
		if !ready {
			m.logf("Warning: skipping plugin %s: marketplace %s is unavailable", id, market)
			continue
		}
		m.logf("Installing plugin %s", id)
		if _, err := m.Client.Run(ctx, name, layout.Home, sandboxEnv(), "claude", "plugin", "install", id); err != nil {
			m.logf("Warning: failed to install plugin %s: %v", id, err)
		}
	}
}

// addMarketplace adds a host marketplace to the sandbox and reports whether
// it succeeded.
func (m *Manager) addMarketplace(ctx context.Context, name, market string, known map[string]marketplaceSource) bool {
	src, ok := known[market]
	if !ok {
		m.logf("Warning: marketplace %s is not in the host's known_marketplaces.json", market)
		return false
	}
	arg, err := m.marketplaceAddSource(src)
	if err != nil {
		m.logf("Warning: cannot add marketplace %s: %v", market, err)
		return false
	}
	m.logf("Adding plugin marketplace %s (%s)", market, arg)
	if _, err := m.Client.Run(ctx, name, layout.Home, sandboxEnv(), "claude", "plugin", "marketplace", "add", arg); err != nil {
		m.logf("Warning: failed to add marketplace %s: %v", market, err)
		return false
	}
	return true
}

func readJSON(file string, v any) error {
	data, err := os.ReadFile(file)
	if err != nil {
		return err
	}
	if err := json.Unmarshal(data, v); err != nil {
		return fmt.Errorf("parse %s: %w", file, err)
	}
	return nil
}

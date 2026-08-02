package dashboard

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/michael-freling/claude-forge/internal/forge/config"
	"github.com/michael-freling/claude-forge/internal/forge/container"
	"github.com/michael-freling/claude-forge/internal/forge/project"
	"github.com/michael-freling/claude-forge/internal/forge/session"
	"github.com/michael-freling/claude-forge/internal/gateway"
)

// Provider builds a Dashboard snapshot from the host's claude-forge state.
type Provider interface {
	Build(ctx context.Context) (*Dashboard, error)
}

// dataProvider is the default Provider. Its git, GitHub, and project-identify
// dependencies are function fields so tests can inject fakes and do no real
// git/Docker/GitHub I/O.
type dataProvider struct {
	containers container.ContainerManager
	homeDir    string
	configDir  string
	cwd        string

	// gitBranch resolves the current branch of a git working tree.
	gitBranch func(dir string) (string, error)
	// lookupPR fetches the pull request for a branch, or nil if none.
	lookupPR func(owner, repo, branch string) (*PR, error)
	// identify resolves a host directory into project metadata (execs git by
	// default); injectable so tests avoid real git.
	identify func(dir string) (*project.Project, error)
	// githubToken resolves the GitHub token once (memoized in the default).
	githubToken func() (string, error)
}

var _ Provider = (*dataProvider)(nil)

// NewProvider returns the production Provider wired to real git, GitHub, and
// Docker dependencies. cwd is the dashboard process's working directory, used
// to fully resolve the current project even when its encoded path is lossy.
func NewProvider(containers container.ContainerManager, homeDir, configDir, cwd string) Provider {
	p := &dataProvider{
		containers: containers,
		homeDir:    homeDir,
		configDir:  configDir,
		cwd:        cwd,
		gitBranch:  defaultGitBranch,
		identify:   project.Identify,
	}

	// Resolve the GitHub token at most once, lazily, and reuse the result for
	// both the PR gate and every PR lookup.
	var once sync.Once
	var tok string
	var tokErr error
	p.githubToken = func() (string, error) {
		once.Do(func() {
			auth, err := gateway.NewGitHubAuth()
			if err != nil {
				tokErr = err
				return
			}
			tok = auth.Token()
		})
		return tok, tokErr
	}
	p.lookupPR = p.defaultLookupPR

	return p
}

// Build gathers projects, sessions, PRs, and MCP server status into a snapshot.
// It never fails on best-effort data sources: problems become warnings so the
// dashboard still renders what it could collect.
func (p *dataProvider) Build(ctx context.Context) (*Dashboard, error) {
	warnings := []string{}

	cfg, err := config.Load(p.configDir)
	if err != nil {
		warnings = append(warnings, fmt.Sprintf("failed to load config: %v", err))
		cfg = config.DefaultConfig()
	}

	containers, err := p.containers.ListForgeContainers(ctx)
	if err != nil {
		warnings = append(warnings, fmt.Sprintf("failed to list containers: %v", err))
		containers = nil
	}

	// Resolve the GitHub token once to decide whether PR lookups are possible.
	token, tokErr := p.githubToken()
	prEnabled := false
	switch {
	case tokErr != nil:
		warnings = append(warnings, fmt.Sprintf("PR lookups disabled: %v", tokErr))
	case token == "":
		// No token available; skip PRs quietly.
	default:
		prEnabled = true
	}

	// Fully resolve the dashboard's own project so the current one is complete
	// even if its encoded ID cannot be reversed unambiguously.
	var cwdProj *project.Project
	if cp, err := p.identify(p.cwd); err == nil {
		cwdProj = cp
	}

	forgeDir := filepath.Join(p.homeDir, ".claude-forge")
	var subdirs []string
	if entries, err := os.ReadDir(forgeDir); err == nil {
		for _, e := range entries {
			if e.IsDir() && e.Name() != "plugins" {
				subdirs = append(subdirs, e.Name())
			}
		}
	}
	sort.Strings(subdirs)

	knownIDs := map[string]bool{}
	for _, id := range subdirs {
		knownIDs[id] = true
	}
	if cwdProj != nil {
		knownIDs[cwdProj.ID] = true
	}

	global, running := p.classifyContainers(containers, cfg, knownIDs)

	// A per-Build PR cache keyed by (owner, repo, branch) avoids duplicate
	// lookups when several sessions share a branch.
	prCache := map[string]*PR{}
	lookupWithCache := func(owner, repo, branch string) *PR {
		key := owner + "\x00" + repo + "\x00" + branch
		if pr, ok := prCache[key]; ok {
			return pr
		}
		pr, err := p.lookupPR(owner, repo, branch)
		if err != nil {
			warnings = append(warnings, fmt.Sprintf("PR lookup failed for %s/%s@%s: %v", owner, repo, branch, err))
			prCache[key] = nil
			return nil
		}
		prCache[key] = pr
		return pr
	}

	projects := []Project{}
	seen := map[string]bool{}
	build := func(projID string, resolved bool, dir, owner, repo string) {
		seen[projID] = true
		projects = append(projects, p.buildProject(
			forgeDir, projID, resolved, dir, owner, repo,
			running[projID], prEnabled, lookupWithCache,
		))
	}

	if cwdProj != nil {
		build(cwdProj.ID, true, cwdProj.Dir, cwdProj.Owner, cwdProj.Repo)
	}
	for _, projID := range subdirs {
		if seen[projID] {
			continue
		}
		resolved, dir, owner, repo := p.resolveProject(projID)
		if !resolved {
			warnings = append(warnings, fmt.Sprintf("could not resolve host directory for project %q; listing sessions only", projID))
		}
		build(projID, resolved, dir, owner, repo)
	}

	// Running sessions whose project was neither scanned nor the cwd still get
	// surfaced under a synthetic unresolved project.
	var leftover []string
	for projID := range running {
		if !seen[projID] {
			leftover = append(leftover, projID)
		}
	}
	sort.Strings(leftover)
	for _, projID := range leftover {
		warnings = append(warnings, fmt.Sprintf("running session found for unknown project %q", projID))
		build(projID, false, "", "", "")
	}

	return &Dashboard{
		GeneratedAt: time.Now().UTC().Format(time.RFC3339),
		Warnings:    warnings,
		Global:      global,
		Projects:    projects,
	}, nil
}

// resolveProject best-effort reverses a project ID back to its host directory
// and identifies it. It fails (resolved=false) when the encoding is lossy (a
// path component contained a hyphen) or the directory is not a git project.
func (p *dataProvider) resolveProject(projID string) (bool, string, string, string) {
	candidate := strings.ReplaceAll(projID, "-", "/")
	info, err := os.Stat(candidate)
	if err != nil || !info.IsDir() {
		return false, "", "", ""
	}
	proj, err := p.identify(candidate)
	if err != nil {
		return false, "", "", ""
	}
	return true, proj.Dir, proj.Owner, proj.Repo
}

// buildProject assembles one Project's sessions (with branches and PRs) and
// running sessions.
func (p *dataProvider) buildProject(
	forgeDir, projID string,
	resolved bool, dir, owner, repo string,
	runningByShort map[string]*RunningSession,
	prEnabled bool,
	lookupPR func(owner, repo, branch string) *PR,
) Project {
	proj := Project{
		ID:              projID,
		Dir:             dir,
		Owner:           owner,
		Repo:            repo,
		Resolved:        resolved,
		Sessions:        []Session{},
		RunningSessions: []RunningSession{},
	}

	sessions, err := session.List(filepath.Join(forgeDir, projID))
	if err == nil {
		for _, s := range sessions {
			branch := ""
			if resolved {
				branchDir := dir
				if s.IsWorktree() {
					branchDir = filepath.Join(dir, ".claude-worktrees", s.WorktreeName())
				}
				if b, berr := p.gitBranch(branchDir); berr == nil {
					branch = b
				}
			}

			var pr *PR
			if prEnabled && resolved && branch != "" {
				pr = lookupPR(owner, repo, branch)
			}

			proj.Sessions = append(proj.Sessions, Session{
				ID:           s.ID,
				Name:         s.Name,
				Worktree:     s.WorktreeName(),
				Branch:       branch,
				CreatedAt:    s.CreatedAt.Format(time.RFC3339),
				LastActive:   s.LastActive.Format(time.RFC3339),
				FirstMessage: s.FirstMsg,
				PR:           pr,
			})
		}
	}

	if runningByShort != nil {
		shorts := make([]string, 0, len(runningByShort))
		for sh := range runningByShort {
			shorts = append(shorts, sh)
		}
		sort.Strings(shorts)
		for _, sh := range shorts {
			rs := runningByShort[sh]
			sort.Slice(rs.MCPServers, func(i, j int) bool {
				return rs.MCPServers[i].Name < rs.MCPServers[j].Name
			})
			proj.RunningSessions = append(proj.RunningSessions, *rs)
		}
	}

	return proj
}

// classifyContainers partitions the forge container list into the global MCP
// panel and per-project running sessions with their session-scope MCP servers.
func (p *dataProvider) classifyContainers(
	containers []container.ContainerInfo,
	cfg *config.Config,
	knownIDs map[string]bool,
) (GlobalMCP, map[string]map[string]*RunningSession) {
	running := map[string]map[string]*RunningSession{}
	ensureRS := func(projID, short string) *RunningSession {
		if running[projID] == nil {
			running[projID] = map[string]*RunningSession{}
		}
		rs := running[projID][short]
		if rs == nil {
			rs = &RunningSession{ShortID: short, MCPServers: []MCPServer{}}
			running[projID][short] = rs
		}
		return rs
	}

	var k8sInfo *container.ContainerInfo
	globalContainers := map[string]container.ContainerInfo{}
	var customSidecars []container.ContainerInfo

	for _, c := range containers {
		name := c.Name
		switch {
		case name == "forge-k8s-mcp":
			ci := c
			k8sInfo = &ci
		case strings.HasPrefix(name, "forge-mcp-global-"):
			globalContainers[name] = c
		case strings.HasPrefix(name, "forge-agent-"):
			if pid, short, ok := splitProjShort(name[len("forge-agent-"):]); ok {
				ensureRS(pid, short)
			}
		case strings.HasPrefix(name, "forge-gateway-"):
			if pid, short, ok := splitProjShort(name[len("forge-gateway-"):]); ok {
				ensureRS(pid, short)
			}
		case strings.HasPrefix(name, "forge-github-mcp-"):
			if pid, short, ok := splitProjShort(name[len("forge-github-mcp-"):]); ok {
				rs := ensureRS(pid, short)
				rs.MCPServers = append(rs.MCPServers, mcpServerFromContainer("github", "session", c))
			}
		case strings.HasPrefix(name, "forge-mcp-"):
			// Deferred: splitting <name>-<projID> needs the full projID set.
			customSidecars = append(customSidecars, c)
		}
	}

	// The projID set for custom sidecars is the known projects plus any project
	// that already has a running session (its agent registered it above).
	pidSet := map[string]bool{}
	for id := range knownIDs {
		pidSet[id] = true
	}
	for pid := range running {
		pidSet[pid] = true
	}
	pids := make([]string, 0, len(pidSet))
	for pid := range pidSet {
		pids = append(pids, pid)
	}
	for _, c := range customSidecars {
		if serverName, pid, short, ok := splitCustom(c.Name[len("forge-mcp-"):], pids); ok {
			rs := ensureRS(pid, short)
			rs.MCPServers = append(rs.MCPServers, mcpServerFromContainer(serverName, "session", c))
		}
	}

	return GlobalMCP{Servers: p.globalServers(cfg, k8sInfo, globalContainers)}, running
}

// globalServers reconciles the declared global MCP config against the running
// global containers (k8s built-in, custom global sidecars, stdio/remote).
func (p *dataProvider) globalServers(
	cfg *config.Config,
	k8sInfo *container.ContainerInfo,
	globalContainers map[string]container.ContainerInfo,
) []MCPServer {
	servers := []MCPServer{}

	switch {
	case k8sInfo != nil:
		servers = append(servers, mcpServerFromContainer("kubernetes", "global", *k8sInfo))
	case cfg.Kubernetes.Enabled:
		servers = append(servers, MCPServer{
			Name:      "kubernetes",
			Scope:     "global",
			Kind:      "container",
			Container: "forge-k8s-mcp",
			Image:     cfg.Kubernetes.Image,
			Status:    "not running",
			Running:   false,
		})
	}

	handled := map[string]bool{}
	for _, s := range cfg.MCPServers {
		switch {
		case s.IsContainer() && s.Scope == "session":
			// Session-scoped sidecars appear under their running session, not here.
			continue
		case s.IsGlobal():
			cname := "forge-mcp-global-" + s.Name
			handled[cname] = true
			if ci, ok := globalContainers[cname]; ok {
				servers = append(servers, mcpServerFromContainer(s.Name, "global", ci))
			} else {
				servers = append(servers, MCPServer{
					Name:      s.Name,
					Scope:     "global",
					Kind:      "container",
					Container: cname,
					Image:     s.Image,
					Status:    "not running",
					Running:   false,
				})
			}
		case s.IsStdio():
			servers = append(servers, MCPServer{
				Name: s.Name, Scope: "global", Kind: "stdio", Status: "n/a",
			})
		default:
			servers = append(servers, MCPServer{
				Name: s.Name, Scope: "global", Kind: "remote", Status: "n/a",
			})
		}
	}

	// Global sidecars running without a matching config entry are still shown.
	var extra []string
	for name := range globalContainers {
		if !handled[name] {
			extra = append(extra, name)
		}
	}
	sort.Strings(extra)
	for _, name := range extra {
		ci := globalContainers[name]
		servers = append(servers, mcpServerFromContainer(strings.TrimPrefix(name, "forge-mcp-global-"), "global", ci))
	}

	return servers
}

// mcpServerFromContainer builds a container-backed MCPServer from live info.
func mcpServerFromContainer(name, scope string, c container.ContainerInfo) MCPServer {
	return MCPServer{
		Name:      name,
		Scope:     scope,
		Kind:      "container",
		Container: c.Name,
		Image:     c.Image,
		Status:    c.Status,
		Running:   isRunning(c.Status),
	}
}

// isRunning reports whether a Docker status string denotes a running container.
func isRunning(status string) bool {
	return strings.HasPrefix(status, "Up")
}

// isShortID reports whether s is an 8-character lowercase-hex session short ID.
func isShortID(s string) bool {
	if len(s) != 8 {
		return false
	}
	for _, r := range s {
		if (r < '0' || r > '9') && (r < 'a' || r > 'f') {
			return false
		}
	}
	return true
}

// splitProjShort parses "<projID>-<short>" where short is an 8-hex suffix.
func splitProjShort(rest string) (projID, short string, ok bool) {
	if len(rest) < 9 || rest[len(rest)-9] != '-' {
		return "", "", false
	}
	short = rest[len(rest)-8:]
	if !isShortID(short) {
		return "", "", false
	}
	return rest[:len(rest)-9], short, true
}

// splitCustom parses "<name>-<projID>-<short>" for a custom session sidecar,
// choosing the longest known projID that fits so a hyphenated server name and a
// hyphenated project path can be told apart.
func splitCustom(rest string, projIDs []string) (name, projID, short string, ok bool) {
	if len(rest) < 9 || rest[len(rest)-9] != '-' {
		return "", "", "", false
	}
	short = rest[len(rest)-8:]
	if !isShortID(short) {
		return "", "", "", false
	}
	middle := rest[:len(rest)-9] // <name>-<projID>

	best := ""
	for _, pid := range projIDs {
		if pid == "" {
			continue
		}
		if strings.HasSuffix(middle, "-"+pid) && len(pid) > len(best) {
			best = pid
		}
	}
	if best == "" {
		return "", "", "", false
	}
	name = strings.TrimSuffix(middle, "-"+best)
	if name == "" {
		return "", "", "", false
	}
	return name, best, short, true
}

// --- default (production) dependency implementations ---

// defaultGitBranch returns the current branch of the git working tree at dir.
func defaultGitBranch(dir string) (string, error) {
	out, err := exec.Command("git", "-C", dir, "rev-parse", "--abbrev-ref", "HEAD").Output()
	if err != nil {
		return "", fmt.Errorf("failed to resolve branch for %s: %w", dir, err)
	}
	return strings.TrimSpace(string(out)), nil
}

// githubAPIBaseURL is the GitHub REST API base; overridable in tests.
var githubAPIBaseURL = "https://api.github.com"

// prHTTPClient bounds PR lookups so a slow API cannot stall a Build.
var prHTTPClient = &http.Client{Timeout: 10 * time.Second}

// defaultLookupPR fetches the first pull request opened from owner:branch.
func (p *dataProvider) defaultLookupPR(owner, repo, branch string) (*PR, error) {
	token, err := p.githubToken()
	if err != nil {
		return nil, err
	}
	if token == "" {
		return nil, nil
	}

	url := fmt.Sprintf("%s/repos/%s/%s/pulls?head=%s:%s&state=all",
		githubAPIBaseURL, owner, repo, owner, branch)
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "token "+token)
	req.Header.Set("Accept", "application/vnd.github+json")

	resp, err := prHTTPClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return nil, fmt.Errorf("github api returned %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	var prs []struct {
		Number   int     `json:"number"`
		Title    string  `json:"title"`
		State    string  `json:"state"`
		HTMLURL  string  `json:"html_url"`
		Draft    bool    `json:"draft"`
		MergedAt *string `json:"merged_at"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&prs); err != nil {
		return nil, err
	}
	if len(prs) == 0 {
		return nil, nil
	}

	first := prs[0]
	state := first.State
	if first.MergedAt != nil && *first.MergedAt != "" {
		state = "merged"
	}
	return &PR{
		Number: first.Number,
		Title:  first.Title,
		State:  state,
		URL:    first.HTMLURL,
		Draft:  first.Draft,
	}, nil
}

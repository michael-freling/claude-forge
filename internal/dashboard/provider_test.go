package dashboard

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/michael-freling/claude-forge/internal/forge/container"
	"github.com/michael-freling/claude-forge/internal/forge/project"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- fakeContainerManager implements container.ContainerManager for tests ---

type fakeContainerManager struct {
	containers []container.ContainerInfo
	listErr    error
}

func (f *fakeContainerManager) ListForgeContainers(_ context.Context) ([]container.ContainerInfo, error) {
	return f.containers, f.listErr
}
func (f *fakeContainerManager) CreateNetwork(_ context.Context, _ string) (string, error) {
	return "", nil
}
func (f *fakeContainerManager) RemoveNetwork(_ context.Context, _ string) error { return nil }
func (f *fakeContainerManager) EnsureSharedNetwork(_ context.Context, _ string) (string, error) {
	return "", nil
}
func (f *fakeContainerManager) ConnectNetwork(_ context.Context, _, _ string, _ []string) error {
	return nil
}
func (f *fakeContainerManager) StartAgent(_ context.Context, _ container.AgentOptions) (string, error) {
	return "", nil
}
func (f *fakeContainerManager) StartGateway(_ context.Context, _ container.GatewayOptions) (string, error) {
	return "", nil
}
func (f *fakeContainerManager) StartGitHubMCP(_ context.Context, _ container.GitHubMCPOptions) (string, error) {
	return "", nil
}
func (f *fakeContainerManager) StartSharedService(_ context.Context, _ container.SharedServiceOptions) (string, error) {
	return "", nil
}
func (f *fakeContainerManager) IsContainerRunning(_ context.Context, _ string) (bool, error) {
	return false, nil
}
func (f *fakeContainerManager) WaitForReady(_ context.Context, _ string, _ time.Duration) error {
	return nil
}
func (f *fakeContainerManager) StopContainer(_ context.Context, _ string) error   { return nil }
func (f *fakeContainerManager) RemoveContainer(_ context.Context, _ string) error { return nil }
func (f *fakeContainerManager) PullImage(_ context.Context, _ string) error       { return nil }
func (f *fakeContainerManager) ImageExists(_ context.Context, _ string) (bool, error) {
	return true, nil
}
func (f *fakeContainerManager) ContainerLogs(_ context.Context, _ string) (string, error) {
	return "", nil
}
func (f *fakeContainerManager) Close() error { return nil }

// encodeID mirrors project.Identify's ID derivation (path "/" -> "-").
func encodeID(dir string) string { return strings.ReplaceAll(dir, "/", "-") }

// writeSession writes a minimal Claude Code JSONL transcript plus a name sidecar.
func writeSession(t *testing.T, dir, id, timestamp, message, name string) {
	t.Helper()
	require.NoError(t, os.MkdirAll(dir, 0o755))
	lines := []map[string]any{
		{"type": "user", "message": map[string]string{"role": "user", "content": message}, "timestamp": timestamp},
	}
	var content []byte
	for _, line := range lines {
		b, err := json.Marshal(line)
		require.NoError(t, err)
		content = append(content, b...)
		content = append(content, '\n')
	}
	require.NoError(t, os.WriteFile(filepath.Join(dir, id+".jsonl"), content, 0o644))
	// The sidecar lives one directory up from the transcript's subdir.
	sidecarDir := filepath.Dir(dir)
	if filepath.Base(dir) == "-work" || strings.HasPrefix(filepath.Base(dir), "-work--") {
		require.NoError(t, os.WriteFile(filepath.Join(sidecarDir, id+".json"),
			[]byte(fmt.Sprintf(`{"name":%q}`, name)), 0o644))
	}
}

func findServer(servers []MCPServer, name string) (MCPServer, bool) {
	for _, s := range servers {
		if s.Name == name {
			return s, true
		}
	}
	return MCPServer{}, false
}

func findProject(projects []Project, id string) (Project, bool) {
	for _, p := range projects {
		if p.ID == id {
			return p, true
		}
	}
	return Project{}, false
}

func hasWarning(warnings []string, substr string) bool {
	for _, w := range warnings {
		if strings.Contains(w, substr) {
			return true
		}
	}
	return false
}

func TestBuild_Full(t *testing.T) {
	homeDir := t.TempDir()
	forgeDir := filepath.Join(homeDir, ".claude-forge")
	configDir := filepath.Join(homeDir, ".config", "claude-forge")
	require.NoError(t, os.MkdirAll(configDir, 0o755))

	// A declared MCP config exercising each global path plus a session-scoped
	// server that must NOT appear in the global panel.
	configYAML := `mcp_servers:
  - name: foo
    type: container
    image: img/foo:latest
    port: 8080
  - name: bar
    type: container
    image: img/bar:latest
    port: 8080
  - name: sh
    type: stdio
    command: my-server
  - name: rem
    type: http
    url: https://mcp.example.com
  - name: sess1
    type: container
    scope: session
    image: img/sess:latest
    port: 8080
`
	require.NoError(t, os.WriteFile(filepath.Join(configDir, "config.yaml"), []byte(configYAML), 0o644))

	// The cwd project: fully resolved via the injected identify.
	rnd := fmt.Sprintf("%x", time.Now().UnixNano())
	cwd := "/tmp/forgedashcwd" + rnd
	cwdID := encodeID(cwd)

	// A resolved scanned (non-cwd) project. Its host dir really exists so
	// resolveProject's os.Stat succeeds and the injected identify resolves it.
	resolvedDir := filepath.Join(os.TempDir(), "forgedashres"+rnd)
	require.NoError(t, os.MkdirAll(resolvedDir, 0o755))
	t.Cleanup(func() { os.RemoveAll(resolvedDir) })
	resolvedID := encodeID(resolvedDir)

	// An unresolved project: its reverse-encoded path does not exist.
	unresolvedID := "-no-such-dir-" + rnd

	// Seed sessions.
	// cwd: two -work sessions (same derived branch "main") + one worktree session.
	writeSession(t, filepath.Join(forgeDir, cwdID, "-work"),
		"11111111-1111-4111-8111-111111111111", "2025-01-15T10:00:00Z", "first message", "main-a")
	writeSession(t, filepath.Join(forgeDir, cwdID, "-work"),
		"22222222-2222-4222-8222-222222222222", "2025-01-15T11:00:00Z", "second message", "main-b")
	writeSession(t, filepath.Join(forgeDir, cwdID, "-work--claude-worktrees-feature"),
		"33333333-3333-4333-8333-333333333333", "2025-01-15T12:00:00Z", "wt message", "wt")
	// resolved scanned project: one session.
	writeSession(t, filepath.Join(forgeDir, resolvedID, "-work"),
		"44444444-4444-4444-8444-444444444444", "2025-01-15T09:00:00Z", "other repo", "other")
	// unresolved project: one session (listed, but no branch/PR).
	writeSession(t, filepath.Join(forgeDir, unresolvedID, "-work"),
		"55555555-5555-4555-8555-555555555555", "2025-01-15T08:00:00Z", "orphan", "orphan")

	short := "abcd1234"
	containers := []container.ContainerInfo{
		{Name: "forge-agent-" + cwdID + "-" + short, Image: "agent:latest", Status: "Up 3 minutes"},
		{Name: "forge-gateway-" + cwdID + "-" + short, Image: "gateway:latest", Status: "Up 3 minutes"},
		{Name: "forge-github-mcp-" + cwdID + "-" + short, Image: "github-mcp:latest", Status: "Up 5 minutes"},
		{Name: "forge-mcp-custom-" + cwdID + "-" + short, Image: "custom:latest", Status: "Exited (0) 1 minute ago"},
		{Name: "forge-k8s-mcp", Image: "kubernetes-mcp-server:latest", Status: "Up 2 hours"},
		{Name: "forge-mcp-global-foo", Image: "img/foo:latest", Status: "Up 10 minutes"},
		{Name: "forge-mcp-global-orphan", Image: "img/orphan:latest", Status: "Up 1 minute"},
	}
	fakeCM := &fakeContainerManager{containers: containers}

	var prCalls int32
	lookupPR := func(owner, repo, branch string) (*PR, error) {
		atomic.AddInt32(&prCalls, 1)
		if owner == "me" && repo == "proj" && branch == "main" {
			return &PR{Number: 7, Title: "My PR", State: "open", URL: "https://github.com/me/proj/pull/7"}, nil
		}
		return nil, nil
	}
	identify := func(dir string) (*project.Project, error) {
		switch dir {
		case cwd:
			return &project.Project{ID: cwdID, Dir: cwd, Owner: "me", Repo: "proj"}, nil
		case resolvedDir:
			return &project.Project{ID: resolvedID, Dir: resolvedDir, Owner: "org", Repo: "repo2"}, nil
		default:
			return nil, fmt.Errorf("not a git project: %s", dir)
		}
	}
	gitBranch := func(dir string) (string, error) {
		switch dir {
		case cwd:
			return "main", nil
		case filepath.Join(cwd, ".claude-worktrees", "feature"):
			return "feature", nil
		case resolvedDir:
			return "develop", nil
		default:
			return "", fmt.Errorf("no branch for %s", dir)
		}
	}

	p := NewProvider(fakeCM, homeDir, configDir, cwd).(*dataProvider)
	p.identify = identify
	p.gitBranch = gitBranch
	p.lookupPR = lookupPR
	p.githubToken = func() (string, error) { return "test-token", nil }

	dash, err := p.Build(context.Background())
	require.NoError(t, err)
	require.NotNil(t, dash)

	// generatedAt is a valid RFC3339 timestamp.
	_, perr := time.Parse(time.RFC3339, dash.GeneratedAt)
	require.NoError(t, perr)

	// --- global panel ---
	k8s, ok := findServer(dash.Global.Servers, "kubernetes")
	require.True(t, ok)
	assert.Equal(t, "global", k8s.Scope)
	assert.Equal(t, "container", k8s.Kind)
	assert.True(t, k8s.Running)
	assert.Equal(t, "forge-k8s-mcp", k8s.Container)

	foo, ok := findServer(dash.Global.Servers, "foo")
	require.True(t, ok)
	assert.True(t, foo.Running)
	assert.Equal(t, "forge-mcp-global-foo", foo.Container)

	bar, ok := findServer(dash.Global.Servers, "bar")
	require.True(t, ok)
	assert.False(t, bar.Running)
	assert.Equal(t, "not running", bar.Status)
	assert.Equal(t, "container", bar.Kind)

	sh, ok := findServer(dash.Global.Servers, "sh")
	require.True(t, ok)
	assert.Equal(t, "stdio", sh.Kind)
	assert.Equal(t, "n/a", sh.Status)

	rem, ok := findServer(dash.Global.Servers, "rem")
	require.True(t, ok)
	assert.Equal(t, "remote", rem.Kind)
	assert.Equal(t, "n/a", rem.Status)

	orphan, ok := findServer(dash.Global.Servers, "orphan")
	require.True(t, ok)
	assert.Equal(t, "global", orphan.Scope)
	assert.True(t, orphan.Running)

	_, sess1Present := findServer(dash.Global.Servers, "sess1")
	assert.False(t, sess1Present, "session-scoped server must not appear in the global panel")

	// --- cwd project ---
	cwdProj, ok := findProject(dash.Projects, cwdID)
	require.True(t, ok)
	assert.True(t, cwdProj.Resolved)
	assert.Equal(t, "me", cwdProj.Owner)
	assert.Equal(t, "proj", cwdProj.Repo)
	assert.Equal(t, cwd, cwdProj.Dir)
	require.Len(t, cwdProj.Sessions, 3)

	var mainSessions, wtSessions int
	for _, s := range cwdProj.Sessions {
		if s.Worktree == "feature" {
			wtSessions++
			assert.Equal(t, "feature", s.Branch)
			assert.Nil(t, s.PR)
		} else {
			mainSessions++
			assert.Equal(t, "main", s.Branch)
			require.NotNil(t, s.PR)
			assert.Equal(t, 7, s.PR.Number)
			assert.Equal(t, "open", s.PR.State)
		}
	}
	assert.Equal(t, 2, mainSessions)
	assert.Equal(t, 1, wtSessions)

	// PR lookups are cached by (owner,repo,branch): the two "main" sessions
	// share one call, so the distinct keys are me/proj@main, me/proj@feature,
	// and org/repo2@develop (the resolved scanned project) => 3 calls, not 4.
	assert.Equal(t, int32(3), atomic.LoadInt32(&prCalls))

	// running session with its session-scope MCP servers.
	require.Len(t, cwdProj.RunningSessions, 1)
	rs := cwdProj.RunningSessions[0]
	assert.Equal(t, short, rs.ShortID)
	gh, ok := findServer(rs.MCPServers, "github")
	require.True(t, ok)
	assert.Equal(t, "session", gh.Scope)
	assert.True(t, gh.Running)
	custom, ok := findServer(rs.MCPServers, "custom")
	require.True(t, ok)
	assert.Equal(t, "session", custom.Scope)
	assert.False(t, custom.Running)

	// --- resolved scanned project ---
	rp, ok := findProject(dash.Projects, resolvedID)
	require.True(t, ok)
	assert.True(t, rp.Resolved)
	assert.Equal(t, "org", rp.Owner)
	require.Len(t, rp.Sessions, 1)
	assert.Equal(t, "develop", rp.Sessions[0].Branch)

	// --- unresolved project ---
	up, ok := findProject(dash.Projects, unresolvedID)
	require.True(t, ok)
	assert.False(t, up.Resolved)
	assert.Empty(t, up.Dir)
	require.Len(t, up.Sessions, 1)
	assert.Empty(t, up.Sessions[0].Branch)
	assert.Nil(t, up.Sessions[0].PR)
	assert.True(t, hasWarning(dash.Warnings, unresolvedID))
}

func TestBuild_TokenFailureWarning(t *testing.T) {
	homeDir := t.TempDir()
	configDir := filepath.Join(homeDir, ".config", "claude-forge")
	require.NoError(t, os.MkdirAll(configDir, 0o755))

	p := NewProvider(&fakeContainerManager{}, homeDir, configDir, "/tmp/none").(*dataProvider)
	p.identify = func(string) (*project.Project, error) { return nil, fmt.Errorf("no repo") }
	p.githubToken = func() (string, error) { return "", fmt.Errorf("no token available") }

	dash, err := p.Build(context.Background())
	require.NoError(t, err)
	assert.True(t, hasWarning(dash.Warnings, "PR lookups disabled"))
	assert.Empty(t, dash.Projects)
}

func TestBuild_TokenEmptyNoWarning(t *testing.T) {
	homeDir := t.TempDir()
	configDir := filepath.Join(homeDir, ".config", "claude-forge")
	require.NoError(t, os.MkdirAll(configDir, 0o755))

	p := NewProvider(&fakeContainerManager{}, homeDir, configDir, "/tmp/none").(*dataProvider)
	p.identify = func(string) (*project.Project, error) { return nil, fmt.Errorf("no repo") }
	p.githubToken = func() (string, error) { return "", nil }

	dash, err := p.Build(context.Background())
	require.NoError(t, err)
	assert.False(t, hasWarning(dash.Warnings, "PR lookups disabled"))
}

func TestBuild_ContainerListError(t *testing.T) {
	homeDir := t.TempDir()
	configDir := filepath.Join(homeDir, ".config", "claude-forge")
	require.NoError(t, os.MkdirAll(configDir, 0o755))

	fakeCM := &fakeContainerManager{listErr: fmt.Errorf("docker down")}
	p := NewProvider(fakeCM, homeDir, configDir, "/tmp/none").(*dataProvider)
	p.identify = func(string) (*project.Project, error) { return nil, fmt.Errorf("no repo") }
	p.githubToken = func() (string, error) { return "test-token", nil }

	dash, err := p.Build(context.Background())
	require.NoError(t, err)
	assert.True(t, hasWarning(dash.Warnings, "failed to list containers"))
}

func TestBuild_ConfigLoadError(t *testing.T) {
	homeDir := t.TempDir()
	configDir := filepath.Join(homeDir, ".config", "claude-forge")
	require.NoError(t, os.MkdirAll(configDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(configDir, "config.yaml"), []byte("mcp_servers: [invalid"), 0o644))

	p := NewProvider(&fakeContainerManager{}, homeDir, configDir, "/tmp/none").(*dataProvider)
	p.identify = func(string) (*project.Project, error) { return nil, fmt.Errorf("no repo") }
	p.githubToken = func() (string, error) { return "test-token", nil }

	dash, err := p.Build(context.Background())
	require.NoError(t, err)
	assert.True(t, hasWarning(dash.Warnings, "failed to load config"))
}

func TestBuild_KubernetesEnabledNotRunning(t *testing.T) {
	homeDir := t.TempDir()
	configDir := filepath.Join(homeDir, ".config", "claude-forge")
	require.NoError(t, os.MkdirAll(configDir, 0o755))
	configYAML := `kubernetes:
  enabled: true
  image: img/k8s:latest
`
	require.NoError(t, os.WriteFile(filepath.Join(configDir, "config.yaml"), []byte(configYAML), 0o644))

	p := NewProvider(&fakeContainerManager{}, homeDir, configDir, "/tmp/none").(*dataProvider)
	p.identify = func(string) (*project.Project, error) { return nil, fmt.Errorf("no repo") }
	p.githubToken = func() (string, error) { return "test-token", nil }

	dash, err := p.Build(context.Background())
	require.NoError(t, err)
	k8s, ok := findServer(dash.Global.Servers, "kubernetes")
	require.True(t, ok)
	assert.False(t, k8s.Running)
	assert.Equal(t, "not running", k8s.Status)
	assert.Equal(t, "img/k8s:latest", k8s.Image)
}

func TestBuild_UnknownProjectRunningSession(t *testing.T) {
	homeDir := t.TempDir()
	configDir := filepath.Join(homeDir, ".config", "claude-forge")
	require.NoError(t, os.MkdirAll(configDir, 0o755))

	fakeCM := &fakeContainerManager{containers: []container.ContainerInfo{
		{Name: "forge-agent-someproj-deadbeef", Image: "agent", Status: "Up 1 minute"},
		{Name: "forge-mcp-side-someproj-deadbeef", Image: "side", Status: "Up 1 minute"},
	}}
	p := NewProvider(fakeCM, homeDir, configDir, "/tmp/none").(*dataProvider)
	p.identify = func(string) (*project.Project, error) { return nil, fmt.Errorf("no repo") }
	p.githubToken = func() (string, error) { return "test-token", nil }

	dash, err := p.Build(context.Background())
	require.NoError(t, err)
	proj, ok := findProject(dash.Projects, "someproj")
	require.True(t, ok)
	assert.False(t, proj.Resolved)
	require.Len(t, proj.RunningSessions, 1)
	assert.Equal(t, "deadbeef", proj.RunningSessions[0].ShortID)
	_, ok = findServer(proj.RunningSessions[0].MCPServers, "side")
	assert.True(t, ok)
	assert.True(t, hasWarning(dash.Warnings, "unknown project"))
}

// --- helper unit tests ---

func TestSplitProjShort(t *testing.T) {
	pid, short, ok := splitProjShort("-home-user-proj-abcd1234")
	require.True(t, ok)
	assert.Equal(t, "-home-user-proj", pid)
	assert.Equal(t, "abcd1234", short)

	_, _, ok = splitProjShort("tooshort")
	assert.False(t, ok)

	_, _, ok = splitProjShort("-proj-nothexid")
	assert.False(t, ok)
}

func TestSplitCustom(t *testing.T) {
	name, pid, short, ok := splitCustom("gcloud--home-user-proj-abcd1234", []string{"-home-user-proj", "-home"})
	require.True(t, ok)
	assert.Equal(t, "gcloud", name)
	assert.Equal(t, "-home-user-proj", pid)
	assert.Equal(t, "abcd1234", short)

	// No known projID matches.
	_, _, _, ok = splitCustom("gcloud--home-user-proj-abcd1234", []string{"-other"})
	assert.False(t, ok)

	// Missing the name segment.
	_, _, _, ok = splitCustom("-home-abcd1234", []string{"-home"})
	assert.False(t, ok)

	// Non-hex short.
	_, _, _, ok = splitCustom("gcloud--home-zzzzzzzz", []string{"-home"})
	assert.False(t, ok)
}

func TestIsRunning(t *testing.T) {
	assert.True(t, isRunning("Up 3 minutes"))
	assert.False(t, isRunning("Exited (0) 1 minute ago"))
	assert.False(t, isRunning(""))
}

func TestResolveProject_Unresolved(t *testing.T) {
	p := &dataProvider{identify: func(string) (*project.Project, error) { return nil, fmt.Errorf("x") }}
	resolved, _, _, _ := p.resolveProject("-definitely-not-a-real-path-xyz")
	assert.False(t, resolved)
}

// --- default dependency impls ---

func TestDefaultGitBranch(t *testing.T) {
	dir := t.TempDir()
	for _, args := range [][]string{
		{"init"},
		{"config", "user.email", "t@t.com"},
		{"config", "user.name", "t"},
		{"checkout", "-b", "feature-x"},
		{"commit", "--allow-empty", "-m", "init"},
	} {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		require.NoError(t, cmd.Run(), "git %v", args)
	}

	branch, err := defaultGitBranch(dir)
	require.NoError(t, err)
	assert.Equal(t, "feature-x", branch)

	_, err = defaultGitBranch(filepath.Join(dir, "does-not-exist"))
	require.Error(t, err)
}

func TestDefaultLookupPR(t *testing.T) {
	var gotAuth, gotAccept string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		gotAccept = r.Header.Get("Accept")
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Query().Get("head") {
		case "me:open-branch":
			_, _ = w.Write([]byte(`[{"number":1,"title":"Open","state":"open","html_url":"u1","draft":true}]`))
		case "me:merged-branch":
			_, _ = w.Write([]byte(`[{"number":2,"title":"Merged","state":"closed","html_url":"u2","merged_at":"2025-01-01T00:00:00Z"}]`))
		case "me:none-branch":
			_, _ = w.Write([]byte(`[]`))
		case "me:bad-branch":
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"message":"Not Found"}`))
		default:
			w.WriteHeader(http.StatusInternalServerError)
		}
	}))
	defer srv.Close()

	oldBase := githubAPIBaseURL
	githubAPIBaseURL = srv.URL
	defer func() { githubAPIBaseURL = oldBase }()

	p := &dataProvider{githubToken: func() (string, error) { return "tok", nil }}

	pr, err := p.defaultLookupPR("me", "proj", "open-branch")
	require.NoError(t, err)
	require.NotNil(t, pr)
	assert.Equal(t, 1, pr.Number)
	assert.Equal(t, "open", pr.State)
	assert.True(t, pr.Draft)
	assert.Equal(t, "token tok", gotAuth)
	assert.Equal(t, "application/vnd.github+json", gotAccept)

	pr, err = p.defaultLookupPR("me", "proj", "merged-branch")
	require.NoError(t, err)
	require.NotNil(t, pr)
	assert.Equal(t, "merged", pr.State)

	pr, err = p.defaultLookupPR("me", "proj", "none-branch")
	require.NoError(t, err)
	assert.Nil(t, pr)

	_, err = p.defaultLookupPR("me", "proj", "bad-branch")
	require.Error(t, err)
}

func TestDefaultLookupPR_TokenPaths(t *testing.T) {
	// Token resolution error propagates.
	p := &dataProvider{githubToken: func() (string, error) { return "", fmt.Errorf("boom") }}
	_, err := p.defaultLookupPR("me", "proj", "b")
	require.Error(t, err)

	// Empty token yields no PR and no error, with no HTTP call.
	p = &dataProvider{githubToken: func() (string, error) { return "", nil }}
	pr, err := p.defaultLookupPR("me", "proj", "b")
	require.NoError(t, err)
	assert.Nil(t, pr)
}

func TestNewProvider_DefaultTokenFromEnv(t *testing.T) {
	t.Setenv("GITHUB_TOKEN", "ghp_env_token")
	p := NewProvider(&fakeContainerManager{}, t.TempDir(), t.TempDir(), "/tmp/none").(*dataProvider)
	tok, err := p.githubToken()
	require.NoError(t, err)
	assert.Equal(t, "ghp_env_token", tok)
	// The default gitBranch and identify are wired.
	require.NotNil(t, p.gitBranch)
	require.NotNil(t, p.identify)
}

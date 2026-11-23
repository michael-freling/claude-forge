package openshell

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/michael-freling/claude-forge/internal/forge/layout"
)

// runPrefix is the openshell command line of a non-interactive exec in the
// fixture's sandbox, as Client.Run builds it.
func runPrefix(workdir string, env map[string]string, command ...string) string {
	return strings.Join(execArgs("demo", "--no-tty", workdir, env, command), " ")
}

// commands returns the sandbox commands (the part after --) of the recorded
// non-interactive execs.
func (f *fakeRunner) commands() []string {
	var out []string
	for _, c := range f.find("sandbox exec -n demo --no-tty") {
		_, command, _ := strings.Cut(c.String(), " -- ")
		out = append(out, command)
	}
	return out
}

func TestClientRun(t *testing.T) {
	runner := newFakeRunner()
	runner.responses["sandbox exec"] = "output"
	c := &Client{Runner: runner}

	out, err := c.Run(context.Background(), "demo", "/w", map[string]string{"B": "2", "A": "1"}, "echo", "hi")
	require.NoError(t, err)
	assert.Equal(t, "output", string(out))
	require.Len(t, runner.calls, 1)
	assert.False(t, runner.calls[0].Interactive)
	assert.Equal(t, "sandbox exec -n demo --no-tty --workdir /w --env A=1 --env B=2 -- echo hi", runner.calls[0].String())

	_, err = c.Run(context.Background(), "demo", "", nil, "true")
	require.NoError(t, err)
	assert.Equal(t, "sandbox exec -n demo --no-tty -- true", runner.calls[1].String())
}

func TestManagerRepositories(t *testing.T) {
	tests := []struct {
		name    string
		config  []string
		extra   []string
		want    []string
		wantErr string
	}{
		{name: "none"},
		{name: "config then flags", config: []string{"octo/a"}, extra: []string{"octo/b"}, want: []string{"octo/a", "octo/b"}},
		{name: "duplicates ignore case", config: []string{"octo/a", "Octo/A"}, extra: []string{"octo/a", "octo/b", "octo/b"}, want: []string{"octo/a", "octo/b"}},
		{name: "invalid flag", extra: []string{"nope"}, wantErr: "invalid repository \"nope\""},
		{name: "git suffix", extra: []string{"octo/a.git"}, wantErr: "invalid repository"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newManagerFixture(t)
			f.m.Config.OpenShell.Repositories = tt.config
			got, err := f.m.repositories(tt.extra)
			if tt.wantErr != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.wantErr)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestCloneTargets(t *testing.T) {
	tests := []struct {
		name  string
		repos []string
		want  []cloneTarget
	}{
		{name: "none", want: []cloneTarget{}},
		{
			name:  "distinct names",
			repos: []string{"octo/a", "octo/b"},
			want: []cloneTarget{
				{Repo: "octo/a", Dir: layout.Workspace + "/a"},
				{Repo: "octo/b", Dir: layout.Workspace + "/b"},
			},
		},
		{
			name:  "same name from different owners",
			repos: []string{"octo/demo", "other/Demo", "third/demo"},
			want: []cloneTarget{
				{Repo: "octo/demo", Dir: layout.Workspace + "/demo"},
				{Repo: "other/Demo", Dir: layout.Workspace + "/other-Demo"},
				{Repo: "third/demo", Dir: layout.Workspace + "/third-demo"},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, cloneTargets(tt.repos))
		})
	}
}

func TestManagerStartSetsUpSandbox(t *testing.T) {
	f := newManagerFixture(t)
	f.runner.responses["sandbox list"] = sandboxListJSON()
	f.m.Config.OpenShell.Repositories = []string{"octo/demo", "octo/exists"}
	f.m.Config.OpenShell.Plugins = []string{"p@gh"}
	writeFile(t, filepath.Join(f.m.ClaudeDir, "plugins", "known_marketplaces.json"),
		`{"gh":{"source":{"source":"github","repo":"octo/market"}}}`)
	// Only octo/exists is already in the workspace.
	f.runner.failures[runPrefix("", nil, "test", "-e", layout.Workspace+"/demo")] = errFake
	f.runner.failures[runPrefix("", nil, "test", "-e", layout.Workspace+"/other-demo")] = errFake
	f.runner.failures[runPrefix("", nil, "test", "-e", layout.Workspace+"/missing")] = errFake
	f.runner.failures[runPrefix(layout.Workspace, sandboxEnv(), "git", "clone", "--", "https://github.com/octo/missing.git")] = errFake

	require.NoError(t, f.m.Start(context.Background(), StartOptions{
		Name:  "demo",
		Repos: []string{"other/demo", "octo/missing", "octo/demo"},
	}))

	settingsDir := path.Join(layout.Home, ".claude")
	assert.Equal(t, []string{
		"bash -c " + mergeSettingsScript + " bash " + settingsDir + "/" + stagedSettingsFile + " " + settingsDir + "/settings.json",
		"test -e " + layout.Workspace + "/demo",
		"git clone -- https://github.com/octo/demo.git " + layout.Workspace + "/demo",
		"test -e " + layout.Workspace + "/exists",
		"test -e " + layout.Workspace + "/other-demo",
		"git clone -- https://github.com/other/demo.git " + layout.Workspace + "/other-demo",
		"test -e " + layout.Workspace + "/missing",
		"git clone -- https://github.com/octo/missing.git " + layout.Workspace + "/missing",
		"claude plugin marketplace add octo/market",
		"claude plugin install p@gh",
	}, f.runner.commands())

	clone := f.runner.find(runPrefix(layout.Workspace, sandboxEnv(), "git", "clone"))
	require.NotEmpty(t, clone)
	assert.Contains(t, clone[0].String(), "--env HOME="+layout.Home, "the GitHub credential helper is in the real home's .gitconfig")
	assert.Contains(t, clone[0].String(), "--env GIT_TERMINAL_PROMPT=0")

	out := f.out.String()
	assert.Contains(t, out, "Skipping octo/exists")
	assert.Contains(t, out, "Warning: failed to clone octo/missing")

	// Setup runs between uploading the home and starting Claude Code.
	var order []string
	for _, c := range f.runner.calls {
		switch {
		case strings.HasPrefix(c.String(), "sandbox upload"):
			order = append(order, "upload")
		case c.Interactive:
			order = append(order, "claude")
		case strings.Contains(c.String(), " -- git clone"):
			order = append(order, "clone")
		case strings.Contains(c.String(), " -- claude plugin install"):
			order = append(order, "plugin")
		}
	}
	assert.Equal(t, []string{"upload", "upload", "upload", "clone", "clone", "clone", "plugin", "claude"}, order)
}

func TestManagerStartInvalidRepo(t *testing.T) {
	f := newManagerFixture(t)
	f.runner.responses["sandbox list"] = sandboxListJSON()
	err := f.m.Start(context.Background(), StartOptions{Name: "demo", Repos: []string{"bad"}})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid repository")
	assert.Empty(t, f.runner.find("sandbox create"), "nothing is created for an invalid --repo")
}

func TestManagerInstallPlugins(t *testing.T) {
	const known = `{
		"gh": {"source": {"source": "github", "repo": "octo/gh-market"}},
		"git": {"source": {"source": "git", "url": "https://example.com/m.git"}},
		"web": {"source": {"source": "url", "url": "https://example.com/marketplace.json"}},
		"dir": {"source": {"source": "directory", "path": "/src/market"}},
		"gitlab": {"source": {"source": "directory", "path": "/src/gitlab"}},
		"broken": {"source": {"source": "directory", "path": "/src/broken"}},
		"odd": {"source": {"source": "npm"}}
	}`
	origins := map[string]string{
		"/src/market": "git@github.com:octo/dir-market.git",
		"/src/gitlab": "git@gitlab.com:octo/market.git",
	}
	nilPlugins := []string(nil)

	tests := []struct {
		name         string
		plugins      []string // openshell.plugins
		hostSettings string   // host ~/.claude/settings.json; "" for none
		knownFile    string   // "" for no known_marketplaces.json
		failures     []string // sandbox commands that fail
		wantCommands []string
		wantOut      []string
	}{
		{
			name:         "host plugins when not configured",
			plugins:      nilPlugins,
			hostSettings: `{"enabledPlugins":{"b@dir":true,"a@gh":true,"off@gh":false,"odd-value@gh":["x"],"c@git":true,"d@web":true}}`,
			knownFile:    known,
			wantCommands: []string{
				"claude plugin marketplace add octo/gh-market",
				"claude plugin install a@gh",
				"claude plugin marketplace add octo/dir-market",
				"claude plugin install b@dir",
				"claude plugin marketplace add https://example.com/m.git",
				"claude plugin install c@git",
				"claude plugin marketplace add https://example.com/marketplace.json",
				"claude plugin install d@web",
			},
		},
		{
			name:         "explicit empty list installs none",
			plugins:      []string{},
			hostSettings: `{"enabledPlugins":{"a@gh":true}}`,
			knownFile:    known,
		},
		{
			name:         "explicit list replaces host plugins",
			plugins:      []string{"x@gh", "y@gh"},
			hostSettings: `{"enabledPlugins":{"a@gh":true}}`,
			knownFile:    known,
			wantCommands: []string{
				"claude plugin marketplace add octo/gh-market",
				"claude plugin install x@gh",
				"claude plugin install y@gh",
			},
		},
		{name: "no host settings", plugins: nilPlugins},
		{
			name:         "invalid host settings",
			plugins:      nilPlugins,
			hostSettings: `{`,
			wantOut:      []string{"Warning: cannot read the host's plugins"},
		},
		{
			name:    "no known marketplaces",
			plugins: []string{"a@gh"},
			wantOut: []string{"marketplace gh is not in the host's known_marketplaces.json", "skipping plugin a@gh"},
		},
		{
			name:      "invalid known marketplaces",
			plugins:   []string{"a@gh"},
			knownFile: `[`,
			wantOut:   []string{"Warning: parse ", "skipping plugin a@gh"},
		},
		{
			name:      "unresolvable marketplaces are skipped",
			plugins:   []string{"a@gitlab", "b@broken", "c@odd", "d@missing", "e@gh"},
			knownFile: known,
			wantCommands: []string{
				"claude plugin marketplace add octo/gh-market",
				"claude plugin install e@gh",
			},
			wantOut: []string{
				"origin remote of /src/gitlab (git@gitlab.com:octo/market.git) is not a GitHub repository",
				"cannot add marketplace broken: no origin",
				`unsupported marketplace source "npm"`,
				"marketplace missing is not in the host's known_marketplaces.json",
				"skipping plugin d@missing",
			},
		},
		{
			name:         "invalid plugin ID from the host",
			plugins:      nilPlugins,
			hostSettings: `{"enabledPlugins":{"bare":true,"trailing@":true}}`,
			knownFile:    known,
			wantOut:      []string{"skipping plugin bare: use name@marketplace", "skipping plugin trailing@"},
		},
		{
			name:      "failed marketplace add skips its plugins and is tried once",
			plugins:   []string{"a@gh", "b@gh", "c@git"},
			knownFile: known,
			failures:  []string{"claude plugin marketplace add octo/gh-market"},
			wantCommands: []string{
				"claude plugin marketplace add octo/gh-market",
				"claude plugin marketplace add https://example.com/m.git",
				"claude plugin install c@git",
			},
			wantOut: []string{"failed to add marketplace gh", "skipping plugin a@gh", "skipping plugin b@gh"},
		},
		{
			name:      "failed install continues with the next plugin",
			plugins:   []string{"a@gh", "b@gh"},
			knownFile: known,
			failures:  []string{"claude plugin install a@gh"},
			wantCommands: []string{
				"claude plugin marketplace add octo/gh-market",
				"claude plugin install a@gh",
				"claude plugin install b@gh",
			},
			wantOut: []string{"Warning: failed to install plugin a@gh"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newManagerFixture(t)
			f.m.Config.OpenShell.Plugins = tt.plugins
			f.m.GitOrigin = func(dir string) (string, error) {
				if o, ok := origins[dir]; ok {
					return o, nil
				}
				return "", errNoOrigin
			}
			if tt.hostSettings != "" {
				writeFile(t, filepath.Join(f.m.ClaudeDir, "settings.json"), tt.hostSettings)
			}
			if tt.knownFile != "" {
				writeFile(t, filepath.Join(f.m.ClaudeDir, "plugins", "known_marketplaces.json"), tt.knownFile)
			}
			for _, c := range tt.failures {
				f.runner.failures[runPrefix(layout.Home, sandboxEnv(), strings.Fields(c)...)] = errFake
			}

			f.m.installPlugins(context.Background(), "demo")

			assert.Equal(t, tt.wantCommands, f.runner.commands())
			for _, c := range f.runner.calls {
				assert.Contains(t, c.String(), "--workdir "+layout.Home)
				assert.Contains(t, c.String(), "--env HOME="+layout.Home)
			}
			for _, want := range tt.wantOut {
				assert.Contains(t, f.out.String(), want)
			}
		})
	}
}

var errNoOrigin = errors.New("no origin")

func TestGitHubRemotePattern(t *testing.T) {
	tests := []struct {
		remote string
		want   string
	}{
		{remote: "git@github.com:octo/market.git", want: "octo/market"},
		{remote: "git@github.com:octo/market", want: "octo/market"},
		{remote: "ssh://git@github.com/octo/market.git", want: "octo/market"},
		{remote: "https://github.com/octo/market.git", want: "octo/market"},
		{remote: "https://github.com/octo/market/", want: "octo/market"},
		{remote: "https://gitlab.com/octo/market.git"},
		{remote: "git@github.com:octo/market/extra.git"},
	}
	for _, tt := range tests {
		t.Run(tt.remote, func(t *testing.T) {
			match := githubRemotePattern.FindStringSubmatch(tt.remote)
			if tt.want == "" {
				assert.Nil(t, match)
				return
			}
			require.NotNil(t, match)
			assert.Equal(t, tt.want, match[1])
		})
	}
}

func TestMarketplaceAddSourceDefaultGitOrigin(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not installed")
	}
	repo := t.TempDir()
	for _, args := range [][]string{
		{"init", "-q"},
		{"remote", "add", "origin", "git@github.com:octo/dir-market.git"},
	} {
		out, err := exec.Command("git", append([]string{"-C", repo}, args...)...).CombinedOutput()
		require.NoError(t, err, string(out))
	}

	m := &Manager{}
	got, err := m.marketplaceAddSource(marketplaceSource{Source: "directory", Path: repo})
	require.NoError(t, err)
	assert.Equal(t, "octo/dir-market", got)

	_, err = m.marketplaceAddSource(marketplaceSource{Source: "directory", Path: t.TempDir()})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "read the origin remote")
}

func TestUploadHomeMergeFails(t *testing.T) {
	f := newManagerFixture(t)
	f.runner.responses["sandbox list"] = sandboxListJSON(sandboxJSON("demo", "Ready", true))
	f.runner.failures[runPrefix("", nil, "bash", "-c", mergeSettingsScript)] = errFake
	err := f.m.Connect(context.Background(), "demo", StartOptions{})
	require.ErrorIs(t, err, errFake)
	assert.Contains(t, err.Error(), "failed to update settings.json")
	assert.Empty(t, f.runner.find("sandbox exec -n demo --tty"))
}

// TestMergeSettingsScript runs the merge script against real files: the
// forge settings replace the sandbox's, except for the plugin keys Claude
// Code manages in the sandbox.
func TestMergeSettingsScript(t *testing.T) {
	if _, err := exec.LookPath("jq"); err != nil {
		t.Skip("jq is not installed")
	}
	tests := []struct {
		name    string
		current string // "" for no settings.json yet
		want    map[string]any
	}{
		{
			name: "first run",
			want: map[string]any{"theme": "new"},
		},
		{
			name:    "keeps sandbox plugins",
			current: `{"theme":"old","stale":1,"enabledPlugins":{"a@m":true},"extraKnownMarketplaces":{"m":{}}}`,
			want:    map[string]any{"theme": "new", "enabledPlugins": map[string]any{"a@m": true}, "extraKnownMarketplaces": map[string]any{"m": map[string]any{}}},
		},
		{
			name:    "no plugin keys",
			current: `{"theme":"old"}`,
			want:    map[string]any{"theme": "new"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			staged := filepath.Join(dir, stagedSettingsFile)
			current := filepath.Join(dir, "settings.json")
			writeFile(t, staged, `{"theme":"new"}`)
			if tt.current != "" {
				writeFile(t, current, tt.current)
			}

			out, err := exec.Command("bash", "-c", mergeSettingsScript, "bash", staged, current).CombinedOutput()
			require.NoError(t, err, string(out))

			data, err := os.ReadFile(current)
			require.NoError(t, err)
			var got map[string]any
			require.NoError(t, json.Unmarshal(data, &got))
			assert.Equal(t, tt.want, got)
			assert.NoFileExists(t, staged)
		})
	}

	t.Run("invalid current settings fail", func(t *testing.T) {
		dir := t.TempDir()
		staged := filepath.Join(dir, stagedSettingsFile)
		current := filepath.Join(dir, "settings.json")
		writeFile(t, staged, `{"theme":"new"}`)
		writeFile(t, current, `{`)
		assert.Error(t, exec.Command("bash", "-c", mergeSettingsScript, "bash", staged, current).Run())
	})
}

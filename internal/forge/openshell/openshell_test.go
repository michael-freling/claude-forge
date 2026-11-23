package openshell

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"

	"github.com/michael-freling/claude-forge/internal/forge/auth"
	"github.com/michael-freling/claude-forge/internal/forge/config"
	"github.com/michael-freling/claude-forge/internal/forge/layout"
	"github.com/michael-freling/claude-forge/internal/forge/openshell/spec"
)

func TestMemory(t *testing.T) {
	network := "Only some hosts are reachable: Anthropic and api.example.com."

	noToken := Memory(network, false, nil)
	assert.Contains(t, noToken, layout.Workspace)
	assert.Contains(t, noToken, "## Network access is an allowlist\n\n"+network+"\nOther hosts fail")
	assert.Contains(t, noToken, "only public repositories")
	assert.NotContains(t, noToken, "$GITHUB_TOKEN")

	token := Memory(network, true, nil)
	assert.Contains(t, token, "$GITHUB_TOKEN")
	assert.NotContains(t, token, "allowed only for these repositories")

	scoped := Memory(network, true, []string{"octo/demo"})
	assert.Contains(t, scoped, "- octo/demo")
}

func TestManagerNetworkSummary(t *testing.T) {
	tests := []struct {
		name      string
		openshell config.OpenShellConfig
		want      string
	}{
		{
			name: "generated policy",
			openshell: config.OpenShellConfig{Policy: spec.PolicyConfig{
				Allow:   []string{"api.example.com"},
				Disable: []string{"aws"},
			}},
			want: spec.DescribeNetwork(spec.PolicyConfig{Allow: []string{"api.example.com"}, Disable: []string{"aws"}}),
		},
		{
			name:      "policy file",
			openshell: config.OpenShellConfig{PolicyFile: "/policy.yaml"},
			want:      "The user's own sandbox policy decides which hosts are reachable; most are not.",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newManagerFixture(t)
			f.m.Config.OpenShell = tt.openshell
			assert.Equal(t, tt.want, f.m.networkSummary())
		})
	}
}

func TestGitconfig(t *testing.T) {
	got := Gitconfig(HomeOptions{GitUserName: "Octo", GitUserEmail: "octo@example.com", GitHubToken: true})
	assert.Contains(t, got, "name = Octo")
	assert.Contains(t, got, "email = octo@example.com")
	assert.Contains(t, got, `[credential "https://github.com"]`)
	assert.Contains(t, got, "password=$GITHUB_TOKEN")

	bare := Gitconfig(HomeOptions{})
	assert.NotContains(t, bare, "[user]")
	assert.NotContains(t, bare, "credential")
	assert.Contains(t, bare, "defaultBranch = main")

	nameOnly := Gitconfig(HomeOptions{GitUserName: "Octo"})
	assert.Contains(t, nameOnly, "name = Octo")
	assert.NotContains(t, nameOnly, "email")
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
	require.NoError(t, os.WriteFile(path, []byte(content), 0o644))
}

func TestStageHome(t *testing.T) {
	host := t.TempDir()
	claudeDir := filepath.Join(host, ".claude")
	configDir := filepath.Join(host, "config")
	writeFile(t, filepath.Join(claudeDir, "CLAUDE.md"), "host instructions")
	writeFile(t, filepath.Join(claudeDir, "rules", "go.md"), "rule")
	writeFile(t, filepath.Join(host, "dotfiles", "skill", "SKILL.md"), "skill")
	writeFile(t, filepath.Join(host, "dotfiles", "agent.md"), "agent")
	require.NoError(t, os.MkdirAll(filepath.Join(claudeDir, "skills"), 0o755))
	require.NoError(t, os.Symlink(filepath.Join(host, "dotfiles", "skill"), filepath.Join(claudeDir, "skills", "linked")))
	require.NoError(t, os.MkdirAll(filepath.Join(claudeDir, "agents"), 0o755))
	require.NoError(t, os.Symlink(filepath.Join(host, "dotfiles", "agent.md"), filepath.Join(claudeDir, "agents", "agent.md")))
	require.NoError(t, os.Symlink(filepath.Join(host, "missing"), filepath.Join(claudeDir, "agents", "broken.md")))
	writeFile(t, filepath.Join(configDir, "settings.json"), `{"theme":"x","mcpServers":{"a":{}},"enabledPlugins":{},"env":{"OTEL_X":"1","CLAUDE_CODE_ENABLE_TELEMETRY":"1","KEEP":"y"}}`)
	writeFile(t, filepath.Join(host, ".claude.json"), `{"theme":"light"}`)

	t.Run("first run", func(t *testing.T) {
		stage := t.TempDir()
		require.NoError(t, StageHome(stage, HomeOptions{
			ClaudeDir: claudeDir, ConfigDir: configDir, HomeDir: host,
			FirstRun: true, GitHubToken: true, GitUserName: "Octo",
		}))

		memory, err := os.ReadFile(filepath.Join(stage, ".claude", "CLAUDE.md"))
		require.NoError(t, err)
		assert.True(t, strings.HasPrefix(string(memory), "# OpenShell sandbox"))
		assert.Contains(t, string(memory), "host instructions")

		for _, p := range []string{"rules/go.md", "skills/linked/SKILL.md", "agents/agent.md"} {
			assert.FileExists(t, filepath.Join(stage, ".claude", p))
		}
		assert.NoFileExists(t, filepath.Join(stage, ".claude", "agents", "broken.md"))

		var settings map[string]any
		data, err := os.ReadFile(filepath.Join(stage, ".claude", stagedSettingsFile))
		require.NoError(t, err)
		require.NoError(t, json.Unmarshal(data, &settings))
		assert.NotContains(t, settings, "mcpServers")
		assert.NotContains(t, settings, "enabledPlugins")
		assert.Equal(t, map[string]any{"KEEP": "y"}, settings["env"])

		var userConfig map[string]any
		data, err = os.ReadFile(filepath.Join(stage, ".claude.json"))
		require.NoError(t, err)
		require.NoError(t, json.Unmarshal(data, &userConfig))
		assert.Equal(t, "light", userConfig["theme"])
		assert.Contains(t, userConfig["projects"], layout.Workspace)

		gitconfig, err := os.ReadFile(filepath.Join(stage, ".gitconfig"))
		require.NoError(t, err)
		assert.Contains(t, string(gitconfig), "name = Octo")
	})

	t.Run("reconnect leaves runtime files alone", func(t *testing.T) {
		stage := t.TempDir()
		require.NoError(t, StageHome(stage, HomeOptions{ClaudeDir: claudeDir, ConfigDir: configDir, HomeDir: host}))
		assert.NoFileExists(t, filepath.Join(stage, ".claude.json"))
		assert.NoFileExists(t, filepath.Join(stage, ".gitconfig"))
		assert.FileExists(t, filepath.Join(stage, ".claude", stagedSettingsFile))
	})

	t.Run("defaults without host files", func(t *testing.T) {
		stage := t.TempDir()
		empty := t.TempDir()
		require.NoError(t, StageHome(stage, HomeOptions{ClaudeDir: empty, ConfigDir: empty, HomeDir: empty, FirstRun: true}))
		var settings map[string]any
		data, err := os.ReadFile(filepath.Join(stage, ".claude", stagedSettingsFile))
		require.NoError(t, err)
		require.NoError(t, json.Unmarshal(data, &settings))
		assert.Equal(t, "disabled", settings["autoUpdaterStatus"])
		assert.Equal(t, "dark", hostTheme(empty))
	})

	t.Run("settings with only telemetry env drop env", func(t *testing.T) {
		cfg := t.TempDir()
		writeFile(t, filepath.Join(cfg, "settings.json"), `{"env":{"OTEL_A":"1"}}`)
		data, err := sandboxSettings(cfg)
		require.NoError(t, err)
		assert.NotContains(t, string(data), "env")
	})

	t.Run("unwritable stage dir", func(t *testing.T) {
		blocker := filepath.Join(t.TempDir(), "file")
		writeFile(t, blocker, "")
		assert.Error(t, StageHome(blocker, HomeOptions{}))
	})
}

func TestHasCredentialKey(t *testing.T) {
	out := "Provider:\n\n  Name: x\n  Credential keys: ANTHROPIC_API_KEY, OTHER\n"
	assert.True(t, hasCredentialKey(out, "ANTHROPIC_API_KEY"))
	assert.True(t, hasCredentialKey(out, "OTHER"))
	assert.False(t, hasCredentialKey(out, "CLAUDE_CODE_OAUTH_TOKEN"))
}

func TestValidateName(t *testing.T) {
	for _, ok := range []string{"a", "forge-1", "abc123"} {
		assert.NoError(t, ValidateName(ok), ok)
	}
	for _, bad := range []string{"", "Upper", "-x", "x-", "a_b", strings.Repeat("a", 64)} {
		assert.Error(t, ValidateName(bad), bad)
	}
}

type managerFixture struct {
	runner *fakeRunner
	m      *Manager
	out    *bytes.Buffer
	env    map[string]string
	claude *auth.Credentials // nil: no Claude login on the host
}

func newManagerFixture(t *testing.T) *managerFixture {
	t.Helper()
	runner := newFakeRunner()
	host := t.TempDir()
	cfg := config.DefaultConfig()
	runner.responses["profile export"] = "id: x\nresource_version: 3\n"
	f := &managerFixture{
		runner: runner, out: &bytes.Buffer{}, env: map[string]string{},
		claude: &auth.Credentials{AuthType: "oauth", Token: "oauth-secret"},
	}
	f.m = &Manager{
		Client:    &Client{Runner: runner, TempDir: t.TempDir()},
		Config:    cfg,
		ClaudeDir: filepath.Join(host, ".claude"),
		ConfigDir: filepath.Join(host, "config"),
		HomeDir:   host,
		Out:       f.out,
		LookupEnv: func(k string) (string, bool) { v, ok := f.env[k]; return v, ok },
		ResolveClaude: func() (*auth.Credentials, error) {
			if f.claude == nil {
				return nil, errFake
			}
			return f.claude, nil
		},
		GitIdentity: func() (string, string) { return "Octo", "octo@example.com" },
		ClaudeModel: func() string { return "opus" },
	}
	return f
}

// missingProviders makes `provider get` fail, as for providers not created yet.
func (f *managerFixture) missingProviders() {
	f.runner.failures["provider get"] = errFake
}

func TestManagerStart(t *testing.T) {
	t.Run("creates sandbox with providers, uploads home and runs claude", func(t *testing.T) {
		f := newManagerFixture(t)
		f.missingProviders()
		f.runner.failures["profile export"] = errFake
		f.runner.responses["sandbox list"] = sandboxListJSON()
		f.env[spec.EnvClaudeOAuthToken] = "oauth-secret"
		f.env[spec.EnvGitHubToken] = "gh-secret"

		require.NoError(t, f.m.Start(context.Background(), StartOptions{Name: "demo", Prompt: "hi", SkipPermissions: true}))

		imports := f.runner.find("profile import")
		require.Len(t, imports, 2)
		assert.NotContains(t, imports[0].File, "resource_version")
		creates := f.runner.find("provider create")
		require.Len(t, creates, 2)
		for _, c := range creates {
			for _, a := range c.Args {
				assert.NotContains(t, a, "secret", "secrets must not be passed as arguments")
			}
		}
		assert.Equal(t, []string{spec.EnvClaudeOAuthToken + "=oauth-secret"}, creates[0].Env)
		assert.Equal(t, []string{spec.EnvGitHubToken + "=gh-secret"}, creates[1].Env)

		create := f.runner.find("sandbox create")
		require.Len(t, create, 1)
		args := create[0].String()
		assert.Contains(t, args, "--provider "+ClaudeProviderName)
		assert.Contains(t, args, "--provider "+GitHubProviderName)
		assert.Contains(t, args, "--label claude-forge=true")
		assert.Contains(t, args, "--from "+f.m.Config.Images.Agent)
		assert.True(t, strings.HasSuffix(args, "-- sleep infinity"))

		uploads := f.runner.find("sandbox upload demo")
		var dests []string
		for _, u := range uploads {
			dests = append(dests, u.Args[4])
		}
		assert.ElementsMatch(t, []string{layout.Home, layout.Home + "/.claude.json", layout.Home + "/.gitconfig"}, dests)

		execs := f.runner.find("sandbox exec -n demo --tty")
		require.Len(t, execs, 1)
		assert.True(t, execs[0].Interactive)
		e := execs[0].String()
		assert.Contains(t, e, "--workdir "+layout.Workspace)
		assert.Contains(t, e, "--env HOME="+layout.Home)
		assert.Contains(t, e, "--env ANTHROPIC_MODEL=opus")
		assert.True(t, strings.HasSuffix(e, "-- claude --dangerously-skip-permissions --name demo hi"), e)
		assert.Contains(t, f.out.String(), "openshell connect demo")
	})

	t.Run("reuses existing providers and profiles", func(t *testing.T) {
		f := newManagerFixture(t)
		f.runner.responses["sandbox list"] = sandboxListJSON()
		f.runner.responses["provider get"] = "  Credential keys: " + spec.EnvClaudeOAuthToken + "\n"
		f.m.Config.OpenShell.Image = "custom:1"
		f.m.Config.Defaults.Agent = "reviewer"

		require.NoError(t, f.m.Start(context.Background(), StartOptions{Name: "demo"}))
		updates := f.runner.find("profile update")
		require.Len(t, updates, 2)
		assert.Contains(t, updates[0].File, "resource_version: 3")
		assert.Empty(t, f.runner.find("provider create"))
		assert.Len(t, f.runner.find("provider update "+ClaudeProviderName), 1, "the Claude token is refreshed on every start")
		e := f.runner.find("sandbox exec -n demo --tty")[0].String()
		assert.Contains(t, e, "-- claude --agent reviewer --name demo")
		assert.Contains(t, f.runner.find("sandbox create")[0].String(), "--from custom:1")
	})

	t.Run("uses the policy file", func(t *testing.T) {
		f := newManagerFixture(t)
		f.runner.responses["sandbox list"] = sandboxListJSON()
		policy := "version: 1\nnetwork_policies: {}\n"
		f.m.Config.OpenShell.PolicyFile = filepath.Join(t.TempDir(), "policy.yaml")
		writeFile(t, f.m.Config.OpenShell.PolicyFile, policy)

		require.NoError(t, f.m.Start(context.Background(), StartOptions{Name: "demo"}))
		create := f.runner.find("sandbox create")
		require.Len(t, create, 1)
		assert.Equal(t, policy, create[0].File)
	})

	t.Run("no GitHub token", func(t *testing.T) {
		f := newManagerFixture(t)
		f.missingProviders()
		f.runner.responses["sandbox list"] = sandboxListJSON()
		f.env[spec.EnvAnthropicAPIKey] = "key"
		f.m.GitHubCLIToken = func(context.Context) (string, error) { return "", errFake }

		require.NoError(t, f.m.Start(context.Background(), StartOptions{Name: "demo"}))
		args := f.runner.find("sandbox create")[0].String()
		assert.NotContains(t, args, GitHubProviderName)
		assert.Contains(t, f.out.String(), "only public repositories")
	})

	t.Run("GitHub token from gh CLI", func(t *testing.T) {
		f := newManagerFixture(t)
		f.missingProviders()
		f.runner.responses["sandbox list"] = sandboxListJSON()
		f.env[spec.EnvAnthropicAPIKey] = "key"
		f.m.GitHubCLIToken = func(context.Context) (string, error) { return "gho_x\n", nil }

		require.NoError(t, f.m.Start(context.Background(), StartOptions{Name: "demo"}))
		creates := f.runner.find("provider create --name " + GitHubProviderName)
		require.Len(t, creates, 1)
		assert.Equal(t, []string{spec.EnvGitHubToken + "=gho_x"}, creates[0].Env)
	})

	t.Run("errors", func(t *testing.T) {
		tests := []struct {
			name    string
			setup   func(f *managerFixture)
			start   StartOptions
			wantErr string
		}{
			{name: "invalid name", start: StartOptions{Name: "Bad"}, wantErr: "invalid sandbox name"},
			{
				name: "already exists",
				setup: func(f *managerFixture) {
					f.runner.responses["sandbox list"] = sandboxListJSON(sandboxJSON("demo", "Ready", true))
				},
				wantErr: "already exists",
			},
			{
				name:    "list fails",
				setup:   func(f *managerFixture) { f.runner.failures["sandbox list"] = errFake },
				wantErr: errFake.Error(),
			},
			{
				name: "no Claude login on the host",
				setup: func(f *managerFixture) {
					f.claude = nil
					f.runner.responses["sandbox list"] = sandboxListJSON()
				},
				wantErr: "log in with 'claude'",
			},
			{
				name: "profile import fails",
				setup: func(f *managerFixture) {
					f.runner.responses["sandbox list"] = sandboxListJSON()
					f.runner.failures["profile"] = errFake
				},
				wantErr: errFake.Error(),
			},
			{
				name: "profile export without version",
				setup: func(f *managerFixture) {
					f.runner.responses["sandbox list"] = sandboxListJSON()
					f.runner.responses["profile export"] = "id: x\n"
				},
				wantErr: "failed to read the version",
			},
			{
				name: "create fails",
				setup: func(f *managerFixture) {
					f.runner.responses["sandbox list"] = sandboxListJSON()
					f.runner.failures["sandbox create"] = errFake
				},
				wantErr: errFake.Error(),
			},
			{
				name: "upload fails",
				setup: func(f *managerFixture) {
					f.runner.responses["sandbox list"] = sandboxListJSON()
					f.runner.failures["sandbox upload"] = errFake
				},
				wantErr: errFake.Error(),
			},
			{
				name: "provider create fails",
				setup: func(f *managerFixture) {
					f.missingProviders()
					f.env[spec.EnvClaudeOAuthToken] = "t"
					f.runner.responses["sandbox list"] = sandboxListJSON()
					f.runner.failures["provider create"] = errFake
				},
				wantErr: errFake.Error(),
			},
			{
				name: "GitHub provider create fails",
				setup: func(f *managerFixture) {
					f.missingProviders()
					f.env[spec.EnvClaudeOAuthToken] = "t"
					f.env["GH_TOKEN"] = "g"
					f.runner.responses["sandbox list"] = sandboxListJSON()
					f.runner.failures["provider create --name "+GitHubProviderName] = errFake
				},
				wantErr: errFake.Error(),
			},
		}
		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				f := newManagerFixture(t)
				if tt.setup != nil {
					tt.setup(f)
				}
				start := tt.start
				if start.Name == "" {
					start.Name = "demo"
				}
				err := f.m.Start(context.Background(), start)
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.wantErr)
			})
		}
	})
}

func TestManagerConnect(t *testing.T) {
	tests := []struct {
		name      string
		phase     string
		wantStart bool
		wantErr   string
	}{
		{name: "ready", phase: "Ready"},
		{name: "stopped is started first", phase: "Stopped", wantStart: true},
		{name: "completed cannot reconnect", phase: "Completed", wantErr: "cannot be reconnected"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newManagerFixture(t)
			f.runner.responses["sandbox list"] = sandboxListJSON(sandboxJSON("demo", tt.phase, true))

			err := f.m.Connect(context.Background(), "demo", StartOptions{SkipPermissions: true})
			if tt.wantErr != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.wantErr)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.wantStart, len(f.runner.find("sandbox start demo")) == 1)

			for _, u := range f.runner.find("sandbox upload") {
				assert.Equal(t, layout.Home, u.Args[4], "reconnect only refreshes ~/.claude")
			}
			e := f.runner.find("sandbox exec -n demo --tty")[0]
			assert.Equal(t, []string{"bash", "-c"}, e.Args[len(e.Args)-5:len(e.Args)-3])
			assert.Contains(t, e.Args[len(e.Args)-3], "claude --continue")
			assert.Equal(t, []string{"claude", "--dangerously-skip-permissions"}, e.Args[len(e.Args)-2:])
		})
	}

	t.Run("not a claude-forge sandbox", func(t *testing.T) {
		f := newManagerFixture(t)
		f.runner.responses["sandbox list"] = sandboxListJSON(sandboxJSON("demo", "Ready", false))
		err := f.m.Connect(context.Background(), "demo", StartOptions{})
		assert.ErrorIs(t, err, ErrNotFound)
	})

	t.Run("start fails", func(t *testing.T) {
		f := newManagerFixture(t)
		f.runner.responses["sandbox list"] = sandboxListJSON(sandboxJSON("demo", "Stopped", true))
		f.runner.failures["sandbox start"] = errFake
		assert.ErrorIs(t, f.m.Connect(context.Background(), "demo", StartOptions{}), errFake)
	})

	t.Run("upload fails", func(t *testing.T) {
		f := newManagerFixture(t)
		f.runner.responses["sandbox list"] = sandboxListJSON(sandboxJSON("demo", "Ready", true))
		f.runner.failures["sandbox upload"] = errFake
		assert.ErrorIs(t, f.m.Connect(context.Background(), "demo", StartOptions{}), errFake)
	})
}

func TestManagerAuth(t *testing.T) {
	t.Run("updates matching credential in place", func(t *testing.T) {
		f := newManagerFixture(t)
		f.runner.responses["provider get"] = "  Credential keys: " + spec.EnvClaudeOAuthToken + "\n"
		f.env[spec.EnvClaudeOAuthToken] = "new"
		f.env[spec.EnvGitHubToken] = "gh"
		require.NoError(t, f.m.Auth(context.Background()))
		assert.Len(t, f.runner.find("provider update "+ClaudeProviderName+" --credential "+spec.EnvClaudeOAuthToken), 1)
		assert.Empty(t, f.runner.find("provider delete "+ClaudeProviderName))
		// The GitHub provider's key differs, so it is recreated.
		assert.Len(t, f.runner.find("provider delete "+GitHubProviderName), 1)
		assert.Len(t, f.runner.find("provider create --name "+GitHubProviderName), 1)
	})

	t.Run("delete in use fails", func(t *testing.T) {
		f := newManagerFixture(t)
		f.runner.responses["provider get"] = "  Credential keys: " + spec.EnvAnthropicAPIKey + "\n"
		f.runner.failures["provider delete"] = errFake
		f.env[spec.EnvClaudeOAuthToken] = "new"
		err := f.m.Auth(context.Background())
		require.Error(t, err)
		assert.Contains(t, err.Error(), "delete the sandboxes using it first")
	})
}

func TestManagerListStopDelete(t *testing.T) {
	f := newManagerFixture(t)
	f.runner.responses["sandbox list"] = sandboxListJSON(
		sandboxJSON("demo", "Ready", true),
		sandboxJSON("other", "Ready", false),
	)
	var out bytes.Buffer
	require.NoError(t, f.m.List(context.Background(), &out))
	assert.Contains(t, out.String(), "demo")
	assert.NotContains(t, out.String(), "other")

	require.NoError(t, f.m.Stop(context.Background(), "demo"))
	assert.Len(t, f.runner.find("sandbox stop demo"), 1)
	require.NoError(t, f.m.Delete(context.Background(), "demo"))
	assert.Len(t, f.runner.find("sandbox delete demo"), 1)

	assert.ErrorIs(t, f.m.Stop(context.Background(), "other"), ErrNotFound)
	assert.ErrorIs(t, f.m.Delete(context.Background(), "other"), ErrNotFound)

	empty := newManagerFixture(t)
	empty.runner.responses["sandbox list"] = sandboxListJSON()
	out.Reset()
	require.NoError(t, empty.m.List(context.Background(), &out))
	assert.Contains(t, out.String(), "No claude-forge sandboxes")

	broken := newManagerFixture(t)
	broken.runner.responses["sandbox list"] = "not json"
	assert.Error(t, broken.m.List(context.Background(), &out))
	broken.runner.failures["sandbox list"] = errFake
	assert.ErrorIs(t, broken.m.List(context.Background(), &out), errFake)
}

func TestManagerPolicyYAML(t *testing.T) {
	t.Run("generated", func(t *testing.T) {
		f := newManagerFixture(t)
		with, err := f.m.PolicyYAML(true)
		require.NoError(t, err)
		without, err := f.m.PolicyYAML(false)
		require.NoError(t, err)
		assert.NotContains(t, string(with), "api.github.com")
		assert.Contains(t, string(without), "api.github.com")
	})

	t.Run("generated with extensions", func(t *testing.T) {
		f := newManagerFixture(t)
		f.m.Config.OpenShell.Policy = spec.PolicyConfig{
			Allow:    []string{"db.example.com:5432"},
			ReadOnly: []string{"docs.example.com"},
			Disable:  []string{"aws"},
		}
		data, err := f.m.PolicyYAML(true)
		require.NoError(t, err)
		var p spec.Policy
		require.NoError(t, yaml.Unmarshal(data, &p))
		assert.NotContains(t, p.NetworkPolicies, "aws")
		assert.Equal(t, []spec.Endpoint{{Host: "db.example.com", Port: 5432}}, p.NetworkPolicies["custom_allow"].Endpoints)
		require.Len(t, p.NetworkPolicies["custom_read_only"].Endpoints, 1)
		assert.Equal(t, "read-only", p.NetworkPolicies["custom_read_only"].Endpoints[0].Access)
	})

	t.Run("invalid extension", func(t *testing.T) {
		f := newManagerFixture(t)
		f.m.Config.OpenShell.Policy.Allow = []string{"bad host"}
		_, err := f.m.PolicyYAML(true)
		assert.Error(t, err)
	})

	t.Run("policy file verbatim", func(t *testing.T) {
		f := newManagerFixture(t)
		policy := "# mine\nversion: 1\nnetwork_policies:\n  x:\n    endpoints:\n      - host: example.com\n        port: 443\n"
		f.m.Config.OpenShell.PolicyFile = filepath.Join(t.TempDir(), "policy.yaml")
		writeFile(t, f.m.Config.OpenShell.PolicyFile, policy)
		data, err := f.m.PolicyYAML(true)
		require.NoError(t, err)
		assert.Equal(t, policy, string(data))
		assert.NotContains(t, f.out.String(), "Warning")
	})

	t.Run("policy file with provider host warns", func(t *testing.T) {
		f := newManagerFixture(t)
		policy := "network_policies:\n  x:\n    endpoints:\n      - host: \"**.github.com\"\n        port: 443\n"
		f.m.Config.OpenShell.PolicyFile = filepath.Join(t.TempDir(), "policy.yaml")
		writeFile(t, f.m.Config.OpenShell.PolicyFile, policy)
		data, err := f.m.PolicyYAML(true)
		require.NoError(t, err)
		assert.Equal(t, policy, string(data))
		assert.Contains(t, f.out.String(), "Warning: openshell.policy_file "+f.m.Config.OpenShell.PolicyFile+": network_policies.x: **.github.com:443 overlaps")
	})

	t.Run("policy file that does not match the schema warns", func(t *testing.T) {
		f := newManagerFixture(t)
		policy := "network_policies: [1, 2]\n"
		f.m.Config.OpenShell.PolicyFile = filepath.Join(t.TempDir(), "policy.yaml")
		writeFile(t, f.m.Config.OpenShell.PolicyFile, policy)
		data, err := f.m.PolicyYAML(true)
		require.NoError(t, err)
		assert.Equal(t, policy, string(data))
		assert.Contains(t, f.out.String(), "could not check openshell.policy_file")
	})

	t.Run("missing policy file", func(t *testing.T) {
		f := newManagerFixture(t)
		f.m.Config.OpenShell.PolicyFile = filepath.Join(t.TempDir(), "missing.yaml")
		_, err := f.m.PolicyYAML(true)
		assert.ErrorContains(t, err, "openshell.policy_file")
	})
}

func TestExecRunner(t *testing.T) {
	dir := t.TempDir()
	script := filepath.Join(dir, "openshell")
	writeFile(t, script, "#!/bin/sh\necho \"args=$* gw=$OPENSHELL_GATEWAY secret=$MY_SECRET\"\n[ \"$1\" = fail ] && exit 3\nexit 0\n")
	require.NoError(t, os.Chmod(script, 0o755))

	r := ExecRunner{Binary: script, Gateway: "gw1"}
	out, err := r.Output(context.Background(), []string{"MY_SECRET=s"}, "sandbox", "list")
	require.NoError(t, err)
	assert.Equal(t, "args=sandbox list gw=gw1 secret=s\n", string(out))

	_, err = r.Output(context.Background(), nil, "fail", "x", "y")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "openshell fail x")

	assert.NoError(t, r.Interactive(context.Background(), "ok"))
	assert.Error(t, r.Interactive(context.Background(), "fail"))

	assert.Equal(t, "openshell", ExecRunner{}.command(context.Background(), nil, nil).Args[0])
}

func TestClientWriteTempError(t *testing.T) {
	c := &Client{Runner: newFakeRunner(), TempDir: filepath.Join(t.TempDir(), "missing")}
	assert.Error(t, c.EnsureProfile(context.Background(), spec.ClaudeProfile()))
	assert.Error(t, c.CreateSandbox(context.Background(), CreateOptions{Name: "x"}))
	assert.Error(t, c.uploadHome(context.Background(), "x", filepath.Join(t.TempDir(), "missing"), layout.Home))
}

func TestCreateSandboxEnv(t *testing.T) {
	r := newFakeRunner()
	c := &Client{Runner: r, TempDir: t.TempDir()}
	require.NoError(t, c.CreateSandbox(context.Background(), CreateOptions{Name: "x", Image: "img", Env: map[string]string{"B": "2", "A": "1"}}))
	assert.Contains(t, r.calls[0].String(), "--env A=1 --env B=2 --detach")
}

func TestManagerClaudeCredential(t *testing.T) {
	t.Run("API key", func(t *testing.T) {
		f := newManagerFixture(t)
		f.claude = &auth.Credentials{AuthType: "api_key", Token: "sk-key"}
		key, value, err := f.m.claudeCredential()
		require.NoError(t, err)
		assert.Equal(t, spec.EnvAnthropicAPIKey, key)
		assert.Equal(t, "sk-key", value)
	})

	t.Run("defaults to the host login claude-forge already uses", func(t *testing.T) {
		t.Setenv("ANTHROPIC_API_KEY", "")
		t.Setenv("CLAUDE_CODE_OAUTH_TOKEN", "")
		f := newManagerFixture(t)
		f.m.ResolveClaude = nil
		writeFile(t, filepath.Join(f.m.ClaudeDir, ".credentials.json"), `{"claudeAiOauth":{"accessToken":"sk-ant-oat01-host","refreshToken":"never-sent"}}`)
		key, value, err := f.m.claudeCredential()
		require.NoError(t, err)
		assert.Equal(t, spec.EnvClaudeOAuthToken, key)
		assert.Equal(t, "sk-ant-oat01-host", value)
	})

	t.Run("connect refreshes the Claude provider", func(t *testing.T) {
		f := newManagerFixture(t)
		f.runner.responses["sandbox list"] = sandboxListJSON(sandboxJSON("demo", "Ready", true))
		f.runner.responses["provider get"] = "  Credential keys: " + spec.EnvClaudeOAuthToken + "\n"
		require.NoError(t, f.m.Connect(context.Background(), "demo", StartOptions{}))
		updates := f.runner.find("provider update " + ClaudeProviderName)
		require.Len(t, updates, 1)
		assert.Equal(t, []string{spec.EnvClaudeOAuthToken + "=oauth-secret"}, updates[0].Env)
	})

	t.Run("connect fails without a host login", func(t *testing.T) {
		f := newManagerFixture(t)
		f.claude = nil
		f.runner.responses["sandbox list"] = sandboxListJSON(sandboxJSON("demo", "Ready", true))
		assert.ErrorIs(t, f.m.Connect(context.Background(), "demo", StartOptions{}), errFake)
	})
}

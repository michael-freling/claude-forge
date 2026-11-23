package openshell

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/michael-freling/claude-forge/internal/forge/auth"
	"github.com/michael-freling/claude-forge/internal/forge/layout"
	"github.com/michael-freling/claude-forge/internal/forge/openshell/spec"
)

// hostLoginToken is the host's real access token in fullLogin: it must never
// reach the sandbox or an openshell command line.
const hostLoginToken = "sk-ant-oat01-host-secret"

// fullLogin switches the host credential to a 'claude' login and gives the
// host a ~/.claude.json account with personal fields.
func (f *managerFixture) fullLogin(t *testing.T) {
	t.Helper()
	f.claude = &auth.Credentials{
		AuthType:         "oauth",
		Token:            hostLoginToken,
		Scopes:           []string{"user:inference", "user:sessions:claude_code"},
		SubscriptionType: "max",
	}
	writeFile(t, filepath.Join(f.m.HomeDir, ".claude.json"), `{"theme":"light","oauthAccount":{
		"accountUuid":"acc-1","organizationUuid":"org-1","organizationRole":"admin",
		"emailAddress":"octo@example.com","displayName":"Octo","fullName":"Octo Cat"}}`)
}

// assertNoSecret fails if secret shows up in any openshell argument or in a
// file passed to openshell: provider env vars are the only place for it.
func assertNoSecret(t *testing.T, r *fakeRunner, secret string) {
	t.Helper()
	for _, c := range r.calls {
		assert.NotContains(t, c.String(), secret)
		assert.NotContains(t, c.File, secret)
	}
}

func TestManagerClaudeLogin(t *testing.T) {
	loginRun := runPrefix("", nil, "bash", "-c", loginScript)
	tests := []struct {
		name      string
		setup     func(t *testing.T, f *managerFixture)
		wantLogin bool
	}{
		{name: "claude login", setup: func(t *testing.T, f *managerFixture) { f.fullLogin(t) }, wantLogin: true},
		{name: "API key", setup: func(_ *testing.T, f *managerFixture) {
			f.claude = &auth.Credentials{AuthType: "api_key", Token: "sk-key", FromEnv: true}
		}},
		{name: "setup-token from the environment", setup: func(*testing.T, *managerFixture) {}},
	}
	for _, tt := range tests {
		for _, resume := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s resume=%v", tt.name, resume), func(t *testing.T) {
				f := newManagerFixture(t)
				tt.setup(t, f)
				var err error
				if resume {
					f.runner.responses["sandbox list"] = sandboxListJSON(sandboxJSON("demo", "Ready", true))
					err = f.m.Connect(context.Background(), "demo", StartOptions{})
				} else {
					f.runner.responses["sandbox list"] = sandboxListJSON()
					err = f.m.Start(context.Background(), StartOptions{Name: "demo"})
				}
				require.NoError(t, err)

				logins := f.runner.find(loginRun)
				execs := f.runner.find("sandbox exec -n demo --tty")
				require.Len(t, execs, 1)
				_, command, _ := strings.Cut(execs[0].String(), " -- ")
				if !tt.wantLogin {
					assert.Empty(t, logins)
					assert.False(t, strings.HasPrefix(command, "env "), command)
					return
				}
				require.Len(t, logins, 1, "the login is rewritten on every start and connect")
				home := layout.Home
				assert.Equal(t, []string{
					home + "/.claude/" + stagedOAuthAccountFile,
					home + "/.claude.json",
					home + "/.claude/.credentials.json",
					`["user:inference","user:sessions:claude_code"]`,
					"max",
				}, logins[0].Args[len(logins[0].Args)-5:])
				assert.True(t, strings.HasPrefix(command, "env -u "+spec.EnvClaudeOAuthToken+" "), command)
				assertNoSecret(t, f.runner, hostLoginToken)
			})
		}
	}

	t.Run("login write fails", func(t *testing.T) {
		f := newManagerFixture(t)
		f.fullLogin(t)
		f.runner.responses["sandbox list"] = sandboxListJSON(sandboxJSON("demo", "Ready", true))
		f.runner.failures[loginRun] = errFake
		err := f.m.Connect(context.Background(), "demo", StartOptions{})
		require.ErrorIs(t, err, errFake)
		assert.Contains(t, err.Error(), "failed to write the Claude login")
		assert.Empty(t, f.runner.find("sandbox exec -n demo --tty"))
	})
}

func TestManagerRemoteControl(t *testing.T) {
	t.Run("start", func(t *testing.T) {
		f := newManagerFixture(t)
		f.fullLogin(t)
		f.runner.responses["sandbox list"] = sandboxListJSON()
		require.NoError(t, f.m.Start(context.Background(), StartOptions{Name: "demo", Prompt: "hi", RemoteControl: true}))
		e := f.runner.find("sandbox exec -n demo --tty")[0].String()
		assert.True(t, strings.HasSuffix(e, "-- env -u "+spec.EnvClaudeOAuthToken+" claude --remote-control demo --name demo hi"), e)
	})

	t.Run("connect", func(t *testing.T) {
		f := newManagerFixture(t)
		f.fullLogin(t)
		f.runner.responses["sandbox list"] = sandboxListJSON(sandboxJSON("demo", "Ready", true))
		require.NoError(t, f.m.Connect(context.Background(), "demo", StartOptions{RemoteControl: true}))
		e := f.runner.find("sandbox exec -n demo --tty")[0]
		assert.Equal(t, []string{"env", "-u", spec.EnvClaudeOAuthToken, "bash", "-c", continueScript, "claude", "--remote-control", "demo"},
			e.Args[len(e.Args)-9:])
	})

	for _, creds := range []*auth.Credentials{
		{AuthType: "api_key", Token: "k", FromEnv: true},
		{AuthType: "oauth", Token: "t", FromEnv: true},
	} {
		t.Run("needs a claude login with "+creds.AuthType, func(t *testing.T) {
			f := newManagerFixture(t)
			f.claude = creds
			f.runner.responses["sandbox list"] = sandboxListJSON()
			err := f.m.Start(context.Background(), StartOptions{Name: "demo", RemoteControl: true})
			require.ErrorIs(t, err, ErrRemoteControlNeedsLogin)
			assert.Empty(t, f.runner.find("sandbox create"))
			assert.Empty(t, f.runner.find("provider"))

			f.runner.responses["sandbox list"] = sandboxListJSON(sandboxJSON("demo", "Stopped", true))
			err = f.m.Connect(context.Background(), "demo", StartOptions{RemoteControl: true})
			require.ErrorIs(t, err, ErrRemoteControlNeedsLogin)
			assert.Empty(t, f.runner.find("sandbox start"))
		})
	}
}

func TestManagerAuthErrors(t *testing.T) {
	tests := []struct {
		name  string
		setup func(f *managerFixture)
	}{
		{name: "no Claude login", setup: func(f *managerFixture) { f.claude = nil }},
		{name: "Claude provider fails", setup: func(f *managerFixture) {
			f.runner.failures["provider update "+ClaudeProviderName] = errFake
		}},
		{name: "GitHub profile fails", setup: func(f *managerFixture) {
			f.runner.failures["profile update "+spec.GitHubProfileID] = errFake
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newManagerFixture(t)
			f.runner.responses["provider get"] = "  Credential keys: " + spec.EnvClaudeOAuthToken + "\n"
			tt.setup(f)
			assert.ErrorIs(t, f.m.Auth(context.Background()), errFake)
			assert.NotContains(t, f.out.String(), "Synced")
		})
	}
}

func TestStageHomeOAuthAccount(t *testing.T) {
	tests := []struct {
		name       string
		claudeJSON string // host ~/.claude.json; "" for none
		fullLogin  bool
		want       map[string]any // nil: nothing staged
	}{
		{
			name:       "keeps only account and organization IDs",
			claudeJSON: `{"oauthAccount":{"accountUuid":"a","organizationUuid":"o","organizationRole":"admin","organizationType":"claude_max","billingType":"stripe","workspaceRole":null,"organizationRateLimitTier":"t1","userRateLimitTier":"t2","seatTier":"s","emailAddress":"octo@example.com","displayName":"Octo","fullName":"Octo Cat","organizationName":"Octo's Org"}}`,
			fullLogin:  true,
			want: map[string]any{
				"accountUuid": "a", "organizationUuid": "o", "organizationRole": "admin",
				"organizationType": "claude_max", "billingType": "stripe", "workspaceRole": nil,
				"organizationRateLimitTier": "t1", "userRateLimitTier": "t2", "seatTier": "s",
			},
		},
		{name: "not a full login", claudeJSON: `{"oauthAccount":{"accountUuid":"a"}}`},
		{name: "no host config", fullLogin: true},
		{name: "invalid host config", claudeJSON: `{`, fullLogin: true},
		{name: "no account fields", claudeJSON: `{"oauthAccount":{"emailAddress":"octo@example.com"}}`, fullLogin: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			host := t.TempDir()
			if tt.claudeJSON != "" {
				writeFile(t, filepath.Join(host, ".claude.json"), tt.claudeJSON)
			}
			stage := t.TempDir()
			require.NoError(t, StageHome(stage, HomeOptions{ClaudeDir: host, ConfigDir: host, HomeDir: host, FullLogin: tt.fullLogin}))
			staged := filepath.Join(stage, ".claude", stagedOAuthAccountFile)
			if tt.want == nil {
				assert.NoFileExists(t, staged)
				return
			}
			data, err := os.ReadFile(staged)
			require.NoError(t, err)
			var got map[string]any
			require.NoError(t, json.Unmarshal(data, &got))
			assert.Equal(t, tt.want, got)
			assert.NotContains(t, string(data), "Octo")
		})
	}

	t.Run("unwritable", func(t *testing.T) {
		host := t.TempDir()
		writeFile(t, filepath.Join(host, ".claude.json"), `{"oauthAccount":{"accountUuid":"a"}}`)
		claudeDir := filepath.Join(t.TempDir(), "missing")
		assert.ErrorContains(t, stageOAuthAccount(claudeDir, host), "failed to write "+stagedOAuthAccountFile)
	})
}

// TestLoginScript runs the login script against real files.
func TestLoginScript(t *testing.T) {
	if _, err := exec.LookPath("jq"); err != nil {
		t.Skip("jq is not installed")
	}
	const placeholder = "openshell:resolve:env:abc123"
	run := func(t *testing.T, dir, token, scopes string) ([]byte, error) {
		t.Helper()
		cmd := exec.Command("bash", "-c", loginScript, "bash",
			filepath.Join(dir, ".claude", stagedOAuthAccountFile),
			filepath.Join(dir, ".claude.json"),
			filepath.Join(dir, ".claude", ".credentials.json"),
			scopes, "max")
		cmd.Env = append(os.Environ(), spec.EnvClaudeOAuthToken+"="+token)
		return cmd.CombinedOutput()
	}
	readJSONFile := func(t *testing.T, file string) map[string]any {
		t.Helper()
		var got map[string]any
		require.NoError(t, readJSON(file, &got))
		return got
	}

	tests := []struct {
		name       string
		claudeJSON string // sandbox ~/.claude.json; "" for none
		staged     bool
		wantConfig map[string]any // nil: no ~/.claude.json
	}{
		{
			name:       "replaces the account in the sandbox config",
			claudeJSON: `{"theme":"dark","oauthAccount":{"accountUuid":"old","emailAddress":"x"}}`,
			staged:     true,
			wantConfig: map[string]any{"theme": "dark", "oauthAccount": map[string]any{"accountUuid": "acc-1"}},
		},
		{
			name:       "creates the sandbox config",
			staged:     true,
			wantConfig: map[string]any{"oauthAccount": map[string]any{"accountUuid": "acc-1"}},
		},
		{
			name:       "no staged account leaves the config alone",
			claudeJSON: `{"theme":"dark"}`,
			wantConfig: map[string]any{"theme": "dark"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			staged := filepath.Join(dir, ".claude", stagedOAuthAccountFile)
			if tt.staged {
				writeFile(t, staged, `{"accountUuid":"acc-1"}`)
			}
			if tt.claudeJSON != "" {
				writeFile(t, filepath.Join(dir, ".claude.json"), tt.claudeJSON)
			}

			out, err := run(t, dir, placeholder, `["user:inference"]`)
			require.NoError(t, err, string(out))

			assert.NoFileExists(t, staged)
			assert.Equal(t, tt.wantConfig, readJSONFile(t, filepath.Join(dir, ".claude.json")))
			credsFile := filepath.Join(dir, ".claude", ".credentials.json")
			info, err := os.Stat(credsFile)
			require.NoError(t, err)
			assert.Equal(t, os.FileMode(0o600), info.Mode().Perm())
			assert.Equal(t, map[string]any{"claudeAiOauth": map[string]any{
				"accessToken":      placeholder,
				"refreshToken":     nil,
				"expiresAt":        float64(4102444800000),
				"scopes":           []any{"user:inference"},
				"subscriptionType": "max",
			}}, readJSONFile(t, credsFile))
		})
	}

	t.Run("rewrites an existing login", func(t *testing.T) {
		dir := t.TempDir()
		writeFile(t, filepath.Join(dir, ".claude", ".credentials.json"), `{"claudeAiOauth":{"accessToken":"stale"}}`)
		out, err := run(t, dir, placeholder, `null`)
		require.NoError(t, err, string(out))
		oauth := readJSONFile(t, filepath.Join(dir, ".claude", ".credentials.json"))["claudeAiOauth"].(map[string]any)
		assert.Equal(t, placeholder, oauth["accessToken"])
		assert.Nil(t, oauth["scopes"])
	})

	t.Run("fails without the provider token", func(t *testing.T) {
		dir := t.TempDir()
		out, err := run(t, dir, "", `[]`)
		require.Error(t, err)
		assert.Contains(t, string(out), "CLAUDE_CODE_OAUTH_TOKEN is not set")
		assert.NoFileExists(t, filepath.Join(dir, ".claude", ".credentials.json"))
	})
}

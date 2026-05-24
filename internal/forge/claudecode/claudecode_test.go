package claudecode

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBuildContainerConfig_FullOptions(t *testing.T) {
	homeDir := t.TempDir()
	configDir := t.TempDir()
	projectDir := t.TempDir()
	sessionDir := t.TempDir()

	// Create all conditional paths
	require.NoError(t, os.WriteFile(filepath.Join(homeDir, "CLAUDE.md"), []byte("# Home"), 0o644))
	require.NoError(t, os.MkdirAll(filepath.Join(homeDir, ".claude"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(homeDir, ".claude", "CLAUDE.md"), []byte("# DotClaude"), 0o644))
	for _, subdir := range []string{"rules", "agents", "commands", "skills", "plugins"} {
		require.NoError(t, os.MkdirAll(filepath.Join(homeDir, ".claude", subdir), 0o755))
	}
	require.NoError(t, os.WriteFile(filepath.Join(homeDir, ".claude", ".credentials.json"), []byte(`{"claudeAiOauth":{"accessToken":"tk"}}`), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(configDir, "settings.json"), []byte(`{}`), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(configDir, "gitconfig"), []byte("[user]\n"), 0o644))

	opts := Options{
		SkipPermissions: true,
		Worktree:        true,
		Prompt:          "hello world",
		ProjectDir:      projectDir,
		HomeDir:         homeDir,
		ConfigDir:       configDir,
		SessionDir:      sessionDir,
		AuthToken:       "sk-ant-test",
		AuthType:        "api_key",
		ProjectID:       "my-project",
		Owner:           "owner",
		Repo:            "repo",
		GitUserName:     "Test User",
		GitUserEmail:    "test@example.com",
	}

	cfg, err := BuildContainerConfig(opts)

	require.NoError(t, err)

	// Env vars
	assert.Equal(t, "/home/user", cfg.Env["HOME"])
	assert.Equal(t, "0", cfg.Env["GIT_TERMINAL_PROMPT"])
	assert.Equal(t, "sk-ant-test", cfg.Env["ANTHROPIC_API_KEY"])
	assert.Empty(t, cfg.Env["CLAUDE_CODE_OAUTH_TOKEN"])

	// Cmd args
	assert.Contains(t, cfg.Cmd, "--dangerously-skip-permissions")
	assert.Contains(t, cfg.Cmd, "--worktree")
	assert.Contains(t, cfg.Cmd, "-p")
	assert.Contains(t, cfg.Cmd, "hello world")

	// Mounts: project dir + session dir + home CLAUDE.md + .claude/CLAUDE.md +
	// 5 subdirs + .credentials.json + settings.json + gitconfig = 12
	assert.Len(t, cfg.Mounts, 12)

	// Verify project dir mount
	assert.Equal(t, projectDir, cfg.Mounts[0].Source)
	assert.Equal(t, "/work", cfg.Mounts[0].Target)
	assert.False(t, cfg.Mounts[0].ReadOnly)

	// Verify session dir mount
	assert.Equal(t, sessionDir, cfg.Mounts[1].Source)
	assert.Equal(t, "/home/user/.claude/projects/my-project/", cfg.Mounts[1].Target)
	assert.False(t, cfg.Mounts[1].ReadOnly)

	// Verify home CLAUDE.md mount
	assert.Equal(t, filepath.Join(homeDir, "CLAUDE.md"), cfg.Mounts[2].Source)
	assert.Equal(t, "/home/user/CLAUDE.md", cfg.Mounts[2].Target)
	assert.True(t, cfg.Mounts[2].ReadOnly)

	// Verify .claude/CLAUDE.md mount
	assert.Equal(t, filepath.Join(homeDir, ".claude", "CLAUDE.md"), cfg.Mounts[3].Source)
	assert.Equal(t, "/home/user/.claude/CLAUDE.md", cfg.Mounts[3].Target)
	assert.True(t, cfg.Mounts[3].ReadOnly)

	// Verify subdirectory mounts (rules, agents, commands, skills, plugins)
	subdirs := []string{"rules", "agents", "commands", "skills", "plugins"}
	for i, subdir := range subdirs {
		idx := 4 + i
		assert.Equal(t, filepath.Join(homeDir, ".claude", subdir), cfg.Mounts[idx].Source)
		assert.Equal(t, "/home/user/.claude/"+subdir+"/", cfg.Mounts[idx].Target)
		assert.True(t, cfg.Mounts[idx].ReadOnly)
	}

	// Verify credentials mount (read-write)
	assert.Equal(t, filepath.Join(homeDir, ".claude", ".credentials.json"), cfg.Mounts[9].Source)
	assert.Equal(t, "/home/user/.claude/.credentials.json", cfg.Mounts[9].Target)
	assert.False(t, cfg.Mounts[9].ReadOnly)

	// Verify settings.json mount
	assert.Equal(t, filepath.Join(configDir, "settings.json"), cfg.Mounts[10].Source)
	assert.Equal(t, "/home/user/.claude/settings.json", cfg.Mounts[10].Target)
	assert.True(t, cfg.Mounts[10].ReadOnly)

	// Verify gitconfig mount
	assert.Equal(t, filepath.Join(configDir, "gitconfig"), cfg.Mounts[11].Source)
	assert.Equal(t, "/home/user/.gitconfig", cfg.Mounts[11].Target)
	assert.True(t, cfg.Mounts[11].ReadOnly)

	// Gitconfig content
	assert.Contains(t, cfg.Gitconfig, "Test User")
	assert.Contains(t, cfg.Gitconfig, "test@example.com")
	assert.Contains(t, cfg.Gitconfig, `insteadOf = https://github.com/`)
}

func TestBuildContainerConfig_MinimalOptions(t *testing.T) {
	projectDir := t.TempDir()

	opts := Options{
		ProjectDir: projectDir,
		AuthToken:  "sk-ant-test",
		AuthType:   "api_key",
	}

	cfg, err := BuildContainerConfig(opts)

	require.NoError(t, err)

	// Only the required project dir mount
	assert.Len(t, cfg.Mounts, 1)
	assert.Equal(t, projectDir, cfg.Mounts[0].Source)
	assert.Equal(t, "/work", cfg.Mounts[0].Target)

	// No command args (SkipPermissions is false by default)
	assert.Empty(t, cfg.Cmd)
}

func TestBuildContainerConfig_OAuthAuth(t *testing.T) {
	projectDir := t.TempDir()

	opts := Options{
		ProjectDir: projectDir,
		AuthToken:  "oauth-token-123",
		AuthType:   "oauth",
	}

	cfg, err := BuildContainerConfig(opts)

	require.NoError(t, err)
	// OAuth tokens are not passed as env vars; Claude Code reads the mounted credentials file
	assert.Empty(t, cfg.Env["CLAUDE_CODE_OAUTH_TOKEN"])
	assert.Empty(t, cfg.Env["ANTHROPIC_API_KEY"])
}

func TestBuildContainerConfig_ValidationErrors(t *testing.T) {
	tests := []struct {
		name        string
		opts        Options
		errContains string
	}{
		{
			name: "missing project dir",
			opts: Options{
				AuthToken: "sk-test",
				AuthType:  "api_key",
			},
			errContains: "project directory is required",
		},
		{
			name: "missing auth token",
			opts: Options{
				ProjectDir: "/some/dir",
				AuthType:   "api_key",
			},
			errContains: "auth token is required",
		},
		{
			name: "invalid auth type",
			opts: Options{
				ProjectDir: "/some/dir",
				AuthToken:  "sk-test",
				AuthType:   "bearer",
			},
			errContains: `auth type must be "api_key" or "oauth"`,
		},
		{
			name: "empty auth type",
			opts: Options{
				ProjectDir: "/some/dir",
				AuthToken:  "sk-test",
				AuthType:   "",
			},
			errContains: `auth type must be "api_key" or "oauth"`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := BuildContainerConfig(tt.opts)

			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.errContains)
		})
	}
}

func TestBuildContainerConfig_CommandCombinations(t *testing.T) {
	tests := []struct {
		name     string
		opts     Options
		wantCmd  []string
		notInCmd []string
	}{
		{
			name: "skip permissions only",
			opts: Options{
				SkipPermissions: true,
			},
			wantCmd: []string{"--dangerously-skip-permissions"},
		},
		{
			name: "worktree only",
			opts: Options{
				Worktree: true,
			},
			wantCmd: []string{"--worktree"},
		},
		{
			name: "resume session",
			opts: Options{
				Resume: "session-123",
			},
			wantCmd: []string{"--resume", "session-123"},
		},
		{
			name: "continue most recent",
			opts: Options{
				Continue: true,
			},
			wantCmd: []string{"--continue"},
		},
		{
			name: "resume takes precedence over continue",
			opts: Options{
				Resume:   "session-123",
				Continue: true,
			},
			wantCmd:  []string{"--resume", "session-123"},
			notInCmd: []string{"--continue"},
		},
		{
			name: "prompt only",
			opts: Options{
				Prompt: "do something",
			},
			wantCmd: []string{"-p", "do something"},
		},
		{
			name: "skip permissions false",
			opts: Options{
				SkipPermissions: false,
			},
			wantCmd:  nil,
			notInCmd: []string{"--dangerously-skip-permissions"},
		},
		{
			name: "all flags together",
			opts: Options{
				SkipPermissions: true,
				Worktree:        true,
				Resume:          "sess-1",
				Prompt:          "build it",
			},
			wantCmd: []string{"--dangerously-skip-permissions", "--worktree", "--resume", "sess-1", "-p", "build it"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Set required fields
			tt.opts.ProjectDir = t.TempDir()
			tt.opts.AuthToken = "sk-test"
			tt.opts.AuthType = "api_key"

			cfg, err := BuildContainerConfig(tt.opts)

			require.NoError(t, err)

			if tt.wantCmd == nil {
				assert.Empty(t, cfg.Cmd)
			} else {
				assert.Equal(t, tt.wantCmd, cfg.Cmd)
			}

			for _, arg := range tt.notInCmd {
				assert.NotContains(t, cfg.Cmd, arg)
			}
		})
	}
}

func TestBuildContainerConfig_ConditionalMounts(t *testing.T) {
	projectDir := t.TempDir()
	homeDir := t.TempDir()

	// Only create some paths
	require.NoError(t, os.WriteFile(filepath.Join(homeDir, "CLAUDE.md"), []byte("# Home"), 0o644))
	require.NoError(t, os.MkdirAll(filepath.Join(homeDir, ".claude", "rules"), 0o755))
	// Don't create agents, commands, skills, .claude/CLAUDE.md

	opts := Options{
		ProjectDir: projectDir,
		HomeDir:    homeDir,
		AuthToken:  "sk-test",
		AuthType:   "api_key",
	}

	cfg, err := BuildContainerConfig(opts)

	require.NoError(t, err)

	// project dir + home CLAUDE.md + rules = 3
	assert.Len(t, cfg.Mounts, 3)
	assert.Equal(t, "/work", cfg.Mounts[0].Target)
	assert.Equal(t, "/home/user/CLAUDE.md", cfg.Mounts[1].Target)
	assert.Equal(t, "/home/user/.claude/rules/", cfg.Mounts[2].Target)
}

func TestGenerateGitconfig(t *testing.T) {
	opts := Options{
		GitUserName:  "Jane Doe",
		GitUserEmail: "jane@example.com",
	}

	result := generateGitconfig(opts)

	assert.Contains(t, result, `[url "http://gateway:8080/github.com/"]`)
	assert.Contains(t, result, `insteadOf = https://github.com/`)
	assert.Contains(t, result, `[user]`)
	assert.Contains(t, result, `name = Jane Doe`)
	assert.Contains(t, result, `email = jane@example.com`)
	assert.Contains(t, result, `[worktree]`)
	assert.Contains(t, result, `useRelativePaths = true`)
}

func TestGenerateGitconfig_EmptyUserInfo(t *testing.T) {
	opts := Options{}

	result := generateGitconfig(opts)

	assert.Contains(t, result, "name = \n")
	assert.Contains(t, result, "email = \n")
	// Proxy settings should still be present
	assert.Contains(t, result, `insteadOf = https://github.com/`)
}

func TestWriteGitconfig(t *testing.T) {
	configDir := filepath.Join(t.TempDir(), "nested", "config")

	opts := Options{
		GitUserName:  "John Doe",
		GitUserEmail: "john@example.com",
	}

	err := WriteGitconfig(configDir, opts)

	require.NoError(t, err)

	// Verify file was created
	data, err := os.ReadFile(filepath.Join(configDir, "gitconfig"))
	require.NoError(t, err)

	content := string(data)
	assert.Contains(t, content, "name = John Doe")
	assert.Contains(t, content, "email = john@example.com")
	assert.Contains(t, content, `insteadOf = https://github.com/`)
}

func TestWriteGitconfig_CreatesDirectory(t *testing.T) {
	baseDir := t.TempDir()
	configDir := filepath.Join(baseDir, "does", "not", "exist")

	opts := Options{
		GitUserName:  "User",
		GitUserEmail: "user@test.com",
	}

	err := WriteGitconfig(configDir, opts)

	require.NoError(t, err)

	// Verify directory was created
	info, err := os.Stat(configDir)
	require.NoError(t, err)
	assert.True(t, info.IsDir())

	// Verify file exists
	_, err = os.Stat(filepath.Join(configDir, "gitconfig"))
	require.NoError(t, err)
}

func TestWriteGitconfig_DirectoryCreationError(t *testing.T) {
	// Create a file where a directory is expected, so MkdirAll fails
	baseDir := t.TempDir()
	blockingFile := filepath.Join(baseDir, "blocked")
	require.NoError(t, os.WriteFile(blockingFile, []byte("file"), 0o644))

	// Try to create a config dir under the file -- will fail
	configDir := filepath.Join(blockingFile, "config")

	opts := Options{
		GitUserName:  "User",
		GitUserEmail: "user@test.com",
	}

	err := WriteGitconfig(configDir, opts)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to create config directory")
}

func TestEnsureSettings_DirectoryCreationError(t *testing.T) {
	baseDir := t.TempDir()
	blockingFile := filepath.Join(baseDir, "blocked")
	require.NoError(t, os.WriteFile(blockingFile, []byte("file"), 0o644))

	configDir := filepath.Join(blockingFile, "config")

	err := EnsureSettings(configDir)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to create config directory")
}

func TestEnsureSettings_CreatesIfMissing(t *testing.T) {
	configDir := t.TempDir()

	err := EnsureSettings(configDir)

	require.NoError(t, err)

	data, err := os.ReadFile(filepath.Join(configDir, "settings.json"))
	require.NoError(t, err)
	assert.Equal(t, DefaultSettings(), string(data))
}

func TestEnsureSettings_DoesNotOverwrite(t *testing.T) {
	configDir := t.TempDir()

	existingContent := `{"custom": "settings"}`
	require.NoError(t, os.WriteFile(filepath.Join(configDir, "settings.json"), []byte(existingContent), 0o644))

	err := EnsureSettings(configDir)

	require.NoError(t, err)

	// Verify content was NOT overwritten
	data, err := os.ReadFile(filepath.Join(configDir, "settings.json"))
	require.NoError(t, err)
	assert.Equal(t, existingContent, string(data))
}

func TestEnsureSettings_CreatesDirectory(t *testing.T) {
	baseDir := t.TempDir()
	configDir := filepath.Join(baseDir, "new", "dir")

	err := EnsureSettings(configDir)

	require.NoError(t, err)

	info, err := os.Stat(configDir)
	require.NoError(t, err)
	assert.True(t, info.IsDir())

	_, err = os.Stat(filepath.Join(configDir, "settings.json"))
	require.NoError(t, err)
}

func TestDefaultSettings(t *testing.T) {
	settings := DefaultSettings()

	assert.Contains(t, settings, `"autoUpdaterStatus": "disabled"`)
}

func TestEnsureUserConfig(t *testing.T) {
	t.Run("reads theme from host .claude.json", func(t *testing.T) {
		homeDir := t.TempDir()
		configDir := t.TempDir()

		hostConfig := `{"theme": "light-daltonized", "numStartups": 5}`
		require.NoError(t, os.WriteFile(filepath.Join(homeDir, ".claude.json"), []byte(hostConfig), 0o644))

		err := EnsureUserConfig(configDir, homeDir)
		require.NoError(t, err)

		data, err := os.ReadFile(filepath.Join(configDir, ".claude.json"))
		require.NoError(t, err)
		assert.Contains(t, string(data), `"hasCompletedOnboarding": true`)
		assert.Contains(t, string(data), `"light-daltonized"`)
	})

	t.Run("defaults to dark when host file missing", func(t *testing.T) {
		homeDir := t.TempDir()
		configDir := t.TempDir()

		err := EnsureUserConfig(configDir, homeDir)
		require.NoError(t, err)

		data, err := os.ReadFile(filepath.Join(configDir, ".claude.json"))
		require.NoError(t, err)
		assert.Contains(t, string(data), `"theme": "dark"`)
	})

	t.Run("defaults to dark when host file has no theme key", func(t *testing.T) {
		homeDir := t.TempDir()
		configDir := t.TempDir()

		hostConfig := `{"numStartups": 5}`
		require.NoError(t, os.WriteFile(filepath.Join(homeDir, ".claude.json"), []byte(hostConfig), 0o644))

		err := EnsureUserConfig(configDir, homeDir)
		require.NoError(t, err)

		data, err := os.ReadFile(filepath.Join(configDir, ".claude.json"))
		require.NoError(t, err)
		assert.Contains(t, string(data), `"theme": "dark"`)
	})

	t.Run("defaults to dark when host file has invalid JSON", func(t *testing.T) {
		homeDir := t.TempDir()
		configDir := t.TempDir()

		require.NoError(t, os.WriteFile(filepath.Join(homeDir, ".claude.json"), []byte(`{invalid`), 0o644))

		err := EnsureUserConfig(configDir, homeDir)
		require.NoError(t, err)

		data, err := os.ReadFile(filepath.Join(configDir, ".claude.json"))
		require.NoError(t, err)
		assert.Contains(t, string(data), `"theme": "dark"`)
	})

	t.Run("directory creation error", func(t *testing.T) {
		baseDir := t.TempDir()
		blockingFile := filepath.Join(baseDir, "blocked")
		require.NoError(t, os.WriteFile(blockingFile, []byte("file"), 0o644))

		configDir := filepath.Join(blockingFile, "config")

		err := EnsureUserConfig(configDir, baseDir)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "failed to create config directory")
	})

	t.Run("skips if file already exists", func(t *testing.T) {
		homeDir := t.TempDir()
		configDir := t.TempDir()

		existing := `{"theme": "custom"}`
		require.NoError(t, os.WriteFile(filepath.Join(configDir, ".claude.json"), []byte(existing), 0o644))

		err := EnsureUserConfig(configDir, homeDir)
		require.NoError(t, err)

		data, err := os.ReadFile(filepath.Join(configDir, ".claude.json"))
		require.NoError(t, err)
		assert.Equal(t, existing, string(data))
	})
}

func TestBuildEnv_UIDGIDEnvVars(t *testing.T) {
	tests := []struct {
		name       string
		uid        int
		gid        int
		wantUID    bool
		wantGID    bool
		wantUIDVal string
		wantGIDVal string
	}{
		{
			name:       "sets FORGE_UID and FORGE_GID when both positive",
			uid:        1000,
			gid:        1000,
			wantUID:    true,
			wantGID:    true,
			wantUIDVal: "1000",
			wantGIDVal: "1000",
		},
		{
			name:    "does not set when both zero",
			uid:     0,
			gid:     0,
			wantUID: false,
			wantGID: false,
		},
		{
			name:       "sets only UID when GID is zero",
			uid:        501,
			gid:        0,
			wantUID:    true,
			wantGID:    false,
			wantUIDVal: "501",
		},
		{
			name:       "sets only GID when UID is zero",
			uid:        0,
			gid:        20,
			wantUID:    false,
			wantGID:    true,
			wantGIDVal: "20",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			opts := Options{
				AuthToken: "sk-test",
				AuthType:  "api_key",
				UID:       tt.uid,
				GID:       tt.gid,
			}

			env := buildEnv(opts)

			if tt.wantUID {
				assert.Equal(t, tt.wantUIDVal, env["FORGE_UID"])
			} else {
				_, exists := env["FORGE_UID"]
				assert.False(t, exists, "FORGE_UID should not be set")
			}
			if tt.wantGID {
				assert.Equal(t, tt.wantGIDVal, env["FORGE_GID"])
			} else {
				_, exists := env["FORGE_GID"]
				assert.False(t, exists, "FORGE_GID should not be set")
			}
		})
	}
}

func TestWriteGitconfig_WriteFileError(t *testing.T) {
	configDir := t.TempDir()
	require.NoError(t, os.Chmod(configDir, 0o555))
	t.Cleanup(func() { os.Chmod(configDir, 0o755) })

	err := WriteGitconfig(configDir, Options{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to write gitconfig")
}

func TestEnsureSettings_WriteFileError(t *testing.T) {
	configDir := t.TempDir()
	require.NoError(t, os.Chmod(configDir, 0o555))
	t.Cleanup(func() { os.Chmod(configDir, 0o755) })

	err := EnsureSettings(configDir)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to write settings.json")
}

func TestEnsureUserConfig_WriteFileError(t *testing.T) {
	homeDir := t.TempDir()
	configDir := t.TempDir()
	require.NoError(t, os.Chmod(configDir, 0o555))
	t.Cleanup(func() { os.Chmod(configDir, 0o755) })

	err := EnsureUserConfig(configDir, homeDir)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to write .claude.json")
}

func TestReadHostTheme_EmptyThemeString(t *testing.T) {
	homeDir := t.TempDir()
	hostConfig := `{"theme": ""}`
	require.NoError(t, os.WriteFile(filepath.Join(homeDir, ".claude.json"), []byte(hostConfig), 0o644))

	theme := readHostTheme(homeDir)
	assert.Equal(t, "dark", theme)
}

func TestReadHostModel(t *testing.T) {
	t.Run("returns model from valid settings.json", func(t *testing.T) {
		claudeDir := t.TempDir()
		settings := `{"model": "claude-opus-4-6", "autoUpdaterStatus": "disabled"}`
		require.NoError(t, os.WriteFile(filepath.Join(claudeDir, "settings.json"), []byte(settings), 0o644))

		model := ReadHostModel(claudeDir)
		assert.Equal(t, "claude-opus-4-6", model)
	})

	t.Run("returns empty string when file missing", func(t *testing.T) {
		claudeDir := t.TempDir()

		model := ReadHostModel(claudeDir)
		assert.Equal(t, "", model)
	})

	t.Run("returns empty string for invalid JSON", func(t *testing.T) {
		claudeDir := t.TempDir()
		require.NoError(t, os.WriteFile(filepath.Join(claudeDir, "settings.json"), []byte(`{invalid`), 0o644))

		model := ReadHostModel(claudeDir)
		assert.Equal(t, "", model)
	})

	t.Run("returns empty string when model key missing", func(t *testing.T) {
		claudeDir := t.TempDir()
		settings := `{"autoUpdaterStatus": "disabled"}`
		require.NoError(t, os.WriteFile(filepath.Join(claudeDir, "settings.json"), []byte(settings), 0o644))

		model := ReadHostModel(claudeDir)
		assert.Equal(t, "", model)
	})

	t.Run("returns empty string when model is empty string", func(t *testing.T) {
		claudeDir := t.TempDir()
		settings := `{"model": ""}`
		require.NoError(t, os.WriteFile(filepath.Join(claudeDir, "settings.json"), []byte(settings), 0o644))

		model := ReadHostModel(claudeDir)
		assert.Equal(t, "", model)
	})
}

func TestBuildEnv_ModelEnvVar(t *testing.T) {
	t.Run("sets ANTHROPIC_MODEL when model is set", func(t *testing.T) {
		opts := Options{
			AuthToken: "sk-test",
			AuthType:  "api_key",
			Model:     "claude-opus-4-6",
		}

		env := buildEnv(opts)
		assert.Equal(t, "claude-opus-4-6", env["ANTHROPIC_MODEL"])
	})

	t.Run("does not set ANTHROPIC_MODEL when model is empty", func(t *testing.T) {
		opts := Options{
			AuthToken: "sk-test",
			AuthType:  "api_key",
			Model:     "",
		}

		env := buildEnv(opts)
		_, exists := env["ANTHROPIC_MODEL"]
		assert.False(t, exists, "ANTHROPIC_MODEL should not be set")
	})
}

func TestDetectCacheDirs(t *testing.T) {
	t.Run("go caches from host plus forge caches always present", func(t *testing.T) {
		homeDir := t.TempDir()

		// Create Go cache directories on host
		require.NoError(t, os.MkdirAll(filepath.Join(homeDir, "go", "pkg", "mod"), 0o755))
		// Don't create .cache/go-build

		result := DetectCacheDirs(homeDir)

		// 1 Go host cache + 3 forge caches (npm, pnpm, pip) = 4
		assert.Len(t, result, 4)

		// Verify correct source/target pairs
		sources := make(map[string]string)
		for _, cd := range result {
			sources[cd.Source] = cd.Target
		}

		// Go host cache
		assert.Equal(t, "/home/user/go/pkg/mod", sources[filepath.Join(homeDir, "go", "pkg", "mod")])

		// Forge caches (always created)
		forgeCacheBase := filepath.Join(homeDir, ".claude-forge", "caches")
		assert.Equal(t, "/home/user/.npm", sources[filepath.Join(forgeCacheBase, "npm")])
		assert.Equal(t, "/home/user/.local/share/pnpm/store", sources[filepath.Join(forgeCacheBase, "pnpm")])
		assert.Equal(t, "/home/user/.cache/pip", sources[filepath.Join(forgeCacheBase, "pip")])
	})

	t.Run("forge caches always present even with no host caches", func(t *testing.T) {
		homeDir := t.TempDir()

		result := DetectCacheDirs(homeDir)

		// 0 Go host caches + 3 forge caches = 3
		assert.Len(t, result, 3)

		// Verify forge cache directories were created
		forgeCacheBase := filepath.Join(homeDir, ".claude-forge", "caches")
		for _, name := range []string{"npm", "pnpm", "pip"} {
			info, err := os.Stat(filepath.Join(forgeCacheBase, name))
			require.NoError(t, err, "forge cache dir %s should exist", name)
			assert.True(t, info.IsDir())
		}
	})

	t.Run("all go host caches plus forge caches", func(t *testing.T) {
		homeDir := t.TempDir()

		// Create all Go cache directories
		require.NoError(t, os.MkdirAll(filepath.Join(homeDir, "go", "pkg", "mod"), 0o755))
		require.NoError(t, os.MkdirAll(filepath.Join(homeDir, ".cache", "go-build"), 0o755))

		result := DetectCacheDirs(homeDir)

		// 2 Go host caches + 3 forge caches = 5
		assert.Len(t, result, 5)
	})

	t.Run("forge caches use per-forge paths not host paths", func(t *testing.T) {
		homeDir := t.TempDir()

		// Create host npm/pip dirs that should NOT be used
		require.NoError(t, os.MkdirAll(filepath.Join(homeDir, ".npm"), 0o755))
		require.NoError(t, os.MkdirAll(filepath.Join(homeDir, ".cache", "pip"), 0o755))

		result := DetectCacheDirs(homeDir)

		forgeCacheBase := filepath.Join(homeDir, ".claude-forge", "caches")
		for _, cd := range result {
			if cd.Target == "/home/user/.npm" {
				// Must come from forge cache, not host
				assert.Equal(t, filepath.Join(forgeCacheBase, "npm"), cd.Source)
			}
			if cd.Target == "/home/user/.cache/pip" {
				assert.Equal(t, filepath.Join(forgeCacheBase, "pip"), cd.Source)
			}
		}
	})
}

func TestUpdateMCPServers(t *testing.T) {
	t.Run("creates mcpServers in new settings.json", func(t *testing.T) {
		configDir := filepath.Join(t.TempDir(), "config")

		servers := map[string]MCPServerConfig{
			"my-server": {Type: "sse", URL: "http://localhost:3000/sse"},
		}

		err := UpdateMCPServers(configDir, servers)
		require.NoError(t, err)

		data, err := os.ReadFile(filepath.Join(configDir, "settings.json"))
		require.NoError(t, err)

		var settings map[string]any
		require.NoError(t, json.Unmarshal(data, &settings))

		// Default keys from DefaultSettings should be present
		assert.Equal(t, "disabled", settings["autoUpdaterStatus"])
		assert.Equal(t, true, settings["skipDangerousModePermissionPrompt"])

		// mcpServers should contain our server
		mcpServers, ok := settings["mcpServers"].(map[string]any)
		require.True(t, ok, "mcpServers should be a map")

		server, ok := mcpServers["my-server"].(map[string]any)
		require.True(t, ok, "my-server should be a map")
		assert.Equal(t, "sse", server["type"])
		assert.Equal(t, "http://localhost:3000/sse", server["url"])
	})

	t.Run("merges mcpServers into existing settings with other keys", func(t *testing.T) {
		configDir := t.TempDir()

		existingSettings := `{
  "autoUpdaterStatus": "disabled",
  "model": "claude-opus-4-6",
  "customKey": "customValue"
}`
		require.NoError(t, os.WriteFile(filepath.Join(configDir, "settings.json"), []byte(existingSettings), 0o644))

		servers := map[string]MCPServerConfig{
			"gateway": {Type: "sse", URL: "http://gateway:8080/mcp"},
		}

		err := UpdateMCPServers(configDir, servers)
		require.NoError(t, err)

		data, err := os.ReadFile(filepath.Join(configDir, "settings.json"))
		require.NoError(t, err)

		var settings map[string]any
		require.NoError(t, json.Unmarshal(data, &settings))

		// Existing keys should be preserved
		assert.Equal(t, "disabled", settings["autoUpdaterStatus"])
		assert.Equal(t, "claude-opus-4-6", settings["model"])
		assert.Equal(t, "customValue", settings["customKey"])

		// mcpServers should be added
		mcpServers, ok := settings["mcpServers"].(map[string]any)
		require.True(t, ok, "mcpServers should be a map")

		server, ok := mcpServers["gateway"].(map[string]any)
		require.True(t, ok, "gateway should be a map")
		assert.Equal(t, "sse", server["type"])
		assert.Equal(t, "http://gateway:8080/mcp", server["url"])
	})

	t.Run("overwrites existing mcpServers entries", func(t *testing.T) {
		configDir := t.TempDir()

		existingSettings := `{
  "autoUpdaterStatus": "disabled",
  "mcpServers": {
    "old-server": {"type": "sse", "url": "http://old:1111/sse"},
    "shared-server": {"type": "sse", "url": "http://shared:2222/sse"}
  }
}`
		require.NoError(t, os.WriteFile(filepath.Join(configDir, "settings.json"), []byte(existingSettings), 0o644))

		servers := map[string]MCPServerConfig{
			"shared-server": {Type: "sse", URL: "http://shared:9999/new"},
			"new-server":    {Type: "sse", URL: "http://new:3333/sse"},
		}

		err := UpdateMCPServers(configDir, servers)
		require.NoError(t, err)

		data, err := os.ReadFile(filepath.Join(configDir, "settings.json"))
		require.NoError(t, err)

		var settings map[string]any
		require.NoError(t, json.Unmarshal(data, &settings))

		mcpServers, ok := settings["mcpServers"].(map[string]any)
		require.True(t, ok, "mcpServers should be a map")

		// Old server should be preserved
		oldServer, ok := mcpServers["old-server"].(map[string]any)
		require.True(t, ok, "old-server should still exist")
		assert.Equal(t, "http://old:1111/sse", oldServer["url"])

		// Shared server should be overwritten with new URL
		sharedServer, ok := mcpServers["shared-server"].(map[string]any)
		require.True(t, ok, "shared-server should exist")
		assert.Equal(t, "http://shared:9999/new", sharedServer["url"])

		// New server should be added
		newServer, ok := mcpServers["new-server"].(map[string]any)
		require.True(t, ok, "new-server should exist")
		assert.Equal(t, "http://new:3333/sse", newServer["url"])
	})

	t.Run("handles nonexistent configDir by creating it", func(t *testing.T) {
		baseDir := t.TempDir()
		configDir := filepath.Join(baseDir, "does", "not", "exist")

		servers := map[string]MCPServerConfig{
			"server": {Type: "sse", URL: "http://localhost:8080/sse"},
		}

		err := UpdateMCPServers(configDir, servers)
		require.NoError(t, err)

		// Directory should have been created
		info, err := os.Stat(configDir)
		require.NoError(t, err)
		assert.True(t, info.IsDir())

		// File should exist with correct content
		data, err := os.ReadFile(filepath.Join(configDir, "settings.json"))
		require.NoError(t, err)

		var settings map[string]any
		require.NoError(t, json.Unmarshal(data, &settings))

		mcpServers, ok := settings["mcpServers"].(map[string]any)
		require.True(t, ok)
		_, ok = mcpServers["server"]
		assert.True(t, ok)
	})

	t.Run("directory creation error", func(t *testing.T) {
		baseDir := t.TempDir()
		blockingFile := filepath.Join(baseDir, "blocked")
		require.NoError(t, os.WriteFile(blockingFile, []byte("file"), 0o644))

		configDir := filepath.Join(blockingFile, "config")

		servers := map[string]MCPServerConfig{
			"server": {Type: "sse", URL: "http://localhost:8080/sse"},
		}

		err := UpdateMCPServers(configDir, servers)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "failed to create config directory")
	})

	t.Run("invalid JSON in existing settings.json", func(t *testing.T) {
		configDir := t.TempDir()
		require.NoError(t, os.WriteFile(filepath.Join(configDir, "settings.json"), []byte(`{invalid`), 0o644))

		servers := map[string]MCPServerConfig{
			"server": {Type: "sse", URL: "http://localhost:8080/sse"},
		}

		err := UpdateMCPServers(configDir, servers)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "failed to parse settings.json")
	})
}

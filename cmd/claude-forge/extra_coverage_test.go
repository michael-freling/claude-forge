package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestExtractGitHubRepo(t *testing.T) {
	tests := []struct {
		name string
		path string
		want string
	}{
		{"no marker", "/home/user/src/example.com/owner/repo", ""},
		{"owner only", "github.com/owner", ""},
		{"empty owner", "github.com//repo", ""},
		{"empty repo", "github.com/owner/", ""},
		{"owner and repo", "github.com/owner/repo", "owner/repo"},
		{"trailing path", "/src/github.com/owner/repo/.claude/worktrees/main", "owner/repo"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, extractGitHubRepo(tt.path))
		})
	}
}

func TestEnablePluginsInSettings_InvalidJSON(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "settings.json"), []byte("not json"), 0o644))

	err := enablePluginsInSettings(dir, []string{"foo@bar"})
	assert.Error(t, err)
}

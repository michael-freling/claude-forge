package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/michael-freling/claude-forge/internal/forge/openshell/spec"
)

func writeConfig(t *testing.T, dir, yml string) {
	t.Helper()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "config.yaml"), []byte(yml), 0o644))
}

func TestLoad_OpenShellPolicy(t *testing.T) {
	t.Run("extensions", func(t *testing.T) {
		dir := t.TempDir()
		writeConfig(t, dir, "openshell:\n  policy:\n    allow: [api.example.com, \"**.example.org\", db.example.com:5432]\n"+
			"    read_only: [docs.example.com]\n    disable: [aws]\n")
		cfg, err := Load(dir)
		require.NoError(t, err)
		assert.Equal(t, spec.PolicyConfig{
			Allow:    []string{"api.example.com", "**.example.org", "db.example.com:5432"},
			ReadOnly: []string{"docs.example.com"},
			Disable:  []string{"aws"},
		}, cfg.OpenShell.Policy)
		assert.Empty(t, cfg.OpenShell.PolicyFile)
	})

	tests := []struct {
		name    string
		yaml    string
		wantErr []string
	}{
		{name: "invalid port", yaml: "openshell:\n  policy:\n    allow: [example.com:99999]\n", wantErr: []string{"openshell.policy.allow:", "port"}},
		{name: "invalid wildcard", yaml: "openshell:\n  policy:\n    read_only: [\"foo.*.com\"]\n", wantErr: []string{"openshell.policy.read_only:", "first label"}},
		{name: "short wildcard", yaml: "openshell:\n  policy:\n    allow: [\"**.org\"]\n", wantErr: []string{"three labels"}},
		{
			name:    "provider host",
			yaml:    "openshell:\n  policy:\n    allow: [\"*.anthropic.com\"]\n",
			wantErr: []string{"openshell.policy.allow:", "api.anthropic.com:443", "disagree on inspection"},
		},
		{name: "loopback", yaml: "openshell:\n  policy:\n    allow: [localhost:8080]\n", wantErr: []string{"always blocks"}},
		{name: "unknown group", yaml: "openshell:\n  policy:\n    disable: [azure]\n", wantErr: []string{"openshell.policy.disable:", "azure"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			writeConfig(t, dir, tt.yaml)
			_, err := Load(dir)
			require.Error(t, err)
			for _, want := range tt.wantErr {
				assert.Contains(t, err.Error(), want)
			}
		})
	}
}

func TestLoad_OpenShellPolicyFile(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	const policy = "version: 1\nnetwork_policies: {}\n"
	homePolicy := filepath.Join(home, "policies", "home.yaml")
	require.NoError(t, os.MkdirAll(filepath.Dir(homePolicy), 0o755))
	require.NoError(t, os.WriteFile(homePolicy, []byte(policy), 0o644))

	tests := []struct {
		name     string
		file     string
		contents string // written to file in the config dir when set
		extra    string // appended to the openshell section
		want     func(configDir string) string
		wantErr  []string
	}{
		{
			name: "relative to the config dir", file: "sandbox.yaml", contents: policy,
			want: func(configDir string) string { return filepath.Join(configDir, "sandbox.yaml") },
		},
		{name: "home directory", file: "~/policies/home.yaml", want: func(string) string { return homePolicy }},
		{name: "absolute", file: homePolicy, want: func(string) string { return homePolicy }},
		{name: "missing", file: "missing.yaml", wantErr: []string{"openshell.policy_file:", "failed to read policy file"}},
		{name: "invalid YAML", file: "bad.yaml", contents: "version: [1\n", wantErr: []string{"openshell.policy_file:", "failed to parse policy file"}},
		{name: "not a mapping", file: "list.yaml", contents: "- a\n", wantErr: []string{"failed to parse policy file"}},
		{name: "empty", file: "empty.yaml", contents: "\n", wantErr: []string{"is empty"}},
		{
			name: "combined with policy", file: "sandbox.yaml", contents: policy, extra: "  policy:\n    disable: [aws]\n",
			wantErr: []string{"cannot be combined with openshell.policy"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			if tt.contents != "" {
				require.NoError(t, os.WriteFile(filepath.Join(dir, tt.file), []byte(tt.contents), 0o644))
			}
			writeConfig(t, dir, "openshell:\n  policy_file: "+tt.file+"\n"+tt.extra)
			cfg, err := Load(dir)
			if tt.wantErr != nil {
				require.Error(t, err)
				for _, want := range tt.wantErr {
					assert.Contains(t, err.Error(), want)
				}
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want(dir), cfg.OpenShell.PolicyFile)
		})
	}
}

func TestResolvePathWithoutHome(t *testing.T) {
	t.Setenv("HOME", "")
	_, err := resolvePath("~/policy.yaml", "/config")
	assert.Error(t, err)
}

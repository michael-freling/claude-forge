package kube

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

// sampleKubeconfig returns a minimal kubeconfig YAML with two contexts.
func sampleKubeconfig() string {
	return `apiVersion: v1
kind: Config
current-context: ctx-a
clusters:
- name: cluster-a
  cluster:
    server: https://api.cluster-a.example.com
    certificate-authority-data: Y2EtZGF0YS1h
- name: cluster-b
  cluster:
    server: https://api.cluster-b.example.com
    certificate-authority-data: Y2EtZGF0YS1i
contexts:
- name: ctx-a
  context:
    cluster: cluster-a
    user: user-a
- name: ctx-b
  context:
    cluster: cluster-b
    user: user-b
users:
- name: user-a
  user:
    token: original-token-a
- name: user-b
  user:
    token: original-token-b
`
}

// stubResolveToken replaces resolveToken for the duration of the test.
func stubResolveToken(t *testing.T, fn func(ctx ContextConfig, kubeconfigPath string, duration time.Duration) (string, error)) {
	t.Helper()
	orig := resolveToken
	resolveToken = fn
	t.Cleanup(func() { resolveToken = orig })
}

func staticToken(ctx ContextConfig, kubeconfigPath string, duration time.Duration) (string, error) {
	return "sa-token-" + ctx.HostContext, nil
}

func TestGenerateKubeconfig_Success(t *testing.T) {
	stubResolveToken(t, staticToken)

	tmpDir := t.TempDir()
	srcPath := filepath.Join(tmpDir, "kubeconfig")
	outDir := filepath.Join(tmpDir, "out")
	require.NoError(t, os.WriteFile(srcPath, []byte(sampleKubeconfig()), 0o600))

	contexts := []ContextConfig{
		{HostContext: "ctx-a", ServiceAccountName: "sa-a", ServiceAccountNamespace: "ns-a"},
		{HostContext: "ctx-b", ServiceAccountName: "sa-b", ServiceAccountNamespace: "ns-b"},
	}

	err := GenerateKubeconfig(contexts, srcPath, "ctx-a", outDir, time.Hour)
	require.NoError(t, err)

	data, err := os.ReadFile(filepath.Join(outDir, KubeconfigFileName))
	require.NoError(t, err)

	var out kubeConfig
	require.NoError(t, yaml.Unmarshal(data, &out))

	assert.Equal(t, "v1", out.APIVersion)
	assert.Equal(t, "Config", out.Kind)
	assert.Equal(t, "ctx-a", out.CurrentContext)

	// Clusters
	require.Len(t, out.Clusters, 2)
	assert.Equal(t, "ctx-a", out.Clusters[0].Name)
	assert.Equal(t, "https://api.cluster-a.example.com", out.Clusters[0].Cluster.Server)
	assert.Equal(t, "Y2EtZGF0YS1h", out.Clusters[0].Cluster.CertificateAuthorityData)
	assert.Equal(t, "ctx-b", out.Clusters[1].Name)
	assert.Equal(t, "https://api.cluster-b.example.com", out.Clusters[1].Cluster.Server)

	// Contexts
	require.Len(t, out.Contexts, 2)
	assert.Equal(t, "ctx-a", out.Contexts[0].Name)
	assert.Equal(t, "ctx-a", out.Contexts[0].Context.Cluster)
	assert.Equal(t, "ctx-a-sa", out.Contexts[0].Context.User)
	assert.Equal(t, "ctx-b", out.Contexts[1].Name)
	assert.Equal(t, "ctx-b", out.Contexts[1].Context.Cluster)
	assert.Equal(t, "ctx-b-sa", out.Contexts[1].Context.User)

	// Users reference container-side token files instead of embedding tokens.
	require.Len(t, out.Users, 2)
	assert.Equal(t, "ctx-a-sa", out.Users[0].Name)
	assert.Equal(t, MCPServerKubeDir+"/"+TokensDirName+"/"+TokenFileName("ctx-a"), out.Users[0].User.TokenFile)
	assert.Equal(t, "ctx-b-sa", out.Users[1].Name)
	assert.Equal(t, MCPServerKubeDir+"/"+TokensDirName+"/"+TokenFileName("ctx-b"), out.Users[1].User.TokenFile)
	assert.NotContains(t, string(data), "sa-token-", "tokens must not be embedded in the kubeconfig")

	// Token files hold the minted tokens.
	for _, name := range []string{"ctx-a", "ctx-b"} {
		tokenPath := filepath.Join(outDir, TokensDirName, TokenFileName(name))
		token, err := os.ReadFile(tokenPath)
		require.NoError(t, err)
		assert.Equal(t, "sa-token-"+name, string(token))

		info, err := os.Stat(tokenPath)
		require.NoError(t, err)
		assert.Equal(t, os.FileMode(0o644), info.Mode().Perm())
	}

	// The container user is not necessarily the host user: the mounted
	// directory tree must be world-readable.
	for _, p := range []string{outDir, filepath.Join(outDir, TokensDirName)} {
		info, err := os.Stat(p)
		require.NoError(t, err)
		assert.Equal(t, os.FileMode(0o755), info.Mode().Perm(), p)
	}
	info, err := os.Stat(filepath.Join(outDir, KubeconfigFileName))
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o644), info.Mode().Perm())
}

func TestGenerateKubeconfig_OverwritesRestrictivePermissions(t *testing.T) {
	stubResolveToken(t, staticToken)

	tmpDir := t.TempDir()
	srcPath := filepath.Join(tmpDir, "kubeconfig")
	outDir := filepath.Join(tmpDir, "out")
	require.NoError(t, os.WriteFile(srcPath, []byte(sampleKubeconfig()), 0o600))

	// Pre-create the output dir with restrictive 0700 permissions, simulating
	// a directory left over from a prior version.
	require.NoError(t, os.MkdirAll(outDir, 0o700))

	contexts := []ContextConfig{
		{HostContext: "ctx-a", ServiceAccountName: "sa-a", ServiceAccountNamespace: "ns-a"},
	}

	err := GenerateKubeconfig(contexts, srcPath, "ctx-a", outDir, time.Hour)
	require.NoError(t, err)

	info, err := os.Stat(outDir)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o755), info.Mode().Perm(), "dir permissions should be widened to 0755")

	info, err = os.Stat(filepath.Join(outDir, KubeconfigFileName))
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o644), info.Mode().Perm())
}

func TestGenerateKubeconfig_KubeconfigNotFound(t *testing.T) {
	stubResolveToken(t, staticToken)

	tmpDir := t.TempDir()
	missingPath := filepath.Join(tmpDir, "does-not-exist")
	outDir := filepath.Join(tmpDir, "out")

	err := GenerateKubeconfig([]ContextConfig{{HostContext: "ctx-a"}}, missingPath, "ctx-a", outDir, time.Hour)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to read kubeconfig")
}

func TestGenerateKubeconfig_ContextNotFound(t *testing.T) {
	stubResolveToken(t, staticToken)

	tmpDir := t.TempDir()
	srcPath := filepath.Join(tmpDir, "kubeconfig")
	outDir := filepath.Join(tmpDir, "out")
	require.NoError(t, os.WriteFile(srcPath, []byte(sampleKubeconfig()), 0o600))

	contexts := []ContextConfig{
		{HostContext: "ctx-nonexistent", ServiceAccountName: "sa", ServiceAccountNamespace: "ns"},
	}

	err := GenerateKubeconfig(contexts, srcPath, "ctx-nonexistent", outDir, time.Hour)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "context \"ctx-nonexistent\" not found in kubeconfig")
}

func TestGenerateKubeconfig_ResolveTokenError(t *testing.T) {
	stubResolveToken(t, func(ctx ContextConfig, kubeconfigPath string, duration time.Duration) (string, error) {
		return "", fmt.Errorf("kubectl not available")
	})

	tmpDir := t.TempDir()
	srcPath := filepath.Join(tmpDir, "kubeconfig")
	outDir := filepath.Join(tmpDir, "out")
	require.NoError(t, os.WriteFile(srcPath, []byte(sampleKubeconfig()), 0o600))

	contexts := []ContextConfig{
		{HostContext: "ctx-a", ServiceAccountName: "sa-a", ServiceAccountNamespace: "ns-a"},
	}

	err := GenerateKubeconfig(contexts, srcPath, "ctx-a", outDir, time.Hour)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to resolve token for context \"ctx-a\"")
	assert.Contains(t, err.Error(), "kubectl not available")
}

func TestGenerateKubeconfig_CurrentContext(t *testing.T) {
	stubResolveToken(t, staticToken)

	tmpDir := t.TempDir()
	srcPath := filepath.Join(tmpDir, "kubeconfig")
	outDir := filepath.Join(tmpDir, "out")
	require.NoError(t, os.WriteFile(srcPath, []byte(sampleKubeconfig()), 0o600))

	contexts := []ContextConfig{
		{HostContext: "ctx-a", ServiceAccountName: "sa-a", ServiceAccountNamespace: "ns-a"},
		{HostContext: "ctx-b", ServiceAccountName: "sa-b", ServiceAccountNamespace: "ns-b"},
	}

	// Set defaultContext to ctx-b (not ctx-a which is current in source)
	err := GenerateKubeconfig(contexts, srcPath, "ctx-b", outDir, time.Hour)
	require.NoError(t, err)

	data, err := os.ReadFile(filepath.Join(outDir, KubeconfigFileName))
	require.NoError(t, err)

	var out kubeConfig
	require.NoError(t, yaml.Unmarshal(data, &out))

	assert.Equal(t, "ctx-b", out.CurrentContext)
}

func TestGenerateKubeconfig_PassesTokenDuration(t *testing.T) {
	var gotDuration time.Duration
	stubResolveToken(t, func(ctx ContextConfig, kubeconfigPath string, duration time.Duration) (string, error) {
		gotDuration = duration
		return "token", nil
	})

	tmpDir := t.TempDir()
	srcPath := filepath.Join(tmpDir, "kubeconfig")
	outDir := filepath.Join(tmpDir, "out")
	require.NoError(t, os.WriteFile(srcPath, []byte(sampleKubeconfig()), 0o600))

	contexts := []ContextConfig{
		{HostContext: "ctx-a", ServiceAccountName: "sa-a", ServiceAccountNamespace: "ns-a"},
	}

	require.NoError(t, GenerateKubeconfig(contexts, srcPath, "ctx-a", outDir, 24*time.Hour))
	assert.Equal(t, 24*time.Hour, gotDuration)
}

func TestRefreshTokens_RewritesTokenFiles(t *testing.T) {
	token := "first"
	stubResolveToken(t, func(ctx ContextConfig, kubeconfigPath string, duration time.Duration) (string, error) {
		return token, nil
	})

	tmpDir := t.TempDir()
	srcPath := filepath.Join(tmpDir, "kubeconfig")
	outDir := filepath.Join(tmpDir, "out")
	require.NoError(t, os.WriteFile(srcPath, []byte(sampleKubeconfig()), 0o600))

	contexts := []ContextConfig{
		{HostContext: "ctx-a", ServiceAccountName: "sa-a", ServiceAccountNamespace: "ns-a"},
	}
	require.NoError(t, GenerateKubeconfig(contexts, srcPath, "ctx-a", outDir, time.Hour))

	tokenPath := filepath.Join(outDir, TokensDirName, TokenFileName("ctx-a"))
	data, err := os.ReadFile(tokenPath)
	require.NoError(t, err)
	assert.Equal(t, "first", string(data))

	kubeconfigBefore, err := os.ReadFile(filepath.Join(outDir, KubeconfigFileName))
	require.NoError(t, err)

	token = "second"
	require.NoError(t, RefreshTokens(contexts, srcPath, outDir, time.Hour))

	data, err = os.ReadFile(tokenPath)
	require.NoError(t, err)
	assert.Equal(t, "second", string(data))

	// Refresh only rotates token files; the kubeconfig stays untouched.
	kubeconfigAfter, err := os.ReadFile(filepath.Join(outDir, KubeconfigFileName))
	require.NoError(t, err)
	assert.Equal(t, string(kubeconfigBefore), string(kubeconfigAfter))
}

func TestRefreshTokens_ResolveTokenError(t *testing.T) {
	stubResolveToken(t, func(ctx ContextConfig, kubeconfigPath string, duration time.Duration) (string, error) {
		return "", fmt.Errorf("boom")
	})

	outDir := filepath.Join(t.TempDir(), "out")
	contexts := []ContextConfig{
		{HostContext: "ctx-a", ServiceAccountName: "sa-a", ServiceAccountNamespace: "ns-a"},
	}

	err := RefreshTokens(contexts, "unused", outDir, time.Hour)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to resolve token for context \"ctx-a\"")
}

func TestTokenFileName(t *testing.T) {
	// Unsafe runes (EKS ARN-style contexts) are replaced.
	name := TokenFileName("arn:aws:eks:us-east-1:123:cluster/prod")
	assert.NotContains(t, name, "/")
	assert.NotContains(t, name, ":")

	// Distinct contexts that sanitize identically still get distinct names.
	assert.NotEqual(t, TokenFileName("ctx/a"), TokenFileName("ctx:a"))

	// Stable across calls.
	assert.Equal(t, TokenFileName("ctx-a"), TokenFileName("ctx-a"))

	// Very long context names are truncated but stay unique via the hash.
	long := ""
	for range 20 {
		long += "0123456789"
	}
	assert.LessOrEqual(t, len(TokenFileName(long)), 64+1+8)
	assert.NotEqual(t, TokenFileName(long), TokenFileName(long+"x"))
}

func TestListContexts(t *testing.T) {
	t.Run("returns context names", func(t *testing.T) {
		tmpDir := t.TempDir()
		kubeconfigPath := filepath.Join(tmpDir, "config")
		require.NoError(t, os.WriteFile(kubeconfigPath, []byte(sampleKubeconfig()), 0o600))

		contexts, err := ListContexts(kubeconfigPath)
		require.NoError(t, err)
		assert.Equal(t, []string{"ctx-a", "ctx-b"}, contexts)
	})

	t.Run("returns error for missing file", func(t *testing.T) {
		_, err := ListContexts("/nonexistent/kubeconfig")
		assert.Error(t, err)
	})

	t.Run("returns nil for empty contexts", func(t *testing.T) {
		tmpDir := t.TempDir()
		kubeconfigPath := filepath.Join(tmpDir, "config")
		require.NoError(t, os.WriteFile(kubeconfigPath, []byte("apiVersion: v1\nkind: Config\n"), 0o600))

		contexts, err := ListContexts(kubeconfigPath)
		require.NoError(t, err)
		assert.Nil(t, contexts)
	})
}

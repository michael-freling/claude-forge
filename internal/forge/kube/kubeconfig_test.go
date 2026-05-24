package kube

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

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

func TestGenerateKubeconfig_Success(t *testing.T) {
	origResolveToken := resolveToken
	resolveToken = func(ctx ContextConfig, kubeconfigPath string) (string, error) {
		return "sa-token-" + ctx.HostContext, nil
	}
	t.Cleanup(func() { resolveToken = origResolveToken })

	tmpDir := t.TempDir()
	srcPath := filepath.Join(tmpDir, "kubeconfig")
	outPath := filepath.Join(tmpDir, "kubeconfig-out")
	require.NoError(t, os.WriteFile(srcPath, []byte(sampleKubeconfig()), 0o600))

	contexts := []ContextConfig{
		{HostContext: "ctx-a", ServiceAccountName: "sa-a", ServiceAccountNamespace: "ns-a"},
		{HostContext: "ctx-b", ServiceAccountName: "sa-b", ServiceAccountNamespace: "ns-b"},
	}

	err := GenerateKubeconfig(contexts, srcPath, "ctx-a", outPath)
	require.NoError(t, err)

	data, err := os.ReadFile(outPath)
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

	// Users
	require.Len(t, out.Users, 2)
	assert.Equal(t, "ctx-a-sa", out.Users[0].Name)
	assert.Equal(t, "sa-token-ctx-a", out.Users[0].User.Token)
	assert.Equal(t, "ctx-b-sa", out.Users[1].Name)
	assert.Equal(t, "sa-token-ctx-b", out.Users[1].User.Token)

	// Verify file permissions
	info, err := os.Stat(outPath)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o600), info.Mode().Perm())
}

func TestGenerateKubeconfig_KubeconfigNotFound(t *testing.T) {
	origResolveToken := resolveToken
	resolveToken = func(ctx ContextConfig, kubeconfigPath string) (string, error) {
		return "unused", nil
	}
	t.Cleanup(func() { resolveToken = origResolveToken })

	tmpDir := t.TempDir()
	missingPath := filepath.Join(tmpDir, "does-not-exist")
	outPath := filepath.Join(tmpDir, "kubeconfig-out")

	err := GenerateKubeconfig([]ContextConfig{{HostContext: "ctx-a"}}, missingPath, "ctx-a", outPath)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to read kubeconfig")
}

func TestGenerateKubeconfig_ContextNotFound(t *testing.T) {
	origResolveToken := resolveToken
	resolveToken = func(ctx ContextConfig, kubeconfigPath string) (string, error) {
		return "token", nil
	}
	t.Cleanup(func() { resolveToken = origResolveToken })

	tmpDir := t.TempDir()
	srcPath := filepath.Join(tmpDir, "kubeconfig")
	outPath := filepath.Join(tmpDir, "kubeconfig-out")
	require.NoError(t, os.WriteFile(srcPath, []byte(sampleKubeconfig()), 0o600))

	contexts := []ContextConfig{
		{HostContext: "ctx-nonexistent", ServiceAccountName: "sa", ServiceAccountNamespace: "ns"},
	}

	err := GenerateKubeconfig(contexts, srcPath, "ctx-nonexistent", outPath)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "context \"ctx-nonexistent\" not found in kubeconfig")
}

func TestGenerateKubeconfig_ResolveTokenError(t *testing.T) {
	origResolveToken := resolveToken
	resolveToken = func(ctx ContextConfig, kubeconfigPath string) (string, error) {
		return "", fmt.Errorf("kubectl not available")
	}
	t.Cleanup(func() { resolveToken = origResolveToken })

	tmpDir := t.TempDir()
	srcPath := filepath.Join(tmpDir, "kubeconfig")
	outPath := filepath.Join(tmpDir, "kubeconfig-out")
	require.NoError(t, os.WriteFile(srcPath, []byte(sampleKubeconfig()), 0o600))

	contexts := []ContextConfig{
		{HostContext: "ctx-a", ServiceAccountName: "sa-a", ServiceAccountNamespace: "ns-a"},
	}

	err := GenerateKubeconfig(contexts, srcPath, "ctx-a", outPath)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to resolve token for context \"ctx-a\"")
	assert.Contains(t, err.Error(), "kubectl not available")
}

func TestGenerateKubeconfig_CurrentContext(t *testing.T) {
	origResolveToken := resolveToken
	resolveToken = func(ctx ContextConfig, kubeconfigPath string) (string, error) {
		return "token", nil
	}
	t.Cleanup(func() { resolveToken = origResolveToken })

	tmpDir := t.TempDir()
	srcPath := filepath.Join(tmpDir, "kubeconfig")
	outPath := filepath.Join(tmpDir, "kubeconfig-out")
	require.NoError(t, os.WriteFile(srcPath, []byte(sampleKubeconfig()), 0o600))

	contexts := []ContextConfig{
		{HostContext: "ctx-a", ServiceAccountName: "sa-a", ServiceAccountNamespace: "ns-a"},
		{HostContext: "ctx-b", ServiceAccountName: "sa-b", ServiceAccountNamespace: "ns-b"},
	}

	// Set defaultContext to ctx-b (not ctx-a which is current in source)
	err := GenerateKubeconfig(contexts, srcPath, "ctx-b", outPath)
	require.NoError(t, err)

	data, err := os.ReadFile(outPath)
	require.NoError(t, err)

	var out kubeConfig
	require.NoError(t, yaml.Unmarshal(data, &out))

	assert.Equal(t, "ctx-b", out.CurrentContext)
}

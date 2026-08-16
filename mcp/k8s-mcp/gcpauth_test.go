package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
	"k8s.io/client-go/rest"
	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"
)

// gkeKubeconfig is a kubeconfig whose current context authenticates via the
// gke-gcloud-auth-plugin exec plugin (as `gcloud container clusters
// get-credentials` writes it), plus a second token-based context so tests can
// exercise named-context selection.
const gkeKubeconfig = `apiVersion: v1
kind: Config
clusters:
- name: gke
  cluster:
    server: https://34.0.0.1
- name: plain
  cluster:
    server: https://127.0.0.1:6443
contexts:
- name: gke
  context:
    cluster: gke
    user: gke
- name: plain
  context:
    cluster: plain
    user: plain
current-context: gke
users:
- name: gke
  user:
    exec:
      apiVersion: client.authentication.k8s.io/v1beta1
      command: gke-gcloud-auth-plugin
      installHint: Install gke-gcloud-auth-plugin
      provideClusterInfo: true
- name: plain
  user:
    token: abc
`

// writeKubeconfig writes content to a temp file and returns its path.
func writeKubeconfig(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config")
	require.NoError(t, os.WriteFile(path, []byte(content), 0o600))
	return path
}

// withFakeCredentials swaps findDefaultCredentials for one returning a static
// token source, restoring the real lookup when the test ends.
func withFakeCredentials(t *testing.T) {
	t.Helper()
	orig := findDefaultCredentials
	findDefaultCredentials = func(_ context.Context, _ ...string) (*google.Credentials, error) {
		return &google.Credentials{
			TokenSource: oauth2.StaticTokenSource(&oauth2.Token{AccessToken: "fake-adc-token"}),
		}, nil
	}
	t.Cleanup(func() { findDefaultCredentials = orig })
}

// --- usesGKEAuthPlugin ---

// loadTestKubeconfig writes content and returns the parsed config.
func loadTestKubeconfig(t *testing.T, content string) *clientcmdapi.Config {
	t.Helper()
	cfg, err := loadKubeconfig(writeKubeconfig(t, content))
	require.NoError(t, err)
	return cfg
}

func TestUsesGKEAuthPlugin(t *testing.T) {
	gke := loadTestKubeconfig(t, gkeKubeconfig)
	plain := loadTestKubeconfig(t, testKubeconfig)

	// Current context uses the gke exec plugin.
	assert.True(t, usesGKEAuthPlugin(gke, ""))

	// Named-context selection within the same file.
	assert.True(t, usesGKEAuthPlugin(gke, "gke"))
	assert.False(t, usesGKEAuthPlugin(gke, "plain"))

	// Token-based user → false.
	assert.False(t, usesGKEAuthPlugin(plain, ""))

	// Unknown context name → false.
	assert.False(t, usesGKEAuthPlugin(gke, "no-such-context"))
}

func TestUsesGKEAuthPlugin_MissingAuthInfo(t *testing.T) {
	// A context that references a user with no entry in users → false.
	const cfg = `apiVersion: v1
kind: Config
clusters:
- name: c
  cluster:
    server: https://127.0.0.1:6443
contexts:
- name: c
  context:
    cluster: c
    user: ghost
current-context: c
`
	assert.False(t, usesGKEAuthPlugin(loadTestKubeconfig(t, cfg), ""))
}

// --- applyGCPAuth ---

func TestApplyGCPAuth_ReplacesKubeconfigAuth(t *testing.T) {
	withFakeCredentials(t)

	cfg := &rest.Config{
		Host:            "https://34.0.0.1",
		BearerToken:     "stale",
		BearerTokenFile: "/nonexistent/token",
		ExecProvider:    &clientcmdapi.ExecConfig{Command: "gke-gcloud-auth-plugin"},
		AuthProvider:    &clientcmdapi.AuthProviderConfig{Name: "gcp"},
	}
	require.NoError(t, applyGCPAuth(context.Background(), cfg))

	// Every kubeconfig-declared auth mechanism is stripped; the ADC token
	// source wraps the transport instead.
	assert.Nil(t, cfg.ExecProvider)
	assert.Nil(t, cfg.AuthProvider)
	assert.Empty(t, cfg.BearerToken)
	assert.Empty(t, cfg.BearerTokenFile)
	assert.NotNil(t, cfg.WrapTransport)
}

func TestApplyGCPAuth_ADCFailure(t *testing.T) {
	orig := findDefaultCredentials
	findDefaultCredentials = func(_ context.Context, _ ...string) (*google.Credentials, error) {
		return nil, errors.New("no ADC found")
	}
	t.Cleanup(func() { findDefaultCredentials = orig })

	err := applyGCPAuth(context.Background(), &rest.Config{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "gcloud auth application-default login")
}

// --- newClientForContext with GCP auth ---

func TestNewClientForContext_GCPAuthForced(t *testing.T) {
	withFakeCredentials(t)

	// A plain token kubeconfig with --gcp-auth forced still builds: ADC
	// replaces the token.
	c, err := newClientForContext(loadTestKubeconfig(t, testKubeconfig), "", true)
	require.NoError(t, err)
	require.NotNil(t, c)
	assert.NotNil(t, c.dyn)
	assert.NotNil(t, c.typed)
}

func TestNewClientForContext_GCPAuthAutoDetected(t *testing.T) {
	withFakeCredentials(t)

	// The kubeconfig's user declares the gke exec plugin; the client builds
	// without the plugin binary because ADC auth replaces it.
	c, err := newClientForContext(loadTestKubeconfig(t, gkeKubeconfig), "", false)
	require.NoError(t, err)
	require.NotNil(t, c)

	// Named context selection takes the same path.
	c2, err := newClientForContext(loadTestKubeconfig(t, gkeKubeconfig), "gke", false)
	require.NoError(t, err)
	require.NotNil(t, c2)
}

func TestNewClientForContext_GCPAuthADCFailure(t *testing.T) {
	orig := findDefaultCredentials
	findDefaultCredentials = func(_ context.Context, _ ...string) (*google.Credentials, error) {
		return nil, errors.New("no ADC found")
	}
	t.Cleanup(func() { findDefaultCredentials = orig })

	_, err := newClientForContext(loadTestKubeconfig(t, gkeKubeconfig), "", false)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "gcloud auth application-default login")
}

package main

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"
)

// multiContextKubeconfig has three contexts with current-context set to "prod",
// so tests can exercise both the default path and an allowlist that excludes the
// current context.
const multiContextKubeconfig = `apiVersion: v1
kind: Config
clusters:
- name: prod
  cluster:
    server: https://prod.example:6443
- name: local
  cluster:
    server: https://127.0.0.1:16443
- name: staging
  cluster:
    server: https://staging.example:6443
contexts:
- name: prod
  context:
    cluster: prod
    user: prod
- name: local
  context:
    cluster: local
    user: local
- name: staging
  context:
    cluster: staging
    user: prod
current-context: prod
users:
- name: prod
  user:
    token: abc
- name: local
  user:
    token: def
`

// countingFactory records how many times a client was built per context, and
// asserts the factory is handed the already-parsed config rather than re-reading
// the kubeconfig itself.
func countingFactory(t *testing.T, calls map[string]int, err error) clientFactory {
	t.Helper()
	return func(cfg *clientcmdapi.Config, kubeContext string, _ bool) (*Client, error) {
		assert.NotNil(t, cfg, "factory must receive the parsed kubeconfig")
		calls[kubeContext]++
		if err != nil {
			return nil, err
		}
		return newTestClient(), nil
	}
}

// --- NewClientSet ---

func TestNewClientSet_ServesEveryContextByDefault(t *testing.T) {
	cs, err := NewClientSet(writeKubeconfig(t, multiContextKubeconfig), nil, false)
	require.NoError(t, err)

	assert.Equal(t, []string{"local", "prod", "staging"}, cs.Names())
	assert.Equal(t, "prod", cs.DefaultContext())

	infos := cs.Contexts()
	require.Len(t, infos, 3)
	assert.Equal(t, ContextInfo{Name: "local", Cluster: "local", Server: "https://127.0.0.1:16443"}, infos[0])
	assert.Equal(t, ContextInfo{Name: "prod", Cluster: "prod", Server: "https://prod.example:6443", Default: true}, infos[1])
	assert.False(t, infos[2].Default)
}

func TestNewClientSet_AllowlistNarrows(t *testing.T) {
	cs, err := NewClientSet(writeKubeconfig(t, multiContextKubeconfig), []string{"local"}, false)
	require.NoError(t, err)

	assert.Equal(t, []string{"local"}, cs.Names())
	// current-context is excluded, but a lone served context is unambiguous.
	assert.Equal(t, "local", cs.DefaultContext())
	assert.True(t, cs.Contexts()[0].Default)
}

func TestNewClientSet_AllowlistWithoutCurrentContextHasNoDefault(t *testing.T) {
	cs, err := NewClientSet(writeKubeconfig(t, multiContextKubeconfig), []string{"staging", "local"}, false)
	require.NoError(t, err)

	assert.Equal(t, []string{"local", "staging"}, cs.Names(), "served set stays sorted")
	assert.Empty(t, cs.DefaultContext())
	for _, info := range cs.Contexts() {
		assert.False(t, info.Default)
	}

	// With no default, a call must name a context.
	_, err = cs.Get("")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no default context")
	assert.Contains(t, err.Error(), "local, staging")
}

func TestNewClientSet_UnknownContextInAllowlist(t *testing.T) {
	_, err := NewClientSet(writeKubeconfig(t, multiContextKubeconfig), []string{"local", "nope"}, false)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no context named nope")
	assert.Contains(t, err.Error(), "available: local, prod, staging")
}

func TestNewClientSet_NoContexts(t *testing.T) {
	_, err := NewClientSet(writeKubeconfig(t, "apiVersion: v1\nkind: Config\n"), nil, false)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "defines no contexts")
}

func TestNewClientSet_LoadError(t *testing.T) {
	_, err := NewClientSet(filepath.Join(t.TempDir(), "missing"), nil, false)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to load kubeconfig")
}

// A single-context kubeconfig keeps working exactly as before.
func TestNewClientSet_SingleContext(t *testing.T) {
	cs, err := NewClientSet(writeKubeconfig(t, testKubeconfig), nil, false)
	require.NoError(t, err)
	assert.Equal(t, []string{"test"}, cs.Names())
	assert.Equal(t, "test", cs.DefaultContext())

	// The real factory builds without touching the network.
	c, err := cs.Get("")
	require.NoError(t, err)
	assert.NotNil(t, c.dyn)
}

// --- Get ---

func TestClientSet_Get(t *testing.T) {
	cs, err := NewClientSet(writeKubeconfig(t, multiContextKubeconfig), nil, false)
	require.NoError(t, err)
	calls := map[string]int{}
	cs.newClient = countingFactory(t, calls, nil)

	// Empty name resolves to the default context.
	def, err := cs.Get("")
	require.NoError(t, err)
	require.NotNil(t, def)
	assert.Equal(t, map[string]int{"prod": 1}, calls)

	// A named context builds its own client.
	local, err := cs.Get("local")
	require.NoError(t, err)
	assert.NotSame(t, def, local)
	assert.Equal(t, 1, calls["local"])

	// Repeat calls are served from the cache.
	again, err := cs.Get("local")
	require.NoError(t, err)
	assert.Same(t, local, again)
	assert.Equal(t, 1, calls["local"], "cached client must not be rebuilt")
}

func TestClientSet_Get_UnknownContext(t *testing.T) {
	cs, err := NewClientSet(writeKubeconfig(t, multiContextKubeconfig), nil, false)
	require.NoError(t, err)

	_, err = cs.Get("nope")
	require.Error(t, err)
	assert.Contains(t, err.Error(), `unknown context "nope"`)
	assert.Contains(t, err.Error(), "available: local, prod, staging")
}

// A context that fails to build reports the failure against its own name, does
// not poison the cache, and leaves the other contexts usable.
func TestClientSet_Get_BuildErrorIsNotCached(t *testing.T) {
	cs, err := NewClientSet(writeKubeconfig(t, multiContextKubeconfig), nil, false)
	require.NoError(t, err)
	calls := map[string]int{}
	cs.newClient = countingFactory(t, calls, fmt.Errorf("cluster unreachable"))

	_, err = cs.Get("local")
	require.Error(t, err)
	assert.Contains(t, err.Error(), `context "local"`)
	assert.Contains(t, err.Error(), "cluster unreachable")

	// Retried, not remembered as broken.
	_, err = cs.Get("local")
	require.Error(t, err)
	assert.Equal(t, 2, calls["local"])

	// A healthy context still works after the failure.
	cs.newClient = countingFactory(t, calls, nil)
	c, err := cs.Get("prod")
	require.NoError(t, err)
	assert.NotNil(t, c)
}

// The kubeconfig is parsed once, at construction, and building a context's
// client must not touch the file again. Deleting the file after NewClientSet
// proves it with the real factory: when Get resolved a path instead of the
// parsed config, it re-read the file here (twice — once in the client config,
// once in the GKE auth-plugin probe) and this would fail.
func TestClientSet_Get_DoesNotRereadKubeconfig(t *testing.T) {
	path := writeKubeconfig(t, multiContextKubeconfig)
	cs, err := NewClientSet(path, nil, false)
	require.NoError(t, err)
	require.NoError(t, os.Remove(path))

	c, err := cs.Get("local")
	require.NoError(t, err)
	assert.NotNil(t, c.dyn)

	// The startup snapshot still describes the contexts, file or no file.
	assert.Equal(t, []string{"local", "prod", "staging"}, cs.Names())
}

// Get claims a concurrent duplicate build is harmless because one wins the map
// and both callers get that one. Pin it: goroutines racing on an uncached
// context must all succeed and observe the identical *Client, and a later call
// must be served from that same entry.
func TestClientSet_Get_Concurrent(t *testing.T) {
	cs, err := NewClientSet(writeKubeconfig(t, multiContextKubeconfig), nil, false)
	require.NoError(t, err)
	cs.newClient = func(*clientcmdapi.Config, string, bool) (*Client, error) { return newTestClient(), nil }

	const n = 16
	clients := make([]*Client, n)
	errs := make([]error, n)
	var wg sync.WaitGroup
	for i := range n {
		wg.Add(1)
		go func() {
			defer wg.Done()
			clients[i], errs[i] = cs.Get("local")
		}()
	}
	wg.Wait()

	for i := range n {
		require.NoErrorf(t, errs[i], "goroutine %d", i)
		assert.Samef(t, clients[0], clients[i], "goroutine %d got a different client", i)
	}

	cached, err := cs.Get("local")
	require.NoError(t, err)
	assert.Same(t, clients[0], cached)
}

func TestClientSet_ContextsIsACopy(t *testing.T) {
	cs, err := NewClientSet(writeKubeconfig(t, multiContextKubeconfig), nil, false)
	require.NoError(t, err)

	got := cs.Contexts()
	got[0].Name = "mutated"
	assert.Equal(t, "local", cs.Contexts()[0].Name)
}

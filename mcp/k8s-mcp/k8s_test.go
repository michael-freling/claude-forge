package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/discovery"
	dynamicfake "k8s.io/client-go/dynamic/fake"
	kfake "k8s.io/client-go/kubernetes/fake"
	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"
)

// --- shared test fakes (used by k8s_test.go, tools_test.go, server_test.go) ---

// testMapper builds a static REST mapper covering the kinds the tests exercise,
// so resolve works without talking to a live discovery endpoint.
func testMapper() meta.RESTMapper {
	m := meta.NewDefaultRESTMapper(nil)
	m.Add(schema.GroupVersionKind{Group: "", Version: "v1", Kind: "Pod"}, meta.RESTScopeNamespace)
	m.Add(schema.GroupVersionKind{Group: "", Version: "v1", Kind: "ConfigMap"}, meta.RESTScopeNamespace)
	m.Add(schema.GroupVersionKind{Group: "", Version: "v1", Kind: "Secret"}, meta.RESTScopeNamespace)
	m.Add(schema.GroupVersionKind{Group: "", Version: "v1", Kind: "ServiceAccount"}, meta.RESTScopeNamespace)
	m.Add(schema.GroupVersionKind{Group: "", Version: "v1", Kind: "Namespace"}, meta.RESTScopeRoot)
	m.Add(schema.GroupVersionKind{Group: "", Version: "v1", Kind: "Node"}, meta.RESTScopeRoot)
	m.Add(schema.GroupVersionKind{Group: "apps", Version: "v1", Kind: "Deployment"}, meta.RESTScopeNamespace)
	return m
}

// testListKinds maps each seeded resource to its List kind so the dynamic fake
// can satisfy List calls for unstructured objects.
func testListKinds() map[schema.GroupVersionResource]string {
	return map[schema.GroupVersionResource]string{
		{Version: "v1", Resource: "pods"}:                       "PodList",
		{Version: "v1", Resource: "configmaps"}:                 "ConfigMapList",
		{Version: "v1", Resource: "secrets"}:                    "SecretList",
		{Version: "v1", Resource: "serviceaccounts"}:            "ServiceAccountList",
		{Version: "v1", Resource: "namespaces"}:                 "NamespaceList",
		{Version: "v1", Resource: "nodes"}:                      "NodeList",
		{Group: "apps", Version: "v1", Resource: "deployments"}: "DeploymentList",
	}
}

// newTestClient builds a Client backed entirely by fakes, seeding the dynamic
// tracker with objs. Individual tests may override typed/disco as needed.
func newTestClient(objs ...runtime.Object) *Client {
	scheme := runtime.NewScheme()
	dyn := dynamicfake.NewSimpleDynamicClientWithCustomListKinds(scheme, testListKinds(), objs...)
	return &Client{
		dyn:    dyn,
		typed:  kfake.NewSimpleClientset(),
		disco:  &fakeDiscovery{},
		mapper: testMapper(),
	}
}

// staticClientSet wraps an already-built Client as the single served context
// "test", so tests that care about tool behaviour rather than context routing
// need no kubeconfig. The client is pre-cached, so newClient is never called.
func staticClientSet(c *Client) *ClientSet {
	return &ClientSet{
		defaultContext: "test",
		infos:          []ContextInfo{{Name: "test", Cluster: "test", Server: "https://127.0.0.1:6443", Default: true}},
		clients:        map[string]*Client{"test": c},
		newClient: func(*clientcmdapi.Config, string, bool) (*Client, error) {
			return nil, fmt.Errorf("unexpected client build in test")
		},
	}
}

// newTestClientSet is staticClientSet over a fresh fake Client.
func newTestClientSet(objs ...runtime.Object) *ClientSet {
	return staticClientSet(newTestClient(objs...))
}

// unstructuredObj builds a minimal unstructured resource for seeding the dynamic
// fake.
func unstructuredObj(apiVersion, kind, namespace, name string) *unstructured.Unstructured {
	md := map[string]any{"name": name}
	if namespace != "" {
		md["namespace"] = namespace
	}
	return &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": apiVersion,
		"kind":       kind,
		"metadata":   md,
	}}
}

// fakeDiscovery is a stub DiscoveryInterface whose ServerPreferredResources is
// controllable (the real fake always returns nil, nil).
type fakeDiscovery struct {
	discovery.DiscoveryInterface
	resources []*metav1.APIResourceList
	err       error
}

func (f *fakeDiscovery) ServerPreferredResources() ([]*metav1.APIResourceList, error) {
	return f.resources, f.err
}

// sampleAPIResourceLists is a canned discovery result for the api-resources
// tests.
func sampleAPIResourceLists() []*metav1.APIResourceList {
	return []*metav1.APIResourceList{
		{
			GroupVersion: "v1",
			APIResources: []metav1.APIResource{
				{Name: "pods", Kind: "Pod", Namespaced: true},
				{Name: "nodes", Kind: "Node", Namespaced: false},
			},
		},
	}
}

func podResolved() resolved {
	return resolved{gvr: schema.GroupVersionResource{Version: "v1", Resource: "pods"}, namespaced: true}
}

// --- NewClient ---

const testKubeconfig = `apiVersion: v1
kind: Config
clusters:
- name: test
  cluster:
    server: https://127.0.0.1:6443
contexts:
- name: test
  context:
    cluster: test
    user: test
current-context: test
users:
- name: test
  user:
    token: abc
`

func TestNewClientForContext(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config")
	require.NoError(t, os.WriteFile(path, []byte(testKubeconfig), 0o600))

	cfg, err := loadKubeconfig(path)
	require.NoError(t, err)

	// Empty context falls back to the kubeconfig's current-context.
	c, err := newClientForContext(cfg, "", false)
	require.NoError(t, err)
	require.NotNil(t, c)
	assert.NotNil(t, c.dyn)
	assert.NotNil(t, c.typed)
	assert.NotNil(t, c.disco)
	assert.NotNil(t, c.mapper)

	// Explicit context.
	c2, err := newClientForContext(cfg, "test", false)
	require.NoError(t, err)
	require.NotNil(t, c2)

	// A context the kubeconfig does not define is rejected here rather than
	// silently falling back to current-context.
	_, err = newClientForContext(cfg, "no-such-context", false)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to build client config")
}

func TestLoadKubeconfig_Error(t *testing.T) {
	_, err := loadKubeconfig(filepath.Join(t.TempDir(), "does-not-exist"))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to load kubeconfig")
}

// --- resolve ---

func TestClient_Resolve(t *testing.T) {
	c := newTestClient()

	r, err := c.resolve("v1", "Pod")
	require.NoError(t, err)
	assert.Equal(t, schema.GroupVersionResource{Version: "v1", Resource: "pods"}, r.gvr)
	assert.True(t, r.namespaced)

	r, err = c.resolve("apps/v1", "Deployment")
	require.NoError(t, err)
	assert.Equal(t, "apps", r.gvr.Group)
	assert.Equal(t, "deployments", r.gvr.Resource)
	assert.True(t, r.namespaced)

	r, err = c.resolve("v1", "Node")
	require.NoError(t, err)
	assert.False(t, r.namespaced)

	// Unknown kind → mapper error.
	_, err = c.resolve("v1", "Widget")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unknown resource")

	// Malformed apiVersion → parse error.
	_, err = c.resolve("a/b/c", "Pod")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid api_version")
}

// --- list / get / delete ---

func TestClient_List(t *testing.T) {
	c := newTestClient(
		unstructuredObj("v1", "Pod", "default", "p1"),
		unstructuredObj("v1", "Pod", "default", "p2"),
		unstructuredObj("v1", "Pod", "other", "p3"),
	)
	ctx := context.Background()
	r := podResolved()

	// All namespaces (empty ns).
	all, err := c.list(ctx, r, "", "")
	require.NoError(t, err)
	assert.Len(t, all.Items, 3)

	// Single namespace.
	def, err := c.list(ctx, r, "default", "")
	require.NoError(t, err)
	assert.Len(t, def.Items, 2)

	// With a label selector (threaded through to ListOptions).
	sel, err := c.list(ctx, r, "default", "app=web")
	require.NoError(t, err)
	assert.NotNil(t, sel)
}

func TestClient_Get(t *testing.T) {
	c := newTestClient(unstructuredObj("v1", "Pod", "default", "p1"))
	ctx := context.Background()
	r := podResolved()

	obj, err := c.get(ctx, r, "default", "p1")
	require.NoError(t, err)
	assert.Equal(t, "p1", obj.GetName())

	_, err = c.get(ctx, r, "default", "missing")
	require.Error(t, err)
}

func TestClient_Delete(t *testing.T) {
	c := newTestClient(unstructuredObj("v1", "Pod", "default", "p1"))
	ctx := context.Background()
	r := podResolved()

	require.NoError(t, c.delete(ctx, r, "default", "p1"))

	_, err := c.get(ctx, r, "default", "p1")
	require.Error(t, err)
}

// --- applyResource ---

func TestClient_ApplyResource_Create(t *testing.T) {
	c := newTestClient()
	ctx := context.Background()
	r, err := c.resolve("v1", "ConfigMap")
	require.NoError(t, err)

	obj := unstructuredObj("v1", "ConfigMap", "default", "cm1")
	res, err := c.applyResource(ctx, r, obj)
	require.NoError(t, err)
	assert.Equal(t, "cm1", res.GetName())

	// It now exists.
	got, err := c.get(ctx, r, "default", "cm1")
	require.NoError(t, err)
	assert.Equal(t, "cm1", got.GetName())
}

func TestClient_ApplyResource_Update(t *testing.T) {
	existing := unstructuredObj("v1", "ConfigMap", "default", "cm1")
	c := newTestClient(existing)
	ctx := context.Background()
	r, err := c.resolve("v1", "ConfigMap")
	require.NoError(t, err)

	updated := unstructuredObj("v1", "ConfigMap", "default", "cm1")
	updated.Object["data"] = map[string]any{"key": "value"}

	res, err := c.applyResource(ctx, r, updated)
	require.NoError(t, err)
	data, found, _ := unstructured.NestedString(res.Object, "data", "key")
	assert.True(t, found)
	assert.Equal(t, "value", data)
}

// --- logs ---

func TestClient_Logs(t *testing.T) {
	c := newTestClient()
	c.typed = kfake.NewSimpleClientset(&corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "p1", Namespace: "default"},
	})
	ctx := context.Background()

	out, err := c.logs(ctx, "default", "p1", "", 0)
	require.NoError(t, err)
	assert.Equal(t, "fake logs", out)

	// With tail_lines set (exercises the TailLines branch).
	out, err = c.logs(ctx, "default", "p1", "app", 10)
	require.NoError(t, err)
	assert.Equal(t, "fake logs", out)
}

// --- apiResources ---

func TestClient_APIResources(t *testing.T) {
	// Happy path: discovery returns a list.
	c := newTestClient()
	c.disco = &fakeDiscovery{resources: sampleAPIResourceLists()}
	lists, err := c.apiResources()
	require.NoError(t, err)
	require.Len(t, lists, 1)
	assert.Equal(t, "v1", lists[0].GroupVersion)

	// Partial error: some results plus an error → results returned, error swallowed.
	c.disco = &fakeDiscovery{resources: sampleAPIResourceLists(), err: assertAnError()}
	lists, err = c.apiResources()
	require.NoError(t, err)
	assert.Len(t, lists, 1)

	// Total failure: no results and an error → error surfaced.
	c.disco = &fakeDiscovery{err: assertAnError()}
	_, err = c.apiResources()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "discovery failed")
}

// assertAnError returns a non-nil error for the discovery failure branches.
func assertAnError() error {
	return &partialErr{}
}

type partialErr struct{}

func (*partialErr) Error() string { return "boom" }

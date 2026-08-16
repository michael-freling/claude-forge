package main

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	kfake "k8s.io/client-go/kubernetes/fake"
	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"
)

// runTool is a thin wrapper over executeTool with a default policy, routing to a
// single-context ClientSet.
func runTool(t *testing.T, client *Client, name string, args map[string]any) (string, bool, error) {
	t.Helper()
	return executeTool(context.Background(), name, args, &Policy{}, staticClientSet(client))
}

// --- registry / definitions ---

func TestRegistryComplete(t *testing.T) {
	require.Len(t, toolRegistry, 7)
	for name, td := range toolRegistry {
		assert.NotEqualf(t, accessUnset, td.Access, "tool %q must classify its access", name)
		// Exactly one handler kind: cluster-bound, or about the context set.
		assert.Truef(t, (td.handle == nil) != (td.handleSet == nil),
			"tool %q must set exactly one of handle/handleSet", name)
		assert.Equal(t, name, td.Definition.Name)
	}
}

func TestAllToolDefinitions(t *testing.T) {
	defs := allToolDefinitions()
	assert.Len(t, defs, len(toolRegistry))
	for _, d := range defs {
		require.NotNil(t, d.Annotations, "tool %q missing annotations", d.Name)
	}

	// delete_resource is the destructive write; reads are read-only + idempotent.
	del := toolRegistry["delete_resource"]
	da := annotationsFor(del)
	assert.False(t, da.ReadOnlyHint)
	assert.True(t, da.DestructiveHint)
	assert.False(t, da.IdempotentHint)

	get := toolRegistry["get_resource"]
	ga := annotationsFor(get)
	assert.True(t, ga.ReadOnlyHint)
	assert.False(t, ga.DestructiveHint)
	assert.True(t, ga.IdempotentHint)
}

func TestExecuteTool_Unknown(t *testing.T) {
	_, _, err := runTool(t, newTestClient(), "no_such_tool", map[string]any{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unknown tool")
}

// Every cluster-bound tool advertises the optional context parameter; the
// context-set tool does not, since it is not bound to a cluster.
func TestAllToolDefinitions_ContextProperty(t *testing.T) {
	for _, d := range allToolDefinitions() {
		s, ok := d.InputSchema.(jsonSchema)
		require.Truef(t, ok, "tool %q has an unexpected schema type", d.Name)
		if d.Name == "list_contexts" {
			assert.NotContains(t, s.Properties, "context")
			continue
		}
		assert.Containsf(t, s.Properties, "context", "tool %q must accept a context", d.Name)
		assert.NotContains(t, s.Required, "context", "context must stay optional")
	}

	// The injection copies rather than mutating the shared registry definition.
	registered := toolRegistry["list_api_resources"].Definition.InputSchema.(jsonSchema)
	assert.NotContains(t, registered.Properties, "context")
}

// --- context routing ---

// twoContextClientSet serves two pre-built clients as contexts "a" and "b".
func twoContextClientSet(a, b *Client) *ClientSet {
	return &ClientSet{
		defaultContext: "a",
		infos: []ContextInfo{
			{Name: "a", Cluster: "a", Server: "https://a.example:6443", Default: true},
			{Name: "b", Cluster: "b", Server: "https://b.example:6443"},
		},
		clients: map[string]*Client{"a": a, "b": b},
		newClient: func(*clientcmdapi.Config, string, bool) (*Client, error) {
			return nil, fmt.Errorf("unexpected client build in test")
		},
	}
}

func TestExecuteTool_RoutesByContext(t *testing.T) {
	clients := twoContextClientSet(
		newTestClient(unstructuredObj("v1", "Pod", "default", "pod-in-a")),
		newTestClient(unstructuredObj("v1", "Pod", "default", "pod-in-b")),
	)
	args := func(extra map[string]any) map[string]any {
		a := map[string]any{"api_version": "v1", "kind": "Pod", "namespace": "default"}
		for k, v := range extra {
			a[k] = v
		}
		return a
	}

	// No context named → the default context.
	out, _, err := executeTool(context.Background(), "list_resources", args(nil), &Policy{}, clients)
	require.NoError(t, err)
	assert.Contains(t, out, "pod-in-a")
	assert.NotContains(t, out, "pod-in-b")

	// Explicit context → the other cluster.
	out, _, err = executeTool(context.Background(), "list_resources",
		args(map[string]any{"context": "b"}), &Policy{}, clients)
	require.NoError(t, err)
	assert.Contains(t, out, "pod-in-b")
	assert.NotContains(t, out, "pod-in-a")
}

func TestExecuteTool_UnknownContext(t *testing.T) {
	clients := twoContextClientSet(newTestClient(), newTestClient())
	_, _, err := executeTool(context.Background(), "list_resources", map[string]any{
		"api_version": "v1", "kind": "Pod", "context": "nope",
	}, &Policy{}, clients)
	require.Error(t, err)
	assert.Contains(t, err.Error(), `unknown context "nope"`)
}

func TestExecuteTool_ListContexts(t *testing.T) {
	clients := twoContextClientSet(newTestClient(), newTestClient())
	out, isErr, err := executeTool(context.Background(), "list_contexts", map[string]any{}, &Policy{}, clients)
	require.NoError(t, err)
	assert.False(t, isErr)

	var got []ContextInfo
	require.NoError(t, json.Unmarshal([]byte(out), &got))
	assert.Equal(t, []ContextInfo{
		{Name: "a", Cluster: "a", Server: "https://a.example:6443", Default: true},
		{Name: "b", Cluster: "b", Server: "https://b.example:6443"},
	}, got)
}

// --- happy paths for each tool ---

func TestExecuteTool_ListResources(t *testing.T) {
	c := newTestClient(
		unstructuredObj("v1", "Pod", "default", "p1"),
		unstructuredObj("v1", "Pod", "default", "p2"),
	)
	out, isErr, err := runTool(t, c, "list_resources", map[string]any{
		"api_version":    "v1",
		"kind":           "Pod",
		"namespace":      "default",
		"label_selector": "",
	})
	require.NoError(t, err)
	assert.False(t, isErr)
	assert.Contains(t, out, "p1")
	assert.Contains(t, out, "items")
}

func TestExecuteTool_GetResource(t *testing.T) {
	c := newTestClient(unstructuredObj("v1", "Pod", "default", "p1"))
	out, isErr, err := runTool(t, c, "get_resource", map[string]any{
		"api_version": "v1",
		"kind":        "Pod",
		"name":        "p1",
	})
	require.NoError(t, err)
	assert.False(t, isErr)
	assert.Contains(t, out, "p1")
}

// Cluster-scoped get exercises namespaceFor's non-namespaced branch.
func TestExecuteTool_GetResource_ClusterScoped(t *testing.T) {
	c := newTestClient(unstructuredObj("v1", "Node", "", "node-1"))
	out, _, err := runTool(t, c, "get_resource", map[string]any{
		"api_version": "v1",
		"kind":        "Node",
		"name":        "node-1",
	})
	require.NoError(t, err)
	assert.Contains(t, out, "node-1")
}

func TestExecuteTool_ApplyResource_Create(t *testing.T) {
	c := newTestClient()
	manifest := "apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: cm1\ndata:\n  a: b\n"
	out, isErr, err := runTool(t, c, "apply_resource", map[string]any{"manifest": manifest})
	require.NoError(t, err)
	assert.False(t, isErr)
	assert.Contains(t, out, "cm1")

	// It defaulted the namespace to "default".
	r, _ := c.resolve("v1", "ConfigMap")
	got, err := c.get(context.Background(), r, "default", "cm1")
	require.NoError(t, err)
	assert.Equal(t, "cm1", got.GetName())
}

func TestExecuteTool_DeleteResource(t *testing.T) {
	c := newTestClient(unstructuredObj("v1", "Pod", "default", "p1"))
	out, isErr, err := runTool(t, c, "delete_resource", map[string]any{
		"api_version": "v1",
		"kind":        "Pod",
		"name":        "p1",
	})
	require.NoError(t, err)
	assert.False(t, isErr)
	assert.Contains(t, out, "deleted")
}

func TestExecuteTool_GetLogs(t *testing.T) {
	c := newTestClient()
	c.typed = kfake.NewSimpleClientset()
	out, isErr, err := runTool(t, c, "get_logs", map[string]any{
		"name":       "p1",
		"namespace":  "default",
		"tail_lines": float64(5),
	})
	require.NoError(t, err)
	assert.False(t, isErr)
	assert.Equal(t, "fake logs", out)
}

// get_logs with no namespace defaults to "default".
func TestExecuteTool_GetLogs_DefaultNamespace(t *testing.T) {
	c := newTestClient()
	out, _, err := runTool(t, c, "get_logs", map[string]any{"name": "p1"})
	require.NoError(t, err)
	assert.Equal(t, "fake logs", out)
}

func TestExecuteTool_ListAPIResources(t *testing.T) {
	c := newTestClient()
	c.disco = &fakeDiscovery{resources: sampleAPIResourceLists()}
	out, isErr, err := runTool(t, c, "list_api_resources", map[string]any{})
	require.NoError(t, err)
	assert.False(t, isErr)
	assert.Contains(t, out, "Pod")
	assert.Contains(t, out, "nodes")
}

func TestExecuteTool_ListAPIResources_Error(t *testing.T) {
	c := newTestClient()
	c.disco = &fakeDiscovery{err: assertAnError()}
	_, _, err := runTool(t, c, "list_api_resources", map[string]any{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "discovery failed")
}

// --- policy denials routed through executeTool ---

func TestExecuteTool_DeniedSecrets(t *testing.T) {
	c := newTestClient(unstructuredObj("v1", "Secret", "default", "s1"))

	for _, tt := range []struct {
		tool string
		args map[string]any
	}{
		{"get_resource", map[string]any{"api_version": "v1", "kind": "Secret", "name": "s1"}},
		{"delete_resource", map[string]any{"api_version": "v1", "kind": "Secret", "name": "s1"}},
	} {
		t.Run(tt.tool, func(t *testing.T) {
			_, _, err := runTool(t, c, tt.tool, tt.args)
			require.Error(t, err)
			assert.Contains(t, err.Error(), "denied")
		})
	}
}

func TestExecuteTool_ApplyResource_DeniedSecret(t *testing.T) {
	c := newTestClient()
	manifest := "apiVersion: v1\nkind: Secret\nmetadata:\n  name: s1\n"
	_, _, err := runTool(t, c, "apply_resource", map[string]any{"manifest": manifest})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "denied")
}

// --- argument validation / error branches ---

func TestExecuteTool_MissingRequiredArgs(t *testing.T) {
	c := newTestClient()
	cases := []struct {
		name string
		tool string
		args map[string]any
		want string
	}{
		{"list missing api_version", "list_resources", map[string]any{"kind": "Pod"}, "api_version"},
		{"list missing kind", "list_resources", map[string]any{"api_version": "v1"}, "kind"},
		{"get missing name", "get_resource", map[string]any{"api_version": "v1", "kind": "Pod"}, "name"},
		{"get missing api_version", "get_resource", map[string]any{"kind": "Pod", "name": "p1"}, "api_version"},
		{"get missing kind", "get_resource", map[string]any{"api_version": "v1", "name": "p1"}, "kind"},
		{"delete missing name", "delete_resource", map[string]any{"api_version": "v1", "kind": "Pod"}, "name"},
		{"delete missing api_version", "delete_resource", map[string]any{"kind": "Pod", "name": "p1"}, "api_version"},
		{"delete missing kind", "delete_resource", map[string]any{"api_version": "v1", "name": "p1"}, "kind"},
		{"apply missing manifest", "apply_resource", map[string]any{}, "manifest"},
		{"logs missing name", "get_logs", map[string]any{}, "name"},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			_, _, err := runTool(t, c, tt.tool, tt.args)
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.want)
		})
	}
}

func TestExecuteTool_ApplyResource_Errors(t *testing.T) {
	c := newTestClient()
	cases := []struct {
		name     string
		manifest string
		want     string
	}{
		{"parse error", "just-a-string", "invalid manifest"},
		{"missing gvk", "metadata:\n  name: x\n", "apiVersion and kind"},
		{"missing name", "apiVersion: v1\nkind: ConfigMap\n", "metadata.name"},
		{"unknown kind", "apiVersion: v1\nkind: Widget\nmetadata:\n  name: w1\n", "unknown resource"},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			_, _, err := runTool(t, c, "apply_resource", map[string]any{"manifest": tt.manifest})
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.want)
		})
	}
}

// A resolve failure inside a read tool surfaces the error.
func TestExecuteTool_UnknownKind(t *testing.T) {
	c := newTestClient()
	_, _, err := runTool(t, c, "get_resource", map[string]any{
		"api_version": "v1", "kind": "Widget", "name": "x",
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unknown resource")
}

// A get on a resolvable-but-absent object surfaces the API error.
func TestExecuteTool_GetResource_NotFound(t *testing.T) {
	c := newTestClient()
	_, _, err := runTool(t, c, "get_resource", map[string]any{
		"api_version": "v1", "kind": "Pod", "name": "missing",
	})
	require.Error(t, err)
}

// --- helper functions ---

func TestArgHelpers(t *testing.T) {
	args := map[string]any{
		"str":   "hello",
		"num":   float64(3),
		"int":   int(4),
		"int64": int64(5),
		"bad":   true,
	}
	assert.Equal(t, "hello", getString(args, "str"))
	assert.Equal(t, "", getString(args, "missing"))
	assert.Equal(t, "", getString(args, "num")) // present but not a string

	assert.Equal(t, int64(3), getInt64(args, "num"))
	assert.Equal(t, int64(4), getInt64(args, "int"))
	assert.Equal(t, int64(5), getInt64(args, "int64"))
	assert.Equal(t, int64(0), getInt64(args, "missing"))
	assert.Equal(t, int64(0), getInt64(args, "bad")) // present but not numeric

	v, err := requireString(args, "str")
	require.NoError(t, err)
	assert.Equal(t, "hello", v)
	_, err = requireString(args, "missing")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "missing required parameter")
}

func TestNamespaceFor(t *testing.T) {
	nsRes := resolved{namespaced: true}
	assert.Equal(t, "default", namespaceFor(nsRes, map[string]any{}))
	assert.Equal(t, "prod", namespaceFor(nsRes, map[string]any{"namespace": "prod"}))

	clusterRes := resolved{namespaced: false}
	assert.Equal(t, "", namespaceFor(clusterRes, map[string]any{"namespace": "prod"}))
}

func TestToJSON_Error(t *testing.T) {
	_, err := toJSON(make(chan int))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to encode result")
}

func TestGVKProps(t *testing.T) {
	p := gvkProps(map[string]property{"extra": {Type: "string"}})
	assert.Contains(t, p, "api_version")
	assert.Contains(t, p, "kind")
	assert.Contains(t, p, "namespace")
	assert.Contains(t, p, "extra")
	assert.Equal(t, "string", strings.ToLower(p["extra"].Type))
}

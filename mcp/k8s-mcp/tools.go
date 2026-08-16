package main

import (
	"context"
	"encoding/json"
	"fmt"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/yaml"
)

// access classifies a tool as a read or a write. The zero value is invalid so a
// newly added tool that forgets to classify fails the registry-completeness
// test rather than silently shipping.
type access int

const (
	accessUnset access = iota
	accessRead
	accessWrite
)

type toolAnnotations struct {
	ReadOnlyHint    bool `json:"readOnlyHint"`
	DestructiveHint bool `json:"destructiveHint"`
	IdempotentHint  bool `json:"idempotentHint"`
}

// ToolDefinition is the MCP tool definition returned by tools/list.
type ToolDefinition struct {
	Name        string           `json:"name"`
	Description string           `json:"description"`
	InputSchema any              `json:"inputSchema"`
	Annotations *toolAnnotations `json:"annotations,omitempty"`
}

type toolDef struct {
	Definition  ToolDefinition
	Access      access
	Destructive bool
	// handle serves a tool bound to one cluster; executeTool resolves the
	// "context" argument to a Client before calling it.
	handle func(ctx context.Context, args map[string]any, policy *Policy, client *Client) (string, bool, error)
	// handleSet serves a tool that is about the set of contexts rather than any
	// single cluster, so it never touches a Kubernetes API. Exactly one of
	// handle/handleSet is set.
	handleSet func(ctx context.Context, args map[string]any, clients *ClientSet) (string, bool, error)
}

type jsonSchema struct {
	Type       string              `json:"type"`
	Properties map[string]property `json:"properties,omitempty"`
	Required   []string            `json:"required,omitempty"`
}

type property struct {
	Type        string `json:"type"`
	Description string `json:"description,omitempty"`
}

var toolRegistry map[string]*toolDef

func init() {
	toolRegistry = map[string]*toolDef{
		"list_resources":     listResourcesTool(),
		"get_resource":       getResourceTool(),
		"apply_resource":     applyResourceTool(),
		"delete_resource":    deleteResourceTool(),
		"get_logs":           getLogsTool(),
		"list_api_resources": listAPIResourcesTool(),
		"list_contexts":      listContextsTool(),
	}
}

func annotationsFor(td *toolDef) *toolAnnotations {
	return &toolAnnotations{
		ReadOnlyHint:    td.Access == accessRead,
		DestructiveHint: td.Destructive,
		IdempotentHint:  !td.Destructive,
	}
}

func allToolDefinitions() []ToolDefinition {
	defs := make([]ToolDefinition, 0, len(toolRegistry))
	for _, td := range toolRegistry {
		def := td.Definition
		def.Annotations = annotationsFor(td)
		if td.handle != nil {
			// Advertised here rather than in each tool's own schema, so a tool
			// added later cannot forget to accept a context.
			def.InputSchema = withContextProperty(def.InputSchema)
		}
		defs = append(defs, def)
	}
	return defs
}

// withContextProperty adds the optional "context" parameter to a cluster-bound
// tool's schema. It copies the property map rather than writing through to the
// registry's shared definition.
func withContextProperty(schema any) any {
	s, ok := schema.(jsonSchema)
	if !ok {
		return schema
	}
	props := make(map[string]property, len(s.Properties)+1)
	for k, v := range s.Properties {
		props[k] = v
	}
	props["context"] = property{
		Type: "string",
		Description: "Kubeconfig context to target. Defaults to the kubeconfig's current-context; " +
			"call list_contexts to see every cluster this server can reach.",
	}
	s.Properties = props
	return s
}

// executeTool runs a tool call, resolving the "context" argument to the Client
// for that cluster.
func executeTool(ctx context.Context, name string, args map[string]any, policy *Policy, clients *ClientSet) (string, bool, error) {
	td, ok := toolRegistry[name]
	if !ok {
		return "", false, fmt.Errorf("unknown tool: %s", name)
	}
	if td.handleSet != nil {
		return td.handleSet(ctx, args, clients)
	}
	client, err := clients.Get(getString(args, "context"))
	if err != nil {
		return "", false, err
	}
	return td.handle(ctx, args, policy, client)
}

// --- argument helpers ---

func getString(args map[string]any, key string) string {
	if v, ok := args[key]; ok {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return ""
}

func getInt64(args map[string]any, key string) int64 {
	if v, ok := args[key]; ok {
		switch n := v.(type) {
		case float64:
			return int64(n)
		case int:
			return int64(n)
		case int64:
			return n
		}
	}
	return 0
}

func requireString(args map[string]any, key string) (string, error) {
	if v := getString(args, key); v != "" {
		return v, nil
	}
	return "", fmt.Errorf("missing required parameter: %s", key)
}

// namespaceFor returns the namespace to use for a namespaced resource,
// defaulting to "default" when none is given. Returns "" for cluster-scoped.
func namespaceFor(r resolved, args map[string]any) string {
	if !r.namespaced {
		return ""
	}
	if ns := getString(args, "namespace"); ns != "" {
		return ns
	}
	return "default"
}

func toJSON(v any) (string, error) {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return "", fmt.Errorf("failed to encode result: %w", err)
	}
	return string(data), nil
}

// resolveAndCheck resolves api_version/kind, then enforces the policy for the
// given verb before any API call is made.
func resolveAndCheck(client *Client, policy *Policy, apiVersion, kind, subresource, verb string) (resolved, error) {
	r, err := client.resolve(apiVersion, kind)
	if err != nil {
		return resolved{}, err
	}
	if err := policy.CheckResource(r.gvr.Group, r.gvr.Resource, subresource, verb, r.namespaced); err != nil {
		return resolved{}, err
	}
	return r, nil
}

// gvkProps are the api_version/kind properties shared by most tools.
func gvkProps(extra map[string]property) map[string]property {
	p := map[string]property{
		"api_version": {Type: "string", Description: "apiVersion, e.g. \"v1\" or \"apps/v1\""},
		"kind":        {Type: "string", Description: "Kind, e.g. \"Pod\" or \"Deployment\""},
		"namespace":   {Type: "string", Description: "Namespace (namespaced kinds; defaults to \"default\")"},
	}
	for k, v := range extra {
		p[k] = v
	}
	return p
}

// --- tools ---

func listResourcesTool() *toolDef {
	return &toolDef{
		Access: accessRead,
		Definition: ToolDefinition{
			Name:        "list_resources",
			Description: "List Kubernetes resources of a kind. Omit namespace to list across all namespaces.",
			InputSchema: jsonSchema{
				Type: "object",
				Properties: gvkProps(map[string]property{
					"label_selector": {Type: "string", Description: "Label selector, e.g. \"app=web\""},
				}),
				Required: []string{"api_version", "kind"},
			},
		},
		handle: func(ctx context.Context, args map[string]any, policy *Policy, client *Client) (string, bool, error) {
			apiVersion, err := requireString(args, "api_version")
			if err != nil {
				return "", false, err
			}
			kind, err := requireString(args, "kind")
			if err != nil {
				return "", false, err
			}
			r, err := resolveAndCheck(client, policy, apiVersion, kind, "", "list")
			if err != nil {
				return "", false, err
			}
			// Empty namespace lists across all namespaces for namespaced kinds.
			ns := getString(args, "namespace")
			list, err := client.list(ctx, r, ns, getString(args, "label_selector"))
			if err != nil {
				return "", false, err
			}
			out, err := toJSON(list)
			if err != nil {
				return "", false, err
			}
			return out, false, nil
		},
	}
}

func getResourceTool() *toolDef {
	return &toolDef{
		Access: accessRead,
		Definition: ToolDefinition{
			Name:        "get_resource",
			Description: "Get a single Kubernetes resource by name.",
			InputSchema: jsonSchema{
				Type:       "object",
				Properties: gvkProps(map[string]property{"name": {Type: "string", Description: "Resource name"}}),
				Required:   []string{"api_version", "kind", "name"},
			},
		},
		handle: func(ctx context.Context, args map[string]any, policy *Policy, client *Client) (string, bool, error) {
			apiVersion, err := requireString(args, "api_version")
			if err != nil {
				return "", false, err
			}
			kind, err := requireString(args, "kind")
			if err != nil {
				return "", false, err
			}
			name, err := requireString(args, "name")
			if err != nil {
				return "", false, err
			}
			r, err := resolveAndCheck(client, policy, apiVersion, kind, "", "get")
			if err != nil {
				return "", false, err
			}
			obj, err := client.get(ctx, r, namespaceFor(r, args), name)
			if err != nil {
				return "", false, err
			}
			out, err := toJSON(obj.Object)
			if err != nil {
				return "", false, err
			}
			return out, false, nil
		},
	}
}

func applyResourceTool() *toolDef {
	return &toolDef{
		Access: accessWrite,
		Definition: ToolDefinition{
			Name:        "apply_resource",
			Description: "Create or update a resource from a YAML or JSON manifest.",
			InputSchema: jsonSchema{
				Type: "object",
				Properties: map[string]property{
					"manifest": {Type: "string", Description: "A single resource manifest (YAML or JSON)"},
				},
				Required: []string{"manifest"},
			},
		},
		handle: func(ctx context.Context, args map[string]any, policy *Policy, client *Client) (string, bool, error) {
			manifest, err := requireString(args, "manifest")
			if err != nil {
				return "", false, err
			}
			var m map[string]any
			if err := yaml.Unmarshal([]byte(manifest), &m); err != nil {
				return "", false, fmt.Errorf("invalid manifest: %w", err)
			}
			obj := &unstructured.Unstructured{Object: m}
			apiVersion := obj.GetAPIVersion()
			kind := obj.GetKind()
			if apiVersion == "" || kind == "" {
				return "", false, fmt.Errorf("manifest must set apiVersion and kind")
			}
			if obj.GetName() == "" {
				return "", false, fmt.Errorf("manifest must set metadata.name")
			}
			r, err := resolveAndCheck(client, policy, apiVersion, kind, "", "update")
			if err != nil {
				return "", false, err
			}
			if r.namespaced && obj.GetNamespace() == "" {
				obj.SetNamespace("default")
			}
			result, err := client.applyResource(ctx, r, obj)
			if err != nil {
				return "", false, err
			}
			out, err := toJSON(result.Object)
			if err != nil {
				return "", false, err
			}
			return out, false, nil
		},
	}
}

func deleteResourceTool() *toolDef {
	return &toolDef{
		Access:      accessWrite,
		Destructive: true,
		Definition: ToolDefinition{
			Name:        "delete_resource",
			Description: "Delete a Kubernetes resource by name.",
			InputSchema: jsonSchema{
				Type:       "object",
				Properties: gvkProps(map[string]property{"name": {Type: "string", Description: "Resource name"}}),
				Required:   []string{"api_version", "kind", "name"},
			},
		},
		handle: func(ctx context.Context, args map[string]any, policy *Policy, client *Client) (string, bool, error) {
			apiVersion, err := requireString(args, "api_version")
			if err != nil {
				return "", false, err
			}
			kind, err := requireString(args, "kind")
			if err != nil {
				return "", false, err
			}
			name, err := requireString(args, "name")
			if err != nil {
				return "", false, err
			}
			r, err := resolveAndCheck(client, policy, apiVersion, kind, "", "delete")
			if err != nil {
				return "", false, err
			}
			if err := client.delete(ctx, r, namespaceFor(r, args), name); err != nil {
				return "", false, err
			}
			return fmt.Sprintf("deleted %s/%s %q", apiVersion, kind, name), false, nil
		},
	}
}

func getLogsTool() *toolDef {
	return &toolDef{
		Access: accessRead,
		Definition: ToolDefinition{
			Name:        "get_logs",
			Description: "Get logs from a pod (optionally a specific container). Capped in size.",
			InputSchema: jsonSchema{
				Type: "object",
				Properties: map[string]property{
					"name":       {Type: "string", Description: "Pod name"},
					"namespace":  {Type: "string", Description: "Pod namespace (defaults to \"default\")"},
					"container":  {Type: "string", Description: "Container name (optional)"},
					"tail_lines": {Type: "number", Description: "Return only the last N lines (optional)"},
				},
				Required: []string{"name"},
			},
		},
		handle: func(ctx context.Context, args map[string]any, policy *Policy, client *Client) (string, bool, error) {
			name, err := requireString(args, "name")
			if err != nil {
				return "", false, err
			}
			// pods/log is a namespaced read; run it through the policy for
			// consistency (it is allowed, unlike pods/exec).
			if err := policy.CheckResource("", "pods", "log", "get", true); err != nil {
				return "", false, err
			}
			ns := getString(args, "namespace")
			if ns == "" {
				ns = "default"
			}
			out, err := client.logs(ctx, ns, name, getString(args, "container"), getInt64(args, "tail_lines"))
			if err != nil {
				return "", false, err
			}
			return out, false, nil
		},
	}
}

func listAPIResourcesTool() *toolDef {
	return &toolDef{
		Access: accessRead,
		Definition: ToolDefinition{
			Name:        "list_api_resources",
			Description: "List the API resources (apiVersion/kind) the cluster serves, to discover valid targets.",
			InputSchema: jsonSchema{Type: "object"},
		},
		handle: func(ctx context.Context, args map[string]any, policy *Policy, client *Client) (string, bool, error) {
			lists, err := client.apiResources()
			if err != nil {
				return "", false, err
			}
			type apiResource struct {
				APIVersion string `json:"apiVersion"`
				Kind       string `json:"kind"`
				Name       string `json:"name"`
				Namespaced bool   `json:"namespaced"`
			}
			var out []apiResource
			for _, l := range lists {
				for _, res := range l.APIResources {
					out = append(out, apiResource{
						APIVersion: l.GroupVersion,
						Kind:       res.Kind,
						Name:       res.Name,
						Namespaced: res.Namespaced,
					})
				}
			}
			s, err := toJSON(out)
			if err != nil {
				return "", false, err
			}
			return s, false, nil
		},
	}
}

func listContextsTool() *toolDef {
	return &toolDef{
		Access: accessRead,
		Definition: ToolDefinition{
			Name: "list_contexts",
			Description: "List the kubeconfig contexts this server can target, with each cluster's " +
				"API endpoint and which context is used when a call names none.",
			InputSchema: jsonSchema{Type: "object"},
		},
		handleSet: func(ctx context.Context, args map[string]any, clients *ClientSet) (string, bool, error) {
			out, err := toJSON(clients.Contexts())
			if err != nil {
				return "", false, err
			}
			return out, false, nil
		},
	}
}

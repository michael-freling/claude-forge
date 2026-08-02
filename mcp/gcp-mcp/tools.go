package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"strings"
)

// toolAnnotations are the MCP behavioral hints attached to each tool so the
// client can reason about it. They are derived centrally from the tool's
// access classification (see annotationsFor) so they cannot drift from the
// policy the server actually enforces.
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

// toolDef is the internal tool definition including execution metadata.
type toolDef struct {
	Definition ToolDefinition
	Access     access

	// build constructs the HTTP request for this tool from its arguments and
	// the server's project/endpoints. A read tool returns a nil body.
	build func(bc buildContext, args map[string]any) (method, fullURL string, body io.Reader, err error)
}

// buildContext carries the per-server values a tool needs to construct a
// request: the default project and the service endpoints.
type buildContext struct {
	project   string
	endpoints endpoints
}

// jsonSchema is a helper for building JSON Schema objects.
type jsonSchema struct {
	Type       string              `json:"type"`
	Properties map[string]property `json:"properties,omitempty"`
	Required   []string            `json:"required,omitempty"`
}

type property struct {
	Type        string   `json:"type"`
	Description string   `json:"description,omitempty"`
	Enum        []string `json:"enum,omitempty"`
}

// maxToolResultBytes bounds the text returned to the model. Asset and logging
// queries can return payloads large enough to blow out the context window;
// past this size the tool asks the model to narrow the query instead.
const maxToolResultBytes = 200 << 10 // 200 KiB

// defaultLogEntries and maxLogEntries bound read_logs, which is otherwise
// unbounded.
const (
	defaultLogEntries = 20
	maxLogEntries     = 100
)

// toolRegistry is the registry of all available MCP tools.
var toolRegistry map[string]*toolDef

func init() {
	toolRegistry = map[string]*toolDef{
		"list_projects":         listProjectsTool(),
		"describe_project":      describeProjectTool(),
		"get_iam_policy":        getIAMPolicyTool(),
		"search_resources":      searchResourcesTool(),
		"list_assets":           listAssetsTool(),
		"list_instances":        listInstancesTool(),
		"describe_instance":     describeInstanceTool(),
		"list_buckets":          listBucketsTool(),
		"list_objects":          listObjectsTool(),
		"list_services":         listServicesTool(),
		"describe_service":      describeServiceTool(),
		"list_clusters":         listClustersTool(),
		"describe_cluster":      describeClusterTool(),
		"list_secrets":          listSecretsTool(),
		"list_secret_versions":  listSecretVersionsTool(),
		"access_secret_version": accessSecretVersionTool(),
		"read_logs":             readLogsTool(),
	}
}

// annotationsFor derives MCP annotations from a tool's access classification so
// the advertised hints always match the enforced policy.
func annotationsFor(a access) *toolAnnotations {
	if a == accessWrite {
		return &toolAnnotations{ReadOnlyHint: false, DestructiveHint: true, IdempotentHint: false}
	}
	// Reads and secret reads mutate nothing.
	return &toolAnnotations{ReadOnlyHint: true, DestructiveHint: false, IdempotentHint: true}
}

// allToolDefinitions returns all tool definitions for the tools/list response,
// with annotations derived from each tool's classification.
func allToolDefinitions() []ToolDefinition {
	defs := make([]ToolDefinition, 0, len(toolRegistry))
	for _, td := range toolRegistry {
		def := td.Definition
		def.Annotations = annotationsFor(td.Access)
		defs = append(defs, def)
	}
	return defs
}

// executeTool runs a tool call against the Google Cloud REST APIs.
func executeTool(ctx context.Context, name string, args map[string]any, policy *Policy, client *Client) (string, bool, error) {
	td, ok := toolRegistry[name]
	if !ok {
		return "", false, fmt.Errorf("unknown tool: %s", name)
	}

	// The policy — not the handler — decides. This runs before the request is
	// built so a blocked tool never reaches Google.
	if err := policy.CheckTool(name, td.Access); err != nil {
		return "", false, err
	}

	method, fullURL, body, err := td.build(client.buildContext(), args)
	if err != nil {
		return "", false, fmt.Errorf("failed to build request: %w", err)
	}

	respBody, status, err := client.do(ctx, method, fullURL, body)
	if err != nil {
		return "", false, err
	}

	out := string(respBody)
	// A secret payload is the one output that must not be redacted — returning
	// it is the entire purpose of the (separately gated) access tool. Every
	// other surface is redacted, since secrets leak through metadata too.
	if td.Access != accessSecret {
		out = redact(out)
	}

	if status >= 400 {
		return out, true, nil
	}

	if len(out) > maxToolResultBytes {
		return fmt.Sprintf("result is too large (%d bytes) to return safely; narrow the query with a filter or a "+
			"smaller page_size/max_results and page through results", len(out)), true, nil
	}

	return out, false, nil
}

// buildContext returns the tool build context for this client.
func (c *Client) buildContext() buildContext {
	return buildContext{project: c.project, endpoints: c.endpoints}
}

// --- argument helpers ---

// getString safely extracts a string from the args map.
func getString(args map[string]any, key string) string {
	if v, ok := args[key]; ok {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return ""
}

// getNumber safely extracts a number (as int) from the args map. JSON numbers
// are unmarshaled as float64.
func getNumber(args map[string]any, key string) (int, bool) {
	if v, ok := args[key]; ok {
		switch n := v.(type) {
		case float64:
			return int(n), true
		case int:
			return n, true
		case json.Number:
			if i, err := n.Int64(); err == nil {
				return int(i), true
			}
		}
	}
	return 0, false
}

// resolveProject returns the project to target: an explicit "project" argument
// overrides the server default.
func resolveProject(args map[string]any, def string) string {
	if p := getString(args, "project"); p != "" {
		return p
	}
	return def
}

// requireProject resolves the project and errors if none is available.
func requireProject(args map[string]any, def string) (string, error) {
	p := resolveProject(args, def)
	if p == "" {
		return "", fmt.Errorf("no project configured: pass a \"project\" argument or start the server with --project")
	}
	return p, nil
}

// requireString returns args[key] or an error if it is empty.
func requireString(args map[string]any, key string) (string, error) {
	if v := getString(args, key); v != "" {
		return v, nil
	}
	return "", fmt.Errorf("missing required parameter: %s", key)
}

// defaulted returns args[key] if present, otherwise def.
func defaulted(args map[string]any, key, def string) string {
	if v := getString(args, key); v != "" {
		return v
	}
	return def
}

// query builds a "?k=v&..." suffix from the given pairs, skipping empty values
// and URL-encoding both key and value. Order is preserved.
func query(pairs ...[2]string) string {
	var b strings.Builder
	sep := "?"
	for _, p := range pairs {
		if p[1] == "" {
			continue
		}
		b.WriteString(sep)
		b.WriteString(url.QueryEscape(p[0]))
		b.WriteString("=")
		b.WriteString(url.QueryEscape(p[1]))
		sep = "&"
	}
	return b.String()
}

// numStr returns the decimal string of args[key], or "" if absent.
func numStr(args map[string]any, key string) string {
	if n, ok := getNumber(args, key); ok {
		return fmt.Sprintf("%d", n)
	}
	return ""
}

// property maps shared by many tools.
func projectProp() map[string]property {
	return map[string]property{
		"project": {Type: "string", Description: "Project ID (optional; overrides the server default)"},
	}
}

// mergeProps returns a new map combining the base project property with extra.
func mergeProps(extra map[string]property) map[string]property {
	out := projectProp()
	for k, v := range extra {
		out[k] = v
	}
	return out
}

// --- Resource Manager ---

func listProjectsTool() *toolDef {
	return &toolDef{
		Access: accessRead,
		Definition: ToolDefinition{
			Name:        "list_projects",
			Description: "List Google Cloud projects visible to the caller (Resource Manager).",
			InputSchema: jsonSchema{
				Type: "object",
				Properties: map[string]property{
					"filter":     {Type: "string", Description: "Server-side filter, e.g. \"labels.env=prod\""},
					"page_size":  {Type: "number", Description: "Maximum results to return"},
					"page_token": {Type: "string", Description: "Continuation token from a previous response"},
				},
			},
		},
		build: func(bc buildContext, args map[string]any) (string, string, io.Reader, error) {
			u := bc.endpoints.resourceManager + "/projects" + query(
				[2]string{"filter", getString(args, "filter")},
				[2]string{"pageSize", numStr(args, "page_size")},
				[2]string{"pageToken", getString(args, "page_token")},
			)
			return "GET", u, nil, nil
		},
	}
}

func describeProjectTool() *toolDef {
	return &toolDef{
		Access: accessRead,
		Definition: ToolDefinition{
			Name:        "describe_project",
			Description: "Get metadata for a single project (Resource Manager).",
			InputSchema: jsonSchema{Type: "object", Properties: projectProp()},
		},
		build: func(bc buildContext, args map[string]any) (string, string, io.Reader, error) {
			project, err := requireProject(args, bc.project)
			if err != nil {
				return "", "", nil, err
			}
			return "GET", bc.endpoints.resourceManager + "/projects/" + url.PathEscape(project), nil, nil
		},
	}
}

func getIAMPolicyTool() *toolDef {
	return &toolDef{
		Access: accessRead,
		Definition: ToolDefinition{
			Name: "get_iam_policy",
			Description: "Get the IAM policy of a project (Resource Manager). Read-only, but it reveals the " +
				"identities of every principal granted a role, so treat the output as sensitive.",
			InputSchema: jsonSchema{Type: "object", Properties: projectProp()},
		},
		build: func(bc buildContext, args map[string]any) (string, string, io.Reader, error) {
			project, err := requireProject(args, bc.project)
			if err != nil {
				return "", "", nil, err
			}
			u := bc.endpoints.resourceManager + "/projects/" + url.PathEscape(project) + ":getIamPolicy"
			return "POST", u, strings.NewReader("{}"), nil
		},
	}
}

// --- Cloud Asset Inventory ---

func searchResourcesTool() *toolDef {
	return &toolDef{
		Access: accessRead,
		Definition: ToolDefinition{
			Name:        "search_resources",
			Description: "Search all Cloud resources in a project (Cloud Asset Inventory searchAllResources). The broadest discovery tool.",
			InputSchema: jsonSchema{
				Type: "object",
				Properties: mergeProps(map[string]property{
					"query":       {Type: "string", Description: "Search query, e.g. \"state:RUNNING\" or \"name:web-*\""},
					"asset_types": {Type: "string", Description: "Comma-separated asset type filters, e.g. \"compute.googleapis.com/Instance\""},
					"page_size":   {Type: "number", Description: "Maximum results to return"},
					"page_token":  {Type: "string", Description: "Continuation token from a previous response"},
				}),
			},
		},
		build: func(bc buildContext, args map[string]any) (string, string, io.Reader, error) {
			project, err := requireProject(args, bc.project)
			if err != nil {
				return "", "", nil, err
			}
			u := bc.endpoints.asset + "/projects/" + url.PathEscape(project) + ":searchAllResources" + query(
				[2]string{"query", getString(args, "query")},
				[2]string{"assetTypes", getString(args, "asset_types")},
				[2]string{"pageSize", numStr(args, "page_size")},
				[2]string{"pageToken", getString(args, "page_token")},
			)
			return "GET", u, nil, nil
		},
	}
}

func listAssetsTool() *toolDef {
	return &toolDef{
		Access: accessRead,
		Definition: ToolDefinition{
			Name:        "list_assets",
			Description: "List Cloud assets in a project (Cloud Asset Inventory).",
			InputSchema: jsonSchema{
				Type: "object",
				Properties: mergeProps(map[string]property{
					"asset_types":  {Type: "string", Description: "Comma-separated asset type filters"},
					"content_type": {Type: "string", Description: "Asset content type", Enum: []string{"RESOURCE", "IAM_POLICY", "ORG_POLICY", "ACCESS_POLICY", "OS_INVENTORY", "RELATIONSHIP"}},
					"page_size":    {Type: "number", Description: "Maximum results to return"},
					"page_token":   {Type: "string", Description: "Continuation token from a previous response"},
				}),
			},
		},
		build: func(bc buildContext, args map[string]any) (string, string, io.Reader, error) {
			project, err := requireProject(args, bc.project)
			if err != nil {
				return "", "", nil, err
			}
			u := bc.endpoints.asset + "/projects/" + url.PathEscape(project) + "/assets" + query(
				[2]string{"assetTypes", getString(args, "asset_types")},
				[2]string{"contentType", getString(args, "content_type")},
				[2]string{"pageSize", numStr(args, "page_size")},
				[2]string{"pageToken", getString(args, "page_token")},
			)
			return "GET", u, nil, nil
		},
	}
}

// --- Compute Engine ---

func listInstancesTool() *toolDef {
	return &toolDef{
		Access: accessRead,
		Definition: ToolDefinition{
			Name:        "list_instances",
			Description: "List Compute Engine VM instances across all zones (aggregated).",
			InputSchema: jsonSchema{
				Type: "object",
				Properties: mergeProps(map[string]property{
					"filter":      {Type: "string", Description: "Server-side filter, e.g. \"status=RUNNING\""},
					"max_results": {Type: "number", Description: "Maximum results per page"},
					"page_token":  {Type: "string", Description: "Continuation token from a previous response"},
				}),
			},
		},
		build: func(bc buildContext, args map[string]any) (string, string, io.Reader, error) {
			project, err := requireProject(args, bc.project)
			if err != nil {
				return "", "", nil, err
			}
			u := bc.endpoints.compute + "/projects/" + url.PathEscape(project) + "/aggregated/instances" + query(
				[2]string{"filter", getString(args, "filter")},
				[2]string{"maxResults", numStr(args, "max_results")},
				[2]string{"pageToken", getString(args, "page_token")},
			)
			return "GET", u, nil, nil
		},
	}
}

func describeInstanceTool() *toolDef {
	return &toolDef{
		Access: accessRead,
		Definition: ToolDefinition{
			Name:        "describe_instance",
			Description: "Get a single Compute Engine instance.",
			InputSchema: jsonSchema{
				Type: "object",
				Properties: mergeProps(map[string]property{
					"zone":     {Type: "string", Description: "Instance zone, e.g. \"us-central1-a\""},
					"instance": {Type: "string", Description: "Instance name"},
				}),
				Required: []string{"zone", "instance"},
			},
		},
		build: func(bc buildContext, args map[string]any) (string, string, io.Reader, error) {
			project, err := requireProject(args, bc.project)
			if err != nil {
				return "", "", nil, err
			}
			zone, err := requireString(args, "zone")
			if err != nil {
				return "", "", nil, err
			}
			instance, err := requireString(args, "instance")
			if err != nil {
				return "", "", nil, err
			}
			u := bc.endpoints.compute + "/projects/" + url.PathEscape(project) + "/zones/" + url.PathEscape(zone) +
				"/instances/" + url.PathEscape(instance)
			return "GET", u, nil, nil
		},
	}
}

// --- Cloud Storage (metadata only) ---

func listBucketsTool() *toolDef {
	return &toolDef{
		Access: accessRead,
		Definition: ToolDefinition{
			Name:        "list_buckets",
			Description: "List Cloud Storage buckets in a project. Returns bucket metadata only, never object contents.",
			InputSchema: jsonSchema{
				Type: "object",
				Properties: mergeProps(map[string]property{
					"max_results": {Type: "number", Description: "Maximum results per page"},
					"page_token":  {Type: "string", Description: "Continuation token from a previous response"},
				}),
			},
		},
		build: func(bc buildContext, args map[string]any) (string, string, io.Reader, error) {
			project, err := requireProject(args, bc.project)
			if err != nil {
				return "", "", nil, err
			}
			u := bc.endpoints.storage + "/b" + query(
				[2]string{"project", project},
				[2]string{"maxResults", numStr(args, "max_results")},
				[2]string{"pageToken", getString(args, "page_token")},
			)
			return "GET", u, nil, nil
		},
	}
}

func listObjectsTool() *toolDef {
	return &toolDef{
		Access: accessRead,
		Definition: ToolDefinition{
			Name: "list_objects",
			Description: "List object metadata in a Cloud Storage bucket (names, sizes, timestamps). " +
				"Never returns object contents.",
			InputSchema: jsonSchema{
				Type: "object",
				Properties: mergeProps(map[string]property{
					"bucket":      {Type: "string", Description: "Bucket name"},
					"prefix":      {Type: "string", Description: "Only list objects whose name begins with this prefix"},
					"max_results": {Type: "number", Description: "Maximum results per page"},
					"page_token":  {Type: "string", Description: "Continuation token from a previous response"},
				}),
				Required: []string{"bucket"},
			},
		},
		build: func(bc buildContext, args map[string]any) (string, string, io.Reader, error) {
			bucket, err := requireString(args, "bucket")
			if err != nil {
				return "", "", nil, err
			}
			u := bc.endpoints.storage + "/b/" + url.PathEscape(bucket) + "/o" + query(
				[2]string{"prefix", getString(args, "prefix")},
				[2]string{"maxResults", numStr(args, "max_results")},
				[2]string{"pageToken", getString(args, "page_token")},
			)
			return "GET", u, nil, nil
		},
	}
}

// --- Cloud Run ---

func listServicesTool() *toolDef {
	return &toolDef{
		Access: accessRead,
		Definition: ToolDefinition{
			Name:        "list_services",
			Description: "List Cloud Run services in a location.",
			InputSchema: jsonSchema{
				Type: "object",
				Properties: mergeProps(map[string]property{
					"location":   {Type: "string", Description: "Region, e.g. \"us-central1\" (default \"-\" for all regions)"},
					"page_size":  {Type: "number", Description: "Maximum results per page"},
					"page_token": {Type: "string", Description: "Continuation token from a previous response"},
				}),
			},
		},
		build: func(bc buildContext, args map[string]any) (string, string, io.Reader, error) {
			project, err := requireProject(args, bc.project)
			if err != nil {
				return "", "", nil, err
			}
			location := defaulted(args, "location", "-")
			u := bc.endpoints.run + "/projects/" + url.PathEscape(project) + "/locations/" + url.PathEscape(location) +
				"/services" + query(
				[2]string{"pageSize", numStr(args, "page_size")},
				[2]string{"pageToken", getString(args, "page_token")},
			)
			return "GET", u, nil, nil
		},
	}
}

func describeServiceTool() *toolDef {
	return &toolDef{
		Access: accessRead,
		Definition: ToolDefinition{
			Name:        "describe_service",
			Description: "Get a single Cloud Run service.",
			InputSchema: jsonSchema{
				Type: "object",
				Properties: mergeProps(map[string]property{
					"location": {Type: "string", Description: "Region, e.g. \"us-central1\""},
					"service":  {Type: "string", Description: "Service name"},
				}),
				Required: []string{"location", "service"},
			},
		},
		build: func(bc buildContext, args map[string]any) (string, string, io.Reader, error) {
			project, err := requireProject(args, bc.project)
			if err != nil {
				return "", "", nil, err
			}
			location, err := requireString(args, "location")
			if err != nil {
				return "", "", nil, err
			}
			service, err := requireString(args, "service")
			if err != nil {
				return "", "", nil, err
			}
			u := bc.endpoints.run + "/projects/" + url.PathEscape(project) + "/locations/" + url.PathEscape(location) +
				"/services/" + url.PathEscape(service)
			return "GET", u, nil, nil
		},
	}
}

// --- GKE ---

func listClustersTool() *toolDef {
	return &toolDef{
		Access: accessRead,
		Definition: ToolDefinition{
			Name:        "list_clusters",
			Description: "List GKE clusters in a location.",
			InputSchema: jsonSchema{
				Type: "object",
				Properties: mergeProps(map[string]property{
					"location": {Type: "string", Description: "Region or zone (default \"-\" for all)"},
				}),
			},
		},
		build: func(bc buildContext, args map[string]any) (string, string, io.Reader, error) {
			project, err := requireProject(args, bc.project)
			if err != nil {
				return "", "", nil, err
			}
			location := defaulted(args, "location", "-")
			u := bc.endpoints.container + "/projects/" + url.PathEscape(project) + "/locations/" + url.PathEscape(location) + "/clusters"
			return "GET", u, nil, nil
		},
	}
}

func describeClusterTool() *toolDef {
	return &toolDef{
		Access: accessRead,
		Definition: ToolDefinition{
			Name:        "describe_cluster",
			Description: "Get a single GKE cluster.",
			InputSchema: jsonSchema{
				Type: "object",
				Properties: mergeProps(map[string]property{
					"location": {Type: "string", Description: "Region or zone, e.g. \"us-central1\""},
					"cluster":  {Type: "string", Description: "Cluster name"},
				}),
				Required: []string{"location", "cluster"},
			},
		},
		build: func(bc buildContext, args map[string]any) (string, string, io.Reader, error) {
			project, err := requireProject(args, bc.project)
			if err != nil {
				return "", "", nil, err
			}
			location, err := requireString(args, "location")
			if err != nil {
				return "", "", nil, err
			}
			cluster, err := requireString(args, "cluster")
			if err != nil {
				return "", "", nil, err
			}
			u := bc.endpoints.container + "/projects/" + url.PathEscape(project) + "/locations/" + url.PathEscape(location) +
				"/clusters/" + url.PathEscape(cluster)
			return "GET", u, nil, nil
		},
	}
}

// --- Secret Manager (metadata by default; payload gated) ---

func listSecretsTool() *toolDef {
	return &toolDef{
		Access: accessRead,
		Definition: ToolDefinition{
			Name:        "list_secrets",
			Description: "List Secret Manager secrets (metadata: names, labels, replication, timestamps). Never returns secret values.",
			InputSchema: jsonSchema{
				Type: "object",
				Properties: mergeProps(map[string]property{
					"filter":     {Type: "string", Description: "Server-side filter"},
					"page_size":  {Type: "number", Description: "Maximum results per page"},
					"page_token": {Type: "string", Description: "Continuation token from a previous response"},
				}),
			},
		},
		build: func(bc buildContext, args map[string]any) (string, string, io.Reader, error) {
			project, err := requireProject(args, bc.project)
			if err != nil {
				return "", "", nil, err
			}
			u := bc.endpoints.secretManager + "/projects/" + url.PathEscape(project) + "/secrets" + query(
				[2]string{"filter", getString(args, "filter")},
				[2]string{"pageSize", numStr(args, "page_size")},
				[2]string{"pageToken", getString(args, "page_token")},
			)
			return "GET", u, nil, nil
		},
	}
}

func listSecretVersionsTool() *toolDef {
	return &toolDef{
		Access: accessRead,
		Definition: ToolDefinition{
			Name:        "list_secret_versions",
			Description: "List versions of a Secret Manager secret (metadata: version numbers, state, timestamps). Never returns secret values.",
			InputSchema: jsonSchema{
				Type: "object",
				Properties: mergeProps(map[string]property{
					"secret":     {Type: "string", Description: "Secret ID"},
					"page_size":  {Type: "number", Description: "Maximum results per page"},
					"page_token": {Type: "string", Description: "Continuation token from a previous response"},
				}),
				Required: []string{"secret"},
			},
		},
		build: func(bc buildContext, args map[string]any) (string, string, io.Reader, error) {
			project, err := requireProject(args, bc.project)
			if err != nil {
				return "", "", nil, err
			}
			secret, err := requireString(args, "secret")
			if err != nil {
				return "", "", nil, err
			}
			u := bc.endpoints.secretManager + "/projects/" + url.PathEscape(project) + "/secrets/" + url.PathEscape(secret) +
				"/versions" + query(
				[2]string{"pageSize", numStr(args, "page_size")},
				[2]string{"pageToken", getString(args, "page_token")},
			)
			return "GET", u, nil, nil
		},
	}
}

func accessSecretVersionTool() *toolDef {
	return &toolDef{
		Access: accessSecret,
		Definition: ToolDefinition{
			Name: "access_secret_version",
			Description: "Access (decrypt and return) the payload of a Secret Manager secret version. " +
				"Disabled unless the server was started with --allow-secret-access.",
			InputSchema: jsonSchema{
				Type: "object",
				Properties: mergeProps(map[string]property{
					"secret":  {Type: "string", Description: "Secret ID"},
					"version": {Type: "string", Description: "Version number or \"latest\" (default \"latest\")"},
				}),
				Required: []string{"secret"},
			},
		},
		build: func(bc buildContext, args map[string]any) (string, string, io.Reader, error) {
			project, err := requireProject(args, bc.project)
			if err != nil {
				return "", "", nil, err
			}
			secret, err := requireString(args, "secret")
			if err != nil {
				return "", "", nil, err
			}
			version := defaulted(args, "version", "latest")
			u := bc.endpoints.secretManager + "/projects/" + url.PathEscape(project) + "/secrets/" + url.PathEscape(secret) +
				"/versions/" + url.PathEscape(version) + ":access"
			return "GET", u, nil, nil
		},
	}
}

// --- Cloud Logging ---

func readLogsTool() *toolDef {
	return &toolDef{
		Access: accessRead,
		Definition: ToolDefinition{
			Name:        "read_logs",
			Description: "Read recent log entries for a project (Cloud Logging entries:list). Results are capped to protect context.",
			InputSchema: jsonSchema{
				Type: "object",
				Properties: mergeProps(map[string]property{
					"filter":     {Type: "string", Description: "Logging filter, e.g. \"severity>=ERROR\""},
					"order_by":   {Type: "string", Description: "Sort order", Enum: []string{"timestamp desc", "timestamp asc"}},
					"page_size":  {Type: "number", Description: fmt.Sprintf("Maximum entries (default %d, max %d)", defaultLogEntries, maxLogEntries)},
					"page_token": {Type: "string", Description: "Continuation token from a previous response"},
				}),
			},
		},
		build: func(bc buildContext, args map[string]any) (string, string, io.Reader, error) {
			project, err := requireProject(args, bc.project)
			if err != nil {
				return "", "", nil, err
			}
			pageSize := defaultLogEntries
			if n, ok := getNumber(args, "page_size"); ok && n > 0 {
				pageSize = n
			}
			if pageSize > maxLogEntries {
				pageSize = maxLogEntries
			}
			payload := map[string]any{
				"resourceNames": []string{"projects/" + project},
				"pageSize":      pageSize,
			}
			if f := getString(args, "filter"); f != "" {
				payload["filter"] = f
			}
			orderBy := defaulted(args, "order_by", "timestamp desc")
			payload["orderBy"] = orderBy
			if t := getString(args, "page_token"); t != "" {
				payload["pageToken"] = t
			}
			data, err := json.Marshal(payload)
			if err != nil {
				return "", "", nil, fmt.Errorf("failed to marshal request: %w", err)
			}
			return "POST", bc.endpoints.logging + "/entries:list", strings.NewReader(string(data)), nil
		},
	}
}

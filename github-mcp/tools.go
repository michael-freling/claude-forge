package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

// ToolDefinition is the MCP tool definition returned by tools/list.
type ToolDefinition struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	InputSchema any    `json:"inputSchema"`
}

// toolDef is the internal tool definition that includes execution metadata.
type toolDef struct {
	Definition ToolDefinition
	Method     string // HTTP method (GET, POST, PATCH, PUT, DELETE)
	PathTmpl   string // URL path template with {owner}, {repo}, {number}, etc.
	IsWrite    bool   // whether this is a write operation

	// buildRequest constructs the GitHub API method, path, and body from the
	// tool arguments and the server's configured owner/repo.
	buildRequest func(args map[string]any, owner, repo string) (method, path string, body io.Reader, err error)
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

// toolRegistry is the registry of all available MCP tools.
var toolRegistry map[string]*toolDef

func init() {
	toolRegistry = map[string]*toolDef{
		"github_pr_list":       prListTool(),
		"github_pr_get":        prGetTool(),
		"github_pr_create":     prCreateTool(),
		"github_pr_update":     prUpdateTool(),
		"github_pr_merge":      prMergeTool(),
		"github_pr_comment":    prCommentTool(),
		"github_pr_reviews":    prReviewsTool(),
		"github_issue_list":    issueListTool(),
		"github_issue_get":     issueGetTool(),
		"github_issue_create":  issueCreateTool(),
		"github_issue_comment": issueCommentTool(),
		"github_repo_get":      repoGetTool(),
		"github_release_list":  releaseListTool(),
		"github_checks_list":   checksListTool(),
		"github_api":           apiTool(),
	}
}

// allToolDefinitions returns all tool definitions for the tools/list response.
func allToolDefinitions() []ToolDefinition {
	defs := make([]ToolDefinition, 0, len(toolRegistry))
	for _, td := range toolRegistry {
		defs = append(defs, td.Definition)
	}
	return defs
}

// executeTool runs a tool call against the GitHub API.
func executeTool(ctx context.Context, name string, args map[string]any, owner, repo string, policy *Policy, client *GitHubClient) (string, bool, error) {
	td, ok := toolRegistry[name]
	if !ok {
		return "", false, fmt.Errorf("unknown tool: %s", name)
	}

	method, path, body, err := td.buildRequest(args, owner, repo)
	if err != nil {
		return "", false, fmt.Errorf("failed to build request: %w", err)
	}

	// Determine target owner/repo for policy check.
	targetOwner, targetRepo := extractOwnerRepo(path)
	isWrite := td.IsWrite
	// For github_api, determine read/write from the method.
	if name == "github_api" {
		isWrite = method != http.MethodGet && method != http.MethodHead
	}

	if err := policy.CheckTool(name, isWrite, targetOwner, targetRepo); err != nil {
		return "", false, err
	}

	resp, err := client.Do(ctx, method, path, body)
	if err != nil {
		return "", false, fmt.Errorf("GitHub API request failed: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", false, fmt.Errorf("failed to read response: %w", err)
	}

	if resp.StatusCode >= 400 {
		return string(respBody), true, nil
	}

	return string(respBody), false, nil
}

// extractOwnerRepo extracts the owner and repo from a GitHub API path.
func extractOwnerRepo(path string) (string, string) {
	path = strings.TrimPrefix(path, "/")
	parts := strings.Split(path, "/")
	for i := 0; i < len(parts)-2; i++ {
		if parts[i] == "repos" {
			return parts[i+1], parts[i+2]
		}
	}
	return "", ""
}

// resolveOwnerRepo returns the owner/repo to use for a read-only tool call.
// If the args contain owner/repo overrides, those are used; otherwise the
// server's configured owner/repo is used.
func resolveOwnerRepo(args map[string]any, defaultOwner, defaultRepo string) (string, string) {
	owner := defaultOwner
	repo := defaultRepo
	if v, ok := args["owner"]; ok {
		if s, ok := v.(string); ok && s != "" {
			owner = s
		}
	}
	if v, ok := args["repo"]; ok {
		if s, ok := v.(string); ok && s != "" {
			repo = s
		}
	}
	return owner, repo
}

// getString safely extracts a string from the args map.
func getString(args map[string]any, key string) string {
	if v, ok := args[key]; ok {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return ""
}

// getNumber safely extracts a number (as int) from the args map.
// JSON numbers are unmarshaled as float64.
func getNumber(args map[string]any, key string) (int, bool) {
	if v, ok := args[key]; ok {
		switch n := v.(type) {
		case float64:
			return int(n), true
		case int:
			return n, true
		case json.Number:
			i, err := n.Int64()
			if err == nil {
				return int(i), true
			}
		}
	}
	return 0, false
}

// addQueryParam adds a query parameter to a path if the value is non-empty.
func addQueryParam(path, key, value string) string {
	if value == "" {
		return path
	}
	sep := "?"
	if strings.Contains(path, "?") {
		sep = "&"
	}
	return path + sep + url.QueryEscape(key) + "=" + url.QueryEscape(value)
}

// addQueryParamInt adds an integer query parameter to a path if present in args.
func addQueryParamInt(path string, args map[string]any, key string) string {
	if n, ok := getNumber(args, key); ok {
		return addQueryParam(path, key, fmt.Sprintf("%d", n))
	}
	return path
}

// jsonBody creates a JSON body from a map.
func jsonBody(m map[string]any) io.Reader {
	data, _ := json.Marshal(m)
	return strings.NewReader(string(data))
}

// --- Read tools (allow owner/repo override) ---

func prListTool() *toolDef {
	return &toolDef{
		Definition: ToolDefinition{
			Name:        "github_pr_list",
			Description: "List pull requests",
			InputSchema: jsonSchema{
				Type: "object",
				Properties: map[string]property{
					"owner":     {Type: "string", Description: "Repository owner (optional, overrides default)"},
					"repo":      {Type: "string", Description: "Repository name (optional, overrides default)"},
					"state":     {Type: "string", Description: "Filter by state", Enum: []string{"open", "closed", "all"}},
					"per_page":  {Type: "number", Description: "Results per page (max 100)"},
					"sort":      {Type: "string", Description: "Sort field", Enum: []string{"created", "updated", "popularity", "long-running"}},
					"direction": {Type: "string", Description: "Sort direction", Enum: []string{"asc", "desc"}},
					"page":      {Type: "number", Description: "Page number"},
				},
			},
		},
		Method:   http.MethodGet,
		PathTmpl: "/repos/{owner}/{repo}/pulls",
		IsWrite:  false,
		buildRequest: func(args map[string]any, owner, repo string) (string, string, io.Reader, error) {
			o, r := resolveOwnerRepo(args, owner, repo)
			path := fmt.Sprintf("/repos/%s/%s/pulls", o, r)
			path = addQueryParam(path, "state", getString(args, "state"))
			path = addQueryParamInt(path, args, "per_page")
			path = addQueryParam(path, "sort", getString(args, "sort"))
			path = addQueryParam(path, "direction", getString(args, "direction"))
			path = addQueryParamInt(path, args, "page")
			return http.MethodGet, path, nil, nil
		},
	}
}

func prGetTool() *toolDef {
	return &toolDef{
		Definition: ToolDefinition{
			Name:        "github_pr_get",
			Description: "Get a pull request",
			InputSchema: jsonSchema{
				Type: "object",
				Properties: map[string]property{
					"owner":  {Type: "string", Description: "Repository owner (optional, overrides default)"},
					"repo":   {Type: "string", Description: "Repository name (optional, overrides default)"},
					"number": {Type: "number", Description: "Pull request number"},
				},
				Required: []string{"number"},
			},
		},
		Method:   http.MethodGet,
		PathTmpl: "/repos/{owner}/{repo}/pulls/{number}",
		IsWrite:  false,
		buildRequest: func(args map[string]any, owner, repo string) (string, string, io.Reader, error) {
			num, ok := getNumber(args, "number")
			if !ok {
				return "", "", nil, fmt.Errorf("missing required parameter: number")
			}
			o, r := resolveOwnerRepo(args, owner, repo)
			path := fmt.Sprintf("/repos/%s/%s/pulls/%d", o, r, num)
			return http.MethodGet, path, nil, nil
		},
	}
}

func prReviewsTool() *toolDef {
	return &toolDef{
		Definition: ToolDefinition{
			Name:        "github_pr_reviews",
			Description: "List PR reviews",
			InputSchema: jsonSchema{
				Type: "object",
				Properties: map[string]property{
					"owner":  {Type: "string", Description: "Repository owner (optional, overrides default)"},
					"repo":   {Type: "string", Description: "Repository name (optional, overrides default)"},
					"number": {Type: "number", Description: "Pull request number"},
				},
				Required: []string{"number"},
			},
		},
		Method:   http.MethodGet,
		PathTmpl: "/repos/{owner}/{repo}/pulls/{number}/reviews",
		IsWrite:  false,
		buildRequest: func(args map[string]any, owner, repo string) (string, string, io.Reader, error) {
			num, ok := getNumber(args, "number")
			if !ok {
				return "", "", nil, fmt.Errorf("missing required parameter: number")
			}
			o, r := resolveOwnerRepo(args, owner, repo)
			path := fmt.Sprintf("/repos/%s/%s/pulls/%d/reviews", o, r, num)
			return http.MethodGet, path, nil, nil
		},
	}
}

func issueListTool() *toolDef {
	return &toolDef{
		Definition: ToolDefinition{
			Name:        "github_issue_list",
			Description: "List issues",
			InputSchema: jsonSchema{
				Type: "object",
				Properties: map[string]property{
					"owner":    {Type: "string", Description: "Repository owner (optional, overrides default)"},
					"repo":     {Type: "string", Description: "Repository name (optional, overrides default)"},
					"state":    {Type: "string", Description: "Filter by state", Enum: []string{"open", "closed", "all"}},
					"per_page": {Type: "number", Description: "Results per page (max 100)"},
					"labels":   {Type: "string", Description: "Comma-separated list of label names"},
					"page":     {Type: "number", Description: "Page number"},
				},
			},
		},
		Method:   http.MethodGet,
		PathTmpl: "/repos/{owner}/{repo}/issues",
		IsWrite:  false,
		buildRequest: func(args map[string]any, owner, repo string) (string, string, io.Reader, error) {
			o, r := resolveOwnerRepo(args, owner, repo)
			path := fmt.Sprintf("/repos/%s/%s/issues", o, r)
			path = addQueryParam(path, "state", getString(args, "state"))
			path = addQueryParamInt(path, args, "per_page")
			path = addQueryParam(path, "labels", getString(args, "labels"))
			path = addQueryParamInt(path, args, "page")
			return http.MethodGet, path, nil, nil
		},
	}
}

func issueGetTool() *toolDef {
	return &toolDef{
		Definition: ToolDefinition{
			Name:        "github_issue_get",
			Description: "Get an issue",
			InputSchema: jsonSchema{
				Type: "object",
				Properties: map[string]property{
					"owner":  {Type: "string", Description: "Repository owner (optional, overrides default)"},
					"repo":   {Type: "string", Description: "Repository name (optional, overrides default)"},
					"number": {Type: "number", Description: "Issue number"},
				},
				Required: []string{"number"},
			},
		},
		Method:   http.MethodGet,
		PathTmpl: "/repos/{owner}/{repo}/issues/{number}",
		IsWrite:  false,
		buildRequest: func(args map[string]any, owner, repo string) (string, string, io.Reader, error) {
			num, ok := getNumber(args, "number")
			if !ok {
				return "", "", nil, fmt.Errorf("missing required parameter: number")
			}
			o, r := resolveOwnerRepo(args, owner, repo)
			path := fmt.Sprintf("/repos/%s/%s/issues/%d", o, r, num)
			return http.MethodGet, path, nil, nil
		},
	}
}

func repoGetTool() *toolDef {
	return &toolDef{
		Definition: ToolDefinition{
			Name:        "github_repo_get",
			Description: "Get repository info",
			InputSchema: jsonSchema{
				Type: "object",
				Properties: map[string]property{
					"owner": {Type: "string", Description: "Repository owner (optional, overrides default)"},
					"repo":  {Type: "string", Description: "Repository name (optional, overrides default)"},
				},
			},
		},
		Method:   http.MethodGet,
		PathTmpl: "/repos/{owner}/{repo}",
		IsWrite:  false,
		buildRequest: func(args map[string]any, owner, repo string) (string, string, io.Reader, error) {
			o, r := resolveOwnerRepo(args, owner, repo)
			path := fmt.Sprintf("/repos/%s/%s", o, r)
			return http.MethodGet, path, nil, nil
		},
	}
}

func releaseListTool() *toolDef {
	return &toolDef{
		Definition: ToolDefinition{
			Name:        "github_release_list",
			Description: "List releases",
			InputSchema: jsonSchema{
				Type: "object",
				Properties: map[string]property{
					"owner":    {Type: "string", Description: "Repository owner (optional, overrides default)"},
					"repo":     {Type: "string", Description: "Repository name (optional, overrides default)"},
					"per_page": {Type: "number", Description: "Results per page (max 100)"},
				},
			},
		},
		Method:   http.MethodGet,
		PathTmpl: "/repos/{owner}/{repo}/releases",
		IsWrite:  false,
		buildRequest: func(args map[string]any, owner, repo string) (string, string, io.Reader, error) {
			o, r := resolveOwnerRepo(args, owner, repo)
			path := fmt.Sprintf("/repos/%s/%s/releases", o, r)
			path = addQueryParamInt(path, args, "per_page")
			return http.MethodGet, path, nil, nil
		},
	}
}

func checksListTool() *toolDef {
	return &toolDef{
		Definition: ToolDefinition{
			Name:        "github_checks_list",
			Description: "List check runs for a ref",
			InputSchema: jsonSchema{
				Type: "object",
				Properties: map[string]property{
					"owner": {Type: "string", Description: "Repository owner (optional, overrides default)"},
					"repo":  {Type: "string", Description: "Repository name (optional, overrides default)"},
					"ref":   {Type: "string", Description: "Git ref (branch, tag, or SHA)"},
				},
				Required: []string{"ref"},
			},
		},
		Method:   http.MethodGet,
		PathTmpl: "/repos/{owner}/{repo}/commits/{ref}/check-runs",
		IsWrite:  false,
		buildRequest: func(args map[string]any, owner, repo string) (string, string, io.Reader, error) {
			ref := getString(args, "ref")
			if ref == "" {
				return "", "", nil, fmt.Errorf("missing required parameter: ref")
			}
			o, r := resolveOwnerRepo(args, owner, repo)
			path := fmt.Sprintf("/repos/%s/%s/commits/%s/check-runs", o, r, ref)
			return http.MethodGet, path, nil, nil
		},
	}
}

// --- Write tools (always use configured owner/repo) ---

func prCreateTool() *toolDef {
	return &toolDef{
		Definition: ToolDefinition{
			Name:        "github_pr_create",
			Description: "Create a pull request",
			InputSchema: jsonSchema{
				Type: "object",
				Properties: map[string]property{
					"title": {Type: "string", Description: "PR title"},
					"head":  {Type: "string", Description: "Branch containing changes"},
					"body":  {Type: "string", Description: "PR description"},
					"base":  {Type: "string", Description: "Branch to merge into (default: repo default branch)"},
				},
				Required: []string{"title", "head"},
			},
		},
		Method:   http.MethodPost,
		PathTmpl: "/repos/{owner}/{repo}/pulls",
		IsWrite:  true,
		buildRequest: func(args map[string]any, owner, repo string) (string, string, io.Reader, error) {
			title := getString(args, "title")
			if title == "" {
				return "", "", nil, fmt.Errorf("missing required parameter: title")
			}
			head := getString(args, "head")
			if head == "" {
				return "", "", nil, fmt.Errorf("missing required parameter: head")
			}
			path := fmt.Sprintf("/repos/%s/%s/pulls", owner, repo)
			bodyMap := map[string]any{"title": title, "head": head}
			if v := getString(args, "body"); v != "" {
				bodyMap["body"] = v
			}
			if v := getString(args, "base"); v != "" {
				bodyMap["base"] = v
			}
			return http.MethodPost, path, jsonBody(bodyMap), nil
		},
	}
}

func prUpdateTool() *toolDef {
	return &toolDef{
		Definition: ToolDefinition{
			Name:        "github_pr_update",
			Description: "Update a pull request",
			InputSchema: jsonSchema{
				Type: "object",
				Properties: map[string]property{
					"number": {Type: "number", Description: "Pull request number"},
					"title":  {Type: "string", Description: "New title"},
					"body":   {Type: "string", Description: "New description"},
					"state":  {Type: "string", Description: "New state", Enum: []string{"open", "closed"}},
					"base":   {Type: "string", Description: "New base branch"},
				},
				Required: []string{"number"},
			},
		},
		Method:   http.MethodPatch,
		PathTmpl: "/repos/{owner}/{repo}/pulls/{number}",
		IsWrite:  true,
		buildRequest: func(args map[string]any, owner, repo string) (string, string, io.Reader, error) {
			num, ok := getNumber(args, "number")
			if !ok {
				return "", "", nil, fmt.Errorf("missing required parameter: number")
			}
			path := fmt.Sprintf("/repos/%s/%s/pulls/%d", owner, repo, num)
			bodyMap := map[string]any{}
			if v := getString(args, "title"); v != "" {
				bodyMap["title"] = v
			}
			if v := getString(args, "body"); v != "" {
				bodyMap["body"] = v
			}
			if v := getString(args, "state"); v != "" {
				bodyMap["state"] = v
			}
			if v := getString(args, "base"); v != "" {
				bodyMap["base"] = v
			}
			return http.MethodPatch, path, jsonBody(bodyMap), nil
		},
	}
}

func prMergeTool() *toolDef {
	return &toolDef{
		Definition: ToolDefinition{
			Name:        "github_pr_merge",
			Description: "Merge a pull request",
			InputSchema: jsonSchema{
				Type: "object",
				Properties: map[string]property{
					"number":       {Type: "number", Description: "Pull request number"},
					"merge_method": {Type: "string", Description: "Merge method", Enum: []string{"merge", "squash", "rebase"}},
					"commit_title": {Type: "string", Description: "Custom merge commit title"},
				},
				Required: []string{"number"},
			},
		},
		Method:   http.MethodPut,
		PathTmpl: "/repos/{owner}/{repo}/pulls/{number}/merge",
		IsWrite:  true,
		buildRequest: func(args map[string]any, owner, repo string) (string, string, io.Reader, error) {
			num, ok := getNumber(args, "number")
			if !ok {
				return "", "", nil, fmt.Errorf("missing required parameter: number")
			}
			path := fmt.Sprintf("/repos/%s/%s/pulls/%d/merge", owner, repo, num)
			bodyMap := map[string]any{}
			if v := getString(args, "merge_method"); v != "" {
				bodyMap["merge_method"] = v
			}
			if v := getString(args, "commit_title"); v != "" {
				bodyMap["commit_title"] = v
			}
			return http.MethodPut, path, jsonBody(bodyMap), nil
		},
	}
}

func prCommentTool() *toolDef {
	return &toolDef{
		Definition: ToolDefinition{
			Name:        "github_pr_comment",
			Description: "Comment on a PR",
			InputSchema: jsonSchema{
				Type: "object",
				Properties: map[string]property{
					"number": {Type: "number", Description: "Pull request number"},
					"body":   {Type: "string", Description: "Comment body"},
				},
				Required: []string{"number", "body"},
			},
		},
		Method:   http.MethodPost,
		PathTmpl: "/repos/{owner}/{repo}/issues/{number}/comments",
		IsWrite:  true,
		buildRequest: func(args map[string]any, owner, repo string) (string, string, io.Reader, error) {
			num, ok := getNumber(args, "number")
			if !ok {
				return "", "", nil, fmt.Errorf("missing required parameter: number")
			}
			body := getString(args, "body")
			if body == "" {
				return "", "", nil, fmt.Errorf("missing required parameter: body")
			}
			path := fmt.Sprintf("/repos/%s/%s/issues/%d/comments", owner, repo, num)
			return http.MethodPost, path, jsonBody(map[string]any{"body": body}), nil
		},
	}
}

func issueCreateTool() *toolDef {
	return &toolDef{
		Definition: ToolDefinition{
			Name:        "github_issue_create",
			Description: "Create an issue",
			InputSchema: jsonSchema{
				Type: "object",
				Properties: map[string]property{
					"title":  {Type: "string", Description: "Issue title"},
					"body":   {Type: "string", Description: "Issue body"},
					"labels": {Type: "string", Description: "Comma-separated list of label names"},
				},
				Required: []string{"title"},
			},
		},
		Method:   http.MethodPost,
		PathTmpl: "/repos/{owner}/{repo}/issues",
		IsWrite:  true,
		buildRequest: func(args map[string]any, owner, repo string) (string, string, io.Reader, error) {
			title := getString(args, "title")
			if title == "" {
				return "", "", nil, fmt.Errorf("missing required parameter: title")
			}
			path := fmt.Sprintf("/repos/%s/%s/issues", owner, repo)
			bodyMap := map[string]any{"title": title}
			if v := getString(args, "body"); v != "" {
				bodyMap["body"] = v
			}
			if v := getString(args, "labels"); v != "" {
				labels := strings.Split(v, ",")
				trimmed := make([]string, 0, len(labels))
				for _, l := range labels {
					l = strings.TrimSpace(l)
					if l != "" {
						trimmed = append(trimmed, l)
					}
				}
				bodyMap["labels"] = trimmed
			}
			return http.MethodPost, path, jsonBody(bodyMap), nil
		},
	}
}

func issueCommentTool() *toolDef {
	return &toolDef{
		Definition: ToolDefinition{
			Name:        "github_issue_comment",
			Description: "Comment on an issue",
			InputSchema: jsonSchema{
				Type: "object",
				Properties: map[string]property{
					"number": {Type: "number", Description: "Issue number"},
					"body":   {Type: "string", Description: "Comment body"},
				},
				Required: []string{"number", "body"},
			},
		},
		Method:   http.MethodPost,
		PathTmpl: "/repos/{owner}/{repo}/issues/{number}/comments",
		IsWrite:  true,
		buildRequest: func(args map[string]any, owner, repo string) (string, string, io.Reader, error) {
			num, ok := getNumber(args, "number")
			if !ok {
				return "", "", nil, fmt.Errorf("missing required parameter: number")
			}
			body := getString(args, "body")
			if body == "" {
				return "", "", nil, fmt.Errorf("missing required parameter: body")
			}
			path := fmt.Sprintf("/repos/%s/%s/issues/%d/comments", owner, repo, num)
			return http.MethodPost, path, jsonBody(map[string]any{"body": body}), nil
		},
	}
}

// --- Catch-all tool ---

func apiTool() *toolDef {
	return &toolDef{
		Definition: ToolDefinition{
			Name:        "github_api",
			Description: "Raw GitHub API call (catch-all for any endpoint)",
			InputSchema: jsonSchema{
				Type: "object",
				Properties: map[string]property{
					"method": {Type: "string", Description: "HTTP method (GET, POST, PUT, PATCH, DELETE)"},
					"path":   {Type: "string", Description: "GitHub API path (e.g. /repos/owner/repo/pulls)"},
					"body":   {Type: "string", Description: "Request body (JSON string)"},
				},
				Required: []string{"method", "path"},
			},
		},
		Method:   "",    // determined at runtime
		PathTmpl: "",    // determined at runtime
		IsWrite:  false, // determined at runtime based on method
		buildRequest: func(args map[string]any, owner, repo string) (string, string, io.Reader, error) {
			method := strings.ToUpper(getString(args, "method"))
			if method == "" {
				return "", "", nil, fmt.Errorf("missing required parameter: method")
			}
			path := getString(args, "path")
			if path == "" {
				return "", "", nil, fmt.Errorf("missing required parameter: path")
			}
			// Ensure path starts with /
			if !strings.HasPrefix(path, "/") {
				path = "/" + path
			}
			var body io.Reader
			if b := getString(args, "body"); b != "" {
				body = strings.NewReader(b)
			}
			return method, path, body, nil
		},
	}
}

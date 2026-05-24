package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newTestServer creates an MCP Server with its GitHub client pointed at the
// given test server URL.
func newTestServer(ghURL string) *Server {
	auth := NewGitHubAuthFromToken("test-token")
	client := NewGitHubClient(auth)
	client.baseURL = ghURL
	policy := &Policy{AllowedOwner: "my-owner", AllowedRepo: "my-repo"}
	return NewServer("my-owner", "my-repo", policy, client)
}

// mcpCall sends a JSON-RPC request to the MCP server and returns the response.
func mcpCall(t *testing.T, server *Server, method string, params any) *jsonRPCResponse {
	t.Helper()
	reqBody := map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  method,
	}
	if params != nil {
		reqBody["params"] = params
	}
	body, err := json.Marshal(reqBody)
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader(string(body)))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	server.ServeHTTP(w, req)

	if w.Code == http.StatusNoContent {
		return nil
	}

	var resp jsonRPCResponse
	err = json.Unmarshal(w.Body.Bytes(), &resp)
	require.NoError(t, err)
	return &resp
}

func TestInitialize(t *testing.T) {
	server := newTestServer("http://unused")
	resp := mcpCall(t, server, "initialize", nil)

	require.NotNil(t, resp)
	assert.Nil(t, resp.Error)
	assert.Equal(t, "2.0", resp.JSONRPC)

	result, ok := resp.Result.(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "2025-03-26", result["protocolVersion"])

	serverInfo, ok := result["serverInfo"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "github-mcp", serverInfo["name"])
	assert.Equal(t, "1.0.0", serverInfo["version"])

	capabilities, ok := result["capabilities"].(map[string]any)
	require.True(t, ok)
	_, hasTools := capabilities["tools"]
	assert.True(t, hasTools)
}

func TestToolsList(t *testing.T) {
	server := newTestServer("http://unused")
	resp := mcpCall(t, server, "tools/list", nil)

	require.NotNil(t, resp)
	assert.Nil(t, resp.Error)

	result, ok := resp.Result.(map[string]any)
	require.True(t, ok)

	toolsRaw, ok := result["tools"]
	require.True(t, ok)

	// Re-marshal and unmarshal to get proper types
	toolsJSON, err := json.Marshal(toolsRaw)
	require.NoError(t, err)

	var tools []ToolDefinition
	err = json.Unmarshal(toolsJSON, &tools)
	require.NoError(t, err)

	// Verify we have at least 15 tools
	assert.GreaterOrEqual(t, len(tools), 15)

	// Check expected tool names exist
	toolNames := make(map[string]bool)
	for _, tool := range tools {
		toolNames[tool.Name] = true
		assert.NotEmpty(t, tool.Description)
		assert.NotNil(t, tool.InputSchema)
	}

	expectedTools := []string{
		"github_pr_list", "github_pr_get", "github_pr_create",
		"github_pr_update", "github_pr_merge", "github_pr_comment",
		"github_pr_reviews", "github_issue_list", "github_issue_get",
		"github_issue_create", "github_issue_comment", "github_repo_get",
		"github_release_list", "github_checks_list", "github_api",
	}
	for _, name := range expectedTools {
		assert.True(t, toolNames[name], "missing tool: %s", name)
	}
}

func TestToolsCall_ReadAllowed(t *testing.T) {
	ghAPI := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "Bearer test-token", r.Header.Get("Authorization"))
		assert.Equal(t, "application/vnd.github+json", r.Header.Get("Accept"))
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`[{"number":1,"title":"Test PR"}]`))
	}))
	defer ghAPI.Close()

	server := newTestServer(ghAPI.URL)

	// Read from a different repo should be allowed
	resp := mcpCall(t, server, "tools/call", map[string]any{
		"name": "github_pr_list",
		"arguments": map[string]any{
			"owner": "other-owner",
			"repo":  "other-repo",
			"state": "open",
		},
	})

	require.NotNil(t, resp)
	assert.Nil(t, resp.Error)

	resultJSON, err := json.Marshal(resp.Result)
	require.NoError(t, err)

	var result mcpToolResult
	err = json.Unmarshal(resultJSON, &result)
	require.NoError(t, err)

	assert.False(t, result.IsError)
	assert.Len(t, result.Content, 1)
	assert.Equal(t, "text", result.Content[0].Type)
	assert.Contains(t, result.Content[0].Text, "Test PR")
}

func TestToolsCall_WriteAllowed(t *testing.T) {
	var capturedPath string
	var capturedMethod string
	var capturedBody string

	ghAPI := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedPath = r.URL.Path
		capturedMethod = r.Method
		body := make([]byte, r.ContentLength)
		r.Body.Read(body)
		capturedBody = string(body)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		w.Write([]byte(`{"number":42,"title":"New PR"}`))
	}))
	defer ghAPI.Close()

	server := newTestServer(ghAPI.URL)

	resp := mcpCall(t, server, "tools/call", map[string]any{
		"name": "github_pr_create",
		"arguments": map[string]any{
			"title": "New PR",
			"head":  "feature-branch",
			"base":  "main",
		},
	})

	require.NotNil(t, resp)
	assert.Nil(t, resp.Error)

	resultJSON, err := json.Marshal(resp.Result)
	require.NoError(t, err)

	var result mcpToolResult
	err = json.Unmarshal(resultJSON, &result)
	require.NoError(t, err)

	assert.False(t, result.IsError)
	assert.Contains(t, result.Content[0].Text, "New PR")
	assert.Equal(t, "/repos/my-owner/my-repo/pulls", capturedPath)
	assert.Equal(t, http.MethodPost, capturedMethod)
	assert.Contains(t, capturedBody, `"title":"New PR"`)
}

func TestToolsCall_WriteDenied(t *testing.T) {
	// No GitHub API server needed - request should be denied before making the call
	server := newTestServer("http://unused")

	// github_pr_create is a write tool and always uses the configured owner/repo,
	// so it will always target my-owner/my-repo. To test denial, we need a tool
	// that could target a different repo. Use github_api with POST method.
	resp := mcpCall(t, server, "tools/call", map[string]any{
		"name": "github_api",
		"arguments": map[string]any{
			"method": "POST",
			"path":   "/repos/other-owner/other-repo/pulls",
			"body":   `{"title":"hack"}`,
		},
	})

	require.NotNil(t, resp)
	assert.Nil(t, resp.Error) // JSON-RPC level OK, but tool result has error

	resultJSON, err := json.Marshal(resp.Result)
	require.NoError(t, err)

	var result mcpToolResult
	err = json.Unmarshal(resultJSON, &result)
	require.NoError(t, err)

	assert.True(t, result.IsError)
	assert.Contains(t, result.Content[0].Text, "write access denied")
}

func TestToolsCall_GitHubAPIError(t *testing.T) {
	ghAPI := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnprocessableEntity)
		w.Write([]byte(`{"message":"Validation Failed","errors":[{"resource":"PullRequest","code":"custom","message":"No commits between main and main"}]}`))
	}))
	defer ghAPI.Close()

	server := newTestServer(ghAPI.URL)

	resp := mcpCall(t, server, "tools/call", map[string]any{
		"name": "github_pr_create",
		"arguments": map[string]any{
			"title": "Bad PR",
			"head":  "main",
			"base":  "main",
		},
	})

	require.NotNil(t, resp)
	assert.Nil(t, resp.Error)

	resultJSON, err := json.Marshal(resp.Result)
	require.NoError(t, err)

	var result mcpToolResult
	err = json.Unmarshal(resultJSON, &result)
	require.NoError(t, err)

	assert.True(t, result.IsError)
	assert.Contains(t, result.Content[0].Text, "Validation Failed")
}

func TestToolsCall_UnknownTool(t *testing.T) {
	server := newTestServer("http://unused")

	resp := mcpCall(t, server, "tools/call", map[string]any{
		"name":      "nonexistent_tool",
		"arguments": map[string]any{},
	})

	require.NotNil(t, resp)
	assert.Nil(t, resp.Error) // JSON-RPC level OK

	resultJSON, err := json.Marshal(resp.Result)
	require.NoError(t, err)

	var result mcpToolResult
	err = json.Unmarshal(resultJSON, &result)
	require.NoError(t, err)

	assert.True(t, result.IsError)
	assert.Contains(t, result.Content[0].Text, "unknown tool")
}

func TestToolsCall_GitHubApi_ReadAllowed(t *testing.T) {
	ghAPI := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"full_name":"other-owner/other-repo"}`))
	}))
	defer ghAPI.Close()

	server := newTestServer(ghAPI.URL)

	resp := mcpCall(t, server, "tools/call", map[string]any{
		"name": "github_api",
		"arguments": map[string]any{
			"method": "GET",
			"path":   "/repos/other-owner/other-repo",
		},
	})

	require.NotNil(t, resp)
	assert.Nil(t, resp.Error)

	resultJSON, err := json.Marshal(resp.Result)
	require.NoError(t, err)

	var result mcpToolResult
	err = json.Unmarshal(resultJSON, &result)
	require.NoError(t, err)

	assert.False(t, result.IsError)
	assert.Contains(t, result.Content[0].Text, "other-owner/other-repo")
}

func TestToolsCall_GitHubApi_WriteDenied(t *testing.T) {
	server := newTestServer("http://unused")

	resp := mcpCall(t, server, "tools/call", map[string]any{
		"name": "github_api",
		"arguments": map[string]any{
			"method": "POST",
			"path":   "/repos/other-owner/other-repo/issues",
			"body":   `{"title":"test"}`,
		},
	})

	require.NotNil(t, resp)
	assert.Nil(t, resp.Error)

	resultJSON, err := json.Marshal(resp.Result)
	require.NoError(t, err)

	var result mcpToolResult
	err = json.Unmarshal(resultJSON, &result)
	require.NoError(t, err)

	assert.True(t, result.IsError)
	assert.Contains(t, result.Content[0].Text, "write access denied")
}

func TestInvalidMethod(t *testing.T) {
	server := newTestServer("http://unused")

	req := httptest.NewRequest(http.MethodGet, "/mcp", nil)
	w := httptest.NewRecorder()
	server.ServeHTTP(w, req)

	assert.Equal(t, http.StatusMethodNotAllowed, w.Code)

	var resp jsonRPCResponse
	err := json.Unmarshal(w.Body.Bytes(), &resp)
	require.NoError(t, err)
	assert.NotNil(t, resp.Error)
	assert.Equal(t, -32600, resp.Error.Code)
}

func TestInvalidJSON(t *testing.T) {
	server := newTestServer("http://unused")

	req := httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader("this is not json{{{"))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	server.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp jsonRPCResponse
	err := json.Unmarshal(w.Body.Bytes(), &resp)
	require.NoError(t, err)
	assert.NotNil(t, resp.Error)
	assert.Equal(t, -32700, resp.Error.Code)
	assert.Contains(t, resp.Error.Message, "parse error")
}

func TestNotificationsInitialized(t *testing.T) {
	server := newTestServer("http://unused")

	reqBody := `{"jsonrpc":"2.0","method":"notifications/initialized"}`
	req := httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader(reqBody))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	server.ServeHTTP(w, req)

	assert.Equal(t, http.StatusNoContent, w.Code)
}

func TestUnknownMethod(t *testing.T) {
	server := newTestServer("http://unused")

	resp := mcpCall(t, server, "unknown/method", nil)
	require.NotNil(t, resp)
	assert.NotNil(t, resp.Error)
	assert.Equal(t, -32601, resp.Error.Code)
	assert.Contains(t, resp.Error.Message, "method not found")
}

func TestToolsCall_MissingName(t *testing.T) {
	server := newTestServer("http://unused")
	resp := mcpCall(t, server, "tools/call", map[string]any{"arguments": map[string]any{}})
	require.NotNil(t, resp)
	assert.NotNil(t, resp.Error)
	assert.Equal(t, -32602, resp.Error.Code)
	assert.Contains(t, resp.Error.Message, "missing tool name")
}

func TestToolsCall_NilArguments(t *testing.T) {
	gh := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`[]`))
	}))
	defer gh.Close()

	server := newTestServer(gh.URL)
	resp := mcpCall(t, server, "tools/call", map[string]any{
		"name": "github_pr_list",
	})
	require.NotNil(t, resp)
	assert.Nil(t, resp.Error)
}

func TestToolsCall_BuildRequestError(t *testing.T) {
	server := newTestServer("http://unused")
	resp := mcpCall(t, server, "tools/call", map[string]any{
		"name":      "github_pr_get",
		"arguments": map[string]any{},
	})
	require.NotNil(t, resp)
	assert.Nil(t, resp.Error)
	result, ok := resp.Result.(map[string]any)
	require.True(t, ok)
	assert.Equal(t, true, result["isError"])
}

func TestServeHTTP_GETMethodNotAllowed(t *testing.T) {
	server := newTestServer("http://unused")
	req := httptest.NewRequest(http.MethodGet, "/mcp", nil)
	w := httptest.NewRecorder()
	server.ServeHTTP(w, req)
	assert.Equal(t, http.StatusMethodNotAllowed, w.Code)
}

func TestToolsCall_GitHubAPIReturnsError(t *testing.T) {
	gh := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		w.Write([]byte(`{"message":"Not Found"}`))
	}))
	defer gh.Close()

	server := newTestServer(gh.URL)
	resp := mcpCall(t, server, "tools/call", map[string]any{
		"name":      "github_repo_get",
		"arguments": map[string]any{},
	})
	require.NotNil(t, resp)
	assert.Nil(t, resp.Error)
	result, ok := resp.Result.(map[string]any)
	require.True(t, ok)
	assert.Equal(t, true, result["isError"])
}

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

// mcpCall sends a JSON-RPC request to the MCP server and returns the response
// (nil for a 204 No Content).
func mcpCall(t *testing.T, server *Server, method string, params any) *jsonRPCResponse {
	t.Helper()
	reqBody := map[string]any{"jsonrpc": "2.0", "id": 1, "method": method}
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
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	return &resp
}

func TestInitialize(t *testing.T) {
	server := NewServer(&Policy{}, newTestClient())
	resp := mcpCall(t, server, "initialize", nil)
	require.NotNil(t, resp)
	assert.Nil(t, resp.Error)

	result, ok := resp.Result.(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "2025-03-26", result["protocolVersion"])

	serverInfo, ok := result["serverInfo"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "k8s-mcp", serverInfo["name"])
	assert.Equal(t, "1.0.0", serverInfo["version"])
}

func TestToolsList(t *testing.T) {
	server := NewServer(&Policy{}, newTestClient())
	resp := mcpCall(t, server, "tools/list", nil)
	require.NotNil(t, resp)
	assert.Nil(t, resp.Error)

	result, ok := resp.Result.(map[string]any)
	require.True(t, ok)
	tools, ok := result["tools"].([]any)
	require.True(t, ok)
	assert.Len(t, tools, len(toolRegistry))

	for _, tool := range tools {
		m := tool.(map[string]any)
		assert.NotEmpty(t, m["name"])
		assert.NotNil(t, m["annotations"], "tool %v missing annotations", m["name"])
	}
}

func TestToolsCall_Success(t *testing.T) {
	server := NewServer(&Policy{}, newTestClient(unstructuredObj("v1", "Pod", "default", "p1")))
	resp := mcpCall(t, server, "tools/call", map[string]any{
		"name":      "list_resources",
		"arguments": map[string]any{"api_version": "v1", "kind": "Pod", "namespace": "default"},
	})
	require.NotNil(t, resp)
	assert.Nil(t, resp.Error)

	result, ok := resp.Result.(map[string]any)
	require.True(t, ok)
	assert.NotEqual(t, true, result["isError"])
	content := result["content"].([]any)
	require.Len(t, content, 1)
	assert.Contains(t, content[0].(map[string]any)["text"], "p1")
}

func TestToolsCall_ToolError(t *testing.T) {
	// A denied Secret get surfaces as an isError result, not a JSON-RPC error.
	server := NewServer(&Policy{}, newTestClient())
	resp := mcpCall(t, server, "tools/call", map[string]any{
		"name":      "get_resource",
		"arguments": map[string]any{"api_version": "v1", "kind": "Secret", "name": "s1"},
	})
	require.NotNil(t, resp)
	assert.Nil(t, resp.Error)
	result, ok := resp.Result.(map[string]any)
	require.True(t, ok)
	assert.Equal(t, true, result["isError"])
}

func TestToolsCall_UnknownTool(t *testing.T) {
	server := NewServer(&Policy{}, newTestClient())
	resp := mcpCall(t, server, "tools/call", map[string]any{
		"name":      "no_such_tool",
		"arguments": map[string]any{},
	})
	require.NotNil(t, resp)
	assert.Nil(t, resp.Error)
	result, ok := resp.Result.(map[string]any)
	require.True(t, ok)
	assert.Equal(t, true, result["isError"])
}

func TestToolsCall_MissingName(t *testing.T) {
	server := NewServer(&Policy{}, newTestClient())
	resp := mcpCall(t, server, "tools/call", map[string]any{"arguments": map[string]any{}})
	require.NotNil(t, resp)
	require.NotNil(t, resp.Error)
	assert.Equal(t, -32602, resp.Error.Code)
	assert.Contains(t, resp.Error.Message, "missing tool name")
}

func TestToolsCall_NilArgumentsDefaulted(t *testing.T) {
	c := newTestClient()
	c.disco = &fakeDiscovery{resources: sampleAPIResourceLists()}
	server := NewServer(&Policy{}, c)
	// No "arguments" key at all → defaulted to an empty map.
	resp := mcpCall(t, server, "tools/call", map[string]any{"name": "list_api_resources"})
	require.NotNil(t, resp)
	assert.Nil(t, resp.Error)
	result, ok := resp.Result.(map[string]any)
	require.True(t, ok)
	assert.NotEqual(t, true, result["isError"])
}

func TestMethodNotFound(t *testing.T) {
	server := NewServer(&Policy{}, newTestClient())
	resp := mcpCall(t, server, "does/not/exist", nil)
	require.NotNil(t, resp)
	require.NotNil(t, resp.Error)
	assert.Equal(t, -32601, resp.Error.Code)
}

func TestNotificationsInitialized(t *testing.T) {
	server := NewServer(&Policy{}, newTestClient())
	resp := mcpCall(t, server, "notifications/initialized", nil)
	assert.Nil(t, resp) // 204 No Content, no body
}

func TestServeHTTP_NonPost(t *testing.T) {
	server := NewServer(&Policy{}, newTestClient())
	req := httptest.NewRequest(http.MethodGet, "/mcp", nil)
	w := httptest.NewRecorder()
	server.ServeHTTP(w, req)
	assert.Equal(t, http.StatusMethodNotAllowed, w.Code)

	var resp jsonRPCResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	require.NotNil(t, resp.Error)
	assert.Equal(t, -32600, resp.Error.Code)
}

func TestServeHTTP_BadJSON(t *testing.T) {
	server := NewServer(&Policy{}, newTestClient())
	req := httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader("{not json"))
	w := httptest.NewRecorder()
	server.ServeHTTP(w, req)
	assert.Equal(t, http.StatusOK, w.Code)

	var resp jsonRPCResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	require.NotNil(t, resp.Error)
	assert.Equal(t, -32700, resp.Error.Code)
}

func TestServeHTTP_InvalidParams(t *testing.T) {
	server := NewServer(&Policy{}, newTestClient())
	// params is a JSON string where an object is expected → unmarshal error.
	req := httptest.NewRequest(http.MethodPost, "/mcp",
		strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":"oops"}`))
	w := httptest.NewRecorder()
	server.ServeHTTP(w, req)

	var resp jsonRPCResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	require.NotNil(t, resp.Error)
	assert.Equal(t, -32602, resp.Error.Code)
}

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

// newTestServer creates a Server with a nil Docker client (tools that need Docker
// will fail, but we can test the MCP protocol handling).
func newTestServer() *Server {
	tracker := NewTracker()
	return &Server{
		docker:      nil,
		tracker:     tracker,
		networkName: "test-network",
	}
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
	server := newTestServer()
	resp := mcpCall(t, server, "initialize", nil)

	require.NotNil(t, resp)
	assert.Nil(t, resp.Error)
	assert.Equal(t, "2.0", resp.JSONRPC)

	result, ok := resp.Result.(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "2025-03-26", result["protocolVersion"])

	serverInfo, ok := result["serverInfo"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "docker-mcp", serverInfo["name"])
	assert.Equal(t, "1.0.0", serverInfo["version"])

	capabilities, ok := result["capabilities"].(map[string]any)
	require.True(t, ok)
	_, hasTools := capabilities["tools"]
	assert.True(t, hasTools)
}

func TestToolsList(t *testing.T) {
	server := newTestServer()
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

	assert.Len(t, tools, 7)

	toolNames := make(map[string]bool)
	for _, tool := range tools {
		toolNames[tool.Name] = true
		assert.NotEmpty(t, tool.Description)
		assert.NotNil(t, tool.InputSchema)
	}

	expectedTools := []string{
		"docker_ps", "docker_run", "docker_exec",
		"docker_stop", "docker_rm", "docker_logs", "docker_pull",
	}
	for _, name := range expectedTools {
		assert.True(t, toolNames[name], "missing tool: %s", name)
	}
}

func TestToolsCall_UnknownTool(t *testing.T) {
	server := newTestServer()

	resp := mcpCall(t, server, "tools/call", map[string]any{
		"name":      "nonexistent_tool",
		"arguments": map[string]any{},
	})

	require.NotNil(t, resp)
	assert.Nil(t, resp.Error)

	resultJSON, err := json.Marshal(resp.Result)
	require.NoError(t, err)

	var result mcpToolResult
	err = json.Unmarshal(resultJSON, &result)
	require.NoError(t, err)

	assert.True(t, result.IsError)
	assert.Contains(t, result.Content[0].Text, "unknown tool")
}

func TestToolsCall_MissingName(t *testing.T) {
	server := newTestServer()
	resp := mcpCall(t, server, "tools/call", map[string]any{"arguments": map[string]any{}})
	require.NotNil(t, resp)
	assert.NotNil(t, resp.Error)
	assert.Equal(t, -32602, resp.Error.Code)
	assert.Contains(t, resp.Error.Message, "missing tool name")
}

func TestToolsCall_DockerPS_Empty(t *testing.T) {
	server := newTestServer()

	resp := mcpCall(t, server, "tools/call", map[string]any{
		"name":      "docker_ps",
		"arguments": map[string]any{},
	})

	require.NotNil(t, resp)
	assert.Nil(t, resp.Error)

	resultJSON, err := json.Marshal(resp.Result)
	require.NoError(t, err)

	var result mcpToolResult
	err = json.Unmarshal(resultJSON, &result)
	require.NoError(t, err)

	assert.False(t, result.IsError)
	assert.Contains(t, result.Content[0].Text, "No containers running")
}

func TestToolsCall_DockerExec_NotTracked(t *testing.T) {
	server := newTestServer()

	resp := mcpCall(t, server, "tools/call", map[string]any{
		"name": "docker_exec",
		"arguments": map[string]any{
			"container": "untracked-container",
			"cmd":       []string{"echo", "hello"},
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
	assert.Contains(t, result.Content[0].Text, "access denied")
	assert.Contains(t, result.Content[0].Text, "not tracked")
}

func TestToolsCall_DockerStop_NotTracked(t *testing.T) {
	server := newTestServer()

	resp := mcpCall(t, server, "tools/call", map[string]any{
		"name": "docker_stop",
		"arguments": map[string]any{
			"container": "untracked-container",
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
	assert.Contains(t, result.Content[0].Text, "access denied")
}

func TestToolsCall_DockerRM_NotTracked(t *testing.T) {
	server := newTestServer()

	resp := mcpCall(t, server, "tools/call", map[string]any{
		"name": "docker_rm",
		"arguments": map[string]any{
			"container": "untracked-container",
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
	assert.Contains(t, result.Content[0].Text, "access denied")
}

func TestToolsCall_DockerLogs_NotTracked(t *testing.T) {
	server := newTestServer()

	resp := mcpCall(t, server, "tools/call", map[string]any{
		"name": "docker_logs",
		"arguments": map[string]any{
			"container": "untracked-container",
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
	assert.Contains(t, result.Content[0].Text, "access denied")
}

func TestToolsCall_DockerRun_MissingImage(t *testing.T) {
	server := newTestServer()

	resp := mcpCall(t, server, "tools/call", map[string]any{
		"name":      "docker_run",
		"arguments": map[string]any{},
	})

	require.NotNil(t, resp)
	assert.Nil(t, resp.Error)

	resultJSON, err := json.Marshal(resp.Result)
	require.NoError(t, err)

	var result mcpToolResult
	err = json.Unmarshal(resultJSON, &result)
	require.NoError(t, err)

	assert.True(t, result.IsError)
	assert.Contains(t, result.Content[0].Text, "missing required parameter: image")
}

func TestToolsCall_DockerPull_MissingImage(t *testing.T) {
	server := newTestServer()

	resp := mcpCall(t, server, "tools/call", map[string]any{
		"name":      "docker_pull",
		"arguments": map[string]any{},
	})

	require.NotNil(t, resp)
	assert.Nil(t, resp.Error)

	resultJSON, err := json.Marshal(resp.Result)
	require.NoError(t, err)

	var result mcpToolResult
	err = json.Unmarshal(resultJSON, &result)
	require.NoError(t, err)

	assert.True(t, result.IsError)
	assert.Contains(t, result.Content[0].Text, "missing required parameter: image")
}

func TestToolsCall_DockerExec_MissingContainer(t *testing.T) {
	server := newTestServer()

	resp := mcpCall(t, server, "tools/call", map[string]any{
		"name": "docker_exec",
		"arguments": map[string]any{
			"cmd": []string{"echo", "hi"},
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
	assert.Contains(t, result.Content[0].Text, "missing required parameter: container")
}

func TestToolsCall_DockerExec_MissingCmd(t *testing.T) {
	server := newTestServer()
	server.tracker.Add("tracked-id", "tracked-name")

	resp := mcpCall(t, server, "tools/call", map[string]any{
		"name": "docker_exec",
		"arguments": map[string]any{
			"container": "tracked-name",
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
	assert.Contains(t, result.Content[0].Text, "missing required parameter: cmd")
}

func TestInvalidMethod(t *testing.T) {
	server := newTestServer()

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
	server := newTestServer()

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
	server := newTestServer()

	reqBody := `{"jsonrpc":"2.0","method":"notifications/initialized"}`
	req := httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader(reqBody))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	server.ServeHTTP(w, req)

	assert.Equal(t, http.StatusNoContent, w.Code)
}

func TestUnknownMethod(t *testing.T) {
	server := newTestServer()

	resp := mcpCall(t, server, "unknown/method", nil)
	require.NotNil(t, resp)
	assert.NotNil(t, resp.Error)
	assert.Equal(t, -32601, resp.Error.Code)
	assert.Contains(t, resp.Error.Message, "method not found")
}

func TestToolsCall_DockerStop_MissingContainer(t *testing.T) {
	server := newTestServer()

	resp := mcpCall(t, server, "tools/call", map[string]any{
		"name":      "docker_stop",
		"arguments": map[string]any{},
	})

	require.NotNil(t, resp)
	assert.Nil(t, resp.Error)

	resultJSON, err := json.Marshal(resp.Result)
	require.NoError(t, err)

	var result mcpToolResult
	err = json.Unmarshal(resultJSON, &result)
	require.NoError(t, err)

	assert.True(t, result.IsError)
	assert.Contains(t, result.Content[0].Text, "missing required parameter: container")
}

func TestToolsCall_DockerRM_MissingContainer(t *testing.T) {
	server := newTestServer()

	resp := mcpCall(t, server, "tools/call", map[string]any{
		"name":      "docker_rm",
		"arguments": map[string]any{},
	})

	require.NotNil(t, resp)
	assert.Nil(t, resp.Error)

	resultJSON, err := json.Marshal(resp.Result)
	require.NoError(t, err)

	var result mcpToolResult
	err = json.Unmarshal(resultJSON, &result)
	require.NoError(t, err)

	assert.True(t, result.IsError)
	assert.Contains(t, result.Content[0].Text, "missing required parameter: container")
}

func TestToolsCall_DockerLogs_MissingContainer(t *testing.T) {
	server := newTestServer()

	resp := mcpCall(t, server, "tools/call", map[string]any{
		"name":      "docker_logs",
		"arguments": map[string]any{},
	})

	require.NotNil(t, resp)
	assert.Nil(t, resp.Error)

	resultJSON, err := json.Marshal(resp.Result)
	require.NoError(t, err)

	var result mcpToolResult
	err = json.Unmarshal(resultJSON, &result)
	require.NoError(t, err)

	assert.True(t, result.IsError)
	assert.Contains(t, result.Content[0].Text, "missing required parameter: container")
}

func TestHelperFunctions(t *testing.T) {
	t.Run("getString", func(t *testing.T) {
		args := map[string]any{"key": "value", "num": 42}
		assert.Equal(t, "value", getString(args, "key"))
		assert.Equal(t, "", getString(args, "num"))
		assert.Equal(t, "", getString(args, "missing"))
	})

	t.Run("getStringArray", func(t *testing.T) {
		args := map[string]any{
			"arr":     []any{"a", "b", "c"},
			"str_arr": []string{"x", "y"},
			"empty":   []any{},
			"not_arr": "hello",
		}
		assert.Equal(t, []string{"a", "b", "c"}, getStringArray(args, "arr"))
		assert.Equal(t, []string{"x", "y"}, getStringArray(args, "str_arr"))
		assert.Empty(t, getStringArray(args, "empty"))
		assert.Nil(t, getStringArray(args, "not_arr"))
		assert.Nil(t, getStringArray(args, "missing"))
	})

	t.Run("getBool", func(t *testing.T) {
		args := map[string]any{"yes": true, "no": false, "str": "true"}
		assert.True(t, getBool(args, "yes", false))
		assert.False(t, getBool(args, "no", true))
		assert.True(t, getBool(args, "str", true))   // non-bool returns default
		assert.True(t, getBool(args, "missing", true)) // missing returns default
		assert.False(t, getBool(args, "missing", false))
	})
}

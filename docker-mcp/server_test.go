package main

import (
	"context"
	"net/http/httptest"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newTestServer creates a Server with a nil Docker client (tools that need Docker
// will fail, but we can test the MCP protocol handling).
func newTestServer() *Server {
	tracker := NewTracker()
	return NewServer(nil, tracker, "test-network")
}

// newTestClient starts an httptest server with the given MCP server and returns
// a connected MCP client session.
func newTestClient(t *testing.T, server *Server) *mcp.ClientSession {
	t.Helper()
	httpServer := httptest.NewServer(server)
	t.Cleanup(httpServer.Close)

	client := mcp.NewClient(&mcp.Implementation{
		Name:    "test-client",
		Version: "1.0.0",
	}, nil)

	session, err := client.Connect(context.Background(), &mcp.StreamableClientTransport{
		Endpoint: httpServer.URL,
	}, nil)
	require.NoError(t, err)
	t.Cleanup(func() { session.Close() })

	return session
}

func callTool(t *testing.T, session *mcp.ClientSession, name string, args map[string]any) *mcp.CallToolResult {
	t.Helper()
	result, err := session.CallTool(context.Background(), &mcp.CallToolParams{
		Name:      name,
		Arguments: args,
	})
	require.NoError(t, err)
	return result
}

func getTextContent(t *testing.T, result *mcp.CallToolResult) string {
	t.Helper()
	require.NotEmpty(t, result.Content)
	text, ok := result.Content[0].(*mcp.TextContent)
	require.True(t, ok, "expected TextContent, got %T", result.Content[0])
	return text.Text
}

func TestInitialize(t *testing.T) {
	server := newTestServer()
	session := newTestClient(t, server)

	initResult := session.InitializeResult()
	require.NotNil(t, initResult)
	assert.Equal(t, "docker-mcp", initResult.ServerInfo.Name)
	assert.Equal(t, "1.0.0", initResult.ServerInfo.Version)
}

func TestToolsList(t *testing.T) {
	server := newTestServer()
	session := newTestClient(t, server)

	result, err := session.ListTools(context.Background(), &mcp.ListToolsParams{})
	require.NoError(t, err)

	assert.Len(t, result.Tools, 7)

	toolNames := make(map[string]bool)
	for _, tool := range result.Tools {
		toolNames[tool.Name] = true
		assert.NotEmpty(t, tool.Description)
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
	session := newTestClient(t, server)

	_, err := session.CallTool(context.Background(), &mcp.CallToolParams{
		Name: "nonexistent_tool",
	})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "nonexistent_tool")
}

func TestToolsCall_DockerPS_Empty(t *testing.T) {
	server := newTestServer()
	session := newTestClient(t, server)

	result := callTool(t, session, "docker_ps", nil)
	assert.False(t, result.IsError)
	assert.Contains(t, getTextContent(t, result), "No containers running")
}

func TestToolsCall_DockerExec_NotTracked(t *testing.T) {
	server := newTestServer()
	session := newTestClient(t, server)

	result := callTool(t, session, "docker_exec", map[string]any{
		"container": "untracked-container",
		"cmd":       []string{"echo", "hello"},
	})

	assert.True(t, result.IsError)
	text := getTextContent(t, result)
	assert.Contains(t, text, "access denied")
	assert.Contains(t, text, "not tracked")
}

func TestToolsCall_DockerStop_NotTracked(t *testing.T) {
	server := newTestServer()
	session := newTestClient(t, server)

	result := callTool(t, session, "docker_stop", map[string]any{
		"container": "untracked-container",
	})

	assert.True(t, result.IsError)
	assert.Contains(t, getTextContent(t, result), "access denied")
}

func TestToolsCall_DockerRM_NotTracked(t *testing.T) {
	server := newTestServer()
	session := newTestClient(t, server)

	result := callTool(t, session, "docker_rm", map[string]any{
		"container": "untracked-container",
	})

	assert.True(t, result.IsError)
	assert.Contains(t, getTextContent(t, result), "access denied")
}

func TestToolsCall_DockerLogs_NotTracked(t *testing.T) {
	server := newTestServer()
	session := newTestClient(t, server)

	result := callTool(t, session, "docker_logs", map[string]any{
		"container": "untracked-container",
	})

	assert.True(t, result.IsError)
	assert.Contains(t, getTextContent(t, result), "access denied")
}

func TestToolsCall_DockerRun_MissingImage(t *testing.T) {
	server := newTestServer()
	session := newTestClient(t, server)

	result := callTool(t, session, "docker_run", map[string]any{})

	assert.True(t, result.IsError)
	assert.Contains(t, getTextContent(t, result), "image")
}

func TestToolsCall_DockerPull_MissingImage(t *testing.T) {
	server := newTestServer()
	session := newTestClient(t, server)

	result := callTool(t, session, "docker_pull", map[string]any{})

	assert.True(t, result.IsError)
	assert.Contains(t, getTextContent(t, result), "image")
}

func TestToolsCall_DockerExec_MissingContainer(t *testing.T) {
	server := newTestServer()
	session := newTestClient(t, server)

	result := callTool(t, session, "docker_exec", map[string]any{
		"cmd": []string{"echo", "hi"},
	})

	assert.True(t, result.IsError)
	assert.Contains(t, getTextContent(t, result), "container")
}

func TestToolsCall_DockerExec_MissingCmd(t *testing.T) {
	server := newTestServer()
	server.tracker.Add("tracked-id", "tracked-name")
	session := newTestClient(t, server)

	result := callTool(t, session, "docker_exec", map[string]any{
		"container": "tracked-name",
	})

	assert.True(t, result.IsError)
	assert.Contains(t, getTextContent(t, result), "cmd")
}

func TestToolsCall_DockerStop_MissingContainer(t *testing.T) {
	server := newTestServer()
	session := newTestClient(t, server)

	result := callTool(t, session, "docker_stop", map[string]any{})

	assert.True(t, result.IsError)
	assert.Contains(t, getTextContent(t, result), "container")
}

func TestToolsCall_DockerRM_MissingContainer(t *testing.T) {
	server := newTestServer()
	session := newTestClient(t, server)

	result := callTool(t, session, "docker_rm", map[string]any{})

	assert.True(t, result.IsError)
	assert.Contains(t, getTextContent(t, result), "container")
}

func TestToolsCall_DockerLogs_MissingContainer(t *testing.T) {
	server := newTestServer()
	session := newTestClient(t, server)

	result := callTool(t, session, "docker_logs", map[string]any{})

	assert.True(t, result.IsError)
	assert.Contains(t, getTextContent(t, result), "container")
}

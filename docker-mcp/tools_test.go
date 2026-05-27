package main

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/image"
	"github.com/docker/docker/api/types/network"
	"github.com/docker/go-connections/nat"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
)

func newHijackedResponse(data string) types.HijackedResponse {
	server, client := net.Pipe()
	server.Close()
	return types.HijackedResponse{
		Reader: bufio.NewReader(strings.NewReader(data)),
		Conn:   client,
	}
}

func newMockServer(t *testing.T) (*Server, *MockDockerClient) {
	t.Helper()
	ctrl := gomock.NewController(t)
	mock := NewMockDockerClient(ctrl)
	tracker := NewTracker()
	server := NewServer(mock, tracker, "test-network")
	return server, mock
}

func newMockClient(t *testing.T, server *Server) *mcp.ClientSession {
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

func mockCallTool(t *testing.T, session *mcp.ClientSession, name string, args map[string]any) *mcp.CallToolResult {
	t.Helper()
	result, err := session.CallTool(context.Background(), &mcp.CallToolParams{
		Name:      name,
		Arguments: args,
	})
	require.NoError(t, err)
	return result
}

func mockGetText(t *testing.T, result *mcp.CallToolResult) string {
	t.Helper()
	require.NotEmpty(t, result.Content)
	text, ok := result.Content[0].(*mcp.TextContent)
	require.True(t, ok, "expected TextContent, got %T", result.Content[0])
	return text.Text
}

func TestDockerPS_WithContainers(t *testing.T) {
	server, mock := newMockServer(t)
	server.tracker.Add("abc123def456abc123def456abc123def456abc123def456abc123def456abcd", "my-app")

	mock.EXPECT().ContainerInspect(gomock.Any(), "abc123def456abc123def456abc123def456abc123def456abc123def456abcd").Return(container.InspectResponse{
		ContainerJSONBase: &container.ContainerJSONBase{
			State: &container.State{Status: "running"},
		},
		Config: &container.Config{Image: "alpine:3.21"},
	}, nil)

	session := newMockClient(t, server)
	result := mockCallTool(t, session, "docker_ps", nil)

	assert.False(t, result.IsError)
	text := mockGetText(t, result)
	assert.Contains(t, text, "my-app")
	assert.Contains(t, text, "alpine:3.21")
	assert.Contains(t, text, "running")
}

func TestDockerPS_InspectFails(t *testing.T) {
	server, mock := newMockServer(t)
	server.tracker.Add("abc123def456abc123def456abc123def456abc123def456abc123def456abcd", "dead-app")

	mock.EXPECT().ContainerInspect(gomock.Any(), gomock.Any()).Return(container.InspectResponse{}, fmt.Errorf("not found"))

	session := newMockClient(t, server)
	result := mockCallTool(t, session, "docker_ps", nil)

	assert.False(t, result.IsError)
	text := mockGetText(t, result)
	assert.Contains(t, text, "dead-app")
	assert.Contains(t, text, "unknown")
}

func TestDockerRun_Detached(t *testing.T) {
	server, mock := newMockServer(t)

	mock.EXPECT().ContainerCreate(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), "my-web").
		Return(container.CreateResponse{ID: "abc123def456abc123def456abc123def456abc123def456abc123def456abcd"}, nil)
	mock.EXPECT().NetworkConnect(gomock.Any(), "test-network", "abc123def456abc123def456abc123def456abc123def456abc123def456abcd", gomock.Any()).Return(nil)
	mock.EXPECT().ContainerStart(gomock.Any(), "abc123def456abc123def456abc123def456abc123def456abc123def456abcd", gomock.Any()).Return(nil)

	session := newMockClient(t, server)
	result := mockCallTool(t, session, "docker_run", map[string]any{
		"image": "nginx:latest",
		"name":  "my-web",
	})

	assert.False(t, result.IsError)
	text := mockGetText(t, result)
	assert.Contains(t, text, "my-web")
	assert.Contains(t, text, "abc123def456")
	assert.True(t, server.tracker.IsTracked("my-web"))
}

func TestDockerRun_CreateFails(t *testing.T) {
	server, mock := newMockServer(t)

	mock.EXPECT().ContainerCreate(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
		Return(container.CreateResponse{}, fmt.Errorf("image not found"))

	session := newMockClient(t, server)
	result := mockCallTool(t, session, "docker_run", map[string]any{
		"image": "nonexistent:latest",
	})

	assert.True(t, result.IsError)
	assert.Contains(t, mockGetText(t, result), "failed to create container")
}

func TestDockerRun_NetworkConnectFails(t *testing.T) {
	server, mock := newMockServer(t)

	mock.EXPECT().ContainerCreate(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
		Return(container.CreateResponse{ID: "abc123def456abc123def456abc123def456abc123def456abc123def456abcd"}, nil)
	mock.EXPECT().NetworkConnect(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(fmt.Errorf("network error"))
	mock.EXPECT().ContainerRemove(gomock.Any(), "abc123def456abc123def456abc123def456abc123def456abc123def456abcd", gomock.Any()).Return(nil)

	session := newMockClient(t, server)
	result := mockCallTool(t, session, "docker_run", map[string]any{
		"image": "nginx:latest",
		"name":  "fail-net",
	})

	assert.True(t, result.IsError)
	assert.Contains(t, mockGetText(t, result), "failed to connect container to session network")
}

func TestDockerRun_StartFails(t *testing.T) {
	server, mock := newMockServer(t)

	mock.EXPECT().ContainerCreate(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
		Return(container.CreateResponse{ID: "abc123def456abc123def456abc123def456abc123def456abc123def456abcd"}, nil)
	mock.EXPECT().NetworkConnect(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(nil)
	mock.EXPECT().ContainerStart(gomock.Any(), gomock.Any(), gomock.Any()).Return(fmt.Errorf("start error"))
	mock.EXPECT().ContainerRemove(gomock.Any(), "abc123def456abc123def456abc123def456abc123def456abc123def456abcd", gomock.Any()).Return(nil)

	session := newMockClient(t, server)
	result := mockCallTool(t, session, "docker_run", map[string]any{
		"image": "nginx:latest",
	})

	assert.True(t, result.IsError)
	assert.Contains(t, mockGetText(t, result), "failed to start container")
}

func TestDockerRun_NonDetached(t *testing.T) {
	server, mock := newMockServer(t)

	mock.EXPECT().ContainerCreate(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
		Return(container.CreateResponse{ID: "abc123def456abc123def456abc123def456abc123def456abc123def456abcd"}, nil)
	mock.EXPECT().NetworkConnect(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(nil)
	mock.EXPECT().ContainerStart(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil)

	waitCh := make(chan container.WaitResponse, 1)
	waitCh <- container.WaitResponse{StatusCode: 0}
	errCh := make(chan error, 1)
	mock.EXPECT().ContainerWait(gomock.Any(), gomock.Any(), gomock.Any()).Return((<-chan container.WaitResponse)(waitCh), (<-chan error)(errCh))
	mock.EXPECT().ContainerLogs(gomock.Any(), gomock.Any(), gomock.Any()).Return(io.NopCloser(strings.NewReader("hello output")), nil)

	session := newMockClient(t, server)
	result := mockCallTool(t, session, "docker_run", map[string]any{
		"image":  "alpine:latest",
		"cmd":    []string{"echo", "hello"},
		"detach": false,
	})

	assert.False(t, result.IsError)
	assert.Contains(t, mockGetText(t, result), "hello output")
}

func TestDockerExec_Success(t *testing.T) {
	server, mock := newMockServer(t)
	server.tracker.Add("full-container-id-1234567890ab", "my-container")

	mock.EXPECT().ContainerExecCreate(gomock.Any(), "full-container-id-1234567890ab", gomock.Any()).
		Return(container.ExecCreateResponse{ID: "exec-123"}, nil)
	mock.EXPECT().ContainerExecAttach(gomock.Any(), "exec-123", gomock.Any()).
		Return(newHijackedResponse("exec output"), nil)
	mock.EXPECT().ContainerExecInspect(gomock.Any(), "exec-123").
		Return(container.ExecInspect{ExitCode: 0}, nil)

	session := newMockClient(t, server)
	result := mockCallTool(t, session, "docker_exec", map[string]any{
		"container": "my-container",
		"cmd":       []string{"echo", "hello"},
	})

	assert.False(t, result.IsError)
	assert.Contains(t, mockGetText(t, result), "exec output")
}

func TestDockerExec_NonZeroExit(t *testing.T) {
	server, mock := newMockServer(t)
	server.tracker.Add("full-container-id-1234567890ab", "my-container")

	mock.EXPECT().ContainerExecCreate(gomock.Any(), gomock.Any(), gomock.Any()).
		Return(container.ExecCreateResponse{ID: "exec-456"}, nil)
	mock.EXPECT().ContainerExecAttach(gomock.Any(), "exec-456", gomock.Any()).
		Return(newHijackedResponse("command failed"), nil)
	mock.EXPECT().ContainerExecInspect(gomock.Any(), "exec-456").
		Return(container.ExecInspect{ExitCode: 1}, nil)

	session := newMockClient(t, server)
	result := mockCallTool(t, session, "docker_exec", map[string]any{
		"container": "my-container",
		"cmd":       []string{"false"},
	})

	assert.True(t, result.IsError)
	text := mockGetText(t, result)
	assert.Contains(t, text, "Exit code: 1")
	assert.Contains(t, text, "command failed")
}

func TestDockerStop_Success(t *testing.T) {
	server, mock := newMockServer(t)
	server.tracker.Add("container-id-12345", "web-server")

	mock.EXPECT().ContainerStop(gomock.Any(), "container-id-12345", gomock.Any()).Return(nil)

	session := newMockClient(t, server)
	result := mockCallTool(t, session, "docker_stop", map[string]any{
		"container": "web-server",
	})

	assert.False(t, result.IsError)
	assert.Contains(t, mockGetText(t, result), "stopped")
}

func TestDockerStop_Fails(t *testing.T) {
	server, mock := newMockServer(t)
	server.tracker.Add("container-id-12345", "web-server")

	mock.EXPECT().ContainerStop(gomock.Any(), gomock.Any(), gomock.Any()).Return(fmt.Errorf("stop error"))

	session := newMockClient(t, server)
	result := mockCallTool(t, session, "docker_stop", map[string]any{
		"container": "web-server",
	})

	assert.True(t, result.IsError)
	assert.Contains(t, mockGetText(t, result), "failed to stop")
}

func TestDockerRM_Success(t *testing.T) {
	server, mock := newMockServer(t)
	server.tracker.Add("container-id-12345", "old-container")

	mock.EXPECT().ContainerRemove(gomock.Any(), "container-id-12345", gomock.Any()).Return(nil)

	session := newMockClient(t, server)
	result := mockCallTool(t, session, "docker_rm", map[string]any{
		"container": "old-container",
	})

	assert.False(t, result.IsError)
	assert.Contains(t, mockGetText(t, result), "removed")
	assert.False(t, server.tracker.IsTracked("old-container"))
}

func TestDockerRM_Fails(t *testing.T) {
	server, mock := newMockServer(t)
	server.tracker.Add("container-id-12345", "stuck-container")

	mock.EXPECT().ContainerRemove(gomock.Any(), gomock.Any(), gomock.Any()).Return(fmt.Errorf("rm error"))

	session := newMockClient(t, server)
	result := mockCallTool(t, session, "docker_rm", map[string]any{
		"container": "stuck-container",
	})

	assert.True(t, result.IsError)
	assert.Contains(t, mockGetText(t, result), "failed to remove")
}

func TestDockerLogs_Success(t *testing.T) {
	server, mock := newMockServer(t)
	server.tracker.Add("container-id-12345", "app")

	mock.EXPECT().ContainerLogs(gomock.Any(), "container-id-12345", gomock.Any()).
		Return(io.NopCloser(strings.NewReader("log line 1\nlog line 2\n")), nil)

	session := newMockClient(t, server)
	result := mockCallTool(t, session, "docker_logs", map[string]any{
		"container": "app",
	})

	assert.False(t, result.IsError)
	text := mockGetText(t, result)
	assert.Contains(t, text, "log line 1")
	assert.Contains(t, text, "log line 2")
}

func TestDockerLogs_Fails(t *testing.T) {
	server, mock := newMockServer(t)
	server.tracker.Add("container-id-12345", "app")

	mock.EXPECT().ContainerLogs(gomock.Any(), gomock.Any(), gomock.Any()).
		Return(nil, fmt.Errorf("logs error"))

	session := newMockClient(t, server)
	result := mockCallTool(t, session, "docker_logs", map[string]any{
		"container": "app",
	})

	assert.True(t, result.IsError)
	assert.Contains(t, mockGetText(t, result), "failed to get logs")
}

func TestDockerPull_Success(t *testing.T) {
	server, mock := newMockServer(t)

	mock.EXPECT().ImagePull(gomock.Any(), "nginx:latest", image.PullOptions{}).
		Return(io.NopCloser(strings.NewReader(`{"status":"done"}`)), nil)

	session := newMockClient(t, server)
	result := mockCallTool(t, session, "docker_pull", map[string]any{
		"image": "nginx:latest",
	})

	assert.False(t, result.IsError)
	assert.Contains(t, mockGetText(t, result), "Successfully pulled")
}

func TestDockerPull_Fails(t *testing.T) {
	server, mock := newMockServer(t)

	mock.EXPECT().ImagePull(gomock.Any(), "bad:image", gomock.Any()).
		Return(nil, fmt.Errorf("pull error"))

	session := newMockClient(t, server)
	result := mockCallTool(t, session, "docker_pull", map[string]any{
		"image": "bad:image",
	})

	assert.True(t, result.IsError)
	assert.Contains(t, mockGetText(t, result), "failed to pull image")
}

func TestDockerRun_WithPorts(t *testing.T) {
	server, mock := newMockServer(t)

	mock.EXPECT().ContainerCreate(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
		DoAndReturn(func(_ context.Context, cfg *container.Config, hostCfg *container.HostConfig, _ *network.NetworkingConfig, _ interface{}, _ string) (container.CreateResponse, error) {
			assert.Contains(t, cfg.ExposedPorts, nat.Port("80/tcp"))
			assert.Contains(t, hostCfg.PortBindings, nat.Port("80/tcp"))
			return container.CreateResponse{ID: "abc123def456abc123def456abc123def456abc123def456abc123def456abcd"}, nil
		})
	mock.EXPECT().NetworkConnect(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(nil)
	mock.EXPECT().ContainerStart(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil)

	session := newMockClient(t, server)
	result := mockCallTool(t, session, "docker_run", map[string]any{
		"image": "nginx:latest",
		"ports": []string{"8080:80"},
	})

	assert.False(t, result.IsError)
}

func TestDockerRun_InvalidPort(t *testing.T) {
	server, _ := newMockServer(t)

	session := newMockClient(t, server)
	result := mockCallTool(t, session, "docker_run", map[string]any{
		"image": "nginx:latest",
		"ports": []string{"invalid-port"},
	})

	assert.True(t, result.IsError)
	assert.Contains(t, mockGetText(t, result), "invalid port mapping")
}

func TestDockerRun_NoNetwork(t *testing.T) {
	ctrl := gomock.NewController(t)
	mock := NewMockDockerClient(ctrl)
	tracker := NewTracker()
	server := NewServer(mock, tracker, "")

	mock.EXPECT().ContainerCreate(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
		Return(container.CreateResponse{ID: "abc123def456abc123def456abc123def456abc123def456abc123def456abcd"}, nil)
	mock.EXPECT().ContainerStart(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil)

	httpServer := httptest.NewServer(server)
	t.Cleanup(httpServer.Close)
	client := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "1.0"}, nil)
	session, err := client.Connect(context.Background(), &mcp.StreamableClientTransport{Endpoint: httpServer.URL}, nil)
	require.NoError(t, err)
	t.Cleanup(func() { session.Close() })

	result, err := session.CallTool(context.Background(), &mcp.CallToolParams{
		Name:      "docker_run",
		Arguments: map[string]any{"image": "alpine:latest"},
	})
	require.NoError(t, err)

	assert.False(t, result.IsError)
	assert.Contains(t, mockGetText(t, result), "Container started")
}

func TestDockerExec_CreateFails(t *testing.T) {
	server, mock := newMockServer(t)
	server.tracker.Add("container-id-12345", "my-container")

	mock.EXPECT().ContainerExecCreate(gomock.Any(), gomock.Any(), gomock.Any()).
		Return(container.ExecCreateResponse{}, fmt.Errorf("exec create error"))

	session := newMockClient(t, server)
	result := mockCallTool(t, session, "docker_exec", map[string]any{
		"container": "my-container",
		"cmd":       []string{"echo", "hi"},
	})

	assert.True(t, result.IsError)
	assert.Contains(t, mockGetText(t, result), "failed to create exec")
}

func TestDockerExec_AttachFails(t *testing.T) {
	server, mock := newMockServer(t)
	server.tracker.Add("container-id-12345", "my-container")

	mock.EXPECT().ContainerExecCreate(gomock.Any(), gomock.Any(), gomock.Any()).
		Return(container.ExecCreateResponse{ID: "exec-789"}, nil)
	mock.EXPECT().ContainerExecAttach(gomock.Any(), "exec-789", gomock.Any()).
		Return(types.HijackedResponse{}, fmt.Errorf("attach error"))

	session := newMockClient(t, server)
	result := mockCallTool(t, session, "docker_exec", map[string]any{
		"container": "my-container",
		"cmd":       []string{"echo", "hi"},
	})

	assert.True(t, result.IsError)
	assert.Contains(t, mockGetText(t, result), "failed to attach to exec")
}

func TestDockerLogs_CustomTail(t *testing.T) {
	server, mock := newMockServer(t)
	server.tracker.Add("container-id-12345", "app")

	mock.EXPECT().ContainerLogs(gomock.Any(), "container-id-12345", gomock.Any()).
		DoAndReturn(func(_ context.Context, _ string, opts container.LogsOptions) (io.ReadCloser, error) {
			assert.Equal(t, "50", opts.Tail)
			return io.NopCloser(strings.NewReader("custom logs")), nil
		})

	session := newMockClient(t, server)
	result := mockCallTool(t, session, "docker_logs", map[string]any{
		"container": "app",
		"tail":      "50",
	})

	assert.False(t, result.IsError)
	assert.Contains(t, mockGetText(t, result), "custom logs")
}

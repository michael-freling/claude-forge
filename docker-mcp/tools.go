package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/image"
	"github.com/docker/docker/api/types/network"
	"github.com/docker/go-connections/nat"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// Tool input types — the SDK auto-generates JSON Schema from these structs.

type dockerPSInput struct{}

type dockerRunInput struct {
	Image  string   `json:"image" jsonschema:"Docker image to run"`
	Name   string   `json:"name,omitempty" jsonschema:"Container name"`
	Cmd    []string `json:"cmd,omitempty" jsonschema:"Command to run"`
	Env    []string `json:"env,omitempty" jsonschema:"Environment variables in KEY=VALUE format"`
	Ports  []string `json:"ports,omitempty" jsonschema:"Port mappings in host:container format e.g. 8080:80"`
	Detach *bool    `json:"detach,omitempty" jsonschema:"Run in background (default: true)"`
	Rm     bool     `json:"rm,omitempty" jsonschema:"Remove container when it exits"`
}

type dockerExecInput struct {
	Container string   `json:"container" jsonschema:"Container name or ID (must be tracked by this session)"`
	Cmd       []string `json:"cmd" jsonschema:"Command to execute"`
}

type dockerContainerInput struct {
	Container string `json:"container" jsonschema:"Container name or ID (must be tracked by this session)"`
}

type dockerLogsInput struct {
	Container string `json:"container" jsonschema:"Container name or ID (must be tracked by this session)"`
	Tail      string `json:"tail,omitempty" jsonschema:"Number of lines to show from the end (default: 100)"`
}

type dockerPullInput struct {
	Image string `json:"image" jsonschema:"Image to pull"`
}

func textResult(text string) *mcp.CallToolResult {
	return &mcp.CallToolResult{
		Content: []mcp.Content{
			&mcp.TextContent{Text: text},
		},
	}
}

func errorResult(text string) *mcp.CallToolResult {
	return &mcp.CallToolResult{
		Content: []mcp.Content{
			&mcp.TextContent{Text: text},
		},
		IsError: true,
	}
}

// registerTools adds all Docker tools to the MCP server.
func (s *Server) registerTools() {
	mcp.AddTool(s.mcpServer, &mcp.Tool{
		Name:        "docker_ps",
		Description: "List containers created by this session",
	}, s.toolDockerPS)

	mcp.AddTool(s.mcpServer, &mcp.Tool{
		Name:        "docker_run",
		Description: "Run a new container. The container is connected to the session network so the agent can communicate with it by name.",
	}, s.toolDockerRun)

	mcp.AddTool(s.mcpServer, &mcp.Tool{
		Name:        "docker_exec",
		Description: "Execute a command in a running container (must be a container created by this session)",
	}, s.toolDockerExec)

	mcp.AddTool(s.mcpServer, &mcp.Tool{
		Name:        "docker_stop",
		Description: "Stop a running container (must be a container created by this session)",
	}, s.toolDockerStop)

	mcp.AddTool(s.mcpServer, &mcp.Tool{
		Name:        "docker_rm",
		Description: "Remove a container (must be a container created by this session)",
	}, s.toolDockerRM)

	mcp.AddTool(s.mcpServer, &mcp.Tool{
		Name:        "docker_logs",
		Description: "Get container logs (must be a container created by this session)",
	}, s.toolDockerLogs)

	mcp.AddTool(s.mcpServer, &mcp.Tool{
		Name:        "docker_pull",
		Description: "Pull a Docker image",
	}, s.toolDockerPull)
}

func (s *Server) toolDockerPS(ctx context.Context, _ *mcp.CallToolRequest, _ dockerPSInput) (*mcp.CallToolResult, any, error) {
	tracked := s.tracker.List()
	if len(tracked) == 0 {
		return textResult("No containers running in this session."), nil, nil
	}

	type containerStatus struct {
		ID     string `json:"id"`
		Name   string `json:"name"`
		Image  string `json:"image"`
		Status string `json:"status"`
	}

	var results []containerStatus
	for _, tc := range tracked {
		info, err := s.docker.ContainerInspect(ctx, tc.ID)
		if err != nil {
			results = append(results, containerStatus{
				ID:     tc.ID[:12],
				Name:   tc.Name,
				Status: "unknown (inspect failed)",
			})
			continue
		}
		results = append(results, containerStatus{
			ID:     tc.ID[:12],
			Name:   tc.Name,
			Image:  info.Config.Image,
			Status: info.State.Status,
		})
	}

	data, _ := json.MarshalIndent(results, "", "  ")
	return textResult(string(data)), nil, nil
}

func (s *Server) toolDockerRun(ctx context.Context, _ *mcp.CallToolRequest, input dockerRunInput) (*mcp.CallToolResult, any, error) {
	detach := true
	if input.Detach != nil {
		detach = *input.Detach
	}

	exposedPorts := nat.PortSet{}
	portBindings := nat.PortMap{}
	for _, p := range input.Ports {
		parts := strings.SplitN(p, ":", 2)
		if len(parts) != 2 {
			return errorResult(fmt.Sprintf("invalid port mapping %q: expected host:container format", p)), nil, nil
		}
		containerPort := nat.Port(parts[1] + "/tcp")
		exposedPorts[containerPort] = struct{}{}
		portBindings[containerPort] = []nat.PortBinding{
			{HostIP: "0.0.0.0", HostPort: parts[0]},
		}
	}

	containerConfig := &container.Config{
		Image:        input.Image,
		Env:          input.Env,
		Cmd:          input.Cmd,
		ExposedPorts: exposedPorts,
	}

	hostConfig := &container.HostConfig{
		PortBindings: portBindings,
		AutoRemove:   input.Rm,
	}

	resp, err := s.docker.ContainerCreate(ctx, containerConfig, hostConfig, nil, nil, input.Name)
	if err != nil {
		return errorResult(fmt.Sprintf("failed to create container: %v", err)), nil, nil
	}

	if s.networkName != "" {
		endpointSettings := &network.EndpointSettings{}
		if input.Name != "" {
			endpointSettings.Aliases = []string{input.Name}
		}
		if err := s.docker.NetworkConnect(ctx, s.networkName, resp.ID, endpointSettings); err != nil {
			_ = s.docker.ContainerRemove(ctx, resp.ID, container.RemoveOptions{Force: true})
			return errorResult(fmt.Sprintf("failed to connect container to session network: %v", err)), nil, nil
		}
	}

	trackName := input.Name
	if trackName == "" {
		trackName = resp.ID[:12]
	}
	s.tracker.Add(resp.ID, trackName)

	if err := s.docker.ContainerStart(ctx, resp.ID, container.StartOptions{}); err != nil {
		s.tracker.Remove(resp.ID)
		_ = s.docker.ContainerRemove(ctx, resp.ID, container.RemoveOptions{Force: true})
		return errorResult(fmt.Sprintf("failed to start container: %v", err)), nil, nil
	}

	if !detach {
		statusCh, errCh := s.docker.ContainerWait(ctx, resp.ID, container.WaitConditionNotRunning)
		select {
		case err := <-errCh:
			if err != nil {
				return errorResult(fmt.Sprintf("error waiting for container: %v", err)), nil, nil
			}
		case <-statusCh:
		}

		logs, err := s.getContainerLogs(ctx, resp.ID, "all")
		if err != nil {
			return textResult(fmt.Sprintf("Container %s finished but failed to get logs: %v", resp.ID[:12], err)), nil, nil
		}
		return textResult(logs), nil, nil
	}

	return textResult(fmt.Sprintf("Container started: %s (ID: %s)", trackName, resp.ID[:12])), nil, nil
}

func (s *Server) toolDockerExec(ctx context.Context, _ *mcp.CallToolRequest, input dockerExecInput) (*mcp.CallToolResult, any, error) {
	containerID := s.tracker.ResolveID(input.Container)
	if containerID == "" {
		return errorResult(fmt.Sprintf("access denied: container %q is not tracked by this session", input.Container)), nil, nil
	}

	execConfig := container.ExecOptions{
		Cmd:          input.Cmd,
		AttachStdout: true,
		AttachStderr: true,
	}

	execResp, err := s.docker.ContainerExecCreate(ctx, containerID, execConfig)
	if err != nil {
		return errorResult(fmt.Sprintf("failed to create exec: %v", err)), nil, nil
	}

	attachResp, err := s.docker.ContainerExecAttach(ctx, execResp.ID, container.ExecAttachOptions{})
	if err != nil {
		return errorResult(fmt.Sprintf("failed to attach to exec: %v", err)), nil, nil
	}
	defer attachResp.Close()

	output, err := io.ReadAll(attachResp.Reader)
	if err != nil {
		return errorResult(fmt.Sprintf("failed to read exec output: %v", err)), nil, nil
	}

	inspectResp, err := s.docker.ContainerExecInspect(ctx, execResp.ID)
	if err != nil {
		return textResult(string(output)), nil, nil
	}

	if inspectResp.ExitCode != 0 {
		return errorResult(fmt.Sprintf("Exit code: %d\n%s", inspectResp.ExitCode, string(output))), nil, nil
	}

	return textResult(string(output)), nil, nil
}

func (s *Server) toolDockerStop(ctx context.Context, _ *mcp.CallToolRequest, input dockerContainerInput) (*mcp.CallToolResult, any, error) {
	containerID := s.tracker.ResolveID(input.Container)
	if containerID == "" {
		return errorResult(fmt.Sprintf("access denied: container %q is not tracked by this session", input.Container)), nil, nil
	}

	if err := s.docker.ContainerStop(ctx, containerID, container.StopOptions{}); err != nil {
		return errorResult(fmt.Sprintf("failed to stop container: %v", err)), nil, nil
	}

	return textResult(fmt.Sprintf("Container %s stopped.", input.Container)), nil, nil
}

func (s *Server) toolDockerRM(ctx context.Context, _ *mcp.CallToolRequest, input dockerContainerInput) (*mcp.CallToolResult, any, error) {
	containerID := s.tracker.ResolveID(input.Container)
	if containerID == "" {
		return errorResult(fmt.Sprintf("access denied: container %q is not tracked by this session", input.Container)), nil, nil
	}

	if err := s.docker.ContainerRemove(ctx, containerID, container.RemoveOptions{Force: true}); err != nil {
		return errorResult(fmt.Sprintf("failed to remove container: %v", err)), nil, nil
	}

	s.tracker.Remove(containerID)
	return textResult(fmt.Sprintf("Container %s removed.", input.Container)), nil, nil
}

func (s *Server) toolDockerLogs(ctx context.Context, _ *mcp.CallToolRequest, input dockerLogsInput) (*mcp.CallToolResult, any, error) {
	containerID := s.tracker.ResolveID(input.Container)
	if containerID == "" {
		return errorResult(fmt.Sprintf("access denied: container %q is not tracked by this session", input.Container)), nil, nil
	}

	tail := input.Tail
	if tail == "" {
		tail = "100"
	}

	logs, err := s.getContainerLogs(ctx, containerID, tail)
	if err != nil {
		return errorResult(err.Error()), nil, nil
	}

	return textResult(logs), nil, nil
}

func (s *Server) toolDockerPull(ctx context.Context, _ *mcp.CallToolRequest, input dockerPullInput) (*mcp.CallToolResult, any, error) {
	reader, err := s.docker.ImagePull(ctx, input.Image, image.PullOptions{})
	if err != nil {
		return errorResult(fmt.Sprintf("failed to pull image %s: %v", input.Image, err)), nil, nil
	}
	defer reader.Close()

	if _, err := io.Copy(io.Discard, reader); err != nil {
		return errorResult(fmt.Sprintf("failed to complete image pull for %s: %v", input.Image, err)), nil, nil
	}

	return textResult(fmt.Sprintf("Successfully pulled image: %s", input.Image)), nil, nil
}

func (s *Server) getContainerLogs(ctx context.Context, containerID, tail string) (string, error) {
	reader, err := s.docker.ContainerLogs(ctx, containerID, container.LogsOptions{
		ShowStdout: true,
		ShowStderr: true,
		Tail:       tail,
	})
	if err != nil {
		return "", fmt.Errorf("failed to get logs: %w", err)
	}
	defer reader.Close()

	data, err := io.ReadAll(reader)
	if err != nil {
		return "", fmt.Errorf("failed to read logs: %w", err)
	}

	return string(data), nil
}

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
)

// ToolDefinition is the MCP tool definition returned by tools/list.
type ToolDefinition struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	InputSchema any    `json:"inputSchema"`
}

// jsonSchema is a helper for building JSON Schema objects.
type jsonSchema struct {
	Type       string              `json:"type"`
	Properties map[string]property `json:"properties,omitempty"`
	Required   []string            `json:"required,omitempty"`
}

type property struct {
	Type        string      `json:"type"`
	Description string      `json:"description,omitempty"`
	Items       *itemSchema `json:"items,omitempty"`
	Default     any         `json:"default,omitempty"`
}

type itemSchema struct {
	Type string `json:"type"`
}

// allToolDefinitions returns all tool definitions for the tools/list response.
func allToolDefinitions() []ToolDefinition {
	return []ToolDefinition{
		{
			Name:        "docker_ps",
			Description: "List containers created by this session",
			InputSchema: jsonSchema{
				Type:       "object",
				Properties: map[string]property{},
			},
		},
		{
			Name:        "docker_run",
			Description: "Run a new container. The container is connected to the session network so the agent can communicate with it by name.",
			InputSchema: jsonSchema{
				Type: "object",
				Properties: map[string]property{
					"image":  {Type: "string", Description: "Docker image to run (required)"},
					"name":   {Type: "string", Description: "Container name (optional)"},
					"cmd":    {Type: "array", Description: "Command to run (optional)", Items: &itemSchema{Type: "string"}},
					"env":    {Type: "array", Description: "Environment variables in KEY=VALUE format (optional)", Items: &itemSchema{Type: "string"}},
					"ports":  {Type: "array", Description: "Port mappings in host:container format e.g. \"8080:80\" (optional)", Items: &itemSchema{Type: "string"}},
					"detach": {Type: "boolean", Description: "Run in background (default: true)", Default: true},
					"rm":     {Type: "boolean", Description: "Remove container when it exits (optional)"},
				},
				Required: []string{"image"},
			},
		},
		{
			Name:        "docker_exec",
			Description: "Execute a command in a running container (must be a container created by this session)",
			InputSchema: jsonSchema{
				Type: "object",
				Properties: map[string]property{
					"container": {Type: "string", Description: "Container name or ID (must be tracked by this session)"},
					"cmd":       {Type: "array", Description: "Command to execute", Items: &itemSchema{Type: "string"}},
				},
				Required: []string{"container", "cmd"},
			},
		},
		{
			Name:        "docker_stop",
			Description: "Stop a running container (must be a container created by this session)",
			InputSchema: jsonSchema{
				Type: "object",
				Properties: map[string]property{
					"container": {Type: "string", Description: "Container name or ID (must be tracked by this session)"},
				},
				Required: []string{"container"},
			},
		},
		{
			Name:        "docker_rm",
			Description: "Remove a container (must be a container created by this session)",
			InputSchema: jsonSchema{
				Type: "object",
				Properties: map[string]property{
					"container": {Type: "string", Description: "Container name or ID (must be tracked by this session)"},
				},
				Required: []string{"container"},
			},
		},
		{
			Name:        "docker_logs",
			Description: "Get container logs (must be a container created by this session)",
			InputSchema: jsonSchema{
				Type: "object",
				Properties: map[string]property{
					"container": {Type: "string", Description: "Container name or ID (must be tracked by this session)"},
					"tail":      {Type: "string", Description: "Number of lines to show from the end (default: \"100\")"},
				},
				Required: []string{"container"},
			},
		},
		{
			Name:        "docker_pull",
			Description: "Pull a Docker image",
			InputSchema: jsonSchema{
				Type: "object",
				Properties: map[string]property{
					"image": {Type: "string", Description: "Image to pull (required)"},
				},
				Required: []string{"image"},
			},
		},
	}
}

// executeTool runs a tool call.
func (s *Server) executeTool(ctx context.Context, name string, args map[string]any) (string, bool, error) {
	switch name {
	case "docker_ps":
		return s.toolDockerPS(ctx)
	case "docker_run":
		return s.toolDockerRun(ctx, args)
	case "docker_exec":
		return s.toolDockerExec(ctx, args)
	case "docker_stop":
		return s.toolDockerStop(ctx, args)
	case "docker_rm":
		return s.toolDockerRM(ctx, args)
	case "docker_logs":
		return s.toolDockerLogs(ctx, args)
	case "docker_pull":
		return s.toolDockerPull(ctx, args)
	default:
		return "", false, fmt.Errorf("unknown tool: %s", name)
	}
}

// toolDockerPS lists containers tracked by this session.
func (s *Server) toolDockerPS(ctx context.Context) (string, bool, error) {
	tracked := s.tracker.List()
	if len(tracked) == 0 {
		return "No containers running in this session.", false, nil
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
	return string(data), false, nil
}

// toolDockerRun creates and starts a new container.
func (s *Server) toolDockerRun(ctx context.Context, args map[string]any) (string, bool, error) {
	imageName := getString(args, "image")
	if imageName == "" {
		return "", false, fmt.Errorf("missing required parameter: image")
	}

	containerName := getString(args, "name")
	cmd := getStringArray(args, "cmd")
	env := getStringArray(args, "env")
	ports := getStringArray(args, "ports")
	detach := getBool(args, "detach", true)
	autoRemove := getBool(args, "rm", false)

	// Build port bindings
	exposedPorts := nat.PortSet{}
	portBindings := nat.PortMap{}
	for _, p := range ports {
		parts := strings.SplitN(p, ":", 2)
		if len(parts) != 2 {
			return "", false, fmt.Errorf("invalid port mapping %q: expected host:container format", p)
		}
		containerPort := nat.Port(parts[1] + "/tcp")
		exposedPorts[containerPort] = struct{}{}
		portBindings[containerPort] = []nat.PortBinding{
			{HostIP: "0.0.0.0", HostPort: parts[0]},
		}
	}

	containerConfig := &container.Config{
		Image:        imageName,
		Env:          env,
		Cmd:          cmd,
		ExposedPorts: exposedPorts,
	}

	hostConfig := &container.HostConfig{
		PortBindings: portBindings,
		AutoRemove:   autoRemove,
	}

	resp, err := s.docker.ContainerCreate(ctx, containerConfig, hostConfig, nil, nil, containerName)
	if err != nil {
		return "", false, fmt.Errorf("failed to create container: %w", err)
	}

	// Connect the container to the session network so the agent can reach it
	if s.networkName != "" {
		endpointSettings := &network.EndpointSettings{}
		if containerName != "" {
			endpointSettings.Aliases = []string{containerName}
		}
		if err := s.docker.NetworkConnect(ctx, s.networkName, resp.ID, endpointSettings); err != nil {
			// Clean up the created container on network connect failure
			_ = s.docker.ContainerRemove(ctx, resp.ID, container.RemoveOptions{Force: true})
			return "", false, fmt.Errorf("failed to connect container to session network: %w", err)
		}
	}

	// Track the container
	trackName := containerName
	if trackName == "" {
		trackName = resp.ID[:12]
	}
	s.tracker.Add(resp.ID, trackName)

	if err := s.docker.ContainerStart(ctx, resp.ID, container.StartOptions{}); err != nil {
		s.tracker.Remove(resp.ID)
		_ = s.docker.ContainerRemove(ctx, resp.ID, container.RemoveOptions{Force: true})
		return "", false, fmt.Errorf("failed to start container: %w", err)
	}

	if !detach {
		// Wait for the container to finish and return its logs
		statusCh, errCh := s.docker.ContainerWait(ctx, resp.ID, container.WaitConditionNotRunning)
		select {
		case err := <-errCh:
			if err != nil {
				return "", false, fmt.Errorf("error waiting for container: %w", err)
			}
		case <-statusCh:
		}

		logs, err := s.getContainerLogs(ctx, resp.ID, "all")
		if err != nil {
			return fmt.Sprintf("Container %s finished but failed to get logs: %v", resp.ID[:12], err), true, nil
		}
		return logs, false, nil
	}

	return fmt.Sprintf("Container started: %s (ID: %s)", trackName, resp.ID[:12]), false, nil
}

// toolDockerExec executes a command inside a tracked container.
func (s *Server) toolDockerExec(ctx context.Context, args map[string]any) (string, bool, error) {
	containerRef := getString(args, "container")
	if containerRef == "" {
		return "", false, fmt.Errorf("missing required parameter: container")
	}

	cmd := getStringArray(args, "cmd")
	if len(cmd) == 0 {
		return "", false, fmt.Errorf("missing required parameter: cmd")
	}

	containerID := s.tracker.ResolveID(containerRef)
	if containerID == "" {
		return "", false, fmt.Errorf("access denied: container %q is not tracked by this session", containerRef)
	}

	execConfig := container.ExecOptions{
		Cmd:          cmd,
		AttachStdout: true,
		AttachStderr: true,
	}

	execResp, err := s.docker.ContainerExecCreate(ctx, containerID, execConfig)
	if err != nil {
		return "", false, fmt.Errorf("failed to create exec: %w", err)
	}

	attachResp, err := s.docker.ContainerExecAttach(ctx, execResp.ID, container.ExecAttachOptions{})
	if err != nil {
		return "", false, fmt.Errorf("failed to attach to exec: %w", err)
	}
	defer attachResp.Close()

	output, err := io.ReadAll(attachResp.Reader)
	if err != nil {
		return "", false, fmt.Errorf("failed to read exec output: %w", err)
	}

	// Check exit code
	inspectResp, err := s.docker.ContainerExecInspect(ctx, execResp.ID)
	if err != nil {
		return string(output), false, nil
	}

	if inspectResp.ExitCode != 0 {
		return fmt.Sprintf("Exit code: %d\n%s", inspectResp.ExitCode, string(output)), true, nil
	}

	return string(output), false, nil
}

// toolDockerStop stops a tracked container.
func (s *Server) toolDockerStop(ctx context.Context, args map[string]any) (string, bool, error) {
	containerRef := getString(args, "container")
	if containerRef == "" {
		return "", false, fmt.Errorf("missing required parameter: container")
	}

	containerID := s.tracker.ResolveID(containerRef)
	if containerID == "" {
		return "", false, fmt.Errorf("access denied: container %q is not tracked by this session", containerRef)
	}

	if err := s.docker.ContainerStop(ctx, containerID, container.StopOptions{}); err != nil {
		return "", false, fmt.Errorf("failed to stop container: %w", err)
	}

	return fmt.Sprintf("Container %s stopped.", containerRef), false, nil
}

// toolDockerRM removes a tracked container.
func (s *Server) toolDockerRM(ctx context.Context, args map[string]any) (string, bool, error) {
	containerRef := getString(args, "container")
	if containerRef == "" {
		return "", false, fmt.Errorf("missing required parameter: container")
	}

	containerID := s.tracker.ResolveID(containerRef)
	if containerID == "" {
		return "", false, fmt.Errorf("access denied: container %q is not tracked by this session", containerRef)
	}

	if err := s.docker.ContainerRemove(ctx, containerID, container.RemoveOptions{Force: true}); err != nil {
		return "", false, fmt.Errorf("failed to remove container: %w", err)
	}

	s.tracker.Remove(containerID)
	return fmt.Sprintf("Container %s removed.", containerRef), false, nil
}

// toolDockerLogs returns logs from a tracked container.
func (s *Server) toolDockerLogs(ctx context.Context, args map[string]any) (string, bool, error) {
	containerRef := getString(args, "container")
	if containerRef == "" {
		return "", false, fmt.Errorf("missing required parameter: container")
	}

	containerID := s.tracker.ResolveID(containerRef)
	if containerID == "" {
		return "", false, fmt.Errorf("access denied: container %q is not tracked by this session", containerRef)
	}

	tail := getString(args, "tail")
	if tail == "" {
		tail = "100"
	}

	logs, err := s.getContainerLogs(ctx, containerID, tail)
	if err != nil {
		return "", false, err
	}

	return logs, false, nil
}

// toolDockerPull pulls a Docker image.
func (s *Server) toolDockerPull(ctx context.Context, args map[string]any) (string, bool, error) {
	imageName := getString(args, "image")
	if imageName == "" {
		return "", false, fmt.Errorf("missing required parameter: image")
	}

	reader, err := s.docker.ImagePull(ctx, imageName, image.PullOptions{})
	if err != nil {
		return "", false, fmt.Errorf("failed to pull image %s: %w", imageName, err)
	}
	defer reader.Close()

	// Drain the reader to complete the pull
	if _, err := io.Copy(io.Discard, reader); err != nil {
		return "", false, fmt.Errorf("failed to complete image pull for %s: %w", imageName, err)
	}

	return fmt.Sprintf("Successfully pulled image: %s", imageName), false, nil
}

// getContainerLogs reads logs from a container.
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

// Helper functions for extracting values from args map.

func getString(args map[string]any, key string) string {
	if v, ok := args[key]; ok {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return ""
}

func getStringArray(args map[string]any, key string) []string {
	v, ok := args[key]
	if !ok {
		return nil
	}

	switch arr := v.(type) {
	case []any:
		result := make([]string, 0, len(arr))
		for _, item := range arr {
			if s, ok := item.(string); ok {
				result = append(result, s)
			}
		}
		return result
	case []string:
		return arr
	}

	return nil
}

func getBool(args map[string]any, key string, defaultVal bool) bool {
	v, ok := args[key]
	if !ok {
		return defaultVal
	}
	if b, ok := v.(bool); ok {
		return b
	}
	return defaultVal
}


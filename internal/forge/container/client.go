package container

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/api/types/image"
	"github.com/docker/docker/api/types/mount"
	"github.com/docker/docker/api/types/network"
	dockerclient "github.com/docker/docker/client"
)

//go:generate mockgen -destination=mock_docker_test.go -package=container github.com/michael-freling/claude-code-tools/internal/forge/container DockerAPI

// ContainerManager abstracts container operations for the orchestrator.
type ContainerManager interface {
	CreateNetwork(ctx context.Context, name string) (string, error)
	RemoveNetwork(ctx context.Context, name string) error
	EnsureSharedNetwork(ctx context.Context, name string) (string, error)
	ConnectNetwork(ctx context.Context, networkName, containerName string, aliases []string) error
	StartAgent(ctx context.Context, opts AgentOptions) (string, error)
	StartGateway(ctx context.Context, opts GatewayOptions) (string, error)
	StartGitHubMCP(ctx context.Context, opts GitHubMCPOptions) (string, error)
	StartSharedService(ctx context.Context, opts SharedServiceOptions) (string, error)
	IsContainerRunning(ctx context.Context, name string) (bool, error)
	WaitForReady(ctx context.Context, containerID string, timeout time.Duration) error
	StopContainer(ctx context.Context, name string) error
	RemoveContainer(ctx context.Context, name string) error
	ListForgeContainers(ctx context.Context) ([]ContainerInfo, error)
	PullImage(ctx context.Context, image string) error
	ImageExists(ctx context.Context, image string) (bool, error)
	ContainerLogs(ctx context.Context, containerID string) (string, error)
	Close() error
}

// Ensure Client implements ContainerManager at compile time.
var _ ContainerManager = (*Client)(nil)

// DockerAPI abstracts the Docker client methods used by the forge client.
type DockerAPI interface {
	ContainerCreate(ctx context.Context, config *container.Config, hostConfig *container.HostConfig, networkingConfig *network.NetworkingConfig, containerName string) (container.CreateResponse, error)
	ContainerStart(ctx context.Context, containerID string, options container.StartOptions) error
	ContainerStop(ctx context.Context, containerID string, options container.StopOptions) error
	ContainerRemove(ctx context.Context, containerID string, options container.RemoveOptions) error
	ContainerList(ctx context.Context, options container.ListOptions) ([]container.Summary, error)
	ContainerInspect(ctx context.Context, containerID string) (container.InspectResponse, error)
	ContainerLogs(ctx context.Context, containerID string, options container.LogsOptions) (io.ReadCloser, error)
	NetworkCreate(ctx context.Context, name string, options network.CreateOptions) (network.CreateResponse, error)
	NetworkRemove(ctx context.Context, networkID string) error
	NetworkConnect(ctx context.Context, networkID, containerID string, config *network.EndpointSettings) error
	NetworkList(ctx context.Context, options network.ListOptions) ([]network.Inspect, error)
	ImagePull(ctx context.Context, refStr string, options image.PullOptions) (io.ReadCloser, error)
	ImageList(ctx context.Context, options image.ListOptions) ([]image.Summary, error)
	Close() error
}

// dockerAPIWrapper wraps the Docker client to implement DockerAPI.
// This is needed because the Docker SDK's ContainerCreate has a different signature
// (includes platform parameter) than our interface.
type dockerAPIWrapper struct {
	client *dockerclient.Client
}

func (w *dockerAPIWrapper) ContainerCreate(ctx context.Context, config *container.Config, hostConfig *container.HostConfig, networkingConfig *network.NetworkingConfig, containerName string) (container.CreateResponse, error) {
	return w.client.ContainerCreate(ctx, config, hostConfig, networkingConfig, nil, containerName)
}

func (w *dockerAPIWrapper) ContainerStart(ctx context.Context, containerID string, options container.StartOptions) error {
	return w.client.ContainerStart(ctx, containerID, options)
}

func (w *dockerAPIWrapper) ContainerStop(ctx context.Context, containerID string, options container.StopOptions) error {
	return w.client.ContainerStop(ctx, containerID, options)
}

func (w *dockerAPIWrapper) ContainerRemove(ctx context.Context, containerID string, options container.RemoveOptions) error {
	return w.client.ContainerRemove(ctx, containerID, options)
}

func (w *dockerAPIWrapper) ContainerList(ctx context.Context, options container.ListOptions) ([]container.Summary, error) {
	return w.client.ContainerList(ctx, options)
}

func (w *dockerAPIWrapper) ContainerInspect(ctx context.Context, containerID string) (container.InspectResponse, error) {
	return w.client.ContainerInspect(ctx, containerID)
}

func (w *dockerAPIWrapper) ContainerLogs(ctx context.Context, containerID string, options container.LogsOptions) (io.ReadCloser, error) {
	return w.client.ContainerLogs(ctx, containerID, options)
}

func (w *dockerAPIWrapper) NetworkCreate(ctx context.Context, name string, options network.CreateOptions) (network.CreateResponse, error) {
	return w.client.NetworkCreate(ctx, name, options)
}

func (w *dockerAPIWrapper) NetworkRemove(ctx context.Context, networkID string) error {
	return w.client.NetworkRemove(ctx, networkID)
}

func (w *dockerAPIWrapper) NetworkConnect(ctx context.Context, networkID, containerID string, config *network.EndpointSettings) error {
	return w.client.NetworkConnect(ctx, networkID, containerID, config)
}

func (w *dockerAPIWrapper) NetworkList(ctx context.Context, options network.ListOptions) ([]network.Inspect, error) {
	return w.client.NetworkList(ctx, options)
}

func (w *dockerAPIWrapper) ImagePull(ctx context.Context, refStr string, options image.PullOptions) (io.ReadCloser, error) {
	return w.client.ImagePull(ctx, refStr, options)
}

func (w *dockerAPIWrapper) ImageList(ctx context.Context, options image.ListOptions) ([]image.Summary, error) {
	return w.client.ImageList(ctx, options)
}

func (w *dockerAPIWrapper) Close() error {
	return w.client.Close()
}

// Client provides Docker operations for claude-forge.
type Client struct {
	docker DockerAPI
}

// NewClient creates a new Docker client using environment configuration.
func NewClient() (*Client, error) {
	cli, err := dockerclient.NewClientWithOpts(dockerclient.FromEnv, dockerclient.WithAPIVersionNegotiation())
	if err != nil {
		return nil, fmt.Errorf("failed to create Docker client: %w", err)
	}

	return &Client{
		docker: &dockerAPIWrapper{client: cli},
	}, nil
}

// newClientWithAPI creates a new Client with the given DockerAPI (for testing).
func newClientWithAPI(api DockerAPI) *Client {
	return &Client{docker: api}
}

// Close closes the Docker client connection.
func (c *Client) Close() error {
	return c.docker.Close()
}

// CreateNetwork creates a Docker network with the given name.
func (c *Client) CreateNetwork(ctx context.Context, name string) (string, error) {
	resp, err := c.docker.NetworkCreate(ctx, name, network.CreateOptions{
		Driver: "bridge",
	})
	if err != nil {
		return "", fmt.Errorf("failed to create network %s: %w", name, err)
	}

	return resp.ID, nil
}

// RemoveNetwork removes a Docker network by name.
func (c *Client) RemoveNetwork(ctx context.Context, name string) error {
	if err := c.docker.NetworkRemove(ctx, name); err != nil {
		return fmt.Errorf("failed to remove network %s: %w", name, err)
	}
	return nil
}

// CacheDir represents a host dependency cache directory to mount into the container.
type CacheDir struct {
	Source string // host path
	Target string // container path
}

// AgentOptions holds configuration for starting an agent container.
type AgentOptions struct {
	Name        string            // container name: forge-agent-<project-id>-<session-id>
	Image       string            // agent image
	NetworkName string            // Docker network to attach to
	ProjectDir  string            // host path to project (mounted at /work)
	SessionDir  string            // host path to session storage
	ClaudeDir   string            // host path to ~/.claude/
	ConfigDir   string            // host path to ~/.config/claude-forge/
	HomeDir     string            // host home dir for CLAUDE.md paths
	Env         map[string]string // environment variables
	Privileged  bool
	Interactive bool       // allocate TTY and stdin (for docker attach)
	Cmd         []string   // claude args: --dangerously-skip-permissions, --worktree, etc.
	UID         int        // host user UID (for file ownership mapping)
	GID         int        // host user GID (for file ownership mapping)
	PluginsDir  string     // host path to forge plugins dir (mounted rw at ~/.claude/plugins)
	CacheDirs   []CacheDir // host dependency cache directories to mount (rw)
	ExtraMounts []CacheDir // additional user-specified bind mounts (rw)
}

// StartAgent creates and starts an agent container.
func (c *Client) StartAgent(ctx context.Context, opts AgentOptions) (string, error) {
	env := make([]string, 0, len(opts.Env))
	for k, v := range opts.Env {
		env = append(env, fmt.Sprintf("%s=%s", k, v))
	}
	if opts.UID > 0 {
		env = append(env, fmt.Sprintf("FORGE_UID=%d", opts.UID))
	}
	if opts.GID > 0 {
		env = append(env, fmt.Sprintf("FORGE_GID=%d", opts.GID))
	}

	mounts := []mount.Mount{
		{
			Type:   mount.TypeBind,
			Source: opts.ProjectDir,
			Target: "/work",
		},
	}

	// Session directory mount.
	// Mounted at the projects parent so Claude Code's session JSONL files for both
	// the main /work cwd (encoded as -work) and any worktree cwd (encoded as
	// -work-.claude-worktrees-<name>) persist to the host.
	if opts.SessionDir != "" {
		mounts = append(mounts, mount.Mount{
			Type:   mount.TypeBind,
			Source: opts.SessionDir,
			Target: "/home/user/.claude/projects",
		})
	}

	// Claude dir mounts (read-only) for rules, agents, commands, skills, CLAUDE.md.
	// Resolve symlinks so Docker gets the real path, and skip dirs that don't exist.
	if opts.ClaudeDir != "" {
		claudeSubdirs := []string{"rules", "agents", "commands", "skills"}
		for _, subdir := range claudeSubdirs {
			source := filepath.Join(opts.ClaudeDir, subdir)
			resolved, err := filepath.EvalSymlinks(source)
			if err != nil {
				continue
			}
			mounts = append(mounts, mount.Mount{
				Type:     mount.TypeBind,
				Source:   resolved,
				Target:   "/home/user/.claude/" + subdir,
				ReadOnly: true,
			})
		}

		// Credentials file mount (read-write so Claude Code can refresh OAuth tokens)
		credentialsPath := opts.ClaudeDir + "/.credentials.json"
		if _, err := os.Stat(credentialsPath); err == nil {
			mounts = append(mounts, mount.Mount{
				Type:   mount.TypeBind,
				Source: credentialsPath,
				Target: "/home/user/.claude/.credentials.json",
			})
		}
	}

	// Plugins dir mount (read-write, managed separately from host's ~/.claude/plugins)
	if opts.PluginsDir != "" {
		mounts = append(mounts, mount.Mount{
			Type:   mount.TypeBind,
			Source: opts.PluginsDir,
			Target: "/home/user/.claude/plugins",
		})
	}

	// Config dir file mounts for settings.json, .claude.json, gitconfig
	// settings.json and .claude.json are read-write because Claude Code writes to them at runtime.
	if opts.ConfigDir != "" {
		mounts = append(mounts, mount.Mount{
			Type:   mount.TypeBind,
			Source: opts.ConfigDir + "/settings.json",
			Target: "/home/user/.claude/settings.json",
		})
		mounts = append(mounts, mount.Mount{
			Type:   mount.TypeBind,
			Source: opts.ConfigDir + "/.claude.json",
			Target: "/home/user/.claude.json",
		})
		mounts = append(mounts, mount.Mount{
			Type:     mount.TypeBind,
			Source:   opts.ConfigDir + "/gitconfig",
			Target:   "/home/user/.gitconfig",
			ReadOnly: true,
		})
	}

	// Home CLAUDE.md mount (read-only) — skip if file doesn't exist or is a broken symlink
	if opts.HomeDir != "" {
		claudeMDSource := filepath.Join(opts.HomeDir, "CLAUDE.md")
		if resolved, err := filepath.EvalSymlinks(claudeMDSource); err == nil {
			mounts = append(mounts, mount.Mount{
				Type:     mount.TypeBind,
				Source:   resolved,
				Target:   "/home/user/CLAUDE.md",
				ReadOnly: true,
			})
		}
	}

	// Cache directory mounts (read-write)
	for _, cache := range opts.CacheDirs {
		mounts = append(mounts, mount.Mount{
			Type:   mount.TypeBind,
			Source: cache.Source,
			Target: cache.Target,
		})
	}

	// Extra user-specified bind mounts (read-write)
	for _, m := range opts.ExtraMounts {
		mounts = append(mounts, mount.Mount{
			Type:   mount.TypeBind,
			Source: m.Source,
			Target: m.Target,
		})
	}

	containerConfig := &container.Config{
		Image:      opts.Image,
		Env:        env,
		Cmd:        append([]string{"claude"}, opts.Cmd...),
		WorkingDir: "/work",
		Tty:        opts.Interactive,
		OpenStdin:  opts.Interactive,
	}

	hostConfig := &container.HostConfig{
		Mounts:     mounts,
		Privileged: opts.Privileged,
	}

	networkingConfig := &network.NetworkingConfig{
		EndpointsConfig: map[string]*network.EndpointSettings{
			opts.NetworkName: {},
		},
	}

	resp, err := c.docker.ContainerCreate(ctx, containerConfig, hostConfig, networkingConfig, opts.Name)
	if err != nil {
		return "", fmt.Errorf("failed to create agent container: %w", err)
	}

	if err := c.docker.ContainerStart(ctx, resp.ID, container.StartOptions{}); err != nil {
		return "", fmt.Errorf("failed to start agent container: %w", err)
	}

	return resp.ID, nil
}

// GatewayOptions holds configuration for starting a gateway container.
type GatewayOptions struct {
	Name        string // container name: forge-gateway-<project-id>-<session-id>
	Image       string
	NetworkName string
	SSHDir      string // host ~/.ssh/ (ro)
	GHConfigDir string // host ~/.config/gh/ (ro)
	Owner       string // allowed repo owner
	Repo        string // allowed repo name
	Env         map[string]string
}

// StartGateway creates and starts a gateway container.
func (c *Client) StartGateway(ctx context.Context, opts GatewayOptions) (string, error) {
	env := make([]string, 0, len(opts.Env))
	for k, v := range opts.Env {
		env = append(env, fmt.Sprintf("%s=%s", k, v))
	}

	mounts := []mount.Mount{}

	if opts.SSHDir != "" {
		mounts = append(mounts, mount.Mount{
			Type:     mount.TypeBind,
			Source:   opts.SSHDir,
			Target:   "/home/user/.ssh",
			ReadOnly: true,
		})
	}

	if opts.GHConfigDir != "" {
		mounts = append(mounts, mount.Mount{
			Type:     mount.TypeBind,
			Source:   opts.GHConfigDir,
			Target:   "/home/user/.config/gh",
			ReadOnly: true,
		})
	}

	containerConfig := &container.Config{
		Image: opts.Image,
		Env:   env,
		Cmd:   []string{"gateway", fmt.Sprintf("--owner=%s", opts.Owner), fmt.Sprintf("--repo=%s", opts.Repo)},
	}

	hostConfig := &container.HostConfig{
		Mounts: mounts,
	}

	networkingConfig := &network.NetworkingConfig{
		EndpointsConfig: map[string]*network.EndpointSettings{
			opts.NetworkName: {
				Aliases: []string{"gateway"},
			},
		},
	}

	resp, err := c.docker.ContainerCreate(ctx, containerConfig, hostConfig, networkingConfig, opts.Name)
	if err != nil {
		return "", fmt.Errorf("failed to create gateway container: %w", err)
	}

	if err := c.docker.ContainerStart(ctx, resp.ID, container.StartOptions{}); err != nil {
		return "", fmt.Errorf("failed to start gateway container: %w", err)
	}

	return resp.ID, nil
}

// GitHubMCPOptions holds configuration for starting a GitHub MCP sidecar container.
type GitHubMCPOptions struct {
	Name        string            // container name: forge-github-mcp-<project-id>-<session-id>
	Image       string            // github-mcp image
	NetworkName string            // Docker network to attach to
	Owner       string            // allowed repo owner
	Repo        string            // allowed repo name
	Env         map[string]string // environment variables (GITHUB_TOKEN)
}

// StartGitHubMCP creates and starts a GitHub MCP sidecar container.
func (c *Client) StartGitHubMCP(ctx context.Context, opts GitHubMCPOptions) (string, error) {
	env := make([]string, 0, len(opts.Env))
	for k, v := range opts.Env {
		env = append(env, fmt.Sprintf("%s=%s", k, v))
	}

	containerConfig := &container.Config{
		Image: opts.Image,
		Env:   env,
		Cmd:   []string{"--owner=" + opts.Owner, "--repo=" + opts.Repo},
	}

	hostConfig := &container.HostConfig{}

	networkingConfig := &network.NetworkingConfig{
		EndpointsConfig: map[string]*network.EndpointSettings{
			opts.NetworkName: {
				Aliases: []string{"github-mcp"},
			},
		},
	}

	resp, err := c.docker.ContainerCreate(ctx, containerConfig, hostConfig, networkingConfig, opts.Name)
	if err != nil {
		return "", fmt.Errorf("failed to create github-mcp container: %w", err)
	}

	if err := c.docker.ContainerStart(ctx, resp.ID, container.StartOptions{}); err != nil {
		return "", fmt.Errorf("failed to start github-mcp container: %w", err)
	}

	return resp.ID, nil
}

// SharedServiceOptions holds configuration for starting a shared singleton service container.
type SharedServiceOptions struct {
	Name        string            // container name, e.g. "forge-k8s-mcp"
	Image       string            // Docker image
	NetworkName string            // shared network name
	Alias       string            // network alias, e.g. "k8s-mcp"
	Env         map[string]string // environment variables
	Mounts      []mount.Mount     // bind mounts
	Cmd         []string          // container command
}

// StartSharedService creates and starts a shared singleton service container.
func (c *Client) StartSharedService(ctx context.Context, opts SharedServiceOptions) (string, error) {
	env := make([]string, 0, len(opts.Env))
	for k, v := range opts.Env {
		env = append(env, fmt.Sprintf("%s=%s", k, v))
	}

	containerConfig := &container.Config{
		Image: opts.Image,
		Env:   env,
		Cmd:   opts.Cmd,
	}

	hostConfig := &container.HostConfig{
		Mounts: opts.Mounts,
	}

	networkingConfig := &network.NetworkingConfig{
		EndpointsConfig: map[string]*network.EndpointSettings{
			opts.NetworkName: {
				Aliases: []string{opts.Alias},
			},
		},
	}

	resp, err := c.docker.ContainerCreate(ctx, containerConfig, hostConfig, networkingConfig, opts.Name)
	if err != nil {
		return "", fmt.Errorf("failed to create %s container: %w", opts.Name, err)
	}

	if err := c.docker.ContainerStart(ctx, resp.ID, container.StartOptions{}); err != nil {
		return "", fmt.Errorf("failed to start %s container: %w", opts.Name, err)
	}

	return resp.ID, nil
}

// EnsureSharedNetwork creates a shared Docker network if it doesn't already exist.
// Returns the network ID.
func (c *Client) EnsureSharedNetwork(ctx context.Context, name string) (string, error) {
	// Check if network already exists
	filterArgs := filters.NewArgs()
	filterArgs.Add("name", "^"+name+"$")
	networks, err := c.docker.NetworkList(ctx, network.ListOptions{Filters: filterArgs})
	if err != nil {
		return "", fmt.Errorf("failed to list networks: %w", err)
	}
	for _, n := range networks {
		if n.Name == name {
			return n.ID, nil
		}
	}

	// Create new network
	resp, err := c.docker.NetworkCreate(ctx, name, network.CreateOptions{
		Driver: "bridge",
	})
	if err != nil {
		return "", fmt.Errorf("failed to create shared network %s: %w", name, err)
	}
	return resp.ID, nil
}

// ConnectNetwork connects a container to a Docker network with optional aliases.
func (c *Client) ConnectNetwork(ctx context.Context, networkName, containerName string, aliases []string) error {
	config := &network.EndpointSettings{}
	if len(aliases) > 0 {
		config.Aliases = aliases
	}
	if err := c.docker.NetworkConnect(ctx, networkName, containerName, config); err != nil {
		return fmt.Errorf("failed to connect %s to network %s: %w", containerName, networkName, err)
	}
	return nil
}

// IsContainerRunning checks if a container with the given name exists and is running.
// Returns false (no error) if the container doesn't exist.
func (c *Client) IsContainerRunning(ctx context.Context, name string) (bool, error) {
	info, err := c.docker.ContainerInspect(ctx, name)
	if err != nil {
		if strings.Contains(err.Error(), "No such container") || strings.Contains(err.Error(), "not found") {
			return false, nil
		}
		return false, fmt.Errorf("failed to inspect container %s: %w", name, err)
	}
	return info.State != nil && info.State.Running, nil
}

// WaitForReady polls the container state until it is running or exits.
// After the container first appears running, it re-checks after a short
// stabilization delay to catch processes that crash immediately on startup.
// Returns an error if the container exits before the timeout.
func (c *Client) WaitForReady(ctx context.Context, containerID string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	pollInterval := 200 * time.Millisecond
	stabilizeDelay := 500 * time.Millisecond

	for time.Now().Before(deadline) {
		info, err := c.docker.ContainerInspect(ctx, containerID)
		if err != nil {
			return fmt.Errorf("failed to inspect container %s: %w", containerID, err)
		}

		if info.State != nil {
			if info.State.Running {
				select {
				case <-ctx.Done():
					return ctx.Err()
				case <-time.After(stabilizeDelay):
				}
				info, err = c.docker.ContainerInspect(ctx, containerID)
				if err != nil {
					return fmt.Errorf("failed to inspect container %s: %w", containerID, err)
				}
				if info.State != nil && (info.State.Status == "exited" || info.State.Status == "dead") {
					return fmt.Errorf("container %s exited with code %d", containerID, info.State.ExitCode)
				}
				return nil
			}
			if info.State.Status == "exited" || info.State.Status == "dead" {
				return fmt.Errorf("container %s exited with code %d", containerID, info.State.ExitCode)
			}
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(pollInterval):
		}
	}

	return fmt.Errorf("timed out waiting for container %s to be ready", containerID)
}

// ContainerLogs returns the stdout/stderr logs from a container.
func (c *Client) ContainerLogs(ctx context.Context, containerID string) (string, error) {
	reader, err := c.docker.ContainerLogs(ctx, containerID, container.LogsOptions{
		ShowStdout: true,
		ShowStderr: true,
		Tail:       "20",
	})
	if err != nil {
		return "", fmt.Errorf("failed to get logs for container %s: %w", containerID, err)
	}
	defer reader.Close()

	data, err := io.ReadAll(reader)
	if err != nil {
		return "", fmt.Errorf("failed to read logs for container %s: %w", containerID, err)
	}
	return string(data), nil
}

// ContainerInfo holds information about a running container.
type ContainerInfo struct {
	Name    string
	ID      string
	Image   string
	Status  string
	Created time.Time
}

// StopContainer stops a container by name.
func (c *Client) StopContainer(ctx context.Context, name string) error {
	if err := c.docker.ContainerStop(ctx, name, container.StopOptions{}); err != nil {
		return fmt.Errorf("failed to stop container %s: %w", name, err)
	}
	return nil
}

// RemoveContainer removes a container by name.
func (c *Client) RemoveContainer(ctx context.Context, name string) error {
	if err := c.docker.ContainerRemove(ctx, name, container.RemoveOptions{Force: true}); err != nil {
		return fmt.Errorf("failed to remove container %s: %w", name, err)
	}
	return nil
}

// ListForgeContainers lists containers with names matching "forge-agent-*" or "forge-gateway-*".
func (c *Client) ListForgeContainers(ctx context.Context) ([]ContainerInfo, error) {
	filterArgs := filters.NewArgs()
	filterArgs.Add("name", "forge-agent-")
	filterArgs.Add("name", "forge-gateway-")
	filterArgs.Add("name", "forge-github-mcp-")
	filterArgs.Add("name", "forge-k8s-mcp")

	containers, err := c.docker.ContainerList(ctx, container.ListOptions{
		All:     true,
		Filters: filterArgs,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to list forge containers: %w", err)
	}

	result := make([]ContainerInfo, 0, len(containers))
	for _, c := range containers {
		name := ""
		if len(c.Names) > 0 {
			name = strings.TrimPrefix(c.Names[0], "/")
		}

		result = append(result, ContainerInfo{
			Name:    name,
			ID:      c.ID,
			Image:   c.Image,
			Status:  c.Status,
			Created: time.Unix(c.Created, 0),
		})
	}

	return result, nil
}

// PullImage pulls a Docker image.
func (c *Client) PullImage(ctx context.Context, imageName string) error {
	reader, err := c.docker.ImagePull(ctx, imageName, image.PullOptions{})
	if err != nil {
		return fmt.Errorf("failed to pull image %s: %w", imageName, err)
	}
	defer reader.Close()

	// Drain the reader to complete the pull
	if _, err := io.Copy(io.Discard, reader); err != nil {
		return fmt.Errorf("failed to complete image pull for %s: %w", imageName, err)
	}

	return nil
}

// ImageExists checks if a Docker image exists locally.
func (c *Client) ImageExists(ctx context.Context, imageName string) (bool, error) {
	filterArgs := filters.NewArgs()
	filterArgs.Add("reference", imageName)

	images, err := c.docker.ImageList(ctx, image.ListOptions{
		Filters: filterArgs,
	})
	if err != nil {
		return false, fmt.Errorf("failed to list images: %w", err)
	}

	return len(images) > 0, nil
}

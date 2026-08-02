//go:build forge_e2e

package forge_test

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/michael-freling/claude-forge/internal/forge/container"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// findProjectRoot walks up from the current directory to find the go.mod file.
func findProjectRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	require.NoError(t, err)
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		require.NotEqual(t, parent, dir, "could not find project root")
		dir = parent
	}
}

func TestForgeStart(t *testing.T) {
	// Skip if CLAUDE_CODE_OAUTH_TOKEN is not set.
	oauthToken := os.Getenv("CLAUDE_CODE_OAUTH_TOKEN")
	if oauthToken == "" {
		t.Skip("CLAUDE_CODE_OAUTH_TOKEN not set -- required for e2e test")
	}

	// Skip if Docker is not available.
	if _, err := exec.LookPath("docker"); err != nil {
		t.Skip("docker not found in PATH")
	}
	if err := exec.Command("docker", "info").Run(); err != nil {
		t.Skip("Docker daemon not available")
	}

	projectRoot := findProjectRoot(t)

	// Step 1: Build the claude-forge binary for the current platform.
	binaryPath := filepath.Join(t.TempDir(), "claude-forge")
	buildBinary := exec.Command("go", "build", "-o", binaryPath, "./cmd/claude-forge/")
	buildBinary.Dir = projectRoot
	out, err := buildBinary.CombinedOutput()
	require.NoError(t, err, "failed to build claude-forge binary: %s", out)

	// Step 2: Build the agent binary for Linux amd64 (for the Docker image).
	agentBinaryPath := filepath.Join(projectRoot, "docker", "agent", "claude-forge")
	buildAgentBinary := exec.Command("go", "build", "-o", agentBinaryPath, "./cmd/claude-forge/")
	buildAgentBinary.Dir = projectRoot
	buildAgentBinary.Env = append(os.Environ(), "CGO_ENABLED=0", "GOOS=linux", "GOARCH=amd64")
	out, err = buildAgentBinary.CombinedOutput()
	require.NoError(t, err, "failed to build agent binary: %s", out)
	t.Cleanup(func() { os.Remove(agentBinaryPath) })

	// Step 3: Build Docker images locally.
	agentImageName := "forge-e2e-agent"
	gatewayImageName := "forge-e2e-gateway"
	githubMCPImageName := "forge-e2e-github-mcp"

	buildGateway := exec.Command("docker", "build", "-t", gatewayImageName, "-f", "docker/gateway/Dockerfile", ".")
	buildGateway.Dir = projectRoot
	out, err = buildGateway.CombinedOutput()
	require.NoError(t, err, "failed to build gateway image: %s", out)

	buildAgent := exec.Command("docker", "build", "-t", agentImageName, "docker/agent/")
	buildAgent.Dir = projectRoot
	out, err = buildAgent.CombinedOutput()
	require.NoError(t, err, "failed to build agent image: %s", out)

	buildGitHubMCP := exec.Command("docker", "build", "-t", githubMCPImageName, "mcp/github-mcp/")
	buildGitHubMCP.Dir = projectRoot
	out, err = buildGitHubMCP.CombinedOutput()
	require.NoError(t, err, "failed to build github-mcp image: %s", out)

	// Step 4: Set up a temp HOME with proper config structure.
	tempHome := t.TempDir()

	// Write config.yaml pointing to locally-built images.
	configDir := filepath.Join(tempHome, ".config", "claude-forge")
	require.NoError(t, os.MkdirAll(configDir, 0o755))
	configContent := "images:\n  agent: " + agentImageName + "\n  gateway: " + gatewayImageName + "\n  github_mcp: " + githubMCPImageName + "\n"
	require.NoError(t, os.WriteFile(filepath.Join(configDir, "config.yaml"), []byte(configContent), 0o644))

	// Create .claude directory structure that the orchestrator expects to mount.
	claudeDir := filepath.Join(tempHome, ".claude")
	for _, subdir := range []string{"rules", "agents", "commands", "skills"} {
		require.NoError(t, os.MkdirAll(filepath.Join(claudeDir, subdir), 0o755))
	}
	// Create an empty CLAUDE.md at the home level (mounted read-only into the container).
	require.NoError(t, os.WriteFile(filepath.Join(tempHome, "CLAUDE.md"), []byte(""), 0o644))

	// Create .ssh directory (may be mounted by gateway).
	require.NoError(t, os.MkdirAll(filepath.Join(tempHome, ".ssh"), 0o700))
	// Create .config/gh directory (may be mounted by gateway).
	require.NoError(t, os.MkdirAll(filepath.Join(tempHome, ".config", "gh"), 0o755))

	// Step 5: Run claude-forge start -p with a simple prompt.
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	prompt := `Run these three commands and show me the output of each:
1. git log --oneline -3
2. git fetch origin main
3. go test ./internal/forge/config/...

Reply with the raw command outputs only, no other text.`
	sessionName := "e2e-session"
	cmd := exec.CommandContext(ctx, binaryPath, "start", sessionName, "-p", prompt)
	cmd.Dir = projectRoot // Must be a git repo with a GitHub remote for project.Identify.
	cmd.Env = append(os.Environ(),
		"HOME="+tempHome,
		"CLAUDE_CODE_OAUTH_TOKEN="+oauthToken,
	)

	output, err := cmd.CombinedOutput()
	outputStr := string(output)
	t.Logf("claude-forge output:\n%s", outputStr)

	// The command may return an error when docker attach ends (container exits),
	// but we still check the output. A context deadline exceeded is a real failure.
	if ctx.Err() == context.DeadlineExceeded {
		t.Fatalf("claude-forge start timed out after 3 minutes: %v\nOutput:\n%s", err, outputStr)
	}

	if strings.Contains(outputStr, "401") && strings.Contains(outputStr, "Invalid authentication credentials") {
		t.Skip("CLAUDE_CODE_OAUTH_TOKEN is expired or invalid — skipping e2e test")
	}

	// Step 6: Verify the output contains git log and gh repo view results.
	// Git log output should contain a short commit hash (7+ hex chars).
	commitHashPattern := regexp.MustCompile(`[0-9a-f]{7,}`)
	assert.Regexp(t, commitHashPattern, outputStr, "expected output to contain a commit hash from git log")

	// git fetch should succeed. Output varies: it may print nothing (already up to date),
	// show "From" lines with fetched refs, or show branch tracking info.
	// The key verification is that it doesn't error — Claude would report the error in the output.
	assert.False(t,
		strings.Contains(outputStr, "fatal:") && strings.Contains(outputStr, "fetch"),
		"expected git fetch to succeed without fatal errors")

	// go test should show passing output (non-verbose `go test` prints "ok  <pkg>").
	assert.Contains(t, outputStr, "ok  \tgithub.com/michael-freling/claude-forge", "expected output to contain ok from go test")

	// Step 7: Verify containers are cleaned up.
	dockerClient, err := container.NewClient()
	require.NoError(t, err)
	defer dockerClient.Close()

	containers, err := dockerClient.ListForgeContainers(context.Background())
	require.NoError(t, err)

	for _, c := range containers {
		t.Errorf("forge container still running after exit: %s (image=%s, status=%s)", c.Name, c.Image, c.Status)
	}

	// Step 8: Verify the session JSONL was persisted to the host so
	// `claude-forge list` can find it. Claude Code in the container
	// (cwd=/work) writes sessions under the encoded path -work/.
	projectID := strings.ReplaceAll(projectRoot, "/", "-")
	hostSessionDir := filepath.Join(tempHome, ".claude-forge", projectID, "-work")
	entries, err := os.ReadDir(hostSessionDir)
	require.NoError(t, err, "expected session dir %s to exist on host", hostSessionDir)

	var sessionFile string
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".jsonl") {
			sessionFile = e.Name()
			break
		}
	}
	require.NotEmpty(t, sessionFile, "expected at least one .jsonl session file under %s", hostSessionDir)
	t.Logf("session file persisted to host: %s", filepath.Join(hostSessionDir, sessionFile))

	// The session name should be persisted in the sidecar metadata file, keyed
	// by the same session ID and stored at the project session-dir root.
	sessionID := strings.TrimSuffix(sessionFile, ".jsonl")
	sidecarPath := filepath.Join(tempHome, ".claude-forge", projectID, sessionID+".json")
	sidecarData, err := os.ReadFile(sidecarPath)
	require.NoError(t, err, "expected sidecar metadata file at %s", sidecarPath)
	assert.Contains(t, string(sidecarData), sessionName,
		"sidecar metadata should contain the session name")

	// Step 9: `claude-forge list` must surface that session.
	// resume reads from the host, so it does not need Docker or auth.
	listCtx, listCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer listCancel()
	listCmd := exec.CommandContext(listCtx, binaryPath, "list")
	listCmd.Dir = projectRoot
	listCmd.Env = append(os.Environ(), "HOME="+tempHome)

	listOutput, listErr := listCmd.CombinedOutput()
	listOutStr := string(listOutput)
	t.Logf("claude-forge list output:\n%s", listOutStr)
	require.NoError(t, listErr, "list failed: %s", listOutStr)

	assert.NotContains(t, listOutStr, "No sessions found.",
		"list should not be empty after a session was created")
	assert.Contains(t, listOutStr, "WORKTREE",
		"list should include the WORKTREE column header")
	assert.Contains(t, listOutStr, "NAME",
		"list should include the NAME column header")
	assert.Contains(t, listOutStr, sessionName,
		"list should show the session name %s", sessionName)
	expectedID := strings.TrimSuffix(sessionFile, ".jsonl")
	assert.Contains(t, listOutStr, expectedID,
		"list should include the session ID %s", expectedID)
}

// TestForgeStart_NoGitHubAuth verifies that claude-forge fails with a clear
// error when the gateway has no GitHub authentication available, and that the
// agent container is never started.
func TestForgeStart_NoGitHubAuth(t *testing.T) {
	// Skip if Docker is not available.
	if _, err := exec.LookPath("docker"); err != nil {
		t.Skip("docker not found in PATH")
	}
	if err := exec.Command("docker", "info").Run(); err != nil {
		t.Skip("Docker daemon not available")
	}

	oauthToken := os.Getenv("CLAUDE_CODE_OAUTH_TOKEN")
	if oauthToken == "" {
		t.Skip("CLAUDE_CODE_OAUTH_TOKEN not set -- required for e2e test")
	}

	projectRoot := findProjectRoot(t)

	// Build binary
	binaryPath := filepath.Join(t.TempDir(), "claude-forge")
	buildBinary := exec.Command("go", "build", "-o", binaryPath, "./cmd/claude-forge/")
	buildBinary.Dir = projectRoot
	out, err := buildBinary.CombinedOutput()
	require.NoError(t, err, "failed to build claude-forge binary: %s", out)

	// Build Docker images
	agentBinaryPath := filepath.Join(projectRoot, "docker", "agent", "claude-forge")
	buildAgentBinary := exec.Command("go", "build", "-o", agentBinaryPath, "./cmd/claude-forge/")
	buildAgentBinary.Dir = projectRoot
	buildAgentBinary.Env = append(os.Environ(), "CGO_ENABLED=0", "GOOS=linux", "GOARCH=amd64")
	out, err = buildAgentBinary.CombinedOutput()
	require.NoError(t, err, "failed to build agent binary: %s", out)
	t.Cleanup(func() { os.Remove(agentBinaryPath) })

	agentImageName := "forge-e2e-agent-noauth"
	gatewayImageName := "forge-e2e-gateway-noauth"
	githubMCPImageName := "forge-e2e-github-mcp-noauth"

	buildGateway := exec.Command("docker", "build", "-t", gatewayImageName, "-f", "docker/gateway/Dockerfile", ".")
	buildGateway.Dir = projectRoot
	out, err = buildGateway.CombinedOutput()
	require.NoError(t, err, "failed to build gateway image: %s", out)

	buildAgent := exec.Command("docker", "build", "-t", agentImageName, "docker/agent/")
	buildAgent.Dir = projectRoot
	out, err = buildAgent.CombinedOutput()
	require.NoError(t, err, "failed to build agent image: %s", out)

	buildGitHubMCP := exec.Command("docker", "build", "-t", githubMCPImageName, "mcp/github-mcp/")
	buildGitHubMCP.Dir = projectRoot
	out, err = buildGitHubMCP.CombinedOutput()
	require.NoError(t, err, "failed to build github-mcp image: %s", out)

	// Set up temp HOME with NO GitHub auth (no GITHUB_TOKEN, no gh hosts.yml)
	tempHome := t.TempDir()

	configDir := filepath.Join(tempHome, ".config", "claude-forge")
	require.NoError(t, os.MkdirAll(configDir, 0o755))
	configContent := fmt.Sprintf("images:\n  agent: %s\n  gateway: %s\n  github_mcp: %s\n", agentImageName, gatewayImageName, githubMCPImageName)
	require.NoError(t, os.WriteFile(filepath.Join(configDir, "config.yaml"), []byte(configContent), 0o644))

	claudeDir := filepath.Join(tempHome, ".claude")
	for _, subdir := range []string{"rules", "agents", "commands", "skills"} {
		require.NoError(t, os.MkdirAll(filepath.Join(claudeDir, subdir), 0o755))
	}
	require.NoError(t, os.WriteFile(filepath.Join(tempHome, "CLAUDE.md"), []byte(""), 0o644))
	require.NoError(t, os.MkdirAll(filepath.Join(tempHome, ".ssh"), 0o700))
	// Empty .config/gh dir with no hosts.yml — gateway should fail
	require.NoError(t, os.MkdirAll(filepath.Join(tempHome, ".config", "gh"), 0o755))

	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Minute)
	defer cancel()

	// Build env without GITHUB_TOKEN so the gateway has no GitHub auth.
	var filteredEnv []string
	for _, e := range os.Environ() {
		if !strings.HasPrefix(e, "GITHUB_TOKEN=") {
			filteredEnv = append(filteredEnv, e)
		}
	}
	filteredEnv = append(filteredEnv,
		"HOME="+tempHome,
		"CLAUDE_CODE_OAUTH_TOKEN="+oauthToken,
	)

	cmd := exec.CommandContext(ctx, binaryPath, "start", "e2e-noauth", "-p", "hello")
	cmd.Dir = projectRoot
	cmd.Env = filteredEnv

	output, err := cmd.CombinedOutput()
	outputStr := string(output)
	t.Logf("claude-forge output:\n%s", outputStr)

	if strings.Contains(outputStr, "401") && strings.Contains(outputStr, "Invalid authentication credentials") {
		t.Skip("CLAUDE_CODE_OAUTH_TOKEN is expired or invalid — skipping e2e test")
	}

	require.Error(t, err, "expected error when no GitHub auth is available")
	assert.Contains(t, outputStr, "gateway container failed to start",
		"expected error message about gateway failure")

	// Verify all containers are cleaned up
	dockerClient, err := container.NewClient()
	require.NoError(t, err)
	defer dockerClient.Close()

	containers, err := dockerClient.ListForgeContainers(context.Background())
	require.NoError(t, err)

	for _, c := range containers {
		t.Errorf("forge container still running after gateway failure: %s (image=%s, status=%s)", c.Name, c.Image, c.Status)
	}
}

// TestAgentDockerInDocker verifies the agent image's Docker-in-Docker support:
// with FORGE_ENABLE_DOCKER=1 and --privileged (what the orchestrator sets when
// docker.enabled is true), the entrypoint starts an in-container dockerd that
// the non-root user can use — and that daemon is isolated from the host: it
// must not see containers running on the host daemon.
func TestAgentDockerInDocker(t *testing.T) {
	if _, err := exec.LookPath("docker"); err != nil {
		t.Skip("docker not found in PATH")
	}
	if err := exec.Command("docker", "info").Run(); err != nil {
		t.Skip("Docker daemon not available")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	projectRoot := findProjectRoot(t)

	// Build the agent binary and image (the image COPYs the binary).
	agentBinaryPath := filepath.Join(projectRoot, "docker", "agent", "claude-forge")
	buildAgentBinary := exec.Command("go", "build", "-o", agentBinaryPath, "./cmd/claude-forge/")
	buildAgentBinary.Dir = projectRoot
	buildAgentBinary.Env = append(os.Environ(), "CGO_ENABLED=0", "GOOS=linux", "GOARCH=amd64")
	out, err := buildAgentBinary.CombinedOutput()
	require.NoError(t, err, "failed to build agent binary: %s", out)
	t.Cleanup(func() { os.Remove(agentBinaryPath) })

	agentImageName := "forge-e2e-agent-dind"
	buildAgent := exec.CommandContext(ctx, "docker", "build", "-t", agentImageName, "docker/agent/")
	buildAgent.Dir = projectRoot
	out, err = buildAgent.CombinedOutput()
	require.NoError(t, err, "failed to build agent image: %s", out)

	// Start a marker container on the HOST daemon. The inner daemon must not
	// be able to see it.
	markerName := "forge-e2e-dind-host-marker"
	_ = exec.Command("docker", "rm", "-f", markerName).Run()
	out, err = exec.CommandContext(ctx, "docker", "run", "-d", "--name", markerName, "alpine:latest", "sleep", "300").CombinedOutput()
	require.NoError(t, err, "failed to start marker container: %s", out)
	t.Cleanup(func() {
		_ = exec.Command("docker", "rm", "-f", markerName).Run()
	})

	// Run the agent container the way the orchestrator does when docker.enabled
	// is true, but with a script instead of claude: wait for the inner dockerd,
	// list containers (isolation check), then run one (functionality check).
	script := `set -e
for _ in $(seq 1 30); do docker info >/dev/null 2>&1 && break; sleep 1; done
docker info >/dev/null 2>&1 || { echo "inner dockerd never came up"; exit 1; }
echo "INNER_PS_START"
docker ps -a
echo "INNER_PS_END"
docker run --rm hello-world
`
	containerName := "forge-e2e-dind-agent"
	_ = exec.Command("docker", "rm", "-f", "-v", containerName).Run()
	t.Cleanup(func() {
		_ = exec.Command("docker", "rm", "-f", "-v", containerName).Run()
	})

	// -v /var/lib/docker mirrors the orchestrator's anonymous volume: the inner
	// dockerd cannot layer overlayfs on the agent's own overlayfs root.
	runCmd := exec.CommandContext(ctx, "docker", "run",
		"--name", containerName,
		"--privileged",
		"-v", "/var/lib/docker",
		"-e", "FORGE_ENABLE_DOCKER=1",
		agentImageName,
		"bash", "-c", script,
	)
	out, err = runCmd.CombinedOutput()
	logs := string(out)
	require.NoError(t, err, "agent DinD script failed:\n%s", logs)

	// Functionality: the inner daemon ran a container.
	assert.Contains(t, logs, "Hello from Docker!", "expected hello-world to run on the inner daemon:\n%s", logs)

	// Isolation: the inner daemon sees none of the host daemon's containers.
	assert.NotContains(t, logs, markerName, "inner daemon must not see host containers:\n%s", logs)
	assert.NotContains(t, logs, "alpine", "inner daemon must not see host containers:\n%s", logs)
}

// buildForge builds the claude-forge binary and returns its path.
func buildForge(t *testing.T) string {
	t.Helper()
	projectRoot := findProjectRoot(t)
	binaryPath := filepath.Join(t.TempDir(), "claude-forge")
	buildBinary := exec.Command("go", "build", "-o", binaryPath, "./cmd/claude-forge/")
	buildBinary.Dir = projectRoot
	out, err := buildBinary.CombinedOutput()
	require.NoError(t, err, "failed to build claude-forge binary: %s", out)
	return binaryPath
}

// writeTestSession creates a JSONL session file matching Claude Code's real format.
func writeTestSession(t *testing.T, path, timestamp, message string) {
	t.Helper()
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
	content := fmt.Sprintf(
		"{\"type\":\"permission-mode\",\"permissionMode\":\"bypassPermissions\"}\n"+
			"{\"type\":\"user\",\"message\":{\"role\":\"user\",\"content\":\"%s\"},\"timestamp\":\"%s\"}\n",
		message, timestamp,
	)
	require.NoError(t, os.WriteFile(path, []byte(content), 0o644))
}

// TestResumeList_NonWorktreeSession verifies that `list` shows
// non-worktree sessions with an empty WORKTREE column.
func TestResumeList_NonWorktreeSession(t *testing.T) {
	binaryPath := buildForge(t)
	projectRoot := findProjectRoot(t)

	tempHome := t.TempDir()
	projectID := strings.ReplaceAll(projectRoot, "/", "-")

	writeTestSession(t,
		filepath.Join(tempHome, ".claude-forge", projectID, "-work", "non-wt-session.jsonl"),
		"2026-05-20T10:00:00Z", "regular session")

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, binaryPath, "list")
	cmd.Dir = projectRoot
	cmd.Env = append(os.Environ(), "HOME="+tempHome)

	output, err := cmd.CombinedOutput()
	outputStr := string(output)
	t.Logf("list output:\n%s", outputStr)
	require.NoError(t, err, "list failed: %s", outputStr)

	assert.Contains(t, outputStr, "SESSION ID")
	assert.Contains(t, outputStr, "WORKTREE")
	assert.Contains(t, outputStr, "FIRST MESSAGE")
	assert.Contains(t, outputStr, "non-wt-session")
	assert.Contains(t, outputStr, "regular session")

	// Split into lines and verify the data row has no worktree name
	lines := strings.Split(strings.TrimSpace(outputStr), "\n")
	require.GreaterOrEqual(t, len(lines), 2, "expected header + at least one data line")
	dataLine := lines[1]
	assert.Contains(t, dataLine, "non-wt-session")
	// The worktree column should be empty for non-worktree sessions.
	// Between the timestamp and "regular session" there should be no worktree name.
	assert.NotContains(t, dataLine, "worktrees")
}

// TestResumeList_WorktreeSession verifies that `list` shows
// worktree sessions with the worktree name in the WORKTREE column.
func TestResumeList_WorktreeSession(t *testing.T) {
	binaryPath := buildForge(t)
	projectRoot := findProjectRoot(t)

	tempHome := t.TempDir()
	projectID := strings.ReplaceAll(projectRoot, "/", "-")

	writeTestSession(t,
		filepath.Join(tempHome, ".claude-forge", projectID, "-work", "main-session.jsonl"),
		"2026-05-20T10:00:00Z", "main workspace")

	writeTestSession(t,
		filepath.Join(tempHome, ".claude-forge", projectID, "-work--claude-worktrees-my-feature", "wt-session.jsonl"),
		"2026-05-20T11:00:00Z", "worktree task")

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, binaryPath, "list")
	cmd.Dir = projectRoot
	cmd.Env = append(os.Environ(), "HOME="+tempHome)

	output, err := cmd.CombinedOutput()
	outputStr := string(output)
	t.Logf("list output:\n%s", outputStr)
	require.NoError(t, err, "list failed: %s", outputStr)

	assert.Contains(t, outputStr, "WORKTREE")
	assert.Contains(t, outputStr, "wt-session")
	assert.Contains(t, outputStr, "my-feature")
	assert.Contains(t, outputStr, "main-session")

	// Verify both sessions are listed: worktree session first (more recent)
	lines := strings.Split(strings.TrimSpace(outputStr), "\n")
	require.GreaterOrEqual(t, len(lines), 3, "expected header + 2 data lines")

	// First data line: worktree session (newer timestamp)
	assert.Contains(t, lines[1], "wt-session")
	assert.Contains(t, lines[1], "my-feature")

	// Second data line: main session (no worktree)
	assert.Contains(t, lines[2], "main-session")
}

// TestResumeList_MixedSessions verifies correct display when there are
// sessions from both the main workspace and multiple worktrees.
func TestResumeList_MixedSessions(t *testing.T) {
	binaryPath := buildForge(t)
	projectRoot := findProjectRoot(t)

	tempHome := t.TempDir()
	projectID := strings.ReplaceAll(projectRoot, "/", "-")
	baseDir := filepath.Join(tempHome, ".claude-forge", projectID)

	writeTestSession(t,
		filepath.Join(baseDir, "-work", "session-a.jsonl"),
		"2026-05-20T09:00:00Z", "main work")

	writeTestSession(t,
		filepath.Join(baseDir, "-work--claude-worktrees-feature-auth", "session-b.jsonl"),
		"2026-05-20T10:00:00Z", "auth feature")

	writeTestSession(t,
		filepath.Join(baseDir, "-work--claude-worktrees-bugfix-login", "session-c.jsonl"),
		"2026-05-20T11:00:00Z", "login bugfix")

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, binaryPath, "list")
	cmd.Dir = projectRoot
	cmd.Env = append(os.Environ(), "HOME="+tempHome)

	output, err := cmd.CombinedOutput()
	outputStr := string(output)
	t.Logf("list output:\n%s", outputStr)
	require.NoError(t, err, "list failed: %s", outputStr)

	lines := strings.Split(strings.TrimSpace(outputStr), "\n")
	require.GreaterOrEqual(t, len(lines), 4, "expected header + 3 data lines")

	// Most recent first
	assert.Contains(t, lines[1], "session-c")
	assert.Contains(t, lines[1], "bugfix-login")

	assert.Contains(t, lines[2], "session-b")
	assert.Contains(t, lines[2], "feature-auth")

	assert.Contains(t, lines[3], "session-a")
}

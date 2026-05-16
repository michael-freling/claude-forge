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

	"github.com/michael-freling/claude-code-tools/internal/forge/container"
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

	buildGateway := exec.Command("docker", "build", "-t", gatewayImageName, "-f", "docker/gateway/Dockerfile", ".")
	buildGateway.Dir = projectRoot
	out, err = buildGateway.CombinedOutput()
	require.NoError(t, err, "failed to build gateway image: %s", out)

	buildAgent := exec.Command("docker", "build", "-t", agentImageName, "docker/agent/")
	buildAgent.Dir = projectRoot
	out, err = buildAgent.CombinedOutput()
	require.NoError(t, err, "failed to build agent image: %s", out)

	// Step 4: Set up a temp HOME with proper config structure.
	tempHome := t.TempDir()

	// Write config.yaml pointing to locally-built images.
	configDir := filepath.Join(tempHome, ".config", "claude-forge")
	require.NoError(t, os.MkdirAll(configDir, 0o755))
	configContent := "images:\n  agent: " + agentImageName + "\n  gateway: " + gatewayImageName + "\n"
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

	prompt := `Run these four commands and show me the output of each:
1. git log --oneline -3
2. git fetch origin main
3. gh repo view --json name,owner
4. go test ./internal/forge/config/...

Reply with the raw command outputs only, no other text.`
	cmd := exec.CommandContext(ctx, binaryPath, "start", "-p", prompt)
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

	// gh repo view should return the repo name.
	assert.Contains(t, outputStr, "claude-code-tools", "expected output to contain repo name from gh repo view")

	// go test should show passing output (non-verbose `go test` prints "ok  <pkg>").
	assert.Contains(t, outputStr, "ok  \tgithub.com/michael-freling/claude-code-tools", "expected output to contain ok from go test")

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
	// `claude-forge resume --list` can find it. Claude Code in the container
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

	// Step 9: `claude-forge resume --list` must surface that session.
	// resume reads from the host, so it does not need Docker or auth.
	listCtx, listCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer listCancel()
	listCmd := exec.CommandContext(listCtx, binaryPath, "resume", "--list")
	listCmd.Dir = projectRoot
	listCmd.Env = append(os.Environ(), "HOME="+tempHome)

	listOutput, listErr := listCmd.CombinedOutput()
	listOutStr := string(listOutput)
	t.Logf("claude-forge resume --list output:\n%s", listOutStr)
	require.NoError(t, listErr, "resume --list failed: %s", listOutStr)

	assert.NotContains(t, listOutStr, "No sessions found.",
		"resume --list should not be empty after a session was created")
	expectedID := strings.TrimSuffix(sessionFile, ".jsonl")
	assert.Contains(t, listOutStr, expectedID,
		"resume --list should include the session ID %s", expectedID)
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

	buildGateway := exec.Command("docker", "build", "-t", gatewayImageName, "-f", "docker/gateway/Dockerfile", ".")
	buildGateway.Dir = projectRoot
	out, err = buildGateway.CombinedOutput()
	require.NoError(t, err, "failed to build gateway image: %s", out)

	buildAgent := exec.Command("docker", "build", "-t", agentImageName, "docker/agent/")
	buildAgent.Dir = projectRoot
	out, err = buildAgent.CombinedOutput()
	require.NoError(t, err, "failed to build agent image: %s", out)

	// Set up temp HOME with NO GitHub auth (no GITHUB_TOKEN, no gh hosts.yml)
	tempHome := t.TempDir()

	configDir := filepath.Join(tempHome, ".config", "claude-forge")
	require.NoError(t, os.MkdirAll(configDir, 0o755))
	configContent := fmt.Sprintf("images:\n  agent: %s\n  gateway: %s\n", agentImageName, gatewayImageName)
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

	cmd := exec.CommandContext(ctx, binaryPath, "start", "-p", "hello")
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

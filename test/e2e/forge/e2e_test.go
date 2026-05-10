//go:build forge_e2e

package forge_test

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
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

	prompt := `Run these two commands and show me the output of each:
1. git log --oneline -3
2. gh repo view --json name,owner

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

	// Step 6: Verify the output contains git log and gh repo view results.
	// Git log output should contain a short commit hash (7+ hex chars).
	commitHashPattern := regexp.MustCompile(`[0-9a-f]{7,}`)
	assert.Regexp(t, commitHashPattern, outputStr, "expected output to contain a commit hash from git log")

	// gh repo view should return the repo name.
	assert.Contains(t, outputStr, "claude-code-tools", "expected output to contain repo name from gh repo view")

	// Step 7: Verify containers are cleaned up.
	dockerClient, err := container.NewClient()
	require.NoError(t, err)
	defer dockerClient.Close()

	containers, err := dockerClient.ListForgeContainers(context.Background())
	require.NoError(t, err)

	for _, c := range containers {
		t.Errorf("forge container still running after exit: %s (image=%s, status=%s)", c.Name, c.Image, c.Status)
	}
}

//go:build forge_e2e

package forge_test

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/michael-freling/claude-forge/internal/forge/layout"
	"github.com/michael-freling/claude-forge/internal/forge/openshell"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// continueProbeDriver runs in the agent container. It leaves only a claude -p
// transcript in the workspace, checks that a plain interactive
// claude --continue cannot resume it, then runs the continue script connect
// uses under a pty until Claude answers PROMPT or the script exits.
const continueProbeDriver = `set -u
printf '%s\n' "$CLAUDE_JSON" > ~/.claude.json
printf '%s\n' '{"skipDangerousModePermissionPrompt": true}' > ~/.claude/settings.json
claude -p "Say ok." || { echo "SETUP_EXIT=$?"; exit 1; }

echo "=== PLAIN CONTINUE ==="
timeout 60 script -qfec 'claude --continue; echo "PLAIN_EXIT=$?"' /dev/null < <(sleep 120)

echo "=== CONNECT ==="
out=$(mktemp)
script -qfec 'bash -c "$CONTINUE_SCRIPT" claude --dangerously-skip-permissions "$PROMPT"; echo "CONNECT_EXIT=$?"' "$out" < <(sleep 300) > /dev/null &
pid=$!
for _ in $(seq 120); do
	grep -q -e "$ANSWER" -e "CONNECT_EXIT=[0-9]" "$out" && break
	sleep 1
done
kill "$pid" 2>/dev/null
cat "$out"
`

// terminalControl matches the control sequences and characters Claude Code's
// TUI prints; cursor moves also stand in for the spaces between words.
var terminalControl = regexp.MustCompile(`\x1b(\[[0-9;?<>=]*[ -/]*[@-~]|\][^\x07]*\x07|[()][0-9A-Za-z]|[0-9A-Za-z=<>@\\^_-])|[\x00-\x08\x0e-\x1f\x7f]`)

// connectExit matches the exit status the probe prints when the continue
// script ends (the typescript header holds the unexpanded command).
var connectExit = regexp.MustCompile(`CONNECT_EXIT=[0-9]`)

// skipUnlessCI skips the test locally but fails it under GitHub Actions,
// where a missing prerequisite must block the merge instead of passing as a
// skip.
func skipUnlessCI(t *testing.T, reason string) {
	t.Helper()
	if os.Getenv("GITHUB_ACTIONS") == "true" {
		t.Fatal(reason + " (this test must not be skipped in CI)")
	}
	t.Skip(reason)
}

// TestOpenShellContinueFallback reproduces `openshell connect` in a sandbox
// whose only transcript comes from claude -p: claude --continue fails there
// with "No conversation found to continue", and the continue script must
// start a new conversation instead of failing.
func TestOpenShellContinueFallback(t *testing.T) {
	oauthToken := os.Getenv("CLAUDE_CODE_OAUTH_TOKEN")
	if oauthToken == "" {
		skipUnlessCI(t, "CLAUDE_CODE_OAUTH_TOKEN not set -- required for e2e test; in CI the CLAUDE_CODE_OAUTH_TOKEN repository secret is missing and must be added")
	}
	if _, err := exec.LookPath("docker"); err != nil {
		skipUnlessCI(t, "docker not found in PATH")
	}
	if err := exec.Command("docker", "info").Run(); err != nil {
		skipUnlessCI(t, "Docker daemon not available")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
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

	agentImageName := "forge-e2e-agent-continue"
	buildAgent := exec.CommandContext(ctx, "docker", "build", "-t", agentImageName, "docker/agent/")
	buildAgent.Dir = projectRoot
	out, err = buildAgent.CombinedOutput()
	require.NoError(t, err, "failed to build agent image: %s", out)

	// The user config a sandbox starts with (see openshell stageHome), so
	// interactive Claude Code skips onboarding and the trust dialog.
	claudeJSON, err := json.Marshal(map[string]any{
		"hasCompletedOnboarding": true,
		"theme":                  "dark",
		"projects":               map[string]any{layout.Workspace: map[string]any{"hasTrustDialogAccepted": true}},
	})
	require.NoError(t, err)

	const answer = "5555"
	args := []string{"run", "--rm",
		"-w", layout.Workspace,
		"-e", "DISABLE_AUTOUPDATER=1",
		"-e", "CLAUDE_CODE_OAUTH_TOKEN=" + oauthToken,
		"-e", "CLAUDE_JSON=" + string(claudeJSON),
		"-e", "CONTINUE_SCRIPT=" + openshell.ContinueScript(),
		"-e", "PROMPT=What is 1234 + 4321? Reply with only the number.",
		"-e", "ANSWER=" + answer,
		agentImageName, "bash", "-c", continueProbeDriver,
	}
	out, err = exec.CommandContext(ctx, "docker", args...).CombinedOutput()
	logs := strings.Join(strings.Fields(terminalControl.ReplaceAllString(string(out), " ")), " ")
	t.Logf("container output (control sequences stripped):\n%s", logs)

	if strings.Contains(logs, "401") && strings.Contains(logs, "Invalid authentication credentials") {
		skipUnlessCI(t, "CLAUDE_CODE_OAUTH_TOKEN is expired or invalid -- in CI the CLAUDE_CODE_OAUTH_TOKEN repository secret must be refreshed")
	}
	require.NoError(t, err, "probe container failed:\n%s", logs)

	plain, connect, found := strings.Cut(logs, "=== CONNECT ===")
	require.True(t, found, "the probe did not reach the connect step:\n%s", logs)
	// The scenario must still reproduce, or the fallback below is untested.
	assert.Contains(t, plain, "No conversation found to continue",
		"claude --continue resumed a claude -p transcript; the scenario no longer reproduces")
	assert.Contains(t, plain, "PLAIN_EXIT=1")

	assert.NotRegexp(t, connectExit, connect, "the continue script exited instead of starting Claude Code")
	assert.Contains(t, connect, answer, "Claude Code did not start a new conversation and answer the prompt")
}

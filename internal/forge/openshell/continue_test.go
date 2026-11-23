package openshell

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/michael-freling/claude-forge/internal/forge/layout"
	"github.com/michael-freling/claude-forge/internal/forge/openshell/spec"
)

// stubClaude logs each call as "claude [arg]... token=<set|unset>" and, for
// --continue, waits STUB_CONTINUE_SLEEP seconds and exits STUB_CONTINUE_EXIT;
// other calls exit STUB_EXIT.
const stubClaude = `#!/bin/bash
token=unset
[ -z "${CLAUDE_CODE_OAUTH_TOKEN+x}" ] || token=set
{ printf 'claude'; printf ' [%s]' "$@"; printf ' token=%s\n' "$token"; } >> "$STUB_LOG"
if [ "$1" = --continue ]; then
	sleep "${STUB_CONTINUE_SLEEP:-0}"
	[ "${STUB_CONTINUE_EXIT:-0}" -eq 0 ] || echo "No conversation found to continue" >&2
	exit "${STUB_CONTINUE_EXIT:-0}"
fi
exit "${STUB_EXIT:-0}"
`

// TestContinueScript runs the script connect runs in a sandbox against a stub
// claude, so the fallback logic is exercised by a real bash.
func TestContinueScript(t *testing.T) {
	if _, err := exec.LookPath("bash"); err != nil {
		t.Skip("bash not found in PATH")
	}
	// The long-running case must outlast the fallback window, so it uses a
	// short one: whole seconds from $SECONDS make 2 the smallest window a
	// fast failure (under a second) always falls inside.
	const shortWindow = 2
	args := []string{"--dangerously-skip-permissions", "--remote-control", "name"}
	const passedArgs = " [--dangerously-skip-permissions] [--remote-control] [name]"

	tests := []struct {
		name         string
		script       string
		sessionFile  bool
		fullLogin    bool
		stubEnv      []string
		wantCalls    []string
		wantExitCode int
	}{
		{
			name:      "no session files starts a new conversation",
			script:    continueScript,
			wantCalls: []string{"claude" + passedArgs + " token=set"},
		},
		{
			name:        "continue succeeds",
			script:      continueScript,
			sessionFile: true,
			wantCalls:   []string{"claude [--continue]" + passedArgs + " token=set"},
		},
		{
			name:        "continue fails fast falls back to a new conversation",
			script:      continueScript,
			sessionFile: true,
			stubEnv:     []string{"STUB_CONTINUE_EXIT=1"},
			wantCalls: []string{
				"claude [--continue]" + passedArgs + " token=set",
				"claude" + passedArgs + " token=set",
			},
		},
		{
			name:         "fallback exit status propagates",
			script:       continueScript,
			sessionFile:  true,
			stubEnv:      []string{"STUB_CONTINUE_EXIT=1", "STUB_EXIT=7"},
			wantExitCode: 7,
			wantCalls: []string{
				"claude [--continue]" + passedArgs + " token=set",
				"claude" + passedArgs + " token=set",
			},
		},
		{
			name:         "continue fails after the window keeps its status",
			script:       continueScriptWithin(shortWindow),
			sessionFile:  true,
			stubEnv:      []string{"STUB_CONTINUE_EXIT=3", "STUB_CONTINUE_SLEEP=" + strconv.Itoa(shortWindow)},
			wantExitCode: 3,
			wantCalls:    []string{"claude [--continue]" + passedArgs + " token=set"},
		},
		{
			name:        "full login unsets the token on both calls",
			script:      continueScript,
			sessionFile: true,
			fullLogin:   true,
			stubEnv:     []string{"STUB_CONTINUE_EXIT=1"},
			wantCalls: []string{
				"claude [--continue]" + passedArgs + " token=unset",
				"claude" + passedArgs + " token=unset",
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			binDir := filepath.Join(dir, "bin")
			require.NoError(t, os.MkdirAll(binDir, 0o755))
			require.NoError(t, os.WriteFile(filepath.Join(binDir, "claude"), []byte(stubClaude), 0o755))
			home := filepath.Join(dir, "home")
			bucket := filepath.Join(home, ".claude", "projects", layout.SessionSubdir)
			require.NoError(t, os.MkdirAll(bucket, 0o755))
			if tt.sessionFile {
				require.NoError(t, os.WriteFile(filepath.Join(bucket, "s.jsonl"), []byte("{}\n"), 0o644))
			}
			logPath := filepath.Join(dir, "calls.log")

			command := append([]string{"bash", "-c", tt.script, "claude"}, args...)
			if tt.fullLogin {
				command = append([]string{"env", "-u", spec.EnvClaudeOAuthToken}, command...)
			}
			cmd := exec.Command(command[0], command[1:]...)
			cmd.Env = append([]string{
				"PATH=" + binDir + string(os.PathListSeparator) + os.Getenv("PATH"),
				"HOME=" + home,
				"STUB_LOG=" + logPath,
				spec.EnvClaudeOAuthToken + "=token",
			}, tt.stubEnv...)
			out, err := cmd.CombinedOutput()

			gotExitCode := 0
			var exitErr *exec.ExitError
			if errors.As(err, &exitErr) {
				gotExitCode = exitErr.ExitCode()
			} else {
				require.NoError(t, err, "output: %s", out)
			}
			assert.Equal(t, tt.wantExitCode, gotExitCode, "output: %s", out)
			calls, err := os.ReadFile(logPath)
			require.NoError(t, err)
			assert.Equal(t, tt.wantCalls, strings.Split(strings.TrimSuffix(string(calls), "\n"), "\n"))
		})
	}
}

func TestContinueScriptAccessor(t *testing.T) {
	assert.Equal(t, continueScript, ContinueScript())
	assert.Equal(t, continueScriptWithin(continueFallbackSeconds), ContinueScript())
	assert.Contains(t, ContinueScript(), "~/.claude/projects/"+layout.SessionSubdir+"/*.jsonl")
}

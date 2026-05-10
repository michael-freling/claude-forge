package main

import (
	"bytes"
	"io"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewRootCmd(t *testing.T) {
	cmd := newRootCmd()

	assert.Equal(t, "claude-forge", cmd.Use)

	expectedSubcommands := []string{
		"start", "resume", "stop", "status",
		"build", "auth", "version", "gateway", "forge-gh",
	}

	subcommandNames := make(map[string]bool)
	for _, sub := range cmd.Commands() {
		subcommandNames[sub.Use] = true
	}

	for _, expected := range expectedSubcommands {
		found := false
		for _, sub := range cmd.Commands() {
			// cmd.Use may include argument specs like "resume [session-id]",
			// so match on the first word.
			name := sub.Name()
			if name == expected {
				found = true
				break
			}
		}
		assert.True(t, found, "expected subcommand %q not found", expected)
	}
}

func TestNewStartCmd(t *testing.T) {
	cmd := newStartCmd()

	assert.Equal(t, "start", cmd.Use)

	worktreeFlag := cmd.Flags().Lookup("worktree")
	require.NotNil(t, worktreeFlag)
	assert.Equal(t, "false", worktreeFlag.DefValue)

	noSkipFlag := cmd.Flags().Lookup("no-skip-permissions")
	require.NotNil(t, noSkipFlag)
	assert.Equal(t, "false", noSkipFlag.DefValue)

	promptFlag := cmd.Flags().Lookup("prompt")
	require.NotNil(t, promptFlag)
	assert.Equal(t, "", promptFlag.DefValue)
	assert.Equal(t, "p", promptFlag.Shorthand)
}

func TestNewResumeCmd(t *testing.T) {
	cmd := newResumeCmd()

	assert.Equal(t, "resume [session-id]", cmd.Use)

	listFlag := cmd.Flags().Lookup("list")
	require.NotNil(t, listFlag)
	assert.Equal(t, "false", listFlag.DefValue)

	// Verify MaximumNArgs(1) is set by checking the Args validator.
	require.NotNil(t, cmd.Args)
}

func TestNewGatewayCmd(t *testing.T) {
	cmd := newGatewayCmd()

	assert.Equal(t, "gateway", cmd.Use)

	ownerFlag := cmd.Flags().Lookup("owner")
	require.NotNil(t, ownerFlag)
	assert.Equal(t, "", ownerFlag.DefValue)

	repoFlag := cmd.Flags().Lookup("repo")
	require.NotNil(t, repoFlag)
	assert.Equal(t, "", repoFlag.DefValue)

	proxyAddrFlag := cmd.Flags().Lookup("proxy-addr")
	require.NotNil(t, proxyAddrFlag)
	assert.Equal(t, ":8080", proxyAddrFlag.DefValue)

	apiAddrFlag := cmd.Flags().Lookup("api-addr")
	require.NotNil(t, apiAddrFlag)
	assert.Equal(t, ":8083", apiAddrFlag.DefValue)
}

func TestNewVersionCmd(t *testing.T) {
	cmd := newVersionCmd()

	assert.Equal(t, "version", cmd.Use)

	// The version command uses fmt.Printf (writes to os.Stdout), so we
	// capture it by redirecting stdout via a pipe.
	oldStdout := os.Stdout
	r, w, err := os.Pipe()
	require.NoError(t, err)

	os.Stdout = w
	execErr := cmd.Execute()
	w.Close()
	os.Stdout = oldStdout

	require.NoError(t, execErr)

	var buf bytes.Buffer
	_, err = io.Copy(&buf, r)
	require.NoError(t, err)

	assert.Contains(t, buf.String(), "claude-forge version")
}

func TestNewStopCmd(t *testing.T) {
	cmd := newStopCmd()
	assert.Equal(t, "stop", cmd.Use)
}

func TestNewStatusCmd(t *testing.T) {
	cmd := newStatusCmd()
	assert.Equal(t, "status", cmd.Use)
}

func TestNewBuildCmd(t *testing.T) {
	cmd := newBuildCmd()
	assert.Equal(t, "build", cmd.Use)
}

func TestNewAuthCmd(t *testing.T) {
	cmd := newAuthCmd()
	assert.Equal(t, "auth", cmd.Use)
}

func TestNewForgeGHCmd(t *testing.T) {
	cmd := newForgeGHCmd()

	assert.Equal(t, "forge-gh", cmd.Use)
	assert.True(t, cmd.DisableFlagParsing)
}

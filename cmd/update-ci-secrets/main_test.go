package main

import (
	"bytes"
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fakeUpdater struct {
	masked        string
	err           error
	gotToken      string
	gotClaudeDir  string
	updateCalled  bool
	fromCredsCall bool
}

func (f *fakeUpdater) Update(_ context.Context, token string) (string, error) {
	f.updateCalled = true
	f.gotToken = token
	return f.masked, f.err
}

func (f *fakeUpdater) UpdateFromCredentials(_ context.Context, claudeDir string) (string, error) {
	f.fromCredsCall = true
	f.gotClaudeDir = claudeDir
	return f.masked, f.err
}

// withFakeUpdater swaps newUpdater for the duration of a test.
func withFakeUpdater(t *testing.T, f *fakeUpdater) *string {
	t.Helper()
	var gotRepo string
	orig := newUpdater
	newUpdater = func(repo string) updater {
		gotRepo = repo
		return f
	}
	t.Cleanup(func() { newUpdater = orig })
	return &gotRepo
}

func runCmd(args ...string) (string, error) {
	cmd := newRootCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs(args)
	err := cmd.Execute()
	return out.String(), err
}

func TestRun_DefaultsToCredentials(t *testing.T) {
	f := &fakeUpdater{masked: "sk-ant-o...wxyz"}
	withFakeUpdater(t, f)

	out, err := runCmd()
	require.NoError(t, err)
	assert.True(t, f.fromCredsCall)
	assert.False(t, f.updateCalled)
	assert.Contains(t, f.gotClaudeDir, ".claude")
	assert.Contains(t, out, "Set CLAUDE_CODE_OAUTH_TOKEN")
}

func TestRun_FromCredentialsDisabledWithoutToken(t *testing.T) {
	_, err := runCmd("--from-credentials=false")
	assert.ErrorContains(t, err, "--oauth-token or enable --from-credentials")
}

func TestRun_FromCredentialsDefaultValue(t *testing.T) {
	cmd := newRootCmd()
	flag := cmd.Flags().Lookup("from-credentials")
	assert.NotNil(t, flag)
	assert.Equal(t, "true", flag.DefValue)
}

func TestRun_MutuallyExclusiveFlags(t *testing.T) {
	_, err := runCmd("--oauth-token", "tok", "--from-credentials")
	assert.ErrorContains(t, err, "mutually exclusive")
}

func TestRun_OAuthTokenWithDefaultCredentialsFlag(t *testing.T) {
	// --oauth-token alone must work even though --from-credentials now
	// defaults to true (it's only a conflict when explicitly set).
	f := &fakeUpdater{masked: "sk-ant-o...abcd"}
	withFakeUpdater(t, f)

	_, err := runCmd("--oauth-token", "sk-ant-oat-token")
	require.NoError(t, err)
	assert.True(t, f.updateCalled)
	assert.False(t, f.fromCredsCall)
	assert.Equal(t, "sk-ant-oat-token", f.gotToken)
}

func TestRun_RepoFlagDefaultsEmpty(t *testing.T) {
	cmd := newRootCmd()
	flag := cmd.Flags().Lookup("repo")
	assert.NotNil(t, flag)
	assert.Equal(t, "", flag.DefValue)
}

func TestRun_NoRepoUsesCurrentDirectory(t *testing.T) {
	f := &fakeUpdater{masked: "sk-ant-o...abcd"}
	gotRepo := withFakeUpdater(t, f)

	out, err := runCmd("--oauth-token", "sk-ant-oat-token")
	require.NoError(t, err)
	assert.Equal(t, "", *gotRepo)
	assert.Contains(t, out, "on the current repository")
}

func TestRun_OAuthTokenSuccess(t *testing.T) {
	f := &fakeUpdater{masked: "sk-ant-o...abcd"}
	gotRepo := withFakeUpdater(t, f)

	out, err := runCmd("--oauth-token", "sk-ant-oat-token", "--repo", "owner/repo")
	require.NoError(t, err)
	assert.True(t, f.updateCalled)
	assert.Equal(t, "sk-ant-oat-token", f.gotToken)
	assert.Equal(t, "owner/repo", *gotRepo)
	assert.Contains(t, out, "Set CLAUDE_CODE_OAUTH_TOKEN (sk-ant-o...abcd) on owner/repo")
}

func TestRun_FromCredentialsSuccess(t *testing.T) {
	f := &fakeUpdater{masked: "sk-ant-o...wxyz"}
	withFakeUpdater(t, f)

	out, err := runCmd("--from-credentials")
	require.NoError(t, err)
	assert.True(t, f.fromCredsCall)
	assert.Contains(t, f.gotClaudeDir, ".claude")
	assert.Contains(t, out, "Set CLAUDE_CODE_OAUTH_TOKEN")
}

func TestRun_UpdaterError(t *testing.T) {
	f := &fakeUpdater{err: errors.New("gh boom")}
	withFakeUpdater(t, f)

	_, err := runCmd("--oauth-token", "tok")
	assert.ErrorContains(t, err, "gh boom")
}

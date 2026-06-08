package cisecrets

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewUpdater_DefaultRepo(t *testing.T) {
	assert.Equal(t, DefaultRepo, NewUpdater("").Repo)
	assert.Equal(t, "owner/custom", NewUpdater("owner/custom").Repo)
}

func TestMask(t *testing.T) {
	assert.Equal(t, "***", mask("short"))
	assert.Equal(t, "***", mask("exactly12chr")) // 12 chars -> masked entirely
	assert.Equal(t, "abcdefgh...wxyz", mask("abcdefghijklmnopqrstuvwxyz"))
}

func TestUpdate(t *testing.T) {
	t.Run("empty token", func(t *testing.T) {
		u := &Updater{Repo: DefaultRepo, setter: func(context.Context, string, string, string) error {
			t.Fatal("setter must not run for empty token")
			return nil
		}}
		_, err := u.Update(context.Background(), "   ")
		assert.ErrorContains(t, err, "empty token")
	})

	t.Run("success trims and masks", func(t *testing.T) {
		var gotRepo, gotName, gotValue string
		u := &Updater{Repo: "owner/repo", setter: func(_ context.Context, repo, name, value string) error {
			gotRepo, gotName, gotValue = repo, name, value
			return nil
		}}
		masked, err := u.Update(context.Background(), "  sk-ant-oat-1234567890abcd  ")
		require.NoError(t, err)
		assert.Equal(t, "owner/repo", gotRepo)
		assert.Equal(t, SecretName, gotName)
		assert.Equal(t, "sk-ant-oat-1234567890abcd", gotValue)
		assert.Equal(t, "sk-ant-o...abcd", masked)
	})

	t.Run("setter error is wrapped", func(t *testing.T) {
		sentinel := errors.New("gh failed")
		u := &Updater{Repo: "owner/repo", setter: func(context.Context, string, string, string) error {
			return sentinel
		}}
		_, err := u.Update(context.Background(), "sk-ant-oat-1234567890abcd")
		assert.ErrorIs(t, err, sentinel)
		assert.ErrorContains(t, err, SecretName)
	})
}

func TestUpdateFromCredentials(t *testing.T) {
	// auth.Resolve checks env vars first; clear them so the file is used.
	t.Setenv("ANTHROPIC_API_KEY", "")
	t.Setenv("CLAUDE_CODE_OAUTH_TOKEN", "")

	t.Run("oauth token from file", func(t *testing.T) {
		dir := t.TempDir()
		writeCreds(t, dir, `{"claudeAiOauth":{"accessToken":"sk-ant-oat-abcdefgh1234"}}`)

		var gotValue string
		u := &Updater{Repo: DefaultRepo, setter: func(_ context.Context, _, _, value string) error {
			gotValue = value
			return nil
		}}
		masked, err := u.UpdateFromCredentials(context.Background(), dir)
		require.NoError(t, err)
		assert.Equal(t, "sk-ant-oat-abcdefgh1234", gotValue)
		assert.Equal(t, "sk-ant-o...1234", masked)
	})

	t.Run("api key rejected", func(t *testing.T) {
		t.Setenv("ANTHROPIC_API_KEY", "sk-ant-api-key")
		u := &Updater{Repo: DefaultRepo, setter: func(context.Context, string, string, string) error {
			t.Fatal("setter must not run for api_key credential")
			return nil
		}}
		_, err := u.UpdateFromCredentials(context.Background(), t.TempDir())
		assert.ErrorContains(t, err, "not an OAuth token")
	})

	t.Run("resolve error", func(t *testing.T) {
		u := &Updater{Repo: DefaultRepo, setter: func(context.Context, string, string, string) error { return nil }}
		_, err := u.UpdateFromCredentials(context.Background(), filepath.Join(t.TempDir(), "missing"))
		assert.Error(t, err)
	})
}

func TestGhSecretSet_CommandError(t *testing.T) {
	// Point PATH at an empty dir so gh cannot be found, exercising the error path.
	t.Setenv("PATH", t.TempDir())
	err := ghSecretSet(context.Background(), DefaultRepo, SecretName, "value")
	assert.Error(t, err)
}

func writeCreds(t *testing.T, dir, content string) {
	t.Helper()
	require.NoError(t, os.WriteFile(filepath.Join(dir, ".credentials.json"), []byte(content), 0o600))
}

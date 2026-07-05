package claudecode

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMCPOAuthKey(t *testing.T) {
	// Reference vector computed from Claude Code's algorithm:
	// `${name}|${sha256hex(JSON.stringify({type,url,headers}))[:16]}`.
	assert.Equal(t, "vercel|511b08192b045b3d",
		MCPOAuthKey("vercel", "http", "https://mcp.vercel.com", nil))

	// nil and empty headers hash identically (both serialize to {}).
	assert.Equal(t,
		MCPOAuthKey("x", "http", "https://x.com", nil),
		MCPOAuthKey("x", "http", "https://x.com", map[string]string{}))

	// Name is a prefix, hash distinguishes different URLs.
	a := MCPOAuthKey("x", "http", "https://a.com", nil)
	b := MCPOAuthKey("x", "http", "https://b.com", nil)
	assert.NotEqual(t, a, b)
}

func writeCreds(t *testing.T, dir, body string) {
	t.Helper()
	require.NoError(t, os.WriteFile(filepath.Join(dir, ".credentials.json"), []byte(body), 0o600))
}

func TestCheckMCPOAuth(t *testing.T) {
	const now = int64(4_000_000_000_000) // ~year 2096, after any ISO date used below
	key := MCPOAuthKey("vercel", "http", "https://mcp.vercel.com", nil)

	t.Run("missing file", func(t *testing.T) {
		st, err := CheckMCPOAuth(t.TempDir(), "vercel", "http", "https://mcp.vercel.com", nil, now)
		require.NoError(t, err)
		assert.Equal(t, MCPOAuthMissing, st)
	})

	t.Run("no mcpOAuth section", func(t *testing.T) {
		dir := t.TempDir()
		writeCreds(t, dir, `{"claudeAiOauth":{"accessToken":"x"}}`)
		st, err := CheckMCPOAuth(dir, "vercel", "http", "https://mcp.vercel.com", nil, now)
		require.NoError(t, err)
		assert.Equal(t, MCPOAuthMissing, st)
	})

	t.Run("valid unexpired token", func(t *testing.T) {
		dir := t.TempDir()
		writeCreds(t, dir, fmt.Sprintf(`{"mcpOAuth":{%q:{"accessToken":"a","expiresAt":%d}}}`, key, now+10_000))
		st, err := CheckMCPOAuth(dir, "vercel", "http", "https://mcp.vercel.com", nil, now)
		require.NoError(t, err)
		assert.Equal(t, MCPOAuthValid, st)
	})

	t.Run("expired but refreshable is valid", func(t *testing.T) {
		dir := t.TempDir()
		writeCreds(t, dir, fmt.Sprintf(`{"mcpOAuth":{%q:{"accessToken":"a","refreshToken":"r","expiresAt":%d}}}`, key, now-10_000))
		st, err := CheckMCPOAuth(dir, "vercel", "http", "https://mcp.vercel.com", nil, now)
		require.NoError(t, err)
		assert.Equal(t, MCPOAuthValid, st)
	})

	t.Run("expired and not refreshable", func(t *testing.T) {
		dir := t.TempDir()
		writeCreds(t, dir, fmt.Sprintf(`{"mcpOAuth":{%q:{"accessToken":"a","expiresAt":%d}}}`, key, now-1))
		st, err := CheckMCPOAuth(dir, "vercel", "http", "https://mcp.vercel.com", nil, now)
		require.NoError(t, err)
		assert.Equal(t, MCPOAuthExpired, st)
	})

	t.Run("expiresAt as ISO string, expired", func(t *testing.T) {
		dir := t.TempDir()
		writeCreds(t, dir, fmt.Sprintf(`{"mcpOAuth":{%q:{"accessToken":"a","expiresAt":"2000-01-01T00:00:00Z"}}}`, key))
		st, err := CheckMCPOAuth(dir, "vercel", "http", "https://mcp.vercel.com", nil, now)
		require.NoError(t, err)
		assert.Equal(t, MCPOAuthExpired, st)
	})

	t.Run("tokenless stub treated as missing", func(t *testing.T) {
		dir := t.TempDir()
		writeCreds(t, dir, fmt.Sprintf(`{"mcpOAuth":{%q:{}}}`, key))
		st, err := CheckMCPOAuth(dir, "vercel", "http", "https://mcp.vercel.com", nil, now)
		require.NoError(t, err)
		assert.Equal(t, MCPOAuthMissing, st)
	})

	t.Run("no expiresAt means valid", func(t *testing.T) {
		dir := t.TempDir()
		writeCreds(t, dir, fmt.Sprintf(`{"mcpOAuth":{%q:{"accessToken":"a"}}}`, key))
		st, err := CheckMCPOAuth(dir, "vercel", "http", "https://mcp.vercel.com", nil, now)
		require.NoError(t, err)
		assert.Equal(t, MCPOAuthValid, st)
	})

	t.Run("invalid JSON errors", func(t *testing.T) {
		dir := t.TempDir()
		writeCreds(t, dir, `{invalid`)
		_, err := CheckMCPOAuth(dir, "vercel", "http", "https://mcp.vercel.com", nil, now)
		require.Error(t, err)
	})
}

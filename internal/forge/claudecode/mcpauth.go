package claudecode

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// MCPOAuthStatus is the result of a preflight check for a remote MCP server's
// OAuth credentials.
type MCPOAuthStatus int

const (
	// MCPOAuthValid means a usable token is stored (unexpired, or refreshable).
	MCPOAuthValid MCPOAuthStatus = iota
	// MCPOAuthMissing means no token is stored for the server.
	MCPOAuthMissing
	// MCPOAuthExpired means a token is stored but expired and not refreshable.
	MCPOAuthExpired
)

// MCPOAuthKey reproduces the key Claude Code uses to index a remote MCP
// server's OAuth credentials inside the "mcpOAuth" map of
// ~/.claude/.credentials.json. Claude Code computes it as:
//
//	`${name}|${sha256hex(JSON.stringify({type, url, headers}))[:16]}`
//
// where the JSON is produced by a plain JSON.stringify (object key order:
// type, url, headers; headers defaults to {}). Matching this exactly lets the
// preflight look up precisely the record the container's Claude Code will read.
//
// Note: JavaScript's JSON.stringify preserves header insertion order whereas
// Go sorts map keys, so the computed key only matches for servers with zero or
// one header — which is the norm for OAuth servers (they carry no static
// headers).
func MCPOAuthKey(name, typ, url string, headers map[string]string) string {
	if headers == nil {
		headers = map[string]string{}
	}
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false) // match JS JSON.stringify (no &,<,> escaping)
	_ = enc.Encode(struct {
		Type    string            `json:"type"`
		URL     string            `json:"url"`
		Headers map[string]string `json:"headers"`
	}{typ, url, headers})
	payload := strings.TrimRight(buf.String(), "\n")

	sum := sha256.Sum256([]byte(payload))
	return name + "|" + hex.EncodeToString(sum[:])[:16]
}

// CheckMCPOAuth reports whether a valid OAuth token for the given remote MCP
// server is present in claudeDir/.credentials.json. nowMs is the current time
// in Unix milliseconds (injected for testability). A token with a refresh
// token is treated as valid even past its access-token expiry, since Claude
// Code refreshes it automatically.
func CheckMCPOAuth(claudeDir, name, typ, url string, headers map[string]string, nowMs int64) (MCPOAuthStatus, error) {
	path := filepath.Join(claudeDir, ".credentials.json")
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return MCPOAuthMissing, nil
		}
		return MCPOAuthMissing, fmt.Errorf("failed to read credentials: %w", err)
	}

	var creds struct {
		MCPOAuth map[string]map[string]any `json:"mcpOAuth"`
	}
	if err := json.Unmarshal(data, &creds); err != nil {
		return MCPOAuthMissing, fmt.Errorf("failed to parse credentials: %w", err)
	}

	rec := creds.MCPOAuth[MCPOAuthKey(name, typ, url, headers)]
	if rec == nil {
		return MCPOAuthMissing, nil
	}

	access, _ := rec["accessToken"].(string)
	refresh, _ := rec["refreshToken"].(string)
	if access == "" && refresh == "" {
		// Tokenless stub — Claude Code treats these as unauthenticated.
		return MCPOAuthMissing, nil
	}
	if refresh != "" {
		return MCPOAuthValid, nil
	}

	switch v := rec["expiresAt"].(type) {
	case float64:
		if int64(v) <= nowMs {
			return MCPOAuthExpired, nil
		}
	case string:
		if t, err := time.Parse(time.RFC3339, v); err == nil && t.UnixMilli() <= nowMs {
			return MCPOAuthExpired, nil
		}
	}
	return MCPOAuthValid, nil
}

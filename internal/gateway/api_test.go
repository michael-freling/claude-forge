package gateway

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAPIServer_Schema(t *testing.T) {
	server := NewAPIServer(
		ProxyConfig{AllowedOwner: "my-owner", AllowedRepo: "my-repo"},
		NewGitHubAuthFromToken("test-token"),
	)

	req := httptest.NewRequest(http.MethodGet, "/api/schema", nil)
	w := httptest.NewRecorder()

	server.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "application/json", w.Header().Get("Content-Type"))

	var schema SchemaResponse
	err := json.Unmarshal(w.Body.Bytes(), &schema)
	require.NoError(t, err)
	assert.Greater(t, len(schema.Operations), 0)

	// Verify known operations exist
	opNames := make(map[string]bool)
	for _, op := range schema.Operations {
		opNames[op.Name] = true
		assert.NotEmpty(t, op.Method)
		assert.NotEmpty(t, op.Path)
		assert.NotEmpty(t, op.Description)
		assert.Contains(t, []string{"read", "write"}, op.Type)
	}
	assert.True(t, opNames["list-prs"])
	assert.True(t, opNames["create-pr"])
	assert.True(t, opNames["get-pr"])
	assert.True(t, opNames["list-issues"])
	assert.True(t, opNames["create-issue"])
	assert.True(t, opNames["get-repo"])
	assert.True(t, opNames["merge-pr"])
	assert.True(t, opNames["update-pr"])
}

func TestAPIServer_ReadOperationsAllowedForAnyRepo(t *testing.T) {
	ghAPI := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
	}))
	defer ghAPI.Close()

	server := newTestAPIServer(ghAPI.URL)

	tests := []struct {
		name string
		path string
	}{
		{
			name: "list PRs from other repo",
			path: "/api/github/repos/other-owner/other-repo/pulls",
		},
		{
			name: "get PR from other repo",
			path: "/api/github/repos/other-owner/other-repo/pulls/123",
		},
		{
			name: "list issues from other repo",
			path: "/api/github/repos/other-owner/other-repo/issues",
		},
		{
			name: "get issue from other repo",
			path: "/api/github/repos/other-owner/other-repo/issues/456",
		},
		{
			name: "get repo info from other repo",
			path: "/api/github/repos/other-owner/other-repo",
		},
		{
			name: "list releases from other repo",
			path: "/api/github/repos/other-owner/other-repo/releases",
		},
		{
			name: "list PR comments from other repo",
			path: "/api/github/repos/other-owner/other-repo/pulls/1/comments",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, tt.path, nil)
			w := httptest.NewRecorder()

			server.ServeHTTP(w, req)

			assert.Equal(t, http.StatusOK, w.Code)
		})
	}
}

func TestAPIServer_WriteOperationsAllowedForProjectRepo(t *testing.T) {
	ghAPI := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]string{"status": "created"})
	}))
	defer ghAPI.Close()

	server := newTestAPIServer(ghAPI.URL)

	tests := []struct {
		name   string
		method string
		path   string
		body   string
	}{
		{
			name:   "create PR in project repo",
			method: http.MethodPost,
			path:   "/api/github/repos/my-owner/my-repo/pulls",
			body:   `{"title":"test","head":"feature","base":"main"}`,
		},
		{
			name:   "create issue in project repo",
			method: http.MethodPost,
			path:   "/api/github/repos/my-owner/my-repo/issues",
			body:   `{"title":"bug"}`,
		},
		{
			name:   "create issue comment in project repo",
			method: http.MethodPost,
			path:   "/api/github/repos/my-owner/my-repo/issues/1/comments",
			body:   `{"body":"comment"}`,
		},
		{
			name:   "merge PR in project repo",
			method: http.MethodPut,
			path:   "/api/github/repos/my-owner/my-repo/pulls/1/merge",
		},
		{
			name:   "create PR comment in project repo",
			method: http.MethodPost,
			path:   "/api/github/repos/my-owner/my-repo/pulls/1/comments",
			body:   `{"body":"review comment"}`,
		},
		{
			name:   "update PR in project repo",
			method: http.MethodPatch,
			path:   "/api/github/repos/my-owner/my-repo/pulls/1",
			body:   `{"title":"updated title","body":"updated body"}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var body io.Reader
			if tt.body != "" {
				body = strings.NewReader(tt.body)
			}
			req := httptest.NewRequest(tt.method, tt.path, body)
			if tt.body != "" {
				req.Header.Set("Content-Type", "application/json")
			}
			w := httptest.NewRecorder()

			server.ServeHTTP(w, req)

			assert.Equal(t, http.StatusOK, w.Code)
		})
	}
}

func TestAPIServer_WriteOperationsBlockedForOtherRepo(t *testing.T) {
	server := NewAPIServer(
		ProxyConfig{AllowedOwner: "my-owner", AllowedRepo: "my-repo"},
		NewGitHubAuthFromToken("test-token"),
	)

	tests := []struct {
		name   string
		method string
		path   string
	}{
		{
			name:   "create PR in other repo",
			method: http.MethodPost,
			path:   "/api/github/repos/other-owner/other-repo/pulls",
		},
		{
			name:   "create issue in other repo",
			method: http.MethodPost,
			path:   "/api/github/repos/other-owner/other-repo/issues",
		},
		{
			name:   "merge PR in other repo",
			method: http.MethodPut,
			path:   "/api/github/repos/other-owner/other-repo/pulls/1/merge",
		},
		{
			name:   "delete in other repo",
			method: http.MethodDelete,
			path:   "/api/github/repos/other-owner/other-repo/issues/1",
		},
		{
			name:   "patch in other repo",
			method: http.MethodPatch,
			path:   "/api/github/repos/other-owner/other-repo/issues/1",
		},
		{
			name:   "write to same owner different repo",
			method: http.MethodPost,
			path:   "/api/github/repos/my-owner/other-repo/pulls",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(tt.method, tt.path, nil)
			w := httptest.NewRecorder()

			server.ServeHTTP(w, req)

			assert.Equal(t, http.StatusForbidden, w.Code)
			assert.Contains(t, w.Body.String(), "forbidden")
		})
	}
}

func TestAPIServer_NotFound(t *testing.T) {
	server := NewAPIServer(
		ProxyConfig{AllowedOwner: "my-owner", AllowedRepo: "my-repo"},
		NewGitHubAuthFromToken("test-token"),
	)

	req := httptest.NewRequest(http.MethodGet, "/unknown/path", nil)
	w := httptest.NewRecorder()

	server.ServeHTTP(w, req)

	assert.Equal(t, http.StatusNotFound, w.Code)
}

func TestAPIServer_ForwardsRequestToGitHubAPI(t *testing.T) {
	var capturedPath string
	var capturedMethod string
	var capturedAuth string
	var capturedBody string

	ghAPI := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedPath = r.URL.Path
		capturedMethod = r.Method
		capturedAuth = r.Header.Get("Authorization")
		body, _ := io.ReadAll(r.Body)
		capturedBody = string(body)

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"id":1}`))
	}))
	defer ghAPI.Close()

	server := newTestAPIServer(ghAPI.URL)

	body := `{"title":"test PR","head":"feature","base":"main"}`
	req := httptest.NewRequest(http.MethodPost, "/api/github/repos/my-owner/my-repo/pulls", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	server.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "/repos/my-owner/my-repo/pulls", capturedPath)
	assert.Equal(t, http.MethodPost, capturedMethod)
	assert.Equal(t, "Bearer test-token", capturedAuth)
	assert.Equal(t, body, capturedBody)
	assert.Equal(t, `{"id":1}`, w.Body.String())
}

func TestAPIServer_ForwardsQueryParams(t *testing.T) {
	var capturedQuery string

	ghAPI := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedQuery = r.URL.RawQuery
		w.WriteHeader(http.StatusOK)
	}))
	defer ghAPI.Close()

	server := newTestAPIServer(ghAPI.URL)

	req := httptest.NewRequest(http.MethodGet, "/api/github/repos/owner/repo/pulls?state=open&per_page=10", nil)
	w := httptest.NewRecorder()

	server.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "state=open&per_page=10", capturedQuery)
}

func TestAPIServer_CaseInsensitiveOwnerRepo(t *testing.T) {
	ghAPI := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer ghAPI.Close()

	server := newTestAPIServer(ghAPI.URL)

	req := httptest.NewRequest(http.MethodPost, "/api/github/repos/My-Owner/My-Repo/pulls", strings.NewReader("{}"))
	w := httptest.NewRecorder()

	server.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
}

func TestAPIServer_ForwardsResponseHeaders(t *testing.T) {
	ghAPI := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Custom-Header", "custom-value")
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{}`))
	}))
	defer ghAPI.Close()

	server := newTestAPIServer(ghAPI.URL)

	req := httptest.NewRequest(http.MethodGet, "/api/github/repos/owner/repo", nil)
	w := httptest.NewRecorder()

	server.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "custom-value", w.Header().Get("X-Custom-Header"))
}

func TestAPIServer_UpstreamError(t *testing.T) {
	server := NewAPIServer(
		ProxyConfig{AllowedOwner: "my-owner", AllowedRepo: "my-repo"},
		NewGitHubAuthFromToken("test-token"),
	)
	server.upstreamURL = "http://127.0.0.1:1" // port that should refuse

	req := httptest.NewRequest(http.MethodGet, "/api/github/repos/my-owner/my-repo/pulls", nil)
	w := httptest.NewRecorder()

	server.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadGateway, w.Code)
	assert.Contains(t, w.Body.String(), "failed to contact GitHub API")
}

func TestExtractOwnerRepo(t *testing.T) {
	tests := []struct {
		name      string
		path      string
		wantOwner string
		wantRepo  string
	}{
		{
			name:      "repos path",
			path:      "/repos/owner/repo/pulls",
			wantOwner: "owner",
			wantRepo:  "repo",
		},
		{
			name:      "repos path with number",
			path:      "/repos/owner/repo/pulls/123",
			wantOwner: "owner",
			wantRepo:  "repo",
		},
		{
			name:      "repos path only",
			path:      "/repos/owner/repo",
			wantOwner: "owner",
			wantRepo:  "repo",
		},
		{
			name:      "no repos segment",
			path:      "/users/owner/repos",
			wantOwner: "",
			wantRepo:  "",
		},
		{
			name:      "incomplete path",
			path:      "/repos/owner",
			wantOwner: "",
			wantRepo:  "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			owner, repo := extractOwnerRepo(tt.path)
			assert.Equal(t, tt.wantOwner, owner)
			assert.Equal(t, tt.wantRepo, repo)
		})
	}
}

func TestAPIServer_WriteWithNoRepoPath(t *testing.T) {
	server := NewAPIServer(
		ProxyConfig{AllowedOwner: "my-owner", AllowedRepo: "my-repo"},
		NewGitHubAuthFromToken("test-token"),
	)

	// POST to a path that doesn't contain /repos/{owner}/{repo}
	req := httptest.NewRequest(http.MethodPost, "/api/github/users", nil)
	w := httptest.NewRecorder()

	server.ServeHTTP(w, req)

	assert.Equal(t, http.StatusForbidden, w.Code)
}

func TestAPIServer_SetsGitHubAPIHeaders(t *testing.T) {
	var capturedAccept string
	var capturedVersion string

	ghAPI := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedAccept = r.Header.Get("Accept")
		capturedVersion = r.Header.Get("X-GitHub-Api-Version")
		w.WriteHeader(http.StatusOK)
	}))
	defer ghAPI.Close()

	server := newTestAPIServer(ghAPI.URL)

	req := httptest.NewRequest(http.MethodGet, "/api/github/repos/owner/repo", nil)
	w := httptest.NewRecorder()

	server.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "application/vnd.github+json", capturedAccept)
	assert.Equal(t, "2022-11-28", capturedVersion)
}

// newTestAPIServer creates an APIServer with its upstream URL pointed at the test server.
func newTestAPIServer(testServerURL string) *APIServer {
	server := NewAPIServer(
		ProxyConfig{AllowedOwner: "my-owner", AllowedRepo: "my-repo"},
		NewGitHubAuthFromToken("test-token"),
	)
	server.upstreamURL = testServerURL
	return server
}

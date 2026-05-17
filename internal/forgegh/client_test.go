package forgegh

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// testGateway creates a mock gateway server with schema and API endpoints.
func testGateway(t *testing.T, apiHandler http.HandlerFunc) *httptest.Server {
	t.Helper()

	schema := schemaResponse{
		Operations: []operation{
			{Name: "list-prs", Method: "GET", Path: "/repos/{owner}/{repo}/pulls", Description: "List pull requests", Type: "read"},
			{Name: "create-pr", Method: "POST", Path: "/repos/{owner}/{repo}/pulls", Description: "Create a pull request", Type: "write"},
			{Name: "get-pr", Method: "GET", Path: "/repos/{owner}/{repo}/pulls/{number}", Description: "Get a pull request", Type: "read"},
			{Name: "update-pr", Method: "PATCH", Path: "/repos/{owner}/{repo}/pulls/{number}", Description: "Update a pull request", Type: "write"},
			{Name: "list-pr-comments", Method: "GET", Path: "/repos/{owner}/{repo}/pulls/{number}/comments", Description: "List PR review comments", Type: "read"},
			{Name: "create-pr-comment", Method: "POST", Path: "/repos/{owner}/{repo}/pulls/{number}/comments", Description: "Create a PR review comment", Type: "write"},
			{Name: "list-issues", Method: "GET", Path: "/repos/{owner}/{repo}/issues", Description: "List issues", Type: "read"},
			{Name: "create-issue", Method: "POST", Path: "/repos/{owner}/{repo}/issues", Description: "Create an issue", Type: "write"},
			{Name: "get-issue", Method: "GET", Path: "/repos/{owner}/{repo}/issues/{number}", Description: "Get an issue", Type: "read"},
			{Name: "create-issue-comment", Method: "POST", Path: "/repos/{owner}/{repo}/issues/{number}/comments", Description: "Comment on an issue", Type: "write"},
			{Name: "get-repo", Method: "GET", Path: "/repos/{owner}/{repo}", Description: "Get repository info", Type: "read"},
			{Name: "list-releases", Method: "GET", Path: "/repos/{owner}/{repo}/releases", Description: "List releases", Type: "read"},
			{Name: "list-checks", Method: "GET", Path: "/repos/{owner}/{repo}/commits/{ref}/check-runs", Description: "List check runs", Type: "read"},
			{Name: "list-workflow-runs", Method: "GET", Path: "/repos/{owner}/{repo}/actions/runs", Description: "List workflow runs", Type: "read"},
			{Name: "get-workflow-run", Method: "GET", Path: "/repos/{owner}/{repo}/actions/runs/{run_id}", Description: "Get a workflow run", Type: "read"},
			{Name: "list-workflow-run-jobs", Method: "GET", Path: "/repos/{owner}/{repo}/actions/runs/{run_id}/jobs", Description: "List jobs for a workflow run", Type: "read"},
			{Name: "get-workflow-run-job-logs", Method: "GET", Path: "/repos/{owner}/{repo}/actions/jobs/{job_id}/logs", Description: "Get job logs", Type: "read"},
			{Name: "merge-pr", Method: "PUT", Path: "/repos/{owner}/{repo}/pulls/{number}/merge", Description: "Merge a pull request", Type: "write"},
		},
	}

	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/schema" {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(schema)
			return
		}
		if apiHandler != nil {
			apiHandler(w, r)
			return
		}
		http.Error(w, "not found", http.StatusNotFound)
	}))
}

func TestClient_SchemaDiscovery(t *testing.T) {
	gw := testGateway(t, nil)
	defer gw.Close()

	client := NewClient(gw.URL)
	schema, err := client.fetchSchema()

	require.NoError(t, err)
	assert.Greater(t, len(schema.Operations), 0)

	// Verify key operations
	opNames := make(map[string]bool)
	for _, op := range schema.Operations {
		opNames[op.Name] = true
	}
	assert.True(t, opNames["list-prs"])
	assert.True(t, opNames["create-pr"])
	assert.True(t, opNames["get-repo"])
}

func TestClient_PRList(t *testing.T) {
	var capturedPath string
	var capturedMethod string

	gw := testGateway(t, func(w http.ResponseWriter, r *http.Request) {
		capturedPath = r.URL.Path
		capturedMethod = r.Method
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`[{"number":1,"title":"first PR"}]`))
	})
	defer gw.Close()

	client := newTestClient(gw.URL)
	err := client.Run([]string{"pr", "list", "--repo", "owner/repo"})

	require.NoError(t, err)
	assert.Equal(t, "/api/github/repos/owner/repo/pulls", capturedPath)
	assert.Equal(t, http.MethodGet, capturedMethod)
}

func TestClient_PRView(t *testing.T) {
	var capturedPath string

	gw := testGateway(t, func(w http.ResponseWriter, r *http.Request) {
		capturedPath = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"number":42,"title":"test PR"}`))
	})
	defer gw.Close()

	client := newTestClient(gw.URL)
	err := client.Run([]string{"pr", "view", "42", "--repo", "owner/repo"})

	require.NoError(t, err)
	assert.Equal(t, "/api/github/repos/owner/repo/pulls/42", capturedPath)
}

func TestClient_PRCreate(t *testing.T) {
	var capturedPath string
	var capturedMethod string
	var capturedBody map[string]any

	gw := testGateway(t, func(w http.ResponseWriter, r *http.Request) {
		capturedPath = r.URL.Path
		capturedMethod = r.Method
		body, _ := io.ReadAll(r.Body)
		json.Unmarshal(body, &capturedBody)
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"number":99,"html_url":"https://github.com/owner/repo/pull/99"}`))
	})
	defer gw.Close()

	client := newTestClient(gw.URL)
	err := client.Run([]string{
		"pr", "create",
		"--repo", "owner/repo",
		"--title", "My PR",
		"--body", "Description here",
		"--head", "feature-branch",
		"--base", "main",
	})

	require.NoError(t, err)
	assert.Equal(t, "/api/github/repos/owner/repo/pulls", capturedPath)
	assert.Equal(t, http.MethodPost, capturedMethod)
	assert.Equal(t, "My PR", capturedBody["title"])
	assert.Equal(t, "Description here", capturedBody["body"])
	assert.Equal(t, "feature-branch", capturedBody["head"])
	assert.Equal(t, "main", capturedBody["base"])
}

func TestClient_PREdit(t *testing.T) {
	var capturedPath string
	var capturedMethod string
	var capturedBody map[string]any

	gw := testGateway(t, func(w http.ResponseWriter, r *http.Request) {
		capturedPath = r.URL.Path
		capturedMethod = r.Method
		body, _ := io.ReadAll(r.Body)
		json.Unmarshal(body, &capturedBody)
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"number":42,"title":"Updated Title"}`))
	})
	defer gw.Close()

	client := newTestClient(gw.URL)
	err := client.Run([]string{
		"pr", "edit", "42",
		"--repo", "owner/repo",
		"--title", "Updated Title",
		"--body", "Updated description",
	})

	require.NoError(t, err)
	assert.Equal(t, "/api/github/repos/owner/repo/pulls/42", capturedPath)
	assert.Equal(t, http.MethodPatch, capturedMethod)
	assert.Equal(t, "Updated Title", capturedBody["title"])
	assert.Equal(t, "Updated description", capturedBody["body"])
}

func TestClient_PRMerge(t *testing.T) {
	var capturedPath string
	var capturedMethod string

	gw := testGateway(t, func(w http.ResponseWriter, r *http.Request) {
		capturedPath = r.URL.Path
		capturedMethod = r.Method
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"merged":true}`))
	})
	defer gw.Close()

	client := newTestClient(gw.URL)
	err := client.Run([]string{"pr", "merge", "42", "--repo", "owner/repo"})

	require.NoError(t, err)
	assert.Equal(t, "/api/github/repos/owner/repo/pulls/42/merge", capturedPath)
	assert.Equal(t, http.MethodPut, capturedMethod)
}

func TestClient_PRComment(t *testing.T) {
	var capturedPath string
	var capturedMethod string
	var capturedBody map[string]any

	gw := testGateway(t, func(w http.ResponseWriter, r *http.Request) {
		capturedPath = r.URL.Path
		capturedMethod = r.Method
		body, _ := io.ReadAll(r.Body)
		json.Unmarshal(body, &capturedBody)
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"id":1}`))
	})
	defer gw.Close()

	client := newTestClient(gw.URL)
	err := client.Run([]string{"pr", "comment", "42", "--repo", "owner/repo", "--body", "LGTM"})

	require.NoError(t, err)
	assert.Equal(t, "/api/github/repos/owner/repo/pulls/42/comments", capturedPath)
	assert.Equal(t, http.MethodPost, capturedMethod)
	assert.Equal(t, "LGTM", capturedBody["body"])
}

func TestClient_IssueList(t *testing.T) {
	var capturedPath string

	gw := testGateway(t, func(w http.ResponseWriter, r *http.Request) {
		capturedPath = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`[{"number":1,"title":"bug"}]`))
	})
	defer gw.Close()

	client := newTestClient(gw.URL)
	err := client.Run([]string{"issue", "list", "--repo", "owner/repo"})

	require.NoError(t, err)
	assert.Equal(t, "/api/github/repos/owner/repo/issues", capturedPath)
}

func TestClient_IssueView(t *testing.T) {
	var capturedPath string

	gw := testGateway(t, func(w http.ResponseWriter, r *http.Request) {
		capturedPath = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"number":7,"title":"test issue"}`))
	})
	defer gw.Close()

	client := newTestClient(gw.URL)
	err := client.Run([]string{"issue", "view", "7", "--repo", "owner/repo"})

	require.NoError(t, err)
	assert.Equal(t, "/api/github/repos/owner/repo/issues/7", capturedPath)
}

func TestClient_IssueCreate(t *testing.T) {
	var capturedPath string
	var capturedBody map[string]any

	gw := testGateway(t, func(w http.ResponseWriter, r *http.Request) {
		capturedPath = r.URL.Path
		body, _ := io.ReadAll(r.Body)
		json.Unmarshal(body, &capturedBody)
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"number":10}`))
	})
	defer gw.Close()

	client := newTestClient(gw.URL)
	err := client.Run([]string{"issue", "create", "--repo", "owner/repo", "--title", "Bug report", "--body", "Something broke"})

	require.NoError(t, err)
	assert.Equal(t, "/api/github/repos/owner/repo/issues", capturedPath)
	assert.Equal(t, "Bug report", capturedBody["title"])
	assert.Equal(t, "Something broke", capturedBody["body"])
}

func TestClient_IssueComment(t *testing.T) {
	var capturedPath string
	var capturedBody map[string]any

	gw := testGateway(t, func(w http.ResponseWriter, r *http.Request) {
		capturedPath = r.URL.Path
		body, _ := io.ReadAll(r.Body)
		json.Unmarshal(body, &capturedBody)
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"id":5}`))
	})
	defer gw.Close()

	client := newTestClient(gw.URL)
	err := client.Run([]string{"issue", "comment", "7", "--repo", "owner/repo", "--body", "Fixed in v2"})

	require.NoError(t, err)
	assert.Equal(t, "/api/github/repos/owner/repo/issues/7/comments", capturedPath)
	assert.Equal(t, "Fixed in v2", capturedBody["body"])
}

func TestClient_RepoView(t *testing.T) {
	var capturedPath string

	gw := testGateway(t, func(w http.ResponseWriter, r *http.Request) {
		capturedPath = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"full_name":"owner/repo"}`))
	})
	defer gw.Close()

	client := newTestClient(gw.URL)
	err := client.Run([]string{"repo", "view", "--repo", "owner/repo"})

	require.NoError(t, err)
	assert.Equal(t, "/api/github/repos/owner/repo", capturedPath)
}

func TestClient_EnvVarRepo(t *testing.T) {
	var capturedPath string

	gw := testGateway(t, func(w http.ResponseWriter, r *http.Request) {
		capturedPath = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`[{"number":1}]`))
	})
	defer gw.Close()

	t.Setenv("FORGE_PROJECT_OWNER", "env-owner")
	t.Setenv("FORGE_PROJECT_REPO", "env-repo")

	client := newTestClient(gw.URL)
	err := client.Run([]string{"pr", "list"})

	require.NoError(t, err)
	assert.Equal(t, "/api/github/repos/env-owner/env-repo/pulls", capturedPath)
}

func TestClient_RepoFlagOverridesEnvVar(t *testing.T) {
	var capturedPath string

	gw := testGateway(t, func(w http.ResponseWriter, r *http.Request) {
		capturedPath = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`[{"number":1}]`))
	})
	defer gw.Close()

	t.Setenv("FORGE_PROJECT_OWNER", "env-owner")
	t.Setenv("FORGE_PROJECT_REPO", "env-repo")

	client := newTestClient(gw.URL)
	err := client.Run([]string{"pr", "list", "--repo", "flag-owner/flag-repo"})

	require.NoError(t, err)
	assert.Equal(t, "/api/github/repos/flag-owner/flag-repo/pulls", capturedPath)
}

func TestClient_ErrorNoRepo(t *testing.T) {
	gw := testGateway(t, nil)
	defer gw.Close()

	t.Setenv("FORGE_PROJECT_OWNER", "")
	t.Setenv("FORGE_PROJECT_REPO", "")

	client := newTestClient(gw.URL)
	err := client.Run([]string{"pr", "list"})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "owner and repo are required")
}

func TestClient_ErrorTooFewArgs(t *testing.T) {
	client := NewClient("http://localhost:8083")
	err := client.Run([]string{"pr"})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "usage")
}

func TestClient_ErrorUnknownCommand(t *testing.T) {
	gw := testGateway(t, nil)
	defer gw.Close()

	t.Setenv("FORGE_PROJECT_OWNER", "owner")
	t.Setenv("FORGE_PROJECT_REPO", "repo")

	client := newTestClient(gw.URL)
	err := client.Run([]string{"unknown", "action", "--repo", "owner/repo"})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "unknown command")
}

func TestClient_ErrorGatewayReturnsError(t *testing.T) {
	gw := testGateway(t, func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "forbidden", http.StatusForbidden)
	})
	defer gw.Close()

	client := newTestClient(gw.URL)
	err := client.Run([]string{"pr", "list", "--repo", "owner/repo"})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "403")
}

func TestClient_QueryParams(t *testing.T) {
	var capturedQuery string

	gw := testGateway(t, func(w http.ResponseWriter, r *http.Request) {
		capturedQuery = r.URL.RawQuery
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`[]`))
	})
	defer gw.Close()

	client := newTestClient(gw.URL)
	err := client.Run([]string{"pr", "list", "--repo", "owner/repo", "--state", "open", "--per_page", "5"})

	require.NoError(t, err)
	assert.Contains(t, capturedQuery, "state=open")
	assert.Contains(t, capturedQuery, "per_page=5")
}

func TestClient_OutputFormatting(t *testing.T) {
	gw := testGateway(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"number":1,"title":"test"}`))
	})
	defer gw.Close()

	var buf bytes.Buffer
	client := newTestClient(gw.URL)
	client.stdout = &buf

	err := client.Run([]string{"pr", "view", "1", "--repo", "owner/repo"})

	require.NoError(t, err)
	// Output should be pretty-printed
	assert.Contains(t, buf.String(), "  ")
	assert.Contains(t, buf.String(), "\"number\": 1")
}

func TestParseArgs(t *testing.T) {
	tests := []struct {
		name       string
		args       []string
		wantEntity string
		wantAction string
		wantNumber string
		wantFlags  map[string]string
		wantErr    bool
	}{
		{
			name:       "entity and action only",
			args:       []string{"pr", "list"},
			wantEntity: "pr",
			wantAction: "list",
			wantFlags:  map[string]string{},
		},
		{
			name:       "with number",
			args:       []string{"pr", "view", "42"},
			wantEntity: "pr",
			wantAction: "view",
			wantNumber: "42",
			wantFlags:  map[string]string{},
		},
		{
			name:       "with flags",
			args:       []string{"pr", "create", "--title", "My PR", "--body", "desc"},
			wantEntity: "pr",
			wantAction: "create",
			wantFlags:  map[string]string{"title": "My PR", "body": "desc"},
		},
		{
			name:       "with number and flags",
			args:       []string{"issue", "comment", "7", "--body", "comment text"},
			wantEntity: "issue",
			wantAction: "comment",
			wantNumber: "7",
			wantFlags:  map[string]string{"body": "comment text"},
		},
		{
			name:       "with repo flag",
			args:       []string{"pr", "list", "--repo", "owner/repo"},
			wantEntity: "pr",
			wantAction: "list",
			wantFlags:  map[string]string{"repo": "owner/repo"},
		},
		{
			name:    "too few args",
			args:    []string{"pr"},
			wantErr: true,
		},
		{
			name:    "empty args",
			args:    []string{},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cmd, err := parseArgs(tt.args)

			if tt.wantErr {
				require.Error(t, err)
				return
			}

			require.NoError(t, err)
			assert.Equal(t, tt.wantEntity, cmd.Entity)
			assert.Equal(t, tt.wantAction, cmd.Action)
			assert.Equal(t, tt.wantNumber, cmd.Number)
			assert.Equal(t, tt.wantFlags, cmd.Flags)
		})
	}
}

func TestCommandToOperationName(t *testing.T) {
	tests := []struct {
		entity string
		action string
		want   string
	}{
		{"pr", "list", "list-prs"},
		{"pr", "view", "get-pr"},
		{"pr", "create", "create-pr"},
		{"pr", "edit", "update-pr"},
		{"pr", "merge", "merge-pr"},
		{"pr", "comment", "create-pr-comment"},
		{"issue", "list", "list-issues"},
		{"issue", "view", "get-issue"},
		{"issue", "create", "create-issue"},
		{"issue", "comment", "create-issue-comment"},
		{"repo", "view", "get-repo"},
		{"release", "list", "list-releases"},
		{"run", "list", "list-workflow-runs"},
	}

	for _, tt := range tests {
		t.Run(tt.entity+" "+tt.action, func(t *testing.T) {
			cmd := &parsedCommand{Entity: tt.entity, Action: tt.action}
			got := commandToOperationName(cmd)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestResolveNumber(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    string
		wantErr bool
	}{
		{
			name:  "valid number",
			input: "42",
			want:  "42",
		},
		{
			name:    "empty string",
			input:   "",
			wantErr: true,
		},
		{
			name:    "not a number",
			input:   "abc",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := resolveNumber(tt.input)

			if tt.wantErr {
				require.Error(t, err)
				return
			}

			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestClient_FetchSchema_Non200Status(t *testing.T) {
	// Gateway returns a non-200 status code for the schema endpoint
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/schema" {
			w.WriteHeader(http.StatusInternalServerError)
			w.Write([]byte("internal server error"))
			return
		}
		http.Error(w, "not found", http.StatusNotFound)
	}))
	defer srv.Close()

	client := newTestClient(srv.URL)
	schema, err := client.fetchSchema()

	require.Error(t, err)
	assert.Nil(t, schema)
	assert.Contains(t, err.Error(), "schema request failed with status 500")
}

func TestClient_FetchSchema_InvalidJSON(t *testing.T) {
	// Gateway returns 200 but with invalid JSON
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/schema" {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			w.Write([]byte("not valid json{{{"))
			return
		}
		http.Error(w, "not found", http.StatusNotFound)
	}))
	defer srv.Close()

	client := newTestClient(srv.URL)
	schema, err := client.fetchSchema()

	require.Error(t, err)
	assert.Nil(t, schema)
	assert.Contains(t, err.Error(), "failed to decode schema")
}

func TestClient_FetchSchema_ConnectionError(t *testing.T) {
	// Client configured with an invalid URL that will fail to connect
	client := newTestClient("http://127.0.0.1:1")
	schema, err := client.fetchSchema()

	require.Error(t, err)
	assert.Nil(t, schema)
	assert.Contains(t, err.Error(), "failed to connect to gateway")
}

func TestClient_MergePR_WithOptions(t *testing.T) {
	var capturedBody map[string]any

	gw := testGateway(t, func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		json.Unmarshal(body, &capturedBody)
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"merged":true}`))
	})
	defer gw.Close()

	client := newTestClient(gw.URL)
	err := client.Run([]string{
		"pr", "merge", "42",
		"--repo", "owner/repo",
		"--method", "squash",
		"--subject", "feat: merged PR",
	})

	require.NoError(t, err)
	assert.Equal(t, "squash", capturedBody["merge_method"])
	assert.Equal(t, "feat: merged PR", capturedBody["commit_title"])
}

func TestClient_ReleaseList(t *testing.T) {
	var capturedPath string

	gw := testGateway(t, func(w http.ResponseWriter, r *http.Request) {
		capturedPath = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`[{"tag_name":"v1.0.0"}]`))
	})
	defer gw.Close()

	client := newTestClient(gw.URL)
	err := client.Run([]string{"release", "list", "--repo", "owner/repo"})

	require.NoError(t, err)
	assert.Equal(t, "/api/github/repos/owner/repo/releases", capturedPath)
}

func TestClient_RunList(t *testing.T) {
	var capturedPath, capturedQuery string

	gw := testGateway(t, func(w http.ResponseWriter, r *http.Request) {
		capturedPath = r.URL.Path
		capturedQuery = r.URL.RawQuery
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"workflow_runs":[]}`))
	})
	defer gw.Close()

	client := newTestClient(gw.URL)
	err := client.Run([]string{"run", "list", "--branch", "main", "--limit", "5", "--repo", "owner/repo"})

	require.NoError(t, err)
	assert.Equal(t, "/api/github/repos/owner/repo/actions/runs", capturedPath)
	assert.Contains(t, capturedQuery, "branch=main")
	assert.Contains(t, capturedQuery, "per_page=5")
}

func TestClient_RunView(t *testing.T) {
	var capturedPath string

	gw := testGateway(t, func(w http.ResponseWriter, r *http.Request) {
		capturedPath = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"id":123,"status":"completed","conclusion":"failure"}`))
	})
	defer gw.Close()

	client := newTestClient(gw.URL)
	err := client.Run([]string{"run", "view", "12345", "--repo", "owner/repo"})

	require.NoError(t, err)
	assert.Equal(t, "/api/github/repos/owner/repo/actions/runs/12345", capturedPath)
}

func TestClient_RunViewJobLogs(t *testing.T) {
	var capturedPath string

	gw := testGateway(t, func(w http.ResponseWriter, r *http.Request) {
		capturedPath = r.URL.Path
		w.Header().Set("Content-Type", "text/plain")
		w.Write([]byte("2026-05-17T00:00:00Z FAIL TestForgeStart"))
	})
	defer gw.Close()

	client := newTestClient(gw.URL)
	err := client.Run([]string{"run", "view", "--job", "98765", "--repo", "owner/repo"})

	require.NoError(t, err)
	assert.Equal(t, "/api/github/repos/owner/repo/actions/jobs/98765/logs", capturedPath)
}

func TestClient_NonJSONResponse(t *testing.T) {
	gw := testGateway(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		w.Write([]byte("plain text response"))
	})
	defer gw.Close()

	var buf bytes.Buffer
	client := newTestClient(gw.URL)
	client.stdout = &buf

	err := client.Run([]string{"pr", "list", "--repo", "owner/repo"})

	require.NoError(t, err)
	assert.Contains(t, buf.String(), "plain text response")
}

func TestParseArgs_FlagWithoutValue(t *testing.T) {
	// Test a flag at the end with no value following it
	cmd, err := parseArgs([]string{"pr", "list", "--json"})

	require.NoError(t, err)
	assert.Equal(t, "pr", cmd.Entity)
	assert.Equal(t, "list", cmd.Action)
	assert.Equal(t, "", cmd.Flags["json"])
}

func TestCommandToOperationName_Fallback(t *testing.T) {
	// Unknown entity+action should fallback to entity-action concatenation
	cmd := &parsedCommand{Entity: "workflow", Action: "run"}
	got := commandToOperationName(cmd)
	assert.Equal(t, "workflow-run", got)
}

// newTestClient creates a Client with stdout/stderr captured to discard.
func newTestClient(gatewayURL string) *Client {
	c := NewClient(gatewayURL)
	c.stdout = io.Discard
	c.stderr = io.Discard
	return c
}

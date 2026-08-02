package main

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// recorder captures the last request a test server received.
type recorder struct {
	method   string
	path     string
	rawQuery string
	body     string
	quota    string // x-goog-user-project header

	respStatus int
	respBody   string
}

func newRecordingServer(rec *recorder) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		rec.method = r.Method
		rec.path = r.URL.Path
		rec.rawQuery = r.URL.RawQuery
		rec.body = string(b)
		rec.quota = r.Header.Get("x-goog-user-project")
		status := rec.respStatus
		if status == 0 {
			status = http.StatusOK
		}
		w.WriteHeader(status)
		if rec.respBody == "" {
			w.Write([]byte(`{"ok":true}`))
		} else {
			w.Write([]byte(rec.respBody))
		}
	}))
}

// testClientAt returns a Client whose every service endpoint points at url.
func testClientAt(url string) *Client {
	c := NewClient(fakeTokenSource{token: "test-token"}, "test-project", "test-project")
	c.endpoints = endpoints{
		compute:         url,
		resourceManager: url,
		storage:         url,
		run:             url,
		container:       url,
		secretManager:   url,
		asset:           url,
		logging:         url,
	}
	return c
}

// TestRegistry_EveryToolClassified is the safety net required by the spec: a new
// tool registered without a read/write classification (accessUnset zero value)
// fails here, so the default stays safe as the surface grows.
func TestRegistry_EveryToolClassified(t *testing.T) {
	require.NotEmpty(t, toolRegistry)
	for name, td := range toolRegistry {
		assert.Equal(t, name, td.Definition.Name, "registry key must match tool name")
		assert.NotEqual(t, accessUnset, td.Access, "tool %q has no read/write classification", name)
		assert.NotNil(t, td.build, "tool %q has no build func", name)
	}
}

// TestNoWriteToolsByDefault documents that the shipped surface is read-only:
// nothing is classified accessWrite. If a write tool is ever added, this test
// must be updated deliberately (and the write-gate tests exercised for it).
func TestNoWriteToolsByDefault(t *testing.T) {
	for name, td := range toolRegistry {
		assert.NotEqual(t, accessWrite, td.Access, "unexpected write tool %q", name)
	}
}

func TestAllToolDefinitions_Annotations(t *testing.T) {
	defs := allToolDefinitions()
	assert.Len(t, defs, len(toolRegistry))
	for _, d := range defs {
		require.NotNil(t, d.Annotations, "tool %q missing annotations", d.Name)
		// Every shipped tool is a read (or gated secret read): read-only, non-destructive.
		assert.True(t, d.Annotations.ReadOnlyHint, "tool %q should be read-only", d.Name)
		assert.False(t, d.Annotations.DestructiveHint, "tool %q should be non-destructive", d.Name)
	}
}

func TestAnnotationsFor(t *testing.T) {
	w := annotationsFor(accessWrite)
	assert.False(t, w.ReadOnlyHint)
	assert.True(t, w.DestructiveHint)
	assert.False(t, w.IdempotentHint)

	r := annotationsFor(accessRead)
	assert.True(t, r.ReadOnlyHint)
	assert.False(t, r.DestructiveHint)
	assert.True(t, r.IdempotentHint)

	s := annotationsFor(accessSecret)
	assert.True(t, s.ReadOnlyHint)
}

func TestExecuteTool_BuildRequests(t *testing.T) {
	tests := []struct {
		name       string
		args       map[string]any
		wantMethod string
		wantPath   string
		wantQuery  []string
		wantBody   []string
	}{
		{
			name:       "list_projects",
			args:       map[string]any{"filter": "labels.env=prod", "page_size": float64(10), "page_token": "tok"},
			wantMethod: "GET",
			wantPath:   "/projects",
			wantQuery:  []string{"filter=labels.env", "pageSize=10", "pageToken=tok"},
		},
		{
			name:       "describe_project",
			args:       map[string]any{},
			wantMethod: "GET",
			wantPath:   "/projects/test-project",
		},
		{
			name:       "get_iam_policy",
			args:       map[string]any{},
			wantMethod: "POST",
			wantPath:   "/projects/test-project:getIamPolicy",
			wantBody:   []string{"{}"},
		},
		{
			name:       "search_resources",
			args:       map[string]any{"query": "state:RUNNING", "asset_types": "compute.googleapis.com/Instance", "page_size": float64(5)},
			wantMethod: "GET",
			wantPath:   "/projects/test-project:searchAllResources",
			wantQuery:  []string{"query=state", "assetTypes=compute", "pageSize=5"},
		},
		{
			name:       "list_assets",
			args:       map[string]any{"content_type": "RESOURCE"},
			wantMethod: "GET",
			wantPath:   "/projects/test-project/assets",
			wantQuery:  []string{"contentType=RESOURCE"},
		},
		{
			name:       "list_instances",
			args:       map[string]any{"filter": "status=RUNNING", "max_results": float64(50)},
			wantMethod: "GET",
			wantPath:   "/projects/test-project/aggregated/instances",
			wantQuery:  []string{"filter=status", "maxResults=50"},
		},
		{
			name:       "describe_instance",
			args:       map[string]any{"zone": "us-central1-a", "instance": "vm1"},
			wantMethod: "GET",
			wantPath:   "/projects/test-project/zones/us-central1-a/instances/vm1",
		},
		{
			name:       "list_buckets",
			args:       map[string]any{"max_results": float64(20)},
			wantMethod: "GET",
			wantPath:   "/b",
			wantQuery:  []string{"project=test-project", "maxResults=20"},
		},
		{
			name:       "list_objects",
			args:       map[string]any{"bucket": "my-bucket", "prefix": "logs/"},
			wantMethod: "GET",
			wantPath:   "/b/my-bucket/o",
			wantQuery:  []string{"prefix=logs"},
		},
		{
			name:       "list_services default location",
			args:       map[string]any{},
			wantMethod: "GET",
			wantPath:   "/projects/test-project/locations/-/services",
		},
		{
			name:       "describe_service",
			args:       map[string]any{"location": "us-central1", "service": "svc"},
			wantMethod: "GET",
			wantPath:   "/projects/test-project/locations/us-central1/services/svc",
		},
		{
			name:       "list_clusters default location",
			args:       map[string]any{},
			wantMethod: "GET",
			wantPath:   "/projects/test-project/locations/-/clusters",
		},
		{
			name:       "describe_cluster",
			args:       map[string]any{"location": "us-central1", "cluster": "c1"},
			wantMethod: "GET",
			wantPath:   "/projects/test-project/locations/us-central1/clusters/c1",
		},
		{
			name:       "list_secrets",
			args:       map[string]any{"filter": "name:db"},
			wantMethod: "GET",
			wantPath:   "/projects/test-project/secrets",
			wantQuery:  []string{"filter=name"},
		},
		{
			name:       "list_secret_versions",
			args:       map[string]any{"secret": "s1"},
			wantMethod: "GET",
			wantPath:   "/projects/test-project/secrets/s1/versions",
		},
		{
			name:       "read_logs",
			args:       map[string]any{"filter": "severity>=ERROR"},
			wantMethod: "POST",
			wantPath:   "/entries:list",
			wantBody:   []string{"projects/test-project", "\"pageSize\":20", "severity", "timestamp desc"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := &recorder{}
			srv := newRecordingServer(rec)
			defer srv.Close()

			client := testClientAt(srv.URL)
			policy := &Policy{} // read-only default

			// The subtest name may include a suffix; the tool is the first field.
			tool := strings.Fields(tt.name)[0]
			out, isErr, err := executeTool(context.Background(), tool, tt.args, policy, client)
			require.NoError(t, err)
			assert.False(t, isErr, "unexpected error result: %s", out)

			assert.Equal(t, tt.wantMethod, rec.method)
			assert.Equal(t, tt.wantPath, rec.path)
			for _, q := range tt.wantQuery {
				assert.Contains(t, rec.rawQuery, q, "query %q missing", q)
			}
			for _, b := range tt.wantBody {
				assert.Contains(t, rec.body, b, "body %q missing", b)
			}
		})
	}
}

func TestExecuteTool_UnknownTool(t *testing.T) {
	client := testClientAt("http://unused")
	_, _, err := executeTool(context.Background(), "no_such_tool", nil, &Policy{}, client)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unknown tool")
}

func TestExecuteTool_SecretAccessDenied(t *testing.T) {
	rec := &recorder{}
	srv := newRecordingServer(rec)
	defer srv.Close()

	client := testClientAt(srv.URL)
	_, _, err := executeTool(context.Background(), "access_secret_version",
		map[string]any{"secret": "db-password"}, &Policy{}, client)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "secret access denied")
	// The request must never reach Google when blocked.
	assert.Empty(t, rec.path)
}

func TestExecuteTool_SecretAccessAllowed_NotRedacted(t *testing.T) {
	rec := &recorder{respBody: `{"name":"projects/p/secrets/s/versions/1","payload":{"data":"ya29.super-secret"}}`}
	srv := newRecordingServer(rec)
	defer srv.Close()

	client := testClientAt(srv.URL)
	out, isErr, err := executeTool(context.Background(), "access_secret_version",
		map[string]any{"secret": "s", "version": "1"}, &Policy{AllowSecretAccess: true}, client)
	require.NoError(t, err)
	assert.False(t, isErr)
	assert.Equal(t, "/projects/test-project/secrets/s/versions/1:access", rec.path)
	// The payload is the whole point of the gated tool — it must NOT be redacted.
	assert.Contains(t, out, "ya29.super-secret")
	assert.NotContains(t, out, redactionPlaceholder)
}

func TestExecuteTool_RedactsReadOutput(t *testing.T) {
	// A metadata surface (Cloud Run env var) leaking a token must be redacted.
	rec := &recorder{respBody: `{"env":[{"name":"TOKEN","value":"ya29.leaked-token"}]}`}
	srv := newRecordingServer(rec)
	defer srv.Close()

	client := testClientAt(srv.URL)
	out, isErr, err := executeTool(context.Background(), "describe_service",
		map[string]any{"location": "us-central1", "service": "svc"}, &Policy{}, client)
	require.NoError(t, err)
	assert.False(t, isErr)
	assert.NotContains(t, out, "ya29.leaked-token")
	assert.Contains(t, out, redactionPlaceholder)
}

func TestExecuteTool_LargeResultCapped(t *testing.T) {
	big := `{"data":"` + strings.Repeat("A", maxToolResultBytes+10) + `"}`
	rec := &recorder{respBody: big}
	srv := newRecordingServer(rec)
	defer srv.Close()

	client := testClientAt(srv.URL)
	out, isErr, err := executeTool(context.Background(), "list_instances", map[string]any{}, &Policy{}, client)
	require.NoError(t, err)
	assert.True(t, isErr)
	assert.Contains(t, out, "too large")
}

func TestExecuteTool_ErrorStatusPassthrough(t *testing.T) {
	rec := &recorder{respStatus: http.StatusForbidden, respBody: `{"error":{"message":"permission denied"}}`}
	srv := newRecordingServer(rec)
	defer srv.Close()

	client := testClientAt(srv.URL)
	out, isErr, err := executeTool(context.Background(), "list_projects", map[string]any{}, &Policy{}, client)
	require.NoError(t, err)
	assert.True(t, isErr)
	assert.Contains(t, out, "permission denied")
}

func TestExecuteTool_MissingProject(t *testing.T) {
	client := testClientAt("http://unused")
	client.project = "" // no default project and none passed
	_, _, err := executeTool(context.Background(), "describe_project", map[string]any{}, &Policy{}, client)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no project configured")
}

func TestExecuteTool_ProjectOverride(t *testing.T) {
	rec := &recorder{}
	srv := newRecordingServer(rec)
	defer srv.Close()

	client := testClientAt(srv.URL)
	_, _, err := executeTool(context.Background(), "describe_project",
		map[string]any{"project": "other-project"}, &Policy{}, client)
	require.NoError(t, err)
	assert.Equal(t, "/projects/other-project", rec.path)
}

func TestExecuteTool_MissingRequiredParam(t *testing.T) {
	client := testClientAt("http://unused")
	// describe_instance needs zone and instance.
	_, _, err := executeTool(context.Background(), "describe_instance",
		map[string]any{"zone": "us-central1-a"}, &Policy{}, client)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "instance")
}

func TestExecuteTool_TokenErrorPropagates(t *testing.T) {
	client := testClientAt("http://unused")
	client.tokenSource = fakeTokenSource{err: assertErr("token boom")}
	_, _, err := executeTool(context.Background(), "list_projects", map[string]any{}, &Policy{}, client)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "access token")
}

func TestReadLogs_PageSizeCap(t *testing.T) {
	rec := &recorder{}
	srv := newRecordingServer(rec)
	defer srv.Close()

	client := testClientAt(srv.URL)
	_, _, err := executeTool(context.Background(), "read_logs",
		map[string]any{"page_size": float64(500), "order_by": "timestamp asc"}, &Policy{}, client)
	require.NoError(t, err)
	assert.Contains(t, rec.body, "\"pageSize\":100") // capped at maxLogEntries
	assert.Contains(t, rec.body, "timestamp asc")
}

// TestExecuteTool_AllProjectToolsRequireProject exercises the requireProject
// error branch in every project-scoped tool: with no default project and no
// project argument, each must fail before issuing a request.
func TestExecuteTool_AllProjectToolsRequireProject(t *testing.T) {
	projectTools := []string{
		"describe_project", "get_iam_policy", "search_resources", "list_assets",
		"list_instances", "describe_instance", "list_buckets", "list_services",
		"describe_service", "list_clusters", "describe_cluster", "list_secrets",
		"list_secret_versions", "read_logs", "access_secret_version",
	}
	for _, name := range projectTools {
		t.Run(name, func(t *testing.T) {
			client := testClientAt("http://unused")
			client.project = ""
			// Permissive policy so access_secret_version reaches its build func.
			_, _, err := executeTool(context.Background(), name, map[string]any{}, &Policy{AllowSecretAccess: true}, client)
			require.Error(t, err)
			assert.Contains(t, err.Error(), "no project configured")
		})
	}
}

// TestExecuteTool_RequiredParams exercises the requireString error branch for
// tools that need a resource identifier beyond the project.
func TestExecuteTool_RequiredParams(t *testing.T) {
	tests := []struct {
		tool    string
		args    map[string]any
		missing string
	}{
		{"describe_instance", map[string]any{"zone": "z"}, "instance"},
		{"describe_service", map[string]any{"location": "us-central1"}, "service"},
		{"describe_cluster", map[string]any{"location": "us-central1"}, "cluster"},
		{"list_objects", map[string]any{}, "bucket"},
		{"list_secret_versions", map[string]any{}, "secret"},
		{"access_secret_version", map[string]any{}, "secret"},
	}
	for _, tt := range tests {
		t.Run(tt.tool, func(t *testing.T) {
			client := testClientAt("http://unused")
			_, _, err := executeTool(context.Background(), tt.tool, tt.args, &Policy{AllowSecretAccess: true}, client)
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.missing)
		})
	}
}

func TestGetNumber(t *testing.T) {
	// JSON numbers arrive as float64.
	n, ok := getNumber(map[string]any{"x": float64(7)}, "x")
	assert.True(t, ok)
	assert.Equal(t, 7, n)

	// Native int.
	n, ok = getNumber(map[string]any{"x": 42}, "x")
	assert.True(t, ok)
	assert.Equal(t, 42, n)

	// json.Number.
	n, ok = getNumber(map[string]any{"x": json.Number("13")}, "x")
	assert.True(t, ok)
	assert.Equal(t, 13, n)

	// Non-integer json.Number fails.
	_, ok = getNumber(map[string]any{"x": json.Number("nope")}, "x")
	assert.False(t, ok)

	// Absent and wrong-typed keys.
	_, ok = getNumber(map[string]any{}, "x")
	assert.False(t, ok)
	_, ok = getNumber(map[string]any{"x": "str"}, "x")
	assert.False(t, ok)
}

// TestExecuteTool_QuotaFollowsTargetProject verifies the multi-project billing
// behavior: with no fixed --quota-project, each call bills the project it
// targets (its explicit arg, else the server default).
func TestExecuteTool_QuotaFollowsTargetProject(t *testing.T) {
	rec := &recorder{}
	srv := newRecordingServer(rec)
	defer srv.Close()

	client := testClientAt(srv.URL)
	client.quotaProject = "" // no fixed billing project

	// Explicit per-call project: quota follows it.
	_, _, err := executeTool(context.Background(), "describe_project",
		map[string]any{"project": "other-proj"}, &Policy{}, client)
	require.NoError(t, err)
	assert.Equal(t, "other-proj", rec.quota)

	// Omitted project: quota falls back to the server default project.
	_, _, err = executeTool(context.Background(), "describe_project", map[string]any{}, &Policy{}, client)
	require.NoError(t, err)
	assert.Equal(t, "test-project", rec.quota)
}

func TestExecuteTool_QuotaFixedOverrideWins(t *testing.T) {
	rec := &recorder{}
	srv := newRecordingServer(rec)
	defer srv.Close()

	// testClientAt sets a fixed quotaProject ("test-project").
	client := testClientAt(srv.URL)
	_, _, err := executeTool(context.Background(), "describe_project",
		map[string]any{"project": "other-proj"}, &Policy{}, client)
	require.NoError(t, err)
	// The fixed billing project wins over the per-call target.
	assert.Equal(t, "test-project", rec.quota)
}

func TestExecuteTool_NoQuotaHeaderWhenNoProject(t *testing.T) {
	rec := &recorder{}
	srv := newRecordingServer(rec)
	defer srv.Close()

	client := testClientAt(srv.URL)
	client.project = ""
	client.quotaProject = ""
	// list_projects needs no project, so nothing determines a quota project.
	_, _, err := executeTool(context.Background(), "list_projects", map[string]any{}, &Policy{}, client)
	require.NoError(t, err)
	assert.Empty(t, rec.quota)
}

// assertErr is a tiny error type for tests.
type assertErr string

func (e assertErr) Error() string { return string(e) }

package main

import (
	"io"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAllToolDefinitions(t *testing.T) {
	defs := allToolDefinitions()
	assert.Len(t, defs, len(toolRegistry))

	names := make(map[string]bool)
	for _, d := range defs {
		assert.NotEmpty(t, d.Name)
		assert.NotEmpty(t, d.Description)
		assert.False(t, names[d.Name], "duplicate tool name: %s", d.Name)
		names[d.Name] = true
	}
}

func TestPRListTool_BuildRequest(t *testing.T) {
	td := prListTool()

	t.Run("defaults", func(t *testing.T) {
		method, path, body, err := td.buildRequest(map[string]any{}, "owner", "repo")
		require.NoError(t, err)
		assert.Equal(t, http.MethodGet, method)
		assert.Equal(t, "/repos/owner/repo/pulls", path)
		assert.Nil(t, body)
	})

	t.Run("with query params", func(t *testing.T) {
		args := map[string]any{
			"state":     "open",
			"per_page":  float64(50),
			"sort":      "updated",
			"direction": "desc",
			"page":      float64(2),
		}
		method, path, body, err := td.buildRequest(args, "owner", "repo")
		require.NoError(t, err)
		assert.Equal(t, http.MethodGet, method)
		assert.Contains(t, path, "state=open")
		assert.Contains(t, path, "per_page=50")
		assert.Contains(t, path, "sort=updated")
		assert.Contains(t, path, "direction=desc")
		assert.Contains(t, path, "page=2")
		assert.Nil(t, body)
	})

	t.Run("owner/repo override", func(t *testing.T) {
		args := map[string]any{"owner": "other", "repo": "thing"}
		_, path, _, err := td.buildRequest(args, "default-owner", "default-repo")
		require.NoError(t, err)
		assert.Contains(t, path, "/repos/other/thing/pulls")
	})
}

func TestPRGetTool_BuildRequest(t *testing.T) {
	td := prGetTool()

	t.Run("valid", func(t *testing.T) {
		method, path, body, err := td.buildRequest(map[string]any{"number": float64(42)}, "o", "r")
		require.NoError(t, err)
		assert.Equal(t, http.MethodGet, method)
		assert.Equal(t, "/repos/o/r/pulls/42", path)
		assert.Nil(t, body)
	})

	t.Run("missing number", func(t *testing.T) {
		_, _, _, err := td.buildRequest(map[string]any{}, "o", "r")
		assert.ErrorContains(t, err, "missing required parameter: number")
	})
}

func TestPRReviewsTool_BuildRequest(t *testing.T) {
	td := prReviewsTool()

	t.Run("valid", func(t *testing.T) {
		method, path, _, err := td.buildRequest(map[string]any{"number": float64(10)}, "o", "r")
		require.NoError(t, err)
		assert.Equal(t, http.MethodGet, method)
		assert.Equal(t, "/repos/o/r/pulls/10/reviews", path)
	})

	t.Run("missing number", func(t *testing.T) {
		_, _, _, err := td.buildRequest(map[string]any{}, "o", "r")
		assert.ErrorContains(t, err, "missing required parameter: number")
	})
}

func TestIssueListTool_BuildRequest(t *testing.T) {
	td := issueListTool()

	t.Run("defaults", func(t *testing.T) {
		method, path, _, err := td.buildRequest(map[string]any{}, "o", "r")
		require.NoError(t, err)
		assert.Equal(t, http.MethodGet, method)
		assert.Equal(t, "/repos/o/r/issues", path)
	})

	t.Run("with filters", func(t *testing.T) {
		args := map[string]any{
			"state":    "closed",
			"labels":   "bug,critical",
			"per_page": float64(25),
			"page":     float64(3),
		}
		_, path, _, err := td.buildRequest(args, "o", "r")
		require.NoError(t, err)
		assert.Contains(t, path, "state=closed")
		assert.Contains(t, path, "labels=bug%2Ccritical")
		assert.Contains(t, path, "per_page=25")
		assert.Contains(t, path, "page=3")
	})
}

func TestIssueGetTool_BuildRequest(t *testing.T) {
	td := issueGetTool()

	t.Run("valid", func(t *testing.T) {
		method, path, _, err := td.buildRequest(map[string]any{"number": float64(7)}, "o", "r")
		require.NoError(t, err)
		assert.Equal(t, http.MethodGet, method)
		assert.Equal(t, "/repos/o/r/issues/7", path)
	})

	t.Run("missing number", func(t *testing.T) {
		_, _, _, err := td.buildRequest(map[string]any{}, "o", "r")
		assert.ErrorContains(t, err, "missing required parameter: number")
	})
}

func TestRepoGetTool_BuildRequest(t *testing.T) {
	td := repoGetTool()
	method, path, _, err := td.buildRequest(map[string]any{}, "o", "r")
	require.NoError(t, err)
	assert.Equal(t, http.MethodGet, method)
	assert.Equal(t, "/repos/o/r", path)
}

func TestReleaseListTool_BuildRequest(t *testing.T) {
	td := releaseListTool()

	t.Run("defaults", func(t *testing.T) {
		_, path, _, err := td.buildRequest(map[string]any{}, "o", "r")
		require.NoError(t, err)
		assert.Equal(t, "/repos/o/r/releases", path)
	})

	t.Run("with per_page", func(t *testing.T) {
		_, path, _, err := td.buildRequest(map[string]any{"per_page": float64(10)}, "o", "r")
		require.NoError(t, err)
		assert.Contains(t, path, "per_page=10")
	})
}

func TestChecksListTool_BuildRequest(t *testing.T) {
	td := checksListTool()

	t.Run("valid", func(t *testing.T) {
		method, path, _, err := td.buildRequest(map[string]any{"ref": "main"}, "o", "r")
		require.NoError(t, err)
		assert.Equal(t, http.MethodGet, method)
		assert.Equal(t, "/repos/o/r/commits/main/check-runs", path)
	})

	t.Run("missing ref", func(t *testing.T) {
		_, _, _, err := td.buildRequest(map[string]any{}, "o", "r")
		assert.ErrorContains(t, err, "missing required parameter: ref")
	})
}

func TestPRCreateTool_BuildRequest(t *testing.T) {
	td := prCreateTool()

	t.Run("minimal", func(t *testing.T) {
		args := map[string]any{"title": "feat: add X", "head": "my-branch"}
		method, path, body, err := td.buildRequest(args, "o", "r")
		require.NoError(t, err)
		assert.Equal(t, http.MethodPost, method)
		assert.Equal(t, "/repos/o/r/pulls", path)
		assert.NotNil(t, body)
	})

	t.Run("with optional fields", func(t *testing.T) {
		args := map[string]any{
			"title": "feat: add X",
			"head":  "my-branch",
			"body":  "description",
			"base":  "main",
		}
		_, _, body, err := td.buildRequest(args, "o", "r")
		require.NoError(t, err)
		data, _ := io.ReadAll(body)
		assert.Contains(t, string(data), `"body":"description"`)
		assert.Contains(t, string(data), `"base":"main"`)
	})

	t.Run("missing title", func(t *testing.T) {
		_, _, _, err := td.buildRequest(map[string]any{"head": "b"}, "o", "r")
		assert.ErrorContains(t, err, "missing required parameter: title")
	})

	t.Run("missing head", func(t *testing.T) {
		_, _, _, err := td.buildRequest(map[string]any{"title": "t"}, "o", "r")
		assert.ErrorContains(t, err, "missing required parameter: head")
	})
}

func TestPRUpdateTool_BuildRequest(t *testing.T) {
	td := prUpdateTool()

	t.Run("valid with fields", func(t *testing.T) {
		args := map[string]any{
			"number": float64(5),
			"title":  "new title",
			"body":   "new body",
			"state":  "closed",
			"base":   "develop",
		}
		method, path, body, err := td.buildRequest(args, "o", "r")
		require.NoError(t, err)
		assert.Equal(t, http.MethodPatch, method)
		assert.Equal(t, "/repos/o/r/pulls/5", path)
		data, _ := io.ReadAll(body)
		assert.Contains(t, string(data), `"title":"new title"`)
		assert.Contains(t, string(data), `"state":"closed"`)
	})

	t.Run("missing number", func(t *testing.T) {
		_, _, _, err := td.buildRequest(map[string]any{}, "o", "r")
		assert.ErrorContains(t, err, "missing required parameter: number")
	})
}

func TestPRMergeTool_BuildRequest(t *testing.T) {
	td := prMergeTool()

	t.Run("valid with options", func(t *testing.T) {
		args := map[string]any{
			"number":       float64(3),
			"merge_method": "squash",
			"commit_title": "custom title",
		}
		method, path, body, err := td.buildRequest(args, "o", "r")
		require.NoError(t, err)
		assert.Equal(t, http.MethodPut, method)
		assert.Equal(t, "/repos/o/r/pulls/3/merge", path)
		data, _ := io.ReadAll(body)
		assert.Contains(t, string(data), `"merge_method":"squash"`)
		assert.Contains(t, string(data), `"commit_title":"custom title"`)
	})

	t.Run("missing number", func(t *testing.T) {
		_, _, _, err := td.buildRequest(map[string]any{}, "o", "r")
		assert.ErrorContains(t, err, "missing required parameter: number")
	})
}

func TestPRCommentTool_BuildRequest(t *testing.T) {
	td := prCommentTool()

	t.Run("valid", func(t *testing.T) {
		args := map[string]any{"number": float64(1), "body": "LGTM"}
		method, path, body, err := td.buildRequest(args, "o", "r")
		require.NoError(t, err)
		assert.Equal(t, http.MethodPost, method)
		assert.Equal(t, "/repos/o/r/issues/1/comments", path)
		data, _ := io.ReadAll(body)
		assert.Contains(t, string(data), `"body":"LGTM"`)
	})

	t.Run("missing number", func(t *testing.T) {
		_, _, _, err := td.buildRequest(map[string]any{"body": "x"}, "o", "r")
		assert.ErrorContains(t, err, "missing required parameter: number")
	})

	t.Run("missing body", func(t *testing.T) {
		_, _, _, err := td.buildRequest(map[string]any{"number": float64(1)}, "o", "r")
		assert.ErrorContains(t, err, "missing required parameter: body")
	})
}

func TestIssueCreateTool_BuildRequest(t *testing.T) {
	td := issueCreateTool()

	t.Run("minimal", func(t *testing.T) {
		args := map[string]any{"title": "bug report"}
		method, path, body, err := td.buildRequest(args, "o", "r")
		require.NoError(t, err)
		assert.Equal(t, http.MethodPost, method)
		assert.Equal(t, "/repos/o/r/issues", path)
		data, _ := io.ReadAll(body)
		assert.Contains(t, string(data), `"title":"bug report"`)
	})

	t.Run("with body and labels", func(t *testing.T) {
		args := map[string]any{
			"title":  "bug",
			"body":   "description",
			"labels": "bug, critical",
		}
		_, _, body, err := td.buildRequest(args, "o", "r")
		require.NoError(t, err)
		data, _ := io.ReadAll(body)
		assert.Contains(t, string(data), `"title":"bug"`)
		assert.Contains(t, string(data), `"body":"description"`)
		assert.Contains(t, string(data), `"labels"`)
	})

	t.Run("missing title", func(t *testing.T) {
		_, _, _, err := td.buildRequest(map[string]any{}, "o", "r")
		assert.ErrorContains(t, err, "missing required parameter: title")
	})
}

func TestIssueCommentTool_BuildRequest(t *testing.T) {
	td := issueCommentTool()

	t.Run("valid", func(t *testing.T) {
		args := map[string]any{"number": float64(5), "body": "comment"}
		method, path, _, err := td.buildRequest(args, "o", "r")
		require.NoError(t, err)
		assert.Equal(t, http.MethodPost, method)
		assert.Equal(t, "/repos/o/r/issues/5/comments", path)
	})

	t.Run("missing number", func(t *testing.T) {
		_, _, _, err := td.buildRequest(map[string]any{"body": "x"}, "o", "r")
		assert.ErrorContains(t, err, "missing required parameter: number")
	})

	t.Run("missing body", func(t *testing.T) {
		_, _, _, err := td.buildRequest(map[string]any{"number": float64(1)}, "o", "r")
		assert.ErrorContains(t, err, "missing required parameter: body")
	})
}

func TestAPITool_BuildRequest(t *testing.T) {
	td := apiTool()

	t.Run("GET request", func(t *testing.T) {
		args := map[string]any{"method": "GET", "path": "/repos/o/r/pulls"}
		method, path, body, err := td.buildRequest(args, "o", "r")
		require.NoError(t, err)
		assert.Equal(t, "GET", method)
		assert.Equal(t, "/repos/o/r/pulls", path)
		assert.Nil(t, body)
	})

	t.Run("POST with body", func(t *testing.T) {
		args := map[string]any{
			"method": "post",
			"path":   "/repos/o/r/issues",
			"body":   `{"title":"test"}`,
		}
		method, path, body, err := td.buildRequest(args, "o", "r")
		require.NoError(t, err)
		assert.Equal(t, "POST", method)
		assert.Equal(t, "/repos/o/r/issues", path)
		assert.NotNil(t, body)
	})

	t.Run("path without leading slash", func(t *testing.T) {
		args := map[string]any{"method": "GET", "path": "repos/o/r"}
		_, path, _, err := td.buildRequest(args, "o", "r")
		require.NoError(t, err)
		assert.Equal(t, "/repos/o/r", path)
	})

	t.Run("missing method", func(t *testing.T) {
		_, _, _, err := td.buildRequest(map[string]any{"path": "/x"}, "o", "r")
		assert.ErrorContains(t, err, "missing required parameter: method")
	})

	t.Run("missing path", func(t *testing.T) {
		_, _, _, err := td.buildRequest(map[string]any{"method": "GET"}, "o", "r")
		assert.ErrorContains(t, err, "missing required parameter: path")
	})
}

func TestExtractOwnerRepo(t *testing.T) {
	tests := []struct {
		path      string
		wantOwner string
		wantRepo  string
	}{
		{"/repos/alice/bob/pulls", "alice", "bob"},
		{"/repos/alice/bob", "alice", "bob"},
		{"/user", "", ""},
		{"/repos/alice", "", ""},
		{"/repos/alice/bob/pulls/1/reviews", "alice", "bob"},
	}
	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			o, r := extractOwnerRepo(tt.path)
			assert.Equal(t, tt.wantOwner, o)
			assert.Equal(t, tt.wantRepo, r)
		})
	}
}

func TestHelperFunctions(t *testing.T) {
	t.Run("getString", func(t *testing.T) {
		assert.Equal(t, "hello", getString(map[string]any{"k": "hello"}, "k"))
		assert.Equal(t, "", getString(map[string]any{"k": 42}, "k"))
		assert.Equal(t, "", getString(map[string]any{}, "k"))
	})

	t.Run("getNumber", func(t *testing.T) {
		n, ok := getNumber(map[string]any{"k": float64(42)}, "k")
		assert.True(t, ok)
		assert.Equal(t, 42, n)

		_, ok = getNumber(map[string]any{"k": "not a number"}, "k")
		assert.False(t, ok)

		_, ok = getNumber(map[string]any{}, "k")
		assert.False(t, ok)
	})

	t.Run("addQueryParam", func(t *testing.T) {
		assert.Equal(t, "/path?key=val", addQueryParam("/path", "key", "val"))
		assert.Equal(t, "/path", addQueryParam("/path", "key", ""))
		assert.Equal(t, "/path?a=1&b=2", addQueryParam("/path?a=1", "b", "2"))
	})

	t.Run("addQueryParamInt", func(t *testing.T) {
		assert.Equal(t, "/path?n=10", addQueryParamInt("/path", map[string]any{"n": float64(10)}, "n"))
		assert.Equal(t, "/path", addQueryParamInt("/path", map[string]any{}, "n"))
	})

	t.Run("resolveOwnerRepo", func(t *testing.T) {
		o, r := resolveOwnerRepo(map[string]any{"owner": "a", "repo": "b"}, "x", "y")
		assert.Equal(t, "a", o)
		assert.Equal(t, "b", r)

		o, r = resolveOwnerRepo(map[string]any{}, "x", "y")
		assert.Equal(t, "x", o)
		assert.Equal(t, "y", r)
	})
}

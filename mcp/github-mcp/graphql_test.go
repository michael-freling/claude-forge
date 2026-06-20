package main

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newGraphQLClient returns a GitHubClient pointed at the given test server.
func newGraphQLClient(url string) *GitHubClient {
	c := NewGitHubClient(NewGitHubAuthFromToken("test-token"))
	c.baseURL = url
	return c
}

func TestDoGraphQL(t *testing.T) {
	t.Run("success returns data", func(t *testing.T) {
		gh := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			assert.Equal(t, "/graphql", r.URL.Path)
			assert.Equal(t, http.MethodPost, r.Method)
			assert.Equal(t, "Bearer test-token", r.Header.Get("Authorization"))
			w.Write([]byte(`{"data":{"hello":"world"}}`))
		}))
		defer gh.Close()

		data, err := doGraphQL(context.Background(), newGraphQLClient(gh.URL), "query{}", nil)
		require.NoError(t, err)
		assert.JSONEq(t, `{"hello":"world"}`, string(data))
	})

	t.Run("http error", func(t *testing.T) {
		gh := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
			w.Write([]byte("boom"))
		}))
		defer gh.Close()

		_, err := doGraphQL(context.Background(), newGraphQLClient(gh.URL), "query{}", nil)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "HTTP 500")
	})

	t.Run("graphql errors", func(t *testing.T) {
		gh := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Write([]byte(`{"errors":[{"message":"Could not resolve to a node"}]}`))
		}))
		defer gh.Close()

		_, err := doGraphQL(context.Background(), newGraphQLClient(gh.URL), "query{}", nil)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "Could not resolve to a node")
	})

	t.Run("invalid json response", func(t *testing.T) {
		gh := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Write([]byte("not json"))
		}))
		defer gh.Close()

		_, err := doGraphQL(context.Background(), newGraphQLClient(gh.URL), "query{}", nil)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "failed to parse")
	})

	t.Run("transport error", func(t *testing.T) {
		c := newGraphQLClient("http://127.0.0.1:0")
		_, err := doGraphQL(context.Background(), c, "query{}", nil)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "GraphQL request failed")
	})
}

func TestFindReviewThreadByComment(t *testing.T) {
	t.Run("found on first page", func(t *testing.T) {
		gh := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Write([]byte(`{"data":{"repository":{"pullRequest":{"reviewThreads":{
				"pageInfo":{"hasNextPage":false,"endCursor":""},
				"nodes":[{"id":"T1","isResolved":false,"comments":{"nodes":[{"databaseId":100},{"databaseId":101}]}}]
			}}}}}`))
		}))
		defer gh.Close()

		thread, err := findReviewThreadByComment(context.Background(), newGraphQLClient(gh.URL), "o", "r", 5, 101)
		require.NoError(t, err)
		require.NotNil(t, thread)
		assert.Equal(t, "T1", thread.ID)
		assert.False(t, thread.IsResolved)
	})

	t.Run("found on second page", func(t *testing.T) {
		gh := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			body, _ := io.ReadAll(r.Body)
			if strings.Contains(string(body), `"cursor":"c1"`) {
				w.Write([]byte(`{"data":{"repository":{"pullRequest":{"reviewThreads":{
					"pageInfo":{"hasNextPage":false,"endCursor":""},
					"nodes":[{"id":"T2","isResolved":false,"comments":{"nodes":[{"databaseId":200}]}}]
				}}}}}`))
				return
			}
			w.Write([]byte(`{"data":{"repository":{"pullRequest":{"reviewThreads":{
				"pageInfo":{"hasNextPage":true,"endCursor":"c1"},
				"nodes":[{"id":"T1","isResolved":false,"comments":{"nodes":[{"databaseId":999}]}}]
			}}}}}`))
		}))
		defer gh.Close()

		thread, err := findReviewThreadByComment(context.Background(), newGraphQLClient(gh.URL), "o", "r", 5, 200)
		require.NoError(t, err)
		require.NotNil(t, thread)
		assert.Equal(t, "T2", thread.ID)
	})

	t.Run("not found", func(t *testing.T) {
		gh := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Write([]byte(`{"data":{"repository":{"pullRequest":{"reviewThreads":{
				"pageInfo":{"hasNextPage":false,"endCursor":""},
				"nodes":[{"id":"T1","isResolved":false,"comments":{"nodes":[{"databaseId":1}]}}]
			}}}}}`))
		}))
		defer gh.Close()

		thread, err := findReviewThreadByComment(context.Background(), newGraphQLClient(gh.URL), "o", "r", 5, 999)
		require.NoError(t, err)
		assert.Nil(t, thread)
	})

	t.Run("propagates graphql error", func(t *testing.T) {
		gh := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Write([]byte(`{"errors":[{"message":"boom"}]}`))
		}))
		defer gh.Close()

		_, err := findReviewThreadByComment(context.Background(), newGraphQLClient(gh.URL), "o", "r", 5, 1)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "boom")
	})
}

func TestResolveReviewThreadByID(t *testing.T) {
	gh := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		assert.Contains(t, string(body), "resolveReviewThread")
		assert.Contains(t, string(body), "T1")
		w.Write([]byte(`{"data":{"resolveReviewThread":{"thread":{"id":"T1","isResolved":true}}}}`))
	}))
	defer gh.Close()

	resolved, err := resolveReviewThreadByID(context.Background(), newGraphQLClient(gh.URL), "T1")
	require.NoError(t, err)
	assert.True(t, resolved)
}

func TestResolveReviewThreadByID_Error(t *testing.T) {
	gh := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"errors":[{"message":"Could not resolve to a node with the global id"}]}`))
	}))
	defer gh.Close()

	_, err := resolveReviewThreadByID(context.Background(), newGraphQLClient(gh.URL), "bad")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "Could not resolve")
}

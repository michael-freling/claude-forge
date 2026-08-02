package main

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/oauth2"
)

// fakeTokenSource is a static oauth2.TokenSource for tests.
type fakeTokenSource struct {
	token string
	err   error
}

func (f fakeTokenSource) Token() (*oauth2.Token, error) {
	if f.err != nil {
		return nil, f.err
	}
	return &oauth2.Token{AccessToken: f.token}, nil
}

// roundTripFunc lets a test intercept HTTP without a live endpoint.
type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestClientDo_InjectsAuthAndHeaders(t *testing.T) {
	var gotAuth, gotQuota, gotAccept, gotContentType string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		gotQuota = r.Header.Get("x-goog-user-project")
		gotAccept = r.Header.Get("Accept")
		gotContentType = r.Header.Get("Content-Type")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"ok":true}`))
	}))
	defer srv.Close()

	c := NewClient(fakeTokenSource{token: "tok-123"}, "proj", "quota-proj")

	// GET: no Content-Type expected.
	body, status, err := c.do(context.Background(), http.MethodGet, srv.URL+"/x", nil, "quota-proj")
	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, status)
	assert.Contains(t, string(body), "ok")
	assert.Equal(t, "Bearer tok-123", gotAuth)
	assert.Equal(t, "quota-proj", gotQuota)
	assert.Equal(t, "application/json", gotAccept)
	assert.Empty(t, gotContentType)

	// POST with body: Content-Type set.
	_, _, err = c.do(context.Background(), http.MethodPost, srv.URL+"/y", strings.NewReader("{}"), "quota-proj")
	require.NoError(t, err)
	assert.Equal(t, "application/json", gotContentType)
}

func TestClientDo_NoQuotaHeaderWhenEmpty(t *testing.T) {
	var gotQuota string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotQuota = r.Header.Get("x-goog-user-project")
	}))
	defer srv.Close()

	c := NewClient(fakeTokenSource{token: "t"}, "proj", "")
	_, _, err := c.do(context.Background(), http.MethodGet, srv.URL, nil, "")
	require.NoError(t, err)
	assert.Empty(t, gotQuota)
}

func TestClientDo_TokenError(t *testing.T) {
	c := NewClient(fakeTokenSource{err: fmt.Errorf("boom")}, "proj", "proj")
	_, _, err := c.do(context.Background(), http.MethodGet, "http://example.invalid", nil, "proj")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to obtain access token")
}

func TestClientDo_BadURL(t *testing.T) {
	c := NewClient(fakeTokenSource{token: "t"}, "proj", "proj")
	_, _, err := c.do(context.Background(), "bad method", "http://example.com", nil, "proj")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to create request")
}

func TestClientDo_RequestError(t *testing.T) {
	c := NewClient(fakeTokenSource{token: "t"}, "proj", "proj")
	// Nothing is listening at this address.
	_, _, err := c.do(context.Background(), http.MethodGet, "http://127.0.0.1:0/x", nil, "proj")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "request failed")
}

func TestClientDo_PassesThroughErrorStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		w.Write([]byte(`{"error":"denied"}`))
	}))
	defer srv.Close()

	c := NewClient(fakeTokenSource{token: "t"}, "proj", "proj")
	body, status, err := c.do(context.Background(), http.MethodGet, srv.URL, nil, "proj")
	require.NoError(t, err)
	assert.Equal(t, http.StatusForbidden, status)
	assert.Contains(t, string(body), "denied")
}

func TestResolveImpersonationTarget(t *testing.T) {
	t.Run("flag wins", func(t *testing.T) {
		t.Setenv(impersonateEnvVar, "env-sa@example.iam.gserviceaccount.com")
		assert.Equal(t, "flag-sa@example.iam.gserviceaccount.com",
			resolveImpersonationTarget("flag-sa@example.iam.gserviceaccount.com"))
	})
	t.Run("env fallback", func(t *testing.T) {
		t.Setenv(impersonateEnvVar, "env-sa@example.iam.gserviceaccount.com")
		assert.Equal(t, "env-sa@example.iam.gserviceaccount.com", resolveImpersonationTarget(""))
	})
	t.Run("none", func(t *testing.T) {
		t.Setenv(impersonateEnvVar, "")
		assert.Empty(t, resolveImpersonationTarget(""))
	})
}

func TestImpersonatedTokenSource_Success(t *testing.T) {
	var gotAuth, gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		gotPath = r.URL.Path
		w.WriteHeader(http.StatusOK)
		fmt.Fprintf(w, `{"accessToken":"imp-token","expireTime":%q}`, "2030-01-02T03:04:05Z")
	}))
	defer srv.Close()

	its := &impersonatedTokenSource{
		base:       fakeTokenSource{token: "base-token"},
		targetSA:   "target@proj.iam.gserviceaccount.com",
		scopes:     []string{cloudPlatformScope},
		endpoint:   srv.URL,
		httpClient: http.DefaultClient,
	}

	tok, err := its.Token()
	require.NoError(t, err)
	assert.Equal(t, "imp-token", tok.AccessToken)
	assert.Equal(t, "Bearer base-token", gotAuth)
	assert.Contains(t, gotPath, "target@proj.iam.gserviceaccount.com:generateAccessToken")
	want, _ := time.Parse(time.RFC3339, "2030-01-02T03:04:05Z")
	assert.True(t, tok.Expiry.Equal(want))
}

func TestImpersonatedTokenSource_BaseError(t *testing.T) {
	its := &impersonatedTokenSource{
		base:       fakeTokenSource{err: fmt.Errorf("no base creds")},
		targetSA:   "t@p.iam.gserviceaccount.com",
		endpoint:   "http://unused",
		httpClient: http.DefaultClient,
	}
	_, err := its.Token()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "base credentials")
}

func TestImpersonatedTokenSource_HTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		w.Write([]byte(`{"error":"missing tokenCreator role"}`))
	}))
	defer srv.Close()

	its := &impersonatedTokenSource{
		base:       fakeTokenSource{token: "b"},
		targetSA:   "t@p.iam.gserviceaccount.com",
		endpoint:   srv.URL,
		httpClient: http.DefaultClient,
	}
	_, err := its.Token()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "impersonation of t@p.iam.gserviceaccount.com failed")
	assert.Contains(t, err.Error(), "tokenCreator")
}

func TestImpersonatedTokenSource_EmptyToken(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"accessToken":""}`))
	}))
	defer srv.Close()

	its := &impersonatedTokenSource{
		base:       fakeTokenSource{token: "b"},
		targetSA:   "t@p.iam.gserviceaccount.com",
		endpoint:   srv.URL,
		httpClient: http.DefaultClient,
	}
	_, err := its.Token()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "empty access token")
}

func TestImpersonatedTokenSource_BadExpiry(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"accessToken":"x","expireTime":"not-a-time"}`))
	}))
	defer srv.Close()

	its := &impersonatedTokenSource{
		base:       fakeTokenSource{token: "b"},
		targetSA:   "t@p.iam.gserviceaccount.com",
		endpoint:   srv.URL,
		httpClient: http.DefaultClient,
	}
	_, err := its.Token()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "token expiry")
}

func TestImpersonatedTokenSource_BadJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`not json`))
	}))
	defer srv.Close()

	its := &impersonatedTokenSource{
		base:       fakeTokenSource{token: "b"},
		targetSA:   "t@p.iam.gserviceaccount.com",
		endpoint:   srv.URL,
		httpClient: http.DefaultClient,
	}
	_, err := its.Token()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "parse impersonation response")
}

func TestNewImpersonatedTokenSource_WrapsAndDefaultsClient(t *testing.T) {
	// nil httpClient must not panic and yields a usable source.
	ts := newImpersonatedTokenSource(fakeTokenSource{token: "b"}, "t@p.iam.gserviceaccount.com", nil)
	require.NotNil(t, ts)

	// With an injected transport, the wrapped source mints a token end to end.
	client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(strings.NewReader(`{"accessToken":"wrapped","expireTime":"2030-01-01T00:00:00Z"}`)),
			Header:     make(http.Header),
		}, nil
	})}
	ts2 := newImpersonatedTokenSource(fakeTokenSource{token: "b"}, "t@p.iam.gserviceaccount.com", client)
	tok, err := ts2.Token()
	require.NoError(t, err)
	assert.Equal(t, "wrapped", tok.AccessToken)
}

func TestNewADCTokenSource_Error(t *testing.T) {
	// Point ADC discovery at a file that does not exist so it fails
	// deterministically without contacting any metadata server.
	t.Setenv("GOOGLE_APPLICATION_CREDENTIALS", "/nonexistent/adc-does-not-exist.json")
	_, err := NewADCTokenSource(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "Application Default Credentials")
}

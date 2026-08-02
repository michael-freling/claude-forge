package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"time"

	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
)

// cloudPlatformScope is the OAuth scope every Google Cloud REST API accepts.
const cloudPlatformScope = "https://www.googleapis.com/auth/cloud-platform"

// maxResponseBytes bounds how much of an API response the client reads, so a
// pathological response cannot exhaust memory. Tools additionally pass explicit
// page-size caps to keep ordinary responses far below this.
const maxResponseBytes = 8 << 20 // 8 MiB

// endpoints holds the base URL of each Google Cloud service the tools call.
// They are fields (not constants) so tests can point every service at a single
// httptest server.
type endpoints struct {
	compute         string
	resourceManager string
	storage         string
	run             string
	container       string
	secretManager   string
	asset           string
	logging         string
}

// defaultEndpoints returns the production Google Cloud REST base URLs.
func defaultEndpoints() endpoints {
	return endpoints{
		compute:         "https://compute.googleapis.com/compute/v1",
		resourceManager: "https://cloudresourcemanager.googleapis.com/v1",
		storage:         "https://storage.googleapis.com/storage/v1",
		run:             "https://run.googleapis.com/v2",
		container:       "https://container.googleapis.com/v1",
		secretManager:   "https://secretmanager.googleapis.com/v1",
		asset:           "https://cloudasset.googleapis.com/v1",
		logging:         "https://logging.googleapis.com/v2",
	}
}

// Client issues authenticated requests to Google Cloud REST APIs. Auth is a
// Bearer access token minted from Application Default Credentials (optionally
// impersonating a service account); tokens are refreshed in-process so the
// server survives sessions far longer than a single token's lifetime.
//
// The client is not bound to one project: project is only a default for calls
// that omit their own "project" argument, so a single credential can target any
// project it can access.
type Client struct {
	httpClient  *http.Client
	tokenSource oauth2.TokenSource
	// project is the default project for calls that omit a "project" argument.
	// May be empty, in which case such calls must pass one explicitly.
	project string
	// quotaProject, when set, fixes the x-goog-user-project billing project for
	// every call. When empty, each call bills the project it targets (see
	// executeTool), which is what makes cross-project use bill correctly.
	quotaProject string
	endpoints    endpoints
}

// NewClient creates a Client with the production endpoints.
func NewClient(ts oauth2.TokenSource, project, quotaProject string) *Client {
	return &Client{
		httpClient:   http.DefaultClient,
		tokenSource:  ts,
		endpoints:    defaultEndpoints(),
		project:      project,
		quotaProject: quotaProject,
	}
}

// do issues an authenticated request to fullURL and returns the response body
// and status code. quotaProject, when non-empty, is sent as x-goog-user-project
// (the project billed for quota). A non-2xx status is not an error here — the
// caller inspects the status and surfaces the body (which carries Google's
// structured error).
func (c *Client) do(ctx context.Context, method, fullURL string, body io.Reader, quotaProject string) ([]byte, int, error) {
	req, err := http.NewRequestWithContext(ctx, method, fullURL, body)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to create request: %w", err)
	}

	tok, err := c.tokenSource.Token()
	if err != nil {
		return nil, 0, fmt.Errorf("failed to obtain access token: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+tok.AccessToken)
	req.Header.Set("Accept", "application/json")
	if quotaProject != "" {
		req.Header.Set("x-goog-user-project", quotaProject)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, 0, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes))
	if err != nil {
		return nil, resp.StatusCode, fmt.Errorf("failed to read response: %w", err)
	}
	return respBody, resp.StatusCode, nil
}

// --- Authentication ---

// impersonateEnvVar is gcloud's mapping for the
// auth/impersonate_service_account property; honoring it lets the same
// environment configure impersonation for both gcloud and this server.
const impersonateEnvVar = "CLOUDSDK_AUTH_IMPERSONATE_SERVICE_ACCOUNT"

// resolveImpersonationTarget returns the service account to impersonate: an
// explicit flag value wins, else the gcloud-compatible env var, else "".
func resolveImpersonationTarget(flagValue string) string {
	if flagValue != "" {
		return flagValue
	}
	return os.Getenv(impersonateEnvVar)
}

// NewADCTokenSource resolves Application Default Credentials for the
// cloud-platform scope. ADC is read from a read-only mount of the host's
// ~/.config/gcloud; the returned source refreshes access tokens in-process
// (using the refresh token in ADC), which is the whole point — it removes the
// 1-hour access-token expiry that makes Google's remote MCP endpoints unusable
// for long sessions.
func NewADCTokenSource(ctx context.Context) (oauth2.TokenSource, error) {
	creds, err := google.FindDefaultCredentials(ctx, cloudPlatformScope)
	if err != nil {
		return nil, fmt.Errorf("could not load Application Default Credentials "+
			"(run `gcloud auth application-default login` on the host and mount ~/.config/gcloud): %w", err)
	}
	return creds.TokenSource, nil
}

// defaultIAMCredentialsEndpoint is the IAM Credentials API base used to mint
// impersonated access tokens.
const defaultIAMCredentialsEndpoint = "https://iamcredentials.googleapis.com"

// impersonatedTokenSource mints access tokens for a target service account by
// calling the IAM Credentials generateAccessToken API with a base credential.
// The base credential (ADC) must hold roles/iam.serviceAccountTokenCreator on
// the target service account.
type impersonatedTokenSource struct {
	base       oauth2.TokenSource
	targetSA   string
	scopes     []string
	endpoint   string
	httpClient *http.Client
}

// newImpersonatedTokenSource wraps base so that every token is minted for
// targetSA. The result is wrapped in a ReuseTokenSource so a fresh token is
// only requested when the current one is near expiry.
func newImpersonatedTokenSource(base oauth2.TokenSource, targetSA string, httpClient *http.Client) oauth2.TokenSource {
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	return oauth2.ReuseTokenSource(nil, &impersonatedTokenSource{
		base:       base,
		targetSA:   targetSA,
		scopes:     []string{cloudPlatformScope},
		endpoint:   defaultIAMCredentialsEndpoint,
		httpClient: httpClient,
	})
}

// generateAccessTokenResponse is the IAM Credentials API response shape.
type generateAccessTokenResponse struct {
	AccessToken string `json:"accessToken"`
	ExpireTime  string `json:"expireTime"`
}

// Token mints a fresh impersonated access token.
func (s *impersonatedTokenSource) Token() (*oauth2.Token, error) {
	baseTok, err := s.base.Token()
	if err != nil {
		return nil, fmt.Errorf("failed to obtain base credentials for impersonation: %w", err)
	}

	reqURL := fmt.Sprintf("%s/v1/projects/-/serviceAccounts/%s:generateAccessToken",
		s.endpoint, url.PathEscape(s.targetSA))
	payload, err := json.Marshal(map[string]any{"scope": s.scopes})
	if err != nil {
		return nil, fmt.Errorf("failed to marshal impersonation request: %w", err)
	}

	req, err := http.NewRequest(http.MethodPost, reqURL, bytes.NewReader(payload))
	if err != nil {
		return nil, fmt.Errorf("failed to create impersonation request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+baseTok.AccessToken)
	req.Header.Set("Content-Type", "application/json")

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("impersonation request failed: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes))
	if err != nil {
		return nil, fmt.Errorf("failed to read impersonation response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("impersonation of %s failed (HTTP %d): %s", s.targetSA, resp.StatusCode, string(respBody))
	}

	var out generateAccessTokenResponse
	if err := json.Unmarshal(respBody, &out); err != nil {
		return nil, fmt.Errorf("failed to parse impersonation response: %w", err)
	}
	if out.AccessToken == "" {
		return nil, fmt.Errorf("impersonation of %s returned an empty access token", s.targetSA)
	}

	tok := &oauth2.Token{AccessToken: out.AccessToken}
	if out.ExpireTime != "" {
		exp, err := time.Parse(time.RFC3339, out.ExpireTime)
		if err != nil {
			return nil, fmt.Errorf("failed to parse token expiry %q: %w", out.ExpireTime, err)
		}
		tok.Expiry = exp
	}
	return tok, nil
}

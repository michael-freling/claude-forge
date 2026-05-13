package gateway

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// Operation describes a single GitHub API operation exposed by the gateway.
type Operation struct {
	Name        string `json:"name"`
	Method      string `json:"method"`
	Path        string `json:"path"`
	Description string `json:"description"`
	Type        string `json:"type"` // "read" or "write"
}

// SchemaResponse is the JSON response returned by GET /api/schema.
type SchemaResponse struct {
	Operations []Operation `json:"operations"`
}

// operations is the list of supported GitHub API operations.
var operations = []Operation{
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
	{Name: "merge-pr", Method: "PUT", Path: "/repos/{owner}/{repo}/pulls/{number}/merge", Description: "Merge a pull request", Type: "write"},
}

// defaultGitHubAPIBaseURL is the default upstream base URL for GitHub API.
const defaultGitHubAPIBaseURL = "https://api.github.com"

// APIServer is the REST API server that forge-gh calls. It proxies GitHub API
// requests with policy enforcement.
type APIServer struct {
	config      ProxyConfig
	ghAuth      *GitHubAuth
	upstreamURL string // base URL for upstream GitHub API, defaults to https://api.github.com
	httpClient  *http.Client
}

// NewAPIServer creates a new API server with the given config and auth.
func NewAPIServer(config ProxyConfig, ghAuth *GitHubAuth) *APIServer {
	return &APIServer{
		config:      config,
		ghAuth:      ghAuth,
		upstreamURL: defaultGitHubAPIBaseURL,
		httpClient:  http.DefaultClient,
	}
}

// ServeHTTP routes requests to the appropriate handler.
func (s *APIServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	switch {
	case r.URL.Path == "/api/schema" && r.Method == http.MethodGet:
		s.handleSchema(w, r)
	case strings.HasPrefix(r.URL.Path, "/api/github/"):
		s.handleGitHubProxy(w, r)
	default:
		http.Error(w, "not found", http.StatusNotFound)
	}
}

// handleSchema returns the JSON schema of available operations.
func (s *APIServer) handleSchema(w http.ResponseWriter, _ *http.Request) {
	resp := SchemaResponse{Operations: operations}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

// handleGitHubProxy proxies requests to the GitHub API with policy enforcement.
func (s *APIServer) handleGitHubProxy(w http.ResponseWriter, r *http.Request) {
	// Strip the /api/github prefix to get the GitHub API path
	ghPath := strings.TrimPrefix(r.URL.Path, "/api/github")

	if !s.isAllowed(r.Method, ghPath) {
		http.Error(w, "forbidden: write access denied for this repository", http.StatusForbidden)
		return
	}

	s.forwardToGitHubAPI(w, r, ghPath)
}

// isAllowed checks whether a GitHub API request is permitted.
// GET requests are always allowed (read).
// POST/PUT/PATCH/DELETE: extract owner/repo from path, check against allowed.
func (s *APIServer) isAllowed(method, path string) bool {
	if method == http.MethodGet {
		return true
	}

	// Write operation: extract owner/repo and check
	owner, repo := extractOwnerRepo(path)
	if owner == "" || repo == "" {
		// Cannot determine target repo, deny by default
		return false
	}

	return strings.EqualFold(owner, s.config.AllowedOwner) &&
		strings.EqualFold(repo, s.config.AllowedRepo)
}

// extractOwnerRepo extracts the owner and repo from a GitHub API path.
// GitHub API paths follow patterns like /repos/{owner}/{repo}/...
func extractOwnerRepo(path string) (string, string) {
	// Trim leading slash
	path = strings.TrimPrefix(path, "/")

	parts := strings.Split(path, "/")
	// Look for "repos" segment followed by owner and repo
	for i := 0; i < len(parts)-2; i++ {
		if parts[i] == "repos" {
			return parts[i+1], parts[i+2]
		}
	}

	return "", ""
}

// forwardToGitHubAPI forwards a request to the GitHub API.
func (s *APIServer) forwardToGitHubAPI(w http.ResponseWriter, r *http.Request, ghPath string) {
	targetURL := s.upstreamURL + ghPath
	if r.URL.RawQuery != "" {
		targetURL += "?" + r.URL.RawQuery
	}

	upstreamReq, err := http.NewRequestWithContext(r.Context(), r.Method, targetURL, r.Body)
	if err != nil {
		http.Error(w, fmt.Sprintf("failed to create upstream request: %v", err), http.StatusInternalServerError)
		return
	}

	// Copy request headers
	for key, values := range r.Header {
		for _, value := range values {
			upstreamReq.Header.Add(key, value)
		}
	}

	// Set GitHub API headers
	upstreamReq.Header.Set("Authorization", "Bearer "+s.ghAuth.Token())
	upstreamReq.Header.Set("Accept", "application/vnd.github+json")
	upstreamReq.Header.Set("X-GitHub-Api-Version", "2022-11-28")

	resp, err := s.httpClient.Do(upstreamReq)
	if err != nil {
		http.Error(w, fmt.Sprintf("failed to contact GitHub API: %v", err), http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()

	// Copy response headers
	for key, values := range resp.Header {
		for _, value := range values {
			w.Header().Add(key, value)
		}
	}
	w.WriteHeader(resp.StatusCode)
	io.Copy(w, resp.Body)
}

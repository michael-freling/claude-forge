package forgegh

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
)

// Client is the forge-gh client that runs inside the agent container,
// aliased as `gh`. It queries the gateway's API server for schema discovery
// and translates gh-style commands to API calls.
type Client struct {
	gatewayURL string // e.g. http://gateway:8083
	httpClient *http.Client
	stdout     io.Writer
	stderr     io.Writer
}

// NewClient creates a new forge-gh client targeting the given gateway URL.
func NewClient(gatewayURL string) *Client {
	return &Client{
		gatewayURL: strings.TrimRight(gatewayURL, "/"),
		httpClient: http.DefaultClient,
		stdout:     os.Stdout,
		stderr:     os.Stderr,
	}
}

// schemaResponse mirrors the gateway's schema response.
type schemaResponse struct {
	Operations []operation `json:"operations"`
}

// operation describes a single GitHub API operation.
type operation struct {
	Name        string `json:"name"`
	Method      string `json:"method"`
	Path        string `json:"path"`
	Description string `json:"description"`
	Type        string `json:"type"`
}

// parsedCommand is the result of parsing gh-style CLI arguments.
type parsedCommand struct {
	Entity string            // pr, issue, repo, release, run
	Action string            // list, view, create, comment, merge, etc.
	Number string            // positional number arg (e.g. PR or issue number)
	Flags  map[string]string // --key value pairs
}

// Run executes a forge-gh command by:
// 1. Parsing the gh-style arguments
// 2. Resolving the target owner/repo
// 3. Fetching the schema from the gateway
// 4. Mapping the command to an API operation
// 5. Executing the API call
// 6. Printing the result
func (c *Client) Run(args []string) error {
	cmd, err := parseArgs(args)
	if err != nil {
		return err
	}

	owner, repo := c.resolveOwnerRepo(cmd.Flags)

	schema, err := c.fetchSchema()
	if err != nil {
		return fmt.Errorf("failed to fetch schema: %w", err)
	}

	op, err := mapCommand(cmd, schema)
	if err != nil {
		return err
	}

	return c.executeOperation(op, cmd, owner, repo)
}

// parseArgs parses gh-style CLI arguments into a parsedCommand.
// Format: <entity> <action> [number] [--flags...]
func parseArgs(args []string) (*parsedCommand, error) {
	if len(args) < 2 {
		return nil, fmt.Errorf("usage: gh <entity> <action> [number] [--flags]")
	}

	cmd := &parsedCommand{
		Entity: args[0],
		Action: args[1],
		Flags:  make(map[string]string),
	}

	remaining := args[2:]
	flagsParsed := false

	for i := 0; i < len(remaining); i++ {
		arg := remaining[i]

		if strings.HasPrefix(arg, "--") {
			flagsParsed = true
			key := strings.TrimPrefix(arg, "--")
			if i+1 < len(remaining) && !strings.HasPrefix(remaining[i+1], "--") {
				cmd.Flags[key] = remaining[i+1]
				i++
			} else {
				cmd.Flags[key] = ""
			}
		} else if !flagsParsed && cmd.Number == "" {
			// First non-flag arg after action is the number
			cmd.Number = arg
		}
	}

	return cmd, nil
}

// resolveOwnerRepo determines the owner/repo for the command.
// Priority: --repo flag > environment variables.
func (c *Client) resolveOwnerRepo(flags map[string]string) (string, string) {
	if repoFlag, ok := flags["repo"]; ok {
		parts := strings.SplitN(repoFlag, "/", 2)
		if len(parts) == 2 {
			return parts[0], parts[1]
		}
	}

	owner := os.Getenv("FORGE_PROJECT_OWNER")
	repo := os.Getenv("FORGE_PROJECT_REPO")
	return owner, repo
}

// fetchSchema retrieves the operation schema from the gateway.
func (c *Client) fetchSchema() (*schemaResponse, error) {
	resp, err := c.httpClient.Get(c.gatewayURL + "/api/schema")
	if err != nil {
		return nil, fmt.Errorf("failed to connect to gateway: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("schema request failed with status %d: %s", resp.StatusCode, string(body))
	}

	var schema schemaResponse
	if err := json.NewDecoder(resp.Body).Decode(&schema); err != nil {
		return nil, fmt.Errorf("failed to decode schema: %w", err)
	}

	return &schema, nil
}

// mapCommand maps a parsed command to an API operation from the schema.
func mapCommand(cmd *parsedCommand, schema *schemaResponse) (*operation, error) {
	opName := commandToOperationName(cmd)

	for _, op := range schema.Operations {
		if op.Name == opName {
			return &op, nil
		}
	}

	return nil, fmt.Errorf("unknown command: gh %s %s (no matching operation %q)", cmd.Entity, cmd.Action, opName)
}

// commandToOperationName maps a gh entity+action to an operation name.
func commandToOperationName(cmd *parsedCommand) string {
	switch cmd.Entity {
	case "pr":
		switch cmd.Action {
		case "list":
			return "list-prs"
		case "view":
			return "get-pr"
		case "create":
			return "create-pr"
		case "edit":
			return "update-pr"
		case "merge":
			return "merge-pr"
		case "comment":
			return "create-pr-comment"
		}
	case "issue":
		switch cmd.Action {
		case "list":
			return "list-issues"
		case "view":
			return "get-issue"
		case "create":
			return "create-issue"
		case "comment":
			return "create-issue-comment"
		}
	case "repo":
		switch cmd.Action {
		case "view":
			return "get-repo"
		}
	case "release":
		switch cmd.Action {
		case "list":
			return "list-releases"
		}
	case "run":
		switch cmd.Action {
		case "list":
			return "list-workflow-runs"
		case "view":
			if cmd.Flags["job"] != "" {
				return "get-workflow-run-job-logs"
			}
			return "get-workflow-run"
		}
	}
	return cmd.Entity + "-" + cmd.Action
}

// executeOperation builds and sends the HTTP request to the gateway.
func (c *Client) executeOperation(op *operation, cmd *parsedCommand, owner, repo string) error {
	if owner == "" || repo == "" {
		return fmt.Errorf("owner and repo are required; use --repo owner/repo or set FORGE_PROJECT_OWNER and FORGE_PROJECT_REPO")
	}

	// Build the API path by substituting path parameters
	apiPath := op.Path
	apiPath = strings.ReplaceAll(apiPath, "{owner}", owner)
	apiPath = strings.ReplaceAll(apiPath, "{repo}", repo)

	if cmd.Number != "" {
		apiPath = strings.ReplaceAll(apiPath, "{number}", cmd.Number)
		apiPath = strings.ReplaceAll(apiPath, "{ref}", cmd.Number)
		apiPath = strings.ReplaceAll(apiPath, "{run_id}", cmd.Number)
	}
	if jobID, ok := cmd.Flags["job"]; ok && jobID != "" {
		apiPath = strings.ReplaceAll(apiPath, "{job_id}", jobID)
	}

	url := c.gatewayURL + "/api/github" + apiPath

	// Build query params for GET requests
	queryParams := c.buildQueryParams(cmd)
	if queryParams != "" {
		url += "?" + queryParams
	}

	// Build request body for write operations
	var body io.Reader
	if op.Method != http.MethodGet {
		bodyJSON := c.buildRequestBody(cmd, op)
		if bodyJSON != "" {
			body = strings.NewReader(bodyJSON)
		}
	}

	req, err := http.NewRequest(op.Method, url, body)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("failed to execute request: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("failed to read response: %w", err)
	}

	if resp.StatusCode >= 400 {
		return fmt.Errorf("request failed with status %d: %s", resp.StatusCode, string(respBody))
	}

	// Pretty-print JSON if possible
	var pretty json.RawMessage
	if json.Unmarshal(respBody, &pretty) == nil {
		formatted, err := json.MarshalIndent(pretty, "", "  ")
		if err == nil {
			fmt.Fprintln(c.stdout, string(formatted))
			return nil
		}
	}

	fmt.Fprintln(c.stdout, string(respBody))
	return nil
}

// buildQueryParams builds URL query parameters for GET requests.
func (c *Client) buildQueryParams(cmd *parsedCommand) string {
	var params []string

	// Map common flags to query params
	for _, key := range []string{"state", "per_page", "page", "sort", "direction", "branch", "status", "event"} {
		if val, ok := cmd.Flags[key]; ok && val != "" {
			params = append(params, key+"="+val)
		}
	}

	// --limit maps to per_page for GitHub API
	if val, ok := cmd.Flags["limit"]; ok && val != "" {
		params = append(params, "per_page="+val)
	}

	return strings.Join(params, "&")
}

// buildRequestBody builds a JSON request body from command flags.
func (c *Client) buildRequestBody(cmd *parsedCommand, op *operation) string {
	bodyMap := make(map[string]any)

	switch op.Name {
	case "create-pr":
		if v, ok := cmd.Flags["title"]; ok {
			bodyMap["title"] = v
		}
		if v, ok := cmd.Flags["body"]; ok {
			bodyMap["body"] = v
		}
		if v, ok := cmd.Flags["head"]; ok {
			bodyMap["head"] = v
		}
		if v, ok := cmd.Flags["base"]; ok {
			bodyMap["base"] = v
		}
	case "update-pr":
		if v, ok := cmd.Flags["title"]; ok {
			bodyMap["title"] = v
		}
		if v, ok := cmd.Flags["body"]; ok {
			bodyMap["body"] = v
		}
		if v, ok := cmd.Flags["base"]; ok {
			bodyMap["base"] = v
		}
		if v, ok := cmd.Flags["state"]; ok {
			bodyMap["state"] = v
		}
	case "create-issue":
		if v, ok := cmd.Flags["title"]; ok {
			bodyMap["title"] = v
		}
		if v, ok := cmd.Flags["body"]; ok {
			bodyMap["body"] = v
		}
	case "create-pr-comment", "create-issue-comment":
		if v, ok := cmd.Flags["body"]; ok {
			bodyMap["body"] = v
		}
	case "merge-pr":
		if v, ok := cmd.Flags["method"]; ok {
			bodyMap["merge_method"] = v
		}
		if v, ok := cmd.Flags["subject"]; ok {
			bodyMap["commit_title"] = v
		}
	}

	if len(bodyMap) == 0 {
		return "{}"
	}

	data, err := json.Marshal(bodyMap)
	if err != nil {
		return "{}"
	}
	return string(data)
}

// resolveNumber ensures the number is a valid integer string.
func resolveNumber(s string) (string, error) {
	if s == "" {
		return "", fmt.Errorf("number argument is required")
	}
	if _, err := strconv.Atoi(s); err != nil {
		return "", fmt.Errorf("invalid number %q: %w", s, err)
	}
	return s, nil
}

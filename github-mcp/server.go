package main

import (
	"encoding/json"
	"io"
	"log"
	"net/http"
)

// jsonRPCRequest represents a JSON-RPC 2.0 request.
type jsonRPCRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

// jsonRPCResponse represents a JSON-RPC 2.0 response.
type jsonRPCResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Result  any             `json:"result,omitempty"`
	Error   *jsonRPCError   `json:"error,omitempty"`
}

// jsonRPCError represents a JSON-RPC 2.0 error.
type jsonRPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// MCP content types.
type mcpContent struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

type mcpToolResult struct {
	Content []mcpContent `json:"content"`
	IsError bool         `json:"isError,omitempty"`
}

// Server implements the MCP Streamable HTTP protocol for GitHub API operations.
type Server struct {
	owner  string
	repo   string
	policy *Policy
	github *GitHubClient
}

// NewServer creates a new MCP server.
func NewServer(owner, repo string, policy *Policy, github *GitHubClient) *Server {
	return &Server{
		owner:  owner,
		repo:   repo,
		policy: policy,
		github: github,
	}
}

// ServeHTTP handles HTTP requests to the MCP endpoint.
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusMethodNotAllowed)
		json.NewEncoder(w).Encode(jsonRPCResponse{
			JSONRPC: "2.0",
			Error: &jsonRPCError{
				Code:    -32600,
				Message: "only POST is supported",
			},
		})
		return
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(jsonRPCResponse{
			JSONRPC: "2.0",
			Error: &jsonRPCError{
				Code:    -32700,
				Message: "failed to read request body",
			},
		})
		return
	}

	var req jsonRPCRequest
	if err := json.Unmarshal(body, &req); err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(jsonRPCResponse{
			JSONRPC: "2.0",
			Error: &jsonRPCError{
				Code:    -32700,
				Message: "parse error: invalid JSON",
			},
		})
		return
	}

	resp := s.handleRequest(r, &req)
	if resp == nil {
		// Notification - no response needed (e.g., notifications/initialized)
		w.WriteHeader(http.StatusNoContent)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(resp)
}

// handleRequest dispatches a JSON-RPC request to the appropriate handler.
func (s *Server) handleRequest(r *http.Request, req *jsonRPCRequest) *jsonRPCResponse {
	switch req.Method {
	case "initialize":
		return s.handleInitialize(req)
	case "notifications/initialized":
		// No-op notification, no response
		return nil
	case "tools/list":
		return s.handleToolsList(req)
	case "tools/call":
		return s.handleToolsCall(r, req)
	default:
		return &jsonRPCResponse{
			JSONRPC: "2.0",
			ID:      req.ID,
			Error: &jsonRPCError{
				Code:    -32601,
				Message: "method not found",
			},
		}
	}
}

// handleInitialize returns MCP server info and capabilities.
func (s *Server) handleInitialize(req *jsonRPCRequest) *jsonRPCResponse {
	return &jsonRPCResponse{
		JSONRPC: "2.0",
		ID:      req.ID,
		Result: map[string]any{
			"protocolVersion": "2025-03-26",
			"capabilities":    map[string]any{"tools": map[string]any{}},
			"serverInfo": map[string]any{
				"name":    "github-mcp",
				"version": "1.0.0",
			},
		},
	}
}

// handleToolsList returns all available tool definitions.
func (s *Server) handleToolsList(req *jsonRPCRequest) *jsonRPCResponse {
	return &jsonRPCResponse{
		JSONRPC: "2.0",
		ID:      req.ID,
		Result: map[string]any{
			"tools": allToolDefinitions(),
		},
	}
}

// toolsCallParams represents the parameters for a tools/call request.
type toolsCallParams struct {
	Name      string         `json:"name"`
	Arguments map[string]any `json:"arguments"`
}

// handleToolsCall executes a tool and returns the result.
func (s *Server) handleToolsCall(r *http.Request, req *jsonRPCRequest) *jsonRPCResponse {
	var params toolsCallParams
	if req.Params != nil {
		if err := json.Unmarshal(req.Params, &params); err != nil {
			return &jsonRPCResponse{
				JSONRPC: "2.0",
				ID:      req.ID,
				Error: &jsonRPCError{
					Code:    -32602,
					Message: "invalid params: " + err.Error(),
				},
			}
		}
	}

	if params.Name == "" {
		return &jsonRPCResponse{
			JSONRPC: "2.0",
			ID:      req.ID,
			Error: &jsonRPCError{
				Code:    -32602,
				Message: "missing tool name",
			},
		}
	}

	if params.Arguments == nil {
		params.Arguments = map[string]any{}
	}

	result, isError, err := executeTool(r.Context(), params.Name, params.Arguments, s.owner, s.repo, s.policy, s.github)
	if err != nil {
		log.Printf("tool %s error: %v", params.Name, err)
		return &jsonRPCResponse{
			JSONRPC: "2.0",
			ID:      req.ID,
			Result: mcpToolResult{
				Content: []mcpContent{{Type: "text", Text: err.Error()}},
				IsError: true,
			},
		}
	}

	return &jsonRPCResponse{
		JSONRPC: "2.0",
		ID:      req.ID,
		Result: mcpToolResult{
			Content: []mcpContent{{Type: "text", Text: result}},
			IsError: isError,
		},
	}
}

package main

import (
	"net/http"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// Server wraps the MCP server with Docker-specific state.
type Server struct {
	docker      DockerClient
	tracker     *Tracker
	networkName string
	mcpServer   *mcp.Server
	handler     http.Handler
}

// NewServer creates a new MCP server with Docker tools registered.
func NewServer(dockerClient DockerClient, tracker *Tracker, networkName string) *Server {
	s := &Server{
		docker:      dockerClient,
		tracker:     tracker,
		networkName: networkName,
	}

	s.mcpServer = mcp.NewServer(
		&mcp.Implementation{
			Name:    "docker-mcp",
			Version: "1.0.0",
		},
		&mcp.ServerOptions{},
	)

	s.registerTools()

	s.handler = mcp.NewStreamableHTTPHandler(
		func(r *http.Request) *mcp.Server { return s.mcpServer },
		nil,
	)

	return s
}

// ServeHTTP delegates to the SDK's StreamableHTTP handler.
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.handler.ServeHTTP(w, r)
}

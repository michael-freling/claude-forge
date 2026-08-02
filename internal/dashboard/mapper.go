package dashboard

import (
	"time"

	dashboardv1 "github.com/michael-freling/claude-forge/internal/gen/dashboard/v1"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// toProtoResponse maps a gathered Go Dashboard snapshot to the generated
// GetDashboardResponse. A nil snapshot yields an empty response.
func toProtoResponse(d *Dashboard) *dashboardv1.GetDashboardResponse {
	if d == nil {
		return &dashboardv1.GetDashboardResponse{}
	}
	return &dashboardv1.GetDashboardResponse{
		Dashboard: toProtoDashboard(d),
	}
}

// toProtoDashboard maps the top-level snapshot.
func toProtoDashboard(d *Dashboard) *dashboardv1.Dashboard {
	pd := &dashboardv1.Dashboard{
		GeneratedAt: toTimestamp(d.GeneratedAt),
		Warnings:    d.Warnings,
		Global:      toProtoGlobal(d.Global),
	}
	for _, proj := range d.Projects {
		pd.Projects = append(pd.Projects, toProtoProject(proj))
	}
	return pd
}

// toProtoGlobal maps the shared, global-scope MCP servers.
func toProtoGlobal(g GlobalMCP) *dashboardv1.GlobalMcp {
	pg := &dashboardv1.GlobalMcp{}
	for _, s := range g.Servers {
		pg.Servers = append(pg.Servers, toProtoMCPServer(s))
	}
	return pg
}

// toProtoProject maps one project with its sessions and running sessions.
func toProtoProject(p Project) *dashboardv1.Project {
	pp := &dashboardv1.Project{
		Id:       p.ID,
		Dir:      p.Dir,
		Owner:    p.Owner,
		Repo:     p.Repo,
		Resolved: p.Resolved,
	}
	for _, s := range p.Sessions {
		pp.Sessions = append(pp.Sessions, toProtoSession(s))
	}
	for _, rs := range p.RunningSessions {
		pp.RunningSessions = append(pp.RunningSessions, toProtoRunningSession(rs))
	}
	return pp
}

// toProtoSession maps a recorded session, including its optional pull request.
func toProtoSession(s Session) *dashboardv1.Session {
	return &dashboardv1.Session{
		Id:           s.ID,
		Name:         s.Name,
		Worktree:     s.Worktree,
		Branch:       s.Branch,
		CreatedAt:    toTimestamp(s.CreatedAt),
		LastActive:   toTimestamp(s.LastActive),
		FirstMessage: s.FirstMessage,
		Pr:           toProtoPR(s.PR),
	}
}

// toProtoPR maps a pull request, or nil when the session has none.
func toProtoPR(pr *PR) *dashboardv1.PullRequest {
	if pr == nil {
		return nil
	}
	return &dashboardv1.PullRequest{
		Number: int32(pr.Number),
		Title:  pr.Title,
		State:  toPRState(pr.State),
		Url:    pr.URL,
		Draft:  pr.Draft,
	}
}

// toProtoRunningSession maps a live session and its session-scope MCP servers.
func toProtoRunningSession(rs RunningSession) *dashboardv1.RunningSession {
	prs := &dashboardv1.RunningSession{ShortId: rs.ShortID, ClaudeSessionId: rs.ClaudeSessionID}
	for _, s := range rs.MCPServers {
		prs.McpServers = append(prs.McpServers, toProtoMCPServer(s))
	}
	return prs
}

// toProtoMCPServer maps one MCP server's identity and runtime status.
func toProtoMCPServer(s MCPServer) *dashboardv1.McpServer {
	return &dashboardv1.McpServer{
		Name:      s.Name,
		Scope:     toScope(s.Scope),
		Kind:      toKind(s.Kind),
		Container: s.Container,
		Image:     s.Image,
		Status:    s.Status,
		Running:   s.Running,
	}
}

// toScope maps a string MCP scope to its enum, defaulting to UNSPECIFIED.
func toScope(s string) dashboardv1.McpScope {
	switch s {
	case "session":
		return dashboardv1.McpScope_MCP_SCOPE_SESSION
	case "global":
		return dashboardv1.McpScope_MCP_SCOPE_GLOBAL
	case "builtin":
		return dashboardv1.McpScope_MCP_SCOPE_BUILTIN
	default:
		return dashboardv1.McpScope_MCP_SCOPE_UNSPECIFIED
	}
}

// toKind maps a string MCP kind to its enum, defaulting to UNSPECIFIED.
func toKind(s string) dashboardv1.McpKind {
	switch s {
	case "container":
		return dashboardv1.McpKind_MCP_KIND_CONTAINER
	case "stdio":
		return dashboardv1.McpKind_MCP_KIND_STDIO
	case "remote":
		return dashboardv1.McpKind_MCP_KIND_REMOTE
	default:
		return dashboardv1.McpKind_MCP_KIND_UNSPECIFIED
	}
}

// toPRState maps a string pull-request state to its enum, defaulting to
// UNSPECIFIED.
func toPRState(s string) dashboardv1.PullRequestState {
	switch s {
	case "open":
		return dashboardv1.PullRequestState_PULL_REQUEST_STATE_OPEN
	case "closed":
		return dashboardv1.PullRequestState_PULL_REQUEST_STATE_CLOSED
	case "merged":
		return dashboardv1.PullRequestState_PULL_REQUEST_STATE_MERGED
	default:
		return dashboardv1.PullRequestState_PULL_REQUEST_STATE_UNSPECIFIED
	}
}

// toTimestamp parses an RFC3339 time string into a protobuf Timestamp. An empty
// or unparseable string yields nil (the field is simply omitted).
func toTimestamp(s string) *timestamppb.Timestamp {
	if s == "" {
		return nil
	}
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		return nil
	}
	return timestamppb.New(t)
}

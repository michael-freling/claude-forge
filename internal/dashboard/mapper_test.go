package dashboard

import (
	"testing"

	dashboardv1 "github.com/michael-freling/claude-forge/internal/gen/dashboard/v1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestToProtoResponse_Nil(t *testing.T) {
	resp := toProtoResponse(nil)
	require.NotNil(t, resp)
	assert.Nil(t, resp.GetDashboard())
}

func TestToProtoResponse_Full(t *testing.T) {
	dash := &Dashboard{
		GeneratedAt: "2025-01-15T10:00:00Z",
		Warnings:    []string{"w1", "w2"},
		Global: GlobalMCP{Servers: []MCPServer{
			{Name: "kubernetes", Scope: "global", Kind: "container", Container: "forge-k8s-mcp", Image: "img", Status: "Up 2m", Running: true},
			{Name: "remote-one", Scope: "global", Kind: "remote", Status: "n/a"},
		}},
		Projects: []Project{
			{
				ID:       "-home-user-proj",
				Dir:      "/home/user/proj",
				Owner:    "acme",
				Repo:     "proj",
				Resolved: true,
				Sessions: []Session{
					{
						ID:           "sid-1",
						Name:         "session one",
						Worktree:     "wt-1",
						Branch:       "feature/x",
						CreatedAt:    "2025-01-15T09:00:00Z",
						LastActive:   "2025-01-15T09:30:00Z",
						FirstMessage: "hello",
						PR: &PR{
							Number: 42,
							Title:  "Add feature",
							State:  "open",
							URL:    "https://example.com/pr/42",
							Draft:  true,
						},
					},
					{ID: "sid-2", Name: "no pr", PR: nil},
				},
				RunningSessions: []RunningSession{
					{
						ShortID: "abcd1234",
						MCPServers: []MCPServer{
							{Name: "github", Scope: "session", Kind: "container", Container: "c", Image: "i", Status: "Up", Running: true},
							{Name: "local", Scope: "session", Kind: "stdio", Status: "n/a"},
						},
					},
				},
			},
		},
	}

	resp := toProtoResponse(dash)
	pd := resp.GetDashboard()
	require.NotNil(t, pd)

	require.NotNil(t, pd.GetGeneratedAt())
	assert.Equal(t, int64(1736935200), pd.GetGeneratedAt().GetSeconds())
	assert.Equal(t, []string{"w1", "w2"}, pd.GetWarnings())

	require.Len(t, pd.GetGlobal().GetServers(), 2)
	g0 := pd.GetGlobal().GetServers()[0]
	assert.Equal(t, "kubernetes", g0.GetName())
	assert.Equal(t, dashboardv1.McpScope_MCP_SCOPE_GLOBAL, g0.GetScope())
	assert.Equal(t, dashboardv1.McpKind_MCP_KIND_CONTAINER, g0.GetKind())
	assert.Equal(t, "forge-k8s-mcp", g0.GetContainer())
	assert.Equal(t, "img", g0.GetImage())
	assert.Equal(t, "Up 2m", g0.GetStatus())
	assert.True(t, g0.GetRunning())
	assert.Equal(t, dashboardv1.McpKind_MCP_KIND_REMOTE, pd.GetGlobal().GetServers()[1].GetKind())

	require.Len(t, pd.GetProjects(), 1)
	p := pd.GetProjects()[0]
	assert.Equal(t, "-home-user-proj", p.GetId())
	assert.Equal(t, "/home/user/proj", p.GetDir())
	assert.Equal(t, "acme", p.GetOwner())
	assert.Equal(t, "proj", p.GetRepo())
	assert.True(t, p.GetResolved())

	require.Len(t, p.GetSessions(), 2)
	s0 := p.GetSessions()[0]
	assert.Equal(t, "sid-1", s0.GetId())
	assert.Equal(t, "session one", s0.GetName())
	assert.Equal(t, "wt-1", s0.GetWorktree())
	assert.Equal(t, "feature/x", s0.GetBranch())
	assert.Equal(t, "hello", s0.GetFirstMessage())
	require.NotNil(t, s0.GetCreatedAt())
	require.NotNil(t, s0.GetLastActive())

	pr := s0.GetPr()
	require.NotNil(t, pr)
	assert.Equal(t, int32(42), pr.GetNumber())
	assert.Equal(t, "Add feature", pr.GetTitle())
	assert.Equal(t, dashboardv1.PullRequestState_PULL_REQUEST_STATE_OPEN, pr.GetState())
	assert.Equal(t, "https://example.com/pr/42", pr.GetUrl())
	assert.True(t, pr.GetDraft())

	// A session with no PR maps to a nil proto PR.
	assert.Nil(t, p.GetSessions()[1].GetPr())

	require.Len(t, p.GetRunningSessions(), 1)
	rs := p.GetRunningSessions()[0]
	assert.Equal(t, "abcd1234", rs.GetShortId())
	require.Len(t, rs.GetMcpServers(), 2)
	assert.Equal(t, dashboardv1.McpScope_MCP_SCOPE_SESSION, rs.GetMcpServers()[0].GetScope())
	assert.Equal(t, dashboardv1.McpKind_MCP_KIND_STDIO, rs.GetMcpServers()[1].GetKind())
}

func TestToScope(t *testing.T) {
	cases := map[string]dashboardv1.McpScope{
		"session": dashboardv1.McpScope_MCP_SCOPE_SESSION,
		"global":  dashboardv1.McpScope_MCP_SCOPE_GLOBAL,
		"builtin": dashboardv1.McpScope_MCP_SCOPE_BUILTIN,
		"":        dashboardv1.McpScope_MCP_SCOPE_UNSPECIFIED,
		"bogus":   dashboardv1.McpScope_MCP_SCOPE_UNSPECIFIED,
	}
	for in, want := range cases {
		assert.Equalf(t, want, toScope(in), "scope %q", in)
	}
}

func TestToKind(t *testing.T) {
	cases := map[string]dashboardv1.McpKind{
		"container": dashboardv1.McpKind_MCP_KIND_CONTAINER,
		"stdio":     dashboardv1.McpKind_MCP_KIND_STDIO,
		"remote":    dashboardv1.McpKind_MCP_KIND_REMOTE,
		"":          dashboardv1.McpKind_MCP_KIND_UNSPECIFIED,
		"weird":     dashboardv1.McpKind_MCP_KIND_UNSPECIFIED,
	}
	for in, want := range cases {
		assert.Equalf(t, want, toKind(in), "kind %q", in)
	}
}

func TestToPRState(t *testing.T) {
	cases := map[string]dashboardv1.PullRequestState{
		"open":    dashboardv1.PullRequestState_PULL_REQUEST_STATE_OPEN,
		"closed":  dashboardv1.PullRequestState_PULL_REQUEST_STATE_CLOSED,
		"merged":  dashboardv1.PullRequestState_PULL_REQUEST_STATE_MERGED,
		"":        dashboardv1.PullRequestState_PULL_REQUEST_STATE_UNSPECIFIED,
		"unknown": dashboardv1.PullRequestState_PULL_REQUEST_STATE_UNSPECIFIED,
	}
	for in, want := range cases {
		assert.Equalf(t, want, toPRState(in), "state %q", in)
	}
}

func TestToTimestamp(t *testing.T) {
	// Empty string -> nil.
	assert.Nil(t, toTimestamp(""))

	// Unparseable -> nil.
	assert.Nil(t, toTimestamp("not-a-time"))

	// Valid RFC3339 -> parsed timestamp.
	ts := toTimestamp("2025-01-15T10:00:00Z")
	require.NotNil(t, ts)
	assert.Equal(t, int64(1736935200), ts.GetSeconds())
}

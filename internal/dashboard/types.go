package dashboard

// Dashboard is the top-level snapshot the provider gathers; the server maps it
// to the generated proto and serves it via the DashboardService.GetDashboard RPC.
type Dashboard struct {
	// GeneratedAt is when the snapshot was built, in RFC3339.
	GeneratedAt string `json:"generatedAt"`
	// Warnings collects non-fatal problems encountered while gathering data
	// (an unresolved project directory, an unreachable GitHub token, etc.).
	Warnings []string `json:"warnings"`
	// Global holds the shared, cross-session MCP servers.
	Global GlobalMCP `json:"global"`
	// Projects is one entry per claude-forge project directory.
	Projects []Project `json:"projects"`
}

// GlobalMCP describes the shared (global-scope) MCP servers.
type GlobalMCP struct {
	Servers []MCPServer `json:"servers"`
}

// Project is a single claude-forge project: its identity plus its sessions and
// any currently running sessions.
type Project struct {
	// ID is the encoded host directory path used as the ~/.claude-forge subdir.
	ID string `json:"id"`
	// Dir is the resolved absolute host directory, empty when unresolved.
	Dir string `json:"dir"`
	// Owner and Repo come from the git remote, empty when unresolved.
	Owner string `json:"owner"`
	Repo  string `json:"repo"`
	// Resolved reports whether the host directory was located and identified.
	Resolved bool `json:"resolved"`
	// Sessions lists the recorded sessions, most recent first.
	Sessions []Session `json:"sessions"`
	// RunningSessions lists the sessions with live containers.
	RunningSessions []RunningSession `json:"runningSessions"`
}

// Session is a recorded claude-forge session for a project.
type Session struct {
	ID           string `json:"id"`
	Name         string `json:"name"`
	Worktree     string `json:"worktree"`
	Branch       string `json:"branch"`
	CreatedAt    string `json:"createdAt"`
	LastActive   string `json:"lastActive"`
	FirstMessage string `json:"firstMessage"`
	// PR is the pull request for the session's branch, if one exists.
	PR *PR `json:"pr"`
}

// PR is a GitHub pull request summary for a session's branch.
type PR struct {
	Number int    `json:"number"`
	Title  string `json:"title"`
	// State is "open", "closed", or "merged".
	State string `json:"state"`
	URL   string `json:"url"`
	Draft bool   `json:"draft"`
}

// RunningSession is a session with live containers, keyed by its short ID (the
// 8-hex suffix of its container names), together with its session-scope MCP
// servers.
type RunningSession struct {
	ShortID    string      `json:"shortId"`
	MCPServers []MCPServer `json:"mcpServers"`
	// ClaudeSessionID is the Claude session UUID recovered from the agent
	// container's command line (--session-id or --resume). Empty when the
	// session was started with --continue or the args were not inspectable.
	// It is the join key to Session.ID: the container short id and the Claude
	// session id are independent random values.
	ClaudeSessionID string `json:"claudeSessionId"`
}

// MCPServer describes one MCP server's identity and runtime status.
type MCPServer struct {
	Name string `json:"name"`
	// Scope is "session", "global", or "builtin".
	Scope string `json:"scope"`
	// Kind is "container", "stdio", or "remote".
	Kind string `json:"kind"`
	// Container is the Docker container name, empty for stdio/remote servers.
	Container string `json:"container"`
	Image     string `json:"image"`
	// Status is the Docker status string (e.g. "Up 3 minutes") or "not running".
	Status string `json:"status"`
	// Running reports whether the backing container is up.
	Running bool `json:"running"`
}

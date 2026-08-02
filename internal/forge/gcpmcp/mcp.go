// Package gcpmcp holds the launch details for the first-party GCP MCP server,
// shared between the orchestrator (which starts the container) and the E2E test
// (which verifies the image accepts our flags) so the two cannot drift.
package gcpmcp

const (
	// MCPServerPort is the container port the GCP MCP server listens on. It
	// must not collide with the GitHub MCP (8083) or Kubernetes MCP (8090)
	// ports on the shared network.
	MCPServerPort = "8084"

	// MCPServerGcloudDir is where the host's ~/.config/gcloud is bind-mounted
	// (read-only) inside the container. HOME is set to its parent so the
	// google.golang.org/x/oauth2/google ADC lookup finds
	// application_default_credentials.json at the well-known path.
	MCPServerGcloudDir = "/home/user/.config/gcloud"

	// MCPServerHome is the container HOME; the ADC lookup joins it with
	// .config/gcloud, so it must be the parent of MCPServerGcloudDir.
	MCPServerHome = "/home/user"
)

// Options are the configuration values that shape the server's launch flags.
type Options struct {
	Project                   string
	ImpersonateServiceAccount string
	QuotaProject              string
	AllowWrites               bool
	AllowSecretAccess         bool
}

// MCPServerArgs builds the command-line flags for the GCP MCP container. Both
// gates default closed: --allow-writes and --allow-secret-access are only
// appended when explicitly enabled.
func MCPServerArgs(o Options) []string {
	args := []string{"--addr", ":" + MCPServerPort}
	if o.Project != "" {
		args = append(args, "--project", o.Project)
	}
	if o.ImpersonateServiceAccount != "" {
		args = append(args, "--impersonate-service-account", o.ImpersonateServiceAccount)
	}
	if o.QuotaProject != "" {
		args = append(args, "--quota-project", o.QuotaProject)
	}
	if o.AllowWrites {
		args = append(args, "--allow-writes")
	}
	if o.AllowSecretAccess {
		args = append(args, "--allow-secret-access")
	}
	return args
}

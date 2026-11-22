package openshell

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// LabelKey marks the sandboxes claude-forge creates, so list only shows them.
const LabelKey = "claude-forge"

// Runner runs the openshell CLI.
type Runner interface {
	// Output runs openshell with args and extra environment variables and
	// returns its combined output.
	Output(ctx context.Context, env []string, args ...string) ([]byte, error)
	// Interactive runs openshell with the terminal attached.
	Interactive(ctx context.Context, args ...string) error
}

// ExecRunner runs the openshell binary. Gateway, when set, selects the
// gateway through OPENSHELL_GATEWAY.
type ExecRunner struct {
	Binary  string
	Gateway string
}

func (r ExecRunner) command(ctx context.Context, env []string, args []string) *exec.Cmd {
	bin := r.Binary
	if bin == "" {
		bin = "openshell"
	}
	cmd := exec.CommandContext(ctx, bin, args...)
	cmd.Env = append(os.Environ(), env...)
	if r.Gateway != "" {
		cmd.Env = append(cmd.Env, "OPENSHELL_GATEWAY="+r.Gateway)
	}
	return cmd
}

// Output implements Runner.
func (r ExecRunner) Output(ctx context.Context, env []string, args ...string) ([]byte, error) {
	cmd := r.command(ctx, env, args)
	cmd.Stdin = nil
	out, err := cmd.CombinedOutput()
	if err != nil {
		return out, fmt.Errorf("openshell %s: %w: %s", strings.Join(args[:min(len(args), 2)], " "), err, strings.TrimSpace(string(out)))
	}
	return out, nil
}

// Interactive implements Runner.
func (r ExecRunner) Interactive(ctx context.Context, args ...string) error {
	cmd := r.command(ctx, nil, args)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	return cmd.Run()
}

// Client wraps the openshell CLI operations claude-forge uses.
type Client struct {
	Runner  Runner
	TempDir string // where generated files are written; defaults to os.TempDir()
}

func (c *Client) writeTemp(pattern string, data []byte) (string, func(), error) {
	f, err := os.CreateTemp(c.TempDir, pattern)
	if err != nil {
		return "", nil, fmt.Errorf("failed to create temp file: %w", err)
	}
	path := f.Name()
	cleanup := func() { _ = os.Remove(path) }
	if _, err := f.Write(data); err != nil {
		f.Close()
		cleanup()
		return "", nil, fmt.Errorf("failed to write %s: %w", path, err)
	}
	if err := f.Close(); err != nil {
		cleanup()
		return "", nil, fmt.Errorf("failed to write %s: %w", path, err)
	}
	return path, cleanup, nil
}

// EnsureProfile imports the profile, or updates it if it already exists, so
// config changes (such as openshell.github.push_repos) reach new sandboxes.
// Sandboxes that are already running keep the profile they started with.
func (c *Client) EnsureProfile(ctx context.Context, p *Profile) error {
	update := *p
	args := []string{"profile", "import"}
	if current, err := c.Runner.Output(ctx, nil, "profile", "export", p.ID); err == nil {
		// An update must name the version it replaces.
		var existing struct {
			ResourceVersion uint64 `yaml:"resource_version"`
		}
		if err := yaml.Unmarshal(current, &existing); err != nil || existing.ResourceVersion == 0 {
			return fmt.Errorf("failed to read the version of profile %s from 'openshell profile export'", p.ID)
		}
		update.ResourceVersion = existing.ResourceVersion
		args = []string{"profile", "update", p.ID}
	}

	data, err := update.Render()
	if err != nil {
		return err
	}
	path, cleanup, err := c.writeTemp("claude-forge-profile-*.yaml", data)
	if err != nil {
		return err
	}
	defer cleanup()
	_, err = c.Runner.Output(ctx, nil, append(args, "-f", path)...)
	return err
}

// ProviderExists reports whether a provider instance exists.
func (c *Client) ProviderExists(ctx context.Context, name string) bool {
	_, err := c.Runner.Output(ctx, nil, "provider", "get", name)
	return err == nil
}

// SetProvider creates the provider, or replaces its credential if it exists.
// The secret is passed through the child's environment, never as an argument,
// so it does not show up in the process list.
func (c *Client) SetProvider(ctx context.Context, name, profileType, envKey, secret string) error {
	env := []string{envKey + "=" + secret}
	if out, err := c.Runner.Output(ctx, nil, "provider", "get", name); err == nil {
		if hasCredentialKey(string(out), envKey) {
			_, err := c.Runner.Output(ctx, env, "provider", "update", name, "--credential", envKey)
			return err
		}
		// A different credential (e.g. switching from an API key to a
		// subscription token): replace the provider so the old one is gone.
		if _, err := c.Runner.Output(ctx, nil, "provider", "delete", name); err != nil {
			return fmt.Errorf("failed to replace provider %s (delete the sandboxes using it first): %w", name, err)
		}
	}
	_, err := c.Runner.Output(ctx, env, "provider", "create", "--name", name, "--type", profileType, "--credential", envKey)
	return err
}

// hasCredentialKey reports whether `provider get` output lists key under
// "Credential keys:".
func hasCredentialKey(out, key string) bool {
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if rest, ok := strings.CutPrefix(line, "Credential keys:"); ok {
			for _, k := range strings.Split(rest, ",") {
				if strings.TrimSpace(k) == key {
					return true
				}
			}
		}
	}
	return false
}

// Sandbox is a sandbox as listed by the gateway.
type Sandbox struct {
	Name      string            `json:"name"`
	Phase     string            `json:"phase"`
	CreatedAt string            `json:"created_at"`
	Labels    map[string]string `json:"labels"`
}

// ErrNotFound is returned when a sandbox does not exist.
var ErrNotFound = errors.New("sandbox not found")

// ListSandboxes returns the sandboxes claude-forge created.
func (c *Client) ListSandboxes(ctx context.Context) ([]Sandbox, error) {
	out, err := c.Runner.Output(ctx, nil, "sandbox", "list", "-o", "json")
	if err != nil {
		return nil, err
	}
	var resp struct {
		Sandboxes []Sandbox `json:"sandboxes"`
	}
	if err := json.Unmarshal(out, &resp); err != nil {
		return nil, fmt.Errorf("failed to parse sandbox list: %w", err)
	}
	var mine []Sandbox
	for _, s := range resp.Sandboxes {
		if s.Labels[LabelKey] == "true" {
			mine = append(mine, s)
		}
	}
	return mine, nil
}

// GetSandbox returns a claude-forge sandbox by name, or ErrNotFound.
func (c *Client) GetSandbox(ctx context.Context, name string) (*Sandbox, error) {
	sandboxes, err := c.ListSandboxes(ctx)
	if err != nil {
		return nil, err
	}
	for i := range sandboxes {
		if sandboxes[i].Name == name {
			return &sandboxes[i], nil
		}
	}
	return nil, fmt.Errorf("%w: %s", ErrNotFound, name)
}

// CreateOptions configures a new sandbox.
type CreateOptions struct {
	Name      string
	Image     string
	Policy    []byte
	Providers []string
	Env       map[string]string
}

// CreateSandbox creates a sandbox whose main process only keeps it alive.
// Claude Code runs as a separate exec, so exiting Claude leaves the sandbox
// (and its workspace) in place to reconnect to.
func (c *Client) CreateSandbox(ctx context.Context, opts CreateOptions) error {
	policyPath, cleanup, err := c.writeTemp("claude-forge-policy-*.yaml", opts.Policy)
	if err != nil {
		return err
	}
	defer cleanup()

	args := []string{
		"sandbox", "create",
		"--name", opts.Name,
		"--from", opts.Image,
		"--policy", policyPath,
		"--label", LabelKey + "=true",
		"--no-auto-providers",
	}
	for _, p := range opts.Providers {
		args = append(args, "--provider", p)
	}
	for _, k := range sortedKeys(opts.Env) {
		args = append(args, "--env", k+"="+opts.Env[k])
	}
	args = append(args, "--detach", "--", "sleep", "infinity")
	_, err = c.Runner.Output(ctx, nil, args...)
	return err
}

// Upload copies a local file or directory into the sandbox. A directory is
// merged into dest/<base name>; a file is written to dest exactly.
func (c *Client) Upload(ctx context.Context, name, local, dest string) error {
	_, err := c.Runner.Output(ctx, nil, "sandbox", "upload", name, local, dest, "--no-git-ignore")
	return err
}

// Exec runs a command in the sandbox with the terminal attached.
func (c *Client) Exec(ctx context.Context, name, workdir string, env map[string]string, command ...string) error {
	args := []string{"sandbox", "exec", "-n", name, "--tty"}
	if workdir != "" {
		args = append(args, "--workdir", workdir)
	}
	for _, k := range sortedKeys(env) {
		args = append(args, "--env", k+"="+env[k])
	}
	args = append(args, "--")
	args = append(args, command...)
	return c.Runner.Interactive(ctx, args...)
}

// Start starts a stopped sandbox.
func (c *Client) Start(ctx context.Context, name string) error {
	_, err := c.Runner.Output(ctx, nil, "sandbox", "start", name)
	return err
}

// Stop stops a sandbox, keeping its workspace.
func (c *Client) Stop(ctx context.Context, name string) error {
	_, err := c.Runner.Output(ctx, nil, "sandbox", "stop", name)
	return err
}

// Delete deletes a sandbox and its workspace.
func (c *Client) Delete(ctx context.Context, name string) error {
	_, err := c.Runner.Output(ctx, nil, "sandbox", "delete", name)
	return err
}

// uploadHome uploads every top-level entry of stageDir into the sandbox home.
func (c *Client) uploadHome(ctx context.Context, name, stageDir, home string) error {
	entries, err := os.ReadDir(stageDir)
	if err != nil {
		return fmt.Errorf("failed to read %s: %w", stageDir, err)
	}
	for _, e := range entries {
		local := filepath.Join(stageDir, e.Name())
		dest := home
		if !e.IsDir() {
			dest = home + "/" + e.Name()
		}
		if err := c.Upload(ctx, name, local, dest); err != nil {
			return err
		}
	}
	return nil
}

func sortedKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

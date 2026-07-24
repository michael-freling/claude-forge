package kube

import (
	"bytes"
	"crypto/rand"
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// ContextConfig configures a single Kubernetes context to expose to the agent.
type ContextConfig struct {
	HostContext             string
	ServiceAccountName      string
	ServiceAccountNamespace string
}

// kubeconfig types for reading/writing kubeconfig YAML.
type kubeConfig struct {
	APIVersion     string         `yaml:"apiVersion"`
	Kind           string         `yaml:"kind"`
	Clusters       []namedCluster `yaml:"clusters"`
	Contexts       []namedContext `yaml:"contexts"`
	Users          []namedUser    `yaml:"users"`
	CurrentContext string         `yaml:"current-context"`
}

type namedCluster struct {
	Name    string      `yaml:"name"`
	Cluster clusterInfo `yaml:"cluster"`
}

type clusterInfo struct {
	Server                   string `yaml:"server"`
	CertificateAuthorityData string `yaml:"certificate-authority-data,omitempty"`
}

type namedContext struct {
	Name    string      `yaml:"name"`
	Context contextInfo `yaml:"context"`
}

type contextInfo struct {
	Cluster string `yaml:"cluster"`
	User    string `yaml:"user"`
}

type namedUser struct {
	Name string   `yaml:"name"`
	User userInfo `yaml:"user"`
}

type userInfo struct {
	TokenFile string `yaml:"tokenFile,omitempty"`
}

// GenerateKubeconfig reads the user's kubeconfig, extracts the specified
// contexts, mints SA tokens via kubectl, and writes a self-contained
// kubeconfig plus per-context token files into outputDir, which is
// bind-mounted into the k8s-mcp container at MCPServerKubeDir.
//
// The kubeconfig references each token via tokenFile rather than embedding
// it: client-go re-reads tokenFile credentials on its own, so rewriting the
// token files (see RefreshTokens) rotates credentials inside the running
// container without a restart.
func GenerateKubeconfig(contexts []ContextConfig, kubeconfigPath, defaultContext, outputDir string, tokenDuration time.Duration) error {
	if err := ensureDir(outputDir); err != nil {
		return fmt.Errorf("failed to create kubeconfig dir: %w", err)
	}
	salt, err := tokenSalt(outputDir)
	if err != nil {
		return err
	}

	outData, err := buildKubeconfig(contexts, kubeconfigPath, defaultContext, salt)
	if err != nil {
		return err
	}

	if err := RefreshTokens(contexts, kubeconfigPath, outputDir, tokenDuration); err != nil {
		return err
	}

	if err := writeFileAtomic(filepath.Join(outputDir, KubeconfigFileName), outData, 0o644); err != nil {
		return fmt.Errorf("failed to write generated kubeconfig: %w", err)
	}
	return nil
}

// KubeconfigUpToDate reports whether the generated kubeconfig in outputDir
// matches what the given contexts would generate. The k8s-mcp server only
// reads its kubeconfig at startup, so a mismatch means the running container
// must be recreated for context changes to take effect.
func KubeconfigUpToDate(contexts []ContextConfig, kubeconfigPath, defaultContext, outputDir string) (bool, error) {
	salt, err := tokenSalt(outputDir)
	if err != nil {
		return false, err
	}
	desired, err := buildKubeconfig(contexts, kubeconfigPath, defaultContext, salt)
	if err != nil {
		return false, err
	}
	current, err := os.ReadFile(filepath.Join(outputDir, KubeconfigFileName))
	if err != nil {
		return false, err
	}
	return bytes.Equal(desired, current), nil
}

// buildKubeconfig renders the self-contained kubeconfig for the given
// contexts without touching the filesystem beyond reading the source
// kubeconfig. Marshalling is deterministic, so the output is byte-comparable.
func buildKubeconfig(contexts []ContextConfig, kubeconfigPath, defaultContext, salt string) ([]byte, error) {
	data, err := os.ReadFile(kubeconfigPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read kubeconfig %s: %w", kubeconfigPath, err)
	}

	var srcConfig kubeConfig
	if err := yaml.Unmarshal(data, &srcConfig); err != nil {
		return nil, fmt.Errorf("failed to parse kubeconfig: %w", err)
	}

	clusterByContext := make(map[string]namedCluster)
	clusterByName := make(map[string]namedCluster)
	for _, c := range srcConfig.Clusters {
		clusterByName[c.Name] = c
	}
	for _, c := range srcConfig.Contexts {
		if cl, ok := clusterByName[c.Context.Cluster]; ok {
			clusterByContext[c.Name] = cl
		}
	}

	var out kubeConfig
	out.APIVersion = "v1"
	out.Kind = "Config"
	out.CurrentContext = defaultContext

	for _, ctx := range contexts {
		cl, ok := clusterByContext[ctx.HostContext]
		if !ok {
			return nil, fmt.Errorf("context %q not found in kubeconfig or its cluster is missing", ctx.HostContext)
		}

		userName := ctx.HostContext + "-sa"

		out.Clusters = append(out.Clusters, namedCluster{
			Name: ctx.HostContext,
			Cluster: clusterInfo{
				Server:                   cl.Cluster.Server,
				CertificateAuthorityData: cl.Cluster.CertificateAuthorityData,
			},
		})
		out.Contexts = append(out.Contexts, namedContext{
			Name: ctx.HostContext,
			Context: contextInfo{
				Cluster: ctx.HostContext,
				User:    userName,
			},
		})
		out.Users = append(out.Users, namedUser{
			Name: userName,
			// The container-side path of the token file written by RefreshTokens.
			User: userInfo{TokenFile: path.Join(MCPServerKubeDir, TokensDirName, TokenFileName(ctx.HostContext, salt))},
		})
	}

	outData, err := yaml.Marshal(&out)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal generated kubeconfig: %w", err)
	}
	return outData, nil
}

// RefreshTokens re-mints the SA tokens referenced by the generated kubeconfig
// and rewrites the token files under outputDir. Tokens expire (most API
// servers cap lifetime at 1h-48h regardless of what is requested) while
// sessions can stay open for days, so this runs at every session start and
// periodically while a session is attached. The old token stays valid until
// its own expiry, so rotation never races in-flight requests.
func RefreshTokens(contexts []ContextConfig, kubeconfigPath, outputDir string, tokenDuration time.Duration) error {
	tokensDir := filepath.Join(outputDir, TokensDirName)
	if err := ensureDir(tokensDir); err != nil {
		return fmt.Errorf("failed to create tokens dir: %w", err)
	}
	salt, err := tokenSalt(outputDir)
	if err != nil {
		return err
	}
	for _, ctx := range contexts {
		token, err := resolveToken(ctx, kubeconfigPath, tokenDuration)
		if err != nil {
			return fmt.Errorf("failed to resolve token for context %q: %w", ctx.HostContext, err)
		}
		tokenPath := filepath.Join(tokensDir, TokenFileName(ctx.HostContext, salt))
		if err := writeFileAtomic(tokenPath, []byte(token), 0o644); err != nil {
			return fmt.Errorf("failed to write token for context %q: %w", ctx.HostContext, err)
		}
	}
	return nil
}

// TokenFileName returns a stable, filesystem-safe file name for a context's
// token. Context names may contain characters that are invalid in file names
// (EKS ARN contexts contain "/" and ":"), so unsafe runes are replaced and a
// hash keeps distinct contexts from colliding even when their sanitized
// prefixes match.
//
// The salt (see tokenSalt) makes the name underivable from public inputs:
// context names are readable from config.yaml by any local user, and the
// token dirs are 0711 (traversal allowed, listing blocked), so a guessable
// name would let other users open live tokens by exact path.
func TokenFileName(hostContext, salt string) string {
	sum := sha256.Sum256([]byte(salt + "\x00" + hostContext))
	safe := strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9',
			r == '.', r == '_', r == '-':
			return r
		default:
			return '-'
		}
	}, hostContext)
	if len(safe) > 64 {
		safe = safe[:64]
	}
	return fmt.Sprintf("%s-%x", safe, sum[:8])
}

// saltFileName holds the random component of token file names, 0600 inside
// the generated-kubeconfig dir: the container never reads it (the kubeconfig
// carries full token paths) and other host users cannot.
const saltFileName = ".token-salt"

// tokenSalt returns the per-install salt, creating it on first use. Creation
// is write-temp-then-link so concurrent first sessions agree on one salt: the
// hard link fails on an existing path, and the linked file is always fully
// written.
func tokenSalt(outputDir string) (string, error) {
	saltPath := filepath.Join(outputDir, saltFileName)
	if data, err := os.ReadFile(saltPath); err == nil && len(data) > 0 {
		return strings.TrimSpace(string(data)), nil
	}

	var buf [16]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return "", fmt.Errorf("failed to generate token salt: %w", err)
	}
	salt := fmt.Sprintf("%x", buf)

	tmp, err := os.CreateTemp(outputDir, ".tmp-salt-*")
	if err != nil {
		return "", fmt.Errorf("failed to create token salt: %w", err)
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)
	if _, err := tmp.WriteString(salt); err != nil {
		tmp.Close()
		return "", fmt.Errorf("failed to write token salt: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return "", fmt.Errorf("failed to write token salt: %w", err)
	}
	if err := os.Link(tmpPath, saltPath); err != nil {
		if os.IsExist(err) {
			// Another session created the salt first; use theirs.
			data, readErr := os.ReadFile(saltPath)
			if readErr != nil || len(data) == 0 {
				return "", fmt.Errorf("failed to read token salt: %w", readErr)
			}
			return strings.TrimSpace(string(data)), nil
		}
		return "", fmt.Errorf("failed to store token salt: %w", err)
	}
	return salt, nil
}

// ensureDir creates dir if needed and forces 0711 permissions even on a
// pre-existing directory (MkdirAll does not chmod those). The k8s-mcp
// container runs as a non-host uid and opens only exact paths from the
// kubeconfig, so it needs traversal (x) but not listing; withholding the
// read bit keeps other host users from listing the minted token files.
func ensureDir(dir string) error {
	if err := os.MkdirAll(dir, 0o711); err != nil {
		return err
	}
	return os.Chmod(dir, 0o711)
}

// writeFileAtomic writes data via temp-file-plus-rename so concurrent
// refreshers never produce a torn file and the container (which reads these
// files through a bind mount of the parent directory) always sees a complete
// one.
func writeFileAtomic(filePath string, data []byte, perm os.FileMode) error {
	tmp, err := os.CreateTemp(filepath.Dir(filePath), ".tmp-*")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Chmod(perm); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpPath, filePath)
}

// ListContexts reads a kubeconfig file and returns all context names.
func ListContexts(kubeconfigPath string) ([]string, error) {
	data, err := os.ReadFile(kubeconfigPath)
	if err != nil {
		return nil, err
	}
	var cfg kubeConfig
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("failed to parse kubeconfig: %w", err)
	}
	var names []string
	for _, c := range cfg.Contexts {
		names = append(names, c.Name)
	}
	return names, nil
}

// resolveToken calls `kubectl create token` to mint an SA token. Most API
// servers cap the requested duration at their configured maximum (managed
// clusters commonly cap at 24-48h); for servers that reject over-maximum
// requests instead, it retries once without --duration so a long
// token_duration degrades to the server-default lifetime rather than
// disabling the MCP.
var resolveToken = func(ctx ContextConfig, kubeconfigPath string, duration time.Duration) (string, error) {
	args := []string{
		"create", "token",
		ctx.ServiceAccountName,
		"-n", ctx.ServiceAccountNamespace,
		"--context", ctx.HostContext,
		"--kubeconfig", kubeconfigPath,
	}
	if duration <= 0 {
		return kubectlCreateToken(args)
	}

	token, err := kubectlCreateToken(append(args, "--duration", duration.String()))
	if err == nil {
		return token, nil
	}
	// Only a kubectl that ran and was refused (ExitError) can be a
	// duration rejection; exec failures (kubectl missing) won't improve on
	// retry. If the fallback fails too, report the original error — it
	// names the real problem (auth, connectivity, RBAC).
	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) {
		return "", err
	}
	token, fallbackErr := kubectlCreateToken(args)
	if fallbackErr != nil {
		return "", err
	}
	return token, nil
}

func kubectlCreateToken(args []string) (string, error) {
	out, err := exec.Command("kubectl", args...).Output()
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) && len(exitErr.Stderr) > 0 {
			return "", fmt.Errorf("kubectl create token failed: %w: %s", err, strings.TrimSpace(string(exitErr.Stderr)))
		}
		return "", fmt.Errorf("kubectl create token failed: %w", err)
	}
	return strings.TrimSpace(string(out)), nil
}

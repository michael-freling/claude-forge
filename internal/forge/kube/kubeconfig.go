package kube

import (
	"crypto/sha256"
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
	data, err := os.ReadFile(kubeconfigPath)
	if err != nil {
		return fmt.Errorf("failed to read kubeconfig %s: %w", kubeconfigPath, err)
	}

	var srcConfig kubeConfig
	if err := yaml.Unmarshal(data, &srcConfig); err != nil {
		return fmt.Errorf("failed to parse kubeconfig: %w", err)
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
			return fmt.Errorf("context %q not found in kubeconfig or its cluster is missing", ctx.HostContext)
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
			User: userInfo{TokenFile: path.Join(MCPServerKubeDir, TokensDirName, TokenFileName(ctx.HostContext))},
		})
	}

	// The directory (not just the kubeconfig) is bind-mounted, and the
	// container's user is not necessarily the host user, so everything under
	// it must be world-readable.
	if err := os.MkdirAll(outputDir, 0o755); err != nil {
		return fmt.Errorf("failed to create kubeconfig dir: %w", err)
	}
	if err := os.Chmod(outputDir, 0o755); err != nil {
		return fmt.Errorf("failed to set kubeconfig dir permissions: %w", err)
	}

	if err := RefreshTokens(contexts, kubeconfigPath, outputDir, tokenDuration); err != nil {
		return err
	}

	outData, err := yaml.Marshal(&out)
	if err != nil {
		return fmt.Errorf("failed to marshal generated kubeconfig: %w", err)
	}
	if err := writeFileAtomic(filepath.Join(outputDir, KubeconfigFileName), outData, 0o644); err != nil {
		return fmt.Errorf("failed to write generated kubeconfig: %w", err)
	}
	return nil
}

// RefreshTokens re-mints the SA tokens referenced by the generated kubeconfig
// and rewrites the token files under outputDir. Tokens expire (most API
// servers cap lifetime at 1h-48h regardless of what is requested) while
// sessions can stay open for days, so this runs at every session start and
// periodically while a session is attached. The old token stays valid until
// its own expiry, so rotation never races in-flight requests.
func RefreshTokens(contexts []ContextConfig, kubeconfigPath, outputDir string, tokenDuration time.Duration) error {
	tokensDir := filepath.Join(outputDir, TokensDirName)
	if err := os.MkdirAll(tokensDir, 0o755); err != nil {
		return fmt.Errorf("failed to create tokens dir: %w", err)
	}
	if err := os.Chmod(tokensDir, 0o755); err != nil {
		return fmt.Errorf("failed to set tokens dir permissions: %w", err)
	}
	for _, ctx := range contexts {
		token, err := resolveToken(ctx, kubeconfigPath, tokenDuration)
		if err != nil {
			return fmt.Errorf("failed to resolve token for context %q: %w", ctx.HostContext, err)
		}
		tokenPath := filepath.Join(tokensDir, TokenFileName(ctx.HostContext))
		if err := writeFileAtomic(tokenPath, []byte(token), 0o644); err != nil {
			return fmt.Errorf("failed to write token for context %q: %w", ctx.HostContext, err)
		}
	}
	return nil
}

// TokenFileName returns a stable, filesystem-safe file name for a context's
// token. Context names may contain characters that are invalid in file names
// (EKS ARN contexts contain "/" and ":"), so unsafe runes are replaced and a
// short hash keeps distinct contexts from colliding.
func TokenFileName(hostContext string) string {
	sum := sha256.Sum256([]byte(hostContext))
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
	return fmt.Sprintf("%s-%x", safe, sum[:4])
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

// resolveToken calls `kubectl create token` to mint an SA token. The API
// server caps the duration at its configured maximum (managed clusters
// commonly cap at 24-48h), so longer requests degrade gracefully to the
// cluster's cap rather than failing.
var resolveToken = func(ctx ContextConfig, kubeconfigPath string, duration time.Duration) (string, error) {
	args := []string{
		"create", "token",
		ctx.ServiceAccountName,
		"-n", ctx.ServiceAccountNamespace,
		"--context", ctx.HostContext,
		"--kubeconfig", kubeconfigPath,
	}
	if duration > 0 {
		args = append(args, "--duration", duration.String())
	}
	out, err := exec.Command("kubectl", args...).Output()
	if err != nil {
		return "", fmt.Errorf("kubectl create token failed: %w", err)
	}
	return strings.TrimSpace(string(out)), nil
}

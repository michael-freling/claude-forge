package kube

import (
	"fmt"
	"os"
	"os/exec"
	"strings"

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
	Token string `yaml:"token,omitempty"`
}

// GenerateKubeconfig reads the user's kubeconfig, extracts the specified
// contexts, resolves SA tokens via kubectl, and writes a self-contained
// kubeconfig suitable for mounting into the k8s-mcp container.
func GenerateKubeconfig(contexts []ContextConfig, kubeconfigPath, defaultContext, outputPath string) error {
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

		token, err := resolveToken(ctx, kubeconfigPath)
		if err != nil {
			return fmt.Errorf("failed to resolve token for context %q: %w", ctx.HostContext, err)
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
			User: userInfo{Token: token},
		})
	}

	outData, err := yaml.Marshal(&out)
	if err != nil {
		return fmt.Errorf("failed to marshal generated kubeconfig: %w", err)
	}

	if err := os.WriteFile(outputPath, outData, 0o600); err != nil {
		return fmt.Errorf("failed to write generated kubeconfig: %w", err)
	}
	return nil
}

// resolveToken calls `kubectl create token` to get a short-lived SA token.
var resolveToken = func(ctx ContextConfig, kubeconfigPath string) (string, error) {
	args := []string{
		"create", "token",
		ctx.ServiceAccountName,
		"-n", ctx.ServiceAccountNamespace,
		"--context", ctx.HostContext,
		"--kubeconfig", kubeconfigPath,
	}
	out, err := exec.Command("kubectl", args...).Output()
	if err != nil {
		return "", fmt.Errorf("kubectl create token failed: %w", err)
	}
	return strings.TrimSpace(string(out)), nil
}

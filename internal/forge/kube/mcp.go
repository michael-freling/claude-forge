package kube

const (
	MCPServerPort = "8090"

	// MCPServerKubeDir is where the generated kubeconfig directory is
	// bind-mounted inside the k8s-mcp container. The whole directory is
	// mounted (not just the kubeconfig) so token files rewritten on the host
	// appear in the container.
	MCPServerKubeDir        = "/home/user/.kube"
	MCPServerKubeconfigPath = MCPServerKubeDir + "/" + KubeconfigFileName

	// KubeconfigFileName and TokensDirName define the layout inside the
	// mounted directory, identical on the host and in the container.
	KubeconfigFileName = "config"
	TokensDirName      = "tokens"
)

func MCPServerArgs() []string {
	return []string{
		"--port", MCPServerPort,
		"--kubeconfig", MCPServerKubeconfigPath,
	}
}

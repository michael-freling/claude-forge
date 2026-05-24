package kube

const (
	MCPServerPort           = "8090"
	MCPServerKubeconfigPath = "/home/user/.kube/config"
)

func MCPServerArgs() []string {
	return []string{
		"--port", MCPServerPort,
		"--read-only",
		"--kubeconfig", MCPServerKubeconfigPath,
	}
}

package kube

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestMCPServerArgs(t *testing.T) {
	args := MCPServerArgs()
	assert.Contains(t, args, "--port")
	assert.Contains(t, args, MCPServerPort)
	assert.Contains(t, args, "--kubeconfig")
	assert.Contains(t, args, MCPServerKubeconfigPath)
	assert.NotContains(t, args, "--read-only")
	assert.NotContains(t, args, "--disable-destructive")
}

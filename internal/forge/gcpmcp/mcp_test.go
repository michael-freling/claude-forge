package gcpmcp

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestMCPServerArgs_Minimal(t *testing.T) {
	args := MCPServerArgs(Options{})
	assert.Equal(t, []string{"--addr", ":" + MCPServerPort}, args)
	// Both gates default closed.
	assert.NotContains(t, args, "--allow-writes")
	assert.NotContains(t, args, "--allow-secret-access")
}

func TestMCPServerArgs_Full(t *testing.T) {
	args := MCPServerArgs(Options{
		Project:                   "my-proj",
		ImpersonateServiceAccount: "sa@my-proj.iam.gserviceaccount.com",
		QuotaProject:              "quota-proj",
		AllowWrites:               true,
		AllowSecretAccess:         true,
	})

	assertFlagValue(t, args, "--addr", ":"+MCPServerPort)
	assertFlagValue(t, args, "--project", "my-proj")
	assertFlagValue(t, args, "--impersonate-service-account", "sa@my-proj.iam.gserviceaccount.com")
	assertFlagValue(t, args, "--quota-project", "quota-proj")
	assert.Contains(t, args, "--allow-writes")
	assert.Contains(t, args, "--allow-secret-access")
}

func TestMCPServerArgs_SecretAccessWithoutWrites(t *testing.T) {
	// The gates are independent: secret access without writes.
	args := MCPServerArgs(Options{AllowSecretAccess: true})
	assert.Contains(t, args, "--allow-secret-access")
	assert.NotContains(t, args, "--allow-writes")
}

func assertFlagValue(t *testing.T, args []string, flag, value string) {
	t.Helper()
	for i, a := range args {
		if a == flag {
			if i+1 < len(args) {
				assert.Equal(t, value, args[i+1], "flag %s", flag)
				return
			}
		}
	}
	t.Fatalf("flag %s not found in %v", flag, args)
}

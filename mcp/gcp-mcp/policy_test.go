package main

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestPolicy_ReadAlwaysAllowed(t *testing.T) {
	for _, p := range []*Policy{
		{},
		{AllowWrites: true},
		{AllowSecretAccess: true},
		{AllowWrites: true, AllowSecretAccess: true},
	} {
		assert.NoError(t, p.CheckTool("list_projects", accessRead))
	}
}

func TestPolicy_WriteGated(t *testing.T) {
	deny := &Policy{}
	err := deny.CheckTool("some_write", accessWrite)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "write access denied")
	assert.Contains(t, err.Error(), "will always fail")

	allow := &Policy{AllowWrites: true}
	assert.NoError(t, allow.CheckTool("some_write", accessWrite))
}

func TestPolicy_SecretGated(t *testing.T) {
	deny := &Policy{}
	err := deny.CheckTool("access_secret_version", accessSecret)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "secret access denied")
	assert.Contains(t, err.Error(), "will always fail")

	allow := &Policy{AllowSecretAccess: true}
	assert.NoError(t, allow.CheckTool("access_secret_version", accessSecret))
}

// The two gates are independent: enabling one must not enable the other.
func TestPolicy_GatesIndependent(t *testing.T) {
	writesOnly := &Policy{AllowWrites: true}
	assert.Error(t, writesOnly.CheckTool("access_secret_version", accessSecret))

	secretsOnly := &Policy{AllowSecretAccess: true}
	assert.Error(t, secretsOnly.CheckTool("some_write", accessWrite))
}

func TestPolicy_UnclassifiedDenied(t *testing.T) {
	p := &Policy{AllowWrites: true, AllowSecretAccess: true}
	err := p.CheckTool("mystery", accessUnset)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "no read/write classification")
}

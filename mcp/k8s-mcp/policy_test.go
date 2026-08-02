package main

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPolicy_CheckResource(t *testing.T) {
	p := &Policy{}

	tests := []struct {
		name        string
		group       string
		resource    string
		subresource string
		verb        string
		namespaced  bool
		wantDenied  bool
	}{
		// Ordinary namespaced resources: reads and writes allowed.
		{"list pods", "", "pods", "", "list", true, false},
		{"get pod", "", "pods", "", "get", true, false},
		{"create configmap", "", "configmaps", "", "create", true, false},
		{"delete pod", "", "pods", "", "delete", true, false},
		{"update deployment", "apps", "deployments", "", "update", true, false},
		{"pod logs allowed", "", "pods", "log", "get", true, false},

		// Denied core resources (any verb).
		{"read secret denied", "", "secrets", "", "get", true, true},
		{"list secrets denied", "", "secrets", "", "list", true, true},
		{"read serviceaccount denied", "", "serviceaccounts", "", "get", true, true},

		// Denied API groups (any verb).
		{"read rbac denied", "rbac.authorization.k8s.io", "roles", "", "get", true, true},
		{"list clusterroles denied", "rbac.authorization.k8s.io", "clusterroles", "", "list", false, true},
		{"admission denied", "admissionregistration.k8s.io", "validatingwebhookconfigurations", "", "get", false, true},

		// Denied subresources.
		{"pod exec denied", "", "pods", "exec", "create", true, true},
		{"pod attach denied", "", "pods", "attach", "create", true, true},
		{"sa token denied", "", "serviceaccounts", "token", "create", true, true},

		// Impersonation.
		{"impersonate denied", "", "users", "", "impersonate", false, true},

		// Cluster-scoped: read-only except nodes.
		{"read namespace ok", "", "namespaces", "", "get", false, false},
		{"list nodes ok", "", "nodes", "", "list", false, false},
		{"write node ok (cordon/drain)", "", "nodes", "", "update", false, false},
		{"delete namespace denied", "", "namespaces", "", "delete", false, true},
		{"create clusterscoped denied", "storage.k8s.io", "storageclasses", "", "create", false, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := p.CheckResource(tt.group, tt.resource, tt.subresource, tt.verb, tt.namespaced)
			if tt.wantDenied {
				require.Error(t, err)
				assert.Contains(t, err.Error(), "will always be denied here")
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestIsWriteVerb(t *testing.T) {
	for _, v := range []string{"create", "update", "patch", "delete"} {
		assert.True(t, isWriteVerb(v), v)
	}
	for _, v := range []string{"get", "list", "watch", "log"} {
		assert.False(t, isWriteVerb(v), v)
	}
}

package kube

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestIsAPIGroupDenied(t *testing.T) {
	assert.True(t, IsAPIGroupDenied("rbac.authorization.k8s.io"))
	assert.True(t, IsAPIGroupDenied("admissionregistration.k8s.io"))
	assert.False(t, IsAPIGroupDenied("apps"))
	assert.False(t, IsAPIGroupDenied(""))
}

func TestIsResourceDenied(t *testing.T) {
	assert.True(t, IsResourceDenied("secrets"))
	assert.True(t, IsResourceDenied("serviceaccounts"))
	assert.False(t, IsResourceDenied("configmaps"))
	assert.False(t, IsResourceDenied("pods"))
}

func TestIsSubresourceDenied(t *testing.T) {
	assert.True(t, IsSubresourceDenied("pods/exec"))
	assert.True(t, IsSubresourceDenied("pods/attach"))
	assert.True(t, IsSubresourceDenied("serviceaccounts/token"))
	assert.False(t, IsSubresourceDenied("pods/log"))
}

func TestGrantedSubresources(t *testing.T) {
	granted := GrantedSubresources()
	assert.Contains(t, granted, "pods/log")
	for _, sub := range granted {
		assert.False(t, IsSubresourceDenied(sub))
	}
}

func TestScopedWriteGroups_ArgoApplicationSets(t *testing.T) {
	var argo *ScopedWriteGroup
	for i, sg := range ScopedWriteGroups() {
		if sg.APIGroup == "argoproj.io" {
			argo = &ScopedWriteGroups()[i]
			break
		}
	}

	if assert.NotNil(t, argo, "expected a scoped-write group for argoproj.io") {
		assert.Equal(t, []string{"applicationsets"}, argo.WritableResources,
			"writes must be limited to applicationsets")
		assert.Contains(t, argo.WriteVerbs, "patch", "must allow patching target revisions")
		assert.NotContains(t, argo.WriteVerbs, "delete")
		assert.NotContains(t, argo.WriteVerbs, "create")
	}
}

func TestIsScopedWriteGroup(t *testing.T) {
	assert.True(t, IsScopedWriteGroup("argoproj.io"))
	assert.False(t, IsScopedWriteGroup("apps"))
	assert.False(t, IsScopedWriteGroup(""))
}

func TestFilterVerbs(t *testing.T) {
	result := FilterVerbs([]string{"get", "list", "impersonate", "watch"})
	assert.Equal(t, []string{"get", "list", "watch"}, result)

	result = FilterVerbs([]string{"*"})
	assert.Equal(t, []string{"*"}, result)
}

func TestIsWritableClusterResource(t *testing.T) {
	assert.True(t, IsWritableClusterResource("nodes"))
	assert.False(t, IsWritableClusterResource("namespaces"))
	assert.False(t, IsWritableClusterResource("persistentvolumes"))
}

func TestReadOnlyVerbs(t *testing.T) {
	assert.Equal(t, []string{"get", "list", "watch"}, ReadOnlyVerbs())
}

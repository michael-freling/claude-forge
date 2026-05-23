package kube

import (
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBuildRules_SkipsDeniedAPIGroups(t *testing.T) {
	resources := []APIResource{
		{APIGroup: "rbac.authorization.k8s.io", Resource: "roles", Namespaced: true, Verbs: []string{"*"}},
		{APIGroup: "admissionregistration.k8s.io", Resource: "mutatingwebhookconfigurations", Namespaced: false, Verbs: []string{"*"}},
		{APIGroup: "apps", Resource: "deployments", Namespaced: true, Verbs: []string{"*"}},
	}

	rules := buildRules(resources)

	for _, rule := range rules {
		for _, group := range rule.APIGroups {
			assert.NotEqual(t, "rbac.authorization.k8s.io", group)
			assert.NotEqual(t, "admissionregistration.k8s.io", group)
		}
	}
	require.Len(t, rules, 1)
	assert.Equal(t, []string{"apps"}, rules[0].APIGroups)
}

func TestBuildRules_SkipsDeniedResources(t *testing.T) {
	resources := []APIResource{
		{APIGroup: "", Resource: "secrets", Namespaced: true, Verbs: []string{"*"}},
		{APIGroup: "", Resource: "serviceaccounts", Namespaced: true, Verbs: []string{"*"}},
		{APIGroup: "", Resource: "configmaps", Namespaced: true, Verbs: []string{"*"}},
		{APIGroup: "", Resource: "pods", Namespaced: true, Verbs: []string{"*"}},
	}

	rules := buildRules(resources)

	require.Len(t, rules, 1)
	assert.Contains(t, rules[0].Resources, "configmaps")
	assert.Contains(t, rules[0].Resources, "pods")
	assert.NotContains(t, rules[0].Resources, "secrets")
	assert.NotContains(t, rules[0].Resources, "serviceaccounts")
}

func TestBuildRules_SkipsDeniedSubresources(t *testing.T) {
	resources := []APIResource{
		{APIGroup: "", Resource: "pods/exec", Namespaced: true, Verbs: []string{"create"}},
		{APIGroup: "", Resource: "pods/attach", Namespaced: true, Verbs: []string{"create"}},
		{APIGroup: "", Resource: "serviceaccounts/token", Namespaced: true, Verbs: []string{"create"}},
		{APIGroup: "", Resource: "pods/log", Namespaced: true, Verbs: []string{"get"}},
	}

	rules := buildRules(resources)

	require.Len(t, rules, 1)
	assert.Contains(t, rules[0].Resources, "pods/log")
	assert.NotContains(t, rules[0].Resources, "pods/exec")
	assert.NotContains(t, rules[0].Resources, "pods/attach")
	assert.NotContains(t, rules[0].Resources, "serviceaccounts/token")
}

func TestBuildRules_ClusterScopedReadOnly(t *testing.T) {
	resources := []APIResource{
		{APIGroup: "", Resource: "namespaces", Namespaced: false, Verbs: []string{"*"}},
		{APIGroup: "", Resource: "nodes", Namespaced: false, Verbs: []string{"*"}},
		{APIGroup: "", Resource: "pods", Namespaced: true, Verbs: []string{"*"}},
	}

	rules := buildRules(resources)

	require.Len(t, rules, 2)

	// Find the cluster-scoped rule
	var clusterRule *PolicyRule
	var nsRule *PolicyRule
	for i, r := range rules {
		if contains(r.Resources, "namespaces") {
			clusterRule = &rules[i]
		}
		if contains(r.Resources, "pods") {
			nsRule = &rules[i]
		}
	}

	require.NotNil(t, clusterRule)
	assert.Equal(t, ReadOnlyVerbs(), clusterRule.Verbs)

	require.NotNil(t, nsRule)
	assert.Equal(t, []string{"*"}, nsRule.Verbs)
}

func TestBuildRules_FiltersImpersonateVerb(t *testing.T) {
	resources := []APIResource{
		{APIGroup: "apps", Resource: "deployments", Namespaced: true, Verbs: []string{"get", "list", "impersonate"}},
	}

	rules := buildRules(resources)

	require.Len(t, rules, 1)
	assert.NotContains(t, rules[0].Verbs, "impersonate")
}

func TestBuildRules_NonCoreGroupsUseWildcardResources(t *testing.T) {
	resources := []APIResource{
		{APIGroup: "apps", Resource: "deployments", Namespaced: true, Verbs: []string{"*"}},
		{APIGroup: "apps", Resource: "replicasets", Namespaced: true, Verbs: []string{"*"}},
		{APIGroup: "batch", Resource: "jobs", Namespaced: true, Verbs: []string{"*"}},
	}

	rules := buildRules(resources)

	require.Len(t, rules, 1)
	assert.Equal(t, []string{"*"}, rules[0].Resources)
	assert.Contains(t, rules[0].APIGroups, "apps")
	assert.Contains(t, rules[0].APIGroups, "batch")
}

func TestBuildRules_MergesGroupsByScope(t *testing.T) {
	resources := []APIResource{
		// Namespaced-only groups
		{APIGroup: "apps", Resource: "deployments", Namespaced: true, Verbs: []string{"*"}},
		{APIGroup: "batch", Resource: "jobs", Namespaced: true, Verbs: []string{"*"}},
		// Cluster-only group
		{APIGroup: "storage.k8s.io", Resource: "storageclasses", Namespaced: false, Verbs: []string{"*"}},
		// Mixed group (both scopes)
		{APIGroup: "networking.k8s.io", Resource: "ingresses", Namespaced: true, Verbs: []string{"*"}},
		{APIGroup: "networking.k8s.io", Resource: "ingressclasses", Namespaced: false, Verbs: []string{"*"}},
	}

	rules := buildRules(resources)

	require.Len(t, rules, 3)

	var nsRule, clusterRule, bothRule *PolicyRule
	for i, r := range rules {
		if contains(r.APIGroups, "apps") {
			nsRule = &rules[i]
		}
		if contains(r.APIGroups, "storage.k8s.io") {
			clusterRule = &rules[i]
		}
		if contains(r.APIGroups, "networking.k8s.io") {
			bothRule = &rules[i]
		}
	}

	require.NotNil(t, nsRule)
	assert.Contains(t, nsRule.APIGroups, "batch")
	assert.Equal(t, []string{"*"}, nsRule.Resources)
	assert.Equal(t, FilterVerbs([]string{"*"}), nsRule.Verbs)

	require.NotNil(t, clusterRule)
	assert.Equal(t, []string{"*"}, clusterRule.Resources)
	assert.Equal(t, ReadOnlyVerbs(), clusterRule.Verbs)

	require.NotNil(t, bothRule)
	assert.Equal(t, []string{"*"}, bothRule.Resources)
	assert.Equal(t, FilterVerbs([]string{"*"}), bothRule.Verbs)
}

func TestRenderFromResources_GeneratesValidYAML(t *testing.T) {
	resources := []APIResource{
		{APIGroup: "", Resource: "pods", Namespaced: true, Verbs: []string{"*"}},
		{APIGroup: "", Resource: "configmaps", Namespaced: true, Verbs: []string{"*"}},
		{APIGroup: "", Resource: "namespaces", Namespaced: false, Verbs: []string{"*"}},
		{APIGroup: "apps", Resource: "deployments", Namespaced: true, Verbs: []string{"*"}},
	}

	opts := RenderOptions{
		ClusterRoleName:         "claude-forge-agent",
		ServiceAccountName:      "claude-forge-agent",
		ServiceAccountNamespace: "claude-forge",
	}

	output := RenderFromResources(opts, resources)

	assert.Contains(t, output, "kind: ServiceAccount")
	assert.Contains(t, output, "name: claude-forge-agent")
	assert.Contains(t, output, "namespace: claude-forge")
	assert.Contains(t, output, "kind: ClusterRole")
	assert.Contains(t, output, "kind: ClusterRoleBinding")

	// Verify it has three YAML documents
	docs := strings.Split(output, "---")
	assert.Len(t, docs, 3)
}

func TestParseAPIResources(t *testing.T) {
	input := `bindings                                    v1                                     true         Binding                          [create]
configmaps                       cm          v1                                     true         ConfigMap                        [create delete deletecollection get list patch update watch]
namespaces                       ns          v1                                     false        Namespace                        [create delete get list patch update watch]
deployments                      deploy      apps/v1                                true         Deployment                       [create delete deletecollection get list patch update watch]
roles                                        rbac.authorization.k8s.io/v1           true         Role                             [create delete deletecollection get list patch update watch]`

	resources, err := parseAPIResources(input)
	require.NoError(t, err)
	require.Len(t, resources, 5)

	assert.Equal(t, "bindings", resources[0].Resource)
	assert.Equal(t, "", resources[0].APIGroup)
	assert.True(t, resources[0].Namespaced)

	assert.Equal(t, "configmaps", resources[1].Resource)
	assert.Equal(t, "", resources[1].APIGroup)
	assert.True(t, resources[1].Namespaced)

	assert.Equal(t, "namespaces", resources[2].Resource)
	assert.Equal(t, "", resources[2].APIGroup)
	assert.False(t, resources[2].Namespaced)

	assert.Equal(t, "deployments", resources[3].Resource)
	assert.Equal(t, "apps", resources[3].APIGroup)
	assert.True(t, resources[3].Namespaced)

	assert.Equal(t, "roles", resources[4].Resource)
	assert.Equal(t, "rbac.authorization.k8s.io", resources[4].APIGroup)
	assert.True(t, resources[4].Namespaced)
}

func TestRender_Success(t *testing.T) {
	origDiscover := discoverResources
	t.Cleanup(func() { discoverResources = origDiscover })

	discoverResources = func(kubeconfig, context string) ([]APIResource, error) {
		return []APIResource{
			{APIGroup: "", Resource: "pods", Namespaced: true, Verbs: []string{"*"}},
			{APIGroup: "apps", Resource: "deployments", Namespaced: true, Verbs: []string{"*"}},
		}, nil
	}

	output, err := Render(RenderOptions{
		ClusterRoleName:         "test-role",
		ServiceAccountName:      "test-sa",
		ServiceAccountNamespace: "default",
	})
	require.NoError(t, err)
	assert.Contains(t, output, "kind: ClusterRole")
	assert.Contains(t, output, "name: test-role")
}

func TestRender_DiscoverFails(t *testing.T) {
	origDiscover := discoverResources
	t.Cleanup(func() { discoverResources = origDiscover })

	discoverResources = func(kubeconfig, context string) ([]APIResource, error) {
		return nil, fmt.Errorf("kubectl not found")
	}

	_, err := Render(RenderOptions{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to discover API resources")
}

func contains(slice []string, item string) bool {
	for _, s := range slice {
		if s == item {
			return true
		}
	}
	return false
}

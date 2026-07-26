package kube

// Carveout defines an RBAC restriction applied during rendering.
type Carveout struct {
	APIGroups    []string
	Resources    []string
	Subresources []string
	Verbs        []string
}

// DeniedAPIGroups returns apiGroups that are fully blocked for the agent.
func DeniedAPIGroups() []string {
	return []string{
		"rbac.authorization.k8s.io",
		"admissionregistration.k8s.io",
	}
}

// DeniedResources returns core-group resources that are blocked.
func DeniedResources() []string {
	return []string{
		"secrets",
		"serviceaccounts",
	}
}

// DeniedSubresources returns subresources that are blocked.
func DeniedSubresources() []string {
	return []string{
		"serviceaccounts/token",
		"pods/exec",
		"pods/attach",
	}
}

// GrantedSubresources returns core-group namespaced subresources that are
// always granted when their parent resource is discovered. Discovery via
// `kubectl api-resources` never lists subresources, and an RBAC grant on the
// parent resource does not extend to them, so they must be added explicitly.
func GrantedSubresources() []string {
	return []string{
		"pods/log",
	}
}

// ScopedWriteGroup narrows write access for a non-core API group: the agent
// gets read-only access to every resource in the group, but write verbs are
// limited to an explicit set of resources. It is only applied when the group
// is actually discovered in the target cluster (i.e. its CRDs are installed),
// so no rule is emitted for groups the cluster does not have.
type ScopedWriteGroup struct {
	APIGroup          string
	WritableResources []string
	WriteVerbs        []string
}

// ScopedWriteGroups returns non-core API groups whose write access is narrowed
// to specific resources. These override the default "namespaced group → full
// verbs on all resources" behaviour for the listed groups.
func ScopedWriteGroups() []ScopedWriteGroup {
	return []ScopedWriteGroup{
		{
			// ArgoCD: broad read for inspecting Applications/AppProjects, but
			// writes limited to ApplicationSets (e.g. patching target
			// revisions). Only rendered when the argoproj.io CRDs are
			// installed; the ClusterRole is cluster-wide, so this covers
			// ApplicationSets in every namespace.
			APIGroup:          "argoproj.io",
			WritableResources: []string{"applicationsets"},
			WriteVerbs:        []string{"update", "patch"},
		},
	}
}

// IsScopedWriteGroup reports whether a non-core API group has narrowed write
// access and must be excluded from the default wildcard grouping.
func IsScopedWriteGroup(group string) bool {
	for _, sg := range ScopedWriteGroups() {
		if group == sg.APIGroup {
			return true
		}
	}
	return false
}

// DeniedVerbs returns verbs that are filtered from all rules.
func DeniedVerbs() []string {
	return []string{
		"impersonate",
	}
}

// WritableClusterResources returns core-group cluster-scoped resources that
// need write access (e.g. nodes for cordon/drain).
func WritableClusterResources() []string {
	return []string{
		"nodes",
	}
}

// IsWritableClusterResource checks if a core-group cluster-scoped resource
// should receive full verbs instead of read-only.
func IsWritableClusterResource(resource string) bool {
	for _, w := range WritableClusterResources() {
		if resource == w {
			return true
		}
	}
	return false
}

// ReadOnlyVerbs returns the verbs allowed for cluster-scoped resources.
func ReadOnlyVerbs() []string {
	return []string{"get", "list", "watch"}
}

// IsAPIGroupDenied checks if an apiGroup is in the deny list.
func IsAPIGroupDenied(group string) bool {
	for _, denied := range DeniedAPIGroups() {
		if group == denied {
			return true
		}
	}
	return false
}

// IsResourceDenied checks if a core-group resource is in the deny list.
func IsResourceDenied(resource string) bool {
	for _, denied := range DeniedResources() {
		if resource == denied {
			return true
		}
	}
	return false
}

// IsSubresourceDenied checks if a subresource is in the deny list.
func IsSubresourceDenied(subresource string) bool {
	for _, denied := range DeniedSubresources() {
		if subresource == denied {
			return true
		}
	}
	return false
}

// FilterVerbs removes denied verbs from the given list.
func FilterVerbs(verbs []string) []string {
	denied := make(map[string]bool)
	for _, v := range DeniedVerbs() {
		denied[v] = true
	}
	var result []string
	for _, v := range verbs {
		if !denied[v] {
			result = append(result, v)
		}
	}
	return result
}

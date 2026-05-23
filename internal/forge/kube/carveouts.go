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

// DeniedVerbs returns verbs that are filtered from all rules.
func DeniedVerbs() []string {
	return []string{
		"impersonate",
	}
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

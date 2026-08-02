package main

import (
	"fmt"
	"strings"
)

// The policy reproduces the carveouts that `claude-forge kube render` bakes into
// the generated ClusterRole, so this first-party server enforces the *same*
// permission model at the MCP layer when it authenticates with an unrestricted
// local credential. Keep these lists in sync with
// internal/forge/kube/carveouts.go (they intentionally match).

// deniedAPIGroups are API groups blocked entirely (read and write). Touching
// RBAC or admission config is a privilege-escalation surface.
var deniedAPIGroups = map[string]bool{
	"rbac.authorization.k8s.io":    true,
	"admissionregistration.k8s.io": true,
}

// deniedCoreResources are core ("" group) resources blocked entirely. Their
// contents are credentials (Secret) or a token/escalation surface
// (ServiceAccount).
var deniedCoreResources = map[string]bool{
	"secrets":         true,
	"serviceaccounts": true,
}

// deniedSubresources are "<resource>/<subresource>" pairs blocked entirely.
// exec/attach are interactive command execution; serviceaccounts/token mints
// credentials.
var deniedSubresources = map[string]bool{
	"pods/exec":             true,
	"pods/attach":           true,
	"serviceaccounts/token": true,
}

// writableClusterResources are cluster-scoped resources that may be written
// (e.g. nodes, for cordon/drain). Every other cluster-scoped resource is
// read-only.
var writableClusterResources = map[string]bool{
	"nodes": true,
}

// writeVerbs are the mutating verbs.
var writeVerbs = map[string]bool{
	"create": true,
	"update": true,
	"patch":  true,
	"delete": true,
}

// isWriteVerb reports whether verb mutates cluster state.
func isWriteVerb(verb string) bool {
	return writeVerbs[verb]
}

// Policy enforces the kube-render carveouts on every resource operation. Unlike
// a coarse `--read-only` switch, it allows writes on ordinary resources while
// still denying the sensitive surfaces — matching what a rendered ServiceAccount
// could do under RBAC.
type Policy struct{}

// CheckResource reports whether an operation may proceed. group is the API group
// ("" for core), resource is the lowercase plural (e.g. "pods"), subresource is
// "" or e.g. "log"/"exec", verb is get/list/watch/create/update/patch/delete,
// and namespaced says whether the resource is namespace-scoped.
//
// Denials are phrased as non-retryable so the model does not loop.
func (p *Policy) CheckResource(group, resource, subresource, verb string, namespaced bool) error {
	if deniedAPIGroups[group] {
		return denyf("access to the %q API group is not permitted", group)
	}
	if group == "" && deniedCoreResources[resource] {
		return denyf("access to %q is not permitted (it exposes credentials)", resource)
	}
	if subresource != "" {
		if deniedSubresources[resource+"/"+subresource] {
			return denyf("the %s/%s subresource is not permitted", resource, subresource)
		}
	}
	if verb == "impersonate" {
		return denyf("impersonation is not permitted")
	}
	if !namespaced && isWriteVerb(verb) && !writableClusterResources[resource] {
		return denyf("writing cluster-scoped %q is not permitted (cluster-scoped resources are read-only, except nodes)", resource)
	}
	return nil
}

// denyf builds a non-retryable denial error.
func denyf(format string, args ...any) error {
	msg := fmt.Sprintf(format, args...)
	if !strings.HasSuffix(msg, ".") {
		msg += "."
	}
	return fmt.Errorf("%s Do not attempt this again — it will always be denied here", msg)
}

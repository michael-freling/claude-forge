package main

import "fmt"

// access classifies what a tool does, so the policy — not the tool handler —
// decides whether it may run. The zero value is deliberately invalid
// (accessUnset) so a newly registered tool that forgets to declare a
// classification fails the registry-completeness test rather than silently
// defaulting to a runnable state.
type access int

const (
	// accessUnset is the zero value: an unclassified tool. It is always denied.
	accessUnset access = iota
	// accessRead is a non-mutating read that returns no secret payload.
	accessRead
	// accessWrite mutates cloud state.
	accessWrite
	// accessSecret reads a secret payload (e.g. Secret Manager version access).
	// It is a read, but a distinct gate from accessWrite: reading a secret is
	// not a write, so the write gate does not cover it.
	accessSecret
)

// Policy enforces the read-only and secret-access gates. It is the second layer
// of defense; the primary layer is IAM (the service account the server
// authenticates as). Both gates default closed.
type Policy struct {
	// AllowWrites permits accessWrite tools. Defaults to false.
	AllowWrites bool
	// AllowSecretAccess permits accessSecret tools (reading secret payloads).
	// Independent of AllowWrites — enabling one never enables the other.
	AllowSecretAccess bool
}

// CheckTool reports whether a tool with the given classification may run under
// this policy. Denials carry a non-retryable phrasing so the model does not
// loop retrying a call that will always fail.
func (p *Policy) CheckTool(toolName string, a access) error {
	switch a {
	case accessRead:
		return nil
	case accessWrite:
		if p.AllowWrites {
			return nil
		}
		return fmt.Errorf("write access denied: tool %s is a write operation but this server is read-only "+
			"(started without --allow-writes); do not attempt to run this tool again — it will always fail here", toolName)
	case accessSecret:
		if p.AllowSecretAccess {
			return nil
		}
		return fmt.Errorf("secret access denied: tool %s returns a secret payload but this server was started "+
			"without --allow-secret-access; secret metadata is available but the payload is not, so do not attempt "+
			"to run this tool again — it will always fail here", toolName)
	default:
		return fmt.Errorf("tool %s has no read/write classification and is denied by default", toolName)
	}
}

package main

import "regexp"

// Credential-shaped patterns that must never reach the model through ordinary
// tool output. Secrets leak through more than Secret Manager payloads: Cloud
// Run env vars, Compute instance metadata, and Cloud Build substitutions can
// all carry a token or key inline, so redaction runs over *all* non-secret tool
// output regardless of which API produced it.
var redactPatterns = []*regexp.Regexp{
	// Google OAuth 2.0 access tokens.
	regexp.MustCompile(`ya29\.[0-9A-Za-z._\-]+`),
	// GCP API keys.
	regexp.MustCompile(`AIza[0-9A-Za-z._\-]{35}`),
	// PEM private key blocks (service-account keys, TLS keys, SSH keys).
	regexp.MustCompile(`(?s)-----BEGIN [A-Z0-9 ]*PRIVATE KEY-----.*?-----END [A-Z0-9 ]*PRIVATE KEY-----`),
	// The private_key field of a service-account JSON key, whether or not it
	// still contains a PEM block (the value may already be escaped/mangled).
	regexp.MustCompile(`"private_key"\s*:\s*"(?:[^"\\]|\\.)*"`),
}

const redactionPlaceholder = "[REDACTED]"

// redact replaces credential-shaped substrings in s with a placeholder. It is a
// belt-and-suspenders measure: the primary protection against returning secrets
// is IAM (roles/viewer excludes secret access) plus the accessSecret policy
// gate. Redaction catches secrets that leak through metadata surfaces.
func redact(s string) string {
	for _, re := range redactPatterns {
		s = re.ReplaceAllString(s, redactionPlaceholder)
	}
	return s
}

package main

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestRedact(t *testing.T) {
	tests := []struct {
		name    string
		in      string
		absent  string // substring that must be gone
		present string // substring that must remain (optional)
	}{
		{
			name:    "oauth access token",
			in:      `{"token":"ya29.a0AfB_byToKeNvAlUe-123"}`,
			absent:  "ya29.a0AfB_byToKeNvAlUe-123",
			present: `"token"`,
		},
		{
			name:   "api key",
			in:     `key=AIzaSyA1234567890abcdefghijklmnopqrstuvw`,
			absent: "AIzaSyA1234567890abcdefghijklmnopqrstuvw",
		},
		{
			name:   "pem private key",
			in:     "before\n-----BEGIN RSA PRIVATE KEY-----\nMIIEabc\n-----END RSA PRIVATE KEY-----\nafter",
			absent: "MIIEabc",
		},
		{
			name:   "service account private_key field",
			in:     `{"type":"service_account","private_key":"-----BEGIN PRIVATE KEY-----\nabc\n-----END PRIVATE KEY-----\n"}`,
			absent: "BEGIN PRIVATE KEY",
		},
		{
			name:    "no secrets untouched",
			in:      `{"name":"projects/p/instances/i","status":"RUNNING"}`,
			absent:  "REDACTED",
			present: "RUNNING",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			out := redact(tt.in)
			assert.NotContains(t, out, tt.absent)
			if tt.present != "" {
				assert.Contains(t, out, tt.present)
			}
			if tt.name != "no secrets untouched" {
				assert.Contains(t, out, redactionPlaceholder)
			}
		})
	}
}

func TestRedact_MultipleOccurrences(t *testing.T) {
	in := "ya29.aaa and ya29.bbb"
	out := redact(in)
	assert.Equal(t, redactionPlaceholder+" and "+redactionPlaceholder, out)
	assert.Equal(t, 2, strings.Count(out, redactionPlaceholder))
}

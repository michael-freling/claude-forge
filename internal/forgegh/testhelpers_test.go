package forgegh

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewTestClient(t *testing.T) {
	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}

	client := NewTestClient("http://example.com", stdout, stderr)

	require.NotNil(t, client)
	assert.Equal(t, "http://example.com", client.gatewayURL)
	assert.NotNil(t, client.httpClient)
	assert.Equal(t, stdout, client.stdout)
	assert.Equal(t, stderr, client.stderr)
}

func TestNewTestClient_TrimsTrailingSlash(t *testing.T) {
	client := NewTestClient("http://example.com/", &bytes.Buffer{}, &bytes.Buffer{})

	require.NotNil(t, client)
	assert.Equal(t, "http://example.com", client.gatewayURL)
}

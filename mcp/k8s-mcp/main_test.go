package main

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestContextList(t *testing.T) {
	var c contextList

	// Repeated flag.
	require.NoError(t, c.Set("prod"))
	require.NoError(t, c.Set("local"))
	assert.Equal(t, contextList{"prod", "local"}, c)

	// Comma-separated, with surrounding space and empty entries ignored.
	var c2 contextList
	require.NoError(t, c2.Set("prod, local ,,staging"))
	assert.Equal(t, contextList{"prod", "local", "staging"}, c2)

	assert.Equal(t, "prod,local,staging", c2.String())

	// An empty value contributes nothing, leaving the "serve everything" default.
	var c3 contextList
	require.NoError(t, c3.Set(""))
	assert.Empty(t, c3)
}

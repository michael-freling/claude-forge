package session

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestExtractContent(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		want string
	}{
		{"empty", "", ""},
		{"plain string", `"hello"`, "hello"},
		{"object with content", `{"role":"user","content":"hi there"}`, "hi there"},
		{"object without content", `{"role":"user"}`, ""},
		{"unsupported shape", `[1,2,3]`, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, extractContent(json.RawMessage(tt.raw)))
		})
	}
}

func TestDelete_RemovesTranscriptAndSidecar(t *testing.T) {
	dir := t.TempDir()
	subdir := "-work"

	// Seed a transcript and sidecar metadata for the session.
	sess := Session{ID: "abc123", Subdir: subdir}
	require.NoError(t, WriteMetadata(dir, sess.ID, Metadata{Name: "my session"}))

	transcriptDir := filepath.Join(dir, subdir)
	require.NoError(t, os.MkdirAll(transcriptDir, 0o755))
	transcript := filepath.Join(transcriptDir, sess.ID+".jsonl")
	require.NoError(t, os.WriteFile(transcript, []byte("{}"), 0o644))

	require.NoError(t, Delete(dir, sess))

	assert.NoFileExists(t, transcript)
	assert.NoFileExists(t, metadataPath(dir, sess.ID))

	// Deleting again (files already gone) is not an error.
	assert.NoError(t, Delete(dir, sess))
}
